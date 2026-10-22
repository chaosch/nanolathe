//go:build retail

package session

import (
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// Issue 152: stock construction patrols can finish work beside the map edge,
// then stop over it waiting for altitude control that the off-map stamp disables.
// Nanolathe Modern policy: DESIGN_MOVEMENT_PATH "Modern construction patrol
// edge arrival". Observe paid construction and onward travel on ordinary ticks.
func TestModernConstructorPatrolContinuesAtMapEdgeRetail(t *testing.T) {
	fixture := loadRetailFixture(t)
	fixture.cfg.Gameplay = gameplay.Modern
	s := fixture.session(t)
	f := &constructorPatrolRuntime{t: t, s: s, patrol: "VTOL_RepairPatrol", energyStock: 100000, metalStock: 100000}
	for i := 0; i < 5; i++ {
		f.step()
	}
	at := numeric.FixedFromInt
	f.helper = placeCompleteRetailUnit(t, s, "corca", 0, at(600), at(600))
	s.Movement.EnsureUnit(f.helper)
	s.bindOrderQueue(f.helper)
	f.q = orders.QueueForUnit(f.helper)
	f.q.CancelAll()
	s.Movement.SetMoverMode(f.helper, 2)
	if !s.Movement.PlaceUnit(orders.PlaceRequest{Unit: f.helper.Handle, X: f.helper.X, Y: f.helper.Y + at(int64(f.helper.Def.CruiseAlt)), Z: f.helper.Z}) {
		t.Fatal("fixture: airborne constructor placement failed")
	}
	f.order(f.helper, 9, 0, 0, at(600), false)
	f.step()
	if constructorPatrolName(f.q.Head()) != f.patrol || f.q.PrimaryLen() != 2 {
		t.Fatal("fixture: patrol did not establish both waypoints")
	}
	edge, home := f.q.PrimaryAt(0), f.q.PrimaryAt(1)
	edgeGoal := constructorPatrolPoint{edge.GoalX, edge.GoalY, edge.GoalZ}
	homeGoal := constructorPatrolPoint{home.GoalX, home.GoalY, home.GoalZ}
	def, ok := s.Catalog.Unit("corfus")
	if !ok {
		t.Fatal("fixture: core fusion reactor absent")
	}
	h, err := s.Units.CreateNanoframe(def, 0, at(64), s.World.HeightAt(at(64), at(600)), at(600))
	if err != nil {
		t.Fatal(err)
	}
	target := s.Units.Unit(h)
	// Only the initial remainder is staged; the ordinary work service owns
	// every subsequent fraction, payment and completion callback.
	target.Remaining = .002
	target.Health = int32(float32(def.MaxDamage) * (1 - target.Remaining))
	s.Movement.EnsureUnit(target)
	paid := f.observeWork(target)
	var completed, resumed uint32
	for i := 0; i < 2000; i++ {
		f.step()
		if target.Remaining == 0 && completed == 0 {
			completed = s.Clock.GlobalTick
		}
		if completed != 0 && f.q.Head() == home && f.helper.X > at(192) {
			resumed = s.Clock.GlobalTick
			break
		}
	}
	if completed == 0 || resumed == 0 || paid.assist == 0 || paid.energy <= 0 || paid.metal <= 0 {
		n := f.q.Head()
		t.Fatalf("no paid completion and onward patrol: completed=%d resumed=%d paid=%+v position=(%d,%d,%d) head=%s phase=%d gate=%x", completed, resumed, paid, f.helper.X.Floor(), f.helper.Y.Floor(), f.helper.Z.Floor(), constructorPatrolName(n), n.Phase, n.DynamicGate)
	}
	if f.q.PrimaryLen() != 2 || f.q.PrimaryAt(1) != edge ||
		(constructorPatrolPoint{edge.GoalX, edge.GoalY, edge.GoalZ}) != edgeGoal ||
		(constructorPatrolPoint{home.GoalX, home.GoalY, home.GoalZ}) != homeGoal {
		t.Fatal("edge recovery changed the retained patrol route")
	}
	t.Logf("fusion completed at tick %d after %d paid visits; onward patrol at tick %d", completed, paid.assist, resumed)
}
