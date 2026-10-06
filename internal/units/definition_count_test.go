package units

import (
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// TestDefinitionCountIsTheAllocatorsCensus locks the accessor the Modern AI's
// cap observation reads (docs/DESIGN_SESSIONS_AI_SAVE.md "Modern AI
// restriction caps") to the allocator's per-definition census
// [05 R-SHARE-01 §8]: a nanoframe, a completed unit and a dead unit whose
// teardown has not run each count, another player's records and another
// definition's never do, and at every step a limit equal to the count is
// refused while one more is admitted, so the accessor is exactly the number
// the gate compares.
func TestDefinitionCountIsTheAllocatorsCensus(t *testing.T) {
	capped := &content.UnitDef{UnitName: "capped", BMCode: 1, CanMove: true, MaxDamage: 10, LimitEnabled: true, Limit: -1}
	other := &content.UnitDef{UnitName: "other", BMCode: 1, CanMove: true, MaxDamage: 10, LimitEnabled: true, Limit: -1}
	w := newFixtureWorld(8, nil)
	at := numeric.FixedFromInt(64)
	if got := w.DefinitionCount(0, capped); got != 0 {
		t.Fatalf("a definition never created counts %d, want 0", got)
	}
	gateAgrees := func(step string) {
		t.Helper()
		n := w.DefinitionCount(0, capped)
		capped.Limit = int32(n)
		if h, err := w.Create(capped, 0, at, 0, at); err == nil {
			t.Fatalf("%s: a limit equal to the census (%d) admitted handle %d", step, n, h)
		}
		capped.Limit = int32(n + 1)
		h, err := w.Create(capped, 0, at, 0, at)
		if err != nil {
			t.Fatalf("%s: a limit one past the census (%d) refused: %v", step, n, err)
		}
		w.FreeImmediate(h)
		capped.Limit = -1
		if w.DefinitionCount(0, capped) != n {
			t.Fatalf("%s: the probe creation left the census at %d, want %d", step, w.DefinitionCount(0, capped), n)
		}
	}
	gateAgrees("empty")
	frame, err := w.CreateNanoframe(capped, 0, at, 0, at)
	if err != nil {
		t.Fatal(err)
	}
	done, err := w.Create(capped, 0, at, 0, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []*content.UnitDef{capped, other} {
		if _, err := w.Create(d, 1, at, 0, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Create(other, 0, at, 0, at); err != nil {
		t.Fatal(err)
	}
	if got := w.DefinitionCount(0, capped); got != 2 {
		t.Fatalf("census with a nanoframe and a completed unit = %d, want 2", got)
	}
	gateAgrees("nanoframe and completed unit")
	w.Destroy(done, DeathKilled)
	if u := w.RawUnitRecord(done); u == nil || !u.Dying {
		t.Fatal("the destroyed unit is not awaiting teardown")
	}
	if got := w.DefinitionCount(0, capped); got != 2 {
		t.Fatalf("census with a record awaiting teardown = %d, want 2", got)
	}
	gateAgrees("record awaiting teardown")
	w.FreeImmediate(done)
	if got := w.DefinitionCount(0, capped); got != 1 {
		t.Fatalf("census after teardown = %d, want 1 (the nanoframe)", got)
	}
	gateAgrees("after teardown")
	if w.Unit(frame) == nil || w.DefinitionCount(1, capped) != 1 || w.DefinitionCount(0, other) != 1 {
		t.Fatal("the census mixed players or definitions")
	}
	if w.DefinitionCount(-1, capped) != 0 || w.DefinitionCount(10, capped) != 0 || w.DefinitionCount(0, nil) != 0 {
		t.Fatal("an out-of-range player or a nil definition counted records")
	}
}
