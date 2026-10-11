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

//go:embed lenses.wgsl
var lensShader string

const lensSampleStride = 48

// Samples counts submitted sample rectangles; Copies/Draws count encoded
// operations. These counters do not establish GPU completion or pixel results.
type LensCounters struct{ Draws, Copies, Samples, UploadBytes int }

type lensLayer struct{ first, count int }
type lensSpan struct {
	first, count uint32
	read         [4]uint32 // physical xywh, rounded and clamped by native replay
}

// Lenses consumes retained stock operands without generating displacement or
// palette identities. The host ends its world pass, calls Snapshot, resumes a
// load pass and calls Draw for that same original layer/index (C-G3). Prepare,
// replacement and Close require completion of this component's previous use.
type Lenses struct {
	d                           *wgpu.Device
	q                           *wgpu.Queue
	format                      gputypes.TextureFormat
	owned                       []func()
	layout                      *wgpu.BindGroupLayout
	pipeline                    *wgpu.RenderPipeline
	viewport, samples           *wgpu.Buffer
	capacity                    uint64
	snapshot                    *wgpu.Texture
	snapshotUsage               gputypes.TextureUsage
	view                        *wgpu.TextureView
	group                       *wgpu.BindGroup
	textureWidth, textureHeight int
	layers                      []lensLayer
	lenses                      []lensSpan
	data                        []byte
	prepared, closed, ready     bool
	readyLayer, readyIndex      uint32
	counts                      LensCounters
}

func lensError(expected string) error {
	return fmt.Errorf("nanolathe: WebGPU stock lens failed: logical path presentation/stock-lens, providers searched [retained ordered stock effects], expected %s", expected)
}

func lensFinite(v float32) bool { return math.Float32bits(v)&0x7f800000 != 0x7f800000 }

// Native effects packing scales each lane once at float32 precision. Native
// effects replay then floors the origin and ceils the float32 far edge before
// clipping the copy to the physical world surface; it never moves the copy.
func lensReadRect(read [4]float32, scale float32, width, height int) ([4]uint32, error) {
	for i := range read {
		read[i] *= scale
		if !lensFinite(read[i]) {
			return [4]uint32{}, lensError("finite scaled snapshot bounds")
		}
	}
	if read[2] < 0 || read[3] < 0 || !lensFinite(read[0]+read[2]) || !lensFinite(read[1]+read[3]) {
		return [4]uint32{}, lensError("nonnegative finite snapshot spans")
	}
	// Clamping the unused side as well avoids an out-of-range host conversion
	// for wholly offscreen operands; those lenses still encode no copy/draw.
	x := int(min(max(math.Floor(float64(read[0])), 0), float64(width)))
	y := int(min(max(math.Floor(float64(read[1])), 0), float64(height)))
	ex := int(min(max(math.Ceil(float64(read[0]+read[2])), 0), float64(width)))
	ey := int(min(max(math.Ceil(float64(read[1]+read[3])), 0), float64(height)))
	if x >= ex || y >= ey {
		return [4]uint32{}, nil
	}
	return [4]uint32{uint32(x), uint32(y), uint32(ex - x), uint32(ey - y)}, nil
}

func (r *Lenses) pack(stock *meshscene.StockFrame, width, height int) error {
	r.layers, r.lenses, r.data = r.layers[:0], r.lenses[:0], r.data[:0]
	if stock == nil {
		return nil
	}
	if !(stock.Scale > 0) || !lensFinite(stock.Scale) {
		return lensError("finite positive stock presentation scale")
	}
	for _, layer := range stock.Layers {
		r.layers = append(r.layers, lensLayer{len(r.lenses), len(layer.Lenses)})
		for _, lens := range layer.Lenses {
			read, err := lensReadRect(lens.ReadRect, stock.Scale, width, height)
			if err != nil {
				return err
			}
			span := lensSpan{first: uint32(len(r.data) / lensSampleStride), read: read}
			if read[2] > 0 && read[3] > 0 && len(lens.Samples) > 0 {
				if uint64(len(lens.Samples)) > gputypes.DefaultLimits().MaxBufferSize/lensSampleStride-uint64(span.first) {
					return lensError("complete sample storage within the core buffer limit")
				}
				for _, sample := range lens.Samples {
					var record [lensSampleStride]byte
					for lane, vector := range [2][4]float32{sample.Rect, sample.Source} {
						for j, v := range vector {
							v *= stock.Scale
							if !lensFinite(v) {
								return lensError("finite scaled sample rectangles and coordinates")
							}
							binary.LittleEndian.PutUint32(record[lane*16+j*4:], math.Float32bits(v))
						}
					}
					for j, key := range lens.KeyRGB {
						binary.LittleEndian.PutUint32(record[32+j*4:], math.Float32bits(float32(key)))
					}
					r.data = append(r.data, record[:]...)
				}
				span.count = uint32(len(lens.Samples))
			}
			r.lenses = append(r.lenses, span)
		}
	}
	return nil
}

func lensLayoutEntries() []gputypes.BindGroupLayoutEntry {
	return []gputypes.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: 16}},
		{Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
	}
}

func lensVertexLayout() []gputypes.VertexBufferLayout {
	return []gputypes.VertexBufferLayout{{ArrayStride: lensSampleStride, StepMode: gputypes.VertexStepModeInstance, Attributes: []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x4, Offset: 0, ShaderLocation: 0},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 1},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 32, ShaderLocation: 2},
	}}}
}

func NewLenses(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat) (r *Lenses, err error) {
	if d == nil || q == nil {
		return nil, lensError("device, queue and world color format")
	}
	r = &Lenses{d: d, q: q, format: format}
	defer func() {
		if err != nil {
			r.Close()
			r = nil
		}
	}()
	r.layout, err = d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: lensLayoutEntries()})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.layout.Release)
	pl, err := d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{r.layout}})
	if err != nil {
		return
	}
	r.owned = append(r.owned, pl.Release)
	module, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "battle-stock-lens", WGSL: lensShader})
	if err != nil {
		return
	}
	r.owned = append(r.owned, module.Release)
	// Native stock lenses replace copied RGBA and leave world depth untouched.
	r.pipeline, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "battle-stock-lens", Layout: pl,
		Vertex:       wgpu.VertexState{Module: module, EntryPoint: "lens_vertex", Buffers: lensVertexLayout()},
		Fragment:     &wgpu.FragmentState{Module: module, EntryPoint: "lens_fragment", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthCompare: gputypes.CompareFunctionAlways},
		Multisample:  gputypes.MultisampleState{Count: 1, Mask: 0xffffffff},
	})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.pipeline.Release)
	return
}

func lensCapacity(current, need uint64) uint64 {
	limit := gputypes.DefaultLimits().MaxBufferSize
	capacity := max(current, uint64(lensSampleStride))
	for capacity < need {
		capacity = min(capacity*2, limit)
	}
	return capacity
}

func (r *Lenses) targets(width, height int) error {
	if r.viewport == nil {
		b, err := r.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "battle-stock-lens-viewport", Size: 16, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		r.viewport = b
	}
	if r.snapshot != nil && r.textureWidth == width && r.textureHeight == height {
		return nil
	}
	t, err := r.d.CreateTexture(&wgpu.TextureDescriptor{Label: "battle-stock-lens-snapshot", Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: r.format, Usage: wgpu.TextureUsageCopyDst | wgpu.TextureUsageTextureBinding})
	if err != nil {
		return err
	}
	v, err := r.d.CreateTextureView(t, nil)
	if err != nil {
		t.Release()
		return err
	}
	g, err := r.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "battle-stock-lens", Layout: r.layout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: r.viewport, Size: 16}, {Binding: 1, TextureView: v}}})
	if err != nil {
		v.Release()
		t.Release()
		return err
	}
	r.closeSnapshot()
	r.snapshot, r.view, r.group, r.textureWidth, r.textureHeight = t, v, g, width, height
	return nil
}

func (r *Lenses) Prepare(f meshscene.LiveFrame, width, height int) error {
	if r == nil || r.closed {
		return lensError("open component")
	}
	r.prepared, r.ready, r.counts = false, false, LensCounters{}
	limit := int(gputypes.DefaultLimits().MaxTextureDimension2D)
	if width <= 0 || height <= 0 || width > limit || height > limit {
		return lensError("positive physical viewport within core texture dimensions")
	}
	if err := r.pack(f.StockEffects, width, height); err != nil {
		return err
	}
	if len(r.data) > 0 {
		if err := r.targets(width, height); err != nil {
			return err
		}
		if uint64(len(r.data)) > r.capacity {
			capacity := lensCapacity(r.capacity, uint64(len(r.data)))
			b, err := r.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "battle-stock-lens-samples", Size: capacity, Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst})
			if err != nil {
				return err
			}
			if r.samples != nil {
				r.samples.Release()
			}
			r.samples, r.capacity = b, capacity
		}
		var viewport [16]byte
		binary.LittleEndian.PutUint32(viewport[0:], math.Float32bits(float32(width)))
		binary.LittleEndian.PutUint32(viewport[4:], math.Float32bits(float32(height)))
		if err := r.q.WriteBuffer(r.viewport, 0, viewport[:]); err != nil {
			return err
		}
		if err := r.q.WriteBuffer(r.samples, 0, r.data); err != nil {
			return err
		}
		r.counts.UploadBytes = len(r.data) + len(viewport)
	}
	r.prepared = true
	return nil
}

func (r *Lenses) span(layer, index uint32) (lensSpan, bool) {
	if uint64(layer) >= uint64(len(r.layers)) {
		return lensSpan{}, false
	}
	l := r.layers[layer]
	if uint64(index) >= uint64(l.count) {
		return lensSpan{}, false
	}
	return r.lenses[l.first+int(index)], true
}

func lensCopyRegion(world, snapshot *wgpu.Texture, read [4]uint32) wgpu.TextureCopy {
	origin := wgpu.Origin3D{X: read[0], Y: read[1]}
	return wgpu.TextureCopy{Source: wgpu.ImageCopyTexture{Texture: world, Origin: origin, Aspect: gputypes.TextureAspectAll}, Destination: wgpu.ImageCopyTexture{Texture: snapshot, Origin: origin, Aspect: gputypes.TextureAspectAll}, Size: wgpu.Extent3D{Width: read[2], Height: read[3], DepthOrArrayLayers: 1}}
}

func lensCopyTransitions(world, snapshot *wgpu.Texture, previousSnapshot gputypes.TextureUsage) (before, after [2]wgpu.TextureBarrier) {
	rangeAll := wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1}
	before = [2]wgpu.TextureBarrier{
		{Texture: world, Range: rangeAll, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageRenderAttachment, NewUsage: wgpu.TextureUsageCopySrc}},
		{Texture: snapshot, Range: rangeAll, Usage: wgpu.TextureUsageTransition{OldUsage: previousSnapshot, NewUsage: wgpu.TextureUsageCopyDst}},
	}
	after = [2]wgpu.TextureBarrier{
		{Texture: world, Range: rangeAll, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageCopySrc, NewUsage: wgpu.TextureUsageRenderAttachment}},
		{Texture: snapshot, Range: rangeAll, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageCopyDst, NewUsage: wgpu.TextureUsageTextureBinding}},
	}
	return
}

func (r *Lenses) Snapshot(enc *wgpu.CommandEncoder, world *wgpu.Texture, layer, index uint32) error {
	if r == nil || r.closed || !r.prepared {
		return lensError("prepared open component")
	}
	if r.ready {
		return lensError("the preceding snapshot drawn before another lens copy")
	}
	span, ok := r.span(layer, index)
	if !ok {
		return lensError("original stock layer and lens indices")
	}
	if span.count == 0 {
		return nil
	}
	if enc == nil || world == nil || r.snapshot == nil || world == r.snapshot || world.Format() != r.format {
		return lensError("borrowed encoder and distinct same-format completed world texture")
	}
	// The pinned copy API defers encoding errors to Finish/Submit. The host
	// owns those boundaries; this component never finishes or submits an encoder.
	before, after := lensCopyTransitions(world, r.snapshot, r.snapshotUsage)
	enc.TransitionTextures(before[:])
	enc.CopyTextureToTexture(world, r.snapshot, []wgpu.TextureCopy{lensCopyRegion(world, r.snapshot, span.read)})
	enc.TransitionTextures(after[:])
	r.snapshotUsage = wgpu.TextureUsageTextureBinding
	r.ready, r.readyLayer, r.readyIndex = true, layer, index
	r.counts.Copies++
	return nil
}

func (r *Lenses) Draw(pass *wgpu.RenderPassEncoder, layer, index uint32) {
	if r == nil || r.closed || !r.prepared || !r.ready || pass == nil || layer != r.readyLayer || index != r.readyIndex {
		return
	}
	span, ok := r.span(layer, index)
	if !ok || span.count == 0 {
		return
	}
	pass.SetPipeline(r.pipeline)
	pass.SetBindGroup(0, r.group, nil)
	pass.SetVertexBuffer(0, r.samples, uint64(span.first)*lensSampleStride)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: span.count})
	r.ready = false
	r.counts.Draws++
	r.counts.Samples += int(span.count)
}

func (r *Lenses) Counters() LensCounters {
	if r == nil {
		return LensCounters{}
	}
	return r.counts
}

func (r *Lenses) closeSnapshot() {
	if r.group != nil {
		r.group.Release()
		r.group = nil
	}
	if r.view != nil {
		r.view.Release()
		r.view = nil
	}
	if r.snapshot != nil {
		r.snapshot.Release()
		r.snapshot = nil
	}
	r.snapshotUsage = 0
}

func (r *Lenses) Close() {
	if r == nil || r.closed {
		return
	}
	r.closed, r.prepared, r.ready = true, false, false
	r.closeSnapshot()
	if r.samples != nil {
		r.samples.Release()
		r.samples = nil
	}
	if r.viewport != nil {
		r.viewport.Release()
		r.viewport = nil
	}
	for i := len(r.owned) - 1; i >= 0; i-- {
		r.owned[i]()
	}
	r.owned = nil
}
