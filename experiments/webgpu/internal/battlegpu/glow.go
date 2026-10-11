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

//go:embed glow.wgsl
var glowShader string

//go:embed glow_blur.wgsl
var glowBlurShader string

const glowUniformSize = 80
const glowVertexSize = 48

type GlowCounters struct{ Draws, Dispatches, UploadBytes int }

type glowTexture struct {
	t    *wgpu.Texture
	v    *wgpu.TextureView
	w, h int
}

func (t *glowTexture) close() {
	if t.v != nil {
		t.v.Release()
	}
	if t.t != nil {
		t.t.Release()
	}
	*t = glowTexture{}
}

// Glow replays the production source packets and kernels (GPU design §19,
// Metal glow source/shrink/across/down). The host owns completion and submission;
// Prepare, replacement and Close require its completion boundary.
type Glow struct {
	d                         *wgpu.Device
	q                         *wgpu.Queue
	owned                     []func()
	closed, enabled           bool
	width, height, vertices   int
	vertexCapacity            int
	uniform, vertexBuffer     *wgpu.Buffer
	source                    *wgpu.RenderPipeline
	kernels                   [3]*wgpu.ComputePipeline
	sourceLayout, blurLayout  *wgpu.BindGroupLayout
	sourceGroup               *wgpu.BindGroup
	blurGroups                [3]*wgpu.BindGroup
	emission, quarter, across glowTexture
	octaves, atlas            glowTexture
	atlasVersion              uint64
	completed                 *wgpu.TextureView // borrowed from Encode; never released
	counts                    GlowCounters
	vertexBytes               []byte
}

func glowError(expected string, args ...any) error {
	return fmt.Errorf("nanolathe: WebGPU glow operation failed: logical path presentation/glow, providers searched [retained stock effects], expected %s", fmt.Sprintf(expected, args...))
}

func glowFloat(b []byte, at int, x float32) {
	binary.LittleEndian.PutUint32(b[at:], math.Float32bits(x))
}

func glowSizes(width, height int) [4]uint32 {
	return [4]uint32{uint32((width + 3) / 4), uint32((height + 3) / 4), uint32((width + 7) / 8), uint32((height + 7) / 8)}
}

func glowPackUniform(p meshscene.RetainedGlowParameters, width, height int) (out [glowUniformSize]byte) {
	for i, x := range p.Weights {
		glowFloat(out[:], i*4, x)
	}
	for i, x := range p.Blur {
		glowFloat(out[:], 32+i*4, x)
	}
	glowFloat(out[:], 48, float32(width))
	glowFloat(out[:], 52, float32(height))
	for i, x := range glowSizes(width, height) {
		binary.LittleEndian.PutUint32(out[64+i*4:], x)
	}
	return
}

func glowPackVertices(vertices []meshscene.RetainedGlowVertex) ([]byte, error) {
	return glowPackVerticesInto(nil, vertices)
}
func glowPackVerticesInto(out []byte, vertices []meshscene.RetainedGlowVertex) ([]byte, error) {
	// The host's metalVisuals adapter compacts the recorder's two triangles
	// to corners 0,1,2,3 before publication. Preserve all four upload records;
	// the vertex shader supplies their 0,1,2,1,2,3 draw topology.
	if len(vertices)%4 != 0 {
		return nil, glowError("complete four-corner source packets, got %d vertices", len(vertices))
	}
	if len(vertices) > (128<<20)/glowVertexSize {
		return nil, glowError("source storage within the core 128 MiB binding limit")
	}
	unsupported := 0
	for _, v := range vertices {
		if !(v.Params[0] >= 0 && v.Params[0] <= 3) {
			unsupported++
		}
	}
	if unsupported > 0 {
		return nil, glowError("solid/keyed/row-byte/halo operations, got %d unsupported source vertices", unsupported)
	}
	out = layerBytes(out, len(vertices)*glowVertexSize)
	for i, v := range vertices {
		for lane, value := range [3][4]float32{v.PositionUV, v.Color, v.Params} {
			for j, x := range value {
				glowFloat(out, i*glowVertexSize+lane*16+j*4, x)
			}
		}
	}
	return out, nil
}

func glowBufferEntry(binding uint32, stages gputypes.ShaderStages, kind gputypes.BufferBindingType, size uint64) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stages, Buffer: &gputypes.BufferBindingLayout{Type: kind, MinBindingSize: size}}
}

func glowTextureEntry(binding uint32, stages gputypes.ShaderStages) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stages, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}}
}

func (g *Glow) glowLayout(entries []gputypes.BindGroupLayoutEntry) (*wgpu.BindGroupLayout, *wgpu.PipelineLayout, error) {
	l, err := g.d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if err != nil {
		return nil, nil, err
	}
	g.owned = append(g.owned, l.Release)
	p, err := g.d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{l}})
	if err != nil {
		return nil, nil, err
	}
	g.owned = append(g.owned, p.Release)
	return l, p, nil
}

func NewGlow(d *wgpu.Device, q *wgpu.Queue, scene *meshscene.Scene) (g *Glow, err error) {
	g = &Glow{d: d, q: q}
	defer func() {
		if err != nil {
			g.Close()
		}
	}()
	if d == nil || q == nil || scene == nil {
		return g, glowError("device, queue and scene are required")
	}
	g.uniform, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "glow-uniform", Size: glowUniformSize, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
	if err != nil {
		return
	}
	g.owned = append(g.owned, g.uniform.Release)
	shader, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: glowShader})
	if err != nil {
		return
	}
	g.owned = append(g.owned, shader.Release)
	var pl *wgpu.PipelineLayout
	g.sourceLayout, pl, err = g.glowLayout([]gputypes.BindGroupLayoutEntry{
		glowBufferEntry(0, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeUniform, glowUniformSize),
		glowBufferEntry(1, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, glowVertexSize),
		glowTextureEntry(2, wgpu.ShaderStageFragment), glowTextureEntry(3, wgpu.ShaderStageFragment),
	})
	if err != nil {
		return
	}
	add := gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd}
	g.source, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "glow-source", Layout: pl,
		Vertex:    wgpu.VertexState{Module: shader, EntryPoint: "glow_source_vertex"},
		Fragment:  &wgpu.FragmentState{Module: shader, EntryPoint: "glow_source_fragment", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA16Float, Blend: &gputypes.BlendState{Color: add, Alpha: add}, WriteMask: gputypes.ColorWriteMaskAll}}},
		Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff},
	})
	if err != nil {
		return
	}
	g.owned = append(g.owned, g.source.Release)
	g.blurLayout, pl, err = g.glowLayout([]gputypes.BindGroupLayoutEntry{
		glowBufferEntry(0, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeUniform, glowUniformSize), glowTextureEntry(4, wgpu.ShaderStageCompute),
		{Binding: 5, Visibility: wgpu.ShaderStageCompute, StorageTexture: &gputypes.StorageTextureBindingLayout{Access: gputypes.StorageTextureAccessWriteOnly, Format: gputypes.TextureFormatRGBA16Float, ViewDimension: gputypes.TextureViewDimension2D}},
	})
	if err != nil {
		return
	}
	// Native Metal compiles the whole module with the active pipeline's
	// resource map. Source-only runtime-array metadata must not travel into
	// the compute layout, which has no vertex storage or source textures.
	blurShader, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: glowBlurShader})
	if err != nil {
		return
	}
	g.owned = append(g.owned, blurShader.Release)
	for i, name := range []string{"glow_shrink", "glow_across", "glow_down"} {
		g.kernels[i], err = d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: name, Layout: pl, Module: blurShader, EntryPoint: name})
		if err != nil {
			return
		}
		g.owned = append(g.owned, g.kernels[i].Release)
	}
	return
}

func (g *Glow) glowTexture(width, height int, format gputypes.TextureFormat, usage gputypes.TextureUsage) (out glowTexture, err error) {
	out.w, out.h = width, height
	out.t, err = g.d.CreateTexture(&wgpu.TextureDescriptor{Label: "glow-plane", Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: usage})
	if err != nil {
		return
	}
	out.v, err = g.d.CreateTextureView(out.t, nil)
	if err != nil {
		out.close()
	}
	return
}

func (g *Glow) glowGroup(dst **wgpu.BindGroup, layout *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry) error {
	b, err := g.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: entries})
	if err != nil {
		return err
	}
	if *dst != nil {
		(*dst).Release()
	}
	*dst = b
	return nil
}

func (g *Glow) glowBindSource() error {
	if g.completed == nil {
		return nil
	}
	return g.glowGroup(&g.sourceGroup, g.sourceLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: glowUniformSize}, {Binding: 1, Buffer: g.vertexBuffer}, {Binding: 2, TextureView: g.atlas.v}, {Binding: 3, TextureView: g.completed}})
}

func (g *Glow) glowPreparePlanes(width, height int) error {
	if g.width == width && g.height == height && g.octaves.v != nil {
		return nil
	}
	s := glowSizes(width, height)
	for _, item := range []struct {
		dst  *glowTexture
		w, h int
	}{{&g.emission, width, height}, {&g.quarter, int(s[0]), int(s[1])}, {&g.across, int(s[0] + s[2]), int(s[1])}, {&g.octaves, int(s[0] + s[2] + 3), int(s[1] + 2)}} {
		usage := wgpu.TextureUsageTextureBinding | wgpu.TextureUsageStorageBinding | wgpu.TextureUsageCopySrc
		if item.dst == &g.emission {
			usage = wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment
		}
		n, err := g.glowTexture(item.w, item.h, gputypes.TextureFormatRGBA16Float, usage|wgpu.TextureUsageCopySrc)
		if err != nil {
			return err
		}
		item.dst.close()
		*item.dst = n
	}
	for i, pair := range [3][2]*glowTexture{{&g.emission, &g.quarter}, {&g.quarter, &g.across}, {&g.across, &g.octaves}} {
		if err := g.glowGroup(&g.blurGroups[i], g.blurLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: glowUniformSize}, {Binding: 4, TextureView: pair[0].v}, {Binding: 5, TextureView: pair[1].v}}); err != nil {
			return err
		}
	}
	g.width, g.height = width, height
	return nil
}

func (g *Glow) glowUploadAtlas(stock *meshscene.StockFrame) (bool, error) {
	t := stock.Atlas
	if t.Width <= 0 || t.Height <= 0 || t.Width > 8192 || t.Height > 8192 || int64(t.Width)*int64(t.Height)*4 != int64(len(t.RGBA)) {
		return false, glowError("complete RGBA8 source atlas within core dimensions")
	}
	if stock.Version != 0 && stock.Version == g.atlasVersion && t.Width == g.atlas.w && t.Height == g.atlas.h {
		return false, nil
	}
	changed := false
	dirty := stock.Dirty
	if t.Width != g.atlas.w || t.Height != g.atlas.h {
		n, err := g.glowTexture(t.Width, t.Height, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopyDst)
		if err != nil {
			return false, err
		}
		g.atlas.close()
		g.atlas = n
		changed = true
	}
	if changed || g.atlasVersion == 0 || dirty[2] == 0 || dirty[3] == 0 {
		dirty = [4]uint32{0, 0, uint32(t.Width), uint32(t.Height)}
	}
	x, y, w, h := int(dirty[0]), int(dirty[1]), int(dirty[2]), int(dirty[3])
	if uint64(dirty[0])+uint64(dirty[2]) > uint64(t.Width) || uint64(dirty[1])+uint64(dirty[3]) > uint64(t.Height) {
		return changed, glowError("source atlas dirty rectangle within texture")
	}
	if err := g.q.WriteTexture(&wgpu.ImageCopyTexture{Texture: g.atlas.t, Origin: wgpu.Origin3D{X: uint32(x), Y: uint32(y)}}, t.RGBA[(y*t.Width+x)*4:], &wgpu.ImageDataLayout{BytesPerRow: uint32(t.Width * 4), RowsPerImage: uint32(h)}, &wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}); err != nil {
		return changed, err
	}
	g.counts.UploadBytes += w * h * 4
	g.atlasVersion = stock.Version
	return changed, nil
}

// Prepare uploads borrowed source records synchronously. All coordinates and
// kernel/gain operands are producer values; no camera transform is applied here.
func (g *Glow) Prepare(f meshscene.LiveFrame, width, height int) error {
	g.enabled = false
	g.counts = GlowCounters{}
	if g.closed || width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return glowError("open component and positive viewport within core 8192 dimensions")
	}
	if f.StockEffects == nil || len(f.StockEffects.Glow.Vertices) == 0 {
		return nil
	}
	stock := f.StockEffects
	data, err := glowPackVerticesInto(g.vertexBytes, stock.Glow.Vertices)
	g.vertexBytes = data
	if err != nil {
		return err
	}
	if err := g.glowPreparePlanes(width, height); err != nil {
		return err
	}
	changed, err := g.glowUploadAtlas(stock)
	if err != nil {
		return err
	}
	if g.vertexBuffer == nil || g.vertexCapacity < len(data) {
		capacity := glowVertexSize
		for capacity < len(data) {
			capacity *= 2
		}
		capacity = min(capacity, 128<<20)
		b, err := g.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "glow-sources", Size: uint64(capacity), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		if g.vertexBuffer != nil {
			g.vertexBuffer.Release()
		}
		g.vertexBuffer, g.vertexCapacity = b, capacity
		changed = true
	}
	if err := g.q.WriteBuffer(g.vertexBuffer, 0, data); err != nil {
		return err
	}
	u := glowPackUniform(stock.Glow.Parameters, width, height)
	if err := g.q.WriteBuffer(g.uniform, 0, u[:]); err != nil {
		return err
	}
	g.counts.UploadBytes += len(data) + len(u)
	if changed {
		// The previous completed view may have been replaced by the host on
		// resize. Rebind only from Encode's current borrowed view.
		g.completed = nil
	}
	g.vertices, g.enabled = len(stock.Glow.Vertices), true
	return nil
}

// Encode must follow completion of the world's render pass in the same encoder,
// before the final world/fog pass. It owns no submit or completion operation.
func (g *Glow) Encode(enc *wgpu.CommandEncoder, completedWorldView *wgpu.TextureView) error {
	if g.closed {
		return glowError("open component")
	}
	if !g.enabled {
		return nil
	}
	if enc == nil || completedWorldView == nil {
		return glowError("encoder and completed same-size world texture")
	}
	if g.completed != completedWorldView || g.sourceGroup == nil {
		g.completed = completedWorldView
		if err := g.glowBindSource(); err != nil {
			g.completed = nil
			return err
		}
	}
	p, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{Label: "glow-source", ColorAttachments: []wgpu.RenderPassColorAttachment{{View: g.emission.v, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if err != nil {
		return err
	}
	p.SetPipeline(g.source)
	p.SetBindGroup(0, g.sourceGroup, nil)
	p.Draw(gputypes.DrawArgs{VertexCount: uint32(g.vertices / 4 * 6), InstanceCount: 1})
	if err := p.End(); err != nil {
		return err
	}
	g.counts.Draws++
	for i, dst := range []*glowTexture{&g.quarter, &g.across, &g.octaves} {
		p, err := enc.BeginComputePass(nil)
		if err != nil {
			return err
		}
		p.SetPipeline(g.kernels[i])
		p.SetBindGroup(0, g.blurGroups[i], nil)
		p.Dispatch(uint32((dst.w+15)/16), uint32((dst.h+15)/16), 1)
		if err := p.End(); err != nil {
			return err
		}
		g.counts.Dispatches++
	}
	return nil
}

// Output borrows the RGBA16Float bordered octave atlas until resize/Close.
// For pixel centre p=physicalPixel/4, linearly sample near at p+1 and far at
// p/2+(ceil(width/4)+2,1); weight by producer Blur.z/w and clamp each to 0..1.
// Screen near/far, then screen that onto world BEFORE fog/map clip/WorldGain:
// glow=near+far-near*far; world=glow+world*(1-glow) (Metal nmFinalGlow).
func (g *Glow) Output() *wgpu.TextureView {
	if !g.Enabled() {
		return nil
	}
	return g.octaves.v
}

func (g *Glow) Enabled() bool          { return g != nil && !g.closed && g.enabled }
func (g *Glow) Counters() GlowCounters { return g.counts }

func (g *Glow) Close() {
	if g == nil || g.closed {
		return
	}
	g.closed = true
	for _, b := range append(g.blurGroups[:], g.sourceGroup) {
		if b != nil {
			b.Release()
		}
	}
	if g.vertexBuffer != nil {
		g.vertexBuffer.Release()
	}
	for _, t := range []*glowTexture{&g.emission, &g.quarter, &g.across, &g.octaves, &g.atlas} {
		t.close()
	}
	for i := len(g.owned) - 1; i >= 0; i-- {
		g.owned[i]()
	}
	g.owned = nil
	g.completed = nil
}
