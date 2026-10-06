package aikit

import (
	"reflect"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/ai"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/sim/rng"
	"github.com/nanolathe-gg/nanolathe/internal/units"
)

// capFixture is a host for player 0 whose lab makes the bot, capped at 2,
// and whose constructor makes the tower, capped at 1
// (docs/DESIGN_SESSIONS_AI_SAVE.md "Modern AI restriction caps"). The
// manager records the build requests the executor makes.
type capFixture struct {
	h          *Host
	w          *units.World
	defs       map[string]*content.UnitDef
	lab, con   pool.Handle
	bot, tower *UnitInfo
	reqs       []ai.BuildRequest
}

func newCapFixture(t *testing.T) *capFixture {
	t.Helper()
	mk := func(name string, f func(*content.UnitDef)) *content.UnitDef {
		d := &content.UnitDef{DefinitionHeader: content.DefinitionHeader{CanonicalKey: name}, UnitName: name, MaxDamage: 100, LimitEnabled: true, Limit: -1}
		f(d)
		return d
	}
	defs := map[string]*content.UnitDef{
		"bot":   mk("bot", func(d *content.UnitDef) { d.CanMove, d.BMCode, d.Limit = true, 1, 2 }),
		"con":   mk("con", func(d *content.UnitDef) { d.CanMove, d.BMCode, d.Builder = true, 1, true }),
		"lab":   mk("lab", func(d *content.UnitDef) { d.Builder = true }),
		"tower": mk("tower", func(d *content.UnitDef) { d.Limit = 1 }),
	}
	cat := &content.Catalog{Units: defs, BuildMenus: map[string]*content.BuildMenuPage{
		"lab": {Buttons: []string{"bot"}},
		"con": {Buttons: []string{"tower"}},
	}}
	f := &capFixture{w: fixtureWorld(cat), defs: defs}
	var err error
	if f.lab, err = f.w.Create(defs["lab"], 0, 100<<16, 0, 100<<16); err != nil {
		t.Fatal(err)
	}
	if f.con, err = f.w.Create(defs["con"], 0, 140<<16, 0, 100<<16); err != nil {
		t.Fatal(err)
	}
	r := rng.NewSimulation(3)
	m := &ai.Manager{Player: 0, Catalog: cat, RNG: &r, OrderBinding: &orders.QueueBinding{SimRNG: &r},
		QueueBuildTyped: func(req ai.BuildRequest) error { f.reqs = append(f.reqs, req); return nil }}
	f.h = NewHost(m, &countBrain{}, PersonaMax)
	h := f.h
	h.kit.Table = BuildTable(cat, nil)
	h.kit.obs = &h.obs
	h.ex = executor{m: m, table: h.kit.Table, obs: &h.obs}
	f.bot, f.tower = h.kit.Table.Lookup("bot"), h.kit.Table.Lookup("tower")
	f.observe(1)
	return f
}

// observe takes a fresh observation, as a think begins.
func (f *capFixture) observe(tick uint32) {
	f.h.b.reset()
	f.h.buildObs(tick, f.w, computerEconomy())
	f.h.kit.out = &f.h.b
}

func (f *capFixture) record(t *testing.T, name string, owner uint8, frame bool) pool.Handle {
	t.Helper()
	create := f.w.Create
	if frame {
		create = f.w.CreateNanoframe
	}
	h, err := create(f.defs[name], owner, numeric.FixedFromInt(300), 0, numeric.FixedFromInt(300))
	if err != nil {
		t.Fatalf("create %s for %d: %v", name, owner, err)
	}
	return h
}

func (f *capFixture) capped(info *UnitInfo) CapCount {
	return f.h.obs.Capped[info.capSlot-1]
}

// TestModernAIRestrictionCaps locks the Modern AI's side of unit
// restrictions: the table carries each cap and lists the capped definitions;
// the observation copies the allocator's census of the owner's own slice —
// nanoframes and records awaiting teardown included, another player's never
// — and the owner's queued requests that have no record yet; the kit's
// allowance subtracts both and what the think has already emitted, and a
// request past it is not emitted, a factory request queuing at most what is
// left; the executor drops a placement or factory request whose cap the live
// census reached in the reaction window, without spending or asking, and
// applies it again once a record is gone.
func TestModernAIRestrictionCaps(t *testing.T) {
	f := newCapFixture(t)
	h, k := f.h, &f.h.kit
	tab := k.Table
	if f.bot.Cap != 2 || f.tower.Cap != 1 || tab.Lookup("lab").Cap != -1 || !reflect.DeepEqual(tab.Capped, []*UnitInfo{f.bot, f.tower}) {
		t.Fatalf("caps bot %d tower %d lab %d, capped %v", f.bot.Cap, f.tower.Cap, tab.Lookup("lab").Cap, tab.Capped)
	}
	if len(h.obs.Capped) != 2 || f.capped(f.bot) != (CapCount{Info: f.bot}) {
		t.Fatalf("observation %+v", h.obs.Capped)
	}
	if k.Allowance(f.bot) != 2 || k.Allowance(f.tower) != 1 || k.Allowance(tab.Lookup("lab")) != Uncapped {
		t.Fatal("the fresh allowances are not the caps")
	}

	// Emission: a factory request queues at most the allowance, and a
	// request past it is not emitted.
	k.Produce(f.lab, f.bot, 3)
	k.Produce(f.lab, f.bot, 1)
	k.Build(f.con, f.tower, 300, 300, -1, 0, false)
	k.Build(f.con, f.tower, 400, 300, -1, 0, false)
	if len(h.b.cmds) != 2 || h.b.cmds[0].Count != 2 || h.b.cmds[1].Kind != CmdBuild {
		t.Fatalf("emitted %+v, want one factory request for 2 and one placement", h.b.cmds)
	}
	if k.Allowance(f.bot) != 0 || k.Allowance(f.tower) != 0 {
		t.Fatal("the think's own requests did not count against the allowance")
	}

	// The queued requests that have no record yet count, as the observation
	// reads the owner's queues.
	lab, con := f.w.Unit(f.lab), f.w.Unit(f.con)
	lq := orders.BindQueueBinding(lab, h.m.OrderBinding)
	cq := orders.BindQueueBinding(con, h.m.OrderBinding)
	building, site := orders.Lookup("BuildingBuild"), orders.Lookup("MobileBuild")
	lq.Push(building, orders.Node{ID: building, BuildDefKey: "bot", Param2: 2})
	cq.Push(site, orders.Node{ID: site, BuildDefKey: "tower"})
	f.observe(2)
	if got := f.capped(f.bot); got.Records != 0 || got.Queued != 2 || k.Allowance(f.bot) != 0 {
		t.Fatalf("bot %+v with two queued: allowance %d", got, k.Allowance(f.bot))
	}
	if got := f.capped(f.tower); got.Queued != 1 || k.Allowance(f.tower) != 0 {
		t.Fatalf("tower %+v with a placement on its way", got)
	}
	// Production starts and the frame is placed: each request moves from
	// queued to records.
	lq.PrimaryAt(0).Target = f.record(t, "bot", 0, true)
	towerFrame := f.record(t, "tower", 0, true)
	cq.PrimaryAt(0).Target = towerFrame
	f.observe(3)
	if got := f.capped(f.bot); got.Records != 1 || got.Queued != 1 {
		t.Fatalf("bot %+v with one in production", got)
	}
	if got := f.capped(f.tower); got.Records != 1 || got.Queued != 0 {
		t.Fatalf("tower %+v once placed", got)
	}

	// Census: another player's records never count, a dead record counts
	// until its teardown although the observation no longer lists it.
	lq.SetPrimary(nil)
	cq.SetPrimary(nil)
	f.record(t, "bot", 1, false)
	f.record(t, "bot", 1, false)
	dead := f.record(t, "bot", 0, false)
	f.w.Destroy(dead, units.DeathKilled)
	f.observe(4)
	if got := f.capped(f.bot); got.Records != 2 || got.Queued != 0 || k.Allowance(f.bot) != 0 {
		t.Fatalf("bot %+v: want the frame and the record awaiting teardown, and no allowance", got)
	}
	for _, u := range h.obs.Own {
		if u.H == dead {
			t.Fatal("the observation lists a dying unit")
		}
	}
	f.w.FreeImmediate(dead)
	f.observe(5)
	if k.Allowance(f.bot) != 1 {
		t.Fatalf("allowance %d after the teardown, want 1", k.Allowance(f.bot))
	}

	// A stale request: the cap is reached in the reaction window, so the
	// executor drops the request without asking; once a record is gone the
	// same request applies.
	k.Produce(f.lab, f.bot, 1)
	extra := f.record(t, "bot", 0, false)
	before := len(f.reqs)
	h.ex.apply(&h.b, 6, f.w, &Persona{})
	if len(f.reqs) != before || h.ex.stats.Applied != 0 || h.ex.stats.Stale != 1 || h.ex.stats.Capped != 1 {
		t.Fatalf("stale capped request: %+v, %d requests", h.ex.stats, len(f.reqs)-before)
	}
	f.w.FreeImmediate(extra)
	h.ex.apply(&h.b, 7, f.w, &Persona{})
	if len(f.reqs) != before+1 || f.reqs[before].UnitKey != "bot" || f.reqs[before].Count != 1 || h.ex.stats.Applied != 1 {
		t.Fatalf("request after the teardown: %+v, %+v", h.ex.stats, f.reqs[before:])
	}
	// A placement is revalidated the same way, before any site search: none
	// is emitted while the tower's frame stands, and one emitted after its
	// teardown is dropped when another tower appears in the window.
	f.observe(8)
	k.Build(f.con, f.tower, 300, 300, -1, 0, false)
	if len(h.b.cmds) != 0 {
		t.Fatal("a placement past the tower's cap was emitted")
	}
	f.w.FreeImmediate(towerFrame)
	f.observe(9)
	k.Build(f.con, f.tower, 300, 300, -1, 0, false)
	if len(h.b.cmds) != 1 {
		t.Fatal("a placement within the tower's cap was not emitted")
	}
	f.record(t, "tower", 0, true)
	h.ex.apply(&h.b, 10, f.w, &Persona{})
	if len(f.reqs) != before+1 || h.ex.stats.Stale != 2 || h.ex.stats.Capped != 2 {
		t.Fatalf("stale capped placement: %+v", h.ex.stats)
	}
}
