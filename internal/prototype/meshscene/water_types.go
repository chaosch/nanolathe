package meshscene

import "github.com/nanolathe-gg/nanolathe/internal/drawlist"

// WaterUniform is five sequential float4 groups shared with native/water.metal.
// Phase is seconds, integrated tidal X/Z and observed foam energy. Mask is
// projected mask step, width, height and surface-pass admission. Controls is
// motion, shore foam, shading (0 off, 1 water, 2 no tint) and whether water
// lies within one block of the view (set by the host).
// TerrainRect is the full painted-map texture extent. SampleRect bounds the
// viewport at physical pixel centres in painted-map coordinates, retaining
// production's displacement clamp. These values are presentation-only (§26).
type WaterUniform struct {
	Phase, Mask, Controls, TerrainRect, SampleRect [4]float32
}

// NewWaterUniform translates the production surface record and independent
// Effects lanes without observing a session or advancing any history (§30).
// Damaging means both authored water-damage fields are nonzero, as in the
// production surface pass; it suppresses shore foam but retains water colour.
func NewWaterUniform(surface drawlist.WaterSurface, effects drawlist.Effects, lava, damaging bool, maskWidth, maskHeight, maskStep int, terrainRect, sampleRect [4]float32) WaterUniform {
	u := WaterUniform{
		Phase:       [4]float32{(float32(surface.Tick) + float32(surface.Fraction16)/65536) / 30, surface.TidalDriftX, surface.TidalDriftZ, surface.Energy},
		Mask:        [4]float32{float32(maskStep), float32(maskWidth), float32(maskHeight), 0},
		TerrainRect: terrainRect, SampleRect: sampleRect,
	}
	if effects.WaterMotion {
		u.Controls[0] = 1
	}
	if effects.WaterFoam && !lava && !damaging {
		u.Controls[1] = 1
	}
	if effects.WaterSurface {
		u.Controls[2] = 1
		if lava {
			u.Controls[2] = 2
		}
	}
	if surface.Enabled && maskWidth > 0 && maskHeight > 0 && maskStep > 0 && (u.Controls[0] != 0 || u.Controls[1] != 0 || u.Controls[2] != 0) {
		u.Mask[3] = 1
	}
	return u
}
