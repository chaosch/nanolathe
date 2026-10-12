package client

import (
	"math/rand"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	compiledmodel "github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
)

// TestPickSnapshotUnitReachTestChangesNoPick locks the pick's pre-test as a
// pure optimization: skipping units whose rotated hull cannot reach the
// pointer must pick exactly what testing every hull picks, at rest and off-rest
// zooms, for any orientation and for pointers near and on the hull edges.
func TestPickSnapshotUnitReachTestChangesNoPick(t *testing.T) {
	m := hullTestModel()
	src := UnitHullModelFunc(func(string) *compiledmodel.Model { return m })
	rng := rand.New(rand.NewSource(7))
	units := make([]frame.UnitView, 40)
	for i := range units {
		u := hullTestView()
		u.Slot = pool.Handle(i + 1)
		u.X, u.Y, u.Z = fixedUnits(float64(80+rng.Intn(240))), fixedUnits(float64(rng.Intn(40))), fixedUnits(float64(120+rng.Intn(240)))
		u.Heading, u.Pitch, u.Bank = uint16(rng.Intn(65536)), uint16(rng.Intn(65536)), uint16(rng.Intn(65536))
		units[i] = u
	}
	f := &frame.Frame{Units: units}
	every := func(sx, sy int32, cam *camera.Camera) (pool.Handle, bool) {
		sx, sy = recordPoint(cam, sx, sy)
		best, bestScore := pool.Handle(0), int32(0x7fff0000)
		for _, v := range f.Units {
			corners, ok := hoverHullCorners(m, v, cam)
			if !ok || !containsStrictPolygon(sx, sy, corners[:]) {
				continue
			}
			if score := hoverScore(m, v.FootX, v.FootZ); score < bestScore || score == bestScore && (best == 0 || v.Slot < best) {
				best, bestScore = v.Slot, score
			}
		}
		return best, best != 0
	}
	for _, z := range []camera.Zoom{camera.ZoomUnit, camera.ZoomUnit / 2, camera.ZoomUnit * 7 / 10, camera.ZoomUnit * 2} {
		cam := &camera.Camera{X: 40, Z: 40, ViewW: 1024, ViewH: 768, MapW: 4096, MapH: 4096}
		cam.Zoom, cam.Scale = z, z.Step()
		picks := 0
		for sy := int32(0); sy < 520; sy += 3 {
			for sx := int32(0); sx < 700; sx += 3 {
				h, _, ok := PickSnapshotUnit(f, sx, sy, cam, 0, src)
				wh, wok := every(sx, sy, cam)
				if h != wh || ok != wok {
					t.Fatalf("zoom %s pointer (%d,%d): reach-tested pick %d/%v, exhaustive %d/%v", z, sx, sy, h, ok, wh, wok)
				}
				if ok {
					picks++
				}
			}
		}
		if picks == 0 {
			t.Fatalf("zoom %s: the sweep never hit a unit", z)
		}
	}
}
