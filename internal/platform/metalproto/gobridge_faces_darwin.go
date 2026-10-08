//go:build darwin && arm64

package metalproto

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// Projection metadata (projection.inc): CPU records only, no posed vertices.
func (r *gbRenderer) projectionPrepare(ring *gbRing, p *NativeProjectionUpload, instances, pieces uint32, flagGeneration uint64) bool {
	if p != nil && (p.RecordCount != instances || p.PieceCount != pieces || (instances > 0 && p.Records == nil) || (pieces > 0 && p.PieceFlags == nil)) {
		return r.fail("Projection metadata span mismatch")
	}
	rb, fb := max(int(instances)*48, 48), max(int(pieces)*4, 4)
	if rb > r.device.MaxBufferLength() || fb > r.device.MaxBufferLength() {
		return r.fail("Projection metadata exceeds device limit")
	}
	ring.projections = r.grow(ring.projections, rb)
	var flags unsafe.Pointer
	var generation uint64
	if p != nil {
		flags, generation = p.PieceFlags, flagGeneration
	}
	ring.projectionFlags = r.residentBuffer(ring, residentProjectionFlags, generation, flags, int(pieces)*4, upProjection)
	if ring.projections == nil || ring.projectionFlags == nil {
		return r.fail("Projection metadata allocation failed")
	}
	if p != nil {
		r.copyIn(upProjection, ring.projections.Ptr, p.Records, int(instances)*48)
		ring.projectionView = p.View
	} else {
		clear(ring.projections.Bytes(rb))
		ring.projectionView = [4]float32{}
	}
	return true
}

func (r *gbRenderer) projectionBind(ring *gbRing, e mtl.RenderEncoder, start int) {
	if ring == nil || ring.projections == nil {
		return
	}
	e.VertexBuffer(ring.projections, start*48, 27)
	e.VertexBuffer(ring.projectionFlags, 0, 28)
	view := ring.projectionView
	e.VertexBytes(unsafe.Pointer(&view), 16, 29)
}

// Groups (groups.inc).
type gbGroupState struct {
	merge                           mtl.ID
	frames, groups, members, pixels uint64
}
type gbGroupFrame struct {
	descriptors, members, instanceGroups   *mtl.Buffer
	groupCount, memberCount, instanceCount int
}

func (r *gbRenderer) groupsInitialize() error {
	p, err := r.computePipeline("nm_group_merge")
	if err != nil {
		return err
	}
	r.groups = &gbGroupState{merge: p}
	return nil
}

func (r *gbRenderer) groupsPrepare(ring *gbRing, u *nativeGroupUpload) bool {
	f := ring.groups
	if f == nil {
		f = &gbGroupFrame{}
		ring.groups = f
	}
	if u == nil {
		f.groupCount, f.memberCount, f.instanceCount = 0, 0, 0
		f.instanceGroups = r.grow(f.instanceGroups, 16)
		if f.instanceGroups == nil {
			return r.fail("Group dummy buffer allocation failed")
		}
		clear(f.instanceGroups.Bytes(16))
		return true
	}
	if r.groups == nil || u.GroupCount > math.MaxInt32 || u.MemberCount > math.MaxInt32 || u.InstanceCount > math.MaxInt32 || (u.GroupCount > 0 && u.Groups == nil) || (u.MemberCount > 0 && u.Members == nil) || (u.InstanceCount > 0 && u.InstanceGroups == nil) {
		return r.fail("Invalid group upload")
	}
	if int(u.InstanceCount)*4 > ring.instances.Size {
		return r.fail("Group instances exceed retained span")
	}
	f.groupCount, f.memberCount, f.instanceCount = 0, 0, 0
	gb, mb, ib := int(u.GroupCount)*32, int(u.MemberCount)*16, int(u.InstanceCount)*16
	if max(gb, mb, ib) > r.device.MaxBufferLength() {
		return r.fail("Group device buffer limit")
	}
	f.descriptors = r.grow(f.descriptors, max(gb, 32))
	f.members = r.grow(f.members, max(mb, 16))
	f.instanceGroups = r.grow(f.instanceGroups, max(ib, 16))
	if f.descriptors == nil || f.members == nil || f.instanceGroups == nil {
		return r.fail("Group buffer allocation failed")
	}
	clear(f.instanceGroups.Bytes(max(ib, 16)))
	r.copyIn(upGroups, f.instanceGroups.Ptr, u.InstanceGroups, ib)
	r.copyIn(upGroups, f.descriptors.Ptr, u.Groups, gb)
	r.copyIn(upGroups, f.members.Ptr, u.Members, mb)
	groups := unsafe.Slice((*nativeGroup)(f.descriptors.Ptr), u.GroupCount)
	members := unsafe.Slice((*nativeGroupMember)(f.members.Ptr), u.MemberCount)
	identities := unsafe.Slice((*int32)(f.instanceGroups.Ptr), int(u.InstanceCount)*4)
	selectors := unsafe.Slice((*uint32)(ring.compositionSelectors.Ptr), ring.compositionSelectors.Size/4)
	slots := uint32(ring.compositionSlotCount)
	for i, group := range groups {
		parent, source, final, first, count := group.Info[0], group.Info[1], group.Info[2], group.Info[3], group.Counts[0]
		if parent >= u.InstanceCount || source > slots || final == 0 || final > slots || count == 0 || first > u.MemberCount || count > u.MemberCount-first || source == final {
			return r.fail("Invalid group source/final span")
		}
		for _, member := range members[first : first+count] {
			if member.Instance >= u.InstanceCount || member.Slot == 0 || member.Slot > slots || member.Slot == final || selectors[member.Instance] != member.Slot {
				return r.fail("Invalid isolated group member")
			}
			identity := identities[member.Instance*4 : member.Instance*4+4]
			policy := int32(2)
			if group.Counts[2] != 0 {
				policy = 0
				if member.Instance != parent {
					policy = 1
				}
			}
			if identity[0] != int32(i+1) || identity[1] != policy || identity[2] != member.KeyDelta || identity[3] != int32(parent) {
				return r.fail("Group identity sidecar mismatch")
			}
		}
		// Every final slot is write-only. It must never alias any group's source.
		for _, m := range members {
			if m.Slot == final {
				return r.fail("Group final slot aliases immutable source")
			}
		}
		for _, g := range groups[:i] {
			if g.Info[2] == final {
				return r.fail("Group final slots overlap")
			}
		}
	}
	for i := 0; i < int(u.InstanceCount); i++ {
		if identities[i*4] < 0 || uint32(identities[i*4]) > u.GroupCount {
			return r.fail("Group identity outside descriptor span")
		}
	}
	f.groupCount, f.memberCount, f.instanceCount = int(u.GroupCount), int(u.MemberCount), int(u.InstanceCount)
	r.groups.frames++
	return true
}

func (r *gbRenderer) groupsBind(ring *gbRing, e mtl.RenderEncoder) {
	if ring != nil && ring.groups != nil {
		e.VertexBuffer(ring.groups.instanceGroups, 0, 20)
	}
}

// groupFinalSlot returns the group's final selector; ungrouped bodies keep
// their source. Water reflections use it for their extra occlusion test.
func (r *gbRenderer) groupFinalSlot(ring *gbRing, instance int, source uint32) uint32 {
	f := ring.groups
	if f == nil || instance >= f.instanceCount {
		return source
	}
	group := *(*int32)(unsafe.Add(f.instanceGroups.Ptr, instance*16))
	if group <= 0 || int(group) > f.groupCount {
		return source
	}
	return (*nativeGroup)(unsafe.Add(f.descriptors.Ptr, (int(group)-1)*32)).Info[2]
}

func (r *gbRenderer) groupsEncode(ring *gbRing, cb mtl.CommandBuffer, u gbUniforms) bool {
	f := ring.groups
	if f == nil || f.groupCount == 0 {
		return true
	}
	if r.groups.merge == 0 || r.modelAtlasColor == nil || r.modelAtlasEmission == nil || r.modelMetadata == nil {
		return r.fail("Missing group atlas or pipeline")
	}
	if r.device.ReadWriteTextureTier() < mtl.ReadWriteTextureTier2 {
		return r.fail("Group merge requires tier-two read/write textures")
	}
	for _, t := range []*mtl.Texture{r.modelAtlasColor, r.modelAtlasEmission, r.modelMetadata} {
		if t.Usage&mtl.UsageShaderWrite == 0 {
			return r.fail("Group atlas lacks shader-write usage")
		}
	}
	if ring.rules.Size < f.instanceCount*112 {
		return r.fail("Group parent rules exceed retained span")
	}
	groups := unsafe.Slice((*nativeGroup)(f.descriptors.Ptr), f.groupCount)
	slots := unsafe.Slice((*float32)(ring.compositionSlots.Ptr), ring.compositionSlotCount*12)
	enc := r.computeEncoder(cb, "groups.nmGroupsEncode#1")
	enc.Pipeline(r.groups.merge)
	enc.Buffer(f.descriptors, 0, 0)
	enc.Buffer(f.members, 0, 1)
	enc.Buffer(ring.compositionSlots, 0, 2)
	enc.Bytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.Buffer(ring.rules, 0, 4)
	enc.Texture(r.modelAtlasColor, 0)
	enc.Texture(r.modelAtlasEmission, 1)
	enc.Texture(r.modelMetadata, 2)
	for i := uint32(0); i < uint32(f.groupCount); i++ {
		slot := slots[(groups[i].Info[2]-1)*12:]
		width, height := int(slot[6]), int(slot[7])
		if width == 0 || height == 0 || slot[4] < 0 || slot[5] < 0 || int(slot[4])+width > r.modelAtlasColor.Width || int(slot[5])+height > r.modelAtlasColor.Height {
			enc.End()
			return r.fail("Group final rectangle outside atlas")
		}
		index := i
		enc.Bytes(unsafe.Pointer(&index), 4, 5)
		enc.DispatchThreads(width, height, 1, 8, 8, 1)
		r.groups.groups++
		r.groups.members += uint64(groups[i].Counts[0])
		r.groups.pixels += uint64(width * height)
	}
	enc.End()
	return true
}

func (r *gbRenderer) groupsInfo() map[string]any {
	s := r.groups
	return map[string]any{"group_frames": s.frames, "group_merges": s.groups, "group_members": s.members, "group_pixels": s.pixels}
}

// Faces (faces.inc): authored rings prepared on the GPU per instance.
const gbPreparedFaceBytes = 160

type gbFaceMesh struct {
	faces, corners, indices            *mtl.Buffer
	faceCount, cornerCount, indexCount int
}
type gbFaceState struct {
	prepare mtl.ID
	empty   *mtl.Buffer
}
type gbFaceRing struct {
	prepared, bases          *mtl.Buffer
	instanceCount, faceCount int
}

func (r *gbRenderer) facesInitialize() error {
	p, err := r.computePipeline("nm_face_prepare")
	if err != nil {
		return err
	}
	empty := r.device.NewBuffer(gbPreparedFaceBytes, mtl.ResourceShared)
	if empty == nil {
		return fmt.Errorf("prepared face fallback allocation failed")
	}
	clear(empty.Bytes(gbPreparedFaceBytes))
	r.faceState = &gbFaceState{prepare: p, empty: empty}
	return nil
}

func (r *gbRenderer) meshFaces(index uint32, u *NativeMeshFacesUpload) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if u == nil || int(index) >= len(r.meshes) {
		return 0
	}
	if u.FaceCount > math.MaxInt32 || u.CornerCount > math.MaxInt32 || u.IndexCount > math.MaxInt32 || (u.FaceCount > 0 && u.Faces == nil) || (u.CornerCount > 0 && u.Corners == nil) || (u.IndexCount > 0 && u.Indices == nil) {
		r.lastError = "Invalid authored face upload"
		return 0
	}
	fb, cbytes, ib := int(u.FaceCount)*32, int(u.CornerCount)*64, int(u.IndexCount)*4
	if max(fb, cbytes, ib) > r.device.MaxBufferLength() {
		r.lastError = "Authored face device buffer limit"
		return 0
	}
	type face struct{ cornerStart, cornerCount, piece, material, indexStart, indexCount, primitive, flags uint32 }
	faces := unsafe.Slice((*face)(u.Faces), u.FaceCount)
	for _, f := range faces {
		if f.cornerCount < 3 || uint64(f.cornerStart)+uint64(f.cornerCount) > uint64(u.CornerCount) || uint64(f.indexStart)+uint64(f.indexCount) > uint64(u.IndexCount) || uint64(f.indexCount) != 3*uint64(f.cornerCount-2) || f.flags&^1 != 0 || (f.flags&1 != 0) != (f.cornerCount == 4) {
			r.lastError = "Authored face exceeds immutable corner span"
			return 0
		}
	}
	for _, i := range unsafe.Slice((*uint32)(u.Indices), u.IndexCount) {
		if i >= u.CornerCount {
			r.lastError = "Authored face index exceeds immutable corner span"
			return 0
		}
	}
	m := &gbFaceMesh{faces: r.device.NewBuffer(max(fb, 32), mtl.ResourceShared), corners: r.device.NewBuffer(max(cbytes, 64), mtl.ResourceShared), indices: r.device.NewBuffer(max(ib, 4), mtl.ResourceShared)}
	if m.faces == nil || m.corners == nil || m.indices == nil {
		r.lastError = "Authored face allocation failed"
		return 0
	}
	copyRaw(m.faces.Ptr, u.Faces, fb)
	copyRaw(m.corners.Ptr, u.Corners, cbytes)
	copyRaw(m.indices.Ptr, u.Indices, ib)
	m.faceCount, m.cornerCount, m.indexCount = int(u.FaceCount), int(u.CornerCount), int(u.IndexCount)
	r.meshes[index].faceMesh = m
	return 1
}

func (r *gbRenderer) facesPrepare(ring *gbRing, instanceCount int) bool {
	if r.faceState == nil {
		return r.fail("Authored face pipeline is not initialized")
	}
	if instanceCount > math.MaxInt32 || len(ring.spans) < len(r.meshes)*2 {
		return r.fail("Invalid authored face instance spans")
	}
	spans := ring.spans
	total := 0
	for i, m := range r.meshes {
		start, count := int(spans[i*2]), int(spans[i*2+1])
		if start > instanceCount || count > instanceCount-start {
			return r.fail("Authored face span exceeds instance storage")
		}
		if m.faceMesh != nil {
			total += count * m.faceMesh.faceCount
		}
	}
	if total > math.MaxUint32-1 || total > r.device.MaxBufferLength()/gbPreparedFaceBytes || instanceCount*4 > r.device.MaxBufferLength() {
		return r.fail("Prepared face device buffer limit")
	}
	f := ring.faceRing
	if f == nil {
		f = &gbFaceRing{}
		ring.faceRing = f
	}
	f.prepared = r.grow(f.prepared, max(total*gbPreparedFaceBytes, gbPreparedFaceBytes))
	f.bases = r.grow(f.bases, max(instanceCount*4, 4))
	if f.prepared == nil || f.bases == nil {
		return r.fail("Prepared face allocation failed")
	}
	bases := unsafe.Slice((*uint32)(f.bases.Ptr), max(instanceCount, 1))
	clear(bases)
	base := uint32(1)
	for i, m := range r.meshes {
		if m.faceMesh == nil || m.faceMesh.faceCount == 0 {
			continue
		}
		start, count := int(spans[i*2]), int(spans[i*2+1])
		for j := 0; j < count; j++ {
			bases[start+j] = base
			base += uint32(m.faceMesh.faceCount)
		}
	}
	f.faceCount, f.instanceCount = total, instanceCount
	return true
}

func (r *gbRenderer) facesEncode(ring *gbRing, cb mtl.CommandBuffer, poses, waterObjects *mtl.Buffer, u gbUniforms) bool {
	f := ring.faceRing
	if f == nil || f.faceCount == 0 {
		return true
	}
	if waterObjects != nil && waterObjects.Size < f.instanceCount*16 {
		return r.fail("Water objects do not cover prepared face instances")
	}
	// Each thread writes only its own prepared face, so meshes overlap.
	e := r.computeEncoderOf(cb, "faces.nmFacesEncode#1", mtl.DispatchConcurrent)
	e.Pipeline(r.faceState.prepare)
	e.Buffer(ring.projections, 0, 27)
	e.Buffer(ring.projectionFlags, 0, 28)
	view := ring.projectionView
	e.Bytes(unsafe.Pointer(&view), 16, 29)
	e.Buffer(ring.instances, 0, 1)
	e.Buffer(poses, 0, 2)
	e.Bytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	e.Buffer(ring.visuals, 0, 5)
	e.Buffer(r.materials, 0, 6)
	e.Buffer(r.textureFrames, 0, 7)
	e.Buffer(ring.rules, 0, 10)
	e.Buffer(ring.materialOverrides, 0, 11)
	e.Buffer(ring.materialOffsets, 0, 12)
	e.Buffer(ring.compositionSlots, 0, 13)
	e.Buffer(ring.compositionSelectors, 0, 14)
	e.Buffer(orEmpty(waterObjects, r.faceState.empty), 0, 18)
	e.Buffer(f.prepared, 0, 21)
	e.Buffer(f.bases, 0, 22)
	group := min(64, mtl.MaxThreads(r.faceState.prepare))
	for i, m := range r.meshes {
		count := int(ring.spans[i*2+1])
		if count == 0 || m.faceMesh == nil || m.faceMesh.faceCount == 0 {
			continue
		}
		control := struct {
			span    [4]uint32
			texture [4]float32
		}{[4]uint32{ring.spans[i*2], uint32(count), uint32(m.faceMesh.faceCount), uint32(b2f(ring.composed))}, [4]float32{float32(r.atlas.Width), float32(r.atlas.Height), b2f(waterObjects != nil), 0}}
		e.Buffer(m.faceMesh.corners, 0, 0)
		e.Buffer(m.faceMesh.faces, 0, 4)
		e.Bytes(unsafe.Pointer(&control), int(unsafe.Sizeof(control)), 23)
		e.DispatchThreads(count*m.faceMesh.faceCount, 1, 1, group, 1, 1)
	}
	e.End()
	return true
}

// facesBind binds bases with the same instance offset as offsets/visuals on
// every model draw. Slot 21 is frame-wide.
func (r *gbRenderer) facesBind(ring *gbRing, e mtl.RenderEncoder, start int) {
	if ring == nil || ring.faceRing == nil {
		return
	}
	f := ring.faceRing
	e.VertexBuffer(f.prepared, 0, 21)
	e.FragmentBuffer(f.prepared, 0, 21)
	e.VertexBuffer(f.bases, start*4, 22)
}

// Face reflections (face_reflections.inc).
type gbFaceReflectionState struct{ budget, sum mtl.ID }
type gbFaceReflectionFrame struct {
	records, admission, budget *mtl.Buffer
	recordCount, faceCount     int
	encoded                    bool
}
type gbFaceReflectionRecord struct{ instance, faceBase, faceCount, reserved uint32 }

func (r *gbRenderer) faceReflectionsInitialize() error {
	budget, err := r.computePipeline("nm_fr_budget")
	if err != nil {
		return err
	}
	sum, err := r.computePipeline("nm_fr_sum")
	if err != nil {
		return err
	}
	r.faceReflections = &gbFaceReflectionState{budget: budget, sum: sum}
	return nil
}

func (r *gbRenderer) faceReflectionsPrepare(ring *gbRing) bool {
	faces := ring.faceRing
	count := ring.compositionDrawCount
	if (count > 0 && (faces == nil || len(ring.compositionDraws) < count)) || count > math.MaxUint32 {
		return r.fail("Invalid ordered reflection face draws")
	}
	f := ring.faceReflections
	if f == nil {
		f = &gbFaceReflectionFrame{}
		ring.faceReflections = f
	}
	f.recordCount, f.encoded = 0, false
	f.faceCount = 0
	if faces != nil {
		f.faceCount = faces.faceCount
	}
	if count > r.device.MaxBufferLength()/16 || f.faceCount > r.device.MaxBufferLength()/4 {
		return r.fail("Reflection face budget buffer limit")
	}
	f.records = r.grow(f.records, max(count*16, 16))
	f.admission = r.grow(f.admission, max(f.faceCount*4, 4))
	f.budget = r.grow(f.budget, 16)
	if f.records == nil || f.admission == nil || f.budget == nil {
		return r.fail("Reflection face budget allocation failed")
	}
	if count == 0 {
		return true
	}
	bases := unsafe.Slice((*uint32)(faces.bases.Ptr), max(faces.instanceCount, 1))
	records := unsafe.Slice((*gbFaceReflectionRecord)(f.records.Ptr), count)
	for _, d := range ring.compositionDraws[:count] {
		if int(d.Mesh) >= len(r.meshes) || int(d.Instance) >= faces.instanceCount {
			return r.fail("Reflection face order exceeds retained subject span")
		}
		mesh := r.meshes[d.Mesh].faceMesh
		base := bases[d.Instance]
		if base == 0 || mesh == nil || mesh.faceCount == 0 {
			continue
		}
		if int(base)-1+mesh.faceCount > faces.faceCount {
			return r.fail("Reflection face order exceeds prepared face span")
		}
		records[f.recordCount] = gbFaceReflectionRecord{d.Instance, base - 1, uint32(mesh.faceCount), 0}
		f.recordCount++
	}
	return true
}

func (r *gbRenderer) faceReflectionsEncode(ring *gbRing, cb mtl.CommandBuffer, objects *mtl.Buffer, objectCount int) bool {
	f := ring.faceReflections
	if f == nil {
		return r.fail("Reflection face budget was not prepared")
	}
	f.encoded = false
	if objectCount > 0 && (objects == nil || objects.Size < objectCount*16) {
		return r.fail("Reflection objects exceed admission buffer")
	}
	clearing := r.blitEncoder(cb, "face_reflections.nmFaceReflectionsEncode#1")
	clearing.Fill(f.admission, 0, max(f.faceCount*4, 4), 0)
	clearing.Fill(f.budget, 16, 16, 0)
	clearing.End()
	e := r.computeEncoder(cb, "face_reflections.nmFaceReflectionsEncode#2")
	counts := [4]uint32{uint32(f.recordCount), uint32(objectCount), uint32(f.faceCount), 0}
	var prepared *mtl.Buffer
	if ring.faceRing != nil {
		prepared = ring.faceRing.prepared
	}
	bind := func(pipeline mtl.ID, budgetOffset int) {
		e.Pipeline(pipeline)
		e.Buffer(f.records, 0, 0)
		e.Buffer(orEmpty(objects, r.faceState.empty), 0, 18)
		e.Buffer(orEmpty(prepared, r.faceState.empty), 0, 21)
		e.Buffer(f.admission, 0, 25)
		e.Buffer(f.budget, budgetOffset, 26)
		e.Bytes(unsafe.Pointer(&counts), 16, 27)
	}
	bind(r.faceReflections.sum, 16)
	if f.recordCount > 0 {
		e.DispatchGroups(f.recordCount, 1, 1, 64, 1, 1)
	}
	bind(r.faceReflections.budget, 0)
	e.DispatchThreads(1, 1, 1, 1, 1, 1)
	e.End()
	f.encoded = true
	return true
}

func (r *gbRenderer) faceReflectionsBind(ring *gbRing, e mtl.RenderEncoder) {
	f := ring.faceReflections
	if f == nil || !f.encoded {
		return
	}
	e.VertexBuffer(f.admission, 0, 25)
	e.VertexBuffer(f.budget, 0, 26)
}

// faceReflectionsInfo is read only after the ring's command buffer completes.
func (r *gbRenderer) faceReflectionsInfo(ring *gbRing) map[string]any {
	if ring == nil || ring.faceReflections == nil || !ring.faceReflections.encoded {
		return map[string]any{"reflection_face_budget_enabled": false}
	}
	f := ring.faceReflections
	b := unsafe.Slice((*uint32)(f.budget.Ptr), 4)
	return map[string]any{"reflection_face_budget_enabled": true, "reflection_model_source_vertices": b[0], "reflection_admitted_faces": b[1], "reflection_cap_suppressed_faces": b[2], "reflection_eligible_faces": b[3], "reflection_order_records": f.recordCount}
}

// Terrain tiles (terrain_tiles.inc).
type gbTerrainTiles struct {
	device               mtl.Device
	base, detail, lookup *mtl.Texture
	parameters           struct {
		layout   [4]uint32
		controls [4]float32
	}
	hasDetail              bool
	uploadedBytes, uploads uint64
}

func (s *gbTerrainTiles) texture(w, h int, format uint, pixels unsafe.Pointer) *mtl.Texture {
	t := s.device.NewTexture2D(format, w, h, mtl.StorageModeShared, mtl.UsageShaderRead)
	if t != nil {
		t.Replace(0, 0, w, h, pixels, w*4)
	}
	return t
}

func newGBTerrainTiles(device mtl.Device) (*gbTerrainTiles, error) {
	s := &gbTerrainTiles{device: device}
	blank := uint32(0)
	s.base = s.texture(1, 1, mtl.PixelFormatRGBA8Unorm, unsafe.Pointer(&blank))
	s.detail = s.base
	s.lookup = s.texture(1, 1, mtl.PixelFormatR32Uint, unsafe.Pointer(&blank))
	if s.base == nil || s.lookup == nil {
		return nil, fmt.Errorf("Terrain tile fallback allocation failed")
	}
	return s, nil
}

func (s *gbTerrainTiles) prepare(u *nativeTerrainTilesUpload) error {
	if u == nil {
		s.parameters.controls[0] = 0
		return nil
	}
	if u.Base == nil || u.Lookup == nil || u.MapWidth == 0 || u.MapHeight == 0 || u.MapWidth > 4096 || u.MapHeight > 4096 || u.BaseWidth == 0 || u.BaseHeight == 0 || u.BaseWidth%32 != 0 || u.BaseHeight%32 != 0 || u.BaseWidth > 8192 || u.BaseHeight > 8192 {
		return fmt.Errorf("Invalid native terrain tile upload")
	}
	detail := u.Detail != nil
	if detail && (u.DetailWidth == 0 || u.DetailHeight == 0 || u.DetailWidth != u.BaseWidth*2 || u.DetailHeight != u.BaseHeight*2) {
		return fmt.Errorf("Invalid detail terrain tile upload")
	}
	if !detail && (u.DetailWidth != 0 || u.DetailHeight != 0) {
		return fmt.Errorf("Detail terrain dimensions have no pixels")
	}
	cells := (u.BaseWidth / 32) * (u.BaseHeight / 32)
	for _, v := range unsafe.Slice((*uint32)(u.Lookup), int(u.MapWidth)*int(u.MapHeight)) {
		if v >= cells {
			return fmt.Errorf("Terrain lookup exceeds atlas cells")
		}
	}
	base := s.texture(int(u.BaseWidth), int(u.BaseHeight), mtl.PixelFormatRGBA8Unorm, u.Base)
	lookup := s.texture(int(u.MapWidth), int(u.MapHeight), mtl.PixelFormatR32Uint, u.Lookup)
	detailAtlas := base
	if detail {
		detailAtlas = s.texture(int(u.DetailWidth), int(u.DetailHeight), mtl.PixelFormatRGBA8Unorm, u.Detail)
	}
	if base == nil || lookup == nil || detailAtlas == nil {
		return fmt.Errorf("Terrain tile allocation failed")
	}
	s.base, s.lookup, s.detail, s.hasDetail = base, lookup, detailAtlas, detail
	s.parameters.layout = [4]uint32{u.MapWidth, u.MapHeight, u.BaseWidth / 32, 0}
	if detail {
		s.parameters.layout[3] = u.DetailWidth / 64
	}
	s.parameters.controls = [4]float32{1, 0, 0, 0}
	s.uploadedBytes = uint64(u.BaseWidth)*uint64(u.BaseHeight)*4 + uint64(u.MapWidth)*uint64(u.MapHeight)*4
	if detail {
		s.uploadedBytes += uint64(u.DetailWidth) * uint64(u.DetailHeight) * 4
	}
	s.uploads++
	return nil
}

func (s *gbTerrainTiles) bind(e mtl.RenderEncoder, filter, detail bool) bool {
	if s == nil || s.base == nil || s.lookup == nil || s.detail == nil {
		return false
	}
	p := s.parameters
	p.controls[1] = b2f(filter)
	p.controls[2] = b2f(detail && s.hasDetail)
	e.FragmentTexture(s.base, 6)
	e.FragmentTexture(s.lookup, 7)
	e.FragmentTexture(s.detail, 8)
	e.FragmentBytes(unsafe.Pointer(&p), int(unsafe.Sizeof(p)), 25)
	return true
}

func (s *gbTerrainTiles) info() map[string]any {
	detail := []int{}
	if s.hasDetail {
		detail = []int{s.detail.Width, s.detail.Height}
	}
	side := 0
	if s.hasDetail {
		side = 64
	}
	return map[string]any{"terrain_tiles_enabled": s.parameters.controls[0] > .5, "terrain_tiles_native_side": 32, "terrain_tiles_detail_side": side, "terrain_tiles_map_dimensions": []uint32{s.parameters.layout[0], s.parameters.layout[1]}, "terrain_tiles_native_atlas_dimensions": []int{s.base.Width, s.base.Height}, "terrain_tiles_detail_atlas_dimensions": detail, "terrain_tiles_retained_bytes": s.uploadedBytes, "terrain_tiles_uploads": s.uploads, "terrain_tiles_sampling": "nearest at identity; bilinear clamped within each authored tile under fractional world transform"}
}
