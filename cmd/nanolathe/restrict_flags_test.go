package main

import (
	"io"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// TestRestrictFlag locks the desktop command's --restrict flag
// (docs/DESIGN_MODS_MUTATORS.md §15.5): repeatable unit=count entries, none
// for an explicitly empty set, both recorded as given so they replace the
// saved set for the run, and refusals for a malformed entry, a unit given
// twice, none beside an entry, a campaign mission and a loaded save.
func TestRestrictFlag(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		set  bool
	}{
		{nil, "", false},
		{[]string{"--restrict", "armpw=20", "--restrict=armkrog=0"}, "armkrog=0,armpw=20", true},
		{[]string{"--restrict", "none"}, "", true},
		{[]string{"--map", "ashap plateau", "--survival", "--restrict", "armflash=1"}, "armflash=1", true},
	} {
		opts, err := parseFlags(tc.args, io.Discard)
		if err != nil {
			t.Fatalf("parseFlags(%v): %v", tc.args, err)
		}
		if opts.Restrictions.String() != tc.want || opts.RestrictionsSet != tc.set {
			t.Fatalf("parseFlags(%v) = %q set=%v, want %q set=%v", tc.args, opts.Restrictions.String(), opts.RestrictionsSet, tc.want, tc.set)
		}
	}
	for _, args := range [][]string{
		{"--restrict", "ArmPW=2"},
		{"--restrict", "armpw=1.5"},
		{"--restrict", "armpw=2", "--restrict", "armpw=3"},
		{"--restrict", "armpw=2", "--restrict", "none"},
		{"--restrict", "armpw=2", "--mission", "camps/Arm Campaign.tdf:MISSION0"},
		{"--restrict", "none", "--load-save", "/tmp/unused.sav"},
	} {
		if _, err := parseFlags(args, io.Discard); err == nil {
			t.Fatalf("parseFlags(%v) accepted", args)
		}
	}

	// Skirmish and Survival requests carry the set; a mission's does not.
	opts, err := parseFlags([]string{"--restrict", "armpw=0"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cs := testContentSet(vfs.New())
	skirmish, err := skirmishBattleRequest(opts, cs, session.SkirmishConfig{MapName: "test"}, headlessScenarioSkirmish, nil, newBattleSeedSource(opts))
	if err != nil {
		t.Fatal(err)
	}
	mission, err := missionBattleRequest(opts, cs, "campaign:MISSION0", 0, 0, 0, nil, newBattleSeedSource(opts))
	if err != nil {
		t.Fatal(err)
	}
	if !skirmish.value.Restrictions.Equal(opts.Restrictions) || !mission.value.Restrictions.IsZero() {
		t.Fatalf("skirmish request %q, mission request %q", skirmish.value.Restrictions.String(), mission.value.Restrictions.String())
	}
}
