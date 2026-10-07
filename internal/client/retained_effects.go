package client

import "github.com/nanolathe-gg/nanolathe/internal/drawlist"

// RetainedEffectBatch owns its commands and mutable geometry. ProjectileModels
// identifies the projectile pool's batch: a retained world producer may already
// supply those bodies, while the other batches' models are debris/fragments.
type RetainedEffectBatch struct {
	List             drawlist.List
	ProjectileModels bool
}

// RetainedEffects preserves the model/feature barriers of drawCommittedWorld.
// AfterGround contains, in order, strips 5/6, the projectile pool, then whole
// debris, fixed effects and strip 7. Labels belong to the foreground recorder;
// AfterLabels follows them [03 §1][DESIGN_GPU_RENDERER C-G3].
type RetainedEffects struct {
	BeforeFeatures, BeforeGround, AfterGround, AfterAir []RetainedEffectBatch
	AfterLabels                                         []RetainedEffectBatch
	Effects                                             EffectDrawStats
	Strips                                              StripDrawStats
	LightSources                                        RetainedLightSources
}

// RecordRetainedEffects records the existing stock-effect producers once for
// the currently pinned presentation. Call after BeginPresentationFrame and
// before RecordRetainedForeground. This replaces the effect portion of a world
// recording; do not also RecordModernFrame for that same presentation, because
// segmented shots and debris emission consume the private presentation CRT.
// It never advances the session or records unit/feature model bodies. Existing
// building-foam admission and active wreck extent measurement retain their
// canonical pose walks, without building the world's model packets [I4, I6].
func (c *Client) RecordRetainedEffects() RetainedEffects {
	var out RetainedEffects
	if c == nil || c.cam == nil || len(c.indexed) != c.width*c.height {
		return out
	}
	committed := c.committedFrame()
	if committed == nil || c.screenOnly(committed) {
		return out
	}
	cur := c.presentationFrame()
	if cur == nil {
		return out
	}
	if cur != committed && c.beginCameraBlend() {
		defer c.endCameraBlend(true)
	}
	wasGeometry, wasOnly, wasParallel := c.recordModelGeometry, c.geometryOnlyModels, c.parallelRecord
	c.recordModelGeometry, c.geometryOnlyModels, c.parallelRecord = true, true, true
	defer func() {
		c.recordModelGeometry, c.geometryOnlyModels, c.parallelRecord = wasGeometry, wasOnly, wasParallel
	}()
	c.modelScratch.reset()
	c.modelScratch.active = true
	c.modelFrameSerial++
	defer func() { c.modelScratch.active = false }()
	c.pointArena = c.pointArena[:0]
	c.surfaceArena = c.surfaceArena[:0]
	c.effectStats, c.stripStats = EffectDrawStats{}, StripDrawStats{}
	c.frameTick = cur.Tick
	c.refreshRecordExtent()
	// The existing store sweeps before this frame's debris can emit containers.
	// Neither stepping nor creation is repeated by native replay [04 R-COB-04 §2].
	c.stepDebrisTrails(cur)
	record := func(projectiles bool, draw func()) RetainedEffectBatch {
		c.list.Reset()
		c.emitWorldBegin()
		draw()
		c.emitWorldEnd()
		return RetainedEffectBatch{List: c.list.Clone(), ProjectileModels: projectiles}
	}
	out.BeforeFeatures = []RetainedEffectBatch{record(false, func() {
		c.drawTerrainPrep()
		if c.effects.Scorch {
			c.drawScorchMarks(committed)
		}
		if c.effects.HovercraftLandWash && !c.strategicView() {
			if !c.observesInOrder() {
				c.placeSurfaceWakes(committed)
			}
			c.drawSurfaceWakes()
		}
		if c.effects.WaterFoam && !c.strategicView() {
			c.drawBuildingFoam(committed)
		}
		if c.trailStrength > 0 && !c.strategicView() {
			if !c.observesInOrder() {
				c.placeTrails(committed)
			}
			c.drawTrails()
		}
		c.drawStripSlot(cur, 0)
		c.drawStripSlot(cur, 1)
		c.drawStripSlot(cur, 2)
	})}
	out.BeforeGround = []RetainedEffectBatch{record(false, func() {
		c.drawStripSlot(cur, 3)
		c.drawStripSlot(cur, 4)
	})}
	out.AfterGround = []RetainedEffectBatch{
		record(false, func() { c.drawStripSlot(cur, 5); c.drawEffects(cur) }),
		record(true, func() { c.drawProjectiles(cur) }),
		record(false, func() { c.drawFixedEffects(cur); c.drawStripSlot(cur, 7) }),
	}
	out.AfterAir = []RetainedEffectBatch{record(false, func() {
		c.drawStripSlot(cur, 8)
	})}
	out.AfterLabels = []RetainedEffectBatch{record(false, func() {
		c.drawStripSlot(cur, 9)
		c.drawDebrisTrails(cur)
	})}
	out.Effects, out.Strips = c.effectStats, c.stripStats
	out.LightSources = c.RecordRetainedLightSources()
	return out
}
