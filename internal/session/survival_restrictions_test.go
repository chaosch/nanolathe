package session

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/economy"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/sim/rng"
	"github.com/nanolathe-gg/nanolathe/internal/survival"
)

// survivalRestrictionCatalog is a small build tree: the ARM commander makes
// two factories and two extractors, factorya alone makes soldiera, factoryb
// makes soldierb and soldierc, and soldierc flies.
func survivalRestrictionCatalog(t *testing.T) *content.Catalog {
	t.Helper()
	cat := minimalCatalogForStrict()
	com := cat.Units["armcom"]
	com.Builder, com.CanMove = true, true
	mk := func(name string, f func(*content.UnitDef)) {
		d := &content.UnitDef{UnitName: name, ObjectName: name, MaxDamage: 100, LimitEnabled: true, Limit: -1, MovementClass: "testmove", FootprintX: 1, FootprintZ: 1}
		f(d)
		d.CanonicalKey = content.CanonicalKey(name)
		cat.Units[d.CanonicalKey] = d
	}
	building := func(builder bool, extracts float64) func(*content.UnitDef) {
		return func(d *content.UnitDef) { d.Builder, d.ExtractsMetal, d.BuildCostMetal = builder, extracts, 100 }
	}
	// The weapon is the catalog's own, so a restricted clone relinks it.
	gun := &content.WeaponDef{ID: 1}
	gun.CanonicalKey = "gun"
	cat.Weapons = map[string]*content.WeaponDef{"gun": gun}
	soldier := func(fly bool) func(*content.UnitDef) {
		return func(d *content.UnitDef) {
			d.CanMove, d.BMCode, d.CanFly = true, 1, fly
			d.BuildCostMetal, d.BuildCostEnergy = 50, 500
			d.Weapon1, d.Weapon1Def = "gun", gun
		}
	}
	mk("factorya", building(true, 0))
	mk("factoryb", building(true, 0))
	mk("mexa", building(false, 0.001))
	mk("mexb", building(false, 0.002))
	mk("soldiera", soldier(false))
	mk("soldierb", soldier(false))
	mk("soldierc", soldier(true))
	cat.BuildMenus = map[string]*content.BuildMenuPage{
		"armcom":   {Buttons: []string{"factorya", "factoryb", "mexa", "mexb"}},
		"factorya": {Buttons: []string{"soldiera"}},
		"factoryb": {Buttons: []string{"soldierb", "soldierc"}},
	}
	if _, err := content.CompileCategories(cat.Units); err != nil {
		t.Fatal(err)
	}
	installFixtureCOB(cat)
	return cat
}

// survivalEntrySession binds a bare session over cat under rules and runs the
// Survival director's setup, as battle entry does once the services exist.
func survivalEntrySession(t *testing.T, cat *content.Catalog, r content.Restrictions, rules RuleSet) (*Session, error) {
	t.Helper()
	if !r.IsZero() {
		var err error
		if cat, err = applyEntryRestrictions(cat, r); err != nil {
			t.Fatal(err)
		}
	}
	s := &Session{Catalog: cat, Restrictions: r, Build: &construction.Service{}, Econ: &economy.Service{}}
	s.BindRules(rules)
	return s, s.initSurvival(SurvivalSkirmishConfig("fixture", 0, SurvivalOptions{}))
}

func survivalPoolKeys(s *Session) []string {
	var keys []string
	for _, u := range s.Survival.pool.Units {
		keys = append(keys, u.Key)
	}
	return keys
}

// TestSurvivalPoolAndSitesUnderRestrictions: the wave pool is walked over the
// restricted catalog, so a removed unit, and a unit only a removed builder
// makes, never enters it, while a capped unit stays; the start site and the
// extra deposits are measured with the first factory and the first extractor
// on the commander's menu that remain (docs/DESIGN_MODS_MUTATORS.md §15.6,
// DESIGN_SURVIVAL §4.2, §4.5). An unrestricted battle keeps its pool, and
// neither setup draws or spends.
func TestSurvivalPoolAndSitesUnderRestrictions(t *testing.T) {
	r := sessionRestrictions(t, map[string]int{"factorya": 0, "mexa": 0, "soldierc": 0, "soldierb": 3})
	for _, rules := range []RuleSet{StrictRuleSet(), ModernRuleSet()} {
		t.Run(rules.Name, func(t *testing.T) {
			cat := survivalRestrictionCatalog(t)
			plain, err := survivalEntrySession(t, cat, content.Restrictions{}, rules)
			if err != nil {
				t.Fatal(err)
			}
			if got := survivalPoolKeys(plain); !reflect.DeepEqual(got, []string{"soldiera", "soldierb", "soldierc"}) {
				t.Fatalf("unrestricted pool %v", got)
			}
			cfg := SurvivalSkirmishConfig("fixture", 0, SurvivalOptions{})
			commander := cat.Units["armcom"]
			if f, m := plain.survivalFirstFactory(commander), plain.survivalFirstExtractor(cfg); f == nil || f.UnitName != "factorya" || m == nil || m.UnitName != "mexa" {
				t.Fatalf("unrestricted first factory %v and extractor %v", f, m)
			}

			s, err := survivalEntrySession(t, cat, r, rules)
			if err != nil {
				t.Fatal(err)
			}
			sim, crt := *s.SimRNG(), *s.CrtRNG()
			if got := survivalPoolKeys(s); !reflect.DeepEqual(got, []string{"soldierb"}) {
				t.Fatalf("restricted pool %v, want only the capped soldierb: soldierc is removed and soldiera's only builder is", got)
			}
			if d := s.Survival.pool.Units[0].Def; d.Limit != 3 || d != s.Catalog.Units["soldierb"] {
				t.Fatal("the pool's capped unit is not the restricted catalog's record")
			}
			commander = s.Catalog.Units["armcom"]
			if f, m := s.survivalFirstFactory(commander), s.survivalFirstExtractor(cfg); f == nil || f.UnitName != "factoryb" || m == nil || m.UnitName != "mexb" {
				t.Fatalf("restricted first factory %v and extractor %v, want the first that remain", f, m)
			}
			if *s.SimRNG() != sim || *s.CrtRNG() != crt {
				t.Fatal("the restricted setup drew random numbers")
			}
		})
	}
}

// TestSurvivalEntryUnderRestrictionsNeedsAnOpener: a restriction set that
// leaves the pool empty, or without an ordinary tier-1 attacker, refuses
// Survival entry with the existing diagnostic naming the set, for a walked
// pool and for an authored roster that loses its only tier-1 opener; the same
// content without restrictions keeps its unchanged checks
// (docs/DESIGN_MODS_MUTATORS.md §15.6, DESIGN_SURVIVAL §5.1).
func TestSurvivalEntryUnderRestrictionsNeedsAnOpener(t *testing.T) {
	rules := StrictRuleSet()
	refused := func(t *testing.T, err error, r content.Restrictions, what string) {
		t.Helper()
		var named *survivalRestrictionsError
		if !errors.As(err, &named) || !strings.Contains(err.Error(), "survival under unit restrictions "+r.String()+": ") || !strings.Contains(err.Error(), what) {
			t.Fatalf("entry error %v, want %q naming the restrictions %q", err, what, r.String())
		}
		if !strings.HasPrefix(err.Error(), "nanolathe: ") || errors.Unwrap(err) == nil || strings.Contains(errors.Unwrap(err).Error(), "restrictions") {
			t.Fatalf("entry error %v does not wrap the existing diagnostic", err)
		}
	}

	t.Run("empty pool", func(t *testing.T) {
		r := sessionRestrictions(t, map[string]int{"factorya": 0, "factoryb": 0})
		s, err := survivalEntrySession(t, survivalRestrictionCatalog(t), r, rules)
		refused(t, err, r, "survival wave pool is empty")
		if s.Survival != nil {
			t.Fatal("a refused entry published a director")
		}
	})

	t.Run("no tier-1 opener", func(t *testing.T) {
		r := sessionRestrictions(t, map[string]int{"factorya": 0, "soldierb": 0})
		_, err := survivalEntrySession(t, survivalRestrictionCatalog(t), r, rules)
		refused(t, err, r, "survival wave pool has no ordinary tier-1 attacker")
		// Without restrictions an all-air pool is still admitted: the opener
		// check binds only a restricted battle.
		cat := survivalRestrictionCatalog(t)
		cat.Units["soldiera"].CanFly, cat.Units["soldierb"].CanFly = true, true
		if _, err := survivalEntrySession(t, cat, content.Restrictions{}, rules); err != nil {
			t.Fatalf("an unrestricted all-air pool was refused: %v", err)
		}
	})

	t.Run("roster opener", func(t *testing.T) {
		withRoster := func() *content.Catalog {
			cat := survivalRestrictionCatalog(t)
			cat.SurvivalRoster = &content.SurvivalRoster{Units: []content.SurvivalRosterEntry{{Unit: "soldiera", Tier: 1}, {Unit: "soldierc", Tier: 1}, {Unit: "soldierb", Tier: 2}}}
			return cat
		}
		if _, err := survivalEntrySession(t, withRoster(), content.Restrictions{}, rules); err != nil {
			t.Fatalf("the unrestricted roster was refused: %v", err)
		}
		r := sessionRestrictions(t, map[string]int{"soldiera": 0})
		_, err := survivalEntrySession(t, withRoster(), r, rules)
		refused(t, err, r, "has no usable tier-1 pool")
	})
}

// TestSurvivalCappedPickIsDroppedWithoutDraws: a capped unit is planned from
// the same draws as an uncapped one, and at creation the allocator counts the
// attacker's own records, so once the cap is reached the director drops the
// pick: it uses that tick's creation, draws nothing, is not retried and does
// not hold up the rest of the wave. The survivors' records of the same unit
// never count against the attacker (docs/DESIGN_MODS_MUTATORS.md §15.6,
// proposal R-P9).
func TestSurvivalCappedPickIsDroppedWithoutDraws(t *testing.T) {
	for _, mode := range []gameplay.Mode{gameplay.Strict31, gameplay.Modern} {
		t.Run(string(mode), func(t *testing.T) {
			// Picks of the host unit only; the human's own host (the fixture's
			// victim) is a teammate's record that never counts.
			spawn := func(capped bool, picks []int) (*Session, int, rng.Simulation) {
				s := infectionWaveFixture(t, mode)
				st := s.Survival
				st.centreX, st.centreZ = 16, 16
				host := st.pool.Units[0].Def
				if capped {
					host.Limit, host.LimitEnabled = 1, true
				}
				st.plan = survival.Wave{Budget: 900, Groups: []survival.Group{{Angle: 0, Domain: survival.Ground, Picks: picks}}}
				st.nextG, st.nextP, st.phase = 0, 0, survivalActive
				*s.SimRNG() = rng.NewSimulation(11)
				before := s.Units.LiveCountForPlayer(int(st.attacker))
				// SpawnPerTick creations a tick: every pick, refused or not,
				// uses one, and the wave's cursor finishes in as many ticks as
				// its picks need.
				ticks := (len(picks) + st.tuning.SpawnPerTick - 1) / st.tuning.SpawnPerTick
				for tick := uint32(60); tick < 60+uint32(ticks); tick++ {
					s.survivalSpawn(tick)
				}
				return s, s.Units.LiveCountForPlayer(int(st.attacker)) - before, *s.SimRNG()
			}
			one, made1, rng1 := spawn(false, []int{0})
			if _, made3, _ := spawn(false, []int{0, 0, 0}); made1 != 1 || made3 != 3 {
				t.Fatalf("uncapped waves of one and three picks made %d and %d units, want 1 and 3", made1, made3)
			}
			s, made, after := spawn(true, []int{0, 0, 0})
			st := s.Survival
			if made != 1 {
				t.Fatalf("under a cap of 1 the wave made %d units, want 1", made)
			}
			if st.nextG != len(st.plan.Groups) || st.nextP != 0 {
				t.Fatalf("spawn cursor (%d,%d): the refused picks stalled the wave", st.nextG, st.nextP)
			}
			if after != rng1 {
				t.Fatal("the refused picks drew from the simulation stream")
			}
			if len(st.waveUnits) != 1 || len(one.Survival.waveUnits) != 1 {
				t.Fatal("a refused pick joined the wave")
			}
			// Not retried: a later tick creates nothing more.
			s.survivalSpawn(90)
			if s.Units.LiveCountForPlayer(int(st.attacker)) != made+1 { // the fixture's infector plus the one host
				t.Fatalf("attacker units %d after the next tick", s.Units.LiveCountForPlayer(int(st.attacker)))
			}
			// The wave still scores the budget it was planned with.
			if st.plan.Budget != 900 {
				t.Fatal("the planned budget changed")
			}
		})
	}
}

// TestSurvivalCapLeavesThePlanDraws: a cap does not take a unit out of the
// pool, so a wave is planned from the same draws, to the same picks, with or
// without one (docs/DESIGN_MODS_MUTATORS.md §15.6).
func TestSurvivalCapLeavesThePlanDraws(t *testing.T) {
	plan := func(capped bool) (survival.Wave, rng.Simulation) {
		s := infectionWaveFixture(t, gameplay.Strict31)
		if capped {
			d := s.Survival.pool.Units[0].Def
			d.Limit, d.LimitEnabled = 1, true
		}
		*s.SimRNG() = rng.NewSimulation(23)
		w := s.survivalPlanWave(20*60*30, nil)
		return w, *s.SimRNG()
	}
	free, freeRNG := plan(false)
	capped, cappedRNG := plan(true)
	if !reflect.DeepEqual(free, capped) || freeRNG != cappedRNG || free.Units() == 0 {
		t.Fatal("a cap changed the wave plan or its draws")
	}
}
