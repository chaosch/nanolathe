package meshscene

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/frame"
)

// Native scalar shape uses the existing authored Enhanced profile (§25.2).
// The GPU evaluates the attack/decay at committed age+Alpha-1.
func battleBlastShape(size float32, area, damage int32, known bool) (end, width, strength float32) {
	if !known || size >= 128 {
		if size < 64 {
			return
		}
		return min(max(size*2.5, 120), 320), 10 + size*0.12, min(size/16, 7)
	}
	if size < 48 || size < 64 && (area < 32 || damage < 80) {
		return
	}
	baseline := max(size*2.5, 160)
	breadth := float32(math.Sqrt(float64(min(max(float32(area)-48, 0)/208, 1))))
	force := float32(math.Sqrt(float64(min(max(float32(damage)-80, 0)/1120, 1))))
	return max(baseline, min(baseline*(1+0.5*breadth), 280)), 10 + size*0.12, min(size/16, 7) * (1 + 0.75*force)
}
func (b *BattleSprites) featurePosition(v frame.FeatureView) [4]float32 {
	p := spritePosition(v.X, v.Y, v.Z)
	x, z := int(v.CX), int(v.CZ)
	if v.FootX > 0 && v.FootZ > 0 && x >= 0 && z >= 0 && x < b.anchorW && z < b.anchorH && b.anchorHeights[z*b.anchorW+x] >= 0 {
		p[1] = float32(b.anchorHeights[z*b.anchorW+x])
	}
	return p
}
func distortionVisible(v Distortion, camera [3]float32, viewport [2]int) bool {
	rect := [4]float32{-v.Shape[0] - v.Shape[1], -v.Shape[0] - v.Shape[1], 2 * (v.Shape[0] + v.Shape[1]), 2 * (v.Shape[0] + v.Shape[1])}
	if v.Shape[3] == 1 {
		rect = [4]float32{-v.Shape[0], v.Params[2] - 2*v.Shape[1], 2 * v.Shape[0], 2 * v.Shape[1]}
	}
	return spriteVisible(Sprite{Previous: v.Previous, Current: v.Current, Rect: rect}, camera, viewport)
}

// Distortions shares one immutable-world native pass. Selection happens after
// viewport culling; rings precede wreck heat, which precedes vegetation (§27.2).
func (b *BattleSprites) Distortions(dst []Distortion, previous, current *frame.Frame, camera [3]float32, viewport [2]int) []Distortion {
	if current == nil {
		return dst
	}
	start := len(dst)
	for _, v := range current.Effects {
		if !b.effects.BlastRings {
			break
		}
		if !b.explosionVisible(v, current) {
			continue
		}
		if _, ok := b.art(v.AssetID, v.Graphic, v.SeqA, true); !ok {
			continue
		}
		age := float32(current.Tick - v.StartTick)
		if age > 15 || age <= 0 {
			continue
		}
		end, width, strength := battleBlastShape(b.sequenceExtent(v.AssetID, v.Graphic), v.BlastAreaOfEffect, v.BlastDamage, v.HasBlastProfile)
		if strength <= 0 {
			continue
		}
		p := spritePosition(v.X, v.Y, v.Z)
		old := spritePrevious(b.previousEffects, v.PresentationID, p)
		d := Distortion{Previous: old, Current: p, Shape: [4]float32{end, width, strength, 0}, Params: [4]float32{age}}
		probe := d
		// Cull on the current ring's outer radius, rather than its eventual
		// extent, before taking one of the 32 visible-source slots (§25).
		probe.Shape[0] = 12 + (end-12)*age/15
		if !distortionVisible(probe, camera, viewport) {
			b.counts["blast_rings_culled"]++
			continue
		}
		if len(dst)-start < 32 {
			dst = append(dst, d)
			continue
		}
		b.counts["blast_rings_over_limit"]++
		// Evict the latest weakest, preserving earliest source-order ties.
		at := start
		for i := start + 1; i < len(dst); i++ {
			if battleBlastPower(dst[i]) <= battleBlastPower(dst[at]) {
				at = i
			}
		}
		if battleBlastPower(d) > battleBlastPower(dst[at]) {
			copy(dst[at:], dst[at+1:])
			dst[len(dst)-1] = d
		}
	}
	b.counts["blast_rings"] = len(dst) - start
	wrecks := 0
	for fi := range current.Features {
		v := &current.Features[fi]
		if !b.effects.WreckShimmer || wrecks >= 32 {
			break
		}
		if v.Model == "" || !v.WreckHeatKnown || v.Y < current.Visibility.SeaLevel || !battleFeatureVisible(current, *v, b.spectator) || !battlePointVisible(current, v.X, v.Y, v.Z, b.spectator) {
			continue
		}
		age := float32(current.Tick - v.WreckBornTick)
		if age >= 300 {
			continue
		}
		rect, ok := b.featureModelBounds[b.modelKeys.key(v.Model)]
		if !ok {
			b.counts["wreck_heat_bounds_missing"]++
			continue
		}
		p := spritePosition(v.X, v.Y, v.Z)
		old := spritePrevious(b.previousFeatures, v.InstanceID, p)
		p[0] += rect[0] + rect[2]*0.5
		old[0] += rect[0] + rect[2]*0.5
		width, height := min(max(rect[2]*0.55, 12), 40), min(max(rect[3]*0.85, 32), 64)
		heat := 1 - age/300
		d := Distortion{Previous: old, Current: p, Shape: [4]float32{width, height * 0.5, 0.55 * heat * heat, 1}, Params: [4]float32{float32(current.Tick % 3600), float32((v.CX*13 + v.CZ*7) & 255), rect[1] + rect[3]*0.65, age + 1}}
		if !distortionVisible(d, camera, viewport) {
			b.counts["wreck_heat_culled"]++
			continue
		}
		dst = append(dst, d)
		wrecks++
	}
	b.counts["wreck_heat_plumes"] = wrecks
	trees := 0
	for fi := range current.Features {
		v := &current.Features[fi]
		if !b.effects.FireShimmer || trees >= 128 {
			break
		}
		if !v.IsBurning || v.Model != "" && v.Filename == "" || !battleFeatureVisible(current, *v, b.spectator) || !battlePointVisible(current, v.X, v.Y, v.Z, b.spectator) || v.RuntimeLive && v.EventSeqName == "" {
			continue
		}
		var a spriteArt
		var ok bool
		if v.EventSeqName != "" {
			a, ok = b.eventArt(v.Filename, v.EventSeqName, v.EventSeqVisit)
		} else {
			a, ok = b.restArt(v.Filename, v.SeqName, current.Tick, v.Animating)
		}
		if !ok || a.identity == nil {
			continue
		}
		p := b.featurePosition(*v)
		p[0] += -float32(a.identity.XOffset) + float32(a.identity.Width)*0.5
		width, height := min(max(float32(a.identity.Width)*0.55, 14), 38), min(max(float32(a.identity.Height)*1.25, 56), 112)
		d := Distortion{Previous: p, Current: p, Shape: [4]float32{width, height * 0.5, 1, 1}, Params: [4]float32{float32(current.Tick % 3600), float32((v.CX*13 + v.CZ*7) & 255), -float32(a.identity.YOffset) + float32(a.identity.Height)*0.45}}
		if !distortionVisible(d, camera, viewport) {
			b.counts["tree_heat_culled"]++
			continue
		}
		dst = append(dst, d)
		trees++
	}
	b.counts["tree_heat_plumes"] = trees
	b.counts["distortions_submitted"] = len(dst) - start
	// Append accumulates sprite counters first; distortions run immediately after.
	if b.totals == nil {
		b.totals = make(map[string]int)
	}
	for _, key := range []string{"blast_rings", "blast_rings_culled", "blast_rings_over_limit", "wreck_heat_plumes", "wreck_heat_culled", "wreck_heat_bounds_missing", "tree_heat_plumes", "tree_heat_culled", "distortions_submitted"} {
		b.totals[key] += b.counts[key]
	}
	return dst
}

func battleBlastPower(d Distortion) float32 {
	age := min(d.Params[0], 15)
	fade := 1 - age/15
	return d.Shape[2] * min(age, 1) * fade * fade
}
