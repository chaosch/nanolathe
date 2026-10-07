package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"math"
)

// RGBA approximates the authored ordered keyed/ALP graph, preserving leaf
// coverage and offsets. ALP's destination-palette lookup remains an explicit
// prototype approximation [fmt gaf][03 §5.3.1].
func spriteFrameRGBA(f *formats.GAFFrame, pal *palette.Tables) []byte {
	w, h := int(f.Width), int(f.Height)
	pixels := make([]float32, w*h*4)
	var draw func(*formats.GAFFrame, bool)
	draw = func(leaf *formats.GAFFrame, tinted bool) {
		if leaf == nil {
			return
		}
		if len(leaf.Subframes) > 0 {
			for _, child := range leaf.Subframes {
				if child != nil {
					draw(child, tinted || child.AlternateBlitter != 0)
				}
			}
			return
		}
		alpha := float32(1)
		if tinted {
			alpha = 0.5
		}
		for y := 0; y < int(leaf.Height); y++ {
			for x := 0; x < int(leaf.Width); x++ {
				i := y*int(leaf.Width) + x
				if i >= len(leaf.Pixels) || i < len(leaf.Transparent) && leaf.Transparent[i] {
					continue
				}
				dx, dy := x-int(leaf.XOffset)+int(f.XOffset), y-int(leaf.YOffset)+int(f.YOffset)
				if dx < 0 || dy < 0 || dx >= w || dy >= h {
					continue
				}
				at := (dy*w + dx) * 4
				c := pal.Base[leaf.Pixels[i]]
				for j := 0; j < 3; j++ {
					pixels[at+j] = float32(c[j])/255*alpha + pixels[at+j]*(1-alpha)
				}
				pixels[at+3] = alpha + pixels[at+3]*(1-alpha)
			}
		}
	}
	draw(f, false)
	rgba := make([]byte, len(pixels))
	for i := 0; i < w*h; i++ {
		a := pixels[i*4+3]
		if a <= 0 {
			continue
		}
		for j := 0; j < 3; j++ {
			rgba[i*4+j] = byte(min(pixels[i*4+j]/a, 1)*255 + 0.5)
		}
		rgba[i*4+3] = byte(a*255 + 0.5)
	}
	return rgba
}

// BL3 measures covered bright texels, keeping energy separate from hue. The
// threshold and square-root energy are existing Enhanced policy (§23.2).
func spriteEmission(rgba []byte) (hue, emission [3]float32) {
	var weight float32
	count := 0
	for i := 0; i+3 < len(rgba); i += 4 {
		if rgba[i+3] == 0 {
			continue
		}
		count++
		alpha := float32(rgba[i+3]) / 255
		peak := float32(max(rgba[i], rgba[i+1], rgba[i+2])) / 255 * alpha
		w := max((peak-0.45)/0.55, 0)
		w *= w
		weight += w
		for j := range hue {
			hue[j] += float32(rgba[i+j]) / 255 * alpha * w
		}
	}
	if weight == 0 || count == 0 {
		return
	}
	energy := float32(math.Sqrt(float64(weight / float32(count))))
	for j := range hue {
		hue[j] /= weight
		emission[j] = hue[j] * energy
	}
	return
}

func (b *BattleSprites) restArt(bank, name string, tick uint32, animating bool) (spriteArt, bool) {
	if bank == "" {
		bank = content.DefaultEffectBank
	}
	seq := b.sequences[spriteSequenceKey(bank, name)]
	if !animating || seq == nil || seq.cycle == 0 || len(seq.frames) < 2 {
		return b.art(bank, name, 0, false)
	}
	at := uint64(tick) % seq.cycle
	for _, a := range seq.frames {
		hold := uint64(max(a.hold, 1))
		if at < hold {
			b.counts["animated_rest_frames"]++
			return a, a.width > 0 && a.height > 0
		}
		at -= hold
	}
	return spriteArt{}, false
}
func (b *BattleSprites) sequenceExtent(bank, name string) float32 {
	if bank == "" {
		bank = content.DefaultEffectBank
	}
	if seq := b.sequences[spriteSequenceKey(bank, name)]; seq != nil {
		return seq.extent
	}
	return 0
}
func (b *BattleSprites) explosionVisible(v frame.EffectView, f *frame.Frame) bool {
	if !v.ActiveA || !battlePointVisible(f, v.X, v.Y, v.Z, b.spectator) {
		return false
	}
	switch v.Kind {
	case frame.KindExplosion.String(), frame.KindImpact.String(), frame.KindWaterImpact.String():
		return true
	}
	return false
}
func battleLightPower(v Light) float32 {
	return max(v.ColorStrength[0], v.ColorStrength[1], v.ColorStrength[2]) * v.ColorStrength[3]
}
func (b *BattleSprites) appendArtLight(dst []Light, a spriteArt, p [4]float32, extent float32, kind string, tick uint32, camera [3]float32, viewport [2]int) []Light {
	color := a.emission
	peak := max(color[0], color[1], color[2])
	if peak < 0.015 || a.identity == nil {
		return dst
	}
	size := float32(max(a.identity.Width, a.identity.Height))
	radius := min(max(extent*1.4, 48), 192) * 1.5
	switch kind {
	case "fire", "spark":
		if peak < 0.25 {
			color = [3]float32{0.95 * peak, 0.55 * peak, 0.18 * peak}
		}
		gain := float32(0.9)
		radius = min(max(size*1.4, 20), 56)
		if kind == "fire" {
			radius = min(max(size*1.4, 128), 160)
			h := uint32(int32(p[0]))*73856093 ^ uint32(int32(p[2]-p[1]*0.5))*19349663
			h ^= h >> 13
			h *= 2654435761
			phase := float64(h>>22) * (2 * math.Pi / 1024)
			gain = 1.6 * (0.75 + 0.25*(0.5+0.5*float32(math.Sin(2*math.Pi*float64(tick%3600)/15+phase))))
		}
		for j := range color {
			color[j] *= gain
		}
	case "projectile":
		radius = min(max(size*1.4, 56), 96)
	}
	p[1] += float32(a.identity.Height) * 0.25
	b.counts[kind+"_lights"]++
	return b.appendLight(dst, p, radius, 1, color, camera, viewport)
}
func wreckCooling(f frame.FeatureView, tick uint32) [3]float32 {
	age := float32(tick - f.WreckBornTick)
	flash := max(1-age/6, 0)
	cool := max(1-age/180, 0)
	red, amber := cool*cool, cool*cool*cool*cool
	return [3]float32{0.75*red + 0.15*flash, 0.20*amber + 0.55*flash, 0.015*amber + 0.42*flash}
}
func (b *BattleSprites) appendWreckLights(dst []Light, f *frame.Frame, camera [3]float32, viewport [2]int) []Light {
	for _, v := range f.Features {
		if !v.WreckHeatKnown || v.Model == "" || v.Y < f.Visibility.SeaLevel || !battleFeatureVisible(f, v, b.spectator) || !battlePointVisible(f, v.X, v.Y, v.Z, b.spectator) {
			continue
		}
		color := wreckCooling(v, f.Tick)
		for j := range color {
			color[j] *= 0.6
		}
		if max(color[0], color[1], color[2]) < 0.015 {
			continue
		}
		bounds, ok := b.featureModelBounds[battleModelKey(v.Model)]
		if !ok {
			b.counts["wreck_light_bounds_missing"]++
			continue
		}
		p := spritePosition(v.X, v.Y, v.Z)
		p[0] += bounds[0] + bounds[2]*0.5
		p[2] += bounds[1] + bounds[3]*0.5
		dst = b.appendLight(dst, p, 80, 1, color, camera, viewport)
		b.counts["wreck_lights"]++
	}
	return dst
}
