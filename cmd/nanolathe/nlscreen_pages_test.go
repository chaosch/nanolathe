package main

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// The Unit restrictions card (docs/DESIGN_MODS_MUTATORS.md §15.9): it leads
// the mutator cards, summarises the draft against the running content
// — the numbers removed and capped, four named entries, then how many more —
// names in chips the saved entries the content leaves out, offers no
// comparison, and the loadout line counts the set beside the mutators.
func TestNLRestrictionCardSummary(t *testing.T) {
	g := &gameShell{cs: restrictionTestContent(nil)}
	file := settings.Defaults()
	file.Restrictions = map[string]int{"armpw": 0, "armflash": 3, "corak": 0, "armcom": 2, "armshield": 1, "notaunit": 0, "ArmPW": 4}
	g.applySettings(file)
	s := newNLScreen(func() *gameShell { return g })
	s.draft = s.freshDraft(g)
	s.stats.base, _ = g.cs.nlPreviewCatalog()

	page := s.pages()[1]
	card := page.cards[0]
	if page.key != "mutators" || card.key != "restrictions" || card.compare != nil {
		t.Fatalf("the first Mutators card is %s/%s (compare %v)", page.key, card.key, card.compare != nil)
	}
	sum := s.restrictionSummary(s.draft.restrictions)
	if sum.text != "2 removed, 2 capped" || len(sum.rows) != 4 || sum.more != 0 {
		t.Fatalf("summary %q, %d rows, %d more", sum.text, len(sum.rows), sum.more)
	}
	if r := sum.rows[0]; r.key != "armcom" || r.state != "Max 2" || r.removed {
		t.Fatalf("first row %+v", r)
	}
	want := []string{"armshield=1 left out: cannot be restricted", "notaunit=0 left out: not in this content", "ArmPW=4 left out: unreadable"}
	if strings.Join(sum.leftOut, "|") != strings.Join(want, "|") {
		t.Fatalf("chips %q, want %q", sum.leftOut, want)
	}
	if got := nlCardValueText(card, &s.draft); got != "2 removed, 2 capped" {
		t.Fatalf("carousel value %q", got)
	}
	if got := s.restrictionsLabel(); got != "4 restrictions" {
		t.Fatalf("loadout part %q", got)
	}
	next := nlRestrictionsOf(s.draft.restrictions)
	_ = next.Set("armflash", 9)
	s.setRestrictionDraft(next)
	if !s.touched["restrictions"] || s.draft.restrictions != next.String() {
		t.Fatalf("the edit did not reach the draft: %q", s.draft.restrictions)
	}
	// Arrows and the wheel never step a set.
	before := s.draft
	s.step(card, card.get(&s.draft), 1)
	if s.draft != before {
		t.Fatal("stepping the card changed the draft")
	}
	s.setRestrictionDraft(nlRestrictionsOf(""))
	if sum := s.restrictionSummary(s.draft.restrictions); sum.text != "No restrictions" || s.restrictionsLabel() != "No restrictions" {
		t.Fatalf("an empty draft reads %q", sum.text)
	}
}

// Opened from the Nanolathe screen's card, the editor edits the screen's
// draft and Back hands it back: the card counts the change, Apply writes it
// to the running content's layer, and the screen's own Back discards it.
func TestUnitViewerRestrictCardRouteEditsScreenDraft(t *testing.T) {
	t.Setenv(settings.EnvPath, filepath.Join(t.TempDir(), "settings.json"))
	g := &gameShell{cs: restrictionTestContent(nil)}
	file := settings.Defaults()
	file.Restrictions = map[string]int{"armpw": 20}
	g.applySettings(file)
	g.settingsWritable = true
	s := newNLScreen(func() *gameShell { return g })
	s.draft = s.freshDraft(g)
	previous := toolsScreenInst
	toolsScreenInst = &toolsScreen{}
	t.Cleanup(func() {
		toolsScreenInst.release()
		toolsScreenInst = previous
	})
	edit := func(change func(e *unitViewerRestrict)) {
		t.Helper()
		s.editRestrictions()
		tools := toolsScreenInst
		waitUnitViewerLoad(tools)
		if tools.restrict.route != unitViewerRestrictCard || tools.panel.ActiveAt(tools.panel.Index("APPLY")) {
			t.Fatal("the card's editor offers its own Apply")
		}
		change(&tools.restrict)
		tools.back()
		tools.release()
	}
	edit(func(e *unitViewerRestrict) { _ = e.setValue("corak", 0) })
	if s.draft.restrictions != "armpw=20,corak=0" || !s.touched["restrictions"] || s.dirty() != 1 {
		t.Fatalf("the screen's draft is %q (touched %v, %d changed)", s.draft.restrictions, s.touched["restrictions"], s.changed)
	}
	if g.opts.Restrictions.String() != "armpw=20" {
		t.Fatal("Back from the editor changed the battles' set before Apply")
	}
	s.apply()
	if got := g.opts.Restrictions.String(); got != "armpw=20,corak=0" || s.dirty() != 0 {
		t.Fatalf("Apply: battles carry %q, %d cards changed", got, s.changed)
	}
	if saved, err := settings.Load(); err != nil || !maps.Equal(saved.Restrictions, map[string]int{"armpw": 20, "corak": 0}) {
		t.Fatalf("the file holds %v: %v", saved.Restrictions, err)
	}
	// The screen's Back discards an edit: reopening starts from the setting.
	edit(func(e *unitViewerRestrict) { e.draft = content.Restrictions{} })
	if s.draft.restrictions != "" {
		t.Fatalf("Clear all reached the screen as %q", s.draft.restrictions)
	}
	s.hide()
	s.draft = s.freshDraft(g) // what binding the screen again starts from
	if s.draft.restrictions != "armpw=20,corak=0" || g.opts.Restrictions.String() != "armpw=20,corak=0" {
		t.Fatalf("the screen's Back kept %q", s.draft.restrictions)
	}
}
