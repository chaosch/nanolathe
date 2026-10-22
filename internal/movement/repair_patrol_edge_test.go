package movement

import (
	"reflect"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// Nanolathe Modern policy: DESIGN_MOVEMENT_PATH "Modern construction patrol
// edge arrival". The default half-unit horizontal tolerance stays inclusive;
// only the impossible altitude match at a stamped off-map waypoint is waived.
func TestRepairPatrolEdgeArrival(t *testing.T) {
	for _, mode := range []struct {
		name   string
		rules  Rules
		modern bool
	}{{"unbound", nil, false}, {"strict", StrictRules{}, false}, {"community", CommunityRules{}, false}, {"modern", &ModernRules{}, true}} {
		for _, point := range []struct {
			name   string
			x, z   int64
			offMap bool
		}{{"west", -16, 128, true}, {"east", 512, 128, true}, {"north", 128, -16, true}, {"south", 128, 512, true}, {"interior", 128, 128, false}} {
			t.Run(mode.name+"/"+point.name, func(t *testing.T) {
				sys, _, u := airFixture(t)
				sys.Rules = mode.rules
				sys.SetMoverMode(u, 2)
				if !sys.PlaceUnit(orders.PlaceRequest{Unit: u.Handle, X: numeric.FixedFromInt(point.x), Y: numeric.FixedFromInt(120), Z: numeric.FixedFromInt(point.z)}) || sys.airOffMap(u) != point.offMap {
					t.Fatal("fixture: incorrect stamped sector")
				}
				n := pushAirOrder(t, u, "VTOL_RepairPatrol", u.X, u.Z)
				n.Phase, n.DynamicGate, n.Deadline = 1, airLegGate|1, 45
				m := sys.newPointMarker(u, Vec3{X: u.X, Z: u.Z})
				m.setAltitudeOffset(int16(u.Def.CruiseAlt))
				sys.installAirGoal(u, n, m)
				random := *orders.QueueForUnit(u).Binding().SimRNG
				before := *n
				for _, delta := range []numeric.Fixed{0, 1 << 15, (1 << 15) + 1} {
					u.X = m.goal.X + delta
					want := mode.modern && point.offMap && delta <= 1<<15
					if got := m.Arrived(u); got != want {
						t.Fatalf("horizontal offset=%d arrived=%v, want %v", delta, got, want)
					}
				}
				u.X, u.Y = m.goal.X, m.goal.Y
				if !m.Arrived(u) {
					t.Fatal("ordinary altitude-matched arrival failed")
				}
				if *orders.QueueForUnit(u).Binding().SimRNG != random || !reflect.DeepEqual(*n, before) || sys.AirGoalPayload(u.Handle) != m {
					t.Fatal("arrival query changed RNG, order state or payload ownership")
				}
			})
		}
	}
}

func TestRepairPatrolEdgeArrivalExcludesOtherLegs(t *testing.T) {
	for _, leg := range []struct {
		name      string
		row       string
		phase     uint8
		gate      uint32
		offset    int16
		goalDelta numeric.Fixed
	}{{"takeoff", "VTOL_RepairPatrol", 1, airLegGate, 30, 0},
		{"zero-altitude takeoff", "VTOL_RepairPatrol", 1, airLegGate, 0, 0},
		{"setup", "VTOL_RepairPatrol", 0, airLegGate | 1, 60, 0},
		{"different destination", "VTOL_RepairPatrol", 1, airLegGate | 1, 60, 1},
		{"repair", "VTOL_RepairUnit", 2, airLegGate | 1, 60, 0},
		{"assistance", "VTOL_HelpBuild", 3, airLegGate | 1, 60, 0},
		{"return move", "VTOL_Move", 2, airLegGate | 1, 60, 0}} {
		t.Run(leg.name, func(t *testing.T) {
			sys, _, u := airFixture(t)
			sys.Rules = &ModernRules{}
			sys.SetMoverMode(u, 2)
			sys.PlaceUnit(orders.PlaceRequest{Unit: u.Handle, X: -16 << 16, Y: 120 << 16, Z: 128 << 16})
			if leg.name == "zero-altitude takeoff" {
				u.Def.CruiseAlt = 0
			}
			n := pushAirOrder(t, u, leg.row, u.X+leg.goalDelta, u.Z)
			n.Phase, n.DynamicGate = leg.phase, leg.gate
			m := sys.newPointMarker(u, Vec3{X: u.X, Z: u.Z})
			m.setAltitudeOffset(leg.offset)
			sys.installAirGoal(u, n, m)
			if m.Arrived(u) {
				t.Fatal("non-waypoint leg bypassed altitude arrival")
			}
		})
	}
}
