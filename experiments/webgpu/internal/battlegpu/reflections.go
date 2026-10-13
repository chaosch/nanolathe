package battlegpu

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed reflections.wgsl
var reflectionShader string

//go:embed reflectiontiles.wgsl
var reflectionTileShader string

const (
	reflectionUniformSize = 240
	reflectionVertexSize  = 64
	reflectionCornerCap   = 32768
	reflectionSourceCap   = reflectionCornerCap / 4
	reflectionTileSide    = 64
	reflectionModuleSplit = "// --- resolve module ---"
)

type ReflectionCounters struct{ Draws, Dispatches, Sources, UploadBytes int }

type reflectionTexture struct {
	t *wgpu.Texture
	v *wgpu.TextureView
}

func (t *reflectionTexture) close() {
	if t.v != nil {
		t.v.Release()
	}
	if t.t != nil {
		t.t.Release()
	}
	*t = reflectionTexture{}
}

type reflectionInputs struct {
	uniform     *wgpu.Buffer
	atlas, mask *wgpu.TextureView
}

// StockReflections replays the published billboard/stroke packets and native
// RGBA8 source/soft planes, 64-pixel classification and resolve (§26.6/§32.2).
// Inputs and encoders are borrowed. Prepare, SetInputs resource replacement and
// Close require the host's completion boundary; this component never submits.
// Model reflection sources, seabed promotion and underwater commits are absent.
type StockReflections struct {
	d                    *wgpu.Device
	q                    rendererQueue
	uploads              *textureUploads // optional; owned by Renderer, retires replaced buffers
	owned                []func()
	closed, prepared     bool
	encoded              bool
	width, height, count int
	capacity             int
	vertices, indices    *wgpu.Buffer
	control              *wgpu.Buffer
	color, soft, flags   reflectionTexture
	inputs               reflectionInputs
	sourceLayout         *wgpu.BindGroupLayout
	resolveLayout        *wgpu.BindGroupLayout
	tileLayout           *wgpu.BindGroupLayout
	source, resolve      *wgpu.RenderPipeline
	classify             *wgpu.ComputePipeline
	sourceGroup          *wgpu.BindGroup
	resolveGroup         *wgpu.BindGroup
	tileGroup            *wgpu.BindGroup
	sourceDirty          bool
	resolveDirty         bool
	tileDirty            bool
	counts               ReflectionCounters
	sourceBytes          []byte
}

func reflectionError(expected string) error {
	return fmt.Errorf("nanolathe: WebGPU stock reflections failed: logical path presentation/water-reflections, providers searched [published water source packets], expected %s", expected)
}

func reflectionFinite(v float32) bool { return math.Float32bits(v)&0x7f800000 != 0x7f800000 }

func reflectionVec(dst []byte, at int, v [4]float32) {
	for i, x := range v {
		binary.LittleEndian.PutUint32(dst[at+i*4:], math.Float32bits(x))
	}
}

func reflectionControls(scales [3]float32) ([4]float32, error) {
	for _, scale := range scales {
		if scale <= 0 || !reflectionFinite(scale) {
			return [4]float32{}, reflectionError("positive finite production record, effective and output scales")
		}
	}
	out := [4]float32{scales[0] * scales[2], scales[1] * scales[2], scales[2], 0}
	if !reflectionFinite(out[0]) || !reflectionFinite(out[1]) || out[0] <= 0 || out[1] <= 0 {
		return [4]float32{}, reflectionError("representable physical record and effective scales")
	}
	return out, nil
}

func reflectionPack(dst []byte, sources []meshscene.WaterSource, outputScale float32) ([]byte, int, error) {
	if outputScale <= 0 || !reflectionFinite(outputScale) {
		return nil, 0, reflectionError("positive finite physical output scale")
	}
	// TODO(question): mixed native admission needs the admitted model corner
	// cost from the face-budget pass. Add that explicit seam before composing
	// model sources; this stock-only prototype has zero model cost, so the
	// ordered stock prefix may consume the full 32768-corner cap (§32.2).
	count := min(len(sources), reflectionSourceCap)
	dst = slices.Grow(dst[:0], count*4*reflectionVertexSize)[:count*4*reflectionVertexSize]
	for i, source := range sources {
		kind := source.Vertices[0].PositionHeight[3]
		for corner, vertex := range source.Vertices {
			if kind != 1 && kind != 2 || vertex.PositionHeight[3] != kind {
				return nil, 0, reflectionError("consistent production sprite (1) or stroke (2) four-corner kinds")
			}
			for _, lane := range [4][4]float32{vertex.PositionHeight, vertex.UV, vertex.Color, vertex.Bounds} {
				for _, value := range lane {
					if !reflectionFinite(value) {
						return nil, 0, reflectionError("finite source positions, heights, atlas texels, palette colour and bounds")
					}
				}
			}
			vertex.PositionHeight[0] *= outputScale
			vertex.PositionHeight[1] *= outputScale
			if !reflectionFinite(vertex.PositionHeight[0]) || !reflectionFinite(vertex.PositionHeight[1]) {
				return nil, 0, reflectionError("representable physical source positions")
			}
			if i >= count {
				continue
			}
			at := (i*4 + corner) * reflectionVertexSize
			for lane, values := range [4][4]float32{vertex.PositionHeight, vertex.UV, vertex.Color, vertex.Bounds} {
				reflectionVec(dst, at+lane*16, values)
			}
		}
	}
	return dst, count, nil
}

func reflectionIndices(count int) []byte {
	out := make([]byte, count*6*4)
	for i := range count {
		for corner, offset := range [6]uint32{0, 1, 2, 0, 2, 3} {
			binary.LittleEndian.PutUint32(out[(i*6+corner)*4:], uint32(i*4)+offset)
		}
	}
	return out
}

func reflectionTextureEntry(binding uint32, stage gputypes.ShaderStages, kind gputypes.TextureSampleType) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stage, Texture: &gputypes.TextureBindingLayout{SampleType: kind, ViewDimension: gputypes.TextureViewDimension2D}}
}

func reflectionUniformEntry(binding uint32, stage gputypes.ShaderStages, size uint64) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stage, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: size}}
}

func (r *StockReflections) reflectionLayout(entries []gputypes.BindGroupLayoutEntry) (*wgpu.BindGroupLayout, *wgpu.PipelineLayout, error) {
	l, err := r.d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if err != nil {
		return nil, nil, err
	}
	r.owned = append(r.owned, l.Release)
	pl, err := r.d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{l}})
	if err != nil {
		return nil, nil, err
	}
	r.owned = append(r.owned, pl.Release)
	return l, pl, nil
}

func reflectionBlend() *gputypes.BlendState {
	premult := gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}
	return &gputypes.BlendState{Color: premult, Alpha: premult}
}

func reflectionSourceTargets() []gputypes.ColorTargetState {
	return []gputypes.ColorTargetState{
		{Format: gputypes.TextureFormatRGBA8Unorm, Blend: reflectionBlend(), WriteMask: gputypes.ColorWriteMaskAll},
		{Format: gputypes.TextureFormatRGBA8Unorm, Blend: reflectionBlend(), WriteMask: gputypes.ColorWriteMaskAll},
	}
}

func NewStockReflections(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat) (r *StockReflections, err error) {
	if d == nil || q == nil {
		return nil, reflectionError("device and queue")
	}
	r = &StockReflections{d: d, q: q}
	defer func() {
		if err != nil {
			r.Close()
			r = nil
		}
	}()
	sourceCode, resolveCode, ok := strings.Cut(reflectionShader, reflectionModuleSplit)
	if !ok {
		return r, reflectionError("separate source and resolve shader modules")
	}
	var pl *wgpu.PipelineLayout
	r.sourceLayout, pl, err = r.reflectionLayout([]gputypes.BindGroupLayoutEntry{
		reflectionUniformEntry(0, wgpu.ShaderStageVertex, reflectionUniformSize), reflectionTextureEntry(1, wgpu.ShaderStageFragment, gputypes.TextureSampleTypeFloat),
	})
	if err != nil {
		return
	}
	sourceModule, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "stock-reflection-source", WGSL: sourceCode})
	if err != nil {
		return
	}
	r.owned = append(r.owned, sourceModule.Release)
	attrs := make([]gputypes.VertexAttribute, 4)
	for i := range attrs {
		attrs[i] = gputypes.VertexAttribute{Format: gputypes.VertexFormatFloat32x4, Offset: uint64(i * 16), ShaderLocation: uint32(i)}
	}
	r.source, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "stock-reflection-source", Layout: pl,
		Vertex:    wgpu.VertexState{Module: sourceModule, EntryPoint: "reflection_stock_vertex", Buffers: []gputypes.VertexBufferLayout{{ArrayStride: reflectionVertexSize, StepMode: gputypes.VertexStepModeVertex, Attributes: attrs}}},
		Fragment:  &wgpu.FragmentState{Module: sourceModule, EntryPoint: "reflection_stock_fragment", Targets: reflectionSourceTargets()},
		Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff},
	})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.source.Release)
	entries := []gputypes.BindGroupLayoutEntry{
		reflectionUniformEntry(0, wgpu.ShaderStageFragment, reflectionUniformSize), reflectionUniformEntry(1, wgpu.ShaderStageFragment, 16),
	}
	for binding := uint32(2); binding <= 5; binding++ {
		kind := gputypes.TextureSampleTypeFloat
		if binding == 5 {
			kind = gputypes.TextureSampleTypeUint
		}
		entries = append(entries, reflectionTextureEntry(binding, wgpu.ShaderStageFragment, kind))
	}
	r.resolveLayout, pl, err = r.reflectionLayout(entries)
	if err != nil {
		return
	}
	resolveModule, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "stock-reflection-resolve", WGSL: resolveCode})
	if err != nil {
		return
	}
	r.owned = append(r.owned, resolveModule.Release)
	r.resolve, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "stock-reflection-resolve", Layout: pl,
		Vertex: wgpu.VertexState{Module: resolveModule, EntryPoint: "reflection_screen_vertex"}, Fragment: &wgpu.FragmentState{Module: resolveModule, EntryPoint: "reflection_resolve", Targets: []gputypes.ColorTargetState{{Format: format, Blend: reflectionBlend(), WriteMask: gputypes.ColorWriteMaskAll}}},
		DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthCompare: gputypes.CompareFunctionAlways},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff},
	})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.resolve.Release)
	r.tileLayout, pl, err = r.reflectionLayout([]gputypes.BindGroupLayoutEntry{
		reflectionTextureEntry(0, wgpu.ShaderStageCompute, gputypes.TextureSampleTypeFloat), reflectionTextureEntry(1, wgpu.ShaderStageCompute, gputypes.TextureSampleTypeFloat),
		{Binding: 2, Visibility: wgpu.ShaderStageCompute, StorageTexture: &gputypes.StorageTextureBindingLayout{Access: gputypes.StorageTextureAccessWriteOnly, Format: gputypes.TextureFormatR32Uint, ViewDimension: gputypes.TextureViewDimension2D}},
	})
	if err != nil {
		return
	}
	tileModule, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "stock-reflection-tiles", WGSL: reflectionTileShader})
	if err != nil {
		return
	}
	r.owned = append(r.owned, tileModule.Release)
	r.classify, err = d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: "stock-reflection-tiles", Layout: pl, Module: tileModule, EntryPoint: "reflection_classify"})
	if err != nil {
		return
	}
	r.owned = append(r.owned, r.classify.Release)
	return
}

func (r *StockReflections) reflectionTexture(width, height int, format gputypes.TextureFormat, usage gputypes.TextureUsage) (out reflectionTexture, err error) {
	out.t, err = r.d.CreateTexture(&wgpu.TextureDescriptor{Label: "stock-reflection-plane", Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: usage})
	if err != nil {
		return
	}
	out.v, err = r.d.CreateTextureView(out.t, nil)
	if err != nil {
		out.close()
	}
	return
}

func reflectionReleaseGroup(group **wgpu.BindGroup) {
	if *group != nil {
		(*group).Release()
		*group = nil
	}
}

func (r *StockReflections) reflectionResize(width, height int) error {
	if r.color.v != nil && r.width == width && r.height == height {
		return nil
	}
	usage := wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc
	color, err := r.reflectionTexture(width, height, gputypes.TextureFormatRGBA8Unorm, usage)
	if err != nil {
		return err
	}
	soft, err := r.reflectionTexture(width, height, gputypes.TextureFormatRGBA8Unorm, usage)
	if err != nil {
		color.close()
		return err
	}
	flags, err := r.reflectionTexture((width+reflectionTileSide-1)/reflectionTileSide, (height+reflectionTileSide-1)/reflectionTileSide, gputypes.TextureFormatR32Uint, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopySrc)
	if err != nil {
		color.close()
		soft.close()
		return err
	}
	reflectionReleaseGroup(&r.resolveGroup)
	reflectionReleaseGroup(&r.tileGroup)
	r.color.close()
	r.soft.close()
	r.flags.close()
	r.color, r.soft, r.flags, r.width, r.height = color, soft, flags, width, height
	r.resolveDirty, r.tileDirty = true, true
	return nil
}

func (r *StockReflections) reflectionBuffers(count int) error {
	if r.control == nil {
		b, err := r.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "stock-reflection-control", Size: 16, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		r.control, r.resolveDirty = b, true
	}
	if count <= r.capacity {
		return nil
	}
	capacity := max(r.capacity, 16)
	for capacity < count {
		capacity *= 2
	}
	vertices, err := r.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "stock-reflection-corners", Size: uint64(capacity * 4 * reflectionVertexSize), Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	indices, err := r.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "stock-reflection-fans", Size: uint64(capacity * 6 * 4), Usage: wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst})
	if err != nil {
		vertices.Release()
		return err
	}
	indexBytes := reflectionIndices(capacity)
	if err = r.q.WriteBuffer(indices, 0, indexBytes); err != nil {
		vertices.Release()
		indices.Release()
		return err
	}
	if r.vertices != nil {
		r.uploads.retire(r.vertices.Release)
		r.uploads.retire(r.indices.Release)
	}
	r.vertices, r.indices, r.capacity = vertices, indices, capacity
	r.counts.UploadBytes += len(indexBytes)
	return nil
}

func (r *StockReflections) Prepare(f meshscene.LiveFrame, width, height int) error {
	if r == nil || r.closed {
		return reflectionError("open component")
	}
	r.prepared, r.encoded, r.count, r.counts = false, false, 0, ReflectionCounters{}
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return reflectionError("positive viewport within the default core 8192 texture dimensions")
	}
	// Native waterObjectsReflecting uses visibleWater and positive mask step,
	// independently of surface shading/motion admission (Mask.w, §30).
	if len(f.WaterSources) == 0 || f.Water.Controls[3] < 0.5 || f.Water.Mask[0] <= 0 {
		r.prepared = true
		return nil
	}
	for _, values := range [][4]float32{f.Water.Phase, f.Water.Mask, f.Water.Controls} {
		for _, value := range values {
			if !reflectionFinite(value) {
				return reflectionError("finite published water observer, mask and controls")
			}
		}
	}
	control, err := reflectionControls(f.WaterObjectScales)
	if err != nil {
		return err
	}
	r.sourceBytes, r.count, err = reflectionPack(r.sourceBytes, f.WaterSources, f.WaterObjectScales[2])
	if err != nil {
		return err
	}
	if err = r.reflectionResize(width, height); err != nil {
		return err
	}
	if err = r.reflectionBuffers(r.count); err != nil {
		return err
	}
	var controlBytes [16]byte
	reflectionVec(controlBytes[:], 0, control)
	if err = r.q.WriteBuffer(r.control, 0, controlBytes[:]); err != nil {
		return err
	}
	if err = r.q.WriteBuffer(r.vertices, 0, r.sourceBytes); err != nil {
		return err
	}
	r.counts.Sources, r.counts.UploadBytes = r.count, r.counts.UploadBytes+len(r.sourceBytes)+16
	r.prepared = true
	return r.reflectionBind()
}

func (r *StockReflections) SetInputs(uniform *wgpu.Buffer, atlas, mask *wgpu.TextureView) error {
	if r == nil || r.closed || uniform == nil || atlas == nil || mask == nil {
		return reflectionError("open component and borrowed 240-byte uniform, effects atlas and water mask")
	}
	inputs := reflectionInputs{uniform, atlas, mask}
	if inputs != r.inputs {
		r.sourceDirty = r.sourceDirty || uniform != r.inputs.uniform || atlas != r.inputs.atlas
		r.resolveDirty = r.resolveDirty || uniform != r.inputs.uniform || mask != r.inputs.mask
		r.inputs = inputs
	}
	return r.reflectionBind()
}

func (r *StockReflections) reflectionGroup(dst **wgpu.BindGroup, layout *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry) error {
	group, err := r.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: entries})
	if err != nil {
		return err
	}
	reflectionReleaseGroup(dst)
	*dst = group
	return nil
}

func (r *StockReflections) reflectionBind() error {
	if !r.prepared || r.count == 0 || r.inputs.uniform == nil {
		return nil
	}
	if r.sourceDirty || r.sourceGroup == nil {
		if err := r.reflectionGroup(&r.sourceGroup, r.sourceLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: r.inputs.uniform, Size: reflectionUniformSize}, {Binding: 1, TextureView: r.inputs.atlas}}); err != nil {
			return err
		}
		r.sourceDirty = false
	}
	if r.resolveDirty || r.resolveGroup == nil {
		if err := r.reflectionGroup(&r.resolveGroup, r.resolveLayout, []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: r.inputs.uniform, Size: reflectionUniformSize}, {Binding: 1, Buffer: r.control, Size: 16},
			{Binding: 2, TextureView: r.color.v}, {Binding: 3, TextureView: r.soft.v}, {Binding: 4, TextureView: r.inputs.mask}, {Binding: 5, TextureView: r.flags.v},
		}); err != nil {
			return err
		}
		r.resolveDirty = false
	}
	if r.tileDirty || r.tileGroup == nil {
		if err := r.reflectionGroup(&r.tileGroup, r.tileLayout, []wgpu.BindGroupEntry{{Binding: 0, TextureView: r.color.v}, {Binding: 1, TextureView: r.soft.v}, {Binding: 2, TextureView: r.flags.v}}); err != nil {
			return err
		}
		r.tileDirty = false
	}
	return nil
}

func (r *StockReflections) Encode(enc *wgpu.CommandEncoder) error {
	if r == nil || r.closed || !r.prepared {
		return reflectionError("prepared open component")
	}
	if r.count == 0 {
		return nil
	}
	if enc == nil || r.sourceGroup == nil || r.tileGroup == nil || r.resolveGroup == nil {
		return reflectionError("borrowed encoder and rebound source, tile and resolve inputs")
	}
	r.encoded = false
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{Label: "stock-reflection-source", ColorAttachments: []wgpu.RenderPassColorAttachment{
		{View: r.color.v, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}, {View: r.soft.v, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore},
	}})
	if err != nil {
		return err
	}
	pass.SetPipeline(r.source)
	pass.SetBindGroup(0, r.sourceGroup, nil)
	pass.SetVertexBuffer(0, r.vertices, 0)
	pass.SetIndexBuffer(r.indices, gputypes.IndexFormatUint32, 0)
	pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: uint32(r.count * 6), InstanceCount: 1})
	if err = pass.End(); err != nil {
		return err
	}
	r.counts.Draws++
	compute, err := enc.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "stock-reflection-tiles"})
	if err != nil {
		return err
	}
	compute.SetPipeline(r.classify)
	compute.SetBindGroup(0, r.tileGroup, nil)
	compute.Dispatch(uint32((r.width+reflectionTileSide-1)/reflectionTileSide), uint32((r.height+reflectionTileSide-1)/reflectionTileSide), 1)
	if err = compute.End(); err != nil {
		return err
	}
	r.counts.Dispatches++
	r.encoded = true
	return nil
}

// Draw blends the water-mask-clipped, tinted 25-percent reflection immediately
// after terrain/surface and before ordinary bodies. Source planes stay distinct
// from this world attachment. The host must invalidate its cached paint state.
func (r *StockReflections) Draw(pass *wgpu.RenderPassEncoder) {
	if r == nil || r.closed || !r.prepared || !r.encoded || r.count == 0 || pass == nil {
		return
	}
	pass.SetPipeline(r.resolve)
	pass.SetBindGroup(0, r.resolveGroup, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	r.counts.Draws++
}

func (r *StockReflections) Counters() ReflectionCounters {
	if r == nil {
		return ReflectionCounters{}
	}
	return r.counts
}

func (r *StockReflections) Close() {
	if r == nil || r.closed {
		return
	}
	r.closed = true
	reflectionReleaseGroup(&r.sourceGroup)
	reflectionReleaseGroup(&r.resolveGroup)
	reflectionReleaseGroup(&r.tileGroup)
	r.color.close()
	r.soft.close()
	r.flags.close()
	for _, buffer := range []*wgpu.Buffer{r.vertices, r.indices, r.control} {
		if buffer != nil {
			buffer.Release()
		}
	}
	for i := len(r.owned) - 1; i >= 0; i-- {
		r.owned[i]()
	}
	r.owned, r.inputs = nil, reflectionInputs{}
}
