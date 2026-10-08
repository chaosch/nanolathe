package client

import (
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

// RetainedEffectBatch owns its commands and mutable geometry. ProjectileModels
// identifies the projectile pool's batch: a retained world producer may already
// supply those bodies, while the other batches' models are debris/fragments.
type RetainedEffectBatch struct {
	List             drawlist.List
	ProjectileModels bool
	ProjectileOrder  []RetainedProjectileModel // aligned to the successful projectile Model commands
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
	return c.RecordRetainedEffectsInto(nil)
}

// RetainedEffectStorage holds one presenter's batch copies between calls. A
// recording made into it is the same deep copy RecordRetainedEffects returns,
// but it reuses the previous recording's storage, so it stays valid only until
// the next RecordRetainedEffectsInto with the same storage [I6].
type RetainedEffectStorage struct {
	lists         [7]drawlist.List
	orders        [7][]RetainedProjectileModel
	batches       [7]RetainedEffectBatch
	short, ground drawlist.List
	art, leaves   []drawlist.Sprite
	wrecks        []drawlist.WreckSource
	// Milliseconds per batch, then light sources, of the last recording (diagnostic).
	Timings [8]float64
}

// cloneRetained deep-copies the recording list, into storage when supplied.
func (c *Client) cloneRetained(storage *RetainedEffectStorage, slot func(*RetainedEffectStorage) *drawlist.List) drawlist.List {
	if storage == nil {
		return c.list.Clone()
	}
	dst := slot(storage)
	c.list.CloneInto(dst)
	return *dst
}

// RecordRetainedEffectsInto is RecordRetainedEffects with reusable storage; a
// nil storage allocates fresh copies. Recording order and every producer call
// are identical either way.
func (c *Client) RecordRetainedEffectsInto(storage *RetainedEffectStorage) RetainedEffects {
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
	recorded := 0
	record := func(projectiles bool, draw func()) RetainedEffectBatch {
		k := recorded
		recorded++
		if storage != nil {
			start := time.Now()
			defer func() { storage.Timings[k] = float64(time.Since(start)) / 1e6 }()
		}
		// The projectile order is appended through a pointer the client holds
		// while drawing; with storage it points into storage, so the batch
		// stays on the stack.
		var order *[]RetainedProjectileModel
		if storage != nil {
			order = &storage.orders[k]
			*order = (*order)[:0]
		} else {
			order = new([]RetainedProjectileModel)
		}
		previousOrder := c.retainedProjectileOrder
		c.retainedProjectileOrder = nil
		if projectiles {
			c.retainedProjectileOrder = order
		}
		defer func() { c.retainedProjectileOrder = previousOrder }()
		c.list.Reset()
		c.emitWorldBegin()
		draw()
		c.emitWorldEnd()
		batch := RetainedEffectBatch{ProjectileModels: projectiles, ProjectileOrder: *order}
		batch.List = c.cloneRetained(storage, func(s *RetainedEffectStorage) *drawlist.List { return &s.lists[k] })
		return batch
	}
	// Batches keep record order; with storage their slices are reused too.
	batches := func(n int) []RetainedEffectBatch {
		if storage == nil {
			return make([]RetainedEffectBatch, n)
		}
		return storage.batches[recorded : recorded+n : recorded+n]
	}
	out.BeforeFeatures = batches(1)
	out.BeforeFeatures[0] = record(false, func() {
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
	})
	out.BeforeGround = batches(1)
	out.BeforeGround[0] = record(false, func() {
		c.drawStripSlot(cur, 3)
		c.drawStripSlot(cur, 4)
	})
	out.AfterGround = batches(3)
	out.AfterGround[0] = record(false, func() { c.drawStripSlot(cur, 5); c.drawEffects(cur) })
	out.AfterGround[1] = record(true, func() { c.drawProjectiles(cur) })
	out.AfterGround[2] = record(false, func() { c.drawFixedEffects(cur); c.drawStripSlot(cur, 7) })
	out.AfterAir = batches(1)
	out.AfterAir[0] = record(false, func() {
		c.drawStripSlot(cur, 8)
	})
	out.AfterLabels = batches(1)
	out.AfterLabels[0] = record(false, func() {
		c.drawStripSlot(cur, 9)
		c.drawDebrisTrails(cur)
	})
	out.Effects, out.Strips = c.effectStats, c.stripStats
	lights := time.Now()
	out.LightSources = c.recordRetainedLightSources(storage)
	if storage != nil {
		storage.Timings[7] = float64(time.Since(lights)) / 1e6
	}
	return out
}
