//go:build darwin

package metalproto

import (
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// liveUpload copies one live frame into the next free ring slot; livePresent
// acquires a drawable, encodes and commits it. Every read of caller memory
// happens in the upload, so the caller may reuse its buffers once it returns.
func (r *gbRenderer) liveUpload(u *liveUpload) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if (!r.interactive && r.rowCount >= r.rowCapacity) || u.LightCount > 32 || u.OverlayCount > math.MaxInt32 || u.GroundSpriteCount > u.SpriteCount || u.CloakCount > u.InstanceCount || u.AirCount > u.InstanceCount || r.livePending {
		return 0
	}
	r.pump()
	if !r.windowVisible() {
		return -1
	}
	start := mtl.MediaTime()
	r.acquire()
	acquired := mtl.MediaTime()
	copyStart := acquired
	ring := r.rings[r.frameNumber%3]
	ring.worldGain = u.WorldGain
	if !r.effectsPrepare(ring, (*nativeEffectsUpload)(u.Effects)) || !r.groundPrepare(ring, (*nativeGroundUpload)(u.GroundEffects), u.GroundGeneration) {
		return 0
	}
	if ring.shadows == nil {
		ring.shadows = &gbShadowFrame{}
	}
	if !r.shadowsPrepare(r.shadows, ring.shadows, &u.Shadows) {
		r.lastError = "Shadow preparation failed"
		return 0
	}
	ring.shadows.pieceFlags = r.residentBuffer(ring, residentShadowFlags, u.ShadowFlagGeneration, u.Shadows.PieceFlags, int(u.Shadows.PieceCount)*4, upShadows)
	if ring.shadows.pieceFlags == nil {
		return 0
	}
	ring.glowVertices = r.grow(ring.glowVertices, int(u.GlowCount)*48)
	if ring.glowVertices == nil {
		return 0
	}
	r.copyIn(upGlow, ring.glowVertices.Ptr, u.GlowVertices, int(u.GlowCount)*48)
	ring.glowCount, ring.glowEnabled = int(u.GlowCount), u.GlowEnabled != 0
	r.uploadBytes[upGlow] += uint64(unsafe.Sizeof(ring.glowParams))
	ring.glowParams = *(*gbGlowParameters)(unsafe.Pointer(&u.GlowParams))
	if !r.compositionPrepare(ring, u) {
		return 0
	}
	uploadOverlay := u.OverlayVersion != 0 && u.OverlayVersion != r.overlayVersion
	var overlayX, overlayY, overlayW, overlayH, overlayRowBytes int
	if uploadOverlay {
		if u.OverlayTexture == nil || u.OverlayWidth == 0 || u.OverlayHeight == 0 || u.OverlayWidth > math.MaxInt32 || u.OverlayHeight > math.MaxInt32 {
			return 0
		}
		width, height := int(u.OverlayWidth), int(u.OverlayHeight)
		resized := r.overlayAtlas == nil || r.overlayAtlas.Width != width || r.overlayAtlas.Height != height
		full := r.overlayVersion == 0 || resized
		if resized {
			t := r.device.NewTexture2D(mtl.PixelFormatRGBA8Unorm, width, height, mtl.StorageModeShared, mtl.UsageShaderRead)
			if t == nil {
				return 0
			}
			replaceTexture(&r.overlayAtlas, t)
		}
		overlayX, overlayY, overlayW, overlayH = int(u.OverlayDirty[0]), int(u.OverlayDirty[1]), int(u.OverlayDirty[2]), int(u.OverlayDirty[3])
		if full || overlayW == 0 || overlayH == 0 {
			overlayX, overlayY, overlayW, overlayH = 0, 0, width, height
		}
		if overlayX+overlayW > width || overlayY+overlayH > height {
			return 0
		}
		overlayRowBytes = (overlayW*4 + 255) &^ 255
		ring.overlayUpload = r.grow(ring.overlayUpload, overlayRowBytes*overlayH)
		if ring.overlayUpload == nil {
			return 0
		}
		for y := 0; y < overlayH; y++ {
			r.copyIn(upOverlayTexture, unsafe.Add(ring.overlayUpload.Ptr, y*overlayRowBytes), unsafe.Add(u.OverlayTexture, ((overlayY+y)*width+overlayX)*4), overlayW*4)
		}
		r.overlayVersion = u.OverlayVersion
		r.overlayTextureUploads++
		r.overlayTextureBytes += uint64(overlayW * overlayH * 4)
	}
	if u.CloakCount > 0 && u.Cloaks == nil {
		return 0
	}
	cloaks := unsafe.Slice((*cloakDraw)(u.Cloaks), u.CloakCount)
	for _, d := range cloaks {
		if int(d.Mesh) >= len(r.meshes) || d.Instance >= u.InstanceCount || d.Phase > 2 {
			return 0
		}
	}
	ring.cloaks = append(ring.cloaks[:0], cloaks...)
	r.uploadBytes[upCPU] += uint64(len(cloaks) * 16)
	ring.cloakCount, ring.airCount = int(u.CloakCount), int(u.AirCount)
	if u.CloakCount > 0 && r.subjectColor == nil {
		w, h := r.color.Width, r.color.Height
		rt := uint(mtl.UsageRenderTarget | mtl.UsageShaderRead)
		r.subjectColor = r.target(w, h, mtl.PixelFormatRGBA16Float, rt)
		r.subjectEmission = r.target(w, h, mtl.PixelFormatRGBA16Float, rt)
		r.subjectDepth = r.target(w, h, mtl.PixelFormatDepth32Float, rt)
		if r.subjectColor == nil || r.subjectEmission == nil || r.subjectDepth == nil {
			return 0
		}
	}
	ring.overlayAtlas = r.overlayAtlas
	// Reuse/grow only the now-free slot. Command buffers in flight retain
	// their own resources; no other slot is modified or resized here.
	r.water = *(*[20]float32)(unsafe.Pointer(&u.Water))
	ws, freshWorld := r.residentSlot(residentWorld, u.WorldGeneration)
	ring.resident[residentWorld] = ws
	if freshWorld {
		r.worldPoses[ws] = r.grow(r.worldPoses[ws], int(u.PoseCount)*128)
		r.worldInstances[ws] = r.grow(r.worldInstances[ws], int(u.InstanceCount)*4)
		r.worldMaterialOffs[ws] = r.grow(r.worldMaterialOffs[ws], max(int(u.InstanceCount)*4, 4))
		r.worldMaterials[ws] = r.grow(r.worldMaterials[ws], max(int(u.MaterialCount)*48, 48))
		r.worldRules[ws] = r.grow(r.worldRules[ws], max(int(u.InstanceCount)*112, 112))
	} else if c := r.worldCounts[ws]; c[0] != u.PoseCount || c[1] != u.InstanceCount || c[2] != u.MaterialCount || c[3] != u.RulesCount {
		r.lastError = "Resident world generation reused with different counts"
		return 0
	}
	ring.poses, ring.instances, ring.materialOffsets, ring.materialOverrides, ring.rules = r.worldPoses[ws], r.worldInstances[ws], r.worldMaterialOffs[ws], r.worldMaterials[ws], r.worldRules[ws]
	ring.sprites = r.residentBuffer(ring, residentSprites, u.SpriteGeneration, u.Sprites, int(u.SpriteCount)*96, upSprites)
	ring.lights = r.grow(ring.lights, int(u.LightCount)*32)
	ring.visuals = r.grow(ring.visuals, int(u.InstanceCount)*64)
	ring.distortions = r.grow(ring.distortions, int(u.DistortionCount)*64)
	ring.selectors = r.grow(ring.selectors, int(u.InstanceCount)*12)
	ring.overlay = r.grow(ring.overlay, int(u.OverlayCount)*64)
	ring.retainedLights = r.grow(ring.retainedLights, max(int(u.RetainedLightCount)*64, 64))
	if ring.retainedLights == nil {
		return 0
	}
	r.copyIn(upLights, ring.retainedLights.Ptr, u.RetainedLights, int(u.RetainedLightCount)*64)
	ring.interpolatedPoses = r.growPoseOutput(ring.interpolatedPoses, int(u.PoseCount)*64)
	if ring.rules == nil || ring.materialOffsets == nil || ring.materialOverrides == nil || ring.poses == nil || ring.interpolatedPoses == nil || ring.instances == nil || ring.sprites == nil || ring.lights == nil || ring.visuals == nil || ring.distortions == nil || ring.selectors == nil || ring.overlay == nil {
		return 0
	}
	if freshWorld {
		if u.PoseCount > 0 {
			r.copyIn(upPoses, ring.poses.Ptr, u.Previous, int(u.PoseCount)*64)
			r.copyIn(upPoses, unsafe.Add(ring.poses.Ptr, int(u.PoseCount)*64), u.Current, int(u.PoseCount)*64)
		}
		if u.InstanceCount > 0 {
			r.copyIn(upTables, ring.rules.Ptr, u.Rules, int(u.InstanceCount)*112)
			r.copyIn(upTables, ring.materialOffsets.Ptr, u.MaterialOffsets, int(u.InstanceCount)*4)
			r.copyIn(upTables, ring.instances.Ptr, u.Offsets, int(u.InstanceCount)*4)
		}
		r.copyIn(upMaterials, ring.materialOverrides.Ptr, u.Materials, int(u.MaterialCount)*48)
		r.worldCounts[ws] = [4]uint32{u.PoseCount, u.InstanceCount, u.MaterialCount, u.RulesCount}
		r.residentGenerations[residentWorld][ws] = u.WorldGeneration
	}
	r.copyIn(upVisuals, ring.visuals.Ptr, u.Visuals, int(u.InstanceCount)*64)
	r.copyIn(upDistortions, ring.distortions.Ptr, u.Distortions, int(u.DistortionCount)*64)
	ring.distortionCount = int(u.DistortionCount)
	if u.FogWidth != 0 && u.FogHeight != 0 {
		fs, freshFog := r.residentSlot(residentFog, u.FogGeneration)
		ring.resident[residentFog] = fs
		fog := r.fogTextures[fs]
		if freshFog && (fog == nil || fog.Width != int(u.FogWidth) || fog.Height != int(u.FogHeight)) {
			t := r.device.NewTexture2D(mtl.PixelFormatRGBA8Unorm, int(u.FogWidth), int(u.FogHeight), mtl.StorageModeShared, mtl.UsageShaderRead)
			replaceTexture(&r.fogTextures[fs], t)
			fog = t
		}
		if fog == nil || fog.Width != int(u.FogWidth) || fog.Height != int(u.FogHeight) {
			return 0
		}
		if freshFog {
			r.uploadBytes[upFog] += uint64(u.FogWidth) * uint64(u.FogHeight) * 4
			fog.Replace(0, 0, int(u.FogWidth), int(u.FogHeight), u.Fog, int(u.FogWidth)*4)
			r.residentGenerations[residentFog][fs] = u.FogGeneration
		}
		ring.fog = fog
	} else {
		ring.fog = nil
		ring.resident[residentFog] = 3
	}
	r.copyIn(upOverlay, ring.overlay.Ptr, u.Overlay, int(u.OverlayCount)*64)
	ring.overlayCount = int(u.OverlayCount)
	r.copyIn(upLights, ring.lights.Ptr, u.Lights, int(u.LightCount)*32)
	spanWords := len(r.meshes) * gbSpanPasses * 2
	if cap(ring.spans) < spanWords {
		ring.spans = make([]uint32, spanWords)
	}
	ring.spans = ring.spans[:spanWords]
	if spanWords > 0 {
		copy(ring.spans, unsafe.Slice((*uint32)(u.Spans), spanWords))
		r.uploadBytes[upCPU] += uint64(spanWords * 4)
	}
	ring.spriteCount, ring.groundSpriteCount = int(u.SpriteCount), int(u.GroundSpriteCount)
	selectorCount := 0
	for i := len(r.meshes); i < len(r.meshes)*gbSpanPasses; i++ {
		selectorCount += int(ring.spans[i*2+1])
	}
	r.copyIn(upSelectors, ring.selectors.Ptr, u.Selectors, selectorCount*4)
	ring.terrainFlags = u.TerrainFlags
	if !r.projectionPrepare(ring, (*NativeProjectionUpload)(u.Projections), u.InstanceCount, u.PoseCount, u.ProjectionFlagGeneration) {
		return 0
	}
	if !r.groupsPrepare(ring, (*nativeGroupUpload)(u.Groups)) || !r.facesPrepare(ring, int(u.InstanceCount)) || !r.faceReflectionsPrepare(ring) || !r.outlineRowsPrepare(ring) {
		return 0
	}
	if !r.waterObjectsPrepare(ring, (*nativeWaterObjectsUpload)(u.WaterObjects)) || !r.waterObjectsPrepareSources(ring, (*nativeWaterSourcesUpload)(u.WaterSources)) {
		return 0
	}
	un := r.uniforms
	un.camera = [4]float32{u.Camera[0], u.Camera[1], u.Camera[2], 0}
	un.timing = [4]float32{u.Alpha, float32(u.Seconds), float32(u.PoseCount), b2f(r.effects)}
	un.mode = [4]float32{1, float32(u.LightCount), 0, 0}
	un.lightControl = [4]float32{float32(u.RetainedLightCount), b2f(u.LightControls&1 != 0), b2f(u.LightControls&2 != 0), b2f(u.LightControls&4 != 0)}
	var aw, ah float32
	if r.modelAtlasColor != nil {
		aw, ah = float32(r.modelAtlasColor.Width), float32(r.modelAtlasColor.Height)
	}
	un.composition = [4]float32{aw, ah, 1, float32(u.CompositionSlotCount)}
	if u.LightControls&8 != 0 {
		un.composition[2] = 2
	}
	un.visual[0] = b2f(r.visualPasses)
	un.visual[3] = b2f(u.RulesCount != 0)
	un.visual[2] = b2f(u.FogWidth != 0 && u.FogHeight != 0)
	un.fogRect = u.FogRect
	r.lastUniforms, r.lastRing = un, ring
	r.pendingRing, r.pendingUniforms, r.pendingOverlay = ring, un, uploadOverlay
	r.pendingOverlayRect = [5]int{overlayX, overlayY, overlayW, overlayH, overlayRowBytes}
	r.uploadFrames++
	r.pendingRetainedLights = u.RetainedLightCount
	r.pendingWait = (acquired - start) * 1000
	r.pendingCopy = (mtl.MediaTime() - copyStart) * 1000
	r.livePending = true
	return 1
}

func (r *gbRenderer) livePresent() int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if !r.livePending {
		return 0
	}
	r.livePending = false
	ring, u, uploadOverlay := r.pendingRing, r.pendingUniforms, r.pendingOverlay
	r.pendingRing = nil
	rect := r.pendingOverlayRect
	acquired := mtl.MediaTime()
	var drawable mtl.ID
	if !r.offscreen {
		if drawable = r.nextDrawable(); drawable == 0 {
			return 0
		}
	}
	ready := mtl.MediaTime()
	rowIndex, generation := r.beginRow()
	row := &r.timingRows[rowIndex]
	row.Wait = r.pendingWait
	row.DrawableWait = (ready - acquired) * 1000
	cb := r.queue.CommandBuffer()
	timing := r.timing.begin(r.device, r.frameNumber)
	// GPU writes are ordered after earlier readers on this serial queue. Only
	// the acquired ring's staging bytes are modified; the atlas is never
	// rewritten while an earlier command buffer can sample it.
	if uploadOverlay {
		blit := r.blitEncoder(cb, "hud.atlasUpload")
		blit.CopyBufferToTexture(ring.overlayUpload, 0, rect[4], rect[4]*rect[3], rect[2], rect[3], ring.overlayAtlas, rect[0], rect[1])
		blit.End()
	}
	r.effectsSetLighting(ring, ring.retainedLights, r.pendingRetainedLights)
	r.effectsBegin(ring, cb)
	if !r.encode(cb, ring.poses, ring.interpolatedPoses, u, r.destination(drawable), ring) {
		return 0
	}
	row.Encode = r.pendingCopy + (mtl.MediaTime()-ready)*1000
	row.Submit = mtl.MediaTime()
	row.Memory = r.device.CurrentAllocatedSize()
	r.submit(cb, ring, drawable, rowIndex, generation, timing)
	return 1
}
