package main

import (
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/content"
)

// The field's death inputs give the selected severity through the battle's
// own severity arithmetic [06 §12.1], with a non-positive health that fits
// the signed 16-bit word retail reads and a sampled percentage of at most
// 100. A selection no reachable pair gives is refused, never approximated.
func TestUnitViewerFieldDeathInputsGiveTheSelectedSeverity(t *testing.T) {
	for _, maxHealth := range []int32{1, 7, 49, 50, 99, 100, 250, 326, 2600, 3000, 16384, 29918, 32768, 40000} {
		for _, severity := range unitViewerSeverities {
			health, prior, ok := unitViewerFieldDeathInputs(maxHealth, severity)
			if !ok {
				// Only a severity whose health term needs more than the
				// signed word can hold is out of reach.
				if severity <= 50 || int64(2*severity-100)*int64(maxHealth) <= 32768*100 {
					t.Errorf("maxHealth %d severity %d: refused", maxHealth, severity)
				}
				continue
			}
			if health > 0 || health < -32768 || prior > 100 || cob.KilledSeverity(health, maxHealth, prior) != severity {
				t.Errorf("maxHealth %d severity %d: health %d prior %d gives %d", maxHealth, severity, health, prior, cob.KilledSeverity(health, maxHealth, prior))
			}
		}
	}
	// The stock Peewee's 250 hit points: a unit sampled at half or full
	// health killed at exactly zero, then blows that leave it at -125 and
	// -250.
	want := map[int32][2]int32{25: {0, 50}, 50: {0, 100}, 75: {-125, 100}, 100: {-250, 100}}
	for severity, w := range want {
		if health, prior, _ := unitViewerFieldDeathInputs(250, severity); health != w[0] || int32(prior) != w[1] {
			t.Errorf("severity %d: health %d prior %d, want %d and %d", severity, health, prior, w[0], w[1])
		}
	}
	if _, _, ok := unitViewerFieldDeathInputs(40000, 100); ok {
		t.Error("severity 100 at 40000 hit points needs a health below the signed word")
	}
	if _, _, ok := unitViewerFieldDeathInputs(0, 50); ok {
		t.Error("a maximum health of zero has no severity")
	}
}

// Wreck hands the field's corpse to the turntable: the feature its death
// left, at its chain depth, replacing the presentation script's own answer;
// a death that left none leaves the stage empty and says so.
func TestUnitViewerFieldWreckAdoptsTheFieldCorpse(t *testing.T) {
	heap := &content.FeatureDef{DefinitionHeader: content.DefinitionHeader{CanonicalKey: "heap"}, Object: "heap", Metal: 7}
	wreck := &content.FeatureDef{DefinitionHeader: content.DefinitionHeader{CanonicalKey: "wreck"}, Object: "wreck", Metal: 15, FeatureDead: "heap", FeatureDeadDef: heap}
	features := map[string]*content.FeatureDef{"wreck": wreck, "heap": heap}
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerReturn}},
		// Choose depth 2 whatever the severity.
		viewerScriptFixture{"Killed", []uint32{viewerPush, 2, viewerPopLocal, 1, viewerPush, 0, viewerReturn}},
	)
	def.Corpse = "Wreck"
	cs := &contentSet{}
	m := unitViewerModel{features: features}
	m.loadAnimation(def, mdl)
	m.setAnimation(unitViewerWreck, 1)
	if m.anim.wreckFeature() != heap {
		t.Fatal("the turntable's own query did not choose the heap")
	}
	m.adoptFieldWreck(cs, def, wreck, 1)
	if m.anim.wreckFeature() != wreck || m.anim.death.depth != 1 || !strings.Contains(m.poseNote, "WRECK / depth 1 / 15 metal") {
		t.Fatalf("adopted %v depth %d, note %q", m.anim.wreckFeature(), m.anim.death.depth, m.poseNote)
	}
	m.adoptFieldWreck(cs, def, nil, 0)
	if m.anim.wreckFeature() != nil || !strings.Contains(m.poseNote, "left no corpse in the field") {
		t.Fatalf("no corpse drew %v, note %q", m.anim.wreckFeature(), m.poseNote)
	}
	// Every choice is a new generation, so the field stages afresh.
	gen := m.gen
	m.setAnimation(unitViewerWreck, 1)
	if m.gen == gen {
		t.Fatal("choosing Wreck again kept the field's generation")
	}
}

// unitViewerFieldScreen opens the viewer on the retail catalog as a capture
// does, with name selected.
func unitViewerFieldScreen(t *testing.T, name string) *toolsScreen {
	t.Helper()
	opts, cs := openNLTestContent(t)
	s := &toolsScreen{}
	s.show(&gameShell{cs: cs, opts: opts})
	t.Cleanup(s.release)
	cat, err := cs.nlPreviewCatalog()
	if err != nil {
		t.Fatal(err)
	}
	s.viewer, s.entries, s.features = true, unitViewerEntries(cat), cat.Features
	s.buildPanel()
	s.layout(1440, 900)
	s.filter()
	def, ok := cat.Unit(name)
	if !ok {
		t.Fatalf("no %s", name)
	}
	s.selectUnit(def)
	return s
}

// The field follows the chosen action: Death stages a real battle whose
// unit dies at the selected severity and leaves its corpse; another
// severity stages afresh; Death restarts once its hold has passed; Idle
// leaves the field and closes its battle; Wreck hands its corpse to the
// turntable and closes its battle; and closing the viewer joins a staging
// still running.
func TestUnitViewerFieldLifecycleRetail(t *testing.T) {
	s := unitViewerFieldScreen(t, "armpw")
	s.activateTool("DEATH")
	s.syncField()
	if s.field.phase != unitViewerFieldStaging || s.field.preview == nil || s.field.gen != s.model.gen {
		t.Fatalf("Death did not stage a field: phase %d", s.field.phase)
	}
	s.stageFieldNow(unitViewerFieldSettle + 2)
	inst := s.field.preview.cur
	run := inst.field
	if !run.killed || !run.settled || run.unit.Alive || run.corpse == nil || run.corpse.CanonicalKey != "armpw_dead" || run.depth != 1 {
		t.Fatalf("severity 25 death: killed %v settled %v corpse %v depth %d (%s)", run.killed, run.settled, run.corpse, run.depth, run.err)
	}
	if got := cob.KilledSeverity(run.health, run.unit.MaxHealth, run.prior); got != 25 {
		t.Fatalf("the death's inputs give severity %d", got)
	}
	// Another severity is a new generation and a fresh battle.
	s.activateTool("SEVERITY")
	s.syncField()
	if !inst.closed || s.field.phase != unitViewerFieldStaging || s.field.severity != 50 {
		t.Fatalf("Severity kept the old field: closed %v phase %d severity %d", inst.closed, s.field.phase, s.field.severity)
	}
	// Once the hold has passed, Death stages its next run.
	s.stageFieldNow(unitViewerFieldSettle + unitViewerFieldHold)
	s.field.capture = false
	inst = s.field.preview.cur
	if !s.settleField(inst.key) || !s.field.preview.loading {
		t.Fatal("Death did not restart after its hold")
	}
	// Idle leaves the field: its battle closes and the restart still
	// staging is closed when it arrives.
	s.activateTool("IDLE")
	s.syncField()
	if s.field.phase != unitViewerFieldOff || s.field.preview != nil || !inst.closed || len(s.field.pending) != 1 {
		t.Fatalf("Idle left the field running: phase %d closed %v pending %d", s.field.phase, inst.closed, len(s.field.pending))
	}
	// Wreck plays the same death, then the turntable shows its corpse.
	s.activateTool("WRECK")
	s.stageFieldNow(unitViewerFieldSettle + unitViewerFieldHold)
	if s.field.phase != unitViewerFieldHanded || s.field.preview != nil || s.model.anim.wreckFeature() != s.features["armpw_heap"] || s.model.anim.death.depth != 2 {
		t.Fatalf("Wreck did not hand its corpse over: phase %d corpse %v (%s)", s.field.phase, s.model.anim.wreckFeature(), s.model.poseNote)
	}
	s.field.release(s.cs)
	if len(s.field.pending) != 0 {
		t.Fatal("closing the viewer left a staging running")
	}
}

// A ship needs water: the placement validator refuses it on the ground site
// and the field stands it at the water site instead.
func TestUnitViewerFieldStagesShipsOnWaterRetail(t *testing.T) {
	s := unitViewerFieldScreen(t, "armroy")
	s.activateTool("DEATH")
	s.stageFieldNow(1)
	run := s.field.preview.cur.field
	if run.err != "" || !run.site.water || run.unit == nil {
		t.Fatalf("ship field: site %s, err %q", run.site.name, run.err)
	}
}
