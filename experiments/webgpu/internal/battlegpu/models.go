// Package battlegpu is the isolated real-battle WebGPU experiment.
package battlegpu

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed liveposes.wgsl
var modelPoseShader string

//go:embed models.wgsl
var modelBodyShader string

//go:embed modelresolve.wgsl
var modelResolveShader string

const (
	modelVertexStride    = 80
	modelPoseStride      = 80
	modelInstanceStride  = 384
	modelUniformSize     = 256
	modelSlotStride      = 48
	modelShadowStride    = 96
	modelCoreBufferLimit = 128 << 20
)

type ModelCounters struct{ Draws, Dispatches, Instances, Pieces, Triangles, UploadBytes int }
type modelMesh struct {
	first, count         uint32
	edgeFirst, edgeCount uint32
	base                 int32
	pieces, centers      int
	selectedCount        int
}
type modelDraw struct {
	mesh     int
	instance uint32
	shadow   int
}
type modelPaint struct{ slot, phase uint32 }
type modelShadowPaint struct{ slot, subject uint32 }
type modelRelease func()
type modelBuffer struct {
	buffer *wgpu.Buffer
	size   uint64
}

// Models borrows detached meshscene frames only during Prepare. Compute borrows
// an encoder; Draw borrows a pass. The host establishes completion before Close.
// Geometry stays resident; endpoint, rule and material records grow as needed.
type Models struct {
	d                                                                                     *wgpu.Device
	q                                                                                     *wgpu.Queue
	format                                                                                gputypes.TextureFormat
	owned                                                                                 []modelRelease
	closed, prepared                                                                      bool
	meshes                                                                                []modelMesh
	materials                                                                             []meshscene.Material
	frames                                                                                [][4]float32
	vertices, indices, materialData, frameData, centers                                   *wgpu.Buffer
	endpoints, poses, instances, selected, lights, slots, shadowRecords                   modelBuffer
	uniform                                                                               *wgpu.Buffer
	textureView, paletteView                                                              *wgpu.TextureView
	sampler                                                                               *wgpu.Sampler
	poseLayout, bodyLayout, resolveLayout                                                 *wgpu.BindGroupLayout
	posePipeline                                                                          *wgpu.ComputePipeline
	bodyPipeline, outlinePipeline, shadowPipeline, resolvePipeline, shadowResolvePipeline *wgpu.RenderPipeline
	poseGroup, bodyGroup, resolveGroup                                                    *wgpu.BindGroup
	colorView, depthView, shadowView                                                      *wgpu.TextureView
	colorTexture, shadowTexture                                                           *wgpu.Texture
	atlasWidth, atlasHeight                                                               int
	width, height, pieceCount                                                             int
	generation                                                                            uint64
	poseBytes, instanceBytes, selectedBytes, lightBytes, slotBytes, shadowBytes           []byte
	draws, shadowDraws                                                                    []modelDraw
	paint                                                                                 []modelPaint
	shadowPaint                                                                           []modelShadowPaint
	shadowCount                                                                           int
	counters                                                                              ModelCounters
}

func modelError(reason string) error {
	return fmt.Errorf("nanolathe: WebGPU models failed: logical path retained battle models, providers searched [meshscene production composition], expected %s", reason)
}
func modelFloat(b []byte, at int, value float32) {
	binary.LittleEndian.PutUint32(b[at:], math.Float32bits(value))
}
func modelVec(b []byte, at int, value [4]float32) {
	for j, v := range value {
		modelFloat(b, at+j*4, v)
	}
}
func modelBytes(b []byte, n int) []byte {
	if cap(b) < n {
		b = make([]byte, n)
	}
	b = b[:n]
	clear(b)
	return b
}
func modelFinite(v float32) bool           { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func (m *Models) modelRetain(close func()) { m.owned = append(m.owned, close) }
func (m *Models) modelBuffer(label string, n uint64, usage gputypes.BufferUsage, data []byte) (*wgpu.Buffer, error) {
	if n < 4 {
		n = 4
	}
	if n > modelCoreBufferLimit {
		return nil, modelError("storage buffers within the default 128 MiB binding limit")
	}
	b, e := m.d.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: n, Usage: usage})
	if e != nil {
		return nil, e
	}
	m.modelRetain(b.Release)
	if len(data) > 0 {
		e = m.q.WriteBuffer(b, 0, data)
	}
	return b, e
}
func (m *Models) modelGrow(b *modelBuffer, label string, n int, usage gputypes.BufferUsage) (bool, error) {
	need := uint64(max(n, 64))
	if need <= b.size {
		return false, nil
	}
	size := uint64(64)
	for size < need {
		size *= 2
	}
	v, e := m.modelBuffer(label, size, usage|wgpu.BufferUsageCopyDst, nil)
	if e != nil {
		return false, e
	}
	b.buffer, b.size = v, size
	// Keep replaced resources alive until the host's completion boundary. A
	// queued frame may still reference them; Close releases every growth tier.
	return true, nil
}
func modelBufferEntry(binding uint32, stage gputypes.ShaderStages, kind gputypes.BufferBindingType, size uint64) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stage, Buffer: &gputypes.BufferBindingLayout{Type: kind, MinBindingSize: size}}
}
func modelTextureEntry(binding uint32) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}}
}
func (m *Models) modelLayout(entries []gputypes.BindGroupLayoutEntry) (*wgpu.BindGroupLayout, *wgpu.PipelineLayout, error) {
	b, e := m.d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if e != nil {
		return nil, nil, e
	}
	m.modelRetain(b.Release)
	p, e := m.d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{b}})
	if e == nil {
		m.modelRetain(p.Release)
	}
	return b, p, e
}
func (m *Models) modelModule(source string) (*wgpu.ShaderModule, error) {
	s, e := m.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: source})
	if e == nil {
		m.modelRetain(s.Release)
	}
	return s, e
}
func (m *Models) modelTexture(label string, w, h int, format gputypes.TextureFormat, usage gputypes.TextureUsage) (*wgpu.Texture, *wgpu.TextureView, error) {
	if w < 1 || h < 1 || w > 8192 || h > 8192 {
		return nil, nil, modelError("textures within default core 8192 dimensions")
	}
	t, e := m.d.CreateTexture(&wgpu.TextureDescriptor{Label: label, Size: wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: usage})
	if e != nil {
		return nil, nil, e
	}
	m.modelRetain(t.Release)
	v, e := m.d.CreateTextureView(t, nil)
	if e == nil {
		m.modelRetain(v.Release)
	}
	return t, v, e
}
func (m *Models) modelArt(label string, t meshscene.Texture) (*wgpu.TextureView, error) {
	if t.Width < 1 || t.Height < 1 || len(t.RGBA) != t.Width*t.Height*4 {
		return nil, modelError("valid RGBA model atlas and palette")
	}
	b, v, e := m.modelTexture(label, t.Width, t.Height, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopyDst)
	if e != nil {
		return nil, e
	}
	e = m.q.WriteTexture(&wgpu.ImageCopyTexture{Texture: b}, t.RGBA, &wgpu.ImageDataLayout{BytesPerRow: uint32(t.Width * 4), RowsPerImage: uint32(t.Height)}, &wgpu.Extent3D{Width: uint32(t.Width), Height: uint32(t.Height), DepthOrArrayLayers: 1})
	return v, e
}
func (m *Models) modelPipeline(module *wgpu.ShaderModule, layout *wgpu.PipelineLayout, vertex, fragment string, format gputypes.TextureFormat, depth int, blend bool, topology gputypes.PrimitiveTopology) (*wgpu.RenderPipeline, error) {
	target := gputypes.ColorTargetState{Format: format, WriteMask: gputypes.ColorWriteMaskAll}
	if blend {
		target.Blend = &gputypes.BlendState{Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}, Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}}
	}
	desc := &wgpu.RenderPipelineDescriptor{Layout: layout, Vertex: wgpu.VertexState{Module: module, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: topology, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}, Fragment: &wgpu.FragmentState{Module: module, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{target}}}
	if depth == 1 {
		desc.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}
	}
	if depth == 2 {
		desc.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthCompare: gputypes.CompareFunctionAlways}
	}
	p, e := m.d.CreateRenderPipeline(desc)
	if e == nil {
		m.modelRetain(p.Release)
	}
	return p, e
}

func NewModels(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, scene *meshscene.Scene) (m *Models, err error) {
	if d == nil || q == nil || scene == nil {
		return nil, modelError("device, queue and retained scene")
	}
	m = &Models{d: d, q: q, format: format, materials: slices.Clone(scene.Materials), frames: slices.Clone(scene.TextureFrames)}
	defer func() {
		if err != nil {
			m.Close()
			m = nil
		}
	}()
	var vb, ib, cb []byte
	for _, mesh := range scene.Meshes {
		if mesh.Pieces < 0 || len(mesh.Indices)%3 != 0 {
			return m, modelError("triangle-list retained mesh topology")
		}
		entry := modelMesh{first: uint32(len(ib) / 4), count: uint32(len(mesh.Indices)), base: int32(len(vb) / modelVertexStride), pieces: mesh.Pieces, centers: len(cb) / 16}
		for _, v := range mesh.Vertices {
			if int(v.Piece) >= mesh.Pieces || int(v.Material) >= len(scene.Materials) && v.Material != 0 {
				return m, modelError("vertex piece and material indices within topology")
			}
			if v.Material != 0 {
				local := int(scene.Materials[v.Material].Reserved)
				if local >= len(mesh.PrimitiveCenters) {
					return m, modelError("primitive material selectors aligned to face centers")
				}
				entry.selectedCount = max(entry.selectedCount, local+1)
			}
			b := make([]byte, modelVertexStride)
			for i, p := range v.Position {
				modelFloat(b, i*4, p)
			}
			for i, p := range v.Normal {
				modelFloat(b, 16+i*4, p)
			}
			for i, p := range v.UV {
				modelFloat(b, 32+i*4, p)
			}
			binary.LittleEndian.PutUint32(b[40:], v.Piece)
			binary.LittleEndian.PutUint32(b[44:], v.Material)
			for i, p := range v.Color {
				modelFloat(b, 48+i*4, p)
			}
			for i, p := range v.ShadeNormal {
				modelFloat(b, 64+i*4, p)
			}
			vb = append(vb, b...)
		}
		for _, index := range mesh.Indices {
			if int(index) >= len(mesh.Vertices) {
				return m, modelError("indices within resident vertex topology")
			}
			ib = binary.LittleEndian.AppendUint32(ib, index)
		}
		if len(mesh.EdgeIndices)%2 != 0 {
			return m, modelError("authored outline edge pairs")
		}
		entry.edgeFirst, entry.edgeCount = uint32(len(ib)/4), uint32(len(mesh.EdgeIndices))
		for _, index := range mesh.EdgeIndices {
			if int(index) >= len(mesh.Vertices) {
				return m, modelError("authored edge indices within resident vertices")
			}
			ib = binary.LittleEndian.AppendUint32(ib, index)
		}
		faces, e := modelFaceRecords(mesh, entry.centers)
		if e != nil {
			return m, e
		}
		cb = append(cb, faces...)
		m.meshes = append(m.meshes, entry)
	}
	mb := make([]byte, max(16, len(m.materials)*16))
	for i, v := range m.materials {
		at := i * 16
		binary.LittleEndian.PutUint32(mb[at:], v.FirstFrame)
		binary.LittleEndian.PutUint32(mb[at+4:], v.FrameCount)
		binary.LittleEndian.PutUint32(mb[at+8:], v.Kind)
		binary.LittleEndian.PutUint32(mb[at+12:], v.Reserved)
		if uint64(v.FirstFrame)+uint64(max(v.FrameCount, 1)) > uint64(len(m.frames)) {
			return m, modelError("material frame spans within immutable texture frames")
		}
	}
	fb := make([]byte, max(16, len(m.frames)*16))
	for i, v := range m.frames {
		modelVec(fb, i*16, v)
	}
	for _, u := range []struct {
		dst   **wgpu.Buffer
		label string
		data  []byte
		usage gputypes.BufferUsage
	}{{&m.vertices, "battle-model-vertices", vb, wgpu.BufferUsageStorage}, {&m.indices, "battle-model-indices", ib, wgpu.BufferUsageIndex}, {&m.materialData, "battle-materials", mb, wgpu.BufferUsageStorage}, {&m.frameData, "battle-texture-frames", fb, wgpu.BufferUsageStorage}, {&m.centers, "battle-face-centers", cb, wgpu.BufferUsageStorage}} {
		minimum := 16
		if u.dst == &m.vertices {
			minimum = modelVertexStride
		}
		if *u.dst, err = m.modelBuffer(u.label, uint64(max(len(u.data), minimum)), u.usage|wgpu.BufferUsageCopyDst, u.data); err != nil {
			return
		}
	}
	if m.uniform, err = m.modelBuffer("battle-model-uniforms", modelUniformSize, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst, nil); err != nil {
		return
	}
	if m.textureView, err = m.modelArt("battle-model-art", scene.Atlas); err != nil {
		return
	}
	pal := scene.ModelPalette
	if pal.Width == 0 {
		pal = meshscene.Texture{Width: 256, Height: 2, RGBA: make([]byte, 256*2*4)}
		for i := range 512 {
			copy(pal.RGBA[i*4:], []byte{255, 255, 255, 255})
		}
	}
	if pal.Width < 256 || pal.Height < 2 {
		return m, modelError("model palette with base and BLUE rows")
	}
	if m.paletteView, err = m.modelArt("battle-model-palette", pal); err != nil {
		return
	}
	if m.sampler, err = d.CreateSampler(&wgpu.SamplerDescriptor{AddressModeU: gputypes.AddressModeClampToEdge, AddressModeV: gputypes.AddressModeClampToEdge, AddressModeW: gputypes.AddressModeClampToEdge, MinFilter: gputypes.FilterModeNearest, MagFilter: gputypes.FilterModeNearest, MipmapFilter: gputypes.FilterModeNearest}); err != nil {
		return
	}
	m.modelRetain(m.sampler.Release)
	var layout *wgpu.PipelineLayout
	if m.poseLayout, layout, err = m.modelLayout([]gputypes.BindGroupLayoutEntry{modelBufferEntry(0, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeReadOnlyStorage, modelPoseStride), modelBufferEntry(1, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeStorage, modelPoseStride), modelBufferEntry(2, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeUniform, 80)}); err != nil {
		return
	}
	var module *wgpu.ShaderModule
	if module, err = m.modelModule(modelPoseShader); err != nil {
		return
	}
	if m.posePipeline, err = d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: layout, Module: module, EntryPoint: "interpolate"}); err != nil {
		return
	}
	m.modelRetain(m.posePipeline.Release)
	vf := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	entries := []gputypes.BindGroupLayoutEntry{modelBufferEntry(0, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 80), modelBufferEntry(1, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, modelPoseStride), modelBufferEntry(2, vf, gputypes.BufferBindingTypeReadOnlyStorage, modelInstanceStride), modelBufferEntry(3, vf, gputypes.BufferBindingTypeUniform, 80), modelTextureEntry(4), modelTextureEntry(5), modelBufferEntry(6, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 16), modelBufferEntry(7, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 16), modelBufferEntry(8, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 48), modelBufferEntry(9, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 64), {Binding: 10, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}}, modelBufferEntry(11, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 16)}
	if m.bodyLayout, layout, err = m.modelLayout(entries); err != nil {
		return
	}
	if module, err = m.modelModule(modelBodyShader); err != nil {
		return
	}
	if m.bodyPipeline, err = m.modelPipeline(module, layout, "model_vertex", "model_fragment", wgpu.TextureFormatRGBA8Unorm, 1, false, gputypes.PrimitiveTopologyTriangleList); err != nil {
		return
	}
	if m.outlinePipeline, err = m.modelPipeline(module, layout, "model_vertex", "outline_fragment", wgpu.TextureFormatRGBA8Unorm, 1, false, gputypes.PrimitiveTopologyLineList); err != nil {
		return
	}
	if m.shadowPipeline, err = m.modelPipeline(module, layout, "shadow_vertex", "shadow_fragment", wgpu.TextureFormatRGBA8Unorm, 0, false, gputypes.PrimitiveTopologyTriangleList); err != nil {
		return
	}
	entries = []gputypes.BindGroupLayoutEntry{modelBufferEntry(0, vf, gputypes.BufferBindingTypeReadOnlyStorage, 48), modelBufferEntry(1, vf, gputypes.BufferBindingTypeReadOnlyStorage, 96), modelBufferEntry(2, vf, gputypes.BufferBindingTypeUniform, 80), modelTextureEntry(3), modelTextureEntry(4), {Binding: 5, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}}}
	if m.resolveLayout, layout, err = m.modelLayout(entries); err != nil {
		return
	}
	if module, err = m.modelModule(modelResolveShader); err != nil {
		return
	}
	if m.resolvePipeline, err = m.modelPipeline(module, layout, "resolve_vertex", "resolve_fragment", format, 2, true, gputypes.PrimitiveTopologyTriangleList); err != nil {
		return
	}
	if m.shadowResolvePipeline, err = m.modelPipeline(module, layout, "shadow_resolve_vertex", "shadow_resolve_fragment", format, 2, true, gputypes.PrimitiveTopologyTriangleList); err != nil {
		return
	}
	return
}

// modelFaceRecords retains authored rings, without posing a vertex. Headers
// share the material's mesh-local selector with the centroid. GPU admission
// uses the whole projected ring, independent of triangle tessellation (§22.1).
func modelFaceRecords(mesh meshscene.Mesh, base int) ([]byte, error) {
	n := len(mesh.PrimitiveCenters)
	if n == 0 {
		return nil, nil
	}
	b := make([]byte, n*32)
	for i, center := range mesh.PrimitiveCenters {
		modelVec(b, i*32, center)
	}
	if len(mesh.Faces) == 0 {
		return b, nil
	}
	if len(mesh.Faces) != n {
		return nil, modelError("authored faces aligned to primitive material selectors")
	}
	for i, face := range mesh.Faces {
		if uint64(face.CornerStart)+uint64(face.CornerCount) > uint64(len(mesh.FaceCorners)) || face.CornerCount < 3 {
			return nil, modelError("valid authored face corner rings")
		}
		at := i*32 + 16
		binary.LittleEndian.PutUint32(b[at:], uint32(base+len(b)/16))
		binary.LittleEndian.PutUint32(b[at+4:], face.CornerCount)
		for _, v := range mesh.FaceCorners[face.CornerStart : face.CornerStart+face.CornerCount] {
			corner := make([]byte, 16)
			modelVec(corner, 0, [4]float32{v.Position[0], v.Position[1], v.Position[2], 0})
			b = append(b, corner...)
		}
	}
	return b, nil
}

func (m *Models) modelGroup(layout *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
	g, e := m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: entries})
	if e == nil {
		m.modelRetain(g.Release)
	}
	return g, e
}

// modelPackSlots allocates every final subject independently, including Metal's
// direct-slab slots. The experiment always resolves subjects in painter order.
func modelPackSlots(input []meshscene.ModelSlot) ([]meshscene.ModelSlot, int, int, error) {
	out := slices.Clone(input)
	width := 2048
	for _, s := range out {
		if !modelFinite(s.Screen[0]+s.Screen[1]+s.Screen[2]+s.Screen[3]) || s.Screen[2] <= 0 || s.Screen[3] <= 0 || s.Screen[2] > 8192 || s.Screen[3] > 8192 {
			return nil, 0, 0, modelError("finite positive subject rectangles within atlas dimensions")
		}
		for width < int(math.Ceil(float64(s.Screen[2]))) {
			width *= 2
		}
	}
	for {
		x, y, row := 0, 0, 0
		for i := range out {
			w, h := int(math.Ceil(float64(out[i].Screen[2]))), int(math.Ceil(float64(out[i].Screen[3])))
			if x+w > width {
				x, y, row = 0, y+row, 0
			}
			out[i].Atlas = [4]float32{float32(x), float32(y), float32(w), float32(h)}
			x += w
			row = max(row, h)
		}
		height := max(64, (y+row+63)&^63)
		if height <= 8192 {
			return out, width, height, nil
		}
		if width >= 8192 {
			return nil, 0, 0, modelError("all isolated model slots fitting an 8192-square atlas")
		}
		width *= 2
	}
}

func modelSelectLights(lights []meshscene.RetainedLight, screen [4]float32) ([8]uint32, uint32) {
	var ids [8]uint32
	var scores [8]float32
	count := uint32(0)
	x, y, extent := screen[0]+screen[2]*.5, screen[1]+screen[3]*.5, (screen[2]+screen[3])*.5
	for j, l := range lights {
		if l.Flags[0] == 0 {
			continue
		}
		dx, dy := l.PositionRadius[0]-x, l.PositionRadius[1]-l.PositionRadius[2]*.5-y
		r := l.PositionRadius[3]
		d2 := dx*dx + dy*dy
		if r <= 0 || d2 > (r*1.5+extent)*(r*1.5+extent) {
			continue
		}
		score := max(l.ColorGroundGain[0], l.ColorGroundGain[1], l.ColorGroundGain[2]) / (1 + d2/(r*r))
		at := count
		if at == 8 {
			at = 0
			for i := uint32(1); i < count; i++ {
				if scores[i] < scores[at] {
					at = i
				}
			}
			if score <= scores[at] {
				continue
			}
		} else {
			count++
		}
		ids[at], scores[at] = uint32(j), score
	}
	return ids, count
}

func (m *Models) Prepare(f meshscene.LiveFrame, width, height int) error {
	if m == nil || m.closed {
		return modelError("open model component")
	}
	m.prepared = false
	m.counters = ModelCounters{}
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 || len(f.Previous) != len(f.Current) || !modelFinite(f.Alpha) || f.Alpha < 0 || f.Alpha > 1 || !modelFinite(f.Camera[0]+f.Camera[1]+f.Camera[2]) || f.Camera[2] <= 0 {
		return modelError("finite frame, aligned endpoints and core viewport")
	}
	if len(f.ModelRules) != 0 && len(f.ModelRules) != len(f.Instances) || len(f.ModelProjections.Records) != 0 && len(f.ModelProjections.Records) != len(f.Instances) || len(f.ModelProjections.PieceFlags) != 0 && len(f.ModelProjections.PieceFlags) != len(f.Current) {
		return modelError("rules and projection metadata aligned to original instances/poses")
	}
	if f.Composed && len(f.Composition.InstanceSlots) != len(f.Instances) {
		return modelError("composed slot selectors aligned to original instances")
	}
	m.width, m.height, m.pieceCount = width, height, len(f.Current)
	m.draws, m.shadowDraws, m.paint = m.draws[:0], m.shadowDraws[:0], m.paint[:0]
	m.shadowPaint = m.shadowPaint[:0]
	m.shadowCount = 0
	inputSlots := f.Composition.Slots
	if !f.Composed {
		inputSlots = make([]meshscene.ModelSlot, len(f.Instances))
		for i, in := range f.Instances {
			opacity := in.Visual.State[1]
			if in.Visual.State == [4]float32{} {
				opacity = 1
			}
			inputSlots[i] = meshscene.ModelSlot{Screen: [4]float32{0, 0, float32(width), float32(height)}, Params: [4]float32{opacity, float32(in.Phase + 1)}}
		}
	}
	// Grouped members rasterize into the final union region, sharing its key
	// test. TODO(question): full cached/live solo-source chronology and erased
	// hole propagation require the staged group compute port; this prototype
	// preserves parent rules and signed wrapped child-key shifts only.
	slots, aw, ah, e := modelPackSlots(inputSlots)
	if e != nil {
		return e
	}
	m.slotBytes = modelBytes(m.slotBytes, max(modelSlotStride, len(slots)*modelSlotStride))
	for i, s := range slots {
		modelVec(m.slotBytes, i*48, s.Screen)
		modelVec(m.slotBytes, i*48+16, s.Atlas)
		modelVec(m.slotBytes, i*48+32, s.Params)
	}
	m.instanceBytes = modelBytes(m.instanceBytes, max(modelInstanceStride, len(f.Instances)*modelInstanceStride))
	m.selectedBytes = m.selectedBytes[:0]
	for i, in := range f.Instances {
		if in.Mesh < 0 || in.Mesh >= len(m.meshes) || in.PoseOffset < 0 || in.PoseOffset+m.meshes[in.Mesh].pieces > len(f.Current) {
			return modelError("live instance mesh and pose span within resident topology")
		}
		at := i * modelInstanceStride
		binary.LittleEndian.PutUint32(m.instanceBytes[at:], uint32(in.PoseOffset))
		binary.LittleEndian.PutUint32(m.instanceBytes[at+4:], uint32(m.meshes[in.Mesh].centers))
		binary.LittleEndian.PutUint32(m.instanceBytes[at+8:], uint32(len(m.selectedBytes)/48))
		binary.LittleEndian.PutUint32(m.instanceBytes[at+12:], uint32(len(in.Materials)))
		if len(in.Materials) > 0 {
			if len(in.Materials) < m.meshes[in.Mesh].selectedCount {
				return modelError("selected material spans aligned to the instance mesh")
			}
			for _, mat := range in.Materials {
				b := make([]byte, 48)
				modelVec(b, 0, mat.UV)
				modelVec(b, 16, mat.Color)
				modelVec(b, 32, mat.Params)
				m.selectedBytes = append(m.selectedBytes, b...)
			}
		}
		visual := in.Visual
		if visual.State == [4]float32{} {
			visual.State[1] = 1
		}
		modelVec(m.instanceBytes, at+16, visual.State)
		modelVec(m.instanceBytes, at+32, visual.Bounds)
		modelVec(m.instanceBytes, at+48, visual.Outline)
		modelVec(m.instanceBytes, at+64, visual.Emission)
		var rule meshscene.ModelRules
		if len(f.ModelRules) > 0 {
			rule = f.ModelRules[i]
		}
		slot := uint32(i + 1)
		if f.Composed {
			slot = f.Composition.FinalSlotForInstance(i)
			if i < len(f.Composition.MemberGroup) && f.Composition.MemberGroup[i] > 0 {
				gi := f.Composition.MemberGroup[i] - 1
				if int(gi) >= len(f.Composition.Groups) {
					return modelError("valid production model group")
				}
				g := f.Composition.Groups[gi]
				if int(g.ParentInstance) >= len(f.Instances) {
					return modelError("valid group parent")
				}
				if g.Keyed && len(f.ModelRules) > 0 {
					parent := f.ModelRules[g.ParentInstance]
					rule.Reveal, rule.Below, rule.Band, rule.Above, rule.Clip = parent.Reveal, parent.Below, parent.Band, parent.Above, parent.Clip
					for _, member := range g.Members {
						if member.Instance == uint32(i) {
							binary.LittleEndian.PutUint32(m.instanceBytes[at+316:], uint32(member.KeyDelta))
							break
						}
					}
				}
			}
		}
		for j, v := range [][4]float32{rule.Reveal, rule.Below, rule.Band, rule.Above, rule.Clip, rule.Shadow, rule.Outline} {
			modelVec(m.instanceBytes, at+80+j*16, v)
		}
		var projection meshscene.ModelProjection
		if len(f.ModelProjections.Records) > 0 {
			projection = f.ModelProjections.Records[i]
		}
		modelVec(m.instanceBytes, at+192, projection.Origin)
		modelVec(m.instanceBytes, at+208, projection.Anchors)
		modelVec(m.instanceBytes, at+224, projection.Control)
		if slot > uint32(len(slots)) {
			return modelError("valid final composition slot")
		}
		if slot > 0 {
			s := slots[slot-1]
			modelVec(m.instanceBytes, at+240, s.Screen)
			modelVec(m.instanceBytes, at+256, s.Atlas)
			ids, n := modelSelectLights(f.Lighting.Lights, s.Screen)
			for j, id := range ids {
				binary.LittleEndian.PutUint32(m.instanceBytes[at+272+j*4:], id)
			}
			binary.LittleEndian.PutUint32(m.instanceBytes[at+304:], n)
			binary.LittleEndian.PutUint32(m.instanceBytes[at+308:], 1)
			m.draws = append(m.draws, modelDraw{in.Mesh, uint32(i), 0})
			m.counters.Triangles += int(m.meshes[in.Mesh].count) / 3
		}
		binary.LittleEndian.PutUint32(m.instanceBytes[at+312:], uint32(len(f.ModelRules)))
	}
	if len(m.selectedBytes) == 0 {
		m.selectedBytes = modelBytes(m.selectedBytes, 48)
	}
	if f.Composed {
		for _, p := range f.Composition.Paint {
			if p.Slot == 0 || int(p.Slot) > len(slots) {
				return modelError("valid production painter slots")
			}
			m.paint = append(m.paint, modelPaint{p.Slot, p.Phase})
		}
	} else {
		for i, in := range f.Instances {
			m.paint = append(m.paint, modelPaint{uint32(i + 1), in.Phase + 1})
		}
	}
	// A separate projected silhouette atlas collapses each subject's overlapping
	// triangles before darkening the world once (GPU design §13.3). The source
	// body's alpha punches the shadow at its unshifted screen coordinates.
	m.shadowBytes = modelBytes(m.shadowBytes, max(modelShadowStride, len(f.Shadows.Subjects)*modelShadowStride))
	for si, s := range f.Shadows.Subjects {
		if int(s.Order[3]) >= len(f.Instances) {
			return modelError("valid shadow source instance")
		}
		i := int(s.Order[3])
		slot := uint32(0)
		if f.Composed {
			slot = f.Composition.FinalSlotForInstance(i)
		}
		if slot == 0 || int(slot) > len(slots) {
			continue
		}
		if s.Params[0] < .5 {
			continue
		}
		m.shadowCount = max(m.shadowCount, si+1)
		at := si * 96
		modelVec(m.shadowBytes, at, s.Screen)
		modelVec(m.shadowBytes, at+16, slots[slot-1].Screen)
		modelVec(m.shadowBytes, at+32, slots[slot-1].Atlas)
		modelVec(m.shadowBytes, at+48, s.Params)
		modelVec(m.shadowBytes, at+64, s.Placement)
		// Use one shadow per original instance. Group shadow sources can include
		// children; their independent solo masks are an explicit remaining gap.
		ia := len(m.instanceBytes)
		m.instanceBytes = append(m.instanceBytes, m.instanceBytes[i*modelInstanceStride:(i+1)*modelInstanceStride]...)
		modelVec(m.instanceBytes, ia+336, s.Params)
		modelVec(m.instanceBytes, ia+352, s.Placement)
		modelVec(m.instanceBytes, ia+368, s.Screen)
		m.shadowDraws = append(m.shadowDraws, modelDraw{f.Instances[i].Mesh, uint32(ia / modelInstanceStride), si})
		m.shadowPaint = append(m.shadowPaint, modelShadowPaint{slot, uint32(si)})
	}
	// Shadow regions use the world viewport directly. Their union coverage is
	// resolved per subject from a private vertical strip in the same page; this
	// keeps ownership independent even when their world rectangles overlap.
	shadowSlots := make([]meshscene.ModelSlot, len(f.Shadows.Subjects))
	for i, s := range f.Shadows.Subjects {
		r := s.Screen
		if r[2] <= 0 || r[3] <= 0 {
			r = [4]float32{0, 0, 1, 1}
		}
		shadowSlots[i] = meshscene.ModelSlot{Screen: r}
	}
	packed, sw, sh, e := modelPackSlots(shadowSlots)
	if e != nil {
		return e
	}
	aw, ah = max(aw, sw), max(ah, sh)
	for i, s := range packed {
		if i < m.shadowCount {
			if modelFinite(math.Float32frombits(binary.LittleEndian.Uint32(m.shadowBytes[i*96+48:]))) && math.Float32frombits(binary.LittleEndian.Uint32(m.shadowBytes[i*96+48:])) > .5 {
				modelVec(m.shadowBytes, i*96+80, s.Atlas)
			}
		}
	}
	for _, d := range m.shadowDraws {
		modelVec(m.instanceBytes, int(d.instance)*modelInstanceStride+320, packed[d.shadow].Atlas)
	}
	if aw > m.atlasWidth || ah > m.atlasHeight || m.colorView == nil {
		m.atlasWidth, m.atlasHeight = max(aw, m.atlasWidth), max(ah, m.atlasHeight)
		m.colorTexture, m.colorView, e = m.modelTexture("battle-model-subject-atlas", m.atlasWidth, m.atlasHeight, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageTextureBinding|wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageCopySrc)
		if e != nil {
			return e
		}
		_, m.depthView, e = m.modelTexture("battle-model-subject-depth", m.atlasWidth, m.atlasHeight, wgpu.TextureFormatDepth32Float, wgpu.TextureUsageRenderAttachment)
		if e != nil {
			return e
		}
		m.shadowTexture, m.shadowView, e = m.modelTexture("battle-model-shadow-atlas", m.atlasWidth, m.atlasHeight, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageTextureBinding|wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageCopySrc)
		if e != nil {
			return e
		}
		m.resolveGroup = nil
	}
	changed := false
	for _, u := range []struct {
		b     *modelBuffer
		label string
		n     int
		usage gputypes.BufferUsage
	}{{&m.endpoints, "battle-pose-endpoints", max(modelPoseStride, len(f.Current)*modelPoseStride*2), wgpu.BufferUsageStorage}, {&m.poses, "battle-rigid-poses", max(modelPoseStride, len(f.Current)*modelPoseStride), wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc}, {&m.instances, "battle-model-instances", len(m.instanceBytes), wgpu.BufferUsageStorage}, {&m.selected, "battle-selected-materials", len(m.selectedBytes), wgpu.BufferUsageStorage}, {&m.lights, "battle-retained-lights", max(64, len(f.Lighting.Lights)*64), wgpu.BufferUsageStorage}, {&m.slots, "battle-model-slots", len(m.slotBytes), wgpu.BufferUsageStorage}, {&m.shadowRecords, "battle-shadow-records", len(m.shadowBytes), wgpu.BufferUsageStorage}} {
		grow, err := m.modelGrow(u.b, u.label, u.n, u.usage)
		if err != nil {
			return err
		}
		changed = changed || grow
	}
	if changed {
		m.poseGroup, m.bodyGroup, m.resolveGroup = nil, nil, nil
	}
	if m.generation != f.Generation || f.Generation == 0 || len(m.poseBytes) != len(f.Current)*modelPoseStride*2 {
		m.poseBytes = modelBytes(m.poseBytes, len(f.Current)*modelPoseStride*2)
		for endpoint, poses := range [][]meshscene.Mat4{f.Previous, f.Current} {
			for i, p := range poses {
				for j, v := range p {
					modelFloat(m.poseBytes, (endpoint*len(poses)+i)*modelPoseStride+j*4, v)
				}
				at := (endpoint*len(poses) + i) * modelPoseStride
				if len(f.ModelProjections.PieceFlags) > 0 {
					binary.LittleEndian.PutUint32(m.poseBytes[at+64:], f.ModelProjections.PieceFlags[i])
				}
				if len(f.Shadows.PieceFlags) > 0 {
					if len(f.Shadows.PieceFlags) != len(poses) {
						return modelError("shadow piece flags aligned to poses")
					}
					binary.LittleEndian.PutUint32(m.poseBytes[at+68:], f.Shadows.PieceFlags[i])
				}
			}
		}
		if len(m.poseBytes) > 0 {
			if e = m.q.WriteBuffer(m.endpoints.buffer, 0, m.poseBytes); e != nil {
				return e
			}
			m.counters.UploadBytes += len(m.poseBytes)
		}
		m.generation = f.Generation
	}
	m.lightBytes = modelBytes(m.lightBytes, max(64, len(f.Lighting.Lights)*64))
	for i, l := range f.Lighting.Lights {
		at := i * 64
		modelVec(m.lightBytes, at, l.PositionRadius)
		modelVec(m.lightBytes, at+16, l.ColorGroundGain)
		modelVec(m.lightBytes, at+32, l.GroundAgeFadeKind)
		for j, v := range l.Flags {
			binary.LittleEndian.PutUint32(m.lightBytes[at+48+j*4:], v)
		}
	}
	var ub [modelUniformSize]byte
	modelVec(ub[:], 0, [4]float32{f.Camera[0], f.Camera[1], f.Camera[2], f.Alpha})
	modelVec(ub[:], 16, [4]float32{float32(width), float32(height), float32(m.atlasWidth), float32(m.atlasHeight)})
	modelVec(ub[:], 32, [4]float32{float32(len(f.Current)), float32(len(f.Lighting.Lights)), float32(f.LightControls), float32(len(f.ModelRules))})
	modelVec(ub[:], 48, f.ModelProjections.View)
	modelVec(ub[:], 64, f.Shadows.Tint)
	for _, u := range []struct {
		b    *wgpu.Buffer
		data []byte
	}{{m.instances.buffer, m.instanceBytes}, {m.selected.buffer, m.selectedBytes}, {m.lights.buffer, m.lightBytes}, {m.slots.buffer, m.slotBytes}, {m.shadowRecords.buffer, m.shadowBytes}, {m.uniform, ub[:]}} {
		if e = m.q.WriteBuffer(u.b, 0, u.data); e != nil {
			return e
		}
		m.counters.UploadBytes += len(u.data)
	}
	if m.poseGroup == nil {
		m.poseGroup, e = m.modelGroup(m.poseLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: m.endpoints.buffer}, {Binding: 1, Buffer: m.poses.buffer}, {Binding: 2, Buffer: m.uniform, Size: modelUniformSize}})
		if e != nil {
			return e
		}
	}
	if m.bodyGroup == nil {
		m.bodyGroup, e = m.modelGroup(m.bodyLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: m.vertices}, {Binding: 1, Buffer: m.poses.buffer}, {Binding: 2, Buffer: m.instances.buffer}, {Binding: 3, Buffer: m.uniform, Size: modelUniformSize}, {Binding: 4, TextureView: m.textureView}, {Binding: 5, TextureView: m.paletteView}, {Binding: 6, Buffer: m.materialData}, {Binding: 7, Buffer: m.frameData}, {Binding: 8, Buffer: m.selected.buffer}, {Binding: 9, Buffer: m.lights.buffer}, {Binding: 10, Sampler: m.sampler}, {Binding: 11, Buffer: m.centers}})
		if e != nil {
			return e
		}
	}
	if m.resolveGroup == nil {
		m.resolveGroup, e = m.modelGroup(m.resolveLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: m.slots.buffer}, {Binding: 1, Buffer: m.shadowRecords.buffer}, {Binding: 2, Buffer: m.uniform, Size: modelUniformSize}, {Binding: 3, TextureView: m.colorView}, {Binding: 4, TextureView: m.shadowView}, {Binding: 5, Sampler: m.sampler}})
		if e != nil {
			return e
		}
	}
	m.counters.Instances, m.counters.Pieces = len(f.Instances), len(f.Current)
	m.prepared = true
	return nil
}

func (m *Models) Compute(enc *wgpu.CommandEncoder) error {
	if m == nil || m.closed || !m.prepared || enc == nil {
		return modelError("prepared models and borrowed encoder")
	}
	if m.pieceCount > 0 {
		p, e := enc.BeginComputePass(nil)
		if e != nil {
			return e
		}
		p.SetPipeline(m.posePipeline)
		p.SetBindGroup(0, m.poseGroup, nil)
		p.Dispatch(uint32((m.pieceCount+63)/64), 1, 1)
		e = p.End()
		if e != nil {
			return e
		}
		m.counters.Dispatches++
	}
	pass, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: m.colorView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: m.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	if e != nil {
		return e
	}
	pass.SetBindGroup(0, m.bodyGroup, nil)
	pass.SetIndexBuffer(m.indices, gputypes.IndexFormatUint32, 0)
	for _, d := range m.draws {
		mesh := m.meshes[d.mesh]
		if mesh.count == 0 {
			continue
		}
		if mesh.edgeCount > 0 {
			pass.SetPipeline(m.outlinePipeline)
			pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.edgeCount, InstanceCount: 1, FirstIndex: mesh.edgeFirst, BaseVertex: mesh.base, FirstInstance: d.instance})
			m.counters.Draws++
		}
		pass.SetPipeline(m.bodyPipeline)
		pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.count, InstanceCount: 1, FirstIndex: mesh.first, BaseVertex: mesh.base, FirstInstance: d.instance})
		m.counters.Draws++
	}
	e = pass.End()
	if e != nil {
		return e
	}
	pass, e = enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: m.shadowView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if e != nil {
		return e
	}
	pass.SetPipeline(m.shadowPipeline)
	pass.SetBindGroup(0, m.bodyGroup, nil)
	pass.SetIndexBuffer(m.indices, gputypes.IndexFormatUint32, 0)
	for _, d := range m.shadowDraws {
		mesh := m.meshes[d.mesh]
		pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.count, InstanceCount: 1, FirstIndex: mesh.first, BaseVertex: mesh.base, FirstInstance: d.instance})
		m.counters.Draws++
	}
	e = pass.End()
	return e
}

func (m *Models) Draw(pass *wgpu.RenderPassEncoder, phase uint32) {
	if m == nil || m.closed || !m.prepared || pass == nil {
		return
	}
	pass.SetPipeline(m.resolvePipeline)
	pass.SetBindGroup(0, m.resolveGroup, nil)
	for _, p := range m.paint {
		wanted := p.phase == phase+1 || phase == meshscene.PhaseGround && p.phase == meshscene.ModelPhaseShortFeatures
		if !wanted {
			continue
		}
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: p.slot - 1})
		m.counters.Draws++
	}
}
func (m *Models) DrawShadows(pass *wgpu.RenderPassEncoder) {
	if m == nil || m.closed || !m.prepared || pass == nil || m.shadowCount == 0 {
		return
	}
	pass.SetPipeline(m.shadowResolvePipeline)
	pass.SetBindGroup(0, m.resolveGroup, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(m.shadowCount)})
	m.counters.Draws++
}

// DrawSlot and DrawShadowSlot let the host interleave production model paint
// entries with feature sprites and projectile effect barriers. Slots are the
// original composition's one-based final slots, never private atlas selectors.
func (m *Models) DrawSlot(pass *wgpu.RenderPassEncoder, slot uint32) {
	if m == nil || m.closed || !m.prepared || pass == nil || slot == 0 || int(slot) > len(m.slotBytes)/modelSlotStride {
		return
	}
	pass.SetPipeline(m.resolvePipeline)
	pass.SetBindGroup(0, m.resolveGroup, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: slot - 1})
	m.counters.Draws++
}
func (m *Models) DrawShadowSlot(pass *wgpu.RenderPassEncoder, slot uint32) {
	if m == nil || m.closed || !m.prepared || pass == nil {
		return
	}
	pass.SetPipeline(m.shadowResolvePipeline)
	pass.SetBindGroup(0, m.resolveGroup, nil)
	for _, p := range m.shadowPaint {
		if p.slot != slot {
			continue
		}
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: p.subject})
		m.counters.Draws++
	}
}
func (m *Models) Counters() ModelCounters {
	if m == nil {
		return ModelCounters{}
	}
	return m.counters
}

// DebugResources exposes borrowed untimed readback sources. Pose stride
// is 80 bytes: a column-major Mat4 followed by four flag words. The caller must
// finish all copies/maps before the next Prepare or Close. Native texture reads
// need an explicit transition from TextureBinding before CopyTextureToBuffer.
func (m *Models) DebugResources() (color, shadow *wgpu.Texture, poses *wgpu.Buffer, width, height, pieceCount int) {
	if m == nil || m.closed || !m.prepared {
		return
	}
	return m.colorTexture, m.shadowTexture, m.poses.buffer, m.atlasWidth, m.atlasHeight, m.pieceCount
}

// DebugPacking returns detached copies of the 384-byte instance and 48-byte
// slot records uploaded by the last successful Prepare.
func (m *Models) DebugPacking() (instances, slots []byte) {
	if m == nil || m.closed || !m.prepared {
		return
	}
	return slices.Clone(m.instanceBytes), slices.Clone(m.slotBytes)
}
func (m *Models) Close() {
	if m == nil || m.closed {
		return
	}
	m.closed = true
	for i := len(m.owned) - 1; i >= 0; i-- {
		m.owned[i]()
	}
	m.owned = nil
}
