// Package battlegpu is the isolated real-battle WebGPU experiment.
package battlegpu

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

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

//go:embed modelgroups.wgsl
var modelGroupsShader string

const (
	modelVertexStride    = 80
	modelPoseStride      = 80
	modelInstanceStride  = 416
	modelGroupStride     = 48
	modelMemberStride    = 16
	modelUniformSize     = 256
	modelSlotStride      = 48
	modelShadowStride    = 96
	modelCoreBufferLimit = 128 << 20
)

type ModelCounters struct {
	Draws, Dispatches, Instances, Pieces, Triangles, UploadBytes, PipelineBuilds int
	PreparationMS                                                                [6]float64
	ChunkedUploads                                                               bool
	// Attempted API calls/full payload bytes, including zero writes or failed prefixes.
	UploadWrites, ChunkedUploadBytes int
}
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
	outline  bool
	shadow   int
}
type modelPaint struct{ slot, phase uint32 }
type modelShadowPaint struct{ slot, subject uint32 }
type modelRelease func()
type modelBuffer struct {
	buffer *wgpu.Buffer
	size   uint64
}
type modelAtlasResources struct {
	color, depth, shadow             *wgpu.Texture
	colorView, depthView, shadowView *wgpu.TextureView
	metadata, merged                 *wgpu.Texture
	metadataView, mergedView         *wgpu.TextureView
	mergeGroup                       *wgpu.BindGroup
	width, height                    int
}

func (a *modelAtlasResources) modelReleaseGroup() {
	if a.mergeGroup != nil {
		a.mergeGroup.Release()
		a.mergeGroup = nil
	}
}
func (a *modelAtlasResources) modelClose() {
	a.modelReleaseGroup()
	for _, v := range []*wgpu.TextureView{a.mergedView, a.metadataView, a.shadowView, a.depthView, a.colorView} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{a.merged, a.metadata, a.shadow, a.depth, a.color} {
		if t != nil {
			t.Release()
		}
	}
	*a = modelAtlasResources{}
}

// Models borrows detached meshscene frames only during Prepare. Compute borrows
// an encoder; Draw borrows a pass. The host establishes completion before each
// Prepare and Close, so replaced atlas resources can be released immediately.
// Geometry stays resident; endpoint, rule and material records grow as needed.
type Models struct {
	chunkedUploads                                                              bool
	d                                                                           *wgpu.Device
	q                                                                           *wgpu.Queue
	format                                                                      gputypes.TextureFormat
	owned                                                                       []modelRelease
	closed, prepared                                                            bool
	runtimeArrays                                                               bool
	state                                                                       *battleRenderState
	meshes                                                                      []modelMesh
	materials                                                                   []meshscene.Material
	frames                                                                      [][4]float32
	vertices, indices, materialData, frameData, centers                         *wgpu.Buffer
	endpoints, poses, instances, selected, lights, slots, shadowRecords         modelBuffer
	groups, members                                                             modelBuffer
	uniform                                                                     *wgpu.Buffer
	textureView, paletteView                                                    *wgpu.TextureView
	sampler                                                                     *wgpu.Sampler
	poseLayout, bodyLayout, resolveLayout                                       *wgpu.BindGroupLayout
	bodyPipelineLayout                                                          *wgpu.PipelineLayout
	bodyWork                                                                    modelBodyPipelines
	resolvePipelineLayout                                                       *wgpu.PipelineLayout
	mergeLayout                                                                 *wgpu.BindGroupLayout
	mergePipelineLayout                                                         *wgpu.PipelineLayout
	posePipeline                                                                *wgpu.ComputePipeline
	bodyPipeline, outlinePipeline, resolvePipeline                              *wgpu.RenderPipeline
	commitWork                                                                  modelCommitPipeline
	poseGroup, bodyGroup                                                        *wgpu.BindGroup
	commitGroup                                                                 *wgpu.BindGroup
	commitViews                                                                 [5]*wgpu.TextureView
	mergePipeline, copyPipeline                                                 *wgpu.RenderPipeline
	mergeCapacities                                                             [3]int
	groupWork                                                                   modelGroupWork
	shadowWork                                                                  modelShadowWork
	copySlots                                                                   []uint32
	packedSlots, shadowSlots, shadowPacked, inputSlots                          []meshscene.ModelSlot
	atlas                                                                       modelAtlasResources
	width, height, pieceCount                                                   int
	generation                                                                  uint64
	poseBytes, instanceBytes, selectedBytes, lightBytes, slotBytes, shadowBytes []byte
	draws, shadowDraws                                                          []modelDraw
	batchWork                                                                   modelBatchWork
	packingWork                                                                 modelPackingWork
	disableModelBatching                                                        bool
	paint                                                                       []modelPaint
	shadowPaint                                                                 []modelShadowPaint
	shadowCount                                                                 int
	counters                                                                    ModelCounters
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

// The shared world group borrows both atlas families. Completion precedes
// Prepare/Close; release it before replacing either family's views.
func (m *Models) modelReleaseCommitGroup() {
	if m.commitGroup != nil {
		m.commitGroup.Release()
		m.commitGroup = nil
	}
	m.commitViews = [5]*wgpu.TextureView{}
}

func modelCommitLayoutEntries() []gputypes.BindGroupLayoutEntry {
	vf := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	return []gputypes.BindGroupLayoutEntry{
		// Both fixed arrays grow after New; validate their current capacities
		// against the actual buffers when the common pipeline is selected.
		modelBufferEntry(0, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(1, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(2, vf, gputypes.BufferBindingTypeUniform, 80),
		modelTextureEntry(3),
		modelBufferEntry(4, vf, gputypes.BufferBindingTypeUniform, 96),
		modelTextureEntry(5),
		{Binding: 6, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeDepth, ViewDimension: gputypes.TextureViewDimension2D}},
		modelTextureEntry(7), modelTextureEntry(8),
	}
}

func (m *Models) modelCommitTextureViews() [5]*wgpu.TextureView {
	a := &m.shadowWork.dummy
	if m.shadowCount > 0 {
		a = &m.shadowWork.atlas
	}
	return [5]*wgpu.TextureView{m.modelBodyView(), a.bodyView, a.depthView, a.maskView, m.shadowWork.waterView}
}

func (m *Models) modelCommitEntries(views [5]*wgpu.TextureView) []wgpu.BindGroupEntry {
	return []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.slots.buffer}, {Binding: 1, Buffer: m.shadowRecords.buffer},
		{Binding: 2, Buffer: m.uniform, Size: 80}, {Binding: 3, TextureView: views[0]},
		{Binding: 4, Buffer: m.shadowWork.uniform, Size: 96}, {Binding: 5, TextureView: views[1]},
		{Binding: 6, TextureView: views[2]}, {Binding: 7, TextureView: views[3]}, {Binding: 8, TextureView: views[4]},
	}
}

func (m *Models) modelSyncCommitViews(views [5]*wgpu.TextureView) {
	if views != m.commitViews {
		m.modelReleaseCommitGroup()
	}
}

func (m *Models) modelPrepareCommitGroup() error {
	views := m.modelCommitTextureViews()
	m.modelSyncCommitViews(views)
	if m.commitGroup != nil {
		return nil
	}
	g, err := m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: m.resolveLayout, Entries: m.modelCommitEntries(views)})
	if err == nil {
		m.commitGroup, m.commitViews = g, views
	}
	return err
}
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
	// Geometric capacity bounds all retained tiers to less than twice the
	// current buffer capacity; Close releases them and their old bind groups.
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
func (m *Models) modelCreateTexture(label string, w, h int, format gputypes.TextureFormat, usage gputypes.TextureUsage) (*wgpu.Texture, *wgpu.TextureView, error) {
	if w < 1 || h < 1 || w > 8192 || h > 8192 {
		return nil, nil, modelError("textures within default core 8192 dimensions")
	}
	t, e := m.d.CreateTexture(&wgpu.TextureDescriptor{Label: label, Size: wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: usage})
	if e != nil {
		return nil, nil, e
	}
	v, e := m.d.CreateTextureView(t, nil)
	if e != nil {
		t.Release()
		return nil, nil, e
	}
	return t, v, nil
}
func (m *Models) modelTexture(label string, w, h int, format gputypes.TextureFormat, usage gputypes.TextureUsage) (*wgpu.Texture, *wgpu.TextureView, error) {
	t, v, e := m.modelCreateTexture(label, w, h, format, usage)
	if e == nil {
		m.modelRetain(t.Release)
		m.modelRetain(v.Release)
	}
	return t, v, e
}

func modelAtlasExtent(need, current int) int {
	size := max(64, current)
	for size < need {
		size *= 2
	}
	return size
}
func (m *Models) modelResizeAtlas(width, height int) (err error) {
	if width <= m.atlas.width && height <= m.atlas.height && m.atlas.colorView != nil {
		return nil
	}
	next := modelAtlasResources{width: modelAtlasExtent(width, m.atlas.width), height: modelAtlasExtent(height, m.atlas.height)}
	defer func() {
		if err != nil {
			next.modelClose()
		}
	}()
	usage := wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc
	if next.color, next.colorView, err = m.modelCreateTexture("battle-model-subject-atlas", next.width, next.height, wgpu.TextureFormatRGBA8Unorm, usage); err != nil {
		return err
	}
	if next.depth, next.depthView, err = m.modelCreateTexture("battle-model-subject-depth", next.width, next.height, wgpu.TextureFormatDepth32Float, wgpu.TextureUsageRenderAttachment); err != nil {
		return err
	}
	// DebugResources retains its legacy transparent shadow diagnostic plane.
	if next.shadow, next.shadowView, err = m.modelCreateTexture("battle-model-shadow-dummy", 1, 1, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopyDst|wgpu.TextureUsageCopySrc); err != nil {
		return err
	}
	if err = m.q.WriteTexture(&wgpu.ImageCopyTexture{Texture: next.shadow}, []byte{0, 0, 0, 0}, &wgpu.ImageDataLayout{BytesPerRow: 4, RowsPerImage: 1}, &wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1}); err != nil {
		return err
	}
	m.counters.UploadBytes += 4
	if next.metadata, next.metadataView, err = m.modelCreateTexture("battle-model-source-keys", next.width, next.height, wgpu.TextureFormatRGBA8Unorm, usage); err != nil {
		return err
	}
	if m.groupWork.count > 0 || m.atlas.merged != nil {
		if next.merged, next.mergedView, err = m.modelCreateTexture("battle-model-merged-atlas", next.width, next.height, wgpu.TextureFormatRGBA8Unorm, usage); err != nil {
			return err
		}
	}
	// Prepare follows host completion. Release the old group before its views
	// and textures; atlas resources never enter the static/growth-tier owner.
	m.modelReleaseCommitGroup()
	m.atlas.modelClose()
	m.atlas = next
	return nil
}
func (m *Models) modelPrepareMergedAtlas() error {
	if m.groupWork.count == 0 || m.atlas.merged != nil {
		return nil
	}
	usage := wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc
	t, v, e := m.modelCreateTexture("battle-model-merged-atlas", m.atlas.width, m.atlas.height, wgpu.TextureFormatRGBA8Unorm, usage)
	if e != nil {
		return e
	}
	m.atlas.merged, m.atlas.mergedView = t, v
	return nil
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
func (m *Models) modelCreatePipeline(module *wgpu.ShaderModule, layout *wgpu.PipelineLayout, vertex, fragment string, format gputypes.TextureFormat, depth int, blend bool, topology gputypes.PrimitiveTopology) (*wgpu.RenderPipeline, error) {
	m.counters.PipelineBuilds++
	target := gputypes.ColorTargetState{Format: format, WriteMask: gputypes.ColorWriteMaskAll}
	if blend {
		target.Blend = &gputypes.BlendState{Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}, Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}}
	}
	desc := &wgpu.RenderPipelineDescriptor{Layout: layout, Vertex: wgpu.VertexState{Module: module, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: topology, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}, Fragment: &wgpu.FragmentState{Module: module, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{target}}}
	if fragment == "model_fragment" || fragment == "outline_fragment" {
		desc.Fragment.Targets = append(desc.Fragment.Targets, gputypes.ColorTargetState{Format: wgpu.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll})
	}
	if fragment == "model_shadow_fragment" {
		desc.Primitive.FrontFace, desc.Primitive.CullMode = gputypes.FrontFaceCW, gputypes.CullModeBack
	}
	if depth == 1 {
		desc.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}
	}
	if depth == 2 {
		desc.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthCompare: gputypes.CompareFunctionAlways}
	}
	return m.d.CreateRenderPipeline(desc)
}

func (m *Models) modelPipeline(module *wgpu.ShaderModule, layout *wgpu.PipelineLayout, vertex, fragment string, format gputypes.TextureFormat, depth int, blend bool, topology gputypes.PrimitiveTopology) (*wgpu.RenderPipeline, error) {
	p, e := m.modelCreatePipeline(module, layout, vertex, fragment, format, depth, blend, topology)
	if e == nil {
		m.modelRetain(p.Release)
	}
	return p, e
}

func modelResolveShaderSource(capacity int) string {
	return strings.ReplaceAll(modelResolveShader, "__SLOTS__", strconv.Itoa(capacity))
}

func modelResolveCapacity(bytes uint64) int {
	return max(1, int(bytes/modelSlotStride))
}

func NewModels(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, scene *meshscene.Scene) (m *Models, err error) {
	if d == nil || q == nil || scene == nil {
		return nil, modelError("device, queue and retained scene")
	}
	m = &Models{chunkedUploads: chunkedModelUploadsEnabled(d), runtimeArrays: runtimeArraysEnabled(d), d: d, q: q, format: format, materials: slices.Clone(scene.Materials), frames: slices.Clone(scene.TextureFrames)}
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
	if m.bodyLayout, m.bodyPipelineLayout, err = m.modelLayout(modelBodyLayoutEntries()); err != nil {
		return
	}
	if err = m.modelInitShadows(scene); err != nil {
		return
	}
	if storageReserveEnabled() {
		for i, u := range []struct {
			b     *modelBuffer
			label string
			usage gputypes.BufferUsage
		}{
			{&m.endpoints, "reserved-pose-endpoints", wgpu.BufferUsageStorage},
			{&m.poses, "reserved-rigid-poses", wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc},
			{&m.instances, "reserved-model-instances", wgpu.BufferUsageStorage},
			{&m.selected, "reserved-selected-materials", wgpu.BufferUsageStorage},
			{&m.lights, "reserved-model-lights", wgpu.BufferUsageStorage},
			{&m.slots, "reserved-model-slots", wgpu.BufferUsageStorage},
			{&m.shadowRecords, "reserved-shadow-records", wgpu.BufferUsageStorage},
			{&m.groups, "reserved-model-groups", wgpu.BufferUsageStorage},
			{&m.members, "reserved-group-members", wgpu.BufferUsageStorage},
		} {
			if _, err = m.modelGrow(u.b, u.label, modelStorageReserveBytes[i], u.usage); err != nil {
				return
			}
		}
	}
	if err = m.modelPrepareBodyPipelines(); err != nil {
		return
	}
	if m.resolveLayout, m.resolvePipelineLayout, err = m.modelLayout(modelCommitLayoutEntries()); err != nil {
		return
	}
	if err = m.modelPrepareCommitPipeline(); err != nil {
		return
	}
	vf := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	entries := []gputypes.BindGroupLayoutEntry{modelBufferEntry(0, vf, gputypes.BufferBindingTypeReadOnlyStorage, modelGroupStride), modelBufferEntry(1, wgpu.ShaderStageFragment, gputypes.BufferBindingTypeReadOnlyStorage, modelMemberStride), modelBufferEntry(2, vf, gputypes.BufferBindingTypeReadOnlyStorage, modelSlotStride), modelBufferEntry(3, vf, gputypes.BufferBindingTypeUniform, 80), modelTextureEntry(4), modelTextureEntry(5)}
	if m.mergeLayout, m.mergePipelineLayout, err = m.modelLayout(entries); err != nil {
		return
	}
	if storageReserveEnabled() {
		// Compile the optional group family before the first grouped subject.
		m.groupWork.count = 1
		err = m.modelPrepareMergePipeline()
		m.groupWork.count = 0
		if err != nil {
			return
		}
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

type modelGroupWork struct {
	groups, members []byte
	operands        [][4]int32
	clips           [][4]float32
	roles           []uint8 // 1 immutable source, 2 reserved final
	count           int
}

func (p *modelGroupWork) modelPrepare(f meshscene.LiveFrame) error {
	c := f.Composition
	p.count = 0
	p.operands = slices.Grow(p.operands[:0], len(f.Instances))[:len(f.Instances)]
	p.clips = slices.Grow(p.clips[:0], len(f.Instances))[:len(f.Instances)]
	p.roles = slices.Grow(p.roles[:0], len(c.Slots))[:len(c.Slots)]
	clear(p.operands)
	clear(p.clips)
	clear(p.roles)
	p.groups = modelBytes(p.groups, max(modelGroupStride, len(c.Groups)*modelGroupStride))
	p.members = p.members[:0]
	if !f.Composed || len(c.Groups) == 0 {
		if f.Composed {
			for _, group := range c.MemberGroup {
				if group != 0 {
					return modelError("group membership backed by exported groups")
				}
			}
		}
		p.members = modelBytes(p.members, modelMemberStride)
		return nil
	}
	if len(c.MemberGroup) != len(f.Instances) || len(c.InstanceSlots) != len(f.Instances) {
		return modelError("complete source selectors and group membership")
	}
	validSlot := func(slot uint32) bool { return slot > 0 && int(slot) <= len(c.Slots) }
	for gi, group := range c.Groups {
		if int(group.ParentInstance) >= len(f.Instances) || !validSlot(group.FinalSlot) || len(group.Members) == 0 || (group.ParentSlot != 0 && !validSlot(group.ParentSlot)) || p.roles[group.FinalSlot-1] != 0 {
			return modelError("valid ordered groups and unique reserved final slots")
		}
		p.roles[group.FinalSlot-1] = 2
		at := gi * modelGroupStride
		for j, value := range [4]uint32{group.ParentInstance, group.ParentSlot, group.FinalSlot, uint32(len(p.members) / modelMemberStride)} {
			binary.LittleEndian.PutUint32(p.groups[at+j*4:], value)
		}
		binary.LittleEndian.PutUint32(p.groups[at+16:], uint32(len(group.Members)))
		if group.PreserveHoles {
			binary.LittleEndian.PutUint32(p.groups[at+20:], 1)
		}
		if group.Keyed {
			binary.LittleEndian.PutUint32(p.groups[at+24:], 1)
		}
		var clip [4]float32
		if len(f.ModelRules) > 0 {
			if len(f.ModelRules) != len(f.Instances) {
				return modelError("group parent rules aligned to instances")
			}
			clip = f.ModelRules[group.ParentInstance].Clip
			binary.LittleEndian.PutUint32(p.groups[at+28:], 1)
		}
		modelVec(p.groups, at+32, clip)
		for mi, member := range group.Members {
			if int(member.Instance) >= len(f.Instances) || !validSlot(member.Slot) || p.roles[member.Slot-1] != 0 || c.InstanceSlots[member.Instance] != member.Slot || c.MemberGroup[member.Instance] != uint32(gi+1) || p.operands[member.Instance][0] != 0 {
				return modelError("unique immutable source slots and matching group members")
			}
			if member.Instance == group.ParentInstance && (mi != 0 || member.Slot != group.ParentSlot || member.KeyDelta != 0) {
				return modelError("parent seed first with unchanged key")
			}
			p.roles[member.Slot-1] = 1
			policy := int32(0)
			if !group.Keyed {
				policy = 2
			} else if member.Instance != group.ParentInstance {
				policy = 1
			}
			p.operands[member.Instance] = [4]int32{int32(gi + 1), policy, member.KeyDelta, int32(group.ParentInstance)}
			p.clips[member.Instance] = clip
			base := len(p.members)
			p.members = slices.Grow(p.members, modelMemberStride)[:base+modelMemberStride]
			for j, value := range [4]uint32{member.Instance, member.Slot, uint32(member.KeyDelta), 0} {
				binary.LittleEndian.PutUint32(p.members[base+j*4:], value)
			}
		}
	}
	for i, group := range c.MemberGroup {
		if group > uint32(len(c.Groups)) || (group != 0 && p.operands[i][0] != int32(group)) {
			return modelError("complete group membership sidecar")
		}
	}
	for i, slot := range c.InstanceSlots {
		if slot != 0 && (!validSlot(slot) || p.roles[slot-1] == 2 || p.roles[slot-1] == 1 && c.MemberGroup[i] == 0) {
			return modelError("instances targeting their immutable source or standalone slot")
		}
	}
	for _, paint := range c.Paint {
		if !validSlot(paint.Slot) || p.roles[paint.Slot-1] == 1 {
			return modelError("painting only final group slots")
		}
	}
	p.count = len(c.Groups)
	return nil
}

func modelGroupShaderSource(capacities [3]int, runtime ...bool) string {
	if len(runtime) > 0 && runtime[0] {
		return runtimeStorageArrays(modelGroupsShader)
	}
	return strings.NewReplacer("__GROUPS__", strconv.Itoa(capacities[0]), "__MEMBERS__", strconv.Itoa(capacities[1]), "__SLOTS__", strconv.Itoa(capacities[2])).Replace(modelGroupsShader)
}
func (m *Models) modelPrepareMergePipeline() error {
	if m.groupWork.count == 0 {
		return nil
	}
	capacities := [3]int{int(m.groups.size / modelGroupStride), int(m.members.size / modelMemberStride), int(m.slots.size / modelSlotStride)}
	if (m.runtimeArrays || capacities == m.mergeCapacities) && m.mergePipeline != nil {
		return nil
	}
	// Fixed arrays match geometric buffer capacities. The pinned render compiler
	// does not bind runtime-array size metadata; this stage never needs it.
	module, e := m.modelModule(modelGroupShaderSource(capacities, m.runtimeArrays))
	if e != nil {
		return e
	}
	merge, e := m.modelPipeline(module, m.mergePipelineLayout, "group_vertex", "group_fragment", wgpu.TextureFormatRGBA8Unorm, 0, false, gputypes.PrimitiveTopologyTriangleList)
	if e != nil {
		return e
	}
	copy, e := m.modelPipeline(module, m.mergePipelineLayout, "copy_vertex", "copy_fragment", wgpu.TextureFormatRGBA8Unorm, 0, false, gputypes.PrimitiveTopologyTriangleList)
	if e != nil {
		return e
	}
	m.mergePipeline, m.copyPipeline, m.mergeCapacities = merge, copy, capacities
	return nil
}
func (m *Models) modelBodyView() *wgpu.TextureView {
	if m.groupWork.count > 0 {
		return m.atlas.mergedView
	}
	return m.atlas.colorView
}

func modelGroupSourceRule(own meshscene.ModelRules, operand [4]int32) meshscene.ModelRules {
	if operand[0] != 0 && operand[1] != 2 {
		if operand[1] == 1 {
			own.Reveal[3] = 1
		}
		// A member's reveal remains its own. Carrier water/Digger erasure
		// follows the ordered merge; BLUE is selected before source shading.
		own.Clip[0], own.Clip[2] = 0, 0
	}
	return own
}

func modelOutlineNeeded(visual meshscene.ModelVisual, rule meshscene.ModelRules, ruled bool) bool {
	alpha := visual.Outline[3]
	if ruled {
		alpha = rule.Outline[3]
	}
	// Match outline_fragment's discard gates, including its comparison order.
	// This removes work with no color, metadata or depth side effects.
	return !(visual.State[2] <= 0 || alpha <= 0)
}

func (m *Models) Prepare(f meshscene.LiveFrame, width, height int) error {
	if m == nil || m.closed {
		return modelError("open model component")
	}
	m.prepared = false
	m.counters = ModelCounters{ChunkedUploads: m.chunkedUploads}
	mark := time.Now()
	split := func(i int) { n := time.Now(); m.counters.PreparationMS[i] = float64(n.Sub(mark)) / 1e6; mark = n }
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 || len(f.Previous) != len(f.Current) || !modelFinite(f.Alpha) || f.Alpha < 0 || f.Alpha > 1 || !modelFinite(f.Camera[0]+f.Camera[1]+f.Camera[2]) || f.Camera[2] <= 0 {
		return modelError("finite frame, aligned endpoints and core viewport")
	}
	if len(f.ModelRules) != 0 && len(f.ModelRules) != len(f.Instances) || len(f.ModelProjections.Records) != 0 && len(f.ModelProjections.Records) != len(f.Instances) || len(f.ModelProjections.PieceFlags) != 0 && len(f.ModelProjections.PieceFlags) != len(f.Current) {
		return modelError("rules and projection metadata aligned to original instances/poses")
	}
	if f.Composed && len(f.Composition.InstanceSlots) != len(f.Instances) {
		return modelError("composed slot selectors aligned to original instances")
	}
	priorGroups := m.groupWork.count
	if e := m.groupWork.modelPrepare(f); e != nil {
		return e
	}
	if (priorGroups == 0) != (m.groupWork.count == 0) {
		m.atlas.modelReleaseGroup()
	}
	m.width, m.height, m.pieceCount = width, height, len(f.Current)
	m.draws, m.shadowDraws, m.paint = m.draws[:0], m.shadowDraws[:0], m.paint[:0]
	m.shadowPaint = m.shadowPaint[:0]
	m.shadowCount = 0
	inputSlots := f.Composition.Slots
	if !f.Composed {
		m.inputSlots = slices.Grow(m.inputSlots[:0], len(f.Instances))[:len(f.Instances)]
		inputSlots = m.inputSlots
		for i, in := range f.Instances {
			opacity := in.Visual.State[1]
			if in.Visual.State == [4]float32{} {
				opacity = 1
			}
			inputSlots[i] = meshscene.ModelSlot{Screen: [4]float32{0, 0, float32(width), float32(height)}, Params: [4]float32{opacity, float32(in.Phase + 1)}}
		}
	}
	// TODO(question): the producer exports no raw/resized cached-plane seed or
	// cached→outline→live chronology. Immutable source rasters and ordered group
	// merges below preserve only the coverage this retained mesh pass can supply.
	slots, aw, ah, e := m.packingWork.pack(m.packedSlots, inputSlots)
	if e != nil {
		return e
	}
	m.packedSlots = slots
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
			slot = f.Composition.InstanceSlots[i]
		}
		operand := m.groupWork.operands[i]
		for j, value := range operand {
			binary.LittleEndian.PutUint32(m.instanceBytes[at+384+j*4:], uint32(value))
		}
		modelVec(m.instanceBytes, at+400, m.groupWork.clips[i])
		rule = modelGroupSourceRule(rule, operand)
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
			m.draws = append(m.draws, modelDraw{mesh: in.Mesh, instance: uint32(i), outline: modelOutlineNeeded(visual, rule, len(f.ModelRules) > 0)})
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
	m.copySlots = m.copySlots[:0]
	if m.groupWork.count > 0 {
		for _, p := range m.paint {
			if m.groupWork.roles[p.slot-1] == 0 {
				m.copySlots = append(m.copySlots, p.slot)
			}
		}
	}
	if e = m.modelPrepareShadowSources(f); e != nil {
		return e
	}
	m.modelBatchSources(f)
	split(0)
	if e = m.modelResizeAtlas(aw, ah); e != nil {
		return e
	}
	if e = m.modelPrepareMergedAtlas(); e != nil {
		return e
	}
	split(1)
	changed := false
	// A common entry statically reads both record families. Even with no active
	// shadow, its one-record fixed declaration needs a full zeroed subject.
	for _, u := range []struct {
		b     *modelBuffer
		label string
		n     int
		usage gputypes.BufferUsage
	}{{&m.endpoints, "battle-pose-endpoints", max(modelPoseStride, len(f.Current)*modelPoseStride*2), wgpu.BufferUsageStorage}, {&m.poses, "battle-rigid-poses", max(modelPoseStride, len(f.Current)*modelPoseStride), wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc}, {&m.instances, "battle-model-instances", len(m.instanceBytes), wgpu.BufferUsageStorage}, {&m.selected, "battle-selected-materials", len(m.selectedBytes), wgpu.BufferUsageStorage}, {&m.lights, "battle-retained-lights", max(64, len(f.Lighting.Lights)*64), wgpu.BufferUsageStorage}, {&m.slots, "battle-model-slots", len(m.slotBytes), wgpu.BufferUsageStorage}, {&m.shadowRecords, "battle-shadow-records", max(modelShadowStride, len(m.shadowBytes)), wgpu.BufferUsageStorage}, {&m.groups, "battle-model-groups", len(m.groupWork.groups), wgpu.BufferUsageStorage}, {&m.members, "battle-model-group-members", len(m.groupWork.members), wgpu.BufferUsageStorage}} {
		grow, err := m.modelGrow(u.b, u.label, u.n, u.usage)
		if err != nil {
			return err
		}
		changed = changed || grow
	}
	if changed {
		m.poseGroup, m.bodyGroup = nil, nil
		m.modelReleaseCommitGroup()
		m.atlas.modelReleaseGroup()
	}
	split(2)
	if e = m.modelPrepareBodyPipelines(); e != nil {
		return e
	}
	if e = m.modelPrepareCommitPipeline(); e != nil {
		return e
	}
	if e = m.modelPrepareMergePipeline(); e != nil {
		return e
	}
	split(3)
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
			if e = m.writeMutableBuffer(m.endpoints.buffer, m.poseBytes); e != nil {
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
	modelVec(ub[:], 16, [4]float32{float32(width), float32(height), float32(m.atlas.width), float32(m.atlas.height)})
	modelVec(ub[:], 32, [4]float32{float32(len(f.Current)), float32(len(f.Lighting.Lights)), float32(f.LightControls), float32(len(f.ModelRules))})
	modelVec(ub[:], 48, f.ModelProjections.View)
	modelVec(ub[:], 64, f.Shadows.Tint)
	for _, u := range []struct {
		b    *wgpu.Buffer
		data []byte
	}{{m.instances.buffer, m.instanceBytes}, {m.selected.buffer, m.selectedBytes}, {m.lights.buffer, m.lightBytes}, {m.slots.buffer, m.slotBytes}, {m.shadowRecords.buffer, m.shadowBytes}, {m.groups.buffer, m.groupWork.groups}, {m.members.buffer, m.groupWork.members}, {m.uniform, ub[:]}} {
		if e = m.writeMutableBuffer(u.b, u.data); e != nil {
			return e
		}
		m.counters.UploadBytes += len(u.data)
	}
	split(4)
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
	if m.groupWork.count > 0 && m.atlas.mergeGroup == nil {
		m.atlas.mergeGroup, e = m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: m.mergeLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: m.groups.buffer}, {Binding: 1, Buffer: m.members.buffer}, {Binding: 2, Buffer: m.slots.buffer}, {Binding: 3, Buffer: m.uniform, Size: modelUniformSize}, {Binding: 4, TextureView: m.atlas.colorView}, {Binding: 5, TextureView: m.atlas.metadataView}}})
		if e != nil {
			return e
		}
	}
	if e = m.modelPrepareShadowGPU(f, changed); e != nil {
		return e
	}
	if e = m.modelPrepareCommitGroup(); e != nil {
		return e
	}
	split(5)
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
	// Body depth is local to this pass and cleared before every use. Only
	// color/metadata feed later passes; the separate shadow height keys store.
	pass, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: m.atlas.colorView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}, {View: m.atlas.metadataView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: m.atlas.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpDiscard, DepthClearValue: 1, StencilReadOnly: true}})
	if e != nil {
		return e
	}
	pass.SetBindGroup(0, m.bodyGroup, nil)
	pass.SetIndexBuffer(m.indices, gputypes.IndexFormatUint32, 0)
	if m.batchWork.independent {
		for first := 0; first < len(m.draws); {
			end := modelDrawRun(m.draws, first, false, true)
			mesh := m.meshes[m.draws[first].mesh]
			if mesh.count != 0 {
				// Outlined records precede ordinary records within this mesh.
				// Distinct half-open atlas rectangles make cross-source order
				// immaterial, while each source still gets outline then body.
				outlines := first
				for outlines < end && m.draws[outlines].outline {
					outlines++
				}
				if mesh.edgeCount > 0 && outlines > first {
					pass.SetPipeline(m.outlinePipeline)
					pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.edgeCount, InstanceCount: uint32(outlines - first), FirstIndex: mesh.edgeFirst, BaseVertex: mesh.base, FirstInstance: m.draws[first].instance})
					m.counters.Draws++
				}
				pass.SetPipeline(m.bodyPipeline)
				pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.count, InstanceCount: uint32(end - first), FirstIndex: mesh.first, BaseVertex: mesh.base, FirstInstance: m.draws[first].instance})
				m.counters.Draws++
			}
			first = end
		}
	} else {
		// Diagnostic unbatched mode and accepted aliased standalone sources
		// retain their original outline/body chronology.
		var selected *wgpu.RenderPipeline
		for _, d := range m.draws {
			mesh := m.meshes[d.mesh]
			if mesh.count == 0 {
				continue
			}
			if mesh.edgeCount > 0 && d.outline {
				if m.state == nil || m.state.disabled || selected != m.outlinePipeline {
					pass.SetPipeline(m.outlinePipeline)
					selected = m.outlinePipeline
				}
				pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.edgeCount, InstanceCount: 1, FirstIndex: mesh.edgeFirst, BaseVertex: mesh.base, FirstInstance: d.instance})
				m.counters.Draws++
			}
			if m.state == nil || m.state.disabled || selected != m.bodyPipeline {
				pass.SetPipeline(m.bodyPipeline)
				selected = m.bodyPipeline
			}
			pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.count, InstanceCount: 1, FirstIndex: mesh.first, BaseVertex: mesh.base, FirstInstance: d.instance})
			m.counters.Draws++
		}
	}
	e = pass.End()
	if e != nil {
		return e
	}
	if m.groupWork.count > 0 {
		pass, e = enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: m.atlas.mergedView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
		if e != nil {
			return e
		}
		pass.SetBindGroup(0, m.atlas.mergeGroup, nil)
		pass.SetPipeline(m.copyPipeline)
		for first := 0; first < len(m.copySlots); {
			end := modelSlotRun(m.copySlots, first, !m.disableModelBatching)
			pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(end - first), FirstInstance: m.copySlots[first] - 1})
			m.counters.Draws++
			first = end
		}
		pass.SetPipeline(m.mergePipeline)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(m.groupWork.count)})
		m.counters.Draws++
		if e = pass.End(); e != nil {
			return e
		}
	}
	return m.modelComputeShadows(enc)
}

func (m *Models) Draw(pass *wgpu.RenderPassEncoder, phase uint32) {
	if m == nil || m.closed || !m.prepared || pass == nil {
		return
	}
	m.state.selectResources(pass, m.resolvePipeline, m.commitGroup)
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
	m.state.selectResources(pass, m.resolvePipeline, m.commitGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(m.shadowCount), FirstInstance: modelCommitShadowIndex(m.commitWork.capacities[0], 0)})
	m.counters.Draws++
}

// DrawSlot and DrawShadowSlot let the host interleave production model paint
// entries with feature sprites and projectile effect barriers. Slots are the
// original composition's one-based final slots, never private atlas selectors.
func (m *Models) DrawSlot(pass *wgpu.RenderPassEncoder, slot uint32) {
	if m == nil || m.closed || !m.prepared || pass == nil || slot == 0 || int(slot) > len(m.slotBytes)/modelSlotStride {
		return
	}
	m.state.selectResources(pass, m.resolvePipeline, m.commitGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: slot - 1})
	m.counters.Draws++
}
func (m *Models) DrawShadowSlot(pass *wgpu.RenderPassEncoder, slot uint32) {
	if m == nil || m.closed || !m.prepared || pass == nil {
		return
	}
	for _, p := range m.shadowPaint {
		if p.slot != slot {
			continue
		}
		m.state.selectResources(pass, m.resolvePipeline, m.commitGroup)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: modelCommitShadowIndex(m.commitWork.capacities[0], p.subject)})
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
// Shadow is a legacy 1x1 compatibility dummy; DebugShadowResources exposes the
// actual retained shadow planes and their independent dimensions.
func (m *Models) DebugResources() (color, shadow *wgpu.Texture, poses *wgpu.Buffer, width, height, pieceCount int) {
	if m == nil || m.closed || !m.prepared {
		return
	}
	color = m.atlas.color
	if m.groupWork.count > 0 {
		color = m.atlas.merged
	}
	return color, m.atlas.shadow, m.poses.buffer, m.atlas.width, m.atlas.height, m.pieceCount
}

// DebugPacking returns detached copies of the 416-byte instance and 48-byte
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
	m.modelReleaseCommitGroup()
	m.bodyWork.modelClose()
	m.bodyPipeline, m.outlinePipeline = nil, nil
	m.shadowWork.bodyPipeline, m.shadowWork.outlinePipeline = nil, nil
	m.shadowWork.modelClose()
	m.atlas.modelClose()
	m.commitWork.modelClose()
	m.resolvePipeline = nil
	for i := len(m.owned) - 1; i >= 0; i-- {
		m.owned[i]()
	}
	m.owned = nil
}
