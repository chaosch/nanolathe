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
// image; native keyed cargo shares the parent's single opacity resolve.
// Match the existing present walk: a root and its direct published cargo.
// Deeper carrier chains have no independent present in that walk.
func battleUnitPresenter(u frame.UnitView, units map[pool.Handle]*frame.UnitView) (frame.UnitView, bool) {
	if u.Carrier == 0 || u.Carrier == u.Slot {
		return u, true
	}
	parent := units[u.Carrier]
	if parent == nil || (parent.Carrier != 0 && parent.Carrier != parent.Slot) || u.CarriedPiece < 0 {
		return u, false
	}
	for _, slot := range parent.Cargo {
		if slot == u.Slot {
			return *parent, true
		}
	}
	return u, false
}
func battleModelUnitVisible(f *frame.Frame, u frame.UnitView, units map[pool.Handle]*frame.UnitView, spectator bool) bool {
	presenter, ok := battleUnitPresenter(u, units)
	return ok && battleUnitVisible(f, presenter, spectator)
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

// u and feature point into f, so the material request borrows them without a copy.
// posed, when set, is the previous build's instance whose pose this one copied;
// its vertical bounds are this pose's.
func (s *battleSource) applyUnitVisual(pub *battlePublication, u *frame.UnitView, f *frame.Frame, posed *Instance) {
	instance := &pub.frame.Instances[len(pub.frame.Instances)-1]
	// Only the build erase reads the vertical extent, while BuildRemaining is
	// positive, so a finished unit skips the vertex walk. A reused pose shares
	// that state (battlePoseInputsEqual).
	var lo, hi float32
	switch {
	case u.BuildRemaining <= 0:
	case posed != nil:
		lo, hi = posed.Visual.Bounds[0], posed.Visual.Bounds[1]
	default:
		matrices := pub.frame.Current[instance.PoseOffset : instance.PoseOffset+len(s.models[instance.Mesh].Pieces)]
		lo, hi = battleModelBounds(s.models[instance.Mesh], matrices, float32(u.Y)/65536)
	}
	instance.Visual.Bounds = [4]float32{lo, hi, battleGroundHeight(s.scene.HeightField, u.X, u.Z), float32(u.Y) / 65536}
	// Unknown team colours omit the textured primitive [03 R-RAST-01 §3].
	instance.Visual.State[0] = -1
	s.applyModelMaterials(instance, ModelMaterialRequest{Frame: f, Unit: u})
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
func (s *battleSource) applyFeatureVisual(pub *battlePublication, feature *frame.FeatureView, f *frame.Frame) {
	instance := &pub.frame.Instances[len(pub.frame.Instances)-1]
	// 3DO features use the pseudo-unit's player-zero logo [03 R-RAST-01 §3].
	instance.Visual.State[0] = -1
	s.applyModelMaterials(instance, ModelMaterialRequest{Frame: f, Feature: feature})
	if f.Players[0].Present {
		instance.Visual.State[0] = float32(f.Players[0].Logo)
	}
	instance.Visual.Bounds[3] = float32(feature.Y) / 65536
	instance.Visual.Bounds[2] = battleGroundHeight(s.scene.HeightField, feature.X, feature.Z)
	if feature.Y >= f.Visibility.SeaLevel {
		instance.Visual.State[3] = 1
	}
	if !s.effects.WreckGlow || !feature.WreckHeatKnown || feature.Y < f.Visibility.SeaLevel || !battlePointVisible(f, feature.X, feature.Y, feature.Z, s.spectator) {
		return
	}
	age := float32(f.Tick - feature.WreckBornTick)
	if age >= 180 {
		return
	}
	instance.CoolingAge = age + 1
	// Reuse Enhanced's cooling policy (DESIGN_GPU_RENDERER §28),
	// without adding a random draw or assigning a birth to unmarked instances.
	// Packing samples the fractional display age from this immutable metadata.
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

// Pass selection follows the presenting carrier's committed mode. Keyed cargo
// shares its parent's final cloak resolve [03 R-RAST-01 §7][03 R-REN-03D §4].
// Native subject depth still approximates the staged byte-height composition.
func (s *battleSource) applyUnitSubject(pub *battlePublication, u frame.UnitView, units map[pool.Handle]*frame.UnitView) {
	instance := &pub.frame.Instances[len(pub.frame.Instances)-1]
	presenter, _ := battleUnitPresenter(u, units)
	subject := u
	instance.Visual.State[1] = 1
	if u.Cloaked {
		instance.Visual.State[1] = 0.5
	}
	if u.Carrier != 0 && u.Carrier != u.Slot {
		if _, resolved := s.modelIndices[s.modelKeys.key(presenter.Model)]; resolved {
			// Keyless/direct cargo writes ordinary opaque polygons [03 R-REN-03A §4].
			instance.Visual.State[1] = 1
			if presenter.ZBuffer || presenter.BuildRemaining > 0 || presenter.Digger {
				subject = presenter
				if presenter.Cloaked {
					instance.Visual.State[1] = 0.5
				}
				for order, slot := range presenter.Cargo {
					if slot == u.Slot {
						instance.SubjectOrder = uint32(order + 1)
						break
					}
				}
			}
		}
	}
	if presenter.MoverMode != 1 {
		instance.Phase = PhaseAir
	}
	instance.Subject = subject.InstanceID
}

// InterpolatedVisual samples only presentation cooling; the publication remains
// immutable and no simulation clock or state is touched (GPU design §28).
func (i Instance) InterpolatedVisual(alpha float32) ModelVisual {
	v := i.Visual
	if i.CoolingAge > 0 {
		age := max(i.CoolingAge+min(max(alpha, 0), 1)-1, 0)
		flash, cool := max(1-age/6, 0), max(1-age/180, 0)
		red, amber := cool*cool, cool*cool*cool*cool
		v.Emission = [4]float32{.75*red + .15*flash, .20*amber + .55*flash, .015*amber + .42*flash, 1}
	}
	return v
}
