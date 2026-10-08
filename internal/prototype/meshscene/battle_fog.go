package meshscene

import (
	"fmt"
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/render"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// Freeze map-load height bytes before the worker starts. GPU height sampling is
// a display approximation; per-instance receivers retain the integer two-stage
// terrain query on these immutable samples [03 §2.3]. Rect places sample centres
// at 0,16,...; its outer edges extend half a cell beyond those centres.
func battleHeightField(t *world.Terrain) HeightField {
	if t == nil || t.CellW <= 0 || t.CellH <= 0 {
		return HeightField{}
	}
	f := HeightField{Width: int(t.CellW), Height: int(t.CellH), Values: make([]float32, int(t.CellW*t.CellH)), Rect: [4]float32{-8, -8, float32(t.CellW)*16 - 8, float32(t.CellH)*16 - 8}}
	for i, p := range t.Plot {
		if i >= len(f.Values) {
			break
		}
		f.Values[i] = float32(p.Height())
	}
	return f
}
func battleGroundHeight(f HeightField, x, z numeric.Fixed) float32 {
	cx, cz := world.WorldToCell(x), world.WorldToCell(z)
	if cx < 0 || cz < 0 || int(cx+1) >= f.Width || int(cz+1) >= f.Height {
		return -1.0 / 65536
	}
	fx := int32((int64(x) - int64(world.CellToWorld(cx))) / 65536)
	fz := int32((int64(z) - int64(world.CellToWorld(cz))) / 65536)
	base := int(cz)*f.Width + int(cx)
	step := func(a, b, fraction int32) int32 {
		d := (b - a) * fraction
		if d < 0 {
			d += 15
		}
		return a + (d >> 4)
	}
	return float32(step(step(int32(f.Values[base]), int32(f.Values[base+1]), fx), step(int32(f.Values[base+f.Width]), int32(f.Values[base+f.Width+1]), fx), fz))
}

func (s *battleSource) prepareFog() error {
	if s.spectator {
		return nil
	}
	gaf, err := formats.LoadGAFFile(s.fs, "anims/fog.gaf")
	if err != nil {
		return err
	}
	for ei := range gaf.Entries {
		entry := &gaf.Entries[ei]
		for i := 0; i < 4; i++ {
			if strings.EqualFold(entry.Name, fmt.Sprintf("Gray%d", i+1)) {
				s.fogGray[i] = entry
			}
			if strings.EqualFold(entry.Name, fmt.Sprintf("Black%d", i+1)) {
				s.fogBlack[i] = entry
			}
		}
	}
	atlas, err := battleFogAtlas(s.fogGray, s.fogBlack, s.palette)
	if err != nil {
		return err
	}
	s.scene.FogAtlas = atlas
	return nil
}

// Retain the production cell operations rather than sampling the authored masks
// on the CPU. The native compositor visits the same ordered 2x2 neighbourhood
// and reads native mask pixels from Scene.FogAtlas (DESIGN_GPU_RENDERER C-G7).
// store, when non-nil, is a texture no reader holds; a changed fog reuses it.
func (s *battleSource) appendFog(cur *frame.Frame, old *battlePublication, store []byte) FogFrame {
	if s.spectator || cur == nil || !cur.Fog.Valid {
		return FogFrame{}
	}
	src := cur.Fog
	dithered := s.scene.DitheredFog
	if old != nil && src.Source != 0 && old.fogSource == src.Source && old.fogVersion == src.Version && old.frame.Fog.Dithered == dithered {
		return old.frame.Fog
	}
	if src.W <= 0 || src.H <= 0 || int64(src.W)*int64(src.H) != int64(len(src.Ch0)) || len(src.Ch1) != len(src.Ch0) {
		return FogFrame{}
	}
	const cell = render.FogTilePixels
	s.fogGeneration++
	dst := FogFrame{
		Width: int(src.W), Height: int(src.H), Dithered: dithered, Generation: s.fogGeneration,
		Rect: [4]float32{float32(src.OriginX*cell + cell/2), float32(src.OriginZ*cell + cell/2), float32((src.OriginX+src.W)*cell + cell/2), float32((src.OriginZ+src.H)*cell + cell/2)},
	}
	if n := len(src.Ch0) * 4; cap(store) >= n {
		dst.RGBA = store[:n]
		clear(dst.RGBA)
	} else {
		dst.RGBA = make([]byte, n)
	}
	for row := 0; row < dst.Height; row++ {
		for col := 0; col < dst.Width; col++ {
			i := row*dst.Width + col
			at := i * 4
			dst.RGBA[at+3] = 255
			black, gray := src.Ch0[i], src.Ch1[i]
			// Solid unexplored cells suppress channel one [03 §3.3].
			if black == 15 {
				dst.RGBA[at+1] = 1
				continue
			}
			variant := render.FogVariant(src.OriginX+int32(col), src.OriginZ+int32(row), nil)
			if gray == 15 {
				dst.RGBA[at] = 1
				if dithered {
					dst.RGBA[at] = 2
				}
			} else if gray >= 1 && gray <= battleFogColumns && battleFogFramePresent(s.fogGray[variant], int(gray)-1, true) {
				slot := byte(variant*battleFogColumns) + gray - 1
				dst.RGBA[at] = 3 + slot
				if dithered {
					dst.RGBA[at] += battleFogSlots
				}
			}
			if black >= 1 && black <= battleFogColumns && battleFogFramePresent(s.fogBlack[variant], int(black)-1, false) {
				dst.RGBA[at+1] = 2 + byte(variant*battleFogColumns) + black - 1
			}
		}
	}
	return dst
}
