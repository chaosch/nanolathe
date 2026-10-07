package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/visibility"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// Same committed point projection as client.SnapshotPointVisible [03 §3.2].
// Keeping the small adapter here avoids initializing Ebitengine in this host.
func battlePointVisible(f *frame.Frame, x, y, z numeric.Fixed, spectator bool) bool {
	if spectator {
		return true
	}
	if f == nil || !f.Visibility.Valid || f.ViewingPlayer >= 10 {
		return false
	}
	v := f.Visibility
	if v.W <= 0 || v.H <= 0 {
		return false
	}
	px, py, pz := int32(int16(int64(x)>>16)), int32(int16(int64(y)>>16)), int32(int16(int64(z)>>16))
	col, row := px>>5, (pz-(py>>1))>>5
	if col < 0 || row < 0 || col >= v.W || row >= v.H {
		return false
	}
	n := int64(v.W) * int64(v.H)
	i := int(row*v.W + col)
	if v.CoverageBytes && int64(len(v.Visible)) == n {
		return v.Visible[i] != 0
	}
	return int64(len(v.WordVisible)) == n && v.WordVisible[i]&(uint16(1)<<f.ViewingPlayer) != 0
}
func battleUnitVisible(f *frame.Frame, u frame.UnitView, spectator bool) bool {
	if spectator {
		return true
	}
	if f == nil || f.ViewingPlayer >= 10 {
		return false
	}
	if u.Owner == f.ViewingPlayer {
		return true
	}
	if !f.Visibility.Valid {
		return false
	}
	if u.DirectVisibilityKnown {
		return u.DirectlyVisible
	}
	var status uint32
	if u.UnderwaterExempt {
		status = visibility.SonarBit
	}
	t := visibility.Target{Owner: visibility.PlayerID(u.Owner), X: u.X + u.HullOffsetX, Y: u.Y + u.HullOffsetY, Z: u.Z + u.HullOffsetZ, XExtent: u.HullXExtent, YExtent: u.HullYExtent, ZExtent: u.HullZExtent, Hidden: u.Cloaked, Status: status}
	return t.IsVisible(visibility.PlayerID(f.ViewingPlayer), f.Visibility.SeaLevel, func(x, y, z numeric.Fixed) bool { return battlePointVisible(f, x, y, z, false) })
}
func battleFeatureVisible(f *frame.Frame, v frame.FeatureView, spectator bool) bool {
	if spectator {
		return true
	}
	if f == nil {
		return false
	}
	if !v.NoDrawUnderGray {
		return true
	}
	if v.Height >= 10 && v.OwnerKnown && v.Owner == world.MapOwnedFeaturePlacer {
		return true
	}
	if f.ViewingPlayer >= 10 {
		return false
	}
	if v.OwnerKnown && v.Owner < 10 && v.Owner == f.ViewingPlayer {
		return true
	}
	return battlePointVisible(f, world.CellToWorld(v.CX), v.Y, world.CellToWorld(v.CZ), false) || battlePointVisible(f, world.CellToWorld(v.CX+int32(v.FootX)), v.Y, world.CellToWorld(v.CZ+int32(v.FootZ)), false)
}
