package meshscene

import (
	"fmt"
	"math"
	"sort"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/effects"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// BattleSprites owns load-time immutable art and worker-owned presentation
// scratch. Append and Report must run on the session worker; Texture may be
// borrowed by the renderer for its one-time upload [I6].
type BattleSprites struct {
	texture                                             Texture
	regions                                             []atlasRegion
	sequences                                           map[string]*spriteSequence
	missing                                             map[string]string
	counts                                              map[string]int
	totals                                              map[string]int
	previousEffects, previousProjectiles, previousBeams map[uint64][4]float32
	colors                                              [256][4]float32
	logical                                             [256]byte
	banksLoaded, framesLoaded, alternateChildrenOmitted int
	anchorHeights                                       []int16
	anchorW, anchorH                                    int
	flashSides                                          [effects.FlashTableCount][]int
}

func (b *BattleSprites) Texture() Texture { return b.texture }

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
	return [4]float32{float32(x) / 65536, float32(y) / 65536, float32(z) / 65536, 1}
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
	seq := b.sequences[spriteSequenceKey(bank, name)]
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
	seq := b.sequences[spriteSequenceKey(bank, name)]
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

func (b *BattleSprites) appendSprite(dst []Sprite, a spriteArt, previous, current [4]float32, color [4]float32, emission float32, camera [3]float32, viewport [2]int, kind string) []Sprite {
	r := b.regions[a.cell]
	u0, u1 := r.uv([2]float32{}, b.texture.Width, b.texture.Height), r.uv([2]float32{1, 1}, b.texture.Width, b.texture.Height)
	s := Sprite{Previous: previous, Current: current, Rect: [4]float32{-float32(a.xoff), -float32(a.yoff), float32(a.width), float32(a.height)}, UV: [4]float32{u0[0], u0[1], u1[0], u1[1]}, Color: color, Params: [4]float32{emission, 0, 0, 0}}
	if !spriteVisible(s, camera, viewport) {
		b.counts["culled_sprites"]++
		return dst
	}
	b.counts[kind]++
	return append(dst, s)
}

func (b *BattleSprites) appendLight(dst []Light, p [4]float32, radius, strength float32, hue [3]float32, camera [3]float32, viewport [2]int) []Light {
	if radius <= 0 || strength <= 0 {
		return dst
	}
	b.counts["light_sources"]++
	probe := Sprite{Previous: p, Current: p, Rect: [4]float32{-radius, -radius, radius * 2, radius * 2}}
	if !spriteVisible(probe, camera, viewport) {
		b.counts["culled_lights"]++
		return dst
	}
	if len(dst) >= 32 {
		b.counts["lights_over_limit"]++
		return dst
	}
	if hue == [3]float32{} {
		hue = [3]float32{1, 1, 1}
	}
	return append(dst, Light{PositionRadius: [4]float32{p[0], p[1], p[2], radius}, ColorStrength: [4]float32{hue[0], hue[1], hue[2], strength}})
}

// Append consumes committed copies only. Effect/projectile identities preserve
// movement interpolation; strip records have no published stable identity and
// snap to their committed point instead of inventing a matching key [I6].
func (b *BattleSprites) Append(dst []Sprite, lights []Light, previous, current *frame.Frame, camera [3]float32, viewport [2]int) ([]Sprite, []Light) {
	clear(b.counts)
	if current == nil {
		return dst, lights
	}
	clear(b.previousEffects)
	clear(b.previousProjectiles)
	clear(b.previousBeams)
	if previous != nil {
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
	white := spriteArt{cell: 0, width: 2, height: 2}
	for _, v := range current.Features {
		b.counts["features_seen"]++
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
		p := spritePosition(v.X, v.Y, v.Z)
		if v.FootX > 0 && v.FootZ > 0 {
			x, z := int(v.CX), int(v.CZ)
			if x >= 0 && z >= 0 && x < b.anchorW && z < b.anchorH && b.anchorHeights[z*b.anchorW+x] >= 0 {
				p[1] = float32(b.anchorHeights[z*b.anchorW+x])
			} else {
				// TODO(question): supply immutable terrain when loading this adapter;
				// incomplete terrain keeps the production snapshot-only fallback.
				b.counts["feature_anchor_fallback"]++
			}
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
					a, ok = b.art(v.Filename, name, 0, false)
				}
				if !shadow && v.Animating {
					b.counts["rest_feature_animations_frozen"]++
				}
			}
			if name == "" {
				continue
			}
			if !ok {
				b.counts["missing_feature_frames"]++
				continue
			}
			color := [4]float32{1, 1, 1, 1}
			// TODO(question): implement destination-palette ALP in the native host;
			// conventional half-alpha is an explicit modern display approximation.
			if trans && !v.RuntimeLive {
				color[3] = 0.5
				b.counts["feature_alp_approximations"]++
			}
			kind := "feature_bodies"
			if shadow {
				kind = "feature_shadows"
			}
			dst = b.appendSprite(dst, a, p, p, color, 0, camera, viewport, kind)
		}
	}
	for _, v := range current.Effects {
		b.counts["effects_seen"]++
		p := spritePosition(v.X, v.Y, v.Z)
		old := spritePrevious(b.previousEffects, v.PresentationID, p)
		a, ok := b.art(v.AssetID, v.Graphic, v.SeqA, true)
		if v.Light && v.HasFlashDisc && v.FlashLevel >= 0 {
			lights = b.appendLight(lights, p, float32(v.FlashRadius), float32(min(v.FlashLevel, 31)+1)/32, a.hue, camera, viewport)
		} else if v.Light {
			b.counts["unresolved_light_geometry"]++
		}
		if v.ActiveB && v.HasCalculatedFlash && int(v.CalculatedTable) < len(b.flashSides) {
			sides := b.flashSides[v.CalculatedTable]
			if v.SeqB >= 0 && int(v.SeqB) < len(sides) {
				lights = b.appendLight(lights, p, float32(sides[v.SeqB])/2, 1, a.hue, camera, viewport)
				b.counts["calculated_discs_as_lights"]++
			}
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
		dst = b.appendSprite(dst, a, old, p, [4]float32{1, 1, 1, 1}, 1, camera, viewport, "effect_sprites")
	}
	for _, v := range current.Strips {
		b.counts["strip_particles_seen"]++
		p := spritePosition(v.X, v.Y, v.Z)
		switch v.Family {
		case frame.StripFamilySprinkle, frame.StripFamilyNano:
			if v.Fill != 0 {
				dst = b.appendSprite(dst, white, p, p, b.colors[v.Fill], 1, camera, viewport, "strip_fills")
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
			dst = b.appendSprite(dst, a, p, p, [4]float32{1, 1, 1, 0.5}, 1, camera, viewport, "strip_sprites")
		default:
			b.counts["unsupported_strip_family"]++
		}
	}
	dst = b.appendProjectiles(dst, current, camera, viewport)
	// Live native depth is 0.5-(z-camera.z+2*y)*scale. Larger z+2*y is nearer;
	// transparent fragments test opaque depth but never write it.
	sort.SliceStable(dst, func(i, j int) bool {
		return dst[i].Current[2]+2*dst[i].Current[1] < dst[j].Current[2]+2*dst[j].Current[1]
	})
	if b.totals == nil {
		b.totals = make(map[string]int)
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
	return map[string]any{"atlas_width": b.texture.Width, "atlas_height": b.texture.Height, "atlas_bytes": len(b.texture.RGBA), "atlas_cells": len(b.regions), "banks_loaded": b.banksLoaded, "frames_loaded": b.framesLoaded, "sequences_loaded": len(b.sequences), "missing_art": missing, "last_counts": counts, "record_visits": totals, "alternate_children_omitted": b.alternateChildrenOmitted, "approximations": []string{
		"rest feature animations use first authored frame; feature event cursors retain authored holds",
		"native straight RGBA alpha approximates destination-palette ALP; alternate composite children omitted",
		"strip particles snap at committed ticks; original nano palette retained without optional team-color ramp",
		"omniscient battle presentation has no fog/LOS admission or retail strip barrier composition",
		"LHT and calculated flash discs become bounded native radial lights from published geometry; no palette lookup or displacement lens",
		"beams use continuous rotated quads and interpolate centres; span/angle snap to current endpoints",
		"sprite depth uses native world anchor; tall billboard pixel depth and terrain overlap are approximations",
	}, "terrain_anchor_snapshot": fmt.Sprintf("%dx%d", b.anchorW, b.anchorH)}
}
