package headless

import (
	"errors"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/testsupport/retailcat"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// TestParseRestrictFlags locks the flag rules both commands share
// (docs/DESIGN_MODS_MUTATORS.md §15.5): repeatable unit=count entries, none
// alone for an explicitly empty set, and a refusal for a malformed entry, a
// unit given twice and none beside an entry.
func TestParseRestrictFlags(t *testing.T) {
	r, err := ParseRestrictFlags([]string{"armpw=20", "armkrog=0"})
	if err != nil || r.String() != "armkrog=0,armpw=20" {
		t.Fatalf("entries = %q, %v", r.String(), err)
	}
	for _, args := range [][]string{nil, {"none"}} {
		if r, err := ParseRestrictFlags(args); err != nil || !r.IsZero() {
			t.Fatalf("%q = %q, %v; want the empty set", args, r.String(), err)
		}
	}
	for _, args := range [][]string{
		{"armpw=20", "armpw=3"}, {"none", "armpw=3"}, {"armpw=3", "none"}, {"none", "none"},
		{"armpw"}, {"ARMPW=1"}, {"armpw=101"}, {"armpw=-1"}, {"armpw=07"},
	} {
		_, err := ParseRestrictFlags(args)
		if err == nil || !strings.Contains(err.Error(), "logical path <command line>, providers searched [restrict]") {
			t.Errorf("%q accepted or refused without the diagnostic: %v", args, err)
		}
	}
}

// TestReportCarriesTheBoundRestrictions: the report prints the canonical set
// the session bound, and an empty field for an unrestricted battle
// (docs/DESIGN_MODS_MUTATORS.md §15.5).
func TestReportCarriesTheBoundRestrictions(t *testing.T) {
	request := Request{Map: "synthetic", SimulationSeed: 17, CRTSeed: 19, TickLimit: 1}
	r, err := content.ParseRestrictions(map[string]int{"armpw": 20, "armkrog": 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		r    content.Restrictions
		want string
	}{{content.Restrictions{}, ""}, {r, "armkrog=0,armpw=20"}} {
		sess := syntheticSession(request)
		sess.Restrictions = tc.r
		report, err := RunSession(request, sess)
		if !errors.Is(err, ErrTickLimit) {
			t.Fatalf("RunSession error = %v, want tick limit", err)
		}
		if report.Restrictions != tc.want {
			t.Fatalf("report restrictions = %q, want %q", report.Restrictions, tc.want)
		}
	}
}

// TestCampaignRequestRefusesRestrictions: a mission keeps its own authored
// unit list, so a request that names restrictions for one is refused rather
// than run without them (§15.1 "Not campaign missions").
func TestCampaignRequestRefusesRestrictions(t *testing.T) {
	r, err := content.ParseRestrictions(map[string]int{"armpw": 0})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ComposeFreshBattle(FreshBattleRequest{Kind: ScenarioCampaign, Mission: "camps/x.tdf:MISSION0", FS: vfs.New(), Restrictions: r})
	if err == nil || !strings.Contains(err.Error(), "unit restrictions do not apply to a campaign mission") {
		t.Fatalf("campaign with restrictions = %v", err)
	}
}

// TestDisplaylessRequestForwardsRestrictions: a request's restrictions reach
// the session entry, in Strict 3.1 as in every mode, and move the reported
// catalog identity; a request without them runs on the unchanged catalog.
// An entry the content cannot take stops the run with the --restrict
// diagnostic naming it (§15.3). Skipped without retail assets.
func TestDisplaylessRequestForwardsRestrictions(t *testing.T) {
	cat, fs := retailcat.Shared(t)
	r, err := content.ParseRestrictions(map[string]int{"armflash": 2, "armpw": 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		r          content.Restrictions
		restricted bool
	}{{content.Restrictions{}, false}, {r, true}} {
		request := Request{Map: "ashap plateau", Gameplay: gameplay.Strict31, SimulationSeed: 7, CRTSeed: 7, TickLimit: 1, Restrictions: tc.r}
		report, err := RunWithContent(request, fs, cat)
		if !errors.Is(err, ErrTickLimit) {
			t.Fatalf("RunWithContent error = %v, want tick limit", err)
		}
		if report.Restrictions != tc.r.String() || (report.CatalogHash != cat.Hash) != tc.restricted {
			t.Fatalf("restrictions %q catalog %q (base %q), want %q restricted=%v", report.Restrictions, report.CatalogHash, cat.Hash, tc.r.String(), tc.restricted)
		}
	}
	bad, err := content.ParseRestrictions(map[string]int{"armfoo": 0, "armpw": 1})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Map: "ashap plateau", Gameplay: gameplay.Strict31, SimulationSeed: 7, CRTSeed: 7, TickLimit: 1, Restrictions: bad}
	_, err = RunWithContent(request, fs, cat)
	want := "nanolathe: unit restriction armfoo=0: logical path --restrict, providers searched [unit catalog], expected a unit the running content defines that is not marked norestrict"
	if err == nil || err.Error() != want {
		t.Fatalf("an unknown unit = %v, want %q", err, want)
	}
}
