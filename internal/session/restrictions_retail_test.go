//go:build retail

package session

import (
	"slices"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/units"
)

// TestRestrictionsAtFreshSkirmishEntryRetail enters the reference skirmish
// with a removal, a cap and mutators, under Strict 3.1 and Modern
// (docs/DESIGN_MODS_MUTATORS.md §15.5, §15.11):
//
//   - an empty set runs on the handed-in catalog; a set is recorded by the
//     session and by the frozen inputs, and the restricted-and-mutated
//     identity is mutators(restrictions(compiled));
//   - the removed product cannot be ordered from a factory or spawned by
//     name;
//   - the capped product is refused at the factory's allocation with the
//     retail caption and a 300-tick wait while the player's own record
//     stands, another player's record never counts, the retry after the
//     teardown allocates, and the factory's nanoframe counts against a
//     further creation [05 R-SHARE-01 §8].
//
// The shared compiled catalog is never written. Skipped without retail assets.
func TestRestrictionsAtFreshSkirmishEntryRetail(t *testing.T) {
	f := loadRetailFixture(t)
	for _, key := range []string{"armpw", "armflash", "armvp", "armlab"} {
		if _, ok := f.cat.Unit(key); !ok {
			t.Skipf("retail fixture unit %q is absent", key)
		}
	}
	r, err := content.ParseRestrictions(map[string]int{"armflash": 1, "armpw": 0})
	if err != nil {
		t.Fatal(err)
	}
	m := content.Mutators{Health: content.Factor{Num: 2, Den: 1}}
	baseHash := f.cat.Hash
	restricted := content.HashDefinition([]byte("catalog+restrictions\n" + baseHash + "\n" + r.Digest() + "\n"))
	wantHash := content.HashDefinition([]byte("catalog+mutators\n" + restricted + "\n" + m.Digest() + "\n"))

	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := f.cfg
			cfg.Gameplay = mode
			plain, err := NewSkirmishWithEntryOptions(f.fs, f.cat, cfg, SkirmishEntryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if plain.Catalog != f.cat || !plain.Restrictions.IsZero() {
				t.Fatal("a skirmish without restrictions must run on the handed-in catalog")
			}
			options := SkirmishEntryOptions{Restrictions: r, Mutators: m}
			entry, err := prepareSkirmishEntry(f.fs, f.cat, cfg, options)
			if err != nil {
				t.Fatal(err)
			}
			if !entry.inputs.Restrictions().Equal(r) || entry.inputs.Mutators() != m {
				t.Fatalf("frozen selection = %q / %q", entry.inputs.Restrictions(), entry.inputs.Mutators())
			}
			s, err := NewSkirmishWithEntryOptions(f.fs, f.cat, cfg, options)
			if err != nil {
				t.Fatal(err)
			}
			if !s.Restrictions.Equal(r) || s.Mutators != m {
				t.Fatalf("session recorded %q / %q", s.Restrictions, s.Mutators)
			}
			if s.Catalog.Hash != wantHash || entry.inputs.Catalog().Hash != wantHash {
				t.Fatal("the restricted-and-mutated identity is not mutators(restrictions(compiled))")
			}
			if _, ok := s.Catalog.Unit("armpw"); ok {
				t.Fatal("the removed product is still in the battle catalog")
			}
			flash, _ := s.Catalog.Unit("armflash")
			if flash.Limit != 1 || !flash.LimitEnabled {
				t.Fatalf("armflash limit %d enabled %v, want 1", flash.Limit, flash.LimitEnabled)
			}

			driver := int32(1 << 20)
			step := func() { s.Step(driver); driver++ }
			for i := 0; i < 60 && s.State != StateBattle; i++ {
				step()
			}
			owner, other := s.LocalOwner, s.EnemyOwner
			at := func(x int64) numeric.Fixed { return numeric.FixedFromInt(x) }

			// A removed product: a factory order for it is refused and the
			// developer spawn finds nothing by its name.
			lab := placeCompleteRetailUnit(t, s, "armlab", owner, at(700), at(1000))
			if err := s.EnqueueHumanCommand(HumanCommand{Kind: HumanFactoryBuild, FactoryBuild: HumanFactoryBuildCommand{Builder: lab.Handle, Product: "armpw", Count: 1}}); err != nil {
				t.Fatal(err)
			}
			before := len(s.Units.Iter())
			s.applyHumanCommand(HumanCommand{Kind: HumanDeveloperSpawn, DeveloperSpawn: HumanDeveloperSpawnCommand{Pattern: "armpw", Owner: owner, X: at(900), Y: 0, Z: at(1300)}}, s.Clock.GlobalTick)
			step()
			if q := orders.QueueForUnit(lab); q.LenPrimary() != 0 {
				t.Fatal("a factory accepted an order for a removed product")
			}
			for _, u := range s.Units.Iter()[before:] {
				if u != nil && u.Def != nil && content.CanonicalKey(u.Def.UnitName) == "armpw" {
					t.Fatal("the developer spawn created a removed definition")
				}
			}

			// The cap: another player's record never counts, the owner's own
			// record does.
			placeCompleteRetailUnit(t, s, "armflash", other, at(2000), at(2000))
			own := placeCompleteRetailUnit(t, s, "armflash", owner, at(900), at(900))
			if _, err := s.Units.Create(flash, owner, at(950), 0, at(950)); err == nil {
				t.Fatal("a second armflash under a cap of 1 was created")
			}
			factory := placeCompleteRetailUnit(t, s, "armvp", owner, at(1000), at(1000))
			if err := s.EnqueueHumanCommand(HumanCommand{Kind: HumanFactoryBuild, FactoryBuild: HumanFactoryBuildCommand{Builder: factory.Handle, Product: "armflash", Count: 1}}); err != nil {
				t.Fatal(err)
			}
			q := orders.QueueForUnit(factory)
			refusals := func() int {
				n := 0
				for _, msg := range s.Build.Messages() {
					if msg == construction.ErrLimitMessage {
						n++
					}
				}
				return n
			}
			var building *orders.Node
			refusedAt := uint32(0)
			for i := 0; i < 600 && refusedAt == 0; i++ {
				seen := refusals()
				step()
				if n := q.Head(); n != nil && orders.DescriptorFor(n.ID).Name == "BuildingBuild" && refusals() > seen {
					building, refusedAt = n, s.Clock.GlobalTick
				}
			}
			if building == nil {
				t.Fatal("the factory's allocation was never refused under the cap")
			}
			if building.Target != 0 || int64(building.Deadline)-int64(refusedAt) != 300 {
				t.Fatalf("refused allocation: target %d, deadline %d at tick %d; want no product and a 300-tick wait", building.Target, building.Deadline, refusedAt)
			}
			// Nothing is retried before the wait ends.
			count := refusals()
			for s.Clock.GlobalTick+1 < uint32(building.Deadline) {
				step()
			}
			if refusals() != count || building.Target != 0 {
				t.Fatal("the factory retried before its 300-tick wait ended")
			}
			// Tear the owner's record down; the factory's next attempt
			// allocates, and its nanoframe counts against another creation.
			s.Units.Destroy(own.Handle, units.DeathKilled)
			for i := 0; i < 900 && building.Target == 0; i++ {
				step()
			}
			if building.Target == 0 {
				t.Fatal("the factory did not allocate once the owner's record was torn down")
			}
			product := s.Units.Unit(building.Target)
			if product == nil || product.Remaining == 0 || !slices.Contains([]string{"armflash", "ARMFLASH"}, product.Def.UnitName) {
				t.Fatalf("the factory's product is %+v, want an armflash nanoframe", product)
			}
			if _, err := s.Units.Create(flash, owner, at(950), 0, at(950)); err == nil {
				t.Fatal("the factory's nanoframe did not count against the cap")
			}
		})
	}
	if f.cat.Hash != baseHash {
		t.Fatal("an entry wrote the shared compiled catalog")
	}
	if u, ok := f.cat.Unit("armflash"); !ok || u.Limit != -1 {
		t.Fatal("an entry capped the shared compiled catalog")
	}
	if _, ok := f.cat.Unit("armpw"); !ok {
		t.Fatal("an entry removed a definition from the shared compiled catalog")
	}
}
