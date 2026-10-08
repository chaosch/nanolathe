//go:build darwin

package metalproto

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// Outline rows (outline_rows.inc).
type gbOutlineRowsMesh struct {
	rings                  *mtl.Buffer
	ringCount, cornerCount int
}
type gbOutlineRowsState struct {
	prepare, resolve                                     mtl.ID
	completedFrames, unsupportedRings, unsupportedFrames uint64
}
type gbOutlineRowsControl struct {
	span   [4]uint32
	domain [4]int32
}
type gbOutlineRowsSubject struct {
	mesh, reserved uint32
	control        gbOutlineRowsControl
}
type gbOutlineRowsRing struct {
	corners, prepared, diagnostics *mtl.Buffer
	subjects                       []gbOutlineRowsSubject
	instanceCount                  int
}

func (r *gbRenderer) outlineRowsInitialize() error {
	prepare, err := r.computePipeline("nm_or_prepare")
	if err != nil {
		return err
	}
	resolve, err := r.computePipeline("nm_or_resolve")
	if err != nil {
		return err
	}
	r.outlineRowsState = &gbOutlineRowsState{prepare: prepare, resolve: resolve}
	return nil
}

func (r *gbRenderer) meshOutlineRows(index uint32, u *NativeOutlineRowsUpload) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if u == nil || int(index) >= len(r.meshes) {
		return 0
	}
	if u.RingCount > math.MaxInt32 || (u.RingCount > 0 && u.Rings == nil) {
		r.lastError = "Invalid outline ring upload"
		return 0
	}
	mesh := r.meshes[index]
	count, bytes := mesh.vertices.Size/64, int(u.RingCount)*16
	if bytes > r.device.MaxBufferLength() {
		r.lastError = "Outline ring device buffer limit"
		return 0
	}
	corners := uint64(0)
	for _, ring := range unsafe.Slice((*OutlineRowsRing)(u.Rings), u.RingCount) {
		if ring.Count < 2 || int(ring.Start) > count || int(ring.Count) > count-int(ring.Start) || uint64(ring.CornerOffset) != corners {
			r.lastError = "Outline ring exceeds immutable mesh vertices"
			return 0
		}
		corners += uint64(ring.Count)
	}
	m := &gbOutlineRowsMesh{rings: r.device.NewBuffer(max(bytes, 16), mtl.ResourceShared)}
	if m.rings == nil {
		r.lastError = "Outline ring allocation failed"
		return 0
	}
	copyRaw(m.rings.Ptr, u.Rings, bytes)
	m.ringCount, m.cornerCount = int(u.RingCount), int(corners)
	mesh.outlineRowsMesh = m
	return 1
}

func (r *gbRenderer) outlineRowsPrepare(ring *gbRing) bool {
	f := ring.outlineRowsRing
	if f == nil {
		f = &gbOutlineRowsRing{}
		ring.outlineRowsRing = f
	}
	f.subjects = f.subjects[:0]
	n := 0
	if ring.faceRing != nil {
		n = ring.faceRing.instanceCount
	}
	if ring.projections.Size < n*48 || ring.visuals.Size < n*64 || ring.rules.Size < n*112 {
		return r.fail("Outline subject metadata span mismatch")
	}
	projections := unsafe.Slice((*float32)(ring.projections.Ptr), ring.projections.Size/4)
	visuals := unsafe.Slice((*float32)(ring.visuals.Ptr), ring.visuals.Size/4)
	slots := unsafe.Slice((*float32)(ring.compositionSlots.Ptr), ring.compositionSlots.Size/4)
	selectors := unsafe.Slice((*uint32)(ring.compositionSelectors.Ptr), ring.compositionSelectors.Size/4)
	var corners, rings uint64
	view := ring.projectionView
draws:
	for _, draw := range ring.compositionDraws[:ring.compositionDrawCount] {
		for _, prior := range f.subjects {
			if prior.control.span[0] == draw.Instance {
				if prior.mesh != draw.Mesh {
					return r.fail("Outline subject uses conflicting meshes")
				}
				continue draws
			}
		}
		if int(draw.Instance) >= n || int(draw.Mesh) >= len(r.meshes) {
			return r.fail("Outline draw exceeds retained subject span")
		}
		if visuals[draw.Instance*16+2] <= 0 || visuals[draw.Instance*16+11] <= 0 {
			continue
		}
		m := r.meshes[draw.Mesh].outlineRowsMesh
		if m == nil {
			return r.fail("Active construction model lacks immutable outline rings")
		}
		if m.ringCount == 0 {
			continue
		}
		p := projections[draw.Instance*12:]
		if uint32(p[3])&1 == 0 || math.IsInf(float64(p[8]), 0) || math.IsNaN(float64(p[8])) || math.IsInf(float64(p[10]), 0) || math.IsNaN(float64(p[10])) || p[8] <= 0 || p[10] <= 0 {
			return r.fail("Active construction model lacks known native projection")
		}
		selector := selectors[draw.Instance]
		if selector == 0 || int(selector) > ring.compositionSlotCount {
			return r.fail("Outline source slot outside composition atlas")
		}
		slot := slots[(selector-1)*12:]
		x0 := math.Floor(float64((slot[0] - view[2]) / p[10]))
		y0 := math.Floor(float64((slot[1] - view[3]) / p[10]))
		x1 := math.Ceil(float64((slot[0] + slot[2] - view[2]) / p[10]))
		y1 := math.Ceil(float64((slot[1] + slot[3] - view[3]) / p[10]))
		finite := func(v float64) bool { return !math.IsInf(v, 0) && !math.IsNaN(v) }
		if !finite(x0) || !finite(y0) || !finite(x1) || !finite(y1) || x0 < math.MinInt32 || y0 < math.MinInt32 || x1 > math.MaxInt32 || y1 > math.MaxInt32 || x1-x0 > math.MaxUint32 || y1-y0 > math.MaxUint32 {
			return r.fail("Outline native block domain exceeds device range")
		}
		f.subjects = append(f.subjects, gbOutlineRowsSubject{mesh: draw.Mesh, control: gbOutlineRowsControl{[4]uint32{draw.Instance, uint32(m.ringCount), uint32(corners), uint32(rings)}, [4]int32{int32(x0), int32(y0), int32(x1), int32(y1)}}})
		corners += uint64(m.cornerCount)
		rings += uint64(m.ringCount)
		if corners > math.MaxUint32 || rings > math.MaxUint32 || corners > uint64(r.device.MaxBufferLength()/16) || rings > uint64(r.device.MaxBufferLength()/32) {
			return r.fail("Outline prepared device buffer limit")
		}
	}
	f.corners = r.grow(f.corners, max(int(corners)*16, 16))
	f.prepared = r.grow(f.prepared, max(int(rings)*32, 32))
	f.diagnostics = r.grow(f.diagnostics, 16)
	if f.corners == nil || f.prepared == nil || f.diagnostics == nil {
		return r.fail("Outline prepared allocation failed")
	}
	clear(f.diagnostics.Bytes(16))
	f.instanceCount = n
	return true
}

func (r *gbRenderer) outlineRowsEncode(ring *gbRing, cb mtl.CommandBuffer, poses *mtl.Buffer, u gbUniforms) bool {
	f := ring.outlineRowsRing
	if u.visual[0] <= .5 || f == nil || len(f.subjects) == 0 {
		return true
	}
	s := r.outlineRowsState
	if r.device.ReadWriteTextureTier() < mtl.ReadWriteTextureTier2 {
		return r.fail("Outline resolve requires tier-two read/write textures")
	}
	// Subjects write disjoint prepared rows and, through distinct composition
	// slots, disjoint atlas boxes, so their dispatches overlap. A subject that
	// reuses an earlier subject's slot waits for the textures written so far.
	enc := r.computeEncoderOf(cb, "outline_rows.nmOutlineRowsEncode#1", mtl.DispatchConcurrent)
	enc.Buffer(ring.instances, 0, 1)
	enc.Buffer(poses, 0, 2)
	enc.Bytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.Buffer(ring.rules, 0, 10)
	enc.Buffer(ring.compositionSlots, 0, 13)
	enc.Buffer(ring.compositionSelectors, 0, 14)
	enc.Buffer(ring.groups.instanceGroups, 0, 20)
	enc.Buffer(f.corners, 0, 21)
	enc.Buffer(f.prepared, 0, 22)
	enc.Buffer(f.diagnostics, 0, 24)
	enc.Buffer(ring.projections, 0, 27)
	view := ring.projectionView
	enc.Bytes(unsafe.Pointer(&view), 16, 29)
	enc.Pipeline(s.prepare)
	group := min(32, mtl.MaxThreads(s.prepare))
	for i := range f.subjects {
		subject := &f.subjects[i]
		m := r.meshes[subject.mesh]
		enc.Buffer(m.vertices, 0, 0)
		enc.Buffer(m.outlineRowsMesh.rings, 0, 4)
		enc.Bytes(unsafe.Pointer(&subject.control), int(unsafe.Sizeof(subject.control)), 23)
		enc.DispatchThreads(int(subject.control.span[1]), 1, 1, group, 1, 1)
	}
	enc.Barrier(mtl.BarrierBuffers)
	enc.Pipeline(s.resolve)
	enc.Texture(r.modelAtlasColor, 0)
	enc.Texture(r.modelAtlasEmission, 1)
	enc.Texture(r.modelMetadata, 2)
	enc.Texture(r.modelPalette, 4)
	maxThreads := mtl.MaxThreads(s.resolve)
	tx := min(8, maxThreads)
	ty := min(8, maxThreads/tx)
	selectors := unsafe.Slice((*uint32)(ring.compositionSelectors.Ptr), ring.compositionSelectors.Size/4)
	for i := range f.subjects {
		subject := &f.subjects[i]
		width := int(int64(subject.control.domain[2]) - int64(subject.control.domain[0]))
		height := int(int64(subject.control.domain[3]) - int64(subject.control.domain[1]))
		if width == 0 || height == 0 {
			continue
		}
		for j := 0; j < i; j++ {
			if selectors[f.subjects[j].control.span[0]] == selectors[subject.control.span[0]] {
				enc.Barrier(mtl.BarrierTextures)
				break
			}
		}
		enc.Buffer(r.meshes[subject.mesh].outlineRowsMesh.rings, 0, 4)
		enc.Bytes(unsafe.Pointer(&subject.control), int(unsafe.Sizeof(subject.control)), 23)
		enc.DispatchThreads(width, height, 1, tx, ty, 1)
	}
	enc.End()
	return true
}

// outlineRowsCompleted owns the diagnostic sample before Prepare resets it.
func (r *gbRenderer) outlineRowsCompleted(ring *gbRing) {
	f := ring.outlineRowsRing
	invalid := uint32(0)
	if f != nil && f.diagnostics != nil {
		invalid = *(*uint32)(f.diagnostics.Ptr)
	}
	s := r.outlineRowsState
	s.completedFrames++
	s.unsupportedRings += uint64(invalid)
	if invalid != 0 {
		s.unsupportedFrames++
	}
}

func (r *gbRenderer) outlineRowsInfo(ring *gbRing) map[string]any {
	var subjects, scratch int
	invalid := uint32(0)
	if ring != nil && ring.outlineRowsRing != nil {
		f := ring.outlineRowsRing
		subjects = len(f.subjects)
		if f.diagnostics != nil {
			invalid = *(*uint32)(f.diagnostics.Ptr)
		}
		for _, b := range []*mtl.Buffer{f.corners, f.prepared, f.diagnostics} {
			if b != nil {
				scratch += b.Size
			}
		}
	}
	s := r.outlineRowsState
	return map[string]any{"outline_row_subjects": subjects, "outline_row_unsupported_rings_last_frame": invalid, "outline_row_completed_frames": s.completedFrames, "outline_row_unsupported_rings_total": s.unsupportedRings, "outline_row_unsupported_frames_total": s.unsupportedFrames, "outline_row_scratch_bytes": scratch}
}

// Shadows (shadows.inc).
type gbShadowMesh struct {
	vertices, indices *mtl.Buffer
	indexCount        int
}
type gbShadowState struct {
	projected, commit           mtl.ID
	meshes                      []*gbShadowMesh
	body, emission, depth, mask *mtl.Texture
}
type gbShadowDraw struct{ mesh, instance, slot, subject uint32 }
type gbShadowFrame struct {
	slots, subjects, bodyRules, pieceFlags  *mtl.Buffer
	bodyDraws, projectedDraws               []gbShadowDraw
	bodyRuleSlots                           []uint32
	slotFirst, subjectNext                  []uint32
	subjectCount, bodyCount, projectedCount int
	width, height                           int
	tint, water                             [4]float32
}

func (r *gbRenderer) shadowsCreate() (*gbShadowState, error) {
	s := &gbShadowState{}
	var err error
	// All faces in this local mask paint one palette index; max is local
	// coverage only. Distinct subjects are composited separately in order.
	if s.projected, err = r.renderPipeline("nm_shadow_project_vertex", "nm_shadow_project_fragment", func(d mtl.PipelineDescriptor) {
		d.Attachment(0).Format(mtl.PixelFormatR8Unorm)
	}); err != nil {
		return nil, err
	}
	if s.commit, err = r.renderPipeline("nm_shadow_commit_vertex", "nm_shadow_commit_fragment", func(d mtl.PipelineDescriptor) {
		for i := 0; i < 2; i++ {
			a := d.Attachment(i)
			a.Format(mtl.PixelFormatRGBA16Float)
			a.Blending(true)
			a.SourceRGB(mtl.BlendOne)
			if i == 1 {
				a.DestRGB(mtl.BlendOne)
			} else {
				a.DestRGB(mtl.BlendOneMinusSourceAlpha)
			}
			a.SourceAlpha(mtl.BlendOne)
			a.DestAlpha(mtl.BlendOneMinusSourceAlpha)
		}
		d.SetDepthFormat(mtl.PixelFormatDepth32Float)
	}); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *gbRenderer) shadowsPrepare(s *gbShadowState, f *gbShadowFrame, u *nativeShadowUpload) bool {
	if s == nil || f == nil || u.Width > 8192 || u.Height > 8192 {
		return false
	}
	if u.TopologyCount == 0 && u.SubjectCount == 0 {
		f.subjectCount, f.bodyCount, f.projectedCount = 0, 0, 0
		return true
	}
	if (u.SubjectCount > 0 && (u.Subjects == nil || u.Slots == nil || u.Width == 0 || u.Height == 0)) || (u.BodyCount > 0 && (u.BodyDraws == nil || u.BodyRuleSlots == nil)) || (u.BodyRuleCount > 0 && u.BodyRules == nil) || (u.ProjectedCount > 0 && u.ProjectedDraws == nil) || (u.PieceCount > 0 && u.PieceFlags == nil) {
		return false
	}
	if len(s.meshes) == 0 && u.TopologyCount > 0 {
		if u.Topology == nil {
			return false
		}
		for _, src := range unsafe.Slice((*nativeShadowMesh)(u.Topology), u.TopologyCount) {
			if (src.VertexCount > 0 && src.Vertices == nil) || (src.IndexCount > 0 && src.Indices == nil) {
				return false
			}
			mesh := &gbShadowMesh{indexCount: int(src.IndexCount), vertices: r.device.NewBuffer(max(int(src.VertexCount)*64, 32), mtl.ResourceShared), indices: r.device.NewBuffer(max(int(src.IndexCount)*4, 32), mtl.ResourceShared)}
			if mesh.vertices == nil || mesh.indices == nil {
				return false
			}
			copyRaw(mesh.vertices.Ptr, src.Vertices, int(src.VertexCount)*64)
			copyRaw(mesh.indices.Ptr, src.Indices, int(src.IndexCount)*4)
			s.meshes = append(s.meshes, mesh)
		}
	}
	if len(s.meshes) != int(u.TopologyCount) {
		return false
	}
	f.subjectCount, f.bodyCount, f.projectedCount = int(u.SubjectCount), int(u.BodyCount), int(u.ProjectedCount)
	f.width, f.height, f.tint, f.water = int(u.Width), int(u.Height), u.Tint, u.Water
	f.slots = r.grow(f.slots, int(u.SlotCount)*48)
	f.subjects = r.grow(f.subjects, int(u.SubjectCount)*96)
	f.bodyRules = r.grow(f.bodyRules, max(int(u.BodyRuleCount)*112, 112))
	if f.slots == nil || f.subjects == nil || f.bodyRules == nil {
		return false
	}
	r.copyIn(upShadows, f.slots.Ptr, u.Slots, int(u.SlotCount)*48)
	r.copyIn(upShadows, f.subjects.Ptr, u.Subjects, int(u.SubjectCount)*96)
	r.copyIn(upShadows, f.bodyRules.Ptr, u.BodyRules, int(u.BodyRuleCount)*112)
	// Index subjects by commit slot, so each slot's commit visits only its own.
	type subject struct {
		screen, body, mask, params, placement [4]float32
		order                                 [4]uint32
	}
	subjects := unsafe.Slice((*subject)(u.Subjects), u.SubjectCount)
	slotCount := uint32(0)
	for i := range subjects {
		slotCount = max(slotCount, subjects[i].order[0]+1)
	}
	f.slotFirst = resizeU32(f.slotFirst, int(slotCount))
	f.subjectNext = resizeU32(f.subjectNext, int(u.SubjectCount))
	for i := range f.slotFirst {
		f.slotFirst[i] = math.MaxUint32
	}
	for i := int(u.SubjectCount) - 1; i >= 0; i-- {
		slot := subjects[i].order[0]
		f.subjectNext[i] = f.slotFirst[slot]
		f.slotFirst[slot] = uint32(i)
	}
	f.bodyRuleSlots = append(f.bodyRuleSlots[:0], unsafe.Slice((*uint32)(u.BodyRuleSlots), u.BodyCount)...)
	r.uploadBytes[upCPU] += uint64(u.BodyCount) * 4
	for _, s := range f.bodyRuleSlots {
		if s > u.BodyRuleCount {
			return false
		}
	}
	// The caller sets pieceFlags from its resident copy.
	f.bodyDraws = append(f.bodyDraws[:0], unsafe.Slice((*gbShadowDraw)(u.BodyDraws), u.BodyCount)...)
	f.projectedDraws = append(f.projectedDraws[:0], unsafe.Slice((*gbShadowDraw)(u.ProjectedDraws), u.ProjectedCount)...)
	r.uploadBytes[upCPU] += uint64(u.BodyCount+u.ProjectedCount) * 16
	w, h := int(u.Width), int(u.Height)
	if s.body != nil {
		w, h = max(s.body.Width, w), max(s.body.Height, h)
	}
	if w > 0 && h > 0 && (s.body == nil || w != s.body.Width || h != s.body.Height) {
		rt := uint(mtl.UsageRenderTarget | mtl.UsageShaderRead)
		replaceTexture(&s.body, r.target(w, h, mtl.PixelFormatRGBA8Unorm, rt))
		replaceTexture(&s.emission, r.target(w, h, mtl.PixelFormatRGBA8Unorm, mtl.UsageRenderTarget))
		replaceTexture(&s.depth, r.target(w, h, mtl.PixelFormatDepth32Float, rt))
		replaceTexture(&s.mask, r.target(w, h, mtl.PixelFormatR8Unorm, rt))
		if s.body == nil || s.emission == nil || s.depth == nil || s.mask == nil {
			return false
		}
	}
	return true
}

func resizeU32(s []uint32, n int) []uint32 {
	if cap(s) < n {
		return make([]uint32, n)
	}
	return s[:n]
}

func (r *gbRenderer) shadowBodyDraw(f *gbShadowFrame, ring *gbRing, enc mtl.RenderEncoder, d gbShadowDraw, ruleSlot uint32, outline bool) {
	mesh := r.meshes[d.mesh]
	count, indices, primitive := mesh.indexCount, mesh.indices, uint(mtl.PrimitiveTriangle)
	if outline {
		count, indices, primitive = mesh.edgeCount, mesh.edges, mtl.PrimitiveLine
	}
	if count == 0 {
		return
	}
	instance := int(d.instance)
	enc.VertexBuffer(mesh.vertices, 0, 0)
	enc.VertexBuffer(ring.instances, instance*4, 1)
	enc.VertexBuffer(ring.visuals, instance*64, 5)
	if ruleSlot != 0 {
		enc.VertexBuffer(f.bodyRules, int(ruleSlot-1)*112, 10)
	} else {
		enc.VertexBuffer(ring.rules, instance*112, 10)
	}
	enc.VertexBuffer(ring.materialOffsets, instance*4, 12)
	enc.VertexBuffer(f.slots, 0, 13)
	slot := d.slot
	enc.VertexBytes(unsafe.Pointer(&slot), 4, 14)
	enc.VertexBuffer(mesh.primitiveCenters, 0, 15)
	enc.DrawIndexedInstanced(primitive, count, indices, 0, 1)
}

func (r *gbRenderer) shadowsRaster(f *gbShadowFrame, ring *gbRing, cb mtl.CommandBuffer, poses *mtl.Buffer, u gbUniforms) {
	s := r.shadows
	if f.subjectCount == 0 {
		return
	}
	// The target can grow after another ring was prepared: clip space uses
	// the texture's size and this frame's own slots. The atlas only grows;
	// clear, raster and store just this frame's page.
	pageW, pageH := min(f.width, s.body.Width), min(f.height, s.body.Height)
	if pageW == 0 || pageH == 0 {
		pageW, pageH = s.body.Width, s.body.Height
	}
	u.composition[0], u.composition[1], u.composition[2] = float32(pageW), float32(pageH), f.tint[3]
	u.lightControl[1], u.mode[2], u.mode[3] = 0, 0, 3
	p := mtl.NewPass()
	p.Color(0, s.body, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
	// The commit reads body alpha and depth only; emission stays in tile memory.
	p.Color(1, s.emission, mtl.LoadDontCare, mtl.StoreDontCare).Clear(0, 0, 0, 0)
	p.Depth(s.depth, mtl.LoadClear, mtl.StoreStore, 1)
	p.TargetSize(pageW, pageH)
	enc := r.renderEncoder(cb, p, "shadows.nmShadowsRaster#1")
	r.modelBindings(enc, poses, ring, u)
	enc.Winding(mtl.WindingClockwise)
	enc.Cull(mtl.CullBack)
	enc.VertexBuffer(ring.subjectLights, 0, 17)
	enc.Pipeline(r.shadowBody)
	enc.DepthState(r.depth)
	visuals := unsafe.Slice((*float32)(ring.visuals.Ptr), ring.visuals.Size/4)
	for i, d := range f.bodyDraws[:f.bodyCount] {
		r.shadowBodyDraw(f, ring, enc, d, f.bodyRuleSlots[i], false)
	}
	// Outline coverage is part of the finished silhouette, before the shadow.
	if u.visual[0] > .5 {
		enc.Pipeline(r.shadowOutline)
		enc.DepthState(r.depth)
		for i, d := range f.bodyDraws[:f.bodyCount] {
			if visuals[d.instance*16+2] > 0 && visuals[d.instance*16+11] > 0 {
				r.shadowBodyDraw(f, ring, enc, d, f.bodyRuleSlots[i], true)
			}
		}
	}
	enc.End()
	p = mtl.NewPass()
	p.Color(0, s.mask, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
	p.TargetSize(pageW, pageH)
	enc = r.renderEncoder(cb, p, "shadows.nmShadowsRaster#2")
	enc.Pipeline(s.projected)
	enc.DepthState(0)
	enc.Winding(mtl.WindingClockwise)
	enc.Cull(mtl.CullBack)
	enc.VertexBuffer(poses, 0, 2)
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.VertexBuffer(f.pieceFlags, 0, 17)
	tint := f.tint
	enc.VertexBytes(unsafe.Pointer(&tint), 16, 18)
	for _, d := range f.projectedDraws[:f.projectedCount] {
		mesh := s.meshes[d.mesh]
		if mesh.indexCount == 0 {
			continue
		}
		enc.VertexBuffer(mesh.vertices, 0, 0)
		enc.VertexBuffer(ring.instances, int(d.instance)*4, 1)
		enc.VertexBuffer(ring.rules, int(d.instance)*112, 10)
		enc.VertexBuffer(f.slots, int(d.slot-1)*48, 13)
		enc.DrawIndexedInstanced(mtl.PrimitiveTriangle, mesh.indexCount, mesh.indices, 0, 1)
	}
	enc.End()
}

// shadowsCommitSlot runs immediately before a model slot's body commit,
// after earlier sprite/model writes. A staged child's solo shadow precedes
// the parent's.
func (r *gbRenderer) shadowsCommitSlot(f *gbShadowFrame, enc mtl.RenderEncoder, u gbUniforms, slot uint32) {
	s := r.shadows
	if f.subjectCount == 0 || r.ablated("shadows") || r.ablated("shadowcommit") {
		return
	}
	enc.Pipeline(s.commit)
	if r.slabCommitActive {
		enc.DepthState(r.spriteDepth)
	} else {
		enc.DepthState(0)
	}
	enc.Cull(mtl.CullNone)
	enc.VertexBuffer(f.subjects, 0, 0)
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBuffer(f.subjects, 0, 0)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	tint, water := f.tint, f.water
	enc.FragmentBytes(unsafe.Pointer(&tint), 16, 5)
	enc.FragmentBytes(unsafe.Pointer(&water), 16, 6)
	enc.FragmentTexture(s.body, 0)
	enc.FragmentTexture(s.depth, 1)
	enc.FragmentTexture(s.mask, 2)
	enc.FragmentTexture(orTexture(r.waterMask, r.emptyFog), 3)
	if int(slot) < len(f.slotFirst) {
		for i := f.slotFirst[slot]; i != math.MaxUint32; i = f.subjectNext[i] {
			index := i
			enc.VertexBytes(unsafe.Pointer(&index), 4, 1)
			enc.Draw(mtl.PrimitiveTriangle, 0, 6)
		}
	}
}

// Composition (composition.inc): the atlas owns model-local depth; the
// world receives subjects in production paint order.
func (r *gbRenderer) compositionPrepare(ring *gbRing, u *liveUpload) bool {
	ring.composed = u.Composed != 0
	ring.compositionSlotCount, ring.compositionDrawCount, ring.compositionPaintCount = int(u.CompositionSlotCount), int(u.CompositionDrawCount), int(u.CompositionPaintCount)
	ring.subjectLights = r.grow(ring.subjectLights, max(int(u.CompositionSlotCount)*36, 36))
	if ring.subjectLights == nil {
		return false
	}
	ring.compositionSlots = r.grow(ring.compositionSlots, max(int(u.CompositionSlotCount)*48, 48))
	ring.compositionSelectors = r.grow(ring.compositionSelectors, int(u.InstanceCount)*4)
	ring.compositionPaint = r.grow(ring.compositionPaint, int(u.CompositionPaintCount)*16)
	if ring.compositionSlots == nil || ring.compositionSelectors == nil || ring.compositionPaint == nil {
		return false
	}
	ring.annotationRangeCount = 0
	ring.projectilePaint = ring.projectilePaint[:0]
	if !ring.composed {
		return true
	}
	if u.AnnotationCount > 0 && u.AnnotationRangeCount == u.CompositionSlotCount && u.Annotations != nil && u.AnnotationRanges != nil {
		ring.annotations = r.grow(ring.annotations, int(u.AnnotationCount)*32)
		if ring.annotations == nil {
			return false
		}
		r.copyIn(upComposition, ring.annotations.Ptr, u.Annotations, int(u.AnnotationCount)*32)
		ring.annotationRanges = append(ring.annotationRanges[:0], unsafe.Slice((*uint32)(u.AnnotationRanges), int(u.AnnotationRangeCount)*2)...)
		r.uploadBytes[upCPU] += uint64(u.AnnotationRangeCount) * 8
		ring.annotationRangeCount = int(u.AnnotationRangeCount)
	}
	if u.CompositionSlotCount > 0 && (u.CompositionSlots == nil || u.CompositionPaint == nil || u.CompositionWidth == 0 || u.CompositionHeight == 0) {
		return false
	}
	if u.CompositionWidth > 8192 || u.CompositionHeight > 8192 {
		return false
	}
	r.copyIn(upComposition, ring.compositionSelectors.Ptr, u.CompositionSelectors, int(u.InstanceCount)*4)
	r.copyIn(upComposition, ring.compositionSlots.Ptr, u.CompositionSlots, int(u.CompositionSlotCount)*48)
	r.copyIn(upComposition, ring.compositionPaint.Ptr, u.CompositionPaint, int(u.CompositionPaintCount)*16)
	paint := unsafe.Slice((*uint32)(u.CompositionPaint), int(u.CompositionPaintCount)*4)
	// Effects commit the n-th projectile body by its position among them.
	for i := 0; i < int(u.CompositionPaintCount); i++ {
		if paint[i*4+3] == 2 {
			ring.projectilePaint = append(ring.projectilePaint, uint32(i))
		}
	}
	// Depth slabs: a direct slot's band is its index in the phase-major paint walk.
	ring.slotRanks = resizeU32(ring.slotRanks, max(int(u.CompositionSlotCount), 1))
	for i := range ring.slotRanks {
		ring.slotRanks[i] = math.MaxUint32
	}
	ring.directSlotCount = 0
	slots := unsafe.Slice((*float32)(u.CompositionSlots), int(u.CompositionSlotCount)*12)
	for i := 0; i < int(u.CompositionPaintCount); i++ {
		if paint[i*4+3] == 0 && paint[i*4] > 0 && paint[i*4] <= u.CompositionSlotCount {
			ring.slotRanks[paint[i*4]-1] = uint32(i)
		}
	}
	for i := 0; i < int(u.CompositionSlotCount); i++ {
		if slots[i*12+10] > .5 && ring.slotRanks[i] != math.MaxUint32 {
			ring.directSlotCount++
		}
	}
	ring.compositionDraws = append(ring.compositionDraws[:0], unsafe.Slice((*cloakDraw)(u.CompositionDraws), u.CompositionDrawCount)...)
	r.uploadBytes[upCPU] += uint64(u.CompositionDrawCount) * 16
	// Production always rasterizes the body at 2x; Supersample chooses the
	// resolve sample pattern, and underwater refraction keeps its gather.
	const rasterScale = 2
	copied := unsafe.Slice((*float32)(ring.compositionSlots.Ptr), int(u.CompositionSlotCount)*12)
	for i := 0; i < int(u.CompositionSlotCount); i++ {
		for j := 4; j < 8; j++ {
			copied[i*12+j] *= rasterScale
		}
	}
	ring.compositionPageWidth, ring.compositionPageHeight = int(u.CompositionWidth)*rasterScale, int(u.CompositionHeight)*rasterScale
	w, h := ring.compositionPageWidth, ring.compositionPageHeight
	if r.modelAtlasColor != nil {
		w, h = max(r.modelAtlasColor.Width, w), max(r.modelAtlasColor.Height, h)
	}
	if w > 0 && h > 0 && (r.modelAtlasColor == nil || w != r.modelAtlasColor.Width || h != r.modelAtlasColor.Height) {
		usage := uint(mtl.UsageRenderTarget | mtl.UsageShaderRead | mtl.UsageShaderWrite)
		replaceTexture(&r.modelMetadata, r.target(w, h, mtl.PixelFormatRGBA16Float, usage))
		replaceTexture(&r.modelAtlasColor, r.target(w, h, mtl.PixelFormatRGBA8Unorm, usage))
		replaceTexture(&r.modelAtlasEmission, r.target(w, h, mtl.PixelFormatRGBA8Unorm, usage))
		replaceTexture(&r.modelAtlasDepth, r.target(w, h, mtl.PixelFormatDepth32Float, mtl.UsageRenderTarget))
		if r.modelMetadata == nil || r.modelAtlasColor == nil || r.modelAtlasEmission == nil || r.modelAtlasDepth == nil {
			return false
		}
	}
	return true
}

// directView is one frame's direct-slot table.
type gbDirectView struct {
	slots []float32
	ranks []uint32
	count int
}

func directView(ring *gbRing) gbDirectView {
	if ring.directSlotCount == 0 {
		return gbDirectView{}
	}
	return gbDirectView{unsafe.Slice((*float32)(ring.compositionSlots.Ptr), ring.compositionSlotCount*12), ring.slotRanks, ring.compositionSlotCount}
}

func (v *gbDirectView) direct(slot uint32) bool {
	return slot > 0 && int(slot) <= v.count && v.slots[(slot-1)*12+10] > .5 && v.ranks[slot-1] != math.MaxUint32
}

// slabDepth is the depth for one paint band; later ranks are nearer and
// local 0..1 orders inside it.
func slabDepth(ring *gbRing, rank int, local float64) float64 {
	bands := float64(ring.compositionPaintCount) + 1
	return (bands - float64(rank) - 1 + local) / bands
}

// Commit, shadow and sprite quads carry no depth of their own: a zero-height
// depth range places every fragment of the draw inside its paint band.
func (r *gbRenderer) slabViewport(enc mtl.RenderEncoder, depth float64) {
	enc.Viewport(mtl.Viewport{W: float64(r.color.Width), H: float64(r.color.Height), Near: depth, Far: depth})
}
func (r *gbRenderer) slabViewportReset(enc mtl.RenderEncoder) {
	enc.Viewport(mtl.Viewport{W: float64(r.color.Width), H: float64(r.color.Height), Near: 0, Far: 1})
}

// directPhase draws one phase's slab subjects straight into the world with
// the staged mapping, before that phase's ordered commits test their depth.
func (r *gbRenderer) directPhase(ring *gbRing, enc mtl.RenderEncoder, poses *mtl.Buffer, u gbUniforms, phase uint32) {
	if ring.directSlotCount == 0 {
		return
	}
	slots := unsafe.Slice((*float32)(ring.compositionSlots.Ptr), ring.compositionSlotCount*12)
	u.mode[2], u.mode[3] = 0, 2
	bound := false
	view := directView(ring)
	for _, d := range ring.compositionDraws[:ring.compositionDrawCount] {
		slot := d.Group
		if !view.direct(slot) || uint32(slots[(slot-1)*12+9]) != phase {
			continue
		}
		if !bound {
			r.modelBindings(enc, poses, ring, u)
			enc.Winding(mtl.WindingClockwise)
			enc.Cull(mtl.CullNone)
			enc.Pipeline(r.directModel)
			enc.DepthState(r.depth)
			bound = true
		}
		s := slots[(slot-1)*12:]
		direct := [12]float32{float32(ring.compositionPaintCount) + 1, float32(ring.slotRanks[slot-1]), 0, 0, s[0], s[1], s[2], s[3], s[4], s[5], s[6], s[7]}
		enc.FragmentBytes(unsafe.Pointer(&direct), 48, 30)
		r.drawSubjectMember(enc, ring, d, false)
	}
}

func (r *gbRenderer) compositionRaster(ring *gbRing, cb mtl.CommandBuffer, poses *mtl.Buffer, u gbUniforms) {
	if ring.compositionSlotCount == 0 {
		return
	}
	p := mtl.NewPass()
	for i, t := range []*mtl.Texture{r.modelAtlasColor, r.modelAtlasEmission, r.modelMetadata} {
		p.Color(i, t, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
	}
	p.Depth(r.modelAtlasDepth, mtl.LoadClear, mtl.StoreDontCare, 1)
	// The textures only grow; clear, raster and store just this frame's page.
	pageW, pageH := min(ring.compositionPageWidth, r.modelAtlasColor.Width), min(ring.compositionPageHeight, r.modelAtlasColor.Height)
	if pageW > 0 && pageH > 0 {
		p.TargetSize(pageW, pageH)
		u.composition[0], u.composition[1] = float32(pageW), float32(pageH)
	}
	enc := r.renderEncoder(cb, p, "composition.nmCompositionRaster#1")
	u.mode[2], u.mode[3] = 0, 2
	r.modelBindings(enc, poses, ring, u)
	enc.Winding(mtl.WindingClockwise)
	enc.Cull(mtl.CullNone)
	enc.Pipeline(r.atlasModelMetadata)
	enc.DepthState(r.depth)
	view := directView(ring)
	for _, d := range ring.compositionDraws[:ring.compositionDrawCount] {
		if !view.direct(d.Group) {
			r.drawSubjectMember(enc, ring, d, false)
		}
	}
	// Construction row endpoints resolve complete native pixel blocks after this pass.
	enc.End()
}

func (r *gbRenderer) compositionCommit(ring *gbRing, enc mtl.RenderEncoder, u gbUniforms, phase uint32) {
	paint := unsafe.Slice((*uint32)(ring.compositionPaint.Ptr), ring.compositionPaintCount*4)
	begin := 0
	for begin < ring.compositionPaintCount && paint[begin*4+1] < phase {
		begin++
	}
	end := begin
	for end < ring.compositionPaintCount && paint[end*4+1] == phase {
		end++
	}
	if begin == end {
		return
	}
	// With slabs, every quad is depth-tested (never written) inside its own
	// paint band, so a later-ranked direct body still covers it.
	slabs := ring.directSlotCount > 0
	r.slabCommitActive = slabs
	view := directView(ring)
	if slabs {
		enc.DepthState(r.spriteDepth)
	} else {
		enc.DepthState(0)
	}
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	for begin < end {
		sprite := paint[begin*4+3] == 1
		stop := begin + 1
		for sprite && stop < end && paint[stop*4+3] == 1 && paint[stop*4] == paint[stop*4-4]+1 {
			stop++
		}
		// A shadow sits at the far edge of its band: behind its own body, in
		// front of earlier ranks.
		if slabs {
			r.slabViewport(enc, slabDepth(ring, begin, 0.9999))
		}
		if !sprite {
			r.shadowsCommitSlot(ring.shadows, enc, u, paint[begin*4])
		}
		// The selected-unit quad precedes its body in the same band: the body
		// covers it, earlier ranks do not [03 R-WATER-01 §1] rule 5.
		if !sprite && paint[begin*4+3] == 0 && paint[begin*4] > 0 && int(paint[begin*4]) <= ring.annotationRangeCount {
			at := (paint[begin*4] - 1) * 2
			if count := ring.annotationRanges[at+1]; count > 0 {
				if slabs {
					r.slabViewport(enc, slabDepth(ring, begin, 0.9998))
					enc.DepthState(r.spriteDepth)
				}
				enc.Pipeline(r.annotation)
				enc.VertexBuffer(ring.annotations, int(ring.annotationRanges[at])*32, 0)
				enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
				enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, int(count))
			}
		}
		if !sprite && paint[begin*4+3] == 0 && view.direct(paint[begin*4]) {
			begin = stop
			continue
		}
		if slabs {
			r.slabViewport(enc, slabDepth(ring, begin, 0.5))
			enc.DepthState(r.spriteDepth)
		}
		if sprite {
			enc.Pipeline(r.sprite)
			enc.VertexBuffer(ring.sprites, int(paint[begin*4]-1)*96, 0)
			enc.FragmentTexture(r.spriteAtlas, 0)
		} else {
			enc.Pipeline(r.atlasCommit)
			enc.VertexBuffer(ring.compositionSlots, 0, 0)
			enc.VertexBuffer(ring.compositionPaint, begin*16, 1)
			enc.FragmentTexture(r.modelAtlasColor, 0)
			enc.FragmentTexture(r.modelAtlasEmission, 1)
			r.waterObjectsBindCommit(ring, enc, u)
		}
		enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, stop-begin)
		begin = stop
	}
	if slabs {
		r.slabViewportReset(enc)
		enc.DepthState(0)
	}
	r.slabCommitActive = false
}

var _ = fmt.Sprintf
