package main

import (
	"maps"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// These lock the unit viewer's restriction editor (docs/DESIGN_MODS_MUTATORS.md
// §15.9, the editor row of §15.11): Nanolathe presentation contracts, plus
// retail's slider ends and Reset [08 R-SKIR-01 §10].

// restrictEditorFixture is an authored catalog for the editor: ordinary
// units, a side's commander that is not norestrict (the stock ones are), a
// norestrict unit, a wacky unit, and a second record carrying ARMPW's name.
func restrictEditorFixture() (*content.Catalog, []unitViewerEntry) {
	unit := func(name, label string, f func(*content.UnitDef)) *content.UnitDef {
		u := &content.UnitDef{UnitName: name, Name: label, CanonicalKey: content.CanonicalKey(name), Limit: -1}
		if f != nil {
			f(u)
		}
		return u
	}
	cat := &content.Catalog{
		Units: map[string]*content.UnitDef{
			"armcom":    unit("ARMCOM", "Commander", func(u *content.UnitDef) { u.Commander = true }),
			"armflash":  unit("ARMFLASH", "Flash", nil),
			"armpw":     unit("ARMPW", "Peewee", nil),
			"armshield": unit("ARMSHIELD", "Shield", func(u *content.UnitDef) { u.NoRestrict = true }),
			"armwack":   unit("ARMWACK", "Wacky", func(u *content.UnitDef) { u.Wacky = true }),
			"corak":     unit("CORAK", "A.K.", nil),
		},
		Sides: []*content.SideDef{{Index: 0, Commander: "armcom"}},
	}
	entries := unitViewerEntries(cat)
	// A record hidden by a duplicate name: the catalog lookup returns the
	// other one, and the entry covers both (§15.3, proposal R-P1).
	entries = append(entries, unitViewerEntry{Key: "armpw", Def: unit("ARMPW", "Peewee", nil)})
	return cat, entries
}

func restrictEditor(t *testing.T, draft map[string]int) *unitViewerRestrict {
	t.Helper()
	cat, entries := restrictEditorFixture()
	e := &unitViewerRestrict{draft: restrictionSet(t, draft)}
	e.names, e.keys = unitViewerRestrictNames(cat, entries)
	return e
}

func TestUnitViewerRestrictRowStates(t *testing.T) {
	e := restrictEditor(t, map[string]int{"armpw": 20, "corak": 0, "armcom": 5, "armshield": 1})
	for key, want := range map[string]unitViewerRestrictState{
		"armflash":  {},
		"armpw":     {text: "Max 20"},
		"corak":     {text: "Removed", removed: true},
		"armshield": {text: "Norestrict", lock: true},
		"armcom":    {text: "Max 5", lock: true},
	} {
		if got := e.rowState(key); got != want {
			t.Errorf("%s row = %+v, want %+v", key, got, want)
		}
	}
	e.draft.Clear("armcom")
	if got := e.rowState("armcom"); got != (unitViewerRestrictState{text: "Commander", lock: true}) {
		t.Errorf("uncapped commander row = %+v, want its padlock and reason", got)
	}
	// Both records carrying ARMPW's name show its state, and the entry says
	// it covers both.
	_, entries := restrictEditorFixture()
	for _, entry := range entries {
		if entry.Def.UnitName == "ARMPW" {
			if got := e.rowState(unitViewerRestrictKey(entry.Def)); got.text != "Max 20" {
				t.Errorf("an ARMPW record shows %+v", got)
			}
			if note, _ := e.note(entry.Def); note != "Covers all 2 records named ARMPW." {
				t.Errorf("duplicate note %q", note)
			}
		}
	}
	if removed, capped, leftOut := e.tally(); removed != 1 || capped != 1 || leftOut != 1 {
		t.Errorf("tally %d removed, %d capped, %d left out; want 1, 1, 1", removed, capped, leftOut)
	}
}

// The stepper runs 0..100 with No limit above: down from No limit gives 100
// and up from 100 gives No limit [08 R-SKIR-01 §10], while up from No limit
// starts a cap at 1 (ten when coarse), so the plus button cycles; it stops at
// 0, at 1 for a commander, and never moves a norestrict name.
func TestUnitViewerRestrictStepperEnds(t *testing.T) {
	e := restrictEditor(t, nil)
	steps := []struct {
		dir    int
		coarse bool
		want   int
		moved  bool
	}{
		{1, false, 1, true},
		{1, false, 2, true},
		{1, false, 3, true},
		{-1, false, 2, true},
		{1, true, 10, true},
		{1, true, 20, true},
		{-1, true, 10, true},
		{-1, true, 0, true},
		{1, false, 1, true},
		{-1, false, 0, true},
		{-1, false, 0, false},
		{-1, true, 0, false},
	}
	for i, st := range steps {
		if moved := e.step("armpw", st.dir, st.coarse); moved != st.moved || e.value("armpw") != st.want {
			t.Fatalf("step %d from No limit: value %d moved %v, want %d moved %v", i, e.value("armpw"), moved, st.want, st.moved)
		}
	}
	// The top end runs as retail's slider does, and a coarse step up from No
	// limit starts at ten.
	ends := []struct {
		from, dir int
		coarse    bool
		want      int
	}{
		{unitViewerNoLimit, -1, false, 100},
		{unitViewerNoLimit, -1, true, 100},
		{100, 1, false, unitViewerNoLimit},
		{95, 1, true, 100},
		{100, 1, true, unitViewerNoLimit},
		{91, -1, true, 90},
		{unitViewerNoLimit, 1, true, 10},
	}
	for _, c := range ends {
		_ = e.setValue("armpw", c.from)
		if !e.step("armpw", c.dir, c.coarse) || e.value("armpw") != c.want {
			t.Fatalf("from %d by %d (coarse %v): %d, want %d", c.from, c.dir, c.coarse, e.value("armpw"), c.want)
		}
	}
	// A commander starts its cap at its floor of 1 too.
	_ = e.setValue("armcom", unitViewerNoLimit)
	if !e.step("armcom", 1, false) || e.value("armcom") != 1 {
		t.Fatalf("commander's first cap is %d", e.value("armcom"))
	}
	_ = e.setValue("armpw", 1)
	if !e.step("armpw", -1, false) || e.value("armpw") != 0 || e.step("armpw", -1, true) {
		t.Fatalf("the bottom end: value %d", e.value("armpw"))
	}
	// A commander takes a cap but never 0: its floor is 1 and its switch
	// cannot go Off.
	_ = e.setValue("armcom", 1)
	if e.step("armcom", -1, false) || e.toggle("armcom") || e.value("armcom") != 1 {
		t.Fatalf("commander left its floor: %d", e.value("armcom"))
	}
	if e.step("armshield", -1, false) || e.toggle("armshield") || e.setValue("armshield", 5) {
		t.Fatal("a norestrict name took a count")
	}
	// The switch: On is No limit and Off is 0, so a cap switches Off.
	_ = e.setValue("corak", 7)
	if !e.toggle("corak") || e.value("corak") != 0 || !e.toggle("corak") || e.value("corak") != unitViewerNoLimit {
		t.Fatalf("switch from a cap went to %d", e.value("corak"))
	}
}

// Reset empties the set: every unit back to No limit, a wacky name and the
// entries the content leaves out included, rather than retail's board of
// 100s (§15.9). It greys once the set is empty.
func TestUnitViewerRestrictResetUncaps(t *testing.T) {
	cat, entries := restrictEditorFixture()
	s := &toolsScreen{entries: entries, restrict: unitViewerRestrict{draft: restrictionSet(t, map[string]int{"armpw": 3, "armwack": 0, "armshield": 1, "notaunit": 0})}}
	s.restrict.names, s.restrict.keys = unitViewerRestrictNames(cat, entries)
	if !s.activateRestrict("RRESET") || !s.restrict.draft.IsZero() {
		t.Fatalf("Reset left %v", s.restrict.draft.Map())
	}
	if removed, capped, leftOut := s.restrict.tally(); removed+capped+leftOut != 0 {
		t.Fatalf("Reset left %d removed, %d capped, %d left out", removed, capped, leftOut)
	}
}

// Restricted only narrows the library together with the search; a single
// edit keeps the list, and a bulk edit applies the filter again.
func TestUnitViewerRestrictedOnlyNarrowsWithSearch(t *testing.T) {
	cat, entries := restrictEditorFixture()
	s := &toolsScreen{entries: entries, restrict: unitViewerRestrict{draft: restrictionSet(t, map[string]int{"armpw": 20, "corak": 0})}}
	s.restrict.names, s.restrict.keys = unitViewerRestrictNames(cat, entries)
	s.activateRestrict("RESTRONLY")
	keys := func() (out []string) {
		for _, e := range s.filtered {
			out = append(out, e.Key)
		}
		return out
	}
	if got := keys(); len(got) != 3 || got[0] != "corak" || got[1] != "armpw" || got[2] != "armpw" {
		t.Fatalf("Restricted only listed %v, want A.K. and both Peewee records", got)
	}
	s.query = "pee"
	s.filter()
	if got := keys(); len(got) != 2 || got[0] != "armpw" {
		t.Fatalf("Restricted only with a search listed %v", got)
	}
	s.selectUnit(s.filtered[0].Def)
	s.activateRestrict("RSWITCH")
	s.activateRestrict("RSWITCH")
	if _, restricted := s.restrict.draft.Count("armpw"); restricted || len(s.filtered) != 2 {
		t.Fatalf("a single edit changed the list: %v", keys())
	}
	s.query = ""
	s.activateRestrict("RRESET")
	if !s.restrict.draft.IsZero() || len(s.filtered) != 0 {
		t.Fatalf("Reset left %v listed", keys())
	}
}

// waitUnitViewerLoad joins the viewer's catalog worker.
func waitUnitViewerLoad(s *toolsScreen) {
	for s.loading != nil {
		s.pollLoad()
		runtime.Gosched()
	}
}

// Opened with Ctrl+U, the editor starts from the running content's saved
// set, shows Apply with the changed names once the draft differs, and Apply
// writes the set to the running content's layer; Back discards a draft.
func TestUnitViewerRestrictMenuApplyAndBack(t *testing.T) {
	t.Setenv(settings.EnvPath, filepath.Join(t.TempDir(), "settings.json"))
	g := &gameShell{cs: restrictionTestContent(nil)}
	file := settings.Defaults()
	file.Restrictions = map[string]int{"armpw": 20, "notaunit": 0}
	g.applySettings(file)
	g.settingsWritable = true
	s := &toolsScreen{}
	s.show(g)
	s.openViewer()
	waitUnitViewerLoad(s)
	defer s.release()
	if s.restrict.route != unitViewerRestrictMenu || s.restrict.draft.String() != "armpw=20,notaunit=0" {
		t.Fatalf("the editor opened on %q", s.restrict.draft.String())
	}
	apply := s.panel.Index("APPLY")
	s.refreshControls()
	if s.panel.ActiveAt(apply) {
		t.Fatal("Apply shows with nothing changed")
	}
	_ = s.restrict.setValue("corak", 0)
	_ = s.restrict.setValue("armpw", unitViewerNoLimit)
	s.refreshControls()
	if !s.panel.ActiveAt(apply) || s.panel.TextAt(apply) != "Apply  2" {
		t.Fatalf("Apply reads %q (shown %v), want the two changed names", s.panel.TextAt(apply), s.panel.ActiveAt(apply))
	}
	s.activateRestrict("APPLY")
	if got := g.opts.Restrictions.String(); got != "corak=0" {
		t.Fatalf("battles carry %q after Apply, want corak=0", got)
	}
	saved, err := settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"corak": 0, "notaunit": 0}; !maps.Equal(saved.Restrictions, want) {
		t.Fatalf("the file holds %v, want %v", saved.Restrictions, want)
	}
	s.refreshControls()
	if s.panel.ActiveAt(apply) {
		t.Fatal("Apply still shows after it wrote the draft")
	}
	_ = s.restrict.setValue("armflash", 3)
	s.back()
	if got := g.opts.Restrictions.String(); got != "corak=0" {
		t.Fatalf("Back applied the draft: %q", got)
	}
}
