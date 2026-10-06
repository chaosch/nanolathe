package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/units"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

type unitViewerAction string

const (
	unitViewerIdle     unitViewerAction = "Idle"
	unitViewerMoving   unitViewerAction = "Move"
	unitViewerFlying   unitViewerAction = "Fly"
	unitViewerFiring   unitViewerAction = "Fire"
	unitViewerBuilding unitViewerAction = "Build"
	unitViewerHit      unitViewerAction = "Hit"
	unitViewerDeath    unitViewerAction = "Death"
	unitViewerWreck    unitViewerAction = "Wreck"

	// These are bounded host preview preferences, not battle rules
	// (DESIGN_DEVELOPER_TOOLS §7). COB keeps its own 30 Hz arithmetic [04 §4.6].
	unitViewerScriptBudget = 4096
	unitViewerTickRate     = 30
	unitViewerMaxTicks     = 5
	unitViewerAimTimeout   = 10 * unitViewerTickRate
	unitViewerAimHeading   = uint16(65536 / 8)  // 45 degrees
	unitViewerAimPitch     = uint16(65536 / 24) // about 15 degrees

	// Explicit preview inputs for map state and work targets. None is a retail
	// value; each stands in for something a battle would supply
	// (DESIGN_DEVELOPER_TOOLS §7).
	//
	// The work target lies 45 degrees off the builder's facing, the same
	// preview heading Aim uses; it is the relative bearing the slot-form
	// StartBuilding carries [04 R-CB-01 §3].
	unitViewerBuildHeading = unitViewerAimHeading
	// One wind re-roll is published before the first preview tick: heading 45
	// degrees and speed 1050, the midpoint of the canonical fallback range
	// 100..2000 a map without wind keys uses [05 R-PROD-01 §3].
	unitViewerWindHeading = uint16(65536 / 8)
	unitViewerWindSpeed   = int32(1050)
	// Every footprint cell under an extractor holds metal byte 127, the middle
	// of the byte range; the sum is (127 + 1) per covered cell [04 R-CB-01 §5].
	unitViewerMetalByte = 127
	// A hit arrives from straight ahead: direction byte 0x80 [06 §9.1], which
	// leaves the unit at half of its maximum damage.
	unitViewerHitDirection = uint8(0x80)
	// Build-stance and landing waits stop with a visible status.
	unitViewerStanceTimeout = 10 * unitViewerTickRate
	unitViewerFlightLimit   = 60 * unitViewerTickRate
)

// unitViewerSeverities are the Death and Wreck preview severities: quarters
// of the established 1..100 clamp [06 §12.1]. They are inputs, not predictions
// of what any weapon would do.
var unitViewerSeverities = [...]int32{25, 50, 75, 100}

// Availability describes authored entry points, not a promise that a callback
// will return. A Fire preview needs the selected weapon's complete aim/fire
// pair; an absent optional callback must not become a fabricated animation.
func unitViewerAnimationAvailable(def *content.UnitDef, action unitViewerAction, weapon int) bool {
	if def == nil || def.Script == nil {
		return false
	}
	switch action {
	case unitViewerIdle, unitViewerDeath:
		return true
	case unitViewerMoving:
		return !def.CanFly && unitViewerHasAny(def, "StartMoving", "MoveRate1", "MoveRate2", "MoveRate3")
	case unitViewerFlying:
		// The flight integrator divides by MaxVelocity without a guard; retail
		// faults on zero [04 §10.1], so the preview refuses it.
		return def.CanFly && def.MaxVelocity > 0
	case unitViewerFiring:
		// One action aims and fires. Only the aim is required: a weapon
		// script without its Fire callback still fires in battle, where the
		// start simply misses [04 R-CB-01 §2].
		if content.IsWeaponInactive(unitViewerWeapon(def, weapon)) {
			return false
		}
		return unitViewerHasCallback(def, unitViewerWeaponCallback("Aim", weapon))
	case unitViewerBuilding:
		if !def.Builder {
			return false
		}
		if unitViewerFactory(def) {
			return unitViewerHasAny(def, "Activate", "StartBuilding", "QueryBuildInfo", "QueryNanoPiece")
		}
		return unitViewerHasAny(def, "StartBuilding", "QueryNanoPiece")
	case unitViewerHit:
		return unitViewerHasAny(def, "HitByWeapon", "TakeDamage")
	case unitViewerWreck:
		return strings.TrimSpace(def.Corpse) != ""
	default:
		return false
	}
}

// unitViewerActionShown hides actions that do not apply to the unit's class;
// unitViewerAnimationAvailable then disables those whose callbacks are absent.
func unitViewerActionShown(def *content.UnitDef, action unitViewerAction) bool {
	if def == nil || def.Script == nil {
		return action == unitViewerIdle
	}
	switch action {
	case unitViewerMoving:
		return def.BMCode != 0 && !def.CanFly
	case unitViewerFlying:
		return def.CanFly
	case unitViewerFiring:
		for weapon := 1; weapon <= 3; weapon++ {
			if !content.IsWeaponInactive(unitViewerWeapon(def, weapon)) {
				return true
			}
		}
		return false
	case unitViewerBuilding:
		return def.Builder
	default:
		return true
	}
}

// unitViewerFactory is the building-class test the factory production state
// machine makes: the runtime status bit set from authored bmcode
// [05 "Factory production lifecycle"].
func unitViewerFactory(def *content.UnitDef) bool { return def != nil && def.BMCode == 0 }

// unitViewerHasPower reports an activation toggle: a definition with an
// Activate or Deactivate callback for the edge machine to start [04 R-UNIT-06 §2].
func unitViewerHasPower(def *content.UnitDef) bool {
	return unitViewerHasAny(def, "Activate", "Deactivate")
}

func unitViewerHasCallback(def *content.UnitDef, name string) bool {
	if def == nil || def.Script == nil {
		return false
	}
	_, ok := def.Script.Scripts[name]
	return ok
}

func unitViewerHasAny(def *content.UnitDef, names ...string) bool {
	for _, name := range names {
		if unitViewerHasCallback(def, name) {
			return true
		}
	}
	return false
}

func unitViewerWeapon(def *content.UnitDef, weapon int) *content.WeaponDef {
	if def == nil || weapon < 1 || weapon > 3 {
		return nil
	}
	return [3]*content.WeaponDef{def.Weapon1Def, def.Weapon2Def, def.Weapon3Def}[weapon-1]
}

func unitViewerWeaponCallback(prefix string, weapon int) string {
	if weapon < 1 || weapon > 3 {
		return ""
	}
	return prefix + [3]string{"Primary", "Secondary", "Tertiary"}[weapon-1]
}

func unitViewerUnavailableReason(def *content.UnitDef, action unitViewerAction, weapon int) string {
	switch action {
	case unitViewerIdle, unitViewerDeath:
		return "no script"
	case unitViewerMoving:
		if def != nil && def.CanFly {
			return "an aircraft moves by flying"
		}
		return "no StartMoving or MoveRate callback"
	case unitViewerFlying:
		if def == nil || !def.CanFly {
			return "not an aircraft"
		}
		return "maxvelocity is zero"
	case unitViewerFiring:
		if content.IsWeaponInactive(unitViewerWeapon(def, weapon)) {
			return fmt.Sprintf("weapon %d is inactive or missing", weapon)
		}
		return "no " + unitViewerWeaponCallback("Aim", weapon) + " callback"
	case unitViewerBuilding:
		if def == nil || !def.Builder {
			return "not a builder"
		}
		return "no construction callbacks"
	case unitViewerHit:
		return "no HitByWeapon or TakeDamage callback"
	case unitViewerWreck:
		return "no corpse authored"
	default:
		return "unknown animation"
	}
}

func (m *unitViewerModel) setAnimation(action unitViewerAction, weapon int) {
	if action == "" {
		action = unitViewerIdle
	}
	if weapon == 0 {
		weapon = 1
	}
	// Explicitly choosing the current action replays it, including a stopped
	// aim or fire preview (DESIGN_DEVELOPER_TOOLS §7).
	m.action, m.weapon = action, weapon
	if m.geometry != nil {
		m.anim = newUnitViewerAnimation(m.def, m.geometry, m.options())
		m.poses, m.poseNote = m.anim.poses(), m.anim.note
	}
	m.key = unitViewerModelKey{}
}

func (m *unitViewerModel) options() unitViewerAnimationOptions {
	o := unitViewerAnimationOptions{action: m.action, weapon: m.weapon, severity: m.severity, power: m.power, features: m.features, speed: m.speed, reach: m.radius}
	if m.action == unitViewerBuilding && unitViewerFactory(m.def) {
		// Product models load from the running content as the cycle reaches
		// them, on this update thread.
		mount := m.mount
		o.products = newUnitViewerProducts(m.builds, m.hidden, func(def *content.UnitDef) (*model.Model, error) {
			if mount == nil {
				return nil, fmt.Errorf("nanolathe: unit viewer product: logical path objects3d/%s.3do, providers searched [], expected mounted content", def.ObjectName)
			}
			return model.Load(mount, vfs.ResourcePath("objects3d", def.ObjectName, "3do"))
		})
	}
	return o
}

// setSpeed changes the product cycle's preview speed in place.
func (m *unitViewerModel) setSpeed(speed int) {
	m.speed = max(1, speed)
	if a := m.anim; a != nil {
		a.build.speed = m.speed
		if a.build.products != nil && a.build.state != unitViewerBuildIdle {
			a.note = a.productStatus()
		}
		m.refreshPose()
	}
}

// Fit once from the creation pose, before a requested action can move the
// limbs. Changing actions preserves that fit; the existing atlas bound still
// limits each draw.
func (m *unitViewerModel) loadAnimation(def *content.UnitDef, mdl *model.Model) {
	m.def, m.geometry = def, mdl
	if m.action == "" {
		m.action = unitViewerIdle
	}
	if m.weapon == 0 {
		m.weapon = 1
	}
	m.anim = newUnitViewerAnimation(def, mdl, unitViewerAnimationOptions{action: unitViewerIdle, weapon: 1, features: m.features})
	m.poses, m.poseNote = m.anim.poses(), m.anim.note
	states := make([]model.PieceState, len(mdl.Pieces))
	for _, pose := range m.poses {
		states[pose.Index] = model.PieceState{RotX: pose.RotX, RotY: pose.RotY, RotZ: pose.RotZ, Trans: [3]numeric.Fixed{pose.Tx, pose.Ty, pose.Tz}, Hidden: pose.Hidden}
	}
	m.fitCreatePose(states)
	if opts := m.options(); opts.action != unitViewerIdle || opts.weapon != 1 || opts.severity != 0 || opts.power != 0 {
		m.anim = newUnitViewerAnimation(def, mdl, opts)
		m.poses, m.poseNote = m.anim.poses(), m.anim.note
	}
}

func (m *unitViewerModel) updateAnimation(dt float64) {
	if m.anim == nil || !m.anim.update(dt) {
		return
	}
	m.refreshPose()
}

// refreshPose republishes the current pose after a tick or a toggle. A
// stationary camera must still redraw a changed pose.
func (m *unitViewerModel) refreshPose() {
	if m.anim == nil {
		return
	}
	m.poses, m.poseNote = m.anim.poses(), m.anim.note
	m.key = unitViewerModelKey{}
}

// unitViewerAnimationOptions carries the viewer's selections into a fresh
// preview. power is the explicit On/Off choice: 0 keeps the creation state,
// +1 and -1 raise or lower the activation bit after creation.
//
// A factory's Build action carries its product cycle and preview speed; a
// mobile Build carries the reach of its stand-in work target, in world units.
// nanoframe marks a factory product's own script: it reads its remaining
// fraction through BUILD_PERCENT_LEFT and is created without the
// completion-time activation (DESIGN_DEVELOPER_TOOLS §7).
type unitViewerAnimationOptions struct {
	action    unitViewerAction
	weapon    int
	severity  int32
	power     int8
	features  map[string]*content.FeatureDef
	products  *unitViewerProducts
	speed     int
	reach     float64
	nanoframe *float32
}

type unitViewerAnimation struct {
	script      *unitViewerScript
	def         *content.UnitDef
	geometry    *model.Model
	spray       unitViewerSpray
	reach       float64
	nanoframe   bool
	action      unitViewerAction
	weapon      int
	severity    int32
	features    map[string]*content.FeatureDef
	note        string
	carry       float64
	ticks       uint64
	nextAim     uint64
	reload      uint64
	aimStarted  uint64
	aimIdentity uint64
	aimThread   int
	aimPending  bool
	aimReturned bool
	aimReady    bool
	windPending bool
	move        unitViewerMover
	build       unitViewerBuild
	fly         *unitViewerFlight
	death       unitViewerDeathState
	stopped     bool
	invalid     bool
}

func newUnitViewerAnimation(def *content.UnitDef, mdl *model.Model, opts unitViewerAnimationOptions) *unitViewerAnimation {
	action, weapon := opts.action, opts.weapon
	if action == "" {
		action = unitViewerIdle
	}
	if weapon == 0 {
		weapon = 1
	}
	a := &unitViewerAnimation{def: def, geometry: mdl, action: action, weapon: weapon, severity: opts.severity, features: opts.features, reach: opts.reach}
	if a.severity < 1 || a.severity > 100 {
		a.severity = unitViewerSeverities[0]
	}
	a.build.nano, a.build.pad = -1, -1
	a.build.speed = max(1, opts.speed)
	a.spray = newUnitViewerSpray()
	a.script = newUnitViewerScript(def, mdl)
	if a.script == nil {
		a.stopped, a.note = true, "Authored pose / no script"
		return a
	}
	if opts.nanoframe != nil {
		a.nanoframe = true
		a.script.remaining = opts.nanoframe
	}
	if !a.createCompletedUnit(opts.power) {
		return a
	}
	if !unitViewerAnimationAvailable(def, action, weapon) {
		a.stopped, a.note = true, fmt.Sprintf("%s unavailable / %s", action, unitViewerUnavailableReason(def, action, weapon))
		return a
	}
	a.note = "Idle / completed unit"
	switch {
	case def.ExtractsMetal > 0:
		a.note = fmt.Sprintf("Idle / extractor SetSpeed %d (preview metal)", int16(unitViewerExtractorSum(def)))
	case def.WindGenerator > 0:
		a.note = fmt.Sprintf("Idle / wind %d at 45 deg (preview)", unitViewerWindSpeed)
	}
	switch action {
	case unitViewerMoving:
		a.note = "Move / accelerating"
	case unitViewerFlying:
		a.fly = newUnitViewerFlight(def)
		a.fly.order = unitViewerFlightTakeoff
		a.note = "Fly / taking off"
	case unitViewerFiring:
		// The loop is a preview policy: retry only after this interval, then
		// await a fresh aim. It is not a prediction of battle firing cadence.
		a.reload = uint64(max(1, unitViewerWeapon(def, weapon).ReloadTime))
		a.startAim()
	case unitViewerBuilding:
		a.build.factory = unitViewerFactory(def)
		if a.build.factory {
			a.build.products = opts.products
		}
		a.build.request = 1
		a.note = "Build / starting"
	case unitViewerHit:
		a.hit()
	case unitViewerDeath:
		a.note = "Death / pending"
	case unitViewerWreck:
		// The wreck is what the same Death leaves behind: one preview tick
		// runs the creation drain and then the local Killed query.
		a.tick()
	}
	if !a.invalid && len(a.script.vm.Diagnostics()) != 0 {
		a.failScript()
	}
	return a
}

// createCompletedUnit presents a finished unit as the battle creates one
// [04 R-CB-01 §4]: Create with its immediate drain; per weapon slot the
// synchronous Query* then AimFrom* (Query* when AimFrom leaves -1); the
// deferred SetMaxReloadTime; the creation-time extractor SetSpeed; and, last,
// the already-built activation of `activatewhenbuilt` through the edge
// machine [04 R-SPEC-01 §12][05 R-SHARE-01 §8]. The deferred starts first run
// in the first preview tick's drain. A wind generator's SetDirection and
// SetSpeed follow on that tick's general unit update.
func (a *unitViewerAnimation) createCompletedUnit(power int8) bool {
	if !a.script.create() {
		a.failScript()
		return false
	}
	def, b := a.def, a.script.bridge
	for slot := cob.WeaponPrimary; slot <= cob.WeaponTertiary; slot++ {
		b.QueryWeapon(slot)
		b.AimPiece(slot)
	}
	// Authored restore threads can use the longest base reload. Its standard
	// callback follows Create and remains deferred [04 R-CB-01 §2].
	if unitViewerHasCallback(def, "SetMaxReloadTime") {
		var reload int32
		for _, w := range [3]*content.WeaponDef{def.Weapon1Def, def.Weapon2Def, def.Weapon3Def} {
			if w != nil {
				reload = max(reload, w.ReloadTime)
			}
		}
		if !b.SetMaxReloadTime(reload).Started {
			a.stop("reload callback unavailable")
			return false
		}
	}
	if def.ExtractsMetal > 0 {
		b.SetSpeedFootprint(unitViewerExtractorSum(def))
	}
	// A nanoframe receives its activation at completion instead
	// [04 R-FAC-02 §3].
	if def.ActivateWhenBuilt && !a.nanoframe {
		a.script.setEdge(unitViewerActivated, true)
	}
	// An explicit On/Off choice is applied after creation, through the same
	// edge machine an Activate or Deactivate order uses.
	if power != 0 && !a.nanoframe {
		a.script.setEdge(unitViewerActivated, power > 0)
	}
	a.windPending = def.WindGenerator > 0
	if len(a.script.vm.Diagnostics()) != 0 {
		a.failScript()
		return false
	}
	return true
}

// unitViewerExtractorSum is the creation-time footprint accumulator for the
// preview's uniform metal byte: (byte + 1) per covered cell, wrapping in 16
// bits [04 R-CB-01 §5][05 R-PROD-01 §6].
func unitViewerExtractorSum(def *content.UnitDef) int32 {
	cells := int64(max(0, def.FootprintX)) * int64(max(0, def.FootprintZ))
	return int32(uint16(cells * (unitViewerMetalByte + 1)))
}

func (a *unitViewerAnimation) poses() []frame.PieceView {
	if a.script == nil || a.invalid {
		return nil
	}
	return a.script.poses()
}

func (a *unitViewerAnimation) update(dt float64) bool {
	if a.stopped || dt <= 0 || math.IsNaN(dt) || math.IsInf(dt, 0) {
		return false
	}
	// Drop excess host elapsed time rather than accumulate catch-up work
	// after a stalled window (DESIGN_DEVELOPER_TOOLS §7).
	a.carry = min(float64(unitViewerMaxTicks), a.carry+dt*unitViewerTickRate)
	steps := int(a.carry)
	a.carry -= float64(steps)
	for i := 0; i < steps && !a.stopped; i++ {
		a.tick()
	}
	return steps != 0
}

// tick is one preview unit visit in the established callback order: the
// general unit update, the weapon update, the normal drain, order and
// construction work, movement, then slot-end death handling [04 §5.4].
func (a *unitViewerAnimation) tick() {
	a.ticks++
	b := a.script.bridge
	if a.windPending {
		// The wind pair is a per-change producer, issued once per re-roll
		// [04 R-CB-01 §5]; the preview publishes one re-roll.
		a.windPending = false
		b.SetDirection(unitViewerWindHeading)
		b.SetSpeed(unitViewerWindSpeed)
	}
	if a.action == unitViewerFiring && !a.aimPending {
		if a.aimReady {
			// Root Fire precedes optional RockUnit [04 R-CB-01 §2]. There
			// are no projectile, burst, sound, resource or effect producers.
			// A script without this Fire callback misses the start, as the
			// battle's spawner does, and RockUnit still follows.
			if unitViewerHasCallback(a.def, unitViewerWeaponCallback("Fire", a.weapon)) && !b.Fire(cob.WeaponSlot(a.weapon-1)).Started {
				a.stop("fire callback unavailable")
				return
			}
			if unitViewerHasCallback(a.def, "RockUnit") && !b.RockUnit(int16(unitViewerAimHeading)).Started {
				a.stop("recoil callback unavailable")
				return
			}
			a.aimReady, a.nextAim = false, a.ticks+a.reload
			a.note = fmt.Sprintf("Weapon %d / firing animation", a.weapon)
		} else if a.ticks >= a.nextAim {
			a.startAim()
		}
	}
	if a.stopped {
		return
	}
	b.Drain(1)
	if len(a.script.vm.Diagnostics()) != 0 {
		a.failScript()
		return
	}
	if a.aimPending {
		switch {
		case a.aimReturned:
			a.aimPending = false
			if !a.aimReady {
				a.stop("aim returned zero")
			} else {
				a.note = fmt.Sprintf("Weapon %d / aim ready", a.weapon)
			}
		case !a.script.vm.ThreadAliveAs(a.aimThread, a.aimIdentity):
			a.stop("aim ended without a return")
		case a.ticks-a.aimStarted >= unitViewerAimTimeout:
			a.stop("aim exceeded 10-second preview limit")
		}
	}
	if a.stopped {
		return
	}
	a.buildStep()
	a.flightOrders()
	a.moveStep()
	a.flightStep()
	if (a.action == unitViewerDeath || a.action == unitViewerWreck) && !a.death.done && !a.stopped {
		a.kill()
	}
	if !a.invalid && len(a.script.vm.Diagnostics()) != 0 {
		a.failScript()
	}
	// The product is a later unit in the same sweep; the strip pass that
	// advances the spray follows every unit visit [03 R-STRIP-01 §2].
	if p := a.build.products; p != nil && p.current != nil && p.current.anim != nil && !p.current.anim.stopped {
		p.current.anim.tick()
	}
	a.spray.update(uint32(a.ticks))
}

func (a *unitViewerAnimation) startAim() {
	a.aimPending, a.aimReturned, a.aimReady = true, false, false
	a.aimStarted = a.ticks
	// Only an explicit nonzero return grants readiness [04 R-CB-01 §6].
	result := a.script.bridge.Aim(cob.WeaponSlot(a.weapon-1), unitViewerAimHeading, unitViewerAimPitch, func(r cob.CallbackReturn) {
		a.aimReturned, a.aimReady = r.Explicit, r.Explicit && r.Value != 0
	})
	if !result.Started {
		a.stop("aim callback unavailable")
		return
	}
	a.aimThread, a.aimIdentity = result.Thread, a.script.vm.ThreadIdentity(result.Thread)
	a.note = fmt.Sprintf("Weapon %d / aiming", a.weapon)
}

func (a *unitViewerAnimation) stop(reason string) {
	a.stopped = true
	a.note = fmt.Sprintf("%s preview stopped / %s", a.action, reason)
}

func (a *unitViewerAnimation) failScript() {
	a.stopped, a.invalid, a.note = true, true, "Authored pose / script unavailable"
}

// activated reports the live activation bit of the preview's state byte.
func (a *unitViewerAnimation) activated() bool {
	return a != nil && a.script != nil && a.script.state&unitViewerActivated != 0
}

// setActivation drives the activation edge machine as an Activate or
// Deactivate order does: Activate on the rising edge, Deactivate on the
// falling edge, nothing when the bit is unchanged [04 R-UNIT-06 §2].
func (a *unitViewerAnimation) setActivation(on bool) {
	if a == nil || a.script == nil || a.invalid {
		return
	}
	a.script.setEdge(unitViewerActivated, on)
	if len(a.script.vm.Diagnostics()) != 0 {
		a.failScript()
	}
}

// The detached unit-state bits the engine ports read and write [04 §4.4]
// [04 §4.7]: the first state byte carries activation, armor and building; the
// second carries the build stance, busy, yard and bugger-off flags.
const (
	unitViewerActivated uint8 = 1 << 0
	unitViewerArmored   uint8 = 1 << 1
	unitViewerBuildBit  uint8 = 1 << 3

	unitViewerStance    uint8 = 1 << 0
	unitViewerBusy      uint8 = 1 << 1
	unitViewerYardOpen  uint8 = 1 << 2
	unitViewerBuggerOff uint8 = 1 << 3
)

// This interpreter owns only detached pose, flag and unit-state words. Full
// health, complete construction, an empty yard, unbound zero reads and random
// low bounds are explicit preview inputs, not a claim about gameplay
// (DESIGN_DEVELOPER_TOOLS §7).
type unitViewerScript struct {
	vm          *cob.VM
	bridge      *cob.CallbackBridge
	pieceMap    []int
	modelFlags  []uint8
	pose        []frame.PieceView
	interrupted string
	state       uint8    // activated, armored, building [04 R-UNIT-06 §2]
	stance      uint8    // in-build stance, busy, yard open, bugger off [04 §4.7]
	health      int32    // port 4 reading
	remaining   *float32 // a product's construction fraction, or nil when complete
}

func newUnitViewerScript(def *content.UnitDef, mdl *model.Model) *unitViewerScript {
	if def == nil || def.Script == nil || mdl == nil {
		return nil
	}
	s := &unitViewerScript{
		vm:         cob.NewPresentationVM(def.Script, unitViewerScriptBudget),
		modelFlags: units.BuildRenderPieceFlags(mdl),
		pose:       make([]frame.PieceView, len(mdl.Pieces)),
		health:     100,
	}
	s.bridge = cob.NewCallbackBridge(s.vm)
	// A Create that ends by signal or malformed termination is a non-returning
	// creation; the preview falls back rather than present it [04 §4.2][04 §4.3].
	// Other callbacks may be signalled by later ones as authored behavior.
	s.bridge.SetLifecycleSink(func(e cob.LifecycleEvent) {
		if e.Phase == "finish-abnormal" && e.Name == "Create" && s.interrupted == "" {
			s.interrupted = e.Name
		}
	})
	modelNames := make([]string, len(mdl.Pieces))
	for i := range mdl.Pieces {
		modelNames[i] = mdl.Pieces[i].Name
	}
	s.pieceMap = cob.LinkPieces(def.Script.Pieces, modelNames)
	flags := make([]uint8, len(s.pieceMap))
	s.vm.BindRenderFlagHandlers(func() []uint8 {
		for i, idx := range s.pieceMap {
			if idx >= 0 && idx < len(s.modelFlags) {
				flags[i] = s.modelFlags[idx]
			}
		}
		return flags
	}, func(piece int, mask uint8, set bool) bool {
		if piece >= 0 && piece < len(s.pieceMap) {
			idx := s.pieceMap[piece]
			if idx >= 0 && idx < len(s.modelFlags) {
				if set {
					s.modelFlags[idx] |= mask
				} else {
					s.modelFlags[idx] &^= mask
				}
			}
		}
		return true
	})
	bit := func(word *uint8, mask uint8) func([4]int32) int32 {
		return func([4]int32) int32 {
			if *word&mask != 0 {
				return 1
			}
			return 0
		}
	}
	set := func(word *uint8, mask uint8) func(int32) {
		return func(v int32) {
			if v&1 != 0 {
				*word |= mask
			} else {
				*word &^= mask
			}
		}
	}
	// Ports 1 and 20 drive the shared edge machine; 5, 6, 18 and 19 write the
	// stance byte. The yard admission always passes because no unit or feature
	// stands in the preview's yard [04 §4.7].
	s.vm.BindPortBinding(cob.Port(1), cob.PortBinding{Read: bit(&s.state, unitViewerActivated), Write: func(v int32) { s.setEdge(unitViewerActivated, v&1 != 0) }})
	s.vm.BindPortBinding(cob.Port(4), cob.PortBinding{Read: func([4]int32) int32 { return s.health }})
	s.vm.BindPortBinding(cob.Port(5), cob.PortBinding{Read: bit(&s.stance, unitViewerStance), Write: set(&s.stance, unitViewerStance)})
	s.vm.BindPortBinding(cob.Port(6), cob.PortBinding{Read: bit(&s.stance, unitViewerBusy), Write: set(&s.stance, unitViewerBusy)})
	// BUILD_PERCENT_LEFT reads a factory product's own fraction; every other
	// preview is a completed unit [04 §4.4].
	s.vm.BindPortBinding(cob.Port(17), cob.PortBinding{Read: func([4]int32) int32 {
		if s.remaining == nil {
			return 0
		}
		return cob.BuildPercentLeft(*s.remaining)
	}})
	s.vm.BindPortBinding(cob.Port(18), cob.PortBinding{Read: bit(&s.stance, unitViewerYardOpen), Write: set(&s.stance, unitViewerYardOpen)})
	s.vm.BindPortBinding(cob.Port(19), cob.PortBinding{Read: bit(&s.stance, unitViewerBuggerOff), Write: set(&s.stance, unitViewerBuggerOff)})
	s.vm.BindPortBinding(cob.Port(20), cob.PortBinding{Read: bit(&s.state, unitViewerArmored), Write: func(v int32) { s.setEdge(unitViewerArmored, v&1 != 0) }})
	return s
}

// setEdge is the activation/production edge machine: write the byte first,
// then start the deferred callbacks of each actual edge in the established
// order — rising activation, falling activation, rising building, falling
// building [04 R-UNIT-06 §2]. Armor has no callback. Engine notifications,
// the interface dirty bit and network events have no preview consumer.
func (s *unitViewerScript) setEdge(mask uint8, on bool) {
	old := s.state
	if on {
		s.state |= mask
	} else {
		s.state &^= mask
	}
	rise, fall := s.state&^old, old&^s.state
	if rise&unitViewerActivated != 0 {
		s.bridge.Activate()
	}
	if fall&unitViewerActivated != 0 {
		s.bridge.Deactivate()
	}
	if rise&unitViewerBuildBit != 0 {
		s.bridge.StartBuilding()
	}
	if fall&unitViewerBuildBit != 0 {
		s.bridge.StopBuilding()
	}
}

// modelPiece maps a script piece index to the model's piece index, or -1.
func (s *unitViewerScript) modelPiece(piece int32) int {
	if s == nil || piece < 0 || int(piece) >= len(s.pieceMap) {
		return -1
	}
	if idx := s.pieceMap[piece]; idx >= 0 && idx < len(s.pose) {
		return idx
	}
	return -1
}

func (s *unitViewerScript) create() bool {
	if _, ok := s.vm.ScriptPC("Create"); ok && !s.bridge.Create().Started {
		return false
	}
	return len(s.vm.Diagnostics()) == 0 && s.interrupted == ""
}

func (s *unitViewerScript) poses() []frame.PieceView {
	for i := range s.pose {
		f := s.modelFlags[i]
		s.pose[i] = frame.PieceView{Index: i, Hidden: f&1 == 0, DontShade: f&4 == 0, DontShadow: f&8 == 0}
	}
	for i, state := range s.vm.Pieces {
		if i >= len(s.pieceMap) {
			continue
		}
		idx := s.pieceMap[i]
		if idx < 0 || idx >= len(s.pose) {
			continue
		}
		p := &s.pose[idx]
		p.RotX, p.RotY, p.RotZ = state.RotX, state.RotY, state.RotZ
		p.Tx, p.Ty, p.Tz = state.Trans[0], state.Trans[1], state.Trans[2]
	}
	return s.pose
}
