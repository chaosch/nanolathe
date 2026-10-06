package main

import (
	"io"
	"strings"
	"testing"
)

// TestParseRestrictions locks the displayless command's --restrict flag
// (docs/DESIGN_MODS_MUTATORS.md §15.5): repeatable unit=count entries, none
// for an explicitly empty set, and the standard diagnostic for a malformed
// entry, a unit given twice, none beside an entry, a campaign mission and the
// fixed simulation-cost benchmark scene. Names are checked at battle entry.
func TestParseRestrictions(t *testing.T) {
	isolateHostFiles(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"--restrict", "armpw=20"}, "armpw=20"},
		{[]string{"--restrict", "armpw=20", "--restrict=armkrog=0"}, "armkrog=0,armpw=20"},
		{[]string{"--restrict", "none"}, ""},
		{[]string{"--map", "ashap plateau", "--survival", "--restrict", "armflash=1"}, "armflash=1"},
	} {
		req, _, _, _, err := parse(tc.args, io.Discard)
		if err != nil {
			t.Fatalf("parse(%v): %v", tc.args, err)
		}
		if got := req.Restrictions.String(); got != tc.want {
			t.Fatalf("parse(%v) restrictions = %q, want %q", tc.args, got, tc.want)
		}
	}
	for _, args := range [][]string{
		{"--restrict", "ARMPW=1"},
		{"--restrict", "armpw=101"},
		{"--restrict", "armpw"},
		{"--restrict", "armpw=2", "--restrict", "armpw=3"},
		{"--restrict", "none", "--restrict", "armpw=3"},
		{"--restrict", "armpw=2", "--mission", "camps/Arm Campaign.tdf:MISSION0"},
		{"--restrict", "none", "--mission", "camps/Arm Campaign.tdf:MISSION0"},
		{"--restrict", "armpw=2", "--sim-benchmark", "/tmp/unused-benchmark"},
	} {
		if _, _, _, _, err := parse(args, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "nanolathe: ") {
			t.Fatalf("parse(%v) = %v, want a nanolathe diagnostic", args, err)
		}
	}
}
