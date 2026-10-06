package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/combat"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
	"github.com/nanolathe-gg/nanolathe/internal/units"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// The unit viewer's field (DESIGN_DEVELOPER_TOOLS §7 "In the field"): Death
// and Wreck play the selected unit's real battle death in a tiny real battle
// staged on the stage through the settings screen's preview machinery
// (nlPreview). The unit stands alone on open ground of a stock map, seen from
// the battle's own still camera; after a short settle it dies through the
// battle's own death path at the selected severity, so its Killed script,
// death explosion, debris and corpse are the battle's. The field is
// presentation only: its battle is never saved, networked or seen by any
// other battle, and nothing here writes settings.

// These are viewer preferences, not retail values.
const (
	// unitViewerFieldPreTicks is the unseen lead-in: one tick, so the
	// unit's creation has run before the field appears.
	unitViewerFieldPreTicks = 1
	// unitViewerFieldSettle is how long the unit stands in the field, in
	// ticks, before it dies, so it is seen whole first.
	unitViewerFieldSettle = 30
	// unitViewerFieldHold is how long the field runs after the death, in
	// ticks, before Death restarts it and Wreck hands over to the turntable:
	// seven seconds, by which the stock debris has landed, burst and its
	// smoke has cleared.
	unitViewerFieldHold = 210
	// unitViewerFieldRings bounds the search for ground or water about the
	// site's anchor, in 16-pixel rings (nlStage.spot).
	unitViewerFieldRings = 24
	// The camera's zoom makes the picture's width span this many of the
	// unit's footprints, within the battle's detail view and a floor.
	unitViewerFieldFootprints = 6
	unitViewerFieldMinZoom    = 0.75
	// unitViewerFieldLow places the unit this fraction of the picture's
	// height below its middle: a model rises up the screen from its
	// footprint, and debris climbs before it falls [03 §2.5].
	unitViewerFieldLow = 0.12
)

// unitViewerFieldSite is where a field stands: a stock map and an anchor on
// it. The ground site is the settings screen's blast-scene ground on
// Greenhaven, flat and cleared; the water site is its naval scene's deep
// water off Coast To Coast.
type unitViewerFieldSite struct {
	name  string
	x, z  int32
	water bool
}

var (
	unitViewerFieldGround = unitViewerFieldSite{name: "Greenhaven", x: 2308, z: 4386}
	unitViewerFieldWater  = unitViewerFieldSite{name: "Coast To Coast", x: 2284, z: 1188, water: true}
)

// nlFieldKey names one field scene. unit is empty for every settings scene.
type nlFieldKey struct {
	unit     string // the record's canonical name
	ordinal  int    // the record's index in Catalog.UnitRecords
	severity int32
	run      uint64 // the viewer's action generation: each choice stages afresh
}

// unitViewerFieldRun is one staged field and what its death did. The stage
// and its events fill it on the staging worker; once the scene crosses to
// the viewer, the viewer's goroutine steps the battle and reads it there.
type unitViewerFieldRun struct {
	key    nlFieldKey
	site   unitViewerFieldSite
	w, h   int
	zoom   float64
	def    *content.UnitDef // the battle catalog's record
	unit   *units.Unit
	chain  []*content.FeatureDef // the corpse chain, depth 1 first
	x, z   int32                 // where the unit stands, world pixels
	health int32                 // the death's post-hit health
	prior  uint8                 // and prior health sample
	killed bool
	// settled is set once the death has been resolved; corpse is then the
	// corpse it left, if any, and depth its place in the chain.
	settled bool
	corpse  *content.FeatureDef
	depth   int
	refused string // the placement validator's refusal on this map
	err     string // why the field cannot show the death
}

// unitViewerFieldDeathInputs chooses the unit inputs of the death severity
// so that it is exactly severity [06 §12.1]:
//
//	clamp(((−health·100) ÷ maxHealth + previousSample) ÷ 2, 1, 100)
//
// with health already at or below zero, an unsigned divide and a halving
// that truncates. The prior health sample is min(100, 2·severity), the
// largest a sampled percentage can be, and the health is the least-negative
// value that supplies the rest: zero up to severity 50, then
// −⌈(2·severity − 100)·maxHealth ÷ 100⌉. Each is a state a battle reaches:
// a unit sampled at that percentage and then killed by a blow that leaves it
// at that health. Retail reads the health as a signed 16-bit word, so a
// value that would not fit is refused, as is a maximum health of zero or
// less; a smaller sample is tried before the selection is reported as
// unreachable. The answer is checked against the battle's own arithmetic
// (cob.KilledSeverity).
func unitViewerFieldDeathInputs(maxHealth, severity int32) (health int32, prior uint8, ok bool) {
	if maxHealth <= 0 || severity < 1 || severity > 100 {
		return 0, 0, false
	}
	for p := min(100, 2*severity); p >= 0; p-- {
		need := int64(2*severity - p)
		neg := (need*int64(maxHealth) + 99) / 100
		if neg > -math.MinInt16 {
			continue
		}
		if h := int32(-neg); cob.KilledSeverity(h, maxHealth, uint8(p)) == severity {
			return h, uint8(p), true
		}
	}
	return 0, 0, false
}

// unitViewerFieldZoom is the field camera's zoom for def on a picture w
// pixels wide.
func unitViewerFieldZoom(def *content.UnitDef, w int) float64 {
	foot := max(def.FootprintX, def.FootprintZ, 1) * 16
	zoom := float64(w) / float64(unitViewerFieldFootprints*foot)
	return min(camera.ZoomMax.Float(), max(unitViewerFieldMinZoom, zoom))
}

// unitViewerFieldWaterFirst reports a unit whose placement profile asks for
// water under it, which the field tries on its water site first. The
// placement validator decides where the unit may stand either way; this
// only orders the two sites.
func unitViewerFieldWaterFirst(cat *content.Catalog, key nlFieldKey) bool {
	records := cat.UnitRecords()
	if key.ordinal < 0 || key.ordinal >= len(records) || records[key.ordinal] == nil {
		return false
	}
	rules, err := world.PlacementRulesForUnit(cat, records[key.ordinal])
	return err == nil && rules.MinWaterDepth > 0
}

// buildUnitViewerField stages the field for key on the staging worker. A
// unit the placement validator refuses at its first site is staged at the
// other; refused at both, the scene carries the refusals and no unit.
func buildUnitViewerField(opts Options, cs *contentSet, key nlSceneKey) (*nlPreviewInstance, error) {
	cat, err := cs.nlPreviewCatalog()
	if err != nil {
		return nil, err
	}
	sites := []unitViewerFieldSite{unitViewerFieldGround, unitViewerFieldWater}
	if unitViewerFieldWaterFirst(cat, key.field) {
		slices.Reverse(sites)
	}
	var first *unitViewerFieldRun
	for i, site := range sites {
		run := &unitViewerFieldRun{key: key.field, site: site, w: key.w, h: key.h, zoom: 1}
		inst, err := buildNLPreviewSceneWith(opts, cs, key, run.preset(), key.mutators)
		if err != nil {
			return nil, err
		}
		inst.field = run
		if run.refused == "" || i == len(sites)-1 {
			if run.refused != "" {
				run.err = unitViewerFieldRefusal(first, run)
			}
			return inst, nil
		}
		first = run
		inst.close()
	}
	return nil, fmt.Errorf("nanolathe: unit viewer field: logical path units/%s, providers searched [field sites], expected a site", key.field.unit)
}

// unitViewerFieldRefusal words the placement validator's refusals at both
// sites.
func unitViewerFieldRefusal(first, second *unitViewerFieldRun) string {
	if first == nil {
		return fmt.Sprintf("%s refuses it: %s", second.site.name, second.refused)
	}
	if first.refused == second.refused {
		return fmt.Sprintf("neither %s nor %s takes it: %s", first.site.name, second.site.name, second.refused)
	}
	return fmt.Sprintf("%s refuses it: %s; %s refuses it: %s", first.site.name, first.refused, second.site.name, second.refused)
}

// preset is the field's scene: an empty battle at the site, centred on the
// unit under a still camera, with commander death at the lobby's "game
// continues" so a commander's death plays alone. The viewer restarts and
// ends the scene itself, so the preset never loops.
func (run *unitViewerFieldRun) preset() nlPreset {
	return nlPreset{
		scene: film.Scene{Kind: "battle", Map: run.site.name, Seed: 7, PreTicks: unitViewerFieldPreTicks, Anchor: []int32{run.site.x, run.site.z}},
		camera: func(float64) (float64, float64, float64) {
			return 0, -unitViewerFieldLow * float64(run.h) / run.zoom, run.zoom
		},
		loop:      math.Inf(1),
		centre:    true,
		continues: true,
		stage:     run.stage,
	}
}

// stage clears the ground about the site's anchor, stands the record on the
// nearest ground the placement validator accepts, and schedules its death.
// The record is created as it is, never substituted: one hidden by a
// duplicate name, which no battle can create by name, is refused.
func (run *unitViewerFieldRun) stage(st *nlStage) {
	s := st.s
	records := s.Catalog.UnitRecords()
	k := run.key.ordinal
	if k < 0 || k >= len(records) || records[k] == nil || content.CanonicalKey(records[k].UnitName) != run.key.unit {
		run.err = "the battle's catalog does not hold this record"
		return
	}
	def := records[k]
	if first, ok := s.Catalog.Unit(def.UnitName); !ok || first != def {
		run.err = "another record of this name hides it, so no battle creates it"
		return
	}
	run.def = def
	for depth := uint8(1); depth <= 15; depth++ {
		f := combat.ResolveCorpse(def, s.Catalog.Features, depth)
		if f == nil {
			break
		}
		run.chain = append(run.chain, f)
	}
	st.requireUnit(def.UnitName)
	// Nothing else stands near the unit: the map's trees and other
	// destructible features about the anchor go first.
	cx, cz := st.cx, st.cz
	nlClear(s, cx-480, cz-400, cx+480, cz+480)
	x, z, y, ok := st.spot(def, cx, cz, unitViewerFieldRings)
	if !ok {
		run.refused = strings.TrimPrefix(nlRefusal(s, def, cx, cz), ": ")
		return
	}
	h, err := s.Units.Create(def, s.LocalOwner, nlFixed(x), y, nlFixed(z))
	if err != nil {
		run.err = err.Error()
		return
	}
	u := s.Units.Unit(h)
	if s.Movement != nil {
		s.Movement.EnsureUnit(u)
	}
	s.BindStagedOrderQueue(u)
	nlHold(u)
	run.unit, run.x, run.z = u, x, z
	st.cx, st.cz = x, z
	run.zoom = unitViewerFieldZoom(def, run.w)
	at := unitViewerFieldPreTicks + unitViewerFieldSettle
	st.at(at, func(s *session.Session, _ int) { run.kill(s) })
	st.every(at+1, 1, func(s *session.Session, _ int) { run.observe(s) })
}

// kill runs the battle's death path with the inputs that give the selected
// severity (unitViewerFieldDeathInputs), as the settings scenes' kills do
// (nlKillWith): the unit's own Killed script, explosion and corpse follow
// on this tick's unit phase [06 §12.1].
func (run *unitViewerFieldRun) kill(s *session.Session) {
	u := run.unit
	if u == nil || !u.Alive || u.Dying {
		run.err = "the unit was gone before its death"
		return
	}
	health, prior, ok := unitViewerFieldDeathInputs(u.MaxHealth, run.key.severity)
	if !ok {
		run.err = fmt.Sprintf("no health a battle reaches gives severity %d at %d hit points", run.key.severity, u.MaxHealth)
		return
	}
	run.health, run.prior = health, prior
	run.x, run.z = int32(u.X.Int()), int32(u.Z.Int())
	nlKillWith(s, u, health, prior)
	run.killed = true
}

// observe records the corpse once the death is resolved: a feature of the
// unit's corpse chain on the footprint where it died.
func (run *unitViewerFieldRun) observe(s *session.Session) {
	if !run.killed || run.settled || run.unit == nil || run.unit.Alive {
		return
	}
	run.settled = true
	if s == nil || s.Features == nil {
		return
	}
	reach := max(run.def.FootprintX, run.def.FootprintZ, 1)*8 + 16
	for _, f := range s.Features.Instances() {
		if f == nil || f.Def == nil {
			continue
		}
		depth := slices.Index(run.chain, f.Def) + 1
		if depth == 0 {
			continue
		}
		fx := int32(f.CX)*16 + max(f.Def.FootprintX, 1)*8
		fz := int32(f.CZ)*16 + max(f.Def.FootprintZ, 1)*8
		if wsAbs(fx-run.x) <= reach && wsAbs(fz-run.z) <= reach {
			run.corpse, run.depth = f.Def, depth
			return
		}
	}
}

// ------------------------------------------------------------- the viewer

type unitViewerFieldPhase uint8

const (
	unitViewerFieldOff     unitViewerFieldPhase = iota
	unitViewerFieldStaging                      // the scene is being staged
	unitViewerFieldShown                        // the field is on the stage
	unitViewerFieldFailed                       // the field cannot show the death; the status says why
	unitViewerFieldHanded                       // Wreck: the death has played; the turntable shows the corpse
)

// unitViewerField is the viewer's side of the field: which death it shows,
// the preview staging and stepping it, and the battles it has left whose
// staging is still running.
type unitViewerField struct {
	preview  *nlPreview
	gen      uint64 // the model's action generation this field serves; 0 for none
	action   unitViewerAction
	def      *content.UnitDef
	ordinal  int
	severity int32
	phase    unitViewerFieldPhase
	err      string
	run      *unitViewerFieldRun // the run on the stage
	dt       float64             // host time since the last frame
	pending  []chan nlPreviewResult
	used     bool // a field battle took the model source during this opening
	capture  bool // a capture steps the field itself and never restarts it
}

// onStage reports whether the field, not the turntable, owns the stage.
func (f *unitViewerField) onStage() bool {
	return f.phase == unitViewerFieldStaging || f.phase == unitViewerFieldShown || f.phase == unitViewerFieldFailed
}

// syncField starts, restarts or leaves the field to match the chosen action:
// Death and Wreck play in the field; every other action, another unit and
// a closed viewer leave it. Choosing Death or Wreck again, or another
// severity, makes a new action generation, which stages a fresh field.
func (s *toolsScreen) syncField() {
	a := unitViewerAction(s.action)
	want := s.viewer && !s.closing && s.selected != nil && s.cs != nil && s.model.gen != 0 &&
		(a == unitViewerDeath || a == unitViewerWreck) && unitViewerAnimationAvailable(s.selected, a, s.weapon)
	if !want {
		s.field.leave()
		return
	}
	if s.field.gen == s.model.gen {
		return
	}
	s.field.enter(s, a)
}

func (f *unitViewerField) enter(s *toolsScreen, action unitViewerAction) {
	f.leave()
	f.gen, f.action, f.def = s.model.gen, action, s.selected
	f.severity = unitViewerSeverities[s.severity]
	f.ordinal = -1
	if cat, err := s.cs.nlPreviewCatalog(); err == nil {
		f.ordinal = slices.Index(cat.UnitRecords(), s.selected)
	}
	f.phase = unitViewerFieldStaging
	switch {
	case f.ordinal < 0:
		f.fail("the running content's catalog does not hold this record")
	case content.CanonicalKey(s.selected.UnitName) == "":
		f.fail("the record has no unit name, so no battle creates it")
	default:
		if _, _, ok := unitViewerFieldDeathInputs(s.selected.MaxDamage, f.severity); !ok {
			f.fail(fmt.Sprintf("no health a battle reaches gives this severity at %d hit points", s.selected.MaxDamage))
			return
		}
		f.preview = newNLPreview(s.opts, s.cs)
		f.preview.noCache = true
		f.used = true
	}
}

// fail ends the field with a reason; nothing is drawn in its place.
func (f *unitViewerField) fail(reason string) {
	f.retire()
	f.phase, f.err = unitViewerFieldFailed, reason
}

// leave closes the field's battle so it holds no CPU, and forgets it.
func (f *unitViewerField) leave() {
	f.retire()
	f.gen, f.phase, f.err, f.run, f.dt, f.capture = 0, unitViewerFieldOff, "", nil, 0, false
}

// retire closes the preview's battles. A scene still staging cannot be
// stopped; its result is closed when it arrives (poll), and closing the
// viewer waits for it (release) so nothing reads the content afterwards.
func (f *unitViewerField) retire() {
	p := f.preview
	f.preview = nil
	if p == nil {
		return
	}
	if p.loading {
		f.pending = append(f.pending, p.results)
		p.loading = false
	}
	p.Close()
}

// poll closes retired scenes that have finished staging.
func (f *unitViewerField) poll() {
	f.pending = slices.DeleteFunc(f.pending, func(ch chan nlPreviewResult) bool {
		select {
		case res := <-ch:
			if res.inst != nil {
				res.inst.close()
			}
			return true
		default:
			return false
		}
	})
}

// release leaves the field, waits for every staging still running and
// hands the model source back to the window's client, as the Nanolathe
// screen does when it closes.
func (f *unitViewerField) release(cs *contentSet) {
	f.leave()
	for _, ch := range f.pending {
		if res := <-ch; res.inst != nil {
			res.inst.close()
		}
	}
	f.pending = nil
	if f.used && cs != nil && clPtr != nil {
		clPtr.SetModelFS(cs.unmappedMount, cs.presentation.TeamLogos)
	}
	f.used = false
}

// fieldSize is the field picture's size: the stage's device rectangle,
// bounded as the turntable's is.
func (s *toolsScreen) fieldSize() (int, int) {
	stage := s.deviceRect(s.viewRect)
	k := min(1, 2048/max(stage.W, stage.H, 1))
	return max(2, int(stage.W*k)), max(2, int(stage.H*k))
}

func (f *unitViewerField) sceneKey(s *toolsScreen, w, h int) nlSceneKey {
	return nlSceneKey{preset: "field", gameplay: s.fieldGameplay, w: w, h: h, field: nlFieldKey{
		unit: content.CanonicalKey(f.def.UnitName), ordinal: f.ordinal, severity: f.severity, run: f.gen,
	}}
}

// frame advances the field by the host time since the last frame and
// returns its picture, with the outgoing run's last picture fading over it
// after a restart; nil while the field stages. Death restarts the field once
// the hold has passed; Wreck hands the stage to the turntable's corpse.
func (s *toolsScreen) fieldFrame() (img, fade *ebiten.Image, fadeAlpha float64) {
	f := &s.field
	p := f.preview
	if p == nil || (f.phase != unitViewerFieldStaging && f.phase != unitViewerFieldShown) {
		return nil, nil, 0
	}
	w, h := s.fieldSize()
	key := f.sceneKey(s, w, h)
	p.request(key)
	dt := f.dt
	f.dt = 0
	p.Frame(dt, s.fieldRender, nil)
	if !s.settleField(key) {
		return nil, nil, 0
	}
	if p.fade != nil && p.fadeLeft > 0 {
		fade, fadeAlpha = p.fade, p.fadeLeft
	}
	return p.frame, fade, fadeAlpha
}

// settleField reads the field after its preview has stepped: a staging
// error or a run that cannot show its death fails the field; once the hold
// after the death has passed, Death stages its next run and Wreck hands over
// to the turntable. It reports whether the field's picture is on the stage.
func (s *toolsScreen) settleField(key nlSceneKey) bool {
	f := &s.field
	p := f.preview
	if p == nil {
		return false
	}
	if p.cur == nil || p.cur.key != key {
		if p.lastErr != "" {
			f.fail(p.lastErr)
		}
		return false
	}
	run := p.cur.field
	if run == nil {
		f.fail("the scene is not a field")
		return false
	}
	run.observe(p.cur.sess)
	if run.err != "" {
		f.fail(run.err)
		return false
	}
	f.phase, f.run = unitViewerFieldShown, run
	if p.cur.ticks < unitViewerFieldSettle+unitViewerFieldHold || f.capture {
		return true
	}
	if f.action == unitViewerWreck {
		s.handOverWreck()
		return false
	}
	if !p.loading {
		p.startLoad(key)
	}
	return true
}

// handOverWreck closes the field after Wreck's death has played and shows
// the corpse it left on the turntable.
func (s *toolsScreen) handOverWreck() {
	f := &s.field
	run := f.run
	f.retire()
	f.phase = unitViewerFieldHanded
	if run == nil {
		return
	}
	var corpse *content.FeatureDef
	if run.corpse != nil {
		// The battle's catalog is a clone; the turntable draws the running
		// content's own record of the same feature.
		corpse = s.features[run.corpse.CanonicalKey]
	}
	s.model.adoptFieldWreck(s.cs, s.selected, corpse, run.depth)
}

// stageFieldNow stages the chosen field synchronously and runs it for ticks
// field ticks without a clock, for a reproducible capture. Wreck hands over
// to the turntable once its hold has passed.
func (s *toolsScreen) stageFieldNow(ticks int) {
	s.syncField()
	f := &s.field
	if f.phase != unitViewerFieldStaging || f.preview == nil {
		return
	}
	f.capture = true
	w, h := s.fieldSize()
	key := f.sceneKey(s, w, h)
	p := f.preview
	p.want, p.loadStarted = key, time.Now()
	inst, err := buildNLPreview(s.opts, s.cs, key)
	if err != nil {
		f.fail(err.Error())
		return
	}
	p.activate(nlCachedPreview{inst: inst})
	for inst.ticks < ticks {
		inst.applyCamera(0)
		inst.advance()
		inst.ticks++
		if run := inst.field; run != nil {
			run.observe(inst.sess)
		}
		if f.action == unitViewerWreck && inst.ticks >= unitViewerFieldSettle+unitViewerFieldHold {
			f.run = inst.field
			if f.run != nil && f.run.err == "" {
				s.handOverWreck()
			}
			return
		}
	}
}

// fieldStatus is the status line while the field owns the stage.
func (s *toolsScreen) fieldStatus() (string, bool) {
	f := &s.field
	head := fmt.Sprintf("%s / severity %d", f.action, f.severity)
	switch f.phase {
	case unitViewerFieldStaging:
		return head + " / staging the field", true
	case unitViewerFieldFailed:
		return head + " / " + f.err, true
	case unitViewerFieldShown:
		run := f.run
		switch {
		case run == nil || !run.killed:
			return head + " / in the field", true
		case !run.settled:
			return head + " / killed", true
		case run.corpse == nil:
			return head + " / no corpse", true
		}
		note := fmt.Sprintf("%s / %s depth %d / %d metal", head, strings.ToUpper(run.corpse.CanonicalKey), run.depth, run.corpse.Metal)
		return note, true
	}
	return "", false
}

// unitViewerFieldRender is how the field is drawn: the player's own
// presentation choices, as the Nanolathe screen's previews take them.
func unitViewerFieldRender(pres settings.Presentation, display settings.Display) nlRender {
	d := nlDraft{pres: pres, glow: display.Glow, glowStrength: display.GlowStrength}
	if d.glowStrength < 0 {
		d.glowStrength = settings.DefaultGlowStrength
	}
	r := nlRender{classic: pres.Renderer == "classic", fps: pres.FPS}
	applyNLDraftEffects(&d, &r)
	return r
}
