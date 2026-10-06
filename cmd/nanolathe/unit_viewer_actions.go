package main

import (
	"fmt"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/combat"
	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/movement"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// Each action below issues the callbacks a battle producer would, in its
// established order, against the preview's detached unit state
// (DESIGN_DEVELOPER_TOOLS §7). None connects a world, allocator, RNG stream,
// projectile, sound, effect or debris sink.

// moveRate issues the movement-rate classifier's callbacks for one tier
// change: StopMoving into tier 0, StartMoving then MoveRateN out of it, and
// MoveRateN alone otherwise, each an immediate wake start [04 §5.2].
func (a *unitViewerAnimation) moveRate(prev, next int) {
	b := a.script.bridge
	for _, kind := range cob.MoveRateTransition(prev, next) {
		switch kind {
		case cob.CallbackStartMoving:
			b.StartMoving()
		case cob.CallbackStopMoving:
			b.StopMoving()
		case cob.CallbackMoveRate1:
			b.MoveRate1()
		case cob.CallbackMoveRate2:
			b.MoveRate2()
		case cob.CallbackMoveRate3:
			b.MoveRate3()
		}
	}
}

// unitViewerMover is the preview's ground speed word. The preview route is a
// straight level leg toward a distant goal, so the steering step always
// accelerates, the pitch-table cap is MaxVelocity itself and nothing turns or
// blocks [04 R-MOV-01 §4].
type unitViewerMover struct {
	speed int32
	tier  int
	done  bool
}

func (a *unitViewerAnimation) moveStep() {
	m := &a.move
	if a.action != unitViewerMoving || m.done {
		return
	}
	def := a.def
	m.speed = max(0, m.speed+def.Acceleration)
	if def.MaxVelocity < m.speed {
		m.speed = def.MaxVelocity
	}
	tier := cob.MoveRateCategory(false, false, m.speed, 0, def.MoveRate1, def.MoveRate2)
	a.moveRate(m.tier, tier)
	m.tier = tier
	switch {
	case def.Acceleration <= 0 || def.MaxVelocity <= 0:
		m.done, a.note = true, "Move / definition never accelerates"
	case m.speed == def.MaxVelocity:
		m.done, a.note = true, fmt.Sprintf("Move / full speed / tier %d", tier)
	default:
		a.note = fmt.Sprintf("Move / accelerating / tier %d", tier)
	}
}

// unitViewerBuild is the preview's construction order. A mobile builder takes
// the work handlers' path: the slot-form StartBuilding with the relative
// bearing, then the build-stance wait, then one QueryNanoPiece for each
// accepted work step [05 R-P0-06 §1][05 R-P0-06 §2]. A factory takes the
// production state machine: raise activation, wait for the stance, query the
// pad, raise the building edge, then the same per-step query
// [05 "Factory production lifecycle"]. Every work step is assumed admitted.
//
// A factory's order builds its product cycle: each product starts as a fresh
// nanoframe on the pad when the building edge rises, falls one construction
// step per preview tick (times the preview speed), and at zero completes, the
// building edge falls, and the completed product holds the pad until the next
// one starts (DESIGN_DEVELOPER_TOOLS §7).
type unitViewerBuild struct {
	factory  bool
	state    unitViewerBuildState
	request  int // +1 start, -1 stop; served by the next order visit
	since    uint64
	nano     int  // model piece of the last nano query, or -1
	pad      int  // model piece of the factory build-info query, or -1
	origin   bool // the build-info answer hangs the product at the factory origin
	speed    int  // construction steps per preview tick
	products *unitViewerProducts
}

type unitViewerBuildState uint8

const (
	unitViewerBuildIdle unitViewerBuildState = iota
	unitViewerBuildWaiting
	unitViewerBuildWorking
	unitViewerBuildHolding // a completed product holds the pad
)

// building reports whether the preview is carrying construction work: the
// stance has been reached and each tick issues a nano query.
func (a *unitViewerAnimation) building() bool {
	return a != nil && a.build.state == unitViewerBuildWorking
}

// nanoPiece is the model piece of the latest QueryNanoPiece answer, or -1
// while no work step is running. The script piece is mapped through the
// strict name link to the model's piece table.
func (a *unitViewerAnimation) nanoPiece() int {
	if !a.building() {
		return -1
	}
	return a.build.nano
}

// padPiece is the factory's QueryBuildInfo piece in the model's piece table,
// or -1 for a mobile builder, a factory that is not building, or an answer
// that names no model piece.
func (a *unitViewerAnimation) padPiece() int {
	if !a.building() || !a.build.factory {
		return -1
	}
	return a.build.pad
}

// buildEngaged reports a started construction order that has not been
// stopped, including one still waiting for its stance.
func (a *unitViewerAnimation) buildEngaged() bool {
	if a == nil || a.action != unitViewerBuilding || a.stopped {
		return false
	}
	if a.build.request != 0 {
		return a.build.request > 0
	}
	return a.build.state != unitViewerBuildIdle
}

// toggleBuild queues a stop for an engaged order, or a fresh start, for the
// next order visit.
func (a *unitViewerAnimation) toggleBuild() {
	if a == nil || a.action != unitViewerBuilding || a.stopped {
		return
	}
	if a.buildEngaged() {
		a.build.request = -1
	} else {
		a.build.request = 1
	}
}

func (a *unitViewerAnimation) buildStep() {
	g := &a.build
	if a.action != unitViewerBuilding {
		return
	}
	b := a.script.bridge
	switch {
	case g.request < 0 && g.state != unitViewerBuildIdle:
		if g.factory {
			// Completion lowers the building edge; the drained queue then
			// lowers activation in the same pump pass. An unfinished
			// product is discarded; particles already in flight finish.
			a.script.setEdge(unitViewerBuildBit, false)
			a.script.setEdge(unitViewerActivated, false)
			if g.products != nil {
				g.products.cancel()
			}
		} else {
			// The slot-form StopBuilding writes four zero cells
			// [04 R-CB-01 §2][R-ORDER-02 §2].
			b.DeferredArgs("StopBuilding", 0, [4]int32{}, nil)
		}
		g.state, g.nano, g.pad, g.origin = unitViewerBuildIdle, -1, -1, false
		a.note = "Build / stopped"
	case g.request > 0 && g.state == unitViewerBuildIdle:
		if g.factory {
			// State 0 raises activation; state 1 then tests the stance in
			// the same pass.
			a.script.setEdge(unitViewerActivated, true)
		} else {
			b.StartBuildingHeading(unitViewerBuildHeading)
		}
		g.state, g.since = unitViewerBuildWaiting, a.ticks
		a.note = "Build / waiting for build stance"
	}
	g.request = 0
	if g.state == unitViewerBuildHolding {
		// The completed product leaves after the viewer's hold; the order
		// then restarts at its stance test with activation still raised, so
		// no second Activate runs [05 "Factory production lifecycle"].
		c := g.products.current
		if c != nil && c.failed != nil {
			g.products.fail(c)
			c = nil
		}
		if c != nil && c.hold > 1 {
			c.hold--
			a.note = a.productStatus()
			return
		}
		g.products.current = nil
		g.state, g.since = unitViewerBuildWaiting, a.ticks
	}
	if g.state == unitViewerBuildWaiting {
		// An aircraft builder polls the stance and discards the verdict
		// [05 R-P0-06 §1]; every other builder waits for the script's level.
		if a.script.stance&unitViewerStance == 0 && !(a.def.CanFly && !g.factory) {
			if a.ticks-g.since >= unitViewerStanceTimeout {
				a.stop("build stance not reached within 10 seconds")
			}
			return
		}
		if g.factory {
			// The answer reaches the carrier as one signed byte: 128 and
			// above, and the unanswered -1, hang the product at the factory
			// origin [04 R-FAC-02 §1].
			hang := int8(uint8(b.QueryBuildInfo().QueryValue()))
			g.origin = hang < 0
			g.pad = -1
			if !g.origin {
				g.pad = a.script.modelPiece(int32(hang))
				// A script piece at or beyond the model's piece count has no
				// record, and the piece locator answers the unit's own
				// position for it [04 R-COB-01 §3][04 R-REV-02].
				if g.pad < 0 && a.geometry != nil && int(hang) >= len(a.geometry.Pieces) {
					g.origin = true
				}
			}
			if g.products != nil {
				g.products.start(a.def.WorkerTime)
			}
			a.script.setEdge(unitViewerBuildBit, true)
		}
		g.state = unitViewerBuildWorking
	}
	if g.state == unitViewerBuildWorking {
		a.workStep()
	}
}

// workStep is one state-3 visit: the product's construction steps, then for
// accepted work the synchronous QueryNanoPiece and one spray record, then
// completion when the fraction reaches zero [05 R-P0-06 §6]. A factory
// without a product keeps the stance and nano query alone; nothing is
// sprayed without a target.
func (a *unitViewerAnimation) workStep() {
	g := &a.build
	var c *unitViewerProduct
	if g.products != nil {
		c = g.products.current
		if c != nil && c.failed != nil {
			// The renderer refused the product: report it and start the next.
			g.products.fail(c)
			c = g.products.start(a.def.WorkerTime)
		}
	}
	if c != nil && !c.work(construction.WorkerQuantum(a.def.WorkerTime), g.speed) {
		// A zero quantum or a stored zero commits nothing: no query, no
		// segment [05 R-P0-06 §1].
		g.nano = -1
		a.note = a.productStatus()
		return
	}
	g.nano = a.script.modelPiece(a.script.bridge.QueryNanoPiece().QueryValue())
	a.emitSpray(c)
	switch {
	case c != nil && c.remaining == 0:
		// Completion: the product's own activation, then the building edge
		// falls [04 R-FAC-02 §3]; the product holds the pad.
		if c.def.ActivateWhenBuilt && c.anim != nil && c.anim.script != nil && !c.anim.invalid {
			c.anim.setActivation(true)
		}
		a.script.setEdge(unitViewerBuildBit, false)
		c.hold = unitViewerProductHold
		g.state = unitViewerBuildHolding
		a.note = a.productStatus()
	case g.products != nil:
		a.note = a.productStatus()
	case g.factory:
		a.note = "Build / pad " + a.pieceName(a.padPiece()) + " / nano " + a.pieceName(a.nanoPiece())
	default:
		a.note = "Build / nano " + a.pieceName(a.nanoPiece())
	}
}

// emitSpray submits one spray record for an accepted step: from the nano
// piece into the product's box on its pad, or, for a mobile builder, into
// the stand-in work target.
func (a *unitViewerAnimation) emitSpray(c *unitViewerProduct) {
	g := &a.build
	poses := a.script.poses()
	src, ok := unitViewerRootLocal(a.geometry, poses, g.nano)
	if !ok {
		return
	}
	var lo, hi [3]numeric.Fixed
	switch {
	case c != nil:
		at := [3]numeric.Fixed{}
		if !g.origin {
			// An answer below the model's piece count but past the
			// script's declared pieces addresses the record the link pass
			// left in that slot [04 R-COB-01 §3]; cob.LinkPieces maps only
			// declared pieces, so the preview places nothing for it. Stock
			// scripts answer a declared piece.
			if at, ok = unitViewerRootLocal(a.geometry, poses, g.pad); !ok {
				return
			}
		}
		lo, hi = unitViewerTargetBox(c.def, at)
	case !g.factory:
		lo, hi = unitViewerMobileTarget(a.def, a.reach)
	default:
		return
	}
	a.spray.emit(uint32(a.ticks), src, lo, hi)
}

// product is the factory's product on its pad, or nil.
func (a *unitViewerAnimation) product() *unitViewerProduct {
	if a == nil || a.action != unitViewerBuilding || a.build.products == nil {
		return nil
	}
	return a.build.products.current
}

func (a *unitViewerAnimation) pieceName(index int) string {
	if index < 0 || a.script == nil || a.def == nil || a.def.Script == nil {
		return "none"
	}
	for script, model := range a.script.pieceMap {
		if model == index && script < len(a.def.Script.Pieces) {
			return a.def.Script.Pieces[script]
		}
	}
	return "none"
}

// hit issues the normal-kind damage pair: health first, then HitByWeapon
// with cos/sin of the direction at radius 400, then the independent
// TakeDamage with the clamped post-hit percentage [04 R-CB-01 §2][04 §5.1].
// The preview's post-hit health is half of the definition's maximum damage.
func (a *unitViewerAnimation) hit() {
	b, def := a.script.bridge, a.def
	health := def.MaxDamage / 2
	a.script.health = cob.HealthPercent(health, def.MaxDamage)
	b.HitByWeapon(unitViewerHitDirection)
	b.TakeDamage(cob.TakeDamagePercent(health, def.MaxDamage))
	a.note = fmt.Sprintf("Hit from ahead / health %d%%", cob.TakeDamagePercent(health, def.MaxDamage))
}

// unitViewerDeath records the local death query's outcome. field marks a
// corpse taken from the field's real death (adoptFieldWreck) rather than
// from this script's own query.
type unitViewerDeathState struct {
	done    bool
	depth   int32
	queried bool
	corpse  *content.FeatureDef
	field   bool
}

// kill runs slot-end death handling's synchronous local Killed query with the
// preview severity, through the battle's own query helper so the unassigned
// variant takes Nanolathe's sanctioned substitute [04 R-CB-01 §7][04 R-CB-01 §9].
// The battle then tears the script down, so the preview stops: pieces the
// script exploded stay hidden, and no debris, explosion or effect follows.
// The stage shows the field's real death for Death and Wreck instead
// (unit_viewer_field.go); Wreck's turntable then draws the corpse that death
// left, and this query's answer stands only until it does.
func (a *unitViewerAnimation) kill() {
	d := &a.death
	a.script.health = 0
	d.depth, d.queried = combat.KilledVariantFromVM(a.script.vm, a.severity)
	d.depth &= 0x0F
	d.corpse = combat.ResolveCorpse(a.def, a.features, uint8(d.depth))
	d.done, a.stopped = true, true
	how := "Killed"
	if !d.queried {
		how = "no Killed body, substitute"
	}
	a.note = fmt.Sprintf("Death / severity %d / %s depth %d / no debris or effects", a.severity, how, d.depth)
	if unitViewerAllHidden(a.script.poses()) {
		a.note = fmt.Sprintf("Death / severity %d / %s depth %d / every piece exploded", a.severity, how, d.depth)
	}
	if a.action == unitViewerWreck {
		a.note = a.wreckNote()
	}
}

func (a *unitViewerAnimation) wreckNote() string {
	d := &a.death
	switch {
	case d.field && d.corpse == nil:
		return fmt.Sprintf("Wreck / severity %d left no corpse in the field", a.severity)
	case d.depth == 0:
		return fmt.Sprintf("Wreck / severity %d leaves no corpse (depth 0)", a.severity)
	case d.corpse == nil:
		return fmt.Sprintf("Wreck / depth %d: %q has no feature there", d.depth, strings.TrimSpace(a.def.Corpse))
	}
	note := fmt.Sprintf("Wreck / %s / depth %d / %d metal", strings.ToUpper(d.corpse.CanonicalKey), d.depth, d.corpse.Metal)
	if d.corpse.Energy != 0 {
		note += fmt.Sprintf(", %d energy", d.corpse.Energy)
	}
	return note
}

// wreckFeature is the corpse feature the Wreck view draws, or nil.
func (a *unitViewerAnimation) wreckFeature() *content.FeatureDef {
	if a == nil || a.action != unitViewerWreck || !a.death.done {
		return nil
	}
	return a.death.corpse
}

// unitViewerFlight is the preview aircraft: the battle's flight integrator
// run over flat ground at height zero with no air sector grid, flying the
// shared takeoff, a straight cruise leg and a terrain landing
// [04 §10.1][04 R-AIR-01 §1][04 R-AIR-01 §6].
type unitViewerFlight struct {
	fl       movement.FlightState
	cmd      [3]int32 // command position, raw 16.16
	heading  uint16   // command heading
	goal     unitViewerAirGoal
	order    unitViewerFlightOrder
	arrived  bool // movement-arrival bit awaiting the next order visit
	tier     int
	frozen   bool
	legTicks int
}

// unitViewerAirGoal is a static point marker. altitude marks an explicit
// altitude offset, whose arrival also needs |dy| < 0x10001 [04 R-AIR-01 §4].
type unitViewerAirGoal struct {
	active   bool
	x, y, z  int32
	altitude bool
}

type unitViewerFlightOrder uint8

const (
	unitViewerFlightGrounded unitViewerFlightOrder = iota
	unitViewerFlightTakeoff
	unitViewerFlightClimb
	unitViewerFlightCruise
	unitViewerFlightLand
	unitViewerFlightLanding
	unitViewerFlightLanded
)

// unitViewerCruiseLeg is how far ahead the preview's cruise goal lies, in
// world units. The aircraft is held once its tier settles, long before the
// approach to that goal could slow it.
const unitViewerCruiseLeg = 30000

func newUnitViewerFlight(def *content.UnitDef) *unitViewerFlight {
	return &unitViewerFlight{fl: movement.FlightState{
		Mode: 1, ModeMirror: 1,
		MaxVelocity: def.MaxVelocity, Acceleration: def.Acceleration, BrakeRate: def.BrakeRate, TurnRate: def.TurnRate,
		BankScale: def.BankScale, PitchScale: def.PitchScale,
	}}
}

// airborne reports a flight that has taken off and not landed.
func (a *unitViewerAnimation) airborne() bool {
	if a == nil || a.fly == nil || a.stopped {
		return false
	}
	switch a.fly.order {
	case unitViewerFlightTakeoff, unitViewerFlightClimb, unitViewerFlightCruise:
		return true
	}
	return false
}

// toggleFlight queues a landing for an aircraft in the air, or a fresh
// takeoff for one on the ground, for the next order visit.
func (a *unitViewerAnimation) toggleFlight() {
	if a == nil || a.fly == nil || a.stopped {
		return
	}
	switch a.fly.order {
	case unitViewerFlightTakeoff, unitViewerFlightClimb, unitViewerFlightCruise:
		a.fly.order = unitViewerFlightLand
	case unitViewerFlightGrounded, unitViewerFlightLanded:
		a.fly.order = unitViewerFlightTakeoff
	}
}

// takeoffPreamble is the shared air preamble's preview-relevant steps: no
// manual target is latched and no carrier holds the unit, so it raises
// activation (the takeoff hook) and, from the ground, sets mover mode 2 and
// installs the climb marker at half the cruise altitude [04 R-AIR-01 §6].
func (a *unitViewerAnimation) takeoffPreamble() {
	f := a.fly
	a.script.setEdge(unitViewerActivated, true)
	if f.fl.Mode&3 != 1 {
		return
	}
	a.setMoverMode(2)
	f.recenter()
	y := int32(movement.CruiseAltitudeForOffset(nil, 0, 0, int32(movement.HalfCruiseAlt(a.def.CruiseAlt))))
	f.install(unitViewerAirGoal{x: f.fl.X, y: y, z: f.fl.Z, altitude: true})
}

// setMoverMode is the committed mover-mode setter: grounding zeroes velocity
// and speed, levels the lean and lowers activation; any other mode raises it
// [04 R-AIR-01 §3].
func (a *unitViewerAnimation) setMoverMode(mode uint8) {
	fl := &a.fly.fl
	if fl.Mode&3 == mode {
		return
	}
	if mode == 1 {
		fl.VX, fl.VY, fl.VZ, fl.Speed = 0, 0, 0, 0
		fl.ApplyLean(0, 0, 0)
		a.script.setEdge(unitViewerActivated, false)
	} else {
		a.script.setEdge(unitViewerActivated, true)
	}
	fl.Mode = mode
}

// recenter moves the preview's flat world so the aircraft stands at the
// origin; nothing in it depends on absolute position.
func (f *unitViewerFlight) recenter() {
	dx, dz := f.fl.X, f.fl.Z
	f.fl.X, f.fl.Z = 0, 0
	f.fl.TargetX -= dx
	f.fl.TargetZ -= dz
	f.cmd[0] -= dx
	f.cmd[2] -= dz
	f.goal.x -= dx
	f.goal.z -= dz
}

func (f *unitViewerFlight) install(goal unitViewerAirGoal) {
	goal.active = true
	f.goal, f.arrived, f.frozen, f.legTicks = goal, false, false, 0
}

// flightOrders is the order visit: VTOL_Move's takeoff, its cruise leg after
// the climb arrives, and VTOL_LandIfCan's landing [04 R-AIR-02][04 R-AIR-01 §6].
func (a *unitViewerAnimation) flightOrders() {
	f := a.fly
	if f == nil {
		return
	}
	switch f.order {
	case unitViewerFlightTakeoff:
		a.takeoffPreamble()
		f.order = unitViewerFlightClimb
		a.note = "Fly / climbing"
	case unitViewerFlightClimb:
		if !f.arrived {
			return
		}
		// Phase 1 installs the cruise goal straight ahead; its altitude is
		// terrain-derived, so its arrival test is horizontal only.
		f.recenter()
		leg := int64(unitViewerCruiseLeg) << 16
		gx := -int32((leg*int64(numeric.Sin(numeric.Angle(f.fl.Heading))) + 0x1000) >> 13)
		gz := -int32((leg*int64(numeric.Cos(numeric.Angle(f.fl.Heading))) + 0x1000) >> 13)
		y := int32(movement.CruiseAltitudeForOffset(nil, 0, 0, a.def.CruiseAlt))
		f.install(unitViewerAirGoal{x: gx, y: y, z: gz})
		f.order = unitViewerFlightCruise
		a.note = "Fly / cruising"
	case unitViewerFlightLand:
		// Phase 0 runs the preamble (the aircraft is airborne, so it builds
		// no climb marker); the search-bearing draw it takes is unused
		// because the preview position is always landable. Phase 1 follows
		// in the same pass: EndTransport with its wake, the landing marker on
		// the ground below, then the landing hook.
		a.takeoffPreamble()
		if unitViewerHasCallback(a.def, "EndTransport") {
			a.script.bridge.DeferredWake("EndTransport", nil, nil)
		}
		f.recenter()
		f.install(unitViewerAirGoal{x: f.fl.X, y: 0, z: f.fl.Z, altitude: true})
		a.script.setEdge(unitViewerActivated, false)
		f.order = unitViewerFlightLanding
		a.note = "Land / descending"
	case unitViewerFlightLanding:
		if f.arrived {
			f.arrived = false
			a.setMoverMode(1)
			f.order = unitViewerFlightLanded
			a.note = "Landed"
		}
	}
}

// flightStep is the mover tick: the command producer for the installed point
// marker, the integrator, then the movement-rate classifier.
func (a *unitViewerAnimation) flightStep() {
	f := a.fly
	if f == nil || f.frozen || f.order == unitViewerFlightGrounded || f.order == unitViewerFlightTakeoff || f.order == unitViewerFlightLand {
		return
	}
	def := a.def
	if f.goal.active {
		a.produceFlightCommand()
	}
	fl := &f.fl
	fl.TargetX, fl.TargetY, fl.TargetZ = f.cmd[0], f.cmd[1], f.cmd[2]
	fl.TargetVX, fl.TargetVZ = 0, 0 // a static marker's command does not move
	fl.TargetHeading = f.heading
	movement.IntegrateFlight(fl)
	tier := cob.MoveRateCategory(false, false, fl.Speed, int32(fl.TurnResidual), def.MoveRate1, def.MoveRate2)
	a.moveRate(f.tier, tier)
	f.tier = tier
	f.legTicks++
	switch f.order {
	case unitViewerFlightClimb:
		a.note = fmt.Sprintf("Fly / climbing / tier %d", tier)
	case unitViewerFlightCruise:
		a.note = fmt.Sprintf("Fly / cruising / tier %d", tier)
		// Hold the cruise once the tier can no longer rise: the speed has
		// reached MaxVelocity's tier and the climb has ended. A tier that
		// never settles is held after the preview's flight limit.
		top := cob.MoveRateCategory(false, false, def.MaxVelocity, 0, def.MoveRate1, def.MoveRate2)
		if (tier == top && fl.VY == 0 && f.legTicks >= unitViewerTickRate) || f.legTicks >= unitViewerFlightLimit {
			f.frozen = true
		}
	case unitViewerFlightLanding:
		a.note = fmt.Sprintf("Land / descending / tier %d", tier)
		if f.legTicks >= unitViewerFlightLimit {
			a.stop("landing exceeded the 60-second preview limit")
		}
	case unitViewerFlightLanded:
		if tier == 0 {
			f.order = unitViewerFlightGrounded
			a.note = "Landed / grounded"
		}
	}
}

// produceFlightCommand is the per-tick producer for a static point marker
// over flat ground with no sector grid [04 R-AIR-01 §1]: copy the marker's
// point, refresh the cruise altitude beyond 160 world units, steer by bearing
// beyond 16 world units (a point marker supplies no heading), then test
// arrival and release the non-persistent marker.
func (a *unitViewerAnimation) produceFlightCommand() {
	f := a.fly
	fl := &f.fl
	f.cmd = [3]int32{f.goal.x, f.goal.y, f.goal.z}
	dx, dz := int64(fl.X)-int64(f.cmd[0]), int64(fl.Z)-int64(f.cmd[2])
	if unitViewerBeyond(dx, dz, 160) {
		f.cmd[1] = int32(int64(a.def.CruiseAlt) << 16) // sector height zero
	}
	if unitViewerBeyond(dx, dz, 16) {
		f.heading = numeric.AngleFromAtan2(dx, dz).Raw()
	}
	if movement.AirArrival(numeric.Fixed(fl.X), numeric.Fixed(fl.Z), numeric.Fixed(f.goal.x), numeric.Fixed(f.goal.z), 0,
		numeric.Fixed(fl.Y), numeric.Fixed(f.goal.y), f.goal.altitude) {
		f.arrived, f.goal.active = true, false
	}
}

// unitViewerBeyond reports trunc(hypot(dx, dz)) > units world units for raw
// 16.16 deltas, exactly and without floating point: the truncated distance
// exceeds the bound iff the squared distance reaches (bound + 1 raw)².
func unitViewerBeyond(dx, dz int64, units int64) bool {
	limit := uint64(units<<16 + 1)
	return uint64(dx*dx)+uint64(dz*dz) >= limit*limit
}
