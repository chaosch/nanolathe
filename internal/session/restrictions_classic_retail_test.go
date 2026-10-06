//go:build retail

package session

import (
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
)

// TestClassicComputerPlayerMeetsACapRetail: a Classic computer player keeps
// retail's behaviour under unit restrictions, in Strict 3.1 and Modern
// (docs/DESIGN_MODS_MUTATORS.md §15.7, D19). The reference skirmish's CORE
// computer builds extractors first; with cormex capped at 1 the battle is the
// unrestricted battle until its commander's second extractor, whose creation
// the allocator refuses: the build shows the retail caption and holds for
// exactly 300 ticks, no record is made, and the planner — which reads neither
// the restriction set nor the limit field [08 R-AI-01 §12]
// [05 R-SHARE-01 §10] — retries the same build when the wait ends
// [05 R-SHARE-01 §8].
func TestClassicComputerPlayerMeetsACapRetail(t *testing.T) {
	f := loadRetailFixture(t)
	if _, ok := f.cat.Unit("cormex"); !ok {
		t.Skip("retail fixture unit cormex is absent")
	}
	r, err := content.ParseRestrictions(map[string]int{"cormex": 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := f.cfg
			cfg.Gameplay = mode
			plain, err := NewSkirmishWithEntryOptions(f.fs, f.cat, cfg, SkirmishEntryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			capped, err := NewSkirmishWithEntryOptions(f.fs, f.cat, cfg, SkirmishEntryOptions{Restrictions: r})
			if err != nil {
				t.Fatal(err)
			}
			const computer = 1
			if capped.Econ.Players[computer].ControllerState != 2 || capped.ModernAIPlayer(computer) {
				t.Fatal("the fixture's computer player is not Classic")
			}
			mex, _ := capped.Catalog.Unit("cormex")
			driver := int32(1 << 20)
			seen := 0
			refusals := func() int {
				msgs := capped.Build.Messages()
				if seen > len(msgs) {
					seen = 0
				}
				n := 0
				for _, msg := range msgs[seen:] {
					if msg == construction.ErrLimitMessage {
						n++
					}
				}
				seen = len(msgs)
				return n
			}
			// Until the refusal both battles are one battle.
			refusedAt := uint32(0)
			for i := 0; i < 6000 && refusedAt == 0; i++ {
				before, err := plain.PartialStateFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				after, err := capped.PartialStateFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				if before != after {
					t.Fatalf("tick %d: the capped battle left the unrestricted one before any refusal", capped.Clock.GlobalTick)
				}
				plain.Step(driver)
				capped.Step(driver)
				driver++
				if refusals() > 0 {
					refusedAt = capped.Clock.GlobalTick
				}
			}
			if refusedAt == 0 {
				t.Fatal("the computer player's second extractor was never refused")
			}
			var com *orders.Node
			for _, u := range capped.Units.IterSliced() {
				if u.Owner == computer && u.Def != nil && u.Def.Commander {
					if q := orders.QueueOfUnit(u); q != nil {
						com = q.Head()
					}
				}
			}
			if com == nil || orders.DescriptorFor(com.ID).Name != "MobileBuild" || content.CanonicalKey(com.BuildDefKey) != "cormex" || com.Target != 0 {
				t.Fatalf("the refused build is %+v, want the commander's unplaced cormex", com)
			}
			if int64(com.Deadline)-int64(refusedAt) != 300 {
				t.Fatalf("refused at tick %d with deadline %d, want a 300-tick wait", refusedAt, com.Deadline)
			}
			if got := capped.Units.DefinitionCount(computer, mex); got != 1 {
				t.Fatalf("the computer player holds %d cormex records under a cap of 1", got)
			}
			// The same build is retried when the wait ends, and refused again.
			for capped.Clock.GlobalTick < refusedAt+300 {
				if n := refusals(); n != 0 {
					t.Fatalf("tick %d: refused again inside the wait", capped.Clock.GlobalTick)
				}
				capped.Step(driver)
				driver++
			}
			if n := refusals(); n == 0 || capped.Clock.GlobalTick != refusedAt+300 {
				t.Fatalf("tick %d: the build was not retried when its wait ended", capped.Clock.GlobalTick)
			}
			if got := capped.Units.DefinitionCount(computer, mex); got != 1 {
				t.Fatalf("the retry created a record: %d cormex", got)
			}
		})
	}
}
