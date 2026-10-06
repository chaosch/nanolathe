package main

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// The unit restrictions are a match selection, not a preference: no preset
// part holds them, saving a preset never captures them and applying one never
// changes them (docs/DESIGN_MODS_MUTATORS.md §15.9).
func TestNLPresetsExcludeRestrictions(t *testing.T) {
	for _, scope := range nlPresetScopes {
		for _, p := range scope.paths() {
			if p == "restrictions" || strings.HasPrefix(p, "restrictions.") {
				t.Fatalf("preset part %q holds %s", scope.label, p)
			}
		}
	}
	t.Setenv(settings.EnvPath, filepath.Join(t.TempDir(), "settings.json"))
	g := &gameShell{cs: restrictionTestContent(nil)}
	file := settings.Defaults()
	file.Restrictions = map[string]int{"armpw": 20}
	g.applySettings(file)
	g.settingsWritable = true
	s := newNLScreen(func() *gameShell { return g })
	s.draft = s.freshDraft(g)

	s.savePreset("Mine")
	if len(g.presets) != 1 {
		t.Fatalf("presets %+v", g.presets)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(g.presets[0].Settings, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["restrictions"]; ok {
		t.Fatalf("the saved preset holds the restrictions: %s", g.presets[0].Settings)
	}
	// A preset saved by an older build may hold a set: no part applies it.
	draft := s.draft.restrictions
	s.applyPresetToDraft(nlPresetEntry{name: "Old", patch: json.RawMessage(`{"restrictions":{"corak":0},"presentation":{"glint":0}}`)}, []bool{true, true, true})
	if s.draft.restrictions != draft || s.touched["restrictions"] || s.draft.pres.Glint != 0 {
		t.Fatalf("the preset moved the restrictions to %q (touched %v) or lost its glint", s.draft.restrictions, s.touched["restrictions"])
	}
	s.apply()
	if !maps.Equal(g.restrictions.saved, map[string]int{"armpw": 20}) || g.opts.Restrictions.String() != "armpw=20" {
		t.Fatalf("applying a preset changed the set: saved %v, battles %q", g.restrictions.saved, g.opts.Restrictions.String())
	}
}
