package main

import (
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// TestLoadingStageMapping locks the two things the loading screen must not get
// wrong: every family the loaders report is attributed to a bar, and a bar's
// percentage is the mean over its own families so it rises in steps.
func TestLoadingStageMapping(t *testing.T) {
	reported := []string{
		content.FamilyWeapons, content.FamilyUnits, content.FamilyFeatures,
		content.FamilyMovement, content.FamilySides, content.FamilySounds,
		content.FamilyMaps, content.FamilyAIProfiles, content.FamilyBattleTables,
		content.FamilyBuildMenus, content.FamilyModels,
		session.FamilyTerrain, session.FamilyUnitWorld,
		session.FamilyPlacement, session.FamilyScripts,
		familyDetailArt,
	}
	for _, family := range reported {
		if _, ok := retailLoadStageOf[family]; !ok {
			t.Errorf("family %q is reported but drives no loading bar", family)
		}
	}

	l := newLoadingState("Canal Crossing")
	// Explosions is fed by weapons and features: one of two done is half a bar.
	l.report(content.FamilyWeapons, 100)
	if got := l.percent[5].Load(); got != 50 {
		t.Errorf("Explosions after weapons = %d, want 50", got)
	}
	l.report(content.FamilyFeatures, 100)
	if got := l.percent[5].Load(); got != 100 {
		t.Errorf("Explosions after features = %d, want 100", got)
	}
	// Terrain is fed by the map census, the terrain load and the load-time
	// remaster (DESIGN_GPU_RENDERER §14.4), and the census reports a running
	// percentage rather than only completion: half of one of three families is
	// a sixth of the bar.
	l.report(content.FamilyMaps, 50)
	if got := l.percent[1].Load(); got != 16 {
		t.Errorf("Terrain at half the census = %d, want 16", got)
	}
	// Every load drives the remaster family to 100, including one that
	// synthesizes nothing, so the bar always completes.
	l.report(content.FamilyMaps, 100)
	l.report(session.FamilyTerrain, 100)
	l.report(familyDetailArt, 100)
	if got := l.percent[1].Load(); got != 100 {
		t.Errorf("Terrain with every family done = %d, want 100", got)
	}
}

// TestRetailLoadBarGeometry locks the authored row geometry against the
// authored by the loading-screen contract [07 "The loading screen"].
func TestRetailLoadBarGeometry(t *testing.T) {
	wantY := []int{0x87, 0xb1, 0xda, 0x106, 0x130, 0x15b}
	wantLabel := []string{"Textures", "Terrain", "Units", "Animation", "3D Data", "Explosions"}
	for i, row := range retailLoadBars {
		if row.y != wantY[i] || row.label != wantLabel[i] {
			t.Errorf("row %d = %q at %d, want %q at %d", i, row.label, row.y, wantLabel[i], wantY[i])
		}
	}
	// A finished bar is exactly the 351-wide LIGHTBAR frame it is stamped
	// under: left + 100*7/2 inclusive.
	if w := 100*7/2 + 1; w != 351 {
		t.Errorf("full bar width = %d, want 351", w)
	}
}

// A phase may finish before the whole remaster does (including cache hits).
// Only overall completion dismisses the popup; phase reports must not dilute
// the six existing bars (DESIGN_GPU_RENDERER §14.4, Nanolathe presentation).
func TestRemasterProgressLifetime(t *testing.T) {
	l := newLoadingState("Canal Crossing")
	l.report(familyDetailArt, 0)
	l.report(familyDetailTiles, 100)
	l.report(familyDetailArt, 50)
	if p := l.remaster.Load(); p == nil || p.label != "Remastering map tiles" || p.percent != 100 {
		t.Fatalf("completed terrain phase lost its status: %+v", p)
	}
	l.report(familyDetailSprites, 25)
	if p := l.remaster.Load(); p == nil || p.label != "Remastering sprites" || p.percent != 25 {
		t.Fatalf("sprite phase inherited terrain progress: %+v", p)
	}
	if got := l.percent[1].Load(); got != 16 {
		t.Fatalf("phase progress changed Terrain attribution: %d", got)
	}
	l.report(familyDetailArt, 100)
	if l.remaster.Load() != nil {
		t.Fatal("completed remaster left the popup active")
	}
	for _, enabled := range []bool{false, true} {
		l := newLoadingState("")
		// Missing inputs take the synthesis fallback; disabled remastering
		// takes the other early return. Neither may leave an active popup.
		detailArtFor(Options{AutoRemaster: enabled}, nil, nil, l.report)
		if l.remaster.Load() != nil {
			t.Fatalf("early return left popup active (enabled=%v)", enabled)
		}
	}
}

// The loading screen's unit-restriction lines (docs/DESIGN_MODS_MUTATORS.md
// §8.3, §15.9): the count a skirmish or Survival battle enters with, under the
// mutators, and the notice naming saved entries the content leaves out; a
// campaign mission draws neither, and a long notice ends with how many more.
func TestLoadingRestrictionLines(t *testing.T) {
	g := &gameShell{cs: restrictionTestContent(nil)}
	file := settings.Defaults()
	file.Restrictions = map[string]int{"armpw": 20, "corak": 0, "armflash": 0, "notaunit": 0}
	g.applySettings(file)
	g.loading = newLoadingState("Canal Crossing")
	want := []string{"Restrictions: 2 removed, 1 capped", "Unit restrictions left out: notaunit=0 (not in this content)"}
	if got := g.loadingRestrictionLines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", got, want)
	}
	g.loading = newLoadingState("")
	if got := g.loadingRestrictionLines(); len(got) != 0 {
		t.Fatalf("a campaign mission draws %q", got)
	}
	omitted := []restrictionOmission{{unit: "a", reason: "gone"}, {unit: "b", reason: "gone"}, {unit: "c", reason: "gone"}}
	measure := func(text string) int { return len(text) }
	full := restrictionNoticeLine(omitted)
	if got := fitRestrictionNotice(omitted, measure, len(full)); got != full {
		t.Fatalf("a notice that fits became %q", got)
	}
	if got := fitRestrictionNotice(omitted, measure, len(full)-1); got != "Unit restrictions left out: a=0 (gone), b=0 (gone) and 1 more" {
		t.Fatalf("a long notice became %q", got)
	}
}
