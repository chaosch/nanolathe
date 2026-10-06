package session

import (
	"errors"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/clock"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/units"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

func sessionRestrictions(t *testing.T, values map[string]int) content.Restrictions {
	t.Helper()
	r, err := content.ParseRestrictions(values)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestApplyEntryRestrictionsClonesOnlyWhenNeeded: with no restrictions battle
// entry runs on the very catalog it was handed, so no identity or fingerprint
// can move; with restrictions it runs on a restricted clone, the handed-in
// catalog is never written, and an entry the catalog cannot take refuses
// entry with the content refusal attached (docs/DESIGN_MODS_MUTATORS.md
// §15.3, §15.5).
func TestApplyEntryRestrictionsClonesOnlyWhenNeeded(t *testing.T) {
	cat := strictHelpBuildCatalog(30, 64)
	cat.Hash = "base"
	if got, err := applyEntryRestrictions(cat, content.Restrictions{}); err != nil || got != cat {
		t.Fatalf("an empty set returned %p (%v), want the same catalog %p", got, err, cat)
	}
	r := sessionRestrictions(t, map[string]int{"mutatorcon": 0, "mutatorprod": 2})
	got, err := applyEntryRestrictions(cat, r)
	if err != nil {
		t.Fatal(err)
	}
	if got == cat || got.Hash == cat.Hash {
		t.Fatal("a restricted battle must run on a clone with its own identity")
	}
	if _, ok := got.Unit("mutatorcon"); ok {
		t.Fatal("a removed definition is still in the battle catalog")
	}
	if prod, _ := got.Unit("mutatorprod"); prod.Limit != 2 || !prod.LimitEnabled {
		t.Fatalf("capped product limit %d enabled %v, want 2", prod.Limit, prod.LimitEnabled)
	}
	if prod := cat.Units["mutatorprod"]; prod.Limit != -1 || cat.Units["mutatorcon"] == nil || cat.Hash != "base" {
		t.Fatal("the shared catalog was written")
	}
	_, err = applyEntryRestrictions(cat, sessionRestrictions(t, map[string]int{"armcom": 0, "mutatorprod": 1}))
	var refusal *content.RestrictionsError
	if !errors.As(err, &refusal) || len(refusal.Issues) != 1 || refusal.Issues[0].Reason != content.RestrictionRemovesCommander {
		t.Fatalf("removing a side's commander = %v, want the commander refusal", err)
	}
}

// newRestrictionScene binds a session in the given mode over cat on the
// minimal terrain, with every player present: just enough of a battle for
// the allocator's per-definition test.
func newRestrictionScene(t *testing.T, cat *content.Catalog, mode gameplay.Mode) *Session {
	t.Helper()
	s := &Session{Gameplay: mode, Catalog: cat, World: minimalTerrain(), Mission: syntheticMission(), LocalOwner: 0}
	w, err := newSlicedWorld(cat)
	if err != nil {
		t.Fatal(err)
	}
	s.Units = w
	s.Econ = economyForTest()
	for i := range s.Econ.Players {
		p := &s.Econ.Players[i]
		p.Exists = true
		p.ControllerState = 1
		p.EndGameCountdown = -1
	}
	s.Econ.SeedDeadlines(0)
	s.Clock = &clock.State{}
	if err := createAndBindServicesForTest(t, s); err != nil {
		t.Fatal(err)
	}
	if want := RuleSetForMode(mode).Name; s.Rules.Name != want {
		t.Fatalf("scene bound %q, want %q", s.Rules.Name, want)
	}
	return s
}

// TestRestrictionCapCountsEachPlayersOwnRecords is the allocator relationship
// a cap of N relies on, under Strict 3.1 and Modern alike: the (N+1)th record
// of a player's slice is refused with nanoframes counted, a dead unit counts
// until its teardown clears the record, the teardown makes room again, and
// another player's records never count [05 R-SHARE-01 §8]. The cap reaches the
// allocator only through the restricted clone's limit field (§15.4 step 2).
func TestRestrictionCapCountsEachPlayersOwnRecords(t *testing.T) {
	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			base := strictHelpBuildCatalog(30, 64)
			if _, err := content.CompileCategories(base.Units); err != nil {
				t.Fatal(err)
			}
			cat, err := applyEntryRestrictions(base, sessionRestrictions(t, map[string]int{"mutatorprod": 2}))
			if err != nil {
				t.Fatal(err)
			}
			s := newRestrictionScene(t, cat, mode)
			prod := cat.Units["mutatorprod"]
			at := world.CellToWorld(8)
			create := func(owner uint8, nanoframe bool) (pool.Handle, error) {
				if nanoframe {
					return s.Units.CreateNanoframe(prod, owner, at, 0, at)
				}
				return s.Units.Create(prod, owner, at, 0, at)
			}
			frame, err := create(0, true)
			if err != nil {
				t.Fatalf("first record (a nanoframe): %v", err)
			}
			done, err := create(0, false)
			if err != nil {
				t.Fatalf("second record: %v", err)
			}
			if _, err := create(0, false); err == nil {
				t.Fatal("the third record of a cap of 2 was created; the nanoframe must count")
			}
			for i := 0; i < 2; i++ {
				if _, err := create(1, false); err != nil {
					t.Fatalf("another player's record %d was refused: the cap counts each player's own slice: %v", i+1, err)
				}
			}
			if _, err := create(1, true); err == nil {
				t.Fatal("the second player's own cap did not bind")
			}
			// A dead unit keeps its record until teardown clears it.
			s.Units.Destroy(done, units.DeathKilled)
			if _, err := create(0, true); err == nil {
				t.Fatal("a dead unit whose teardown has not run stopped counting")
			}
			s.Units.FreeImmediate(done)
			if _, err := create(0, true); err != nil {
				t.Fatalf("teardown did not make room under the cap: %v", err)
			}
			if s.Units.Unit(frame) == nil {
				t.Fatal("the first nanoframe was lost")
			}
		})
	}
}
