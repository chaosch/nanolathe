package meshscene

import (
	"maps"
	"time"

	"fmt"
	"math"
	"sort"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// BattleSprites owns load-time immutable art and worker-owned presentation
// scratch. Append and Report must run on the session worker; Texture may be
// borrowed by the renderer for its one-time upload [I6].
type BattleSprites struct {
	// Splits times the last Append: setup, features, effects+strips, projectiles, wreck lights, totals (ms, diagnostic).
	Splits [6]float64
	// hot holds per-feature counters as integers; Append merges nonzero ones
	// into counts so reports keep the same keys and values.
	hot                                                  [hotSpriteCounters]int
	spriteSources                                        []SpriteSource
	spriteSourceTick                                     uint32
	effects                                              drawlist.Effects
	previousFeatures                                     map[uint64][4]float32
	spectator                                            bool
	featureModelBounds                                   map[string][4]float32
	modelKeys                                            battleModelKeys
	texture                                              Texture
	regions                                              []atlasRegion
	sequences                                            map[string]*spriteSequence
	sequenceLookups                                      map[spriteSequenceName]*spriteSequence
	missing                                              map[string]string
	counts                                               map[string]int
	totals                                               map[string]int
	previousEffects, previousProjectiles, previousBeams  map[uint64][4]float32
	colors                                               [256][4]float32
	logical                                              [256]byte
	banksLoaded, framesLoaded, alternateChildrenPrepared int
	anchorHeights                                        []int16
	anchorW, anchorH                                     int
	bankCensus                                           []spriteBankCensus
	preparedFeatureNames, unresolvedFeatureNames         []string
	featureScopeFiltered                                 bool
	rawSpritePixels, croppedSpritePixels                 int64
	fullyTransparentFrames                               int
	detailFrames, detailOmitted                          int
	detailPixels                                         int64
	detailTexture                                        Texture
	detailRegions                                        []atlasRegion
	// emitted counts what culling decides (accepted kinds, culled lights,
	// ground sprites); Append merges it into counts.
	emitted map[string]int
	walk    spriteWalk
	// artReach bounds every prepared frame's extent from its anchor, in world
	// units, and walkSlack is the build's model culling slack; a feature
	// anchored farther than both outside the view cannot appear in it or in
	// any camera RetainedBattle.Covers accepts.
	artReach, walkSlack float32
}

// spriteWalk keeps the last full walk's feature, effect and strip candidates,
// resolved but not yet culled, with the census that culling does not affect,
// so Reselect can cull them for another camera without resolving them again.
type spriteWalk struct {
	previous, current *frame.Frame
	valid, recording  bool
	features, effects []spriteWalkOp
	at                *[]spriteWalkOp
	counts            map[string]int
	hot               [hotSpriteCounters]int
}

// spriteWalkOp is one appendSprite or appendLight call of a walk. A feature
// sprite also names its source and whether it is a short, ground sprite.
type spriteWalkOp struct {
	sprite           Sprite
	kind             string
	source           SpriteSource
	light            bool
	feature, ground  bool
	p                [4]float32
	radius, strength float32
	hue              [3]float32
}

func (b *BattleSprites) Texture() Texture { return b.texture }

// DetailTexture is the 2x feature variant atlas; empty when there is none.
func (b *BattleSprites) DetailTexture() Texture { return b.detailTexture }

// SetWalkSlack sets the next Append's culling slack (RetainedBattle.Frame).
func (b *BattleSprites) SetWalkSlack(slack float32) { b.walkSlack = slack }

// Both settings shape the recorded walk, so a change discards it.
func (b *BattleSprites) SetSpectator(on bool) {
	if b.spectator != on {
		b.spectator, b.walk.valid = on, false
	}
}
func (b *BattleSprites) SetSourceEffects(e drawlist.Effects) {
	if b.effects != e {
		b.effects, b.walk.valid = e, false
	}
}

// SetFeatureModelBounds copies prepared projected model bounds, relative to the
// world anchor, before the session worker starts (GPU design §28).
func (b *BattleSprites) SetFeatureModelBounds(bounds map[string][4]float32) {
	b.featureModelBounds = make(map[string][4]float32, len(bounds))
	for key, rect := range bounds {
		b.featureModelBounds[battleModelKey(key)] = rect
	}
}

// SetTerrain copies only the immutable height bytes needed by sprite feature
// anchors. Call before the worker starts; no authoritative pointer survives.
// The integer mean is halved by projection [03 §5.1.4].
func (b *BattleSprites) SetTerrain(t *world.Terrain) {
	b.anchorW, b.anchorH, b.anchorHeights = 0, 0, nil
	if t == nil || t.CellW <= 0 || t.CellH <= 0 || len(t.Plot) < int(t.CellW)*int(t.CellH) {
		return
	}
	b.anchorW, b.anchorH = int(t.CellW), int(t.CellH)
	b.anchorHeights = make([]int16, b.anchorW*b.anchorH)
	for i := range b.anchorHeights {
		b.anchorHeights[i] = -1
	}
	for z := 0; z+1 < b.anchorH; z++ {
		for x := 0; x+1 < b.anchorW; x++ {
			i := z*b.anchorW + x
			sum := int(t.Plot[i].Height()) + int(t.Plot[i+1].Height()) + int(t.Plot[i+b.anchorW].Height()) + int(t.Plot[i+b.anchorW+1].Height())
			// Store twice the integer shear so the native float projection
			// preserves sum>>3, including odd means [03 §5.1.4].
			b.anchorHeights[i] = int16((sum >> 3) * 2)
		}
	}
}

func spritePosition(x, y, z numeric.Fixed) [4]float32 {
	return [4]float32{float32(x) / 65536, float32(y) / 65536, float32(z) / 65536, 0}
}

func spritePrevious(m map[uint64][4]float32, id uint64, p [4]float32) [4]float32 {
	if id != 0 {
		if old, ok := m[id]; ok {
			return old
		}
	}
	return p
}

func (b *BattleSprites) art(bank, name string, index int32, clamp bool) (spriteArt, bool) {
	if bank == "" {
		bank = content.DefaultEffectBank
	}
	seq := b.sequence(bank, name)
	if seq == nil || len(seq.frames) == 0 {
		if name != "" {
			b.missing[content.EffectBankPath(content.CanonicalKey(bank))+"#"+content.CanonicalKey(name)] = "sequence unavailable in prepared atlas"
		}
		return spriteArt{}, false
	}
	if clamp {
		index = max(0, min(index, int32(len(seq.frames)-1)))
	}
	if index < 0 || int(index) >= len(seq.frames) {
		b.missing[fmt.Sprintf("%s#%s[%d]", content.EffectBankPath(content.CanonicalKey(bank)), content.CanonicalKey(name), index)] = "published frame outside prepared authored sequence"
		return spriteArt{}, false
	}
	a := seq.frames[index]
	return a, a.width > 0 && a.height > 0
}

func (b *BattleSprites) eventArt(bank, name string, visit int32) (spriteArt, bool) {
	seq := b.sequence(bank, name)
	if seq == nil || len(seq.frames) == 0 {
		return b.art(bank, name, 0, false)
	}
	visit = max(visit, 0)
	var elapsed int64
	for i, a := range seq.frames {
		elapsed += int64(max(a.hold, 1))
		if int64(visit) < elapsed || i == len(seq.frames)-1 {
			return a, a.width > 0 && a.height > 0
		}
	}
	return spriteArt{}, false
}

func spriteVisible(s Sprite, camera [3]float32, viewport [2]int) bool {
	zoom := camera[2]
	if zoom <= 0 || viewport[0] <= 0 || viewport[1] <= 0 {
		return false
	}
	left, top, right, bottom := s.Rect[0], s.Rect[1], s.Rect[0]+s.Rect[2], s.Rect[1]+s.Rect[3]
	if s.Params[2] != 0 {
		// A conservative circle retains every rotated corner.
		r := float32(math.Hypot(float64(max(absSprite(left), absSprite(right))), float64(max(absSprite(top), absSprite(bottom)))))
		left, top, right, bottom = -r, -r, r, r
	}
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := -minX, -minY
	for _, p := range [2][4]float32{s.Previous, s.Current} {
		x := (p[0]-camera[0])*zoom + float32(viewport[0])/2
		y := (p[2]-p[1]*0.5-camera[1])*zoom + float32(viewport[1])/2
		minX, minY = min(minX, x+left*zoom), min(minY, y+top*zoom)
		maxX, maxY = max(maxX, x+right*zoom), max(maxY, y+bottom*zoom)
	}
	return maxX > 0 && maxY > 0 && minX < float32(viewport[0]) && minY < float32(viewport[1])
}

func absSprite(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

const (
	hotFeaturesSeen = iota
	hotFeaturesRejected
	hotCulledSprites
	hotFeatureALP
	hotMissingFeatureFrames
	hotFeaturesOutsideView
	hotDetailSprites
	hotSpriteCounters
)

var hotSpriteCounterNames = [hotSpriteCounters]string{"features_seen", "features_visibility_rejected", "culled_sprites", "feature_alp_approximations", "missing_feature_frames", "features_outside_view", "detail_sprites"}

func (b *BattleSprites) appendSprite(dst []Sprite, a spriteArt, previous, current [4]float32, color [4]float32, emission float32, camera [3]float32, viewport [2]int, kind string) []Sprite {
	r, atlas := b.regions[a.cell], &b.texture
	rect := [4]float32{-float32(a.xoff), -float32(a.yoff), float32(a.width), float32(a.height)}
	// A 2x variant covers the authored frame's world rectangle with twice the
	// texels; it is drawn wherever the camera magnifies the art. Previous.w
	// selects the detail atlas.
	detail := a.detail.cell != 0 && DetailScale(camera[2])
	if detail {
		d := a.detail
		r, atlas = b.detailRegions[d.cell], &b.detailTexture
		rect = [4]float32{-float32(d.xoff) / 2, -float32(d.yoff) / 2, float32(d.width) / 2, float32(d.height) / 2}
		b.hot[hotDetailSprites]++
	}
	u0, u1 := r.uv([2]float32{}, atlas.Width, atlas.Height), r.uv([2]float32{1, 1}, atlas.Width, atlas.Height)
	s := Sprite{Previous: previous, Current: current, Rect: rect, UV: [4]float32{u0[0], u0[1], u1[0], u1[1]}, Color: color, Params: [4]float32{emission, 0, 0, 0}}
	if detail {
		s.Previous[3] = 1
	}
	if b.walk.recording {
		*b.walk.at = append(*b.walk.at, spriteWalkOp{sprite: s, kind: kind})
	}
	return b.cullSprite(dst, s, camera, viewport, kind)
}

func (b *BattleSprites) cullSprite(dst []Sprite, s Sprite, camera [3]float32, viewport [2]int, kind string) []Sprite {
	if !spriteVisible(s, camera, viewport) {
		b.hot[hotCulledSprites]++
		return dst
	}
	b.emitted[kind]++
	return append(dst, s)
}

func (b *BattleSprites) appendLight(dst []Light, p [4]float32, radius, strength float32, hue [3]float32, camera [3]float32, viewport [2]int) []Light {
	if radius <= 0 || strength <= 0 {
		return dst
	}
	if b.walk.recording {
		*b.walk.at = append(*b.walk.at, spriteWalkOp{light: true, p: p, radius: radius, strength: strength, hue: hue})
	}
	b.emitted["light_sources"]++
	probe := Sprite{Previous: p, Current: p, Rect: [4]float32{-radius, -radius, radius * 2, radius * 2}}
	if !spriteVisible(probe, camera, viewport) {
		b.emitted["culled_lights"]++
		return dst
	}

	if hue == [3]float32{} {
		hue = [3]float32{1, 1, 1}
	}
	light := Light{PositionRadius: [4]float32{p[0], p[1], p[2], radius}, ColorStrength: [4]float32{hue[0], hue[1], hue[2], strength}}
	if len(dst) < 32 {
		return append(dst, light)
	}
	b.emitted["lights_over_limit"]++
	// Prototype's 32 slots retain the strongest, with source-order ties (§23).
	at := 0
	for i := 1; i < len(dst); i++ {
		if battleLightPower(dst[i]) <= battleLightPower(dst[at]) {
			at = i
		}
	}
	if battleLightPower(light) > battleLightPower(dst[at]) {
		copy(dst[at:], dst[at+1:])
		dst[len(dst)-1] = light
	}
	return dst
}

// Append consumes committed copies only. Effect/projectile identities preserve
// movement interpolation; strip records have no published stable identity and
// snap to their committed point instead of inventing a matching key [I6].
func (b *BattleSprites) Append(dst []Sprite, lights []Light, previous, current *frame.Frame, camera [3]float32, viewport [2]int) ([]Sprite, []Light) {
	b.Splits = [6]float64{}
	splitMark := time.Now()
	split := func(i int) { now := time.Now(); b.Splits[i] = float64(now.Sub(splitMark)) / 1e6; splitMark = now }
	defer split(5)
	clear(b.counts)
	if b.emitted == nil {
		b.emitted = make(map[string]int)
	}
	clear(b.emitted)
	b.hot = [hotSpriteCounters]int{}
	// Metadata follows the original output indices, including a caller's
	// unclassified prefix. Exact uint64 identities stay outside the GPU ABI.
	b.spriteSources = resizeComposition(b.spriteSources, len(dst))
	clear(b.spriteSources)
	b.spriteSourceTick = 0
	b.walk.valid = false
	if current == nil {
		return dst, lights
	}
	b.spriteSourceTick = current.Tick
	// Record the feature, effect and strip candidates for Reselect.
	w := &b.walk
	w.previous, w.current, w.recording = previous, current, true
	w.features, w.effects = w.features[:0], w.effects[:0]
	w.at = &w.features
	clear(b.previousFeatures)
	clear(b.previousEffects)
	clear(b.previousProjectiles)
	clear(b.previousBeams)
	if previous != nil {
		for _, v := range previous.Features {
			if v.InstanceID != 0 && v.Model != "" {
				b.previousFeatures[v.InstanceID] = spritePosition(v.X, v.Y, v.Z)
			}
		}
		for _, v := range previous.Effects {
			if v.PresentationID != 0 {
				b.previousEffects[v.PresentationID] = spritePosition(v.X, v.Y, v.Z)
			}
		}
		for _, v := range previous.Projectiles {
			if v.PresentationID != 0 {
				b.previousProjectiles[v.PresentationID] = spritePosition(v.X, v.Y, v.Z)
				b.previousBeams[v.PresentationID] = spriteBeamCenter(v)
			}
		}
	}
	split(0)
	white := spriteArt{cell: 0, width: 2, height: 2}
	// Features anchored outside the view by more than any frame's reach are
	// skipped before their art is resolved; the walk then records only
	// candidates a covered camera can see. Burning features keep their light,
	// whose radius is not bounded by the reach.
	zoom := camera[2]
	reachX := float32(viewport[0])/(2*zoom) + b.walkSlack + b.artReach + 1
	reachY := float32(viewport[1])/(2*zoom) + b.walkSlack + b.artReach + 1
	for fi := range current.Features {
		v := &current.Features[fi]
		b.hot[hotFeaturesSeen]++
		if !v.IsBurning && zoom > 0 {
			p := b.featureAnchor(v)
			if dx, dy := p[0]-camera[0], p[2]-p[1]*0.5-camera[1]; dx < -reachX || dx > reachX || dy < -reachY || dy > reachY {
				b.hot[hotFeaturesOutsideView]++
				continue
			}
		}
		if !battleFeatureVisible(current, *v, b.spectator) {
			b.hot[hotFeaturesRejected]++
			continue
		}
		if v.Model != "" && v.Filename == "" {
			b.counts["model_features_delegated"]++
			continue
		}
		if v.Filename == "" && v.SeqName == "" {
			b.counts["features_without_authored_art"]++
			continue
		}
		if v.RuntimeLive && v.EventSeqName == "" {
			b.counts["feature_no_live_cursor"]++
			continue
		}
		p := b.featureAnchor(v)
		if v.FootX > 0 && v.FootZ > 0 && !b.anchored(v) {
			// TODO(question): supply immutable terrain when loading this adapter;
			// incomplete terrain keeps the production snapshot-only fallback.
			b.counts["feature_anchor_fallback"]++
		}
		for _, shadow := range [2]bool{true, false} {
			if shadow && v.RuntimeLive && !v.ShadowEnabled {
				continue
			}
			name, trans := v.SeqName, v.AnimTrans
			if shadow {
				name, trans = v.SeqNameShad, v.ShadTrans
			}
			var a spriteArt
			var ok bool
			if v.EventSeqName != "" {
				name = v.EventSeqName
				if shadow {
					name = v.EventSeqNameShad
				}
				if name != "" {
					a, ok = b.eventArt(v.Filename, name, v.EventSeqVisit)
				}
			} else {
				if name != "" {
					a, ok = b.restArt(v.Filename, name, current.Tick, v.Animating)
				}
			}
			if name == "" {
				continue
			}
			if !ok {
				b.hot[hotMissingFeatureFrames]++
				continue
			}
			color := [4]float32{1, 1, 1, 1}
			// TODO(question): implement destination-palette ALP in the native host;
			// conventional half-alpha is an explicit modern display approximation.
			if trans && !v.RuntimeLive {
				color[3] = 0.5
				b.hot[hotFeatureALP]++
			}
			kind := "feature_bodies"
			if shadow {
				kind = "feature_shadows"
			}
			emission := float32(0)
			if !shadow && v.IsBurning && battlePointVisible(current, v.X, v.Y, v.Z, b.spectator) {
				emission = 1
				lights = b.appendArtLight(lights, a, p, b.sequenceExtent(v.Filename, name), "fire", current.Tick, camera, viewport)
			}
			start := len(dst)
			dst = b.appendSprite(dst, a, p, p, color, emission, camera, viewport, kind)
			sourceKind := SpriteSourceFeatureBody
			if shadow {
				sourceKind = SpriteSourceFeatureShadow
			}
			source := SpriteSource{Kind: sourceKind, FeatureID: v.InstanceID, FeatureIndex: uint32(fi), Transparent: trans && !v.RuntimeLive}
			if w.recording {
				op := &w.features[len(w.features)-1]
				op.feature, op.source, op.ground = true, source, v.Height < 10
			}
			b.featureSprite(dst, start, source, v.Height < 10)
		}
	}
	featureEnd := len(dst)
	w.at = &w.effects
	split(1)
	for _, v := range current.Effects {
		b.counts["effects_seen"]++
		if !battlePointVisible(current, v.X, v.Y, v.Z, b.spectator) {
			b.counts["effects_visibility_rejected"]++
			continue
		}
		p := spritePosition(v.X, v.Y, v.Z)
		old := spritePrevious(b.previousEffects, v.PresentationID, p)
		a, ok := b.art(v.AssetID, v.Graphic, v.SeqA, true)
		if v.Light || v.ActiveB && v.HasCalculatedFlash {
			b.counts["palette_flash_discs_omitted"]++
		}
		if ok && b.explosionVisible(v, current) {
			lights = b.appendArtLight(lights, a, p, b.sequenceExtent(v.AssetID, v.Graphic), "explosion", current.Tick, camera, viewport)
		}

		if v.FragmentSlot != 0 || v.HasModel {
			b.counts["model_effects_omitted"]++
			continue
		}
		if v.Kind == frame.EventKindNanolathe.String() && v.Strip == 6 {
			if v.NanolatheGeometryKnown {
				b.counts["nano_emitters_using_published_particles"]++
			} else {
				b.counts["unresolved_nano_emitters"]++
			}
			continue
		}
		if v.StripFill != 0 {
			dst = b.appendSprite(dst, white, old, p, b.colors[v.StripFill], 1, camera, viewport, "effect_fills")
			continue
		}
		if !v.ActiveA || v.Graphic == "" {
			continue
		}
		if !ok {
			b.counts["missing_effect_frames"]++
			continue
		}
		emission := float32(1)
		if v.Kind == frame.KindSmokeStart.String() {
			emission = 0
			b.counts["smoke_receivers"]++
		}
		dst = b.appendSprite(dst, a, old, p, [4]float32{1, 1, 1, 1}, emission, camera, viewport, "effect_sprites")
	}
	for _, v := range current.Strips {
		b.counts["strip_particles_seen"]++
		if v.Family != frame.StripFamilyVentSteam && !battlePointVisible(current, v.X, v.Y, v.Z, b.spectator) {
			b.counts["strips_visibility_rejected"]++
			continue
		}
		p := spritePosition(v.X, v.Y, v.Z)
		switch v.Family {
		case frame.StripFamilySprinkle, frame.StripFamilyNano:
			if v.Fill != 0 {
				emission := float32(0)
				if v.Family == frame.StripFamilyNano && v.Y >= current.Visibility.SeaLevel {
					emission = 0.16
				}
				dst = b.appendSprite(dst, white, p, p, b.colors[v.Fill], emission, camera, viewport, "strip_fills")
			} else {
				b.counts["strip_fill_missing"]++
			}
		case frame.StripFamilySmokePuff, frame.StripFamilyVentSteam, frame.StripFamilyFlame, frame.StripFamilyFlameTrail:
			a, ok := b.art(v.Bank, v.Entry, v.Frame, true)
			if !ok {
				b.counts["missing_strip_frames"]++
				continue
			}
			// Strip blits take the destination ALP path [03 R-FX-02 §2].
			emission := float32(0)
			if v.Family == frame.StripFamilyFlame || v.Family == frame.StripFamilyFlameTrail {
				emission = 0.5
				kind := "fire"
				if v.Family == frame.StripFamilyFlameTrail {
					kind = "spark"
				}
				lights = b.appendArtLight(lights, a, p, float32(max(a.identity.Width, a.identity.Height)), kind, current.Tick, camera, viewport)
			} else {
				b.counts["smoke_receivers"]++
			}
			dst = b.appendSprite(dst, a, p, p, [4]float32{1, 1, 1, 0.5}, emission, camera, viewport, "strip_sprites")
		default:
			b.counts["unsupported_strip_family"]++
		}
	}
	// The census so far is the walk's own; culling's goes to emitted.
	if w.counts == nil {
		w.counts = make(map[string]int)
	}
	clear(w.counts)
	maps.Copy(w.counts, b.counts)
	w.hot, w.recording, w.valid = b.hot, false, true
	split(2)
	return b.finishAppend(dst, lights, current, camera, viewport, featureEnd, split)
}

// featureAnchor is a feature sprite's anchor: a footprinted feature stands on
// its cell's terrain height when the adapter has it [03 §5.1.4].
func (b *BattleSprites) featureAnchor(v *frame.FeatureView) [4]float32 {
	p := spritePosition(v.X, v.Y, v.Z)
	if v.FootX > 0 && v.FootZ > 0 && b.anchored(v) {
		p[1] = float32(b.anchorHeights[int(v.CZ)*b.anchorW+int(v.CX)])
	}
	return p
}

func (b *BattleSprites) anchored(v *frame.FeatureView) bool {
	x, z := int(v.CX), int(v.CZ)
	return x >= 0 && z >= 0 && x < b.anchorW && z < b.anchorH && b.anchorHeights[z*b.anchorW+x] >= 0
}

// featureSprite records the source of a feature sprite that survived
// culling at dst[start:]; short features precede all subjects
// [03 R-RAST-01 §6].
func (b *BattleSprites) featureSprite(dst []Sprite, start int, source SpriteSource, ground bool) {
	for i := start; i < len(dst); i++ {
		b.spriteSources = append(b.spriteSources, source)
		if ground {
			dst[i].Current[3] = 1
			b.emitted["ground_feature_sprites"]++
		}
	}
}

// Reselect is Append at another camera for the frames of the last full walk.
// It culls that walk's resolved feature, effect and strip candidates again,
// in their order, instead of resolving every one; projectiles and wreck
// lights are walked as Append walks them. Any other frames take Append.
func (b *BattleSprites) Reselect(dst []Sprite, lights []Light, previous, current *frame.Frame, camera [3]float32, viewport [2]int) ([]Sprite, []Light) {
	w := &b.walk
	if !w.valid || current == nil || w.previous != previous || w.current != current {
		// This walk skipped features by its own camera, not the build's, so
		// no later recull may replay it.
		dst, lights = b.Append(dst, lights, previous, current, camera, viewport)
		w.valid = false
		return dst, lights
	}
	b.Splits = [6]float64{}
	splitMark := time.Now()
	split := func(i int) { now := time.Now(); b.Splits[i] = float64(now.Sub(splitMark)) / 1e6; splitMark = now }
	defer split(5)
	clear(b.counts)
	maps.Copy(b.counts, w.counts)
	clear(b.emitted)
	b.hot = w.hot
	b.hot[hotCulledSprites] = 0
	b.spriteSources = resizeComposition(b.spriteSources, len(dst))
	clear(b.spriteSources)
	b.spriteSourceTick = current.Tick
	for i := range w.features {
		dst, lights = b.replaySpriteOp(dst, lights, &w.features[i], camera, viewport)
	}
	featureEnd := len(dst)
	split(1)
	for i := range w.effects {
		dst, lights = b.replaySpriteOp(dst, lights, &w.effects[i], camera, viewport)
	}
	split(2)
	return b.finishAppend(dst, lights, current, camera, viewport, featureEnd, split)
}

func (b *BattleSprites) replaySpriteOp(dst []Sprite, lights []Light, op *spriteWalkOp, camera [3]float32, viewport [2]int) ([]Sprite, []Light) {
	if op.light {
		return dst, b.appendLight(lights, op.p, op.radius, op.strength, op.hue, camera, viewport)
	}
	start := len(dst)
	dst = b.cullSprite(dst, op.sprite, camera, viewport, op.kind)
	if op.feature {
		b.featureSprite(dst, start, op.source, op.ground)
	}
	return dst, lights
}

func (b *BattleSprites) finishAppend(dst []Sprite, lights []Light, current *frame.Frame, camera [3]float32, viewport [2]int, featureEnd int, split func(int)) ([]Sprite, []Light) {
	dst, lights = b.appendProjectiles(dst, lights, current, camera, viewport)
	split(3)
	// Includes fills, strips and specialized beam/projectile appenders. The
	// production producer engine replaces this whole fallback source family.
	b.spriteSources = resizeComposition(b.spriteSources, len(dst))
	for i := featureEnd; i < len(dst); i++ {
		b.spriteSources[i] = SpriteSource{Kind: SpriteSourceFallbackEffect}
	}
	lights = b.appendWreckLights(lights, current, camera, viewport)
	split(4)
	// Preserve publication order here. The native packer keeps short features
	// in that order before models, then sorts other sprites at displayed alpha.
	if b.totals == nil {
		b.totals = make(map[string]int)
	}
	for k, n := range b.emitted {
		b.counts[k] += n
	}
	for i, n := range b.hot {
		if n > 0 {
			b.counts[hotSpriteCounterNames[i]] += n
		}
	}
	for k, v := range b.counts {
		b.totals[k] += v
	}
	b.counts["sprites_submitted"] = len(dst)
	b.counts["lights_submitted"] = len(lights)
	return dst, lights
}

// Report returns detached diagnostic storage safe to retain after the worker
// publishes its next committed frame. Cumulative counters are record-visits,
// not unique objects; missing art lists exact logical paths.
func (b *BattleSprites) Report() map[string]any {
	paths := make([]string, 0, len(b.missing))
	for p := range b.missing {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	missing := make([]map[string]string, 0, len(paths))
	for _, p := range paths {
		missing = append(missing, map[string]string{"path": p, "reason": b.missing[p]})
	}
	counts, totals := make(map[string]int), make(map[string]int)
	for k, v := range b.counts {
		counts[k] = v
	}
	for k, v := range b.totals {
		totals[k] = v
	}
	return map[string]any{"atlas_width": b.texture.Width, "atlas_height": b.texture.Height, "atlas_bytes": len(b.texture.RGBA), "atlas_cells": len(b.regions), "banks_loaded": b.banksLoaded, "frames_loaded": b.framesLoaded, "sequences_loaded": len(b.sequences), "missing_art": missing, "last_counts": counts, "record_visits": totals, "alternate_children_omitted": 0, "alternate_children_composited": b.alternateChildrenPrepared, "banks": append([]spriteBankCensus(nil), b.bankCensus...), "feature_scope_filtered": b.featureScopeFiltered, "prepared_feature_names": append([]string(nil), b.preparedFeatureNames...), "unresolved_feature_names": append([]string(nil), b.unresolvedFeatureNames...), "crop_raw_texel_area": b.rawSpritePixels, "crop_retained_texel_area": b.croppedSpritePixels, "crop_rgba_margin_bytes_removed": (b.rawSpritePixels - b.croppedSpritePixels) * 4, "crop_fully_transparent_frames": b.fullyTransparentFrames, "crop_area_basis": "sum of decoded frame areas before deduplication and gutters; atlas_bytes is actual packed storage", "approximations": []string{
		"rest feature animations and event cursors sample authored holds from committed ticks",
		"native straight RGBA alpha approximates destination-palette ALP; ordered alternate children use authored half-alpha over transparent RGBA",
		"strip particles snap at committed ticks; original nano palette retained without optional team-color ramp",
		"committed perspective admission; spectator bypass; retail strip barrier composition omitted",
		"32 strongest explicit art/projectile/wreck lights; shipping 64-source family reserves and nano clustering omitted; palette flash discs and projectile displacement lenses omitted",
		"smoke is a non-emissive receiver; native sprite shader does not yet apply smoke scattering",
		"fire flicker, wreck cooling strength and light-source position sample committed ticks; source lighting itself is not GPU interpolated",
		"wreck plumes use actual prepared static projected model bounds; orientation-dependent bounds are approximated",
		"beams use continuous rotated quads and interpolate centres; span/angle snap to current endpoints",
		"sprite depth uses native world anchor; tall billboard pixel depth and terrain overlap are approximations",
	}, "terrain_anchor_snapshot": fmt.Sprintf("%dx%d", b.anchorW, b.anchorH), "art_reach_world_units": b.artReach, "detail_frames": b.detailFrames, "detail_frames_omitted": b.detailOmitted, "detail_texel_area": b.detailPixels, "detail_policy": "production 2x feature variants above one device pixel per world pixel; authored frames otherwise"}
}
