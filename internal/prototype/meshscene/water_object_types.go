package meshscene

import (
	"fmt"
	"math"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

// WaterObject is one float4 per retained instance: reflection admission,
// physical sea Y, BLUE refraction admission and reserved. This contains no
// simulation state or geometry. Factory/cargo groups require isolated member
// images for reflections (§22.4); the native module reports/suppresses those
// groups rather than reflecting merged colour at a child's physical height.
type WaterObject struct{ Params [4]float32 }

// WaterSourceVertex is four float4s (64 bytes). PositionHeight is logical
// framebuffer XY, unscaled physical height above sea, and source kind (sprite
// 1, stroke 2). UV addresses the effects atlas in texels; Bounds is its frame's
// texel endpoints. Color is unpremultiplied displayed palette RGB plus alpha.
// Only XY changes during physical packing; heights and atlas texels do not.
type WaterSourceVertex struct{ PositionHeight, UV, Color, Bounds [4]float32 }
type WaterSource struct{ Vertices [4]WaterSourceVertex }
type WaterSourceCounts struct{ Sprites, Lines int }

// NewWaterSources consumes existing recorded leaves, never whole composite
// light emitters. Production visits every sprite before every line, retaining
// list order within each family. Cap admission stays in native after models,
// so stock sources may use all remaining vertices, not only their reserve
// (water_reflections.go; DESIGN_GPU_RENDERER §26.4, §32.2).
func NewWaterSources(layers []EffectLayer, palette [256][4]byte, lookup func(*formats.GAFFrame) ([4]float32, error)) ([]WaterSource, WaterSourceCounts, error) {
	return AppendWaterSources(nil, layers, palette, lookup)
}

// AppendWaterSources is NewWaterSources appending into dst[:0], so a presenter
// can reuse last draw's storage once native submission has copied it.
func AppendWaterSources(dst []WaterSource, layers []EffectLayer, palette [256][4]byte, lookup func(*formats.GAFFrame) ([4]float32, error)) ([]WaterSource, WaterSourceCounts, error) {
	out := dst[:0]
	var counts WaterSourceCounts
	transform := func(w drawlist.WorldSpace) (record, factor float32) {
		record = float32(camera.ZoomOf(w.Step)) / float32(camera.ZoomUnit)
		factor = 1
		if record > 0 {
			if w.Factor > 0 {
				factor = w.Factor / record
			} else if w.Zoom > 0 {
				factor = float32(w.Zoom) / float32(camera.ZoomUnit) / record
			}
		}
		return
	}
	for _, layer := range layers {
		for _, source := range layer.Sources {
			record, factor := transform(source.World)
			for _, sp := range source.Sprites {
				if !sp.ReflectWater || sp.ReflectionHeight < 0 || sp.Frame == nil || sp.Kind != drawlist.BlitKeyed {
					continue
				}
				frame := sp.Frame
				w, h := float32(frame.Width), float32(frame.Height)
				if w <= 0 || h <= 0 {
					continue
				}
				if record <= 0 || lookup == nil {
					return nil, counts, fmt.Errorf("nanolathe: water reflection source requires recording scale and prepared atlas")
				}
				rect, err := lookup(frame)
				if err != nil {
					return nil, counts, err
				}
				x, y := float32(sp.X), float32(sp.Y)
				if sp.Anchored {
					x -= float32(frame.XOffset)
					y -= float32(frame.YOffset)
				}
				pivot := float32(sp.Y) + sp.ReflectionHeight*.5
				y0, y1 := 2*pivot-y, 2*pivot-(y+h)
				var quad WaterSource
				for i, v := range [4][4]float32{{x, y0, rect[0], rect[1]}, {x + w, y0, rect[2], rect[1]}, {x + w, y1, rect[2], rect[3]}, {x, y1, rect[0], rect[3]}} {
					quad.Vertices[i] = WaterSourceVertex{PositionHeight: [4]float32{v[0]*factor + source.World.OffsetX, v[1]*factor + source.World.OffsetY, sp.ReflectionHeight / record, 1}, UV: [4]float32{v[2], v[3], 0, 0}, Color: [4]float32{1, 1, 1, 1}, Bounds: rect}
				}
				out = append(out, quad)
				counts.Sprites++
			}
		}
	}
	for _, layer := range layers {
		for _, source := range layer.Sources {
			record, factor := transform(source.World)
			for _, line := range source.Lines {
				if !line.ReflectWater || max(line.ReflectionHeight0, line.ReflectionHeight1) <= 0 {
					continue
				}
				if record <= 0 {
					return nil, counts, fmt.Errorf("nanolathe: water stroke requires recording scale")
				}
				x0, y0, x1, y1 := float32(line.X0), float32(line.Y0)+line.ReflectionHeight0, float32(line.X1), float32(line.Y1)+line.ReflectionHeight1
				dx, dy := x1-x0, y1-y0
				length := float32(math.Hypot(float64(dx), float64(dy)))
				if length < 1 {
					dx, dy, length = 1, 0, 1
				}
				nx, ny := -dy/length, dx/length
				c := palette[line.Index]
				color := [4]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255, 1}
				var quad WaterSource
				for i, v := range [4][3]float32{{x0 + nx, y0 + ny, line.ReflectionHeight0}, {x1 + nx, y1 + ny, line.ReflectionHeight1}, {x1 - nx, y1 - ny, line.ReflectionHeight1}, {x0 - nx, y0 - ny, line.ReflectionHeight0}} {
					quad.Vertices[i] = WaterSourceVertex{PositionHeight: [4]float32{v[0]*factor + source.World.OffsetX, v[1]*factor + source.World.OffsetY, v[2] / record, 2}, Color: color}
				}
				out = append(out, quad)
				counts.Lines++
			}
		}
	}
	return out, counts, nil
}
