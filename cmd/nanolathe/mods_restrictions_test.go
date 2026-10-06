package main

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/modlibrary"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// restrictionTestContent is an authored content set whose immutable catalog
// is already in place: three ordinary units, a side's commander that is not
// norestrict (the stock ones are), and a norestrict unit. mod is the mounted
// mod, nil for the original game.
func restrictionTestContent(mod *modlibrary.Mod) *contentSet {
	unit := func(name string, f func(*content.UnitDef)) *content.UnitDef {
		u := &content.UnitDef{UnitName: name, CanonicalKey: content.CanonicalKey(name), Limit: -1}
		if f != nil {
			f(u)
		}
		return u
	}
	cat := &content.Catalog{
		Units: map[string]*content.UnitDef{
			"armcom":    unit("ARMCOM", func(u *content.UnitDef) { u.Commander = true }),
			"armflash":  unit("ARMFLASH", nil),
			"armpw":     unit("ARMPW", nil),
			"armshield": unit("ARMSHIELD", func(u *content.UnitDef) { u.NoRestrict = true }),
			"corak":     unit("CORAK", nil),
		},
		Sides: []*content.SideDef{{Index: 0, Commander: "armcom"}},
	}
	cs := &contentSet{mod: mod}
	cs.preview.catalogOnce.Do(func() { cs.preview.catalog = cat })
	return cs
}

func restrictionSet(t *testing.T, values map[string]int) content.Restrictions {
	t.Helper()
	r, err := content.ParseRestrictions(values)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The settings key `restrictions` in the window (docs/DESIGN_MODS_MUTATORS.md
// §15.3, §15.9, proposals R-P4 and R-P5):
//
//   - the saved set, resolved against the running content, is the set the
//     window's battles carry; an entry that does not read, names a unit the
//     content lacks or a norestrict unit, or would remove a side's commander
//     is left out with a notice naming it, and stays in the file;
//   - a mod with no set of its own plays the base set; a mod's layer that
//     states a set replaces the base set whole; a set applied while a mod
//     runs goes to that mod's layer, never into the original game's block,
//     the empty base set included;
//   - the chip counts the restrictions the battles carry;
//   - --restrict replaces the saved set for the run until the player
//     applies one, and capture modes never read the key.
func TestSavedRestrictionsResolveAndLayerPerMod(t *testing.T) {
	file := settings.Defaults()
	file.Restrictions = map[string]int{"armpw": 0, "armflash": 3, "notaunit": 0, "ArmPW": 1, "armcom": 0, "armshield": 2, "corak": 101}

	plain := &gameShell{cs: restrictionTestContent(nil)}
	plain.applySettings(file)
	if got := plain.opts.Restrictions.String(); got != "armflash=3,armpw=0" {
		t.Fatalf("the original game's battles carry %q, want armflash=3,armpw=0", got)
	}
	wantLeft := []restrictionOmission{
		{"ArmPW", 1, "unreadable"},
		{"armcom", 0, "a commander cannot be removed"},
		{"armshield", 2, "cannot be restricted"},
		{"corak", 101, "unreadable"},
		{"notaunit", 0, "not in this content"},
	}
	if len(plain.restrictions.omitted) != len(wantLeft) {
		t.Fatalf("left out %+v, want %+v", plain.restrictions.omitted, wantLeft)
	}
	for i, o := range plain.restrictions.omitted {
		if o != wantLeft[i] {
			t.Fatalf("left out %+v, want %+v", plain.restrictions.omitted, wantLeft)
		}
	}
	notice := plain.restrictionNotice()
	for _, o := range wantLeft {
		if !strings.Contains(notice, o.unit) {
			t.Fatalf("notice %q does not name %s", notice, o.unit)
		}
	}
	if kept := plain.captureSettings(); !maps.Equal(kept.Restrictions, file.Restrictions) {
		t.Fatalf("the file keeps %v, want every saved entry %v", kept.Restrictions, file.Restrictions)
	}
	if line := plain.modStatusLine(); !strings.HasSuffix(line, " - 2 restrictions") {
		t.Fatalf("chip %q does not count the battle's two restrictions", line)
	}
	if draft := plain.restrictions.readable; draft.String() != "armcom=0,armflash=3,armpw=0,armshield=2,notaunit=0" {
		t.Fatalf("editor draft %q, want every readable saved entry", draft)
	}

	mod := &modlibrary.Mod{Metadata: modlibrary.Metadata{ID: "somemod", Name: "Some mod", Version: "1"}}
	inherits := &gameShell{cs: restrictionTestContent(mod)}
	inherits.applySettings(file)
	if got := inherits.opts.Restrictions.String(); got != "armflash=3,armpw=0" {
		t.Fatalf("a mod without its own set plays %q, want the base set", got)
	}
	own := file
	own.ModSettings = map[string]json.RawMessage{mod.ID: json.RawMessage(`{"restrictions":{"corak":4}}`)}
	replaced := &gameShell{cs: restrictionTestContent(mod)}
	replaced.applySettings(own)
	if got := replaced.opts.Restrictions.String(); got != "corak=4" || len(replaced.restrictions.omitted) != 0 {
		t.Fatalf("a mod's own set plays %q leaving out %+v, want corak=4 alone", got, replaced.restrictions.omitted)
	}

	// Applying a set while the mod runs writes the mod's layer; the base
	// block keeps the original game's, whether it has one or not.
	for _, base := range []map[string]int{file.Restrictions, nil} {
		start := settings.Defaults()
		start.Restrictions = base
		g := &gameShell{cs: restrictionTestContent(mod)}
		g.applySettings(start)
		g.selectRestrictions(restrictionSet(t, map[string]int{"armpw": 2}))
		if got := g.opts.Restrictions.String(); got != "armpw=2" {
			t.Fatalf("after applying, the mod's battles carry %q", got)
		}
		saved := g.captureSettings()
		if !maps.Equal(saved.Restrictions, base) {
			t.Fatalf("the base block took %v, want the original game's %v", saved.Restrictions, base)
		}
		again := &gameShell{cs: restrictionTestContent(mod)}
		again.applySettings(saved)
		if got := again.opts.Restrictions.String(); got != "armpw=2" {
			t.Fatalf("the mod reloads %q from %s, want armpw=2", got, saved.ModSettings[mod.ID])
		}
		original := &gameShell{cs: restrictionTestContent(nil)}
		original.applySettings(saved)
		if !maps.Equal(original.restrictions.saved, base) {
			t.Fatalf("the original game plays %v, want its own %v", original.restrictions.saved, base)
		}
	}

	// --restrict chose the run's set; the saved one is not in force.
	flagged := &gameShell{opts: Options{Restrictions: restrictionSet(t, map[string]int{"corak": 1}), RestrictionsSet: true}, cs: restrictionTestContent(nil)}
	flagged.applySettings(file)
	if got := flagged.opts.Restrictions.String(); got != "corak=1" || flagged.restrictionNotice() != "" {
		t.Fatalf("with --restrict the battles carry %q with notice %q, want corak=1 and none", got, flagged.restrictionNotice())
	}
	if kept := flagged.captureSettings(); !maps.Equal(kept.Restrictions, file.Restrictions) {
		t.Fatalf("--restrict overwrote the saved set: %v", kept.Restrictions)
	}
	flagged.selectRestrictions(restrictionSet(t, map[string]int{"armpw": 0}))
	if got := flagged.opts.Restrictions.String(); got != "armpw=0" || flagged.opts.RestrictionsSet {
		t.Fatalf("an applied set after --restrict gives %q (flag %v), want the applied set", got, flagged.opts.RestrictionsSet)
	}

	// A capture never reads the key, so it never compiles a catalog for it:
	// this content has none to compile.
	shot := &gameShell{opts: Options{Shot: "shot.png"}, cs: &contentSet{}}
	shot.applySettings(file)
	if !shot.opts.Restrictions.IsZero() || shot.restrictions.omitted != nil {
		t.Fatalf("a capture took %q, leaving out %+v", shot.opts.Restrictions, shot.restrictions.omitted)
	}
	// Content whose catalog cannot be compiled leaves every entry out and
	// still starts.
	broken := &gameShell{cs: &contentSet{}}
	broken.applySettings(file)
	if !broken.opts.Restrictions.IsZero() || len(broken.restrictions.omitted) != len(file.Restrictions) {
		t.Fatalf("uncompilable content took %q, leaving out %+v", broken.opts.Restrictions, broken.restrictions.omitted)
	}

	// The direct battle view resolves the running mod's layer before any
	// shell exists, and keeps a --restrict set or a mission's none.
	if got := directViewRestrictions(Options{Map: "m"}, restrictionTestContent(mod), own); got.String() != "corak=4" {
		t.Fatalf("direct view takes %q, want the mod's corak=4", got)
	}
	if got := directViewRestrictions(Options{Mission: "camps/x:MISSION0"}, restrictionTestContent(nil), file); !got.IsZero() {
		t.Fatalf("a direct mission takes %q", got)
	}
}
