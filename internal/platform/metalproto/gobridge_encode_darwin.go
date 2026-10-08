//go:build darwin && arm64

package metalproto

import (
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// Encoder helpers attach pass timing when NANOLATHE_METAL_PASS_TIMING is set.
func (r *gbRenderer) renderEncoder(cb mtl.CommandBuffer, pass mtl.PassDescriptor, label string) mtl.RenderEncoder {
	r.timing.attachRender(pass, label)
	return cb.Render(pass)
}
func (r *gbRenderer) computeEncoderOf(cb mtl.CommandBuffer, label string, dispatch uint) mtl.ComputeEncoder {
	if e, ok := r.timing.compute(cb, label, dispatch); ok {
		return e
	}
	return cb.Compute(dispatch)
}
func (r *gbRenderer) computeEncoder(cb mtl.CommandBuffer, label string) mtl.ComputeEncoder {
	return r.computeEncoderOf(cb, label, mtl.DispatchSerial)
}
func (r *gbRenderer) blitEncoder(cb mtl.CommandBuffer, label string) mtl.BlitEncoder {
	if e, ok := r.timing.blit(cb, label); ok {
		return e
	}
	return cb.Blit()
}

func orEmpty(b, empty *mtl.Buffer) *mtl.Buffer {
	if b == nil {
		return empty
	}
	return b
}
func orTexture(t, fallback *mtl.Texture) *mtl.Texture {
	if t == nil {
		return fallback
	}
	return t
}

func (r *gbRenderer) drawMeshes(enc mtl.RenderEncoder, ring *gbRing, visualPass int) {
	n := len(r.meshes)
	for index, m := range r.meshes {
		at := (visualPass*n + index) * 2
		count, start := m.instanceCount, 0
		if ring != nil {
			count, start = int(ring.spans[at+1]), int(ring.spans[index*2])
		}
		outline := visualPass == 2 || visualPass >= 6
		indices, indexBuffer := m.indexCount, m.indices
		if outline {
			indices, indexBuffer = m.edgeCount, m.edges
		}
		if count == 0 || indices == 0 {
			continue
		}
		enc.VertexBuffer(m.primitiveCenters, 0, 15)
		enc.VertexBuffer(m.vertices, 0, 0)
		if ring != nil {
			enc.VertexBuffer(ring.instances, start*4, 1)
			enc.VertexBuffer(ring.visuals, start*64, 5)
			enc.VertexBuffer(ring.rules, start*112, 10)
			enc.VertexBuffer(ring.materialOffsets, start*4, 12)
		} else {
			enc.VertexBuffer(m.instances, 0, 1)
			enc.VertexBuffer(m.visuals, 0, 5)
			enc.VertexBuffer(r.emptyLights, 0, 10)
			enc.VertexBuffer(r.emptyLights, 0, 12)
		}
		if visualPass != 0 {
			enc.VertexBuffer(ring.selectors, int(ring.spans[at])*4, 8)
		} else {
			enc.VertexBuffer(r.emptyLights, 0, 8)
		}
		r.projectionBind(ring, enc, start)
		r.facesBind(ring, enc, start)
		if ring != nil && ring.groups != nil {
			enc.VertexBuffer(ring.groups.instanceGroups, start*16, 20)
		}
		primitive := uint(mtl.PrimitiveTriangle)
		if outline {
			primitive = mtl.PrimitiveLine
		}
		enc.DrawIndexedInstanced(primitive, indices, indexBuffer, 0, count)
	}
}

// encodeOverlayInto draws the HUD into the pass that owns the drawable, so
// the finished world is not stored and reloaded for the overlay alone.
func (r *gbRenderer) encodeOverlayInto(enc mtl.RenderEncoder, u gbUniforms, ring *gbRing) {
	if ring == nil || ring.overlayCount == 0 {
		return
	}
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentTexture(orTexture(ring.overlayAtlas, r.emptyFog), 0)
	enc.FragmentTexture(r.terrainImage, 1)
	if u.visual[2] > .5 {
		enc.FragmentTexture(ring.fog, 2)
	} else {
		enc.FragmentTexture(r.emptyFog, 2)
	}
	// Split only consecutive blend runs. Reordering them would change shaded
	// gadgets, text and modal surfaces that overlap earlier HUD commands.
	quads := unsafe.Slice((*float32)(ring.overlay.Ptr), ring.overlayCount*16)
	for start := 0; start < ring.overlayCount; {
		multiply := quads[start*16+13] > .5
		end := start + 1
		for end < ring.overlayCount && (quads[end*16+13] > .5) == multiply {
			end++
		}
		if multiply {
			enc.Pipeline(r.overlayMultiply)
		} else {
			enc.Pipeline(r.overlay)
		}
		enc.VertexBuffer(ring.overlay, start*64, 0)
		enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, end-start)
		start = end
	}
}

// worldPass keeps colour/emission; the airborne boundary starts a new depth
// plane so earlier terrain/effects cannot clip pass B [03 R-RAST-01 §7].
func (r *gbRenderer) worldPass(clearDepth bool) mtl.PassDescriptor {
	p := mtl.NewPass()
	p.Color(0, r.color, mtl.LoadLoad, mtl.StoreStore)
	if r.emissionDead {
		p.Color(1, r.emission, mtl.LoadDontCare, mtl.StoreDontCare)
	} else {
		p.Color(1, r.emission, mtl.LoadLoad, mtl.StoreStore)
	}
	load := uint(mtl.LoadLoad)
	if clearDepth {
		load = mtl.LoadClear
	}
	p.Depth(r.depthImage, load, mtl.StoreStore, 1)
	return p
}

func (r *gbRenderer) modelBindings(enc mtl.RenderEncoder, poses *mtl.Buffer, ring *gbRing, u gbUniforms) {
	r.projectionBind(ring, enc, 0)
	r.groupsBind(ring, enc)
	r.facesBind(ring, enc, 0)
	if ring != nil {
		enc.FragmentBuffer(ring.rules, 0, 24)
	} else {
		enc.FragmentBuffer(r.emptyLights, 0, 24)
	}
	if ring != nil && ring.groups != nil {
		enc.VertexBuffer(ring.groups.instanceGroups, 0, 20)
	} else {
		enc.VertexBuffer(r.emptyLights, 0, 20)
	}
	if ring == nil {
		enc.VertexBuffer(r.emptyLights, 0, 27)
		enc.VertexBuffer(r.emptyLights, 0, 28)
		var view [4]float32
		enc.VertexBytes(unsafe.Pointer(&view), 16, 29)
		enc.VertexBuffer(r.emptyLights, 0, 21)
		enc.FragmentBuffer(r.emptyLights, 0, 21)
		enc.VertexBuffer(r.emptyLights, 0, 22)
		enc.VertexBuffer(r.emptyLights, 0, 17)
		enc.VertexBuffer(r.emptyLights, 0, 16)
		enc.FragmentBuffer(r.emptyLights, 0, 16)
		enc.VertexBuffer(r.emptyLights, 0, 13)
		enc.VertexBuffer(r.emptyLights, 0, 14)
	} else {
		enc.VertexBuffer(ring.subjectLights, 0, 17)
		enc.VertexBuffer(ring.retainedLights, 0, 16)
		enc.FragmentBuffer(ring.retainedLights, 0, 16)
		enc.VertexBuffer(ring.compositionSlots, 0, 13)
		enc.VertexBuffer(ring.compositionSelectors, 0, 14)
	}
	enc.Cull(mtl.CullNone)
	enc.VertexBuffer(r.emptyLights, 0, 8)
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.VertexBuffer(poses, 0, 2)
	enc.FragmentTexture(r.atlas, 0)
	if ring != nil {
		enc.FragmentBuffer(ring.lights, 0, 4)
	} else {
		enc.FragmentBuffer(r.emptyLights, 0, 4)
	}
	enc.VertexBuffer(r.materials, 0, 6)
	enc.VertexBuffer(r.textureFrames, 0, 7)
	if ring != nil {
		enc.VertexBuffer(ring.materialOverrides, 0, 11)
	} else {
		enc.VertexBuffer(r.emptyLights, 0, 11)
	}
	enc.FragmentTexture(orTexture(r.modelPalette, r.emptyFog), 4)
}

func (r *gbRenderer) drawPhase(enc mtl.RenderEncoder, poses *mtl.Buffer, ring *gbRing, u gbUniforms, phase int) {
	u.mode[2] = 1
	r.modelBindings(enc, poses, ring, u)
	enc.Pipeline(r.model)
	enc.DepthState(r.depth)
	r.drawMeshes(enc, ring, 3+phase)
	if u.visual[0] > .5 {
		enc.Pipeline(r.outline)
		enc.DepthState(r.spriteDepth)
		r.drawMeshes(enc, ring, 6+phase)
	}
}

func (r *gbRenderer) drawSubjectMember(enc mtl.RenderEncoder, ring *gbRing, d cloakDraw, outline bool) {
	instance := int(d.Instance)
	r.projectionBind(ring, enc, instance)
	r.facesBind(ring, enc, instance)
	if ring.groups != nil {
		enc.VertexBuffer(ring.groups.instanceGroups, instance*16, 20)
	}
	enc.VertexBuffer(ring.compositionSelectors, instance*4, 14)
	mesh := r.meshes[d.Mesh]
	count, indices, primitive := mesh.indexCount, mesh.indices, uint(mtl.PrimitiveTriangle)
	if outline {
		count, indices, primitive = mesh.edgeCount, mesh.edges, mtl.PrimitiveLine
	}
	if count == 0 {
		return
	}
	enc.VertexBuffer(mesh.primitiveCenters, 0, 15)
	enc.VertexBuffer(mesh.vertices, 0, 0)
	enc.VertexBuffer(ring.instances, instance*4, 1)
	enc.VertexBuffer(ring.visuals, instance*64, 5)
	enc.VertexBuffer(ring.rules, instance*112, 10)
	enc.VertexBuffer(ring.materialOffsets, instance*4, 12)
	enc.DrawIndexedInstanced(primitive, count, indices, 0, 1)
}

func (r *gbRenderer) encodeCloaks(cb mtl.CommandBuffer, poses *mtl.Buffer, ring *gbRing, u gbUniforms, phase uint32) {
	draws := ring.cloaks
	visuals := unsafe.Slice((*float32)(ring.visuals.Ptr), ring.visuals.Size/4)
	for begin := 0; begin < ring.cloakCount; {
		end := begin + 1
		for end < ring.cloakCount && draws[end].Group == draws[begin].Group {
			end++
		}
		if draws[begin].Phase != phase {
			begin = end
			continue
		}
		p := mtl.NewPass()
		for i, t := range []*mtl.Texture{r.subjectColor, r.subjectEmission} {
			p.Color(i, t, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
		}
		p.Depth(r.subjectDepth, mtl.LoadClear, mtl.StoreStore, 1)
		enc := r.renderEncoder(cb, p, "cloak.raster")
		subject := u
		subject.mode[2], subject.mode[3] = 0, 1
		r.modelBindings(enc, poses, ring, subject)
		enc.Pipeline(r.subjectModel)
		enc.DepthState(r.depth)
		for i := begin; i < end; i++ {
			r.drawSubjectMember(enc, ring, draws[i], false)
		}
		if u.visual[0] > .5 {
			enc.Pipeline(r.outline)
			enc.DepthState(r.depth)
			for i := begin; i < end; i++ {
				if visuals[draws[i].Instance*16+2] > 0 && visuals[draws[i].Instance*16+11] > 0 {
					r.drawSubjectMember(enc, ring, draws[i], true)
				}
			}
		}
		enc.End()
		// Resolve each visible subject pixel once. Native scene depth remains
		// an approximation; opacity is applied after self-occlusion
		// [GPU design §22.1].
		enc = r.renderEncoder(cb, r.worldPass(false), "cloak.resolve")
		enc.Pipeline(r.subjectResolve)
		enc.DepthState(r.depth)
		enc.FragmentTexture(r.subjectColor, 0)
		enc.FragmentTexture(r.subjectEmission, 1)
		enc.FragmentTexture(r.subjectDepth, 2)
		opacity := visuals[draws[begin].Instance*16+1]
		enc.FragmentBytes(unsafe.Pointer(&opacity), 4, 0)
		enc.Draw(mtl.PrimitiveTriangle, 0, 3)
		enc.End()
		begin = end
	}
}

// retainedProjectile commits one projectile body for the effects walk.
func (r *gbRenderer) retainedProjectile(ring *gbRing, cb mtl.CommandBuffer, u gbUniforms, slot, flags uint32) bool {
	if flags&1 != 0 {
		return false
	}
	paint := unsafe.Slice((*uint32)(ring.compositionPaint.Ptr), ring.compositionPaintCount*4)
	if int(slot) >= len(ring.projectilePaint) {
		return false
	}
	at := int(ring.projectilePaint[slot])
	if paint[at*4] == 0 {
		return false
	}
	if flags&2 != 0 {
		return true
	}
	enc := r.fxShared
	if enc == 0 {
		enc = r.renderEncoder(cb, r.worldPass(false), "effects.projectileCommit")
	}
	enc.DepthState(0)
	enc.Pipeline(r.atlasCommit)
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.VertexBuffer(ring.compositionSlots, 0, 0)
	enc.VertexBuffer(ring.compositionPaint, at*16, 1)
	enc.FragmentTexture(r.modelAtlasColor, 0)
	enc.FragmentTexture(r.modelAtlasEmission, 1)
	enc.Draw(mtl.PrimitiveTriangle, 0, 6)
	if enc != r.fxShared {
		enc.End()
	}
	return true
}

func (r *gbRenderer) drawUpperSprites(enc mtl.RenderEncoder, ring *gbRing, u gbUniforms, depth mtl.ID, bindUniforms bool) {
	if ring.spriteCount <= ring.groundSpriteCount {
		return
	}
	enc.Pipeline(r.sprite)
	enc.DepthState(depth)
	enc.VertexBuffer(ring.sprites, ring.groundSpriteCount*96, 0)
	if bindUniforms {
		enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	}
	enc.FragmentTexture(r.spriteAtlas, 0)
	enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, ring.spriteCount-ring.groundSpriteCount)
}

func (r *gbRenderer) encode(cb mtl.CommandBuffer, endpoints, poses *mtl.Buffer, u gbUniforms, destination *mtl.Texture, ring *gbRing) bool {
	// Compute once per submitted piece in this acquired slot. Body, outline
	// and shadow passes share the result; capture reruns the same path.
	r.emissionDead = ring != nil && (ring.glowEnabled || !r.effects)
	if count := int(u.timing[2]); count > 0 {
		c := r.computeEncoder(cb, "pose.interpolate")
		c.Pipeline(r.interpolatePoses)
		c.Buffer(endpoints, 0, 0)
		c.Buffer(poses, 0, 1)
		c.Bytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		c.DispatchThreads(count, 1, 1, min(256, mtl.MaxThreads(r.interpolatePoses)), 1, 1)
		c.End()
	}
	if u.lightControl[1] > .5 && !r.ablated("groundlight") {
		p := mtl.NewPass()
		p.Color(0, r.groundField, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
		e := r.renderEncoder(cb, p, "light.groundField")
		e.Pipeline(r.groundLight)
		e.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		e.VertexBuffer(ring.retainedLights, 0, 16)
		e.FragmentBuffer(ring.retainedLights, 0, 16)
		if u.lightControl[0] > 0 {
			e.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, int(u.lightControl[0]))
		}
		e.End()
	}
	if ring != nil && ring.composed && ring.compositionSlotCount > 0 && u.lightControl[1] > .5 {
		e := r.computeEncoder(cb, "light.selectSubjects")
		e.Pipeline(r.selectLights)
		e.Bytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		e.Buffer(ring.compositionSlots, 0, 13)
		e.Buffer(ring.retainedLights, 0, 16)
		e.Buffer(ring.subjectLights, 0, 17)
		e.DispatchThreads(ring.compositionSlotCount, 1, 1, min(64, mtl.MaxThreads(r.selectLights)), 1, 1)
		e.End()
	}
	if ring != nil && ring.composed {
		var objects *mtl.Buffer
		objectCount := 0
		if ring.waterObjectsRing != nil {
			objects, objectCount = ring.waterObjectsRing.objects, ring.waterObjectsRing.objectCount
		}
		if !r.facesEncode(ring, cb, poses, objects, u) || !r.faceReflectionsEncode(ring, cb, objects, objectCount) {
			return false
		}
		r.compositionRaster(ring, cb, poses, u)
		if !r.outlineRowsEncode(ring, cb, poses, u) || !r.groupsEncode(ring, cb, u) {
			return false
		}
		if !r.ablated("shadows") && !r.ablated("shadowraster") {
			r.shadowsRaster(ring.shadows, ring, cb, poses, u)
		}
	}
	// Composed frames draw shadows as commits, never into the mask, so they
	// bind an empty mask instead of clearing and storing a full-size one.
	shadowMask := r.shadowMask
	if ring != nil && ring.composed {
		shadowMask = r.emptyMask
	}
	if u.visual[0] > .5 && (ring == nil || !ring.composed) {
		pass := mtl.NewPass()
		pass.Color(0, r.shadowMask, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
		shadow := r.renderEncoder(cb, pass, "shadow.mask")
		shadow.Pipeline(r.shadow)
		shadow.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		shadow.VertexBuffer(poses, 0, 2)
		shadow.VertexTexture(r.heights, 1)
		shadow.VertexBuffer(r.materials, 0, 6)
		shadow.VertexBuffer(r.textureFrames, 0, 7)
		if ring != nil {
			shadow.VertexBuffer(ring.materialOverrides, 0, 11)
		} else {
			shadow.VertexBuffer(r.emptyLights, 0, 11)
		}
		shadow.FragmentTexture(r.atlas, 0)
		shadow.FragmentTexture(orTexture(r.modelPalette, r.emptyFog), 4)
		shadow.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		r.drawMeshes(shadow, ring, 1)
		shadow.End()
	}
	pass := mtl.NewPass()
	pass.Color(0, r.color, mtl.LoadClear, mtl.StoreStore).Clear(0.08, 0.09, 0.1, 1)
	if r.emissionDead {
		pass.Color(1, r.emission, mtl.LoadDontCare, mtl.StoreDontCare)
	} else {
		pass.Color(1, r.emission, mtl.LoadClear, mtl.StoreStore)
	}
	pass.Depth(r.depthImage, mtl.LoadClear, mtl.StoreStore, 1)
	enc := r.renderEncoder(cb, pass, "world.terrain")
	enc.DepthState(r.depth)
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.Cull(mtl.CullNone)
	if ring != nil {
		enc.FragmentBuffer(ring.lights, 0, 4)
	} else {
		enc.FragmentBuffer(r.emptyLights, 0, 4)
	}
	deferredWater := ring != nil && ring.composed && ring.waterObjectsRing != nil && r.water[7] > .5
	// The surface pass applies the ground-light field and shadows after the
	// water; the terrain must not fall back to per-pixel point lights.
	if deferredWater {
		base := u
		base.lightControl[1], base.visual[0], base.mode[1] = 0, 0, 0
		water := r.water
		water[7] = 0
		enc.FragmentBytes(unsafe.Pointer(&base), gbUniformsSize, 3)
		enc.FragmentBytes(unsafe.Pointer(&water), int(unsafe.Sizeof(water)), 9)
	}
	var flags uint32
	if ring != nil {
		flags = ring.terrainFlags
	}
	if !r.terrainTileState.bind(enc, flags&1 != 0, flags&2 != 0) {
		enc.End()
		r.lastError = "Terrain tile bind failed"
		return false
	}
	enc.Pipeline(r.terrain)
	enc.DepthState(r.spriteDepth)
	enc.FragmentTexture(r.groundField, 5)
	enc.FragmentTexture(r.terrainImage, 0)
	enc.FragmentTexture(r.heights, 1)
	enc.FragmentTexture(shadowMask, 2)
	enc.FragmentTexture(orTexture(r.waterMask, r.emptyFog), 3)
	if !deferredWater {
		enc.FragmentBytes(unsafe.Pointer(&r.water), int(unsafe.Sizeof(r.water)), 9)
	}
	enc.Draw(mtl.PrimitiveTriangle, 0, 6)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	// Short features are painted after TNT and before any model [03 R-RAST-01 §6].
	if ring != nil && ring.groundSpriteCount > 0 && !ring.composed {
		enc.Pipeline(r.sprite)
		enc.DepthState(r.spriteDepth)
		enc.VertexBuffer(ring.sprites, 0, 0)
		enc.FragmentTexture(r.spriteAtlas, 0)
		enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, ring.groundSpriteCount)
	}
	switch {
	case ring == nil:
		r.modelBindings(enc, poses, nil, u)
		enc.Pipeline(r.model)
		enc.DepthState(r.depth)
		r.drawMeshes(enc, nil, 0)
		enc.End()
	case ring.composed && !r.splitWorld && (ring.effectRing == nil || len(ring.effectRing.lenses) == 0):
		// One world encoder from the first effect layer to the last: effect
		// layers and commit phases alternate in painter order without storing
		// and reloading the world. Lens snapshots need the split path. With
		// no water pass between, the world continues in the terrain encoder.
		r.waterObjectsSeabed(ring, enc, u)
		if !r.ablated("water") && (deferredWater || r.waterObjectsReflecting(ring)) {
			enc.End()
			if deferredWater && !r.waterObjectsSurface(ring, cb, u) {
				return false
			}
			if !r.waterObjectsReflections(ring, cb, poses, u) {
				return false
			}
			enc = r.renderEncoder(cb, r.worldPass(false), "world.merged")
		}
		fx := !r.ablated("effects")
		r.fxExternal = enc
		if fx {
			r.effectsDraw(ring, cb, 0, u)
		}
		r.directPhase(ring, enc, poses, u, 0)
		r.compositionCommit(ring, enc, u, 0)
		if fx {
			r.effectsDraw(ring, cb, 1, u)
		}
		r.directPhase(ring, enc, poses, u, 1)
		r.compositionCommit(ring, enc, u, 1)
		r.drawUpperSprites(enc, ring, u, 0, true)
		if ring.effectRing == nil || ring.effectRing.layerCount == 0 {
			r.compositionCommit(ring, enc, u, 2)
		}
		if fx {
			r.effectsDraw(ring, cb, 2, u)
		}
		r.directPhase(ring, enc, poses, u, 3)
		r.compositionCommit(ring, enc, u, 3)
		if fx {
			r.effectsDraw(ring, cb, 3, u)
			r.effectsDraw(ring, cb, 4, u)
		}
		r.fxExternal = 0
		enc.End()
	case ring.composed:
		r.waterObjectsSeabed(ring, enc, u)
		enc.End()
		if !r.ablated("water") {
			if deferredWater && !r.waterObjectsSurface(ring, cb, u) {
				return false
			}
			if !r.waterObjectsReflections(ring, cb, poses, u) {
				return false
			}
		}
		fx := !r.ablated("effects")
		if fx {
			r.effectsDraw(ring, cb, 0, u)
		}
		enc = r.renderEncoder(cb, r.worldPass(false), "world.commitShortFeatures")
		r.directPhase(ring, enc, poses, u, 0)
		r.compositionCommit(ring, enc, u, 0)
		enc.End()
		if fx {
			r.effectsDraw(ring, cb, 1, u)
		}
		enc = r.renderEncoder(cb, r.worldPass(false), "world.commitGround")
		r.directPhase(ring, enc, poses, u, 1)
		r.compositionCommit(ring, enc, u, 1)
		r.drawUpperSprites(enc, ring, u, 0, true)
		if ring.effectRing == nil || ring.effectRing.layerCount == 0 {
			r.compositionCommit(ring, enc, u, 2)
		}
		enc.End()
		if fx {
			r.effectsDraw(ring, cb, 2, u)
		}
		enc = r.renderEncoder(cb, r.worldPass(false), "world.commitAir")
		r.directPhase(ring, enc, poses, u, 3)
		r.compositionCommit(ring, enc, u, 3)
		enc.End()
		if fx {
			r.effectsDraw(ring, cb, 3, u)
			r.effectsDraw(ring, cb, 4, u)
		}
	case ring.cloakCount == 0:
		// Keep opaque phases in one tile render pass. A depth-only fullscreen
		// draw resets pass B's ownership without storing both HDR attachments.
		r.drawPhase(enc, poses, ring, u, 0)
		r.drawPhase(enc, poses, ring, u, 1)
		r.drawUpperSprites(enc, ring, u, r.spriteDepth, false)
		if ring.airCount > 0 {
			enc.Pipeline(r.clearDepth)
			enc.DepthState(r.alwaysDepth)
			enc.Draw(mtl.PrimitiveTriangle, 0, 3)
			r.drawPhase(enc, poses, ring, u, 2)
		}
		enc.End()
	default:
		r.drawPhase(enc, poses, ring, u, 0)
		enc.End()
		r.encodeCloaks(cb, poses, ring, u, 0)
		enc = r.renderEncoder(cb, r.worldPass(false), "world.depthPhaseGround")
		r.drawPhase(enc, poses, ring, u, 1)
		r.drawUpperSprites(enc, ring, u, r.spriteDepth, false)
		enc.End()
		r.encodeCloaks(cb, poses, ring, u, 1)
		if ring.airCount > 0 {
			enc = r.renderEncoder(cb, r.worldPass(true), "world.depthPhaseAir")
			r.drawPhase(enc, poses, ring, u, 2)
			enc.End()
			r.encodeCloaks(cb, poses, ring, u, 2)
		}
	}
	glowEnabled := ring != nil && ring.glowEnabled
	r.glowDeferResolve = u.visual[0] > .5 && !r.legacyPost
	if glowEnabled && !r.ablated("glow") {
		var atlas *mtl.Texture
		if ring.effectRing != nil {
			atlas = ring.effectRing.atlas
		}
		if !r.productionGlow.encode(r, cb, r.emission, r.color, atlas, r.color, ring.glowVertices, ring.glowCount, ring.glowParams) {
			r.lastError = "Production glow submission failed"
		}
	}
	if r.effects && !glowEnabled {
		compute := r.computeEncoder(cb, "bloom.blurX")
		compute.Pipeline(r.blur)
		d := [2]float32{1, 0}
		compute.Bytes(unsafe.Pointer(&d), 8, 0)
		compute.Texture(r.emission, 0)
		compute.Texture(r.blurA, 1)
		compute.DispatchThreads(r.blurA.Width, r.blurA.Height, 1, 16, 16, 1)
		compute.End()
		compute = r.computeEncoder(cb, "bloom.blurY")
		compute.Pipeline(r.blur)
		d = [2]float32{0, 1}
		compute.Bytes(unsafe.Pointer(&d), 8, 0)
		compute.Texture(r.blurA, 0)
		compute.Texture(r.blurB, 1)
		compute.DispatchThreads(r.blurB.Width, r.blurB.Height, 1, 16, 16, 1)
		compute.End()
	}
	// Resolve, slab silhouettes, distortion and fog in the one pass that owns
	// the drawable; the HUD follows in the same encoder.
	if u.visual[0] > .5 && !r.legacyPost {
		var directSlots, paintCount, distortions int
		var fog *mtl.Texture
		var gain float32
		if ring != nil {
			directSlots, paintCount, distortions, fog, gain = ring.directSlotCount, ring.compositionPaintCount, ring.distortionCount, ring.fog, ring.worldGain
		}
		f := [4]float32{0, 0, gain, 0}
		if directSlots > 0 {
			f[0] = float32(paintCount) + 1
		}
		if u.timing[3] > .5 && !glowEnabled {
			f[1] = 1
		}
		last := mtl.NewPass()
		last.Color(0, destination, mtl.LoadDontCare, mtl.StoreStore)
		enc = r.renderEncoder(cb, last, "post.final")
		enc.Pipeline(r.finalPass)
		enc.FragmentTexture(r.color, 0)
		enc.FragmentTexture(r.blurB, 1)
		enc.FragmentTexture(orTexture(fog, r.emptyFog), 2)
		enc.FragmentTexture(orTexture(r.fogAtlas, r.emptyFog), 3)
		enc.FragmentTexture(r.depthImage, 4)
		enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		enc.FragmentBytes(unsafe.Pointer(&f), 16, 4)
		glow := r.productionGlow
		on := glowEnabled && ring.glowCount > 0 && glow.octaves != nil && !r.ablated("glow")
		gf := struct {
			blur  [4]float32
			sizes [4]uint32
			flags [4]float32
		}{sizes: [4]uint32{uint32(glow.width+3) / 4, uint32(glow.height+3) / 4, uint32(glow.width+7) / 8, uint32(glow.height+7) / 8}}
		if ring != nil {
			gf.blur = ring.glowParams.blur
		}
		if on {
			gf.flags[0] = 1
			enc.FragmentTexture(glow.octaves, 5)
		} else {
			enc.FragmentTexture(r.emptyFog, 5)
		}
		enc.FragmentBytes(unsafe.Pointer(&gf), int(unsafe.Sizeof(gf)), 5)
		enc.Draw(mtl.PrimitiveTriangle, 0, 3)
		if r.effects && distortions > 0 {
			enc.Pipeline(r.distortionFinal)
			enc.VertexBuffer(ring.distortions, 0, 0)
			enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
			enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, distortions)
		}
		r.encodeOverlayInto(enc, u, ring)
		enc.End()
		return r.lastError == ""
	}
	if u.visual[0] > .5 {
		if glowEnabled {
			u.timing[3] = 0
		}
		distort := r.effects && ring != nil && ring.distortionCount > 0
		world := r.postColor
		if distort {
			world = r.worldSnapshot
		}
		pass = mtl.NewPass()
		pass.Color(0, world, mtl.LoadDontCare, mtl.StoreStore)
		enc = r.renderEncoder(cb, pass, "post.worldResolve")
		enc.Pipeline(r.world)
		enc.FragmentTexture(r.color, 0)
		enc.FragmentTexture(r.depthImage, 2)
		bands := float32(0)
		if ring != nil && ring.directSlotCount > 0 {
			bands = float32(ring.compositionPaintCount) + 1
		}
		enc.FragmentBytes(unsafe.Pointer(&bands), 4, 4)
		enc.FragmentTexture(r.blurB, 1)
		enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		enc.Draw(mtl.PrimitiveTriangle, 0, 3)
		enc.End()
		if distort {
			u.timing[3] = b2f(r.effects)
			copy := r.blitEncoder(cb, "post.distortionCopy")
			copy.CopyTexture(r.worldSnapshot, 0, 0, r.postColor.Width, r.postColor.Height, r.postColor, 0, 0)
			copy.End()
			pass = mtl.NewPass()
			pass.Color(0, r.postColor, mtl.LoadLoad, mtl.StoreStore)
			enc = r.renderEncoder(cb, pass, "post.distortion")
			enc.Pipeline(r.distortion)
			enc.VertexBuffer(ring.distortions, 0, 0)
			enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
			enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
			enc.FragmentTexture(r.worldSnapshot, 0)
			enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, ring.distortionCount)
			enc.End()
		}
		pass = mtl.NewPass()
		pass.Color(0, destination, mtl.LoadDontCare, mtl.StoreStore)
		enc = r.renderEncoder(cb, pass, "post.fog")
		enc.Pipeline(r.fog)
		enc.FragmentTexture(r.postColor, 0)
		var fog *mtl.Texture
		if ring != nil {
			fog = ring.fog
		}
		enc.FragmentTexture(orTexture(fog, r.emptyFog), 1)
		enc.FragmentTexture(orTexture(r.fogAtlas, r.emptyFog), 2)
		enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
		enc.Draw(mtl.PrimitiveTriangle, 0, 3)
		r.encodeOverlayInto(enc, u, ring)
		enc.End()
		return r.lastError == ""
	}
	pass = mtl.NewPass()
	pass.Color(0, destination, mtl.LoadDontCare, mtl.StoreStore)
	enc = r.renderEncoder(cb, pass, "post.composite")
	enc.Pipeline(r.composite)
	enc.FragmentTexture(r.color, 0)
	enc.FragmentTexture(r.blurB, 1)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.Draw(mtl.PrimitiveTriangle, 0, 3)
	r.encodeOverlayInto(enc, u, ring)
	enc.End()
	return r.lastError == ""
}
