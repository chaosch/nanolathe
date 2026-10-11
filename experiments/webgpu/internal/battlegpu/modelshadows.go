package battlegpu

import (
	_ "embed"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed modelshadows.wgsl
var modelShadowsShader string

const modelShadowVertexStride = 32

type modelShadowAtlas struct {
	body, depth, mask              *wgpu.Texture
	bodyView, depthView, maskView  *wgpu.TextureView
	group, bodyGroup, projectGroup *wgpu.BindGroup
	width, height                  int
}

func (a *modelShadowAtlas) modelReleaseGroups() {
	for _, g := range []*wgpu.BindGroup{a.group, a.bodyGroup, a.projectGroup} {
		if g != nil {
			g.Release()
		}
	}
	a.group, a.bodyGroup, a.projectGroup = nil, nil, nil
}
func (a *modelShadowAtlas) modelClose() {
	a.modelReleaseGroups()
	for _, v := range []*wgpu.TextureView{a.maskView, a.depthView, a.bodyView} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{a.mask, a.depth, a.body} {
		if t != nil {
			t.Release()
		}
	}
	*a = modelShadowAtlas{}
}

type modelShadowWork struct {
	atlas                           modelShadowAtlas
	uniform                         *wgpu.Buffer
	waterView                       *wgpu.TextureView
	hasWater                        bool
	vertices, indices               *wgpu.Buffer
	vertexCount                     int
	meshes                          []modelMesh
	projected                       []modelDraw
	layout                          *wgpu.BindGroupLayout
	pipelineLayout                  *wgpu.PipelineLayout
	projectLayout                   *wgpu.BindGroupLayout
	projectPipelineLayout           *wgpu.PipelineLayout
	bodyPipeline, outlinePipeline   *wgpu.RenderPipeline
	projectPipeline, commitPipeline *wgpu.RenderPipeline
	module                          *wgpu.ShaderModule
	capacities                      [4]int
}

func (p *modelShadowWork) modelClose() {
	p.atlas.modelClose()
	for _, pipeline := range []*wgpu.RenderPipeline{p.commitPipeline, p.projectPipeline} {
		if pipeline != nil {
			pipeline.Release()
		}
	}
	if p.module != nil {
		p.module.Release()
	}
	p.module, p.projectPipeline, p.commitPipeline = nil, nil, nil
}

// The source topology is the producer's immutable authored polygon mesh, not
// the body's material-filtered mesh. Only unposed bytes are copied here.
func modelShadowTopology(topology []meshscene.Mesh, meshes []modelMesh) (vertices, indices []byte, spans []modelMesh, err error) {
	if len(topology) != len(meshes) {
		return nil, nil, nil, modelError("shadow topology aligned to scene meshes")
	}
	spans = make([]modelMesh, len(topology))
	for mi, mesh := range topology {
		if mesh.Pieces != meshes[mi].pieces || len(mesh.Indices)%3 != 0 {
			return nil, nil, nil, modelError("authored shadow triangle topology and piece spans")
		}
		span := &spans[mi]
		span.base, span.first, span.count, span.pieces = int32(len(vertices)/modelShadowVertexStride), uint32(len(indices)/4), uint32(len(mesh.Indices)), mesh.Pieces
		for _, v := range mesh.Vertices {
			if int(v.Piece) >= mesh.Pieces || !modelFinite(v.Position[0]) || !modelFinite(v.Position[1]) || !modelFinite(v.Position[2]) {
				return nil, nil, nil, modelError("finite authored shadow vertices and valid pieces")
			}
			at := len(vertices)
			vertices = append(vertices, make([]byte, modelShadowVertexStride)...)
			modelVec(vertices, at, [4]float32{v.Position[0], v.Position[1], v.Position[2], 1})
			binary.LittleEndian.PutUint32(vertices[at+16:], v.Piece)
		}
		for _, index := range mesh.Indices {
			if int(index) >= len(mesh.Vertices) {
				return nil, nil, nil, modelError("shadow indices within authored vertices")
			}
			indices = binary.LittleEndian.AppendUint32(indices, index)
		}
	}
	if len(vertices) > modelCoreBufferLimit || len(indices) > modelCoreBufferLimit {
		return nil, nil, nil, modelError("shadow topology within core buffer limits")
	}
	return
}

func modelShadowRect(rect [4]float32, width, height int) bool {
	for _, value := range rect {
		if !modelFinite(value) {
			return false
		}
	}
	return rect[0] >= 0 && rect[1] >= 0 && rect[2] > 0 && rect[3] > 0 && rect[0]+rect[2] <= float32(width) && rect[1]+rect[3] <= float32(height)
}

func modelShadowRules(f meshscene.LiveFrame, draw meshscene.ShadowDraw, ruleSlot uint32) (meshscene.ModelRules, error) {
	if ruleSlot != 0 {
		if int(ruleSlot) > len(f.Shadows.BodyRules) {
			return meshscene.ModelRules{}, modelError("shadow body rule selectors within exported overrides")
		}
		return f.Shadows.BodyRules[ruleSlot-1], nil
	}
	if int(draw.Instance) >= len(f.ModelRules) {
		return meshscene.ModelRules{}, modelError("shadow body draws with own rules or exported overrides")
	}
	return f.ModelRules[draw.Instance], nil
}

// Sources use independent slot identity and their own rules, without carrier
// group deltas or BLUE policy. Shadow mode matches the MIT Metal mode 3 lane.
func modelShadowClone(bytes []byte, f meshscene.LiveFrame, draw meshscene.ShadowDraw, rule meshscene.ModelRules, body bool) ([]byte, modelDraw, error) {
	if int(draw.Instance) >= len(f.Instances) || draw.Slot == 0 || int(draw.Slot) > len(f.Shadows.Slots) || int(draw.Subject) >= len(f.Shadows.Subjects) {
		return bytes, modelDraw{}, modelError("valid shadow source instance, slot and subject")
	}
	i := int(draw.Instance) * modelInstanceStride
	if i+modelInstanceStride > len(bytes) {
		return bytes, modelDraw{}, modelError("packed original shadow source")
	}
	at := len(bytes)
	bytes = append(bytes, bytes[i:i+modelInstanceStride]...)
	clear(bytes[at+320 : at+modelInstanceStride])
	for j, value := range [][4]float32{rule.Reveal, rule.Below, rule.Band, rule.Above, rule.Clip, rule.Shadow, rule.Outline} {
		modelVec(bytes, at+80+j*16, value)
	}
	slot := f.Shadows.Slots[draw.Slot-1]
	modelVec(bytes, at+240, slot.Screen)
	modelVec(bytes, at+256, slot.Atlas)
	binary.LittleEndian.PutUint32(bytes[at+304:], 0) // body lighting is disabled in the shadow lane
	binary.LittleEndian.PutUint32(bytes[at+308:], 1)
	binary.LittleEndian.PutUint32(bytes[at+312:], 1)
	if body {
		binary.LittleEndian.PutUint32(bytes[at+316:], 1)
	}
	visual := f.Instances[draw.Instance].Visual
	// The MIT shadow recorder admits outlines from the original visual before
	// the shader applies the selected rule's own discard gate.
	outline := body && visual.State[2] > 0 && visual.Outline[3] > 0 && modelOutlineNeeded(visual, rule, true)
	return bytes, modelDraw{mesh: int(draw.Mesh), instance: uint32(at / modelInstanceStride), outline: outline, shadow: int(draw.Subject)}, nil
}

func (m *Models) modelPrepareShadowSources(f meshscene.LiveFrame) error {
	c, p := f.Shadows, &m.shadowWork
	p.projected = p.projected[:0]
	m.shadowBytes = modelBytes(m.shadowBytes, max(modelShadowStride, len(c.Subjects)*modelShadowStride))
	if len(c.Subjects) == 0 {
		if len(c.BodyDraws) != 0 || len(c.ProjectedDraws) != 0 {
			return modelError("shadow draws backed by exported subjects")
		}
		return nil
	}
	if c.AtlasWidth <= 0 || c.AtlasHeight <= 0 || c.AtlasWidth > 8192 || c.AtlasHeight > 8192 || len(c.PieceFlags) != len(f.Current) || len(c.BodyRuleSlots) != len(c.BodyDraws) || (c.Tint[3] != 1 && c.Tint[3] != 2) || c.Water[3] < 0 || c.Water[3] > 0 && !p.hasWater {
		return modelError("bounded shadow atlas, aligned flags/rule slots, sample block and available water mask")
	}
	for _, vec := range [][4]float32{c.Tint, c.Water} {
		for _, value := range vec {
			if !modelFinite(value) {
				return modelError("finite shadow tint and water operands")
			}
		}
	}
	for _, slot := range c.Slots {
		if !modelShadowRect(slot.Atlas, c.AtlasWidth, c.AtlasHeight) || !modelFinite(slot.Screen[0]) || !modelFinite(slot.Screen[1]) || !modelFinite(slot.Screen[2]) || !modelFinite(slot.Screen[3]) || slot.Screen[2] <= 0 || slot.Screen[3] <= 0 {
			return modelError("finite independent shadow slots within the exported atlas")
		}
	}
	for si, subject := range c.Subjects {
		if !modelShadowRect(subject.Body, c.AtlasWidth, c.AtlasHeight) || !modelShadowRect(subject.Screen, m.width, m.height) || (subject.Params[0] != 1 && subject.Params[0] != 2 && subject.Params[0] != 3) || subject.Params[2] < 0 || subject.Params[3] <= 0 || subject.Order[0] == 0 || int(subject.Order[0]) > len(m.slotBytes)/modelSlotStride || int(subject.Order[3]) >= len(f.Instances) || subject.Params[0] < 1.5 && !modelShadowRect(subject.Mask, c.AtlasWidth, c.AtlasHeight) {
			return modelError("valid shadow body/mask/commit rectangles, kind and painter attachment")
		}
		for _, vec := range [][4]float32{subject.Params, subject.Placement} {
			for _, value := range vec {
				if !modelFinite(value) {
					return modelError("finite shadow subject parameters and placement")
				}
			}
		}
		at := si * modelShadowStride
		for j, value := range [][4]float32{subject.Screen, subject.Body, subject.Mask, subject.Params, subject.Placement} {
			modelVec(m.shadowBytes, at+j*16, value)
		}
		for j, value := range subject.Order {
			binary.LittleEndian.PutUint32(m.shadowBytes[at+80+j*4:], value)
		}
		m.shadowPaint = append(m.shadowPaint, modelShadowPaint{subject.Order[0], uint32(si)})
	}
	validateDraw := func(draw meshscene.ShadowDraw, body bool) error {
		if int(draw.Mesh) >= len(m.meshes) || int(draw.Instance) >= len(f.Instances) || draw.Slot == 0 || int(draw.Slot) > len(c.Slots) || int(draw.Subject) >= len(c.Subjects) || int(draw.Mesh) != f.Instances[draw.Instance].Mesh {
			return modelError("shadow draws aligned to original scene meshes, slots and subjects")
		}
		rect := c.Subjects[draw.Subject].Mask
		if body {
			rect = c.Subjects[draw.Subject].Body
		}
		if c.Slots[draw.Slot-1].Atlas != rect {
			return modelError("shadow draw slots matching the subject source rectangle")
		}
		return nil
	}
	for di, draw := range c.BodyDraws {
		if err := validateDraw(draw, true); err != nil {
			return err
		}
		rule, err := modelShadowRules(f, draw, c.BodyRuleSlots[di])
		if err != nil {
			return err
		}
		var packed modelDraw
		m.instanceBytes, packed, err = modelShadowClone(m.instanceBytes, f, draw, rule, true)
		if err != nil {
			return err
		}
		m.shadowDraws = append(m.shadowDraws, packed)
	}
	for _, draw := range c.ProjectedDraws {
		if err := validateDraw(draw, false); err != nil {
			return err
		}
		rule, err := modelShadowRules(f, draw, 0)
		if err != nil {
			return err
		}
		var packed modelDraw
		m.instanceBytes, packed, err = modelShadowClone(m.instanceBytes, f, draw, rule, false)
		if err != nil {
			return err
		}
		p.projected = append(p.projected, packed)
	}
	// TODO(question): cached/live source chronology is absent from LiveFrame.
	// This implements exported retained body sources, not exact retail images.
	// TODO(question): an ordered shadow-only paint entry is needed for subjects
	// the producer omits when their body has no viewport commit slot.
	m.shadowCount = len(c.Subjects)
	return nil
}

func (m *Models) modelInitShadows(scene *meshscene.Scene, module *wgpu.ShaderModule, bodyLayout *wgpu.PipelineLayout) (err error) {
	p := &m.shadowWork
	if p.bodyPipeline, err = m.modelPipeline(module, bodyLayout, "model_vertex", "model_shadow_fragment", wgpu.TextureFormatRGBA8Unorm, 1, false, gputypes.PrimitiveTopologyTriangleList); err != nil {
		return
	}
	if p.outlinePipeline, err = m.modelPipeline(module, bodyLayout, "model_vertex", "model_shadow_outline", wgpu.TextureFormatRGBA8Unorm, 1, false, gputypes.PrimitiveTopologyLineList); err != nil {
		return
	}
	if p.uniform, err = m.modelBuffer("battle-shadow-uniforms", modelUniformSize, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst, nil); err != nil {
		return
	}
	water := scene.WaterMask
	p.hasWater = !scene.WaterLava && water.Width > 0 && water.Height > 0 && len(water.RGBA) == water.Width*water.Height*4
	if !p.hasWater {
		water = meshscene.Texture{Width: 1, Height: 1, RGBA: []byte{0, 0, 0, 0}}
	}
	if p.waterView, err = m.modelArt("battle-shadow-liquid-mask", water); err != nil {
		return
	}
	vf := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	// Fixed-array capacities are chosen after growth in Prepare. Zero minimum
	// binding sizes defer their size validation to the actual bound buffers.
	p.projectLayout, p.projectPipelineLayout, err = m.modelLayout([]gputypes.BindGroupLayoutEntry{
		modelBufferEntry(0, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(1, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(2, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(3, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeUniform, 96),
	})
	if err != nil {
		return
	}
	p.layout, p.pipelineLayout, err = m.modelLayout([]gputypes.BindGroupLayoutEntry{
		modelBufferEntry(3, vf, gputypes.BufferBindingTypeUniform, 96),
		modelBufferEntry(4, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelTextureEntry(5),
		{Binding: 6, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeDepth, ViewDimension: gputypes.TextureViewDimension2D}},
		modelTextureEntry(7), modelTextureEntry(8),
	})
	return
}

func modelShadowShaderSource(capacities [4]int) string {
	return strings.NewReplacer("__VERTICES__", strconv.Itoa(capacities[0]), "__POSES__", strconv.Itoa(capacities[1]), "__INSTANCES__", strconv.Itoa(capacities[2]), "__SUBJECTS__", strconv.Itoa(capacities[3])).Replace(modelShadowsShader)
}

func (m *Models) modelResizeShadows(width, height int) (err error) {
	p := &m.shadowWork
	if width <= p.atlas.width && height <= p.atlas.height && p.atlas.body != nil {
		return nil
	}
	next := modelShadowAtlas{width: modelAtlasExtent(width, p.atlas.width), height: modelAtlasExtent(height, p.atlas.height)}
	defer func() {
		if err != nil {
			next.modelClose()
		}
	}()
	usage := wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc
	if next.body, next.bodyView, err = m.modelCreateTexture("battle-shadow-solo-bodies", next.width, next.height, wgpu.TextureFormatRGBA8Unorm, usage); err != nil {
		return
	}
	if next.depth, next.depthView, err = m.modelCreateTexture("battle-shadow-solo-keys", next.width, next.height, wgpu.TextureFormatDepth32Float, usage); err != nil {
		return
	}
	if next.mask, next.maskView, err = m.modelCreateTexture("battle-shadow-projected-masks", next.width, next.height, gputypes.TextureFormatR8Unorm, usage); err != nil {
		return
	}
	// The host waits for prior submissions before Prepare; only current atlases
	// own these views/groups, so geometric growth releases every obsolete tier.
	p.atlas.modelClose()
	p.atlas = next
	return
}

func (m *Models) modelPrepareShadowGPU(f meshscene.LiveFrame, changed bool) (err error) {
	p := &m.shadowWork
	// Core buffers can grow while shadows are absent. Invalidate borrowed
	// groups on that frame too, before a later admission can reuse them.
	if changed {
		p.atlas.modelReleaseGroups()
	}
	if m.shadowCount == 0 {
		return nil
	}
	if len(p.meshes) == 0 {
		vertices, indices, spans, e := modelShadowTopology(f.Shadows.Topology, m.meshes)
		if e != nil {
			return e
		}
		if p.vertices, err = m.modelBuffer("battle-shadow-authored-vertices", uint64(max(modelShadowVertexStride, len(vertices))), wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst, vertices); err != nil {
			return
		}
		if p.indices, err = m.modelBuffer("battle-shadow-authored-indices", uint64(max(4, len(indices))), wgpu.BufferUsageIndex|wgpu.BufferUsageCopyDst, indices); err != nil {
			return
		}
		p.vertexCount, p.meshes = max(1, len(vertices)/modelShadowVertexStride), spans
		m.counters.UploadBytes += len(vertices) + len(indices)
	}
	if len(f.Shadows.Topology) != len(p.meshes) {
		return modelError("immutable scene-aligned shadow topology")
	}
	if err = m.modelResizeShadows(f.Shadows.AtlasWidth, f.Shadows.AtlasHeight); err != nil {
		return
	}
	capacities := [4]int{p.vertexCount, int(m.poses.size / modelPoseStride), int(m.instances.size / modelInstanceStride), int(m.shadowRecords.size / modelShadowStride)}
	if capacities != p.capacities {
		module, e := m.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: modelShadowShaderSource(capacities)})
		if e != nil {
			return e
		}
		create := func(layout *wgpu.PipelineLayout, vertex, fragment string, format gputypes.TextureFormat, depth, blend bool) (*wgpu.RenderPipeline, error) {
			target := gputypes.ColorTargetState{Format: format, WriteMask: gputypes.ColorWriteMaskAll}
			if blend {
				target.Blend = &gputypes.BlendState{Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}, Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}}
			}
			desc := &wgpu.RenderPipelineDescriptor{Layout: layout, Vertex: wgpu.VertexState{Module: module, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCW, CullMode: gputypes.CullModeBack}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}, Fragment: &wgpu.FragmentState{Module: module, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{target}}}
			if depth {
				desc.Primitive.CullMode = gputypes.CullModeNone
				desc.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthCompare: gputypes.CompareFunctionAlways}
			}
			return m.d.CreateRenderPipeline(desc)
		}
		project, e := create(p.projectPipelineLayout, "model_shadow_project_vertex", "model_shadow_project_fragment", gputypes.TextureFormatR8Unorm, false, false)
		if e != nil {
			module.Release()
			return e
		}
		commit, e := create(p.pipelineLayout, "model_shadow_commit_vertex", "model_shadow_commit_fragment", m.format, true, true)
		if e != nil {
			project.Release()
			module.Release()
			return e
		}
		for _, old := range []*wgpu.RenderPipeline{p.commitPipeline, p.projectPipeline} {
			if old != nil {
				old.Release()
			}
		}
		if p.module != nil {
			p.module.Release()
		}
		p.module, p.projectPipeline, p.commitPipeline, p.capacities = module, project, commit, capacities
	}
	var uniforms [modelUniformSize]byte
	modelVec(uniforms[:], 0, [4]float32{f.Camera[0], f.Camera[1], f.Camera[2], f.Alpha})
	modelVec(uniforms[:], 16, [4]float32{float32(m.width), float32(m.height), float32(p.atlas.width), float32(p.atlas.height)})
	modelVec(uniforms[:], 64, f.Shadows.Tint)
	modelVec(uniforms[:], 80, f.Shadows.Water)
	if err = m.q.WriteBuffer(p.uniform, 0, uniforms[:]); err != nil {
		return
	}
	m.counters.UploadBytes += len(uniforms)
	if p.atlas.bodyGroup == nil {
		p.atlas.bodyGroup, err = m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: m.bodyLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: m.vertices}, {Binding: 1, Buffer: m.poses.buffer}, {Binding: 2, Buffer: m.instances.buffer}, {Binding: 3, Buffer: p.uniform, Size: modelUniformSize}, {Binding: 4, TextureView: m.textureView}, {Binding: 5, TextureView: m.paletteView}, {Binding: 6, Buffer: m.materialData}, {Binding: 7, Buffer: m.frameData}, {Binding: 8, Buffer: m.selected.buffer}, {Binding: 9, Buffer: m.lights.buffer}, {Binding: 10, Sampler: m.sampler}, {Binding: 11, Buffer: m.centers}}})
		if err != nil {
			return
		}
	}
	if p.atlas.projectGroup == nil {
		p.atlas.projectGroup, err = m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: p.projectLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: p.vertices}, {Binding: 1, Buffer: m.poses.buffer}, {Binding: 2, Buffer: m.instances.buffer}, {Binding: 3, Buffer: p.uniform, Size: 96}}})
		if err != nil {
			return
		}
	}
	if p.atlas.group == nil {
		p.atlas.group, err = m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: p.layout, Entries: []wgpu.BindGroupEntry{{Binding: 3, Buffer: p.uniform, Size: 96}, {Binding: 4, Buffer: m.shadowRecords.buffer}, {Binding: 5, TextureView: p.atlas.bodyView}, {Binding: 6, TextureView: p.atlas.depthView}, {Binding: 7, TextureView: p.atlas.maskView}, {Binding: 8, TextureView: p.waterView}}})
	}
	return
}

func (m *Models) modelComputeShadows(enc *wgpu.CommandEncoder) error {
	if m.shadowCount == 0 {
		return nil
	}
	p := &m.shadowWork
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: p.atlas.bodyView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: p.atlas.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	if err != nil {
		return err
	}
	pass.SetBindGroup(0, p.atlas.bodyGroup, nil)
	pass.SetIndexBuffer(m.indices, gputypes.IndexFormatUint32, 0)
	pass.SetPipeline(p.bodyPipeline)
	for _, draw := range m.shadowDraws {
		mesh := m.meshes[draw.mesh]
		if mesh.count != 0 {
			pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.count, InstanceCount: 1, FirstIndex: mesh.first, BaseVertex: mesh.base, FirstInstance: draw.instance})
			m.counters.Draws++
		}
	}
	selected := false
	for _, draw := range m.shadowDraws {
		mesh := m.meshes[draw.mesh]
		if !draw.outline || mesh.edgeCount == 0 {
			continue
		}
		if !selected || m.state == nil || m.state.disabled {
			pass.SetPipeline(p.outlinePipeline)
			selected = true
		}
		pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.edgeCount, InstanceCount: 1, FirstIndex: mesh.edgeFirst, BaseVertex: mesh.base, FirstInstance: draw.instance})
		m.counters.Draws++
	}
	if err = pass.End(); err != nil {
		return err
	}
	pass, err = enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: p.atlas.maskView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if err != nil {
		return err
	}
	pass.SetPipeline(p.projectPipeline)
	pass.SetBindGroup(0, p.atlas.projectGroup, nil)
	pass.SetIndexBuffer(p.indices, gputypes.IndexFormatUint32, 0)
	for _, draw := range p.projected {
		mesh := p.meshes[draw.mesh]
		if mesh.count != 0 {
			pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: mesh.count, InstanceCount: 1, FirstIndex: mesh.first, BaseVertex: mesh.base, FirstInstance: draw.instance})
			m.counters.Draws++
		}
	}
	return pass.End()
}

// DebugShadowResources borrows the actual solo-body, height-key and projected
// mask sources. Depth is depth32float, mask is r8unorm; use their own dimensions.
// Complete all copies/maps before the next Prepare or Close.
func (m *Models) DebugShadowResources() (body, depth, mask *wgpu.Texture, width, height int) {
	if m == nil || m.closed || !m.prepared || m.shadowCount == 0 {
		return
	}
	a := &m.shadowWork.atlas
	return a.body, a.depth, a.mask, a.width, a.height
}
