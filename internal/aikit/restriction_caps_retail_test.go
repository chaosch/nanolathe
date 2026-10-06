//go:build retail

package aikit_test

import (
	"fmt"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/ai"
	"github.com/nanolathe-gg/nanolathe/internal/aikit"
	"github.com/nanolathe-gg/nanolathe/internal/aikit/brains/survival"
	"github.com/nanolathe-gg/nanolathe/internal/aikit/brains/tactics"
	"github.com/nanolathe-gg/nanolathe/internal/aikit/brains/utility"
	"github.com/nanolathe-gg/nanolathe/internal/aikit/core"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/economy"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/headless"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/testsupport/retailcat"
	"github.com/nanolathe-gg/nanolathe/internal/units"
)

// The battles here mark their computer players Modern through the lobby, as
// the game does, and capTestStep is the Modern AI's think step: like the
// step mods/aikit installs, it builds each player's host on the player's
// first eligible step and runs it, under whatever set the battle binds.
// Internal packages may not link mods/aikit (internal/architecture), so the
// test binary installs the step itself, and each test says which host a
// player gets (capTestHost).
func init() { session.RegisterModernAI(capTestStep{}) }

// capTestHost builds a Modern player's host; set by the running test.
var capTestHost func(m *ai.Manager) *aikit.Host

type capTestStep struct{}

func (capTestStep) Step(m *ai.Manager, tick uint32, w *units.World, econ *economy.Service) {
	if capTestHost == nil {
		aikit.HostPlanner{}.Step(m, tick, w, econ)
		return
	}
	aikit.StepLazy(m, tick, w, econ, capTestHost)
}

func (capTestStep) ControlsModernAI(*ai.Manager) bool { return true }

// capCheck wraps a brain and checks, on every observation it thinks on,
// that the player's records of each capped definition with its requests
// that have no record yet stay within the cap, and keeps the most records it
// saw of each (docs/DESIGN_SESSIONS_AI_SAVE.md "Modern AI restriction caps").
// It reads only the observation, as the brain does, so it runs on whichever
// goroutine the think does; read it after the host is joined.
type capCheck struct {
	aikit.Brain
	over []string
	peak map[string]int32
}

func newCapCheck(b aikit.Brain) *capCheck { return &capCheck{Brain: b, peak: map[string]int32{}} }

func (c *capCheck) Think(k *aikit.Kit, o *aikit.Obs) {
	for i := range o.Capped {
		cc := &o.Capped[i]
		if cc.Records+cc.Queued > cc.Info.Cap && len(c.over) < 8 {
			c.over = append(c.over, fmt.Sprintf("tick %d: %s records %d queued %d cap %d", o.Tick, cc.Info.Key, cc.Records, cc.Queued, cc.Info.Cap))
		}
		c.peak[cc.Info.Key] = max(c.peak[cc.Info.Key], cc.Records)
	}
	c.Brain.Think(k, o)
}

func utilTac() *core.Brain {
	st, ec, pr := utility.Policies(utility.DefaultParams())
	return core.New("util+tac", st, ec, tactics.New(tactics.DefaultParams()), pr)
}

// capRestrictions caps both sides' extractors and their early vehicles and
// kbots, so the extractor walk and the factory requests both meet a cap:
// unrestricted, both players build more than these within four minutes on
// Great Divide.
func capRestrictions(t *testing.T, cat *content.Catalog) content.Restrictions {
	t.Helper()
	values := map[string]int{}
	for key, n := range map[string]int{
		"armmex": 2, "cormex": 2,
		"armfav": 1, "corfav": 1, "armflash": 1, "corgator": 1, "armstump": 1, "corraid": 1,
		"armpw": 1, "corak": 1, "armrock": 1, "corstorm": 1, "armham": 1, "corthud": 1,
	} {
		if _, ok := cat.Unit(key); !ok {
			t.Skipf("retail unit %q is absent", key)
		}
		values[key] = n
	}
	r, err := content.ParseRestrictions(values)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A Modern AI skirmish under unit restrictions, in Strict 3.1 and Modern: at
// every think each player's records of a capped definition and its requests
// that have no record yet stay within the cap, while the caps on extractors
// and on factory products are both reached, so the brain uses a capped unit
// up to its cap and then stops asking for it; and a host thinking on another
// core plays the same battle as one thinking on the simulation thread
// (docs/DESIGN_MODS_MUTATORS.md §15.7, §15.11).
func TestModernAIMeetsRestrictionCapsRetail(t *testing.T) {
	cat, fs := retailcat.Shared(t)
	const mapName = "Great Divide"
	if _, ok := cat.Maps[content.CanonicalKey(mapName)]; !ok {
		t.Skipf("retail map %q is absent", mapName)
	}
	r := capRestrictions(t, cat)
	// Four minutes: under Strict 3.1 the CORE player, held to two
	// extractors, starts its vehicles in the fourth.
	const ticks = 7200
	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			play := func(async bool) (string, []aikit.ApplyStats, []*capCheck) {
				t.Helper()
				checks := make([]*capCheck, 2)
				capTestHost = func(m *ai.Manager) *aikit.Host {
					persona := aikit.PersonaMed
					persona.Async = async
					c := newCapCheck(utilTac())
					checks[m.Player] = c
					return aikit.NewHost(m, c, persona)
				}
				defer func() { capTestHost = nil }()
				cfg := session.SkirmishConfig{MapName: mapName, NumPlayers: 2, Location: 1}
				for i := 0; i < 2; i++ {
					cfg.Players[i] = session.SkirmishPlayer{Controller: session.SkirmishControllerComputer, AllyGroup: i, Color: i, Side: i, Metal: session.SkirmishDefaultMetal, Energy: session.SkirmishDefaultEnergy}
				}
				cfg.ApplyDefaults()
				cfg.Players[0].Side, cfg.Players[1].Side = 0, 1
				if err := cfg.ApplyComputerAI([]session.ComputerAI{{Row: session.ComputerAIEveryRow, Controller: ai.ControllerModern}}); err != nil {
					t.Fatal(err)
				}
				battle, err := headless.ComposeFreshBattle(headless.FreshBattleRequest{
					Gameplay: mode, Kind: headless.ScenarioDirectOTA, Map: mapName, LocalOwner: -1,
					Difficulty: 2, Skirmish: cfg, SimulationSeed: 7, CRTSeed: 7,
					FS: fs, Catalog: cat, AutomatedPlayers: true, Restrictions: r,
				})
				if err != nil {
					t.Fatal(err)
				}
				s := battle.Session
				defer closeHosts(s)
				for s.Clock.GlobalTick < ticks && s.State != session.StatePostBattle {
					s.Step(s.Clock.ScaledAnchor + 1)
				}
				var stats []aikit.ApplyStats
				for p := uint8(0); p < 2; p++ {
					h := s.AI[p].Ext.(*aikit.Host)
					h.Join()
					if !s.ModernAIPlayer(p) || h.Thinks == 0 {
						t.Fatalf("player %d is not a thinking Modern AI player", p)
					}
					stats = append(stats, h.Stats())
				}
				fp, err := s.PartialStateFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				return fp, stats, checks
			}
			sync, syncStats, checks := play(false)
			for p, c := range checks {
				if len(c.over) > 0 {
					t.Fatalf("player %d asked past a cap: %v", p, c.over)
				}
				mex := [2]string{"armmex", "cormex"}[p]
				if c.peak[mex] != 2 {
					t.Errorf("player %d's %s peaked at %d records, want its cap of 2", p, mex, c.peak[mex])
				}
				product := false
				for key, n := range c.peak {
					if key != mex && n > 0 && n >= int32(mustCount(t, r, key)) {
						product = true
					}
				}
				if !product {
					t.Errorf("player %d reached no factory product's cap (peaks %v)", p, c.peak)
				}
			}
			async, asyncStats, _ := play(true)
			if async != sync || fmt.Sprint(asyncStats) != fmt.Sprint(syncStats) {
				t.Fatalf("the asynchronous hosts played another battle:\n sync %s %+v\nasync %s %+v", sync, syncStats, async, asyncStats)
			}
		})
	}
}

func mustCount(t *testing.T, r content.Restrictions, key string) uint8 {
	t.Helper()
	n, ok := r.Count(key)
	if !ok {
		t.Fatalf("%s has no restriction", key)
	}
	return n
}

// survivalFeed hands the survival brain the warnings the director
// published, as the Modern AI's controller does.
type survivalFeed struct{ info *ai.SurvivalInfo }

func (f survivalFeed) Warnings(tick uint32, dst []survival.Warning) []survival.Warning {
	for _, w := range f.info.Warnings(tick, nil) {
		out := survival.Warning{Wave: w.Wave, Tick: w.Tick, Arrive: w.Arrive}
		for _, g := range w.Groups {
			out.Groups = append(out.Groups, survival.Approach{Angle: g.Angle, X: g.X, Z: g.Z, Air: g.Domain == "air", Naval: g.Domain == "naval", Hover: g.Domain == "hover", Other: g.Domain == "ground"})
		}
		dst = append(dst, out)
	}
	return dst
}

// A Survival battle under a cap, in Strict 3.1 and Modern: the Modern ARM
// buddy builds its capped solar collectors up to the cap on its own slice —
// unrestricted it builds more within the first minute — although the human,
// its teammate, already holds that many of the same unit, since a cap counts
// each player's own records and the team shares sight and income but not
// unit records (docs/DESIGN_MODS_MUTATORS.md §15.6, §15.7, proposal R-P10).
func TestSurvivalBuddyMeetsItsOwnCapRetail(t *testing.T) {
	cat, fs := retailcat.Shared(t)
	const mapName = "Painted Desert"
	if _, ok := cat.Maps[content.CanonicalKey(mapName)]; !ok {
		t.Skipf("retail map %q is absent", mapName)
	}
	solar, ok := cat.Unit("armsolar")
	if !ok {
		t.Skip("retail unit armsolar is absent")
	}
	r, err := content.ParseRestrictions(map[string]int{"armsolar": 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			// Rows: the human (ARM), a Classic CORE buddy, the Modern ARM
			// buddy (row 3, slot 2), then the attacker.
			cfg := session.SurvivalSkirmishConfig(mapName, 2, session.SurvivalOptions{})
			if err := cfg.ApplyComputerAI([]session.ComputerAI{{Row: 3, Controller: ai.ControllerModern}}); err != nil {
				t.Fatal(err)
			}
			const buddy = 2
			var check *capCheck
			capTestHost = func(m *ai.Manager) *aikit.Host {
				if m.Player != buddy || !m.Survival.ComputerSurvivor(buddy) {
					t.Errorf("player %d was given a Modern AI host", m.Player)
				}
				ut := utilTac()
				sc := survival.Scenario{CentreX: m.Survival.CentreX, CentreZ: m.Survival.CentreZ, Me: buddy, Feed: survivalFeed{m.Survival}}
				sc.Team = append(sc.Team, m.Survival.Team...)
				sc.Computer = append(sc.Computer, m.Survival.Computer...)
				sc.Starts = append(sc.Starts, m.Survival.Starts...)
				check = newCapCheck(survival.New(sc, survival.DefaultParams(), survival.Layers{Strategy: ut.Strategy, Economy: ut.Economy, Army: ut.Army, Production: ut.Prod}))
				return aikit.NewHost(m, check, aikit.PersonaMed)
			}
			defer func() { capTestHost = nil }()
			battle, err := headless.ComposeFreshBattle(headless.FreshBattleRequest{
				Kind: headless.ScenarioSurvival, Gameplay: mode, Map: mapName, LocalOwner: -1,
				Skirmish: cfg, Difficulty: 2, SimulationSeed: 5, CRTSeed: 5, FS: fs, Catalog: cat,
				Restrictions: r,
			})
			if err != nil {
				t.Fatal(err)
			}
			s := battle.Session
			defer closeHosts(s)
			if cfg.Players[buddy].Side != 0 {
				t.Fatal("the Modern buddy is not ARM")
			}
			// The human holds the cap of the same unit already.
			restricted, _ := s.Catalog.Unit("armsolar")
			if restricted == solar || restricted.Limit != 2 {
				t.Fatal("the battle does not run on the restricted catalog")
			}
			at := numeric.FixedFromInt(64)
			for i := 0; i < 2; i++ {
				if _, err := s.Units.Create(restricted, 0, at+numeric.FixedFromInt(int64(64*i)), 0, at); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Units.Create(restricted, 0, at, 0, at); err == nil {
				t.Fatal("the human's third armsolar was created under a cap of 2")
			}
			for s.Clock.GlobalTick < 5400 && s.State != session.StatePostBattle {
				s.Step(s.Clock.ScaledAnchor + 1)
			}
			h, ok := s.AI[buddy].Ext.(*aikit.Host)
			if !ok || check == nil {
				t.Fatal("the Modern buddy has no host")
			}
			h.Join()
			if !s.ModernAIPlayer(buddy) || h.Thinks == 0 || h.Brain().Name() != "survival" {
				t.Fatal("the buddy did not think as a Modern survivor")
			}
			if len(check.over) > 0 {
				t.Fatalf("the buddy asked past its cap: %v", check.over)
			}
			if check.peak["armsolar"] != 2 || s.Units.DefinitionCount(buddy, restricted) > 2 {
				t.Fatalf("the buddy's armsolar peaked at %d records, want its own cap of 2 beside the human's 2", check.peak["armsolar"])
			}
			if got := s.Units.DefinitionCount(0, restricted); got != 2 {
				t.Fatalf("the human holds %d armsolar, want 2", got)
			}
		})
	}
}
