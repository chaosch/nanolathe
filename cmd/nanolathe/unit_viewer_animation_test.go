package main

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// Preserve the zero-time Create snapshot contract while production retains
// the same interpreter for subsequent animation ticks.
func unitViewerCreationPose(def *content.UnitDef, mdl *model.Model) ([]frame.PieceView, string) {
	s := newUnitViewerScript(def, mdl)
	if s == nil {
		return nil, "Authored pose / no script"
	}
	if !s.create() {
		return nil, "Authored pose / script unavailable"
	}
	return s.poses(), "Preview pose / rotation only"
}

// Independently authored instructions use the existing COB format and VM;
// these fixtures lock only the viewer's detached callback/timing policy.
const (
	viewerPush      = 0x10021001
	viewerLocal     = 0x10021002
	viewerStatic    = 0x10021004
	viewerPopStatic = 0x10023004
	viewerMoveNow   = 0x1000b000
	viewerTurn      = 0x10002000
	viewerTurnNow   = 0x1000c000
	viewerWaitTurn  = 0x10011000
	viewerShow      = 0x10005000
	viewerHide      = 0x10006000
	viewerSleep     = 0x10013000
	viewerRead      = 0x10042000
	viewerRandom    = 0x10041000
	viewerReturn    = 0x10065000
)

type viewerScriptFixture struct {
	name string
	code []uint32
}

func unitViewerAnimationFixture(scripts ...viewerScriptFixture) (*content.UnitDef, *model.Model) {
	prog := &cob.Program{Pieces: []string{"body", "arm", "flash"}, Scripts: make(map[string]int), Statics: 1}
	for _, script := range scripts {
		pc := len(prog.Code)
		prog.Scripts[script.name] = pc
		prog.ScriptsByID = append(prog.ScriptsByID, pc)
		prog.Code = append(prog.Code, script.code...)
	}
	mdl := &model.Model{Root: 0, Pieces: []model.Piece{
		{Name: "body", Parent: -1}, {Name: "arm", Parent: 0}, {Name: "flash", Parent: 0},
	}}
	for i := range mdl.Pieces {
		mdl.Pieces[i].Vertices = [][3]numeric.Fixed{{}, {65536, 0, 0}, {0, 65536, 0}}
		mdl.Pieces[i].Primitives = []model.Primitive{{IsColored: 1, VertexIndices: []uint16{0, 1, 2}}}
	}
	return &content.UnitDef{Script: prog, Weapon1Def: &content.WeaponDef{ID: 1, ReloadTime: 3}}, mdl
}

func unitViewerPlay(def *content.UnitDef, mdl *model.Model, action unitViewerAction, weapon int) *unitViewerAnimation {
	return newUnitViewerAnimation(def, mdl, unitViewerAnimationOptions{action: action, weapon: weapon})
}

func unitViewerAdvance(a *unitViewerAnimation, ticks int) {
	for i := 0; i < ticks; i++ {
		a.update(1.0 / unitViewerTickRate)
	}
}

func TestUnitViewerAnimationIdleClockAndIsolation(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(viewerScriptFixture{"Create", []uint32{
		viewerPush, 4, viewerRead, viewerMoveNow, 0, 0,
		viewerPush, 7, viewerPush, 9, viewerRandom, viewerMoveNow, 0, 1,
		viewerPush, 17, viewerRead, viewerMoveNow, 0, 2,
		viewerPush, 100, viewerSleep, viewerHide, 2, viewerReturn,
	}})
	sourceCode := append([]uint32(nil), def.Script.Code...)
	a := unitViewerPlay(def, mdl, unitViewerIdle, 1)
	p := a.poses()
	if a.stopped || p[0].Tx != 100 || p[0].Ty != 7 || p[0].Tz != 0 || p[2].Hidden {
		t.Fatalf("Create preview inputs or zero-time pose: %+v, %s", p, a.note)
	}
	if a.update(0.5/unitViewerTickRate) || a.ticks != 0 || !a.update(0.5/unitViewerTickRate) || a.ticks != 1 {
		t.Fatal("host accumulator did not retain half a preview tick")
	}
	a.update(1000)
	if a.ticks != 1+unitViewerMaxTicks || !a.poses()[2].Hidden {
		t.Fatalf("sleep did not advance within the bounded host update: ticks %d, %s", a.ticks, a.note)
	}
	for _, dt := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if a.update(dt) || a.ticks != 1+unitViewerMaxTicks {
			t.Fatal("invalid elapsed time advanced playback or retained unbounded catch-up")
		}
	}
	b := unitViewerPlay(def, mdl, unitViewerIdle, 1)
	if b.poses()[2].Hidden || !reflect.DeepEqual(sourceCode, def.Script.Code) || mdl.Pieces[0].Translate != [3]numeric.Fixed{} {
		t.Fatal("preview mutated its immutable script/model or another playback instance")
	}
}

func TestUnitViewerAnimationWalkingRequestCacheAndSelectionReset(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerHide, 2, viewerReturn}},
		viewerScriptFixture{"StartMoving", []uint32{viewerPush, 90, viewerPush, 12, viewerTurn, 1, 1, viewerWaitTurn, 1, 1, viewerReturn}},
	)
	def.BMCode, def.MaxVelocity, def.Acceleration, def.MoveRate1, def.MoveRate2 = 1, 2<<16, 1<<16, 4<<16, 4<<16
	m := &unitViewerModel{}
	m.setAnimation(unitViewerMoving, 1)
	if m.anim != nil {
		t.Fatal("pre-load action allocated playback without a model")
	}
	m.loadAnimation(def, mdl)
	if m.anim.action != unitViewerMoving || m.poses[1].RotY != 0 || !m.poses[2].Hidden {
		t.Fatal("pre-load request was lost, Create was skipped, or StartMoving advanced time")
	}
	radius := m.radius
	m.key = unitViewerModelKey{def: def, yaw: 123}
	// StartMoving is the movement step's wake start on the first tick; its
	// turn advances on the next drain [04 §5.2][04 §5.4].
	m.updateAnimation(1.0 / unitViewerTickRate)
	if m.poses[1].RotY != 0 || m.key.def != nil || m.radius != radius {
		t.Fatal("movement advanced before its tier change, kept a stale camera, or changed the fit")
	}
	m.updateAnimation(1.0 / unitViewerTickRate)
	if m.poses[1].RotY != 3 {
		t.Fatal("walking did not animate after StartMoving")
	}
	m.updateAnimation(3.0 / unitViewerTickRate)
	if m.poses[1].RotY != 12 {
		t.Fatalf("authored turn did not reach its target: %d", m.poses[1].RotY)
	}
	m.setAnimation(unitViewerMoving, 1)
	if m.poses[1].RotY != 0 || m.anim.ticks != 0 || m.radius != radius {
		t.Fatal("selecting the same action did not restart playback with the same fit")
	}
	m.selectUnit()
	if m.action != unitViewerIdle || m.weapon != 1 || m.anim != nil || m.geometry != nil || m.poses != nil {
		t.Fatal("selection retained the old playback request or detached model")
	}
	m.loadAnimation(def, mdl)
	if m.anim.action != unitViewerIdle || m.anim.ticks != 0 || m.poses[1].RotY != 0 {
		t.Fatal("new selection inherited motion from the old script")
	}
}

func TestUnitViewerAnimationFireWaitsForSelectedAimAndBaseReload(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerHide, 2, viewerReturn}},
		viewerScriptFixture{"SetMaxReloadTime", []uint32{viewerLocal, 0, viewerMoveNow, 0, 2, viewerReturn}},
		viewerScriptFixture{"AimSecondary", []uint32{
			viewerPush, 30 * uint32(unitViewerAimHeading) / 2, viewerLocal, 0, viewerTurn, 1, 1,
			viewerLocal, 1, viewerTurnNow, 1, 0, viewerWaitTurn, 1, 1, viewerPush, 1, viewerReturn,
		}},
		viewerScriptFixture{"FireSecondary", []uint32{
			viewerStatic, 0, viewerPush, 1, 0x10031000, viewerPopStatic, 0,
			viewerStatic, 0, viewerMoveNow, 0, 0, viewerShow, 2, viewerReturn,
		}},
		viewerScriptFixture{"RockUnit", []uint32{
			viewerLocal, 0, viewerMoveNow, 0, 1, viewerLocal, 1, viewerMoveNow, 0, 2,
			viewerStatic, 0, viewerMoveNow, 2, 0, viewerReturn,
		}},
	)
	def.Weapon1Def.ReloadTime = 30
	def.Weapon2Def = &content.WeaponDef{ID: 2, ReloadTime: 4}
	// The reload callback scans all definitions, including the inactive one
	// [04 R-CB-01 §2]. Fire uses only the selected active slot's base reload.
	def.Weapon3Def = &content.WeaponDef{ID: 0, ReloadTime: 90}
	a := unitViewerPlay(def, mdl, unitViewerFiring, 2)
	unitViewerAdvance(a, 3)
	p := a.poses()
	if a.stopped || !a.aimReady || p[0].Tx != 0 || p[0].Tz != 3000 || !p[2].Hidden || p[1].RotY != unitViewerAimHeading || p[1].RotX != unitViewerAimPitch {
		t.Fatalf("Fire skipped the selected aim/return or reload callback: %+v, %s", p, a.note)
	}
	unitViewerAdvance(a, 1)
	p = a.poses()
	if p[0].Tx != 1 || p[2].Tx != 1 || p[2].Hidden || p[0].Ty != -566 || p[0].Tz != -566 {
		t.Fatalf("Fire/Rock order or fixed aim context changed: %+v, %s", p, a.note)
	}
	unitViewerAdvance(a, 4)
	if a.poses()[0].Tx != 1 || !a.aimPending {
		t.Fatalf("fire repeated before reload or failed to re-aim at the base interval: %s", a.note)
	}
	unitViewerAdvance(a, 1)
	if a.poses()[0].Tx != 1 || !a.aimReady {
		t.Fatal("fire did not wait for the repeated aim's authored wait/return")
	}
	unitViewerAdvance(a, 1)
	if a.poses()[0].Tx != 2 || a.stopped {
		t.Fatalf("fire preview did not repeat after a fresh aim: %s", a.note)
	}
}

// Aim and Fire are one action. A script without the selected weapon's Fire
// callback misses that start, as the battle's spawner does; RockUnit still
// follows and the loop keeps aiming [04 R-CB-01 §2].
func TestUnitViewerAnimationFireWithoutFireCallbackKeepsAiming(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerReturn}},
		viewerScriptFixture{"AimPrimary", []uint32{viewerPush, 1, viewerReturn}},
		viewerScriptFixture{"RockUnit", []uint32{viewerStatic, 0, viewerPush, 1, 0x10031000, viewerPopStatic, 0,
			viewerStatic, 0, viewerMoveNow, 0, 0, viewerReturn}},
	)
	def.Weapon1Def.ReloadTime = 2
	if !unitViewerAnimationAvailable(def, unitViewerFiring, 1) {
		t.Fatal("Fire required a Fire callback the battle does not require")
	}
	a := unitViewerPlay(def, mdl, unitViewerFiring, 1)
	unitViewerAdvance(a, 20)
	if a.stopped || a.poses()[0].Tx < 2 {
		t.Fatalf("Fire without a Fire callback stopped or never repeated its recoil: %+v, %s", a.poses()[0], a.note)
	}
}

func TestUnitViewerAnimationAvailabilityAndFailedAim(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerReturn}},
		viewerScriptFixture{"AimPrimary", []uint32{viewerPush, 0, viewerReturn}},
		viewerScriptFixture{"FirePrimary", []uint32{viewerHide, 2, viewerReturn}},
	)
	for _, action := range []unitViewerAction{unitViewerIdle, unitViewerMoving, unitViewerFlying, unitViewerFiring, unitViewerBuilding, unitViewerHit, unitViewerDeath, unitViewerWreck} {
		if unitViewerAnimationAvailable(nil, action, 1) || unitViewerAnimationAvailable(&content.UnitDef{}, action, 1) {
			t.Fatal("missing script advertised an animation")
		}
	}
	if !unitViewerAnimationAvailable(def, unitViewerFiring, 1) || unitViewerAnimationAvailable(def, unitViewerMoving, 1) ||
		unitViewerAnimationAvailable(def, unitViewerFiring, 0) || unitViewerAnimationAvailable(def, unitViewerFiring, 4) || unitViewerAnimationAvailable(def, unitViewerFiring, 2) {
		t.Fatal("callback/weapon availability was inferred instead of checked")
	}
	for _, tc := range []struct {
		name string
		code []uint32
		want string
	}{
		{"zero return", []uint32{viewerPush, 0, viewerReturn}, "aim returned zero"},
		{"signal", []uint32{viewerPush, 1, 0x10067000}, "ended without a return"},
		{"malformed", []uint32{0}, "ended without a return"},
		{"timeout", []uint32{viewerPush, 100000, viewerSleep, viewerPush, 1, viewerReturn}, "10-second preview limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def, mdl := unitViewerAnimationFixture(
				viewerScriptFixture{"Create", []uint32{viewerReturn}},
				viewerScriptFixture{"AimPrimary", tc.code},
				viewerScriptFixture{"FirePrimary", []uint32{viewerHide, 2, viewerReturn}},
			)
			a := unitViewerPlay(def, mdl, unitViewerFiring, 1)
			unitViewerAdvance(a, unitViewerAimTimeout+1)
			if !a.stopped || !strings.Contains(a.note, tc.want) || a.poses()[2].Hidden {
				t.Fatalf("failed aim authorized Fire or lacked a status: %s", a.note)
			}
		})
	}
	delete(def.Script.Scripts, "AimPrimary")
	if unitViewerAnimationAvailable(def, unitViewerFiring, 1) {
		t.Fatal("Fire advertised a missing Aim callback as implemented")
	}
	a := unitViewerPlay(def, mdl, unitViewerFiring, 1)
	if !a.stopped || !strings.Contains(a.note, "unavailable") {
		t.Fatalf("missing aim not reported: %s", a.note)
	}
}

func TestUnitViewerAnimationInstructionLimitAfterCreate(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerReturn}},
		viewerScriptFixture{"StartMoving", []uint32{viewerPush, 0, viewerSleep, 0x10064000, 4}},
	)
	def.MaxVelocity, def.Acceleration = 1<<16, 1<<16
	a := unitViewerPlay(def, mdl, unitViewerMoving, 1)
	unitViewerAdvance(a, 1)
	if a.stopped {
		t.Fatalf("valid initial sleep rejected: %s", a.note)
	}
	unitViewerAdvance(a, 1)
	if !a.stopped || a.poses() != nil || a.note != "Authored pose / script unavailable" {
		t.Fatalf("non-yielding loop after Create escaped the preview bound: %s", a.note)
	}
}
