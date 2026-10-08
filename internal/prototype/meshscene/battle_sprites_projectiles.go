package meshscene

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/render"
)

// These are the same closed engine slots as the production presentation
// resolver, including the repeated plasmasm slot [06 R-WFX-01 §1][06 R-WFX-01 §4].
var spriteProjectileSequences = [...]string{"cannonshell", "plasmasm", "plasmamd", "ultrashell", "plasmasm"}

func spriteProjectileName(family, selector int32, base bool) string {
	if base {
		return "shadow"
	}
	if family == render.RenderTypeLifetimeGAF {
		return "flamestream"
	}
	if family == render.RenderTypeSelectorGAF && selector >= 0 && int(selector) < len(spriteProjectileSequences) {
		return spriteProjectileSequences[selector]
	}
	return ""
}

func spriteBeamCenter(v frame.ProjectileView) [4]float32 {
	h, t := spritePosition(v.X, v.Y, v.Z), spritePosition(v.TailX, v.TailY, v.TailZ)
	return [4]float32{(h[0] + t[0]) * 0.5, (h[1] + t[1]) * 0.5, (h[2] + t[2]) * 0.5, 0}
}

func (b *BattleSprites) appendProjectiles(dst []Sprite, lights []Light, current *frame.Frame, camera [3]float32, viewport [2]int) ([]Sprite, []Light) {
	options := render.ProjectileDispatchOptions{
		FrameCount: func(v frame.ProjectileView) (int, bool) {
			name := spriteProjectileName(v.RenderType, v.Selector, false)
			seq := b.sequence(content.DefaultEffectBank, name)
			if seq == nil {
				return 0, false
			}
			return len(seq.frames), len(seq.frames) > 0
		},
		ResolveGAF: func(req render.ProjectileGAFRequest) (*formats.GAFFrame, bool) {
			name := spriteProjectileName(req.Family, req.Sequence, req.Base)
			if name == "" {
				return nil, false
			}
			a, ok := b.art(content.DefaultEffectBank, name, int32(req.Frame), false)
			return a.identity, ok
		},
	}
	for _, v := range current.Projectiles {
		b.counts["projectiles_seen"]++
		if !battlePointVisible(current, v.X, v.Y, v.Z, b.spectator) {
			b.counts["projectiles_visibility_rejected"]++
			continue
		}
		if v.BurstRemaining != 0 {
			b.counts["burst_schedulers_skipped"]++
			continue
		}
		switch v.RenderType {
		case render.RenderTypeBaseSpriteModel, render.RenderTypeBaseModelDistinct, render.RenderTypeRecordOrientation:
			b.counts["model_projectiles_delegated"]++
			b.counts["model_projectile_shadows_omitted"]++
			continue
		case render.RenderTypeGlobalGAF:
			// TODO(question): add the startup displacement-map pass to the native
			// renderer; the researched lens has no ordinary GAF sprite [03 §5.4].
			b.counts["displacement_lenses_omitted"]++
			continue
		case render.RenderTypeSegmented:
			// TODO(question): publish the production presentation CRT jitter
			// geometry; this adapter has neither a matching CRT snapshot nor
			// authored points, so it draws no invented straight beam [03 §5.4].
			b.counts["segmented_beams_omitted"]++
			continue
		}
		d := render.DispatchProjectileView(v, current.Tick, options)
		if d.Suppressed {
			b.counts["projectile_dispatch_suppressed"]++
			continue
		}
		if d.RenderType == render.RenderTypeBeam {
			dst = b.appendBeam(dst, v, d, camera, viewport)
			p := spriteBeamCenter(v)
			h, t := spritePosition(v.X, v.Y, v.Z), spritePosition(v.TailX, v.TailY, v.TailZ)
			length := float32(math.Hypot(float64(h[0]-t[0]), float64((h[2]-h[1]*0.5)-(t[2]-t[1]*0.5))))
			c := b.colors[b.logical[uint8(d.Color)]]
			lights = b.appendLight(lights, p, min(max(length*0.6, 48), 160), 0.8, [3]float32{c[0], c[1], c[2]}, camera, viewport)
			b.counts["beam_lights"]++
			continue
		}
		if d.FrameAsset == nil {
			b.counts["unsupported_projectile_draw"]++
			continue
		}
		p := spritePosition(v.X, v.Y, v.Z)
		old := spritePrevious(b.previousProjectiles, v.PresentationID, p)
		if d.BaseFrame != nil {
			if v.FloorHeightValid {
				a, ok := b.art(content.DefaultEffectBank, "shadow", 0, false)
				if ok {
					ground, oldGround := p, old
					// Published cached floor is already whole world height. The
					// ordinary projection applies its half-height shear [03 §5.4].
					ground[1], oldGround[1] = float32(v.FloorHeight), float32(v.FloorHeight)
					dst = b.appendSprite(dst, a, oldGround, ground, [4]float32{1, 1, 1, 1}, 0, camera, viewport, "projectile_shadows")
				}
			} else {
				b.counts["projectile_shadow_floor_missing"]++
			}
		}
		name := spriteProjectileName(v.RenderType, d.Selector, false)
		a, ok := b.art(content.DefaultEffectBank, name, int32(d.Frame), false)
		if !ok {
			b.counts["missing_projectile_frames"]++
			continue
		}
		dst = b.appendSprite(dst, a, old, p, [4]float32{1, 1, 1, 1}, 1, camera, viewport, "projectile_sprites")
		lights = b.appendArtLight(lights, a, p, b.sequenceExtent(content.DefaultEffectBank, name), "projectile", current.Tick, camera, viewport)
	}
	return dst, lights
}

func (b *BattleSprites) appendBeam(dst []Sprite, v frame.ProjectileView, d render.ProjectileDraw, camera [3]float32, viewport [2]int) []Sprite {
	h, t := spritePosition(d.Head.X, d.Head.Y, d.Head.Z), spritePosition(d.Tail.X, d.Tail.Y, d.Tail.Z)
	// BeamStrokes owns secondary offset and endpoint ordering. Integer screen
	// operands follow production's high-word/half-height projection [03 §2.5].
	head := [2]int32{int32(v.X) >> 16, (int32(v.Z) >> 16) - ((int32(v.Y) >> 16) >> 1)}
	tail := [2]int32{int32(v.TailX) >> 16, (int32(v.TailZ) >> 16) - ((int32(v.TailY) >> 16) >> 1)}
	centre := spriteBeamCenter(v)
	old := spritePrevious(b.previousBeams, v.PresentationID, centre)
	r := b.regions[0]
	uv := r.uv([2]float32{}, b.texture.Width, b.texture.Height)
	projectedCentre := [2]float32{(h[0] + t[0]) * 0.5, (h[2] + t[2] - (h[1]+t[1])*0.5) * 0.5}
	var pair [2]render.BeamStroke
	for _, stroke := range render.BeamStrokesInto(&pair, head, tail, d.Color, d.Color2) {
		dx, dy := float32(stroke.X1-stroke.X0), float32(stroke.Y1-stroke.Y0)
		length := float32(math.Hypot(float64(dx), float64(dy)))
		if length == 0 {
			length = 1
		}
		p, prev := centre, old
		shiftX := float32(int64(stroke.X0)+int64(stroke.X1))*0.5 - projectedCentre[0]
		shiftY := float32(int64(stroke.Y0)+int64(stroke.Y1))*0.5 - projectedCentre[1]
		p[0] += shiftX
		prev[0] += shiftX
		p[2] += shiftY
		prev[2] += shiftY
		color := b.colors[b.logical[uint8(stroke.Color)]]
		s := Sprite{Previous: prev, Current: p, Rect: [4]float32{-length / 2, -0.5, length, 1}, UV: [4]float32{uv[0], uv[1], uv[0], uv[1]}, Color: color, Params: [4]float32{1, 0, float32(math.Atan2(float64(dy), float64(dx))), 0}}
		if !spriteVisible(s, camera, viewport) {
			b.counts["culled_sprites"]++
			continue
		}
		dst = append(dst, s)
		b.counts["beam_strokes"]++
	}
	return dst
}
