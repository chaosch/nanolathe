package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// Whole-piece debris keeps the authored vertex buffer but detaches its former
// hierarchy, as render.BuildDebrisModelPieceInto does [04 R-COB-04 §2]
// [03 R-COMP-02 §6]. Published debris slots lack an allocation identity, so this
// adapter snaps at committed ticks instead of blending a reused slot.
func (s *battleSource) appendDetachedDebris(pub *battlePublication, current *frame.Frame, camera [3]float32, spectator bool) int {
	count := 0
	for _, v := range current.Debris {
		if !battlePointVisible(current, v.X, v.Y, v.Z, spectator) || !battleInView(v.X, v.Y, v.Z, camera, s.viewport) {
			continue
		}
		mesh, ok := s.modelIndices[battleModelKey(v.Model)]
		if !ok {
			s.noteMissing(v.Model)
			continue
		}
		m := s.models[mesh]
		if v.PieceIndex < 0 || v.PieceIndex >= len(m.Pieces) {
			continue
		}
		span := s.beginInstance(pub, battleEntityKey{kind: 4, id: uint64(v.Slot)}, mesh)
		now := pub.frame.Current[span.offset : span.offset+span.count]
		clear(now)
		piece := m.Pieces[v.PieceIndex]
		piece.Parent = -1
		piece.Children = nil
		piece.Translate = [3]numeric.Fixed{}
		single := model.Model{Name: m.Name, Root: 0, Pieces: []model.Piece{piece}}
		scratch := &s.poseScratch
		scratch.reserve(1)
		scratch.states[0] = model.PieceState{RotX: v.Angles[0], RotY: v.Angles[1], RotZ: v.Angles[2]}
		scratch.transforms[0] = model.ComposeInto(&single, scratch.states, 0, scratch.transforms[0], &scratch.compose)
		now[v.PieceIndex] = battleAffine(scratch.transforms[0], v.X, v.Y, v.Z)
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
