package client

import (
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/render"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// Retained model identities match committed publications, not reusable slots.
const (
	RetainedModelUnit uint8 = iota + 1
	RetainedModelFeature
	RetainedModelProjectile
	RetainedModelDebris
)

// These barriers describe body commits; shadow and strip commands have their
// own positions in the production list [03 R-RAST-01 §6–§7][03 §1].
const (
	RetainedModelsShortFeatures uint32 = iota
	RetainedModelsGround
	RetainedModelsEffects
	RetainedModelsAir
)

// RetainedModelOrder is a model/feature-sprite view of the production walk.
// Rank is monotonic across barriers; sprite features occupy the existing gaps.
// Group identifies a keyed parent staging image, including the presentation
// policy's factory occupants (DESIGN_PRESENTATION_CLIENT §5). Zero is separate.
type RetainedModelOrder struct {
	Kind, GroupKind            uint8
	ID, GroupID                uint64
	Rank, Phase                uint32
	Row                        int32
	FeatureIndex               uint32 // zero-based committed Features index; not a temporal identity
	Sprite                     bool   // feature uses native sprite references rather than a model slot
	FeatureBody, FeatureShadow bool
	GroupKeyDelta              int32 // child integer origin minus parent integer origin
	GroupReveal                bool  // group has a mergeable child carrying a production reveal
	Submerged                  bool  // production Enhanced seabed promotion, before ordinary feature passes
}

// RetainedModelOrderer owns presentation scratch, and may only be used by one
// presentation owner. Prepare does not record geometry, advance cursors, draw,
// or consume either presentation or simulation RNG [I6].
type RetainedModelOrderer struct {
	buckets worldBuckets
	order   []RetainedModelOrder
	rank    uint32
	tick    uint32
}

func NewRetainedModelOrderer() *RetainedModelOrderer { return &RetainedModelOrderer{} }

// Prepare returns storage borrowed until the next Prepare. The Enhanced
// order is selected explicitly, as RecordModernFrame does (§5.5), without
// changing the client's Classic/Modern record selection or its bucket scratch.
func (o *RetainedModelOrderer) Prepare(c *Client, cur *frame.Frame) []RetainedModelOrder {
	o.order, o.rank = o.order[:0], 0
	b := &o.buckets
	b.reset()
	if c == nil || cur == nil || c.cam == nil {
		return o.order
	}
	o.tick = cur.Tick
	win := c.worldWindow()
	for i := range cur.Features {
		f := &cur.Features[i]
		if f.Height >= 10 || !win.admitsCell(f.CX, f.CZ) || !featureVisibleForFrame(cur, *f) {
			continue
		}
		o.feature(c, *f, uint32(i), RetainedModelsShortFeatures, featureBucketRow(f.CZ, c.cam.Z))
	}
	b.fineUnitOrder = true
	b.indexChildren(cur.Units)
	for i := range cur.Units {
		u := &cur.Units[i]
		if !unitVisibleForFrame(cur, *u, cur.ViewingPlayer) {
			continue
		}
		row := unitBucketRow(u.Z, c.cam.Z)
		if row >= 0 && row < win.bucketRows {
			b.add(worldDrawable{row: row, unit: u, index: int32(i)})
		}
	}
	for i := range cur.Features {
		f := &cur.Features[i]
		if f.Height >= 10 && win.admitsCell(f.CX, f.CZ) && tallFeatureVisibleForFrame(cur, *f) {
			// This private bucket walk uses index for the feature ordinal too;
			// it never invokes production geometry's unit-index consumers.
			b.add(worldDrawable{row: featureBucketRow(f.CZ, c.cam.Z), feature: f, index: int32(i)})
		}
	}
	b.indexFactoryOccupants(win)
	ordered := b.ordered()
	for _, d := range ordered {
		if !win.admitsPassARow(d.row) {
			continue
		}
		if d.unit != nil {
			if d.unit.MoverMode == moverModeGrounded {
				o.unit(c, d, RetainedModelsGround)
			}
		} else if d.feature != nil {
			o.feature(c, *d.feature, uint32(d.index), RetainedModelsGround, d.row)
		}
	}
	if !c.strategicView() {
		o.effects(c, cur)
	}
	for _, d := range ordered {
		if d.unit != nil && d.unit.MoverMode != moverModeGrounded {
			o.unit(c, d, RetainedModelsAir)
		}
	}
	return o.order
}

func (o *RetainedModelOrderer) feature(c *Client, f frame.FeatureView, index, phase uint32, row int32) {
	o.rank++
	if f.Filename != "" || (f.Model == "" && f.SeqName != "") {
		// Match drawFeature's runtime gates without resolving/advancing its
		// shared animated rest cursor a second time [03 R-RAST-01 §6][I6].
		cursor := !f.RuntimeLive || f.EventSeqName != ""
		bodyName, shadowName := f.SeqName, f.SeqNameShad
		if f.EventSeqName != "" {
			bodyName, shadowName = f.EventSeqName, f.EventSeqNameShad
		}
		body := cursor && f.Filename != "" && bodyName != ""
		shadow := cursor && c.featureShadows && (!f.RuntimeLive || f.ShadowEnabled) && f.Filename != "" && shadowName != ""
		submerged := c.enhanced && c.effects.SeabedTreated() && !c.strategicView() &&
			c.terrain != nil && !c.terrain.LavaWorld && c.terrain.SeaLevel != 0 &&
			!f.Blocking && !f.RuntimeLive && !f.IsBurning && f.Height < 10 &&
			f.Y >= 0 && int64(f.Y)+int64(max(f.Height, 0))*65536 < int64(c.terrain.SeaLevelWorld())
		o.order = append(o.order, RetainedModelOrder{Kind: RetainedModelFeature, ID: f.InstanceID, FeatureIndex: index, Rank: o.rank, Phase: phase, Row: row, Sprite: true, FeatureBody: body, FeatureShadow: shadow, Submerged: submerged})
		return
	}
	if m := c.modelForFeature(f); m == nil || m.compiled == nil {
		return
	}
	o.order = append(o.order, RetainedModelOrder{Kind: RetainedModelFeature, ID: f.InstanceID, FeatureIndex: index, Rank: o.rank, Phase: phase, Row: row})
}

func (o *RetainedModelOrderer) unit(c *Client, d worldDrawable, phase uint32) {
	o.rank++
	u := *d.unit
	if c.strategicView() || isCarried(u) || o.buckets.isFactoryOccupant(d.index) {
		return
	}
	m := c.modelForUnit(u)
	keyed := m != nil && m.compiled != nil && (u.ZBuffer || u.BuildRemaining > 0 || u.Digger)
	group := uint64(0)
	if keyed {
		group = u.InstanceID
	}
	groupReveal := false
	if group != 0 {
		for i := o.buckets.firstChild(u.Slot); i >= 0; i = o.buckets.nextChild(i) {
			if i >= len(o.buckets.units) {
				break
			}
			child := o.buckets.units[i]
			if model := c.modelForUnit(child); model != nil && model.compiled != nil && c.RetainedUnitRules(child, o.tick).Revealed {
				groupReveal = true
				break
			}
		}
	}
	if m != nil && m.compiled != nil {
		o.appendUnit(u.InstanceID, group, phase, d.row, 0, groupReveal)
	}
	// Use the production's head-first cargo list followed by its admitted yard
	// occupants. Children ride parent admission; their own row/visibility does
	// not run a second gate [03 R-RAST-01 §7][04 R-UNIT-06 §3].
	for i := o.buckets.firstChild(u.Slot); i >= 0; i = o.buckets.nextChild(i) {
		if i >= len(o.buckets.units) {
			break
		}
		child := o.buckets.units[i]
		m := c.modelForUnit(child)
		if m == nil || m.compiled == nil {
			continue
		}
		o.rank++
		delta := int32((int64(child.Y) >> 16) - (int64(u.Y) >> 16))
		o.appendUnit(child.InstanceID, group, phase, d.row, delta, groupReveal)
	}
}

func (o *RetainedModelOrderer) appendUnit(id, group uint64, phase uint32, row int32, delta int32, reveal bool) {
	entry := RetainedModelOrder{Kind: RetainedModelUnit, ID: id, GroupID: group, Rank: o.rank, Phase: phase, Row: row, GroupKeyDelta: delta, GroupReveal: reveal}
	if group != 0 {
		entry.GroupKind = RetainedModelUnit
	}
	o.order = append(o.order, entry)
}

func (o *RetainedModelOrderer) effects(c *Client, cur *frame.Frame) {
	visible := projectileVisible(cur.Visibility, cur.ViewingPlayer)
	opts := c.projectileDispatchOptions()
	for _, p := range cur.Projectiles {
		if p.BurstRemaining != 0 || !visible(p) {
			continue
		}
		// The lens can abort the pool walk before a later model is reached.
		if p.RenderType == render.RenderTypeGlobalGAF && !c.admitProjectileLens(p) {
			break
		}
		o.rank++
		switch p.RenderType {
		case render.RenderTypeBaseSpriteModel, render.RenderTypeBaseModelDistinct, render.RenderTypeRecordOrientation:
			// Dispatch only model-bearing families. Dispatching segmented
			// families here would consume the presentation CRT a second time.
			d := render.DispatchProjectileView(p, cur.Tick, opts)
			if !d.Suppressed && d.BaseFrame != nil && c.modelForProjectile(p) != nil {
				o.order = append(o.order, RetainedModelOrder{Kind: RetainedModelProjectile, ID: p.PresentationID, Rank: o.rank, Phase: RetainedModelsEffects, Row: -1})
			}
		}
	}
	// Whole-piece debris precedes the fixed-effect category walks [03 §1].
	for _, d := range cur.Debris {
		o.rank++
		m := c.modelForDebris(d)
		if m == nil || m.compiled == nil || d.PieceIndex < 0 || d.PieceIndex >= len(m.compiled.Pieces) {
			continue
		}
		// The standalone entry's origin is exactly its committed world point
		// [03 R-COMP-02 §6]; the direct gate needs no transformed geometry.
		draw := render.UnitDraw{WorldPos: [3]numeric.Fixed{d.X, d.Y, d.Z}}
		if c.directModelOriginVisible(&draw) {
			o.order = append(o.order, RetainedModelOrder{Kind: RetainedModelDebris, ID: uint64(d.Slot), Rank: o.rank, Phase: RetainedModelsEffects, Row: -1})
		}
	}
	// TODO(question): export model fragments and named fixed-effect models
	// when retained topology supports them; their production category walks in
	// DrawEffectViews settle the missing positions. No fabricated model ranks.
}
