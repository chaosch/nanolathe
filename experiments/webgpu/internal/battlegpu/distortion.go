package battlegpu

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed distortion.wgsl
var distortionShader string

// The production publisher admits 32 blasts, 32 wreck plumes and 128 tree
// plumes (DESIGN_GPU_RENDERER §§25, 27–28), already in their paint order.
const distortionLimit = 32 + 32 + 128
const distortionStride = 64

type DistortionCounters struct{ Draws, Sources, UploadBytes int }

type distortionInputs struct {
	uniform                  *wgpu.Buffer
	world, grid, masks, glow *wgpu.TextureView
}

// Distortions borrows the final pass and its completed source views. Prepare,
// input replacement and Close require the host's prior completion boundary.
// The unchanged world/glow are sampled independently for each source, so later
// sources replace earlier ones without feedback; fog keeps destination pixels.
type Distortions struct {
	d          *wgpu.Device
	q          *wgpu.Queue
	owned      []func()
	closed     bool
	prepared   bool
	sources    *wgpu.Buffer
	layout     *wgpu.BindGroupLayout
	pipeline   *wgpu.RenderPipeline
	group      *wgpu.BindGroup
	inputs     distortionInputs
	count      int
	counts     DistortionCounters
	sourceData [distortionLimit * distortionStride]byte
}

func distortionError(expected string) error {
	return fmt.Errorf("nanolathe: WebGPU distortion failed: logical path presentation/distortion, providers searched [retained blast and heat sources], expected %s", expected)
}

func distortionPack(dst []byte, sources []meshscene.Distortion) error {
	if len(sources) > distortionLimit || len(dst) < len(sources)*distortionStride {
		return distortionError("the complete production 192-source budget and 64-byte records")
	}
	for i, source := range sources {
		if source.Shape[3] != 0 && source.Shape[3] != 1 {
			return distortionError("production blast or heat source kinds")
		}
		for lane, vector := range [4][4]float32{source.Previous, source.Current, source.Shape, source.Params} {
			for j, v := range vector {
				if math.Float32bits(v)&0x7f800000 == 0x7f800000 {
					return distortionError("finite retained positions, shape and timing operands")
				}
				binary.LittleEndian.PutUint32(dst[i*distortionStride+lane*16+j*4:], math.Float32bits(v))
			}
		}
	}
	return nil
}

func distortionLayoutEntries() []gputypes.BindGroupLayoutEntry {
	both := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	entries := []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: both, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: uint64(layerUniformSize)}}}
	for binding := uint32(1); binding <= 4; binding++ {
		entries = append(entries, gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}})
	}
	return append(entries, gputypes.BindGroupLayoutEntry{Binding: 5, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage, MinBindingSize: distortionLimit * distortionStride}})
}

func NewDistortions(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, finalSource string) (r *Distortions, err error) {
	if d == nil || q == nil || finalSource == "" {
		return nil, distortionError("device, queue and shared final shader source")
	}
	r = &Distortions{d: d, q: q}
	defer func() {
		if err != nil {
			r.Close()
			r = nil
		}
	}()
	r.sources, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "battle-distortion-sources", Size: distortionLimit * distortionStride, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.sources.Release)
	r.layout, err = d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: distortionLayoutEntries()})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.layout.Release)
	pl, err := d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{r.layout}})
	if err != nil {
		return
	}
	r.owned = append(r.owned, pl.Release)
	module, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "battle-distortion-final", WGSL: finalSource + "\n" + distortionShader})
	if err != nil {
		return
	}
	r.owned = append(r.owned, module.Release)
	// No blending or depth: covered fragments replace the previous final
	// pixel from the same immutable world; discarded fragments preserve it.
	r.pipeline, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "battle-distortion-final", Layout: pl,
		Vertex:    wgpu.VertexState{Module: module, EntryPoint: "distortion_vertex"},
		Fragment:  &wgpu.FragmentState{Module: module, EntryPoint: "distortion_fragment", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}},
		Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff},
	})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.pipeline.Release)
	return
}

func (r *Distortions) Prepare(f meshscene.LiveFrame, width, height int) error {
	if r == nil || r.closed {
		return distortionError("open component")
	}
	r.prepared, r.count, r.counts = false, 0, DistortionCounters{}
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return distortionError("positive viewport within the default core 8192 texture dimensions")
	}
	if err := distortionPack(r.sourceData[:], f.Distortions); err != nil {
		return err
	}
	if len(f.Distortions) > 0 {
		if err := r.q.WriteBuffer(r.sources, 0, r.sourceData[:len(f.Distortions)*distortionStride]); err != nil {
			return err
		}
	}
	r.count = len(f.Distortions)
	r.counts.Sources, r.counts.UploadBytes = r.count, r.count*distortionStride
	r.prepared = true
	return nil
}

func (r *Distortions) SetInputs(uniform *wgpu.Buffer, world, grid, masks, glow *wgpu.TextureView) error {
	if r == nil || r.closed || uniform == nil || world == nil || grid == nil || masks == nil || glow == nil {
		return distortionError("open component and all shared final inputs")
	}
	in := distortionInputs{uniform, world, grid, masks, glow}
	if in == r.inputs && r.group != nil {
		return nil
	}
	group, err := r.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "battle-distortion-final", Layout: r.layout, Entries: []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: uniform, Size: uint64(layerUniformSize)},
		{Binding: 1, TextureView: world}, {Binding: 2, TextureView: grid}, {Binding: 3, TextureView: masks}, {Binding: 4, TextureView: glow},
		{Binding: 5, Buffer: r.sources, Size: distortionLimit * distortionStride},
	}})
	if err != nil {
		return err
	}
	if r.group != nil {
		r.group.Release()
	}
	r.group, r.inputs = group, in
	return nil
}

func (r *Distortions) Draw(pass *wgpu.RenderPassEncoder) {
	if r == nil || r.closed || !r.prepared || r.count == 0 || r.group == nil || pass == nil {
		return
	}
	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(0, r.group, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(r.count)})
	r.counts.Draws++
}

func (r *Distortions) Counters() DistortionCounters {
	if r == nil {
		return DistortionCounters{}
	}
	return r.counts
}

func (r *Distortions) Close() {
	if r == nil || r.closed {
		return
	}
	r.closed = true
	if r.group != nil {
		r.group.Release()
		r.group = nil
	}
	for i := len(r.owned) - 1; i >= 0; i-- {
		r.owned[i]()
	}
	r.owned = nil
	r.inputs = distortionInputs{}
}
