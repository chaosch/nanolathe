package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
)

// Whole-piece debris keeps the authored vertex buffer but detaches its former
// hierarchy, as render.BuildDebrisModelPieceInto does [04 R-COB-04 §2]
// [03 R-COMP-02 §6]. Published debris slots lack an allocation identity, so this
// adapter snaps at committed ticks instead of blending a reused slot.
func (s *battleSource) appendDetachedDebris(pub *battlePublication, current *frame.Frame, camera [3]float32, spectator bool) int {
	count := 0
	for _, v := range current.Debris {
		if !battlePointVisible(current, v.X, v.Y, v.Z, spectator) || !battleInView(v.X, v.Y, v.Z, camera, s.viewport, s.cullSlack) {
			continue
		}
		mesh, ok := s.modelIndices[s.modelKeys.key(v.Model)]
		if !ok {
			s.noteMissing(v.Model)
			continue
		}
		m := s.models[mesh]
		if v.PieceIndex < 0 || v.PieceIndex >= len(m.Pieces) {
			continue
		}
		span := s.beginInstance(pub, battleEntityKey{kind: 4, id: uint64(v.Slot)}, mesh)
		pub.frame.Instances[len(pub.frame.Instances)-1].Phase = PhaseEffects
		now := pub.frame.Current[span.offset : span.offset+span.count]
		clear(now)
		now[v.PieceIndex] = battleRotationMatrix(model.PieceState{RotX: v.Angles[0], RotY: v.Angles[1], RotZ: v.Angles[2]}, v.X, v.Y, v.Z)
		copy(pub.frame.Previous[span.offset:span.offset+span.count], now)
		visual := &pub.frame.Instances[len(pub.frame.Instances)-1].Visual
		visual.State[1] = 1
		if v.OwnerColorKnown {
			visual.State[0] = float32(v.OwnerColor)
		}
		count++
	}
	return count
}
