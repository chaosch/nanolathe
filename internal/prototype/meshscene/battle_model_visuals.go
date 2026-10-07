package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/render"
)

// Attachments use their already committed world pose [04 R-UNIT-06 §3]. Their
// draw admission belongs to the carrier and piece-less cargo is suppressed
// [03 R-RAST-01 §7]. Native shared depth approximates the carrier's staged key
// image; it does not apply the parent's ALP once to a flattened child composite.
func battleModelUnitVisible(f *frame.Frame, u frame.UnitView, units map[pool.Handle]*frame.UnitView, spectator bool) bool {
	for depth := 0; u.Carrier != 0 && u.Carrier != u.Slot; depth++ {
		if u.CarriedPiece < 0 || depth >= len(units) {
			return false
		}
		parent := units[u.Carrier]
		if parent == nil {
			return false
		}
		u = *parent
	}
	return battleUnitVisible(f, u, spectator)
}

func battleModelBounds(m *model.Model, matrices []Mat4, originY float32) (float32, float32) {
	lo, hi := originY, originY
	for pi, p := range m.Pieces {
		if pi >= len(matrices) || matrices[pi][15] == 0 {
			continue
		}
		mat := matrices[pi]
		for _, v := range p.Vertices {
			y := mat[1]*float32(v[0])/65536 + mat[5]*float32(v[1])/65536 + mat[9]*float32(v[2])/65536 + mat[13]
			lo, hi = min(lo, y), max(hi, y)
		}
	}
	return lo, hi
}
func (s *battleSource) applyUnitVisual(pub *battlePublication, u frame.UnitView, f *frame.Frame) {
	instance := &pub.frame.Instances[len(pub.frame.Instances)-1]
	matrices := pub.frame.Current[instance.PoseOffset : instance.PoseOffset+len(s.models[instance.Mesh].Pieces)]
	lo, hi := battleModelBounds(s.models[instance.Mesh], matrices, float32(u.Y)/65536)
	instance.Visual.Bounds = [4]float32{lo, hi, battleGroundHeight(s.scene.HeightField, u.X, u.Z), float32(u.Y) / 65536}
	// The backend clamps logo entry frame ranges. Unknown colours deliberately
	// retain frame zero in this prototype; retail omits that texture [03 R-RAST-01 §3].
	if u.OwnerColorKnown {
		instance.Visual.State[0] = float32(u.OwnerColor)
	}
	instance.Visual.State[2] = max(0, min(u.BuildRemaining, 1))
	if u.Cloaked {
		instance.Visual.State[1] = 0.5
	}
	// Both host shadow categories are enabled in this experiment. Structure and
	// keyed Digger branches bypass canhover/floater [03 R-REN-03D §1].
	keyed := u.ZBuffer || u.BuildRemaining > 0
	if !u.NoShadow && ((u.Digger && keyed) || (!u.Digger && !u.BMCode) || (!u.CanHover && !u.Floater)) {
		instance.Visual.State[3] = 1
	}
	if u.BuildRemaining > 0 {
		_, outline := render.NanoframePulse(uint16(u.Slot), f.Tick)
		c := s.palette.Base[outline]
		instance.Visual.Outline = [4]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255, 1}
	}
}
func (s *battleSource) applyFeatureVisual(pub *battlePublication, feature frame.FeatureView, f *frame.Frame) {
	instance := &pub.frame.Instances[len(pub.frame.Instances)-1]
	// 3DO features use the pseudo-unit's player-zero logo [03 R-RAST-01 §3].
	if f.Players[0].Present {
		instance.Visual.State[0] = float32(f.Players[0].Logo)
	}
	instance.Visual.Bounds[3] = float32(feature.Y) / 65536
	instance.Visual.Bounds[2] = battleGroundHeight(s.scene.HeightField, feature.X, feature.Z)
	if feature.Y >= f.Visibility.SeaLevel {
		instance.Visual.State[3] = 1
	}
	if !feature.WreckHeatKnown || feature.Y < f.Visibility.SeaLevel || !battlePointVisible(f, feature.X, feature.Y, feature.Z, s.spectator) {
		return
	}
	age := float32(f.Tick - feature.WreckBornTick)
	if age >= 180 {
		return
	}
	// Reuse Enhanced's committed-age cooling policy (DESIGN_GPU_RENDERER §28),
	// without adding a random draw or assigning a birth to unmarked instances.
	flash := max(1-age/6, 0)
	cool := max(1-age/180, 0)
	red, amber := cool*cool, cool*cool*cool*cool
	instance.Visual.Emission = [4]float32{0.75*red + 0.15*flash, 0.20*amber + 0.55*flash, 0.015*amber + 0.42*flash, 1}
}

// Static neutral model bounds size the wreck distortion plume. Heading/pose
// changes remain an explicit approximation; no guessed fixed sprite size.
func (s *battleSource) featureModelBounds() map[string][4]float32 {
	bounds := make(map[string][4]float32, len(s.models))
	for _, m := range s.models {
		transforms := make([]Mat4, len(m.Pieces))
		s.poseScratch.unitInto(transforms, m, frame.UnitView{})
		var loX, loY, hiX, hiY float32
		for pi, p := range m.Pieces {
			mat := transforms[pi]
			if mat[15] == 0 {
				continue
			}
			for _, v := range p.Vertices {
				x := mat[0]*float32(v[0])/65536 + mat[4]*float32(v[1])/65536 + mat[8]*float32(v[2])/65536 + mat[12]
				y := mat[1]*float32(v[0])/65536 + mat[5]*float32(v[1])/65536 + mat[9]*float32(v[2])/65536 + mat[13]
				z := mat[2]*float32(v[0])/65536 + mat[6]*float32(v[1])/65536 + mat[10]*float32(v[2])/65536 + mat[14]
				projected := z - y/2
				loX, hiX = min(loX, x), max(hiX, x)
				loY, hiY = min(loY, projected), max(hiY, projected)
			}
		}
		bounds[battleModelKey(m.Name)] = [4]float32{loX, loY, hiX - loX, hiY - loY}
	}
	return bounds
}
