//go:build retail

package session

import (
	"errors"
	"maps"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/save"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// TestRestrictionsRestoreRetail saves a restricted, mutated skirmish of the
// reference install and restores it, under Strict 3.1 and Modern
// (docs/DESIGN_MODS_MUTATORS.md §15.5, §15.11):
//
//   - the session's sidecar half records the set, and a restore given it
//     rebuilds the fresh entry's catalog — the same identity,
//     mutators(restrictions(compiled)), and the same definition index space,
//     record for record — so a unit of a capped definition comes back on the
//     capped record with its index, and the restored session records the set
//     for the next save;
//   - a recorded entry the catalog cannot take refuses the restore with the
//     content refusal naming it, and writes nothing;
//   - a campaign bank paired with a set is refused, since a mission keeps
//     its own unit list (§15.1).
//
// The shared compiled catalog is never written. Skipped without retail
// assets.
func TestRestrictionsRestoreRetail(t *testing.T) {
	f := loadRetailFixture(t)
	for _, key := range []string{"armpw", "armflash"} {
		if _, ok := f.cat.Unit(key); !ok {
			t.Skipf("retail fixture unit %q is absent", key)
		}
	}
	r, err := content.ParseRestrictions(map[string]int{"armflash": 2, "armpw": 0})
	if err != nil {
		t.Fatal(err)
	}
	m := content.Mutators{Health: content.Factor{Num: 2, Den: 1}}
	baseHash := f.cat.Hash
	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := f.cfg
			cfg.Gameplay = mode
			src, err := NewSkirmishWithEntryOptions(f.fs, f.cat, cfg, SkirmishEntryOptions{Restrictions: r, Mutators: m})
			if err != nil {
				t.Fatal(err)
			}
			driver := int32(1 << 20)
			for i := 0; i < 60 && src.State != StateBattle; i++ {
				src.Step(driver)
				driver++
			}
			flash := placeCompleteRetailUnit(t, src, "armflash", src.LocalOwner, numeric.FixedFromInt(900), numeric.FixedFromInt(900))
			sc := SaveSidecar(src)
			if !maps.Equal(sc.Restrictions, r.Map()) || sc.Catalog != src.Catalog.Hash {
				t.Fatalf("sidecar records %v over catalog %s, want %v over %s", sc.Restrictions, sc.Catalog, r.Map(), src.Catalog.Hash)
			}
			bank := retailBankOf(t, src)
			deps := RetailLoadDeps{FS: f.fs, Catalog: f.cat, SimSeed: 7, CRTSeed: 7, UnitLimit: src.Skirmish.UnitLimit, Gameplay: mode, Mutators: m, Restrictions: r}
			staged, err := StageRetailBattle(bank, deps)
			if err != nil {
				t.Fatal(err)
			}
			if err := RestoreRetailBattleCore(staged); err != nil {
				t.Fatal(err)
			}
			dst := staged.Session
			if !dst.Restrictions.Equal(r) || dst.Mutators != m || dst.Catalog.Hash != src.Catalog.Hash {
				t.Fatalf("restore bound %q / %q over %s, want the fresh entry's %q / %q over %s",
					dst.Restrictions, dst.Mutators, dst.Catalog.Hash, r, m, src.Catalog.Hash)
			}
			want, got := src.Catalog.UnitRecords(), dst.Catalog.UnitRecords()
			if len(got) != len(want) {
				t.Fatalf("restored %d definitions, want the restricted table's %d", len(got), len(want))
			}
			for i := range want {
				if got[i].UnitName != want[i].UnitName || got[i].UnitDefID != want[i].UnitDefID || got[i].Limit != want[i].Limit || got[i].Hash != want[i].Hash {
					t.Fatalf("definition %d restored as %s #%d limit %d, want %s #%d limit %d",
						i, got[i].UnitName, got[i].UnitDefID, got[i].Limit, want[i].UnitName, want[i].UnitDefID, want[i].Limit)
				}
			}
			if _, ok := dst.Catalog.Unit("armpw"); ok {
				t.Fatal("the restore kept the removed definition")
			}
			restored := dst.Units.Unit(flash.Handle)
			if restored == nil || restored.Def == nil || restored.Def.UnitDefID != flash.Def.UnitDefID || restored.Def.Limit != 2 || !restored.Def.LimitEnabled {
				t.Fatalf("the capped unit restored as %+v, want definition #%d with its cap", restored, flash.Def.UnitDefID)
			}
			if again := SaveSidecar(dst); !maps.Equal(again.Restrictions, r.Map()) {
				t.Fatalf("a restored battle's sidecar records %v, want %v", again.Restrictions, r.Map())
			}

			for _, bad := range []map[string]int{{"armflash": 2, "notaunit": 0}, {"armcom": 1}} {
				deps.Restrictions, err = content.ParseRestrictions(bad)
				if err != nil {
					t.Fatal(err)
				}
				_, err = StageRetailBattle(bank, deps)
				var refusal *content.RestrictionsError
				if !errors.As(err, &refusal) || len(refusal.Issues) != 1 {
					t.Fatalf("restore with %v = %v, want the content refusal naming one entry", bad, err)
				}
			}
		})
	}

	mission, err := NewMissionWithEntryOptions(f.fs, f.cat, mutatorCampaignMission, 0, 7, 7, MissionEntryOptions{Gameplay: gameplay.Strict31}, nil)
	if err != nil {
		t.Skipf("stock campaign %q is unavailable: %v", mutatorCampaignMission, err)
	}
	if sc := SaveSidecar(mission); sc.Restrictions != nil {
		t.Fatalf("a mission's sidecar records restrictions %v", sc.Restrictions)
	}
	if _, err := StageRetailBattle(retailBankOf(t, mission), RetailLoadDeps{FS: f.fs, Catalog: f.cat, SimSeed: 7, CRTSeed: 7, Gameplay: gameplay.Strict31, Restrictions: r}); err == nil {
		t.Fatal("a campaign bank restored with unit restrictions")
	}
	if f.cat.Hash != baseHash {
		t.Fatal("a restore wrote the shared compiled catalog")
	}
	if _, ok := f.cat.Unit("armpw"); !ok {
		t.Fatal("a restore removed a definition from the shared compiled catalog")
	}
}

// retailBankOf is s written as a retail battle save and read back.
func retailBankOf(t *testing.T, s *Session) *save.Bank {
	t.Helper()
	inputs, err := s.RetailBattleSaveInputs(RetailBattleSummary(s, "restrictions", "0", s.Skirmish.UnitLimit), save.Camera{})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.RetailProjection(inputs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := projection.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	bank, err := save.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return bank
}
