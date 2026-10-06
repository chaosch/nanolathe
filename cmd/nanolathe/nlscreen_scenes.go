package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/economy"
	"github.com/nanolathe-gg/nanolathe/internal/features"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/units"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// The Nanolathe screen's preview scenes: which map, which units and what
// happens in front of the camera behind each card
// (docs/DESIGN_INTERFACE_HUD_INPUT.md §3.17). Like the film fixture these are
// capture compositions, not rules: units are created directly and given
// ordinary orders, and every definition, weapon and movement rule is the
// installed content's. The maps are chosen so units stand out: green, lush,
// crystal or red ground, never metal plating.
//
// A scene is the film fixture's battle (film.Scene) plus an optional stage
// function that composes the rest about the anchor: structures, features,
// kills and fires, and events scheduled on the scene's own tick count.

var nlPresets = map[string]nlPreset{
	"arrival": {
		scene:  film.Scene{Kind: "skirmish", Map: "Greenhaven", Seed: 7, PreTicks: 1},
		camera: still(0, 0, 1.6), loop: 6, shot: 1.45,
	},
	"placement": {
		scene:  film.Scene{Kind: "skirmish", Map: "Greenhaven", Seed: 7, PreTicks: 1},
		camera: still(0, 0, 0.9), loop: 60,
	},
	// armor is a tank battle on open grass, for the Game page and the
	// combat mutators.
	"armor": {
		scene:  film.Scene{Kind: "battle", Map: "Great Divide", Seed: 7, PreTicks: 210, Roster: "armor", PerSide: 40, Air: 8, Columns: 8, Anchor: []int32{1440, 1256}},
		camera: still(0, 10, 1.6),
		loop:   30,
		track:  true,
	},
	// naval is every part of the water at once: a shoreline for the foam,
	// ships and aircraft on and over the surface, submarines, underwater
	// structures and the wrecks of ships sunk at the start below it.
	"naval": {
		scene:  film.Scene{Kind: "battle", Map: "Coast To Coast", Seed: 7, PreTicks: 120, Roster: "naval", Anchor: []int32{2284, 1188}},
		camera: still(0, 0, 1.5),
		loop:   40,
		stage:  nlStageNaval,
	},
	// glow is laser towers and lightning holding off waves of attackers
	// while constructors build behind them: beams, bolts, flame and the nano
	// spray, on dark crystal where a halo reads.
	"glow": {
		scene:  film.Scene{Kind: "battle", Map: "Crystal Cracked", Seed: 7, PreTicks: 150, Roster: "glow", Anchor: []int32{3940, 2796}},
		camera: still(90, 5, 1.8),
		loop:   30,
		topUp:  true,
		stage:  func(st *nlStage) { nlStageDefence(st, false) },
	},
	// lighting is the same defence closer in on a forest floor, with trees
	// burning beside it, so light from blasts, fire, beams and the nanolathe
	// pools on the ground and on the units.
	"lighting": {
		scene:  film.Scene{Kind: "battle", Map: "Gasbag Forests", Seed: 7, PreTicks: 150, Roster: "glow", Anchor: []int32{3264, 552}},
		camera: still(85, -15, 2.0),
		loop:   26,
		topUp:  true,
		stage:  func(st *nlStage) { nlStageDefence(st, true) },
	},
	// fireshimmer is a patch of forest burning with nothing else in frame.
	"fireshimmer": {
		scene:  film.Scene{Kind: "battle", Map: "SHERWOOD", Seed: 7, PreTicks: 150, Roster: "armor", Anchor: []int32{2016, 1480}},
		camera: still(0, 4, 1.8),
		loop:   24,
		stage:  nlStageForestFire,
	},
	// hotwrecks is wrecks at every stage of cooling in a quiet frame: a fresh
	// one appears every 40 ticks among the others, glows and shimmers, and
	// cools.
	"hotwrecks": {
		scene:  film.Scene{Kind: "battle", Map: "Crystal Cracked", Seed: 7, PreTicks: 322, Roster: "heavy", Anchor: []int32{4388, 1164}},
		camera: still(0, 3, 2.0),
		loop:   90,
		stage:  nlStageHotWrecks,
	},
	// construct is construction only: constructors raising structures with
	// every site in reach from where they stand, so none walks, framed on the
	// work, for the build mutators. The lead-in includes the stock constructors'
	// opening and aiming before their script-owned readiness write, so the
	// compare appears with work underway [04 R-ORD-01 §5].
	"construct": {
		scene:  film.Scene{Kind: "battle", Map: "Greenhaven", Seed: 7, PreTicks: 120, Roster: "kbots", Anchor: []int32{2230, 4440}},
		camera: still(0, 20, 1.5),
		loop:   40,
		topUp:  true,
		stage:  nlStageWorksite,
	},
	// salvage is constructors reclaiming the wrecks of a fight that has just
	// ended, with the viewer's metal starting empty so the income shows.
	"salvage": {
		scene:  film.Scene{Kind: "battle", Map: "Greenhaven", Seed: 7, PreTicks: 45, Roster: "kbots", Anchor: []int32{2230, 4440}},
		camera: still(0, 20, 1.6),
		loop:   35,
		stage:  nlStageSalvage,
	},
	// blast is a plasma battery out of frame shelling the middle of a block
	// of enemy tanks every three seconds, with each tank's health bar showing
	// what the splash did. The lead-in is sized so the first shell lands 20
	// ticks after the scene appears, and the capture catches the second
	// just after it lands.
	"blast": {
		scene:  film.Scene{Kind: "battle", Map: "Greenhaven", Seed: 7, PreTicks: 187, Roster: "heavy", Anchor: []int32{2308, 4386}},
		camera: still(0, 0, 1.6),
		loop:   36,
		stage:  nlStageBlast,
		shot:   3.8,
	},
	// trails is an armoured march over snow, where footprints and tracks
	// darken the ground behind the columns.
	"trails": {
		scene:  film.Scene{Kind: "battle", Map: "Ice Scream", Seed: 7, PreTicks: 300, Roster: "armor", PerSide: 24, Columns: 6, Gap: 700, Buildings: -1, Anchor: []int32{3744, 552}},
		camera: still(0, 0, 1.6),
		loop:   30,
		track:  true,
	},
	// air is aircraft over open ground, their shadows sliding below them.
	"air": {
		scene:  film.Scene{Kind: "battle", Map: "Greenhaven", Seed: 7, PreTicks: 30, Roster: "air", PerSide: 12, Air: 16, Columns: 4, Gap: 350, Anchor: []int32{832, 4520}},
		camera: still(0, 5, 2.0),
		loop:   20,
	},
	// controls is a march on open ground behind the Controls page. The frame
	// holds the ground the viewer's column crosses in the loop, from where it
	// stands at the end of the lead-in, so the camera never has to follow it.
	"controls": {
		scene:    film.Scene{Kind: "battle", Map: "Luschaven", Seed: 7, PreTicks: 30, Roster: "armor", PerSide: 24, Columns: 6, Gap: 600, Buildings: -1, Anchor: []int32{4504, 5188}},
		camera:   still(250, 0, 1.7),
		loop:     24,
		track:    true,
		trackOwn: true,
	},
	// metal is hovercraft and tanks circling tight loops close up on a
	// hillside that faces the renderer's fixed key light, so their hulls,
	// which lie along the ground, turn their decks into the glint's lobe and
	// the metal finish's brightest step (nlStageMetal).
	"metal": {
		scene:   film.Scene{Kind: "battle", Map: "Great Divide", Seed: 7, PreTicks: 20, Roster: "heavy", Anchor: []int32{1712, 2536}},
		camera:  still(-30, 0, 2.0),
		loop:    40,
		surface: 0.45,
		stage:   nlStageMetal,
	},
	// crater is a heavy battle on red ground: shells throw blast rings and
	// leave scorch marks among the wrecks.
	"crater": {
		scene:  film.Scene{Kind: "battle", Map: "Red Planet", Seed: 7, PreTicks: 380, Roster: "heavy", PerSide: 60, Columns: 8, Gap: 260, Anchor: []int32{1944, 452}},
		camera: still(0, 6, 1.6),
		loop:   30,
		track:  true,
	},
	// march is two long armoured columns closing at a wide view, for the
	// unit-speed mutator and the rule sets. The still frame holds the ground
	// between them where they meet; the lead-in brings the columns to its
	// edges, so at a raised speed they are already closing when the scene
	// appears while at ×1 they are only arriving.
	"march": {
		scene:  film.Scene{Kind: "battle", Map: "Great Divide", Seed: 7, PreTicks: 150, Roster: "armor", PerSide: 40, Columns: 8, Gap: 500, Buildings: -1, Anchor: []int32{1440, 1256}},
		camera: still(0, 0, 0.9),
		loop:   16,
		track:  true,
	},
	// sight is a fogged march for the sight and radar mutators. The frame
	// holds the ground the viewer's column crosses in the loop, from where it
	// stands at the end of the lead-in to where it meets the enemy, so the
	// sight's edge moves with the column while the camera stays still.
	"sight": {
		scene:    film.Scene{Kind: "battle", Map: "Red River", Seed: 7, PreTicks: 60, Roster: "armor", PerSide: 30, Columns: 6, Gap: 600, Buildings: -1, Fog: true, Anchor: []int32{9472, 2216}},
		camera:   still(360, 0, 0.6),
		loop:     25,
		track:    true,
		trackOwn: true,
		// Circular line of sight, whose mask grows to fourteen cells, so the
		// Sight card's edge visibly moves with the factor (see circular).
		circular: true,
	},
	// closeup holds the detail view's own factor, so the Classic executor,
	// which draws only the native and 2x steps, frames it exactly as
	// Enhanced does and the renderer compare lines up (DESIGN_GPU_RENDERER
	// §16.8).
	"closeup": {
		scene:  film.Scene{Kind: "battle", Map: "Greenhaven", Seed: 7, PreTicks: 260, Roster: "armor", PerSide: 40, Air: 6, Columns: 8, Anchor: []int32{832, 4520}},
		camera: still(0, 5, 2.0),
		loop:   30,
		track:  true,
	},
}

// nlStage composes one scene about its anchor: it finds open ground for
// what it places, keeps sites from overlapping, and collects the events the
// preview runs on the scene's tick count.
type nlStage struct {
	s         *session.Session
	cx, cz    int32
	taken     []nlBox
	events    []nlEvent
	roster    *nlRoster
	required  []string
	limits    []string
	placement string
	// live is set once staging is done: an event's units walk off, so what
	// it places is checked against the ground alone and reserves nothing.
	live bool
}

type nlBox struct{ x0, z0, x1, z1 int32 }

// nlEvent runs fn at tick at, and every period ticks after it when period is
// positive. Ticks count from the first lead-in tick, which is 1.
type nlEvent struct {
	at, period int
	fn         func(s *session.Session, tick int)
}

func nlFixed(v int32) numeric.Fixed { return numeric.Fixed(int64(v) << 16) }

func (st *nlStage) at(tick int, fn func(s *session.Session, tick int)) {
	st.events = append(st.events, nlEvent{at: tick, fn: fn})
}

func (st *nlStage) every(start, period int, fn func(s *session.Session, tick int)) {
	st.events = append(st.events, nlEvent{at: start, period: period, fn: fn})
}

// run fires the events due at tick.
func nlRunEvents(events []nlEvent, s *session.Session, tick int) {
	for _, e := range events {
		if tick == e.at || e.period > 0 && tick > e.at && (tick-e.at)%e.period == 0 {
			e.fn(s, tick)
		}
	}
}

func (st *nlStage) free(b nlBox) bool {
	for _, t := range st.taken {
		if b.x0 < t.x1 && t.x0 < b.x1 && b.z0 < t.z1 && t.z0 < b.z1 {
			return false
		}
	}
	return true
}

// footBox is the footprint a definition covers centred on (x, z), with a
// margin so neighbours never touch.
func nlFootBox(def *content.UnitDef, x, z, margin int32) nlBox {
	hx, hz := def.FootprintX*8+margin, def.FootprintZ*8+margin
	return nlBox{x - hx, z - hz, x + hx, z + hz}
}

// spot finds open ground for def nearest (x, z): searched in square rings of
// 16-pixel steps out to rings, refused by the unit's own placement rules and
// by anything the stage already placed. It reserves what it returns, and
// reports the height the unit stands at.
func (st *nlStage) spot(def *content.UnitDef, x, z, rings int32) (int32, int32, numeric.Fixed, bool) {
	s := st.s
	for ring := int32(0); ring <= rings; ring++ {
		for dz := -ring; dz <= ring; dz++ {
			for dx := -ring; dx <= ring; dx++ {
				if max(wsAbs(dx), wsAbs(dz)) != ring {
					continue
				}
				px, pz := x+dx*16, z+dz*16
				if px < 64 || pz < 64 || px >= s.World.PlayRight-64 || pz >= s.World.PlayBottom-64 {
					continue
				}
				fx, fz := nlFixed(px), nlFixed(pz)
				if def.BMCode == 0 {
					ax, az := world.PlacementAnchor(fx, fz, def.FootprintX, def.FootprintZ)
					fx, fz = world.PlacementCenter(ax, az, def.FootprintX, def.FootprintZ)
					px, pz = int32(fx.Int()), int32(fz.Int())
				}
				b := nlFootBox(def, px, pz, 4)
				if !st.live && !st.free(b) {
					continue
				}
				y, ok := nlPlacement(s, def, fx, fz)
				if !ok {
					continue
				}
				if !st.live {
					st.taken = append(st.taken, b)
				}
				return px, pz, y, true
			}
		}
	}
	return 0, 0, 0, false
}

// nlPlacement reports whether def may stand at (x, z) and the height it
// stands at: a structure's site height as the placement validator publishes
// it, the ground (or the surface, for a floater) for a mobile unit.
func nlPlacement(s *session.Session, def *content.UnitDef, x, z numeric.Fixed) (numeric.Fixed, bool) {
	y, err := nlPlace(s, def, x, z)
	return y, err == nil
}

// nlPlace is nlPlacement with the validator's refusal, for the diagnostic a
// scene prints when a unit finds no ground.
func nlPlace(s *session.Session, def *content.UnitDef, x, z numeric.Fixed) (numeric.Fixed, error) {
	fx, fz := world.FootprintForUnit(s.Catalog, def)
	extent, err := world.NewFootprintExtent(fx, fz)
	if err != nil {
		return 0, err
	}
	ax, az := world.PlacementAnchor(x, z, fx, fz)
	rect, err := world.NewFootprintRect(world.NewFootprintAnchor(ax, az), extent)
	if err != nil {
		return 0, err
	}
	rules, err := world.PlacementRulesForUnit(s.Catalog, def)
	if err != nil {
		return 0, err
	}
	query := world.PlacementQuery{Rect: rect, Rules: rules, Mobile: def.BMCode != 0}
	if !query.Mobile {
		// A structure takes the building validator's yard walk, and then the
		// mobile walk for anything standing on its open yard cells, as the
		// spawn command checks it.
		if query.Yard, err = world.ParseYardMap(def.YardMap, int(fx), int(fz)); err != nil {
			return 0, err
		}
		res, err := s.World.CheckPlacement(query)
		if err != nil {
			return 0, err
		}
		query.Mobile, query.Yard, query.SkipTerrainAggregates = true, nil, true
		if _, err := s.World.CheckPlacement(query); err != nil {
			return 0, err
		}
		return nlFixed(res.SiteHeight), nil
	}
	if _, err := s.World.CheckPlacement(query); err != nil {
		return 0, err
	}
	y := s.World.HeightAt(x, z)
	if y < s.World.SeaLevelWorld() && def.Floater {
		y = s.World.SeaLevelWorld()
	}
	return y, nil
}

// unit creates name for owner on the open ground nearest (x, z), or nil when
// the content lacks it or no spot within rings accepts it.
func (st *nlStage) unit(name string, owner uint8, x, z, rings int32) *units.Unit {
	s := st.s
	name = st.name(name)
	def, ok := s.Catalog.Unit(name)
	if !ok {
		return nil
	}
	px, pz, y, ok := st.spot(def, x, z, rings)
	if !ok {
		fmt.Fprintf(os.Stderr, "nanolathe: preview: no ground for %s near %d,%d%s\n", name, x, z, nlRefusal(s, def, x, z))
		return nil
	}
	h, err := s.Units.Create(def, owner, nlFixed(px), y, nlFixed(pz))
	if err != nil {
		return nil
	}
	u := s.Units.Unit(h)
	if s.Movement != nil {
		s.Movement.EnsureUnit(u)
	}
	s.BindStagedOrderQueue(u)
	return u
}

// hold sets a unit's standing move order to hold position, so being shot
// at from beyond its range never draws it out of the frame.
func nlHold(u *units.Unit) {
	if u != nil {
		u.Flags &^= units.StandingFieldMask << units.StandingMoveShift
	}
}

// nlKill destroys a unit now. A wreck death reads as a light one — the
// prior health sample and the health both at zero — so the unit's own
// Killed script chooses its whole corpse rather than a heap [06 §12.1].
func nlKill(s *session.Session, u *units.Unit, wreck bool) {
	if u == nil || !u.Alive || u.Dying {
		return
	}
	if wreck {
		u.PriorSample, u.CurrentSample = 0, 0
		u.Health = 0
	} else {
		u.Health = -u.MaxHealth
	}
	s.Units.Destroy(u.Handle, units.DeathKilled)
}

// nlKillWith destroys a unit now with an explicit post-hit health and prior
// health sample, the two unit inputs of the death severity [06 §12.1]: the
// unit viewer's field chooses them so its unit's Killed script receives the
// selected severity (unitViewerFieldDeathInputs). The current sample takes
// the same value, so a 30-tick sampling boundary on the death tick, which
// shifts the current sample into the prior one [04 §5.1], leaves the input
// unchanged. Like nlKill it records no damage kind.
func nlKillWith(s *session.Session, u *units.Unit, health int32, prior uint8) {
	if u == nil || !u.Alive || u.Dying {
		return
	}
	u.PriorSample, u.CurrentSample = prior, prior
	u.Health = health
	s.Units.Destroy(u.Handle, units.DeathKilled)
}

// nlPush queues an order record by descriptor name, the way the film
// fixture's filmOrder does for moves.
func nlPush(s *session.Session, u *units.Unit, name string, target pool.Handle, x, y, z numeric.Fixed) {
	q := orders.QueueForUnit(u)
	id := orders.Lookup(name)
	if q == nil || id == 0 {
		return
	}
	q.Push(id, orders.NewNodeForOrder(id, target, x, y, z, s.Clock.GlobalTick, u.Handle, false))
}

// loop gives a unit a patrol round a square of half-side r about (x, z),
// starting at corner k, so it keeps turning in place of arriving.
func nlLoop(s *session.Session, u *units.Unit, x, z, r int32, k int) {
	if u == nil {
		return
	}
	for i := 0; i < 4; i++ {
		c := (i + k) % 4
		dx := [4]int32{-r, r, r, -r}[c]
		dz := [4]int32{-r, -r, r, r}[c]
		filmOrder(s, u, 9, nlFixed(x+dx), nlFixed(z+dz))
	}
}

// nlIgnite sets alight the flammable features whose anchor lies within
// radius of (x, z), at most limit of them, nearest first; a feature already
// burning or burnt is not a candidate.
func nlIgnite(s *session.Session, x, z, radius int32, limit int) int {
	if s.Features == nil {
		return 0
	}
	type cand struct {
		f *features.Instance
		d int64
	}
	var cands []cand
	r2 := int64(radius) * int64(radius)
	for _, f := range s.Features.Instances() {
		if f == nil || f.Def == nil || !f.Def.Flamable || f.Def.Indestructible || f.IsBurning {
			continue
		}
		dx, dz := int64(f.CX)*16+8-int64(x), int64(f.CZ)*16+8-int64(z)
		if d := dx*dx + dz*dz; d <= r2 {
			cands = append(cands, cand{f, d})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		switch {
		case a.d < b.d:
			return -1
		case a.d > b.d:
			return 1
		}
		return 0
	})
	lit := 0
	for _, c := range cands {
		if lit >= limit {
			break
		}
		if s.Features.Ignite(c.f.CX, c.f.CZ, 1, 0) {
			lit++
		}
	}
	return lit
}

// nlClear removes every destructible feature whose footprint meets the
// rectangle x0..x1, z0..z1 (world pixels), so nothing staged inside it is
// hidden: a composition choice like the grove planting. A tree's sprite rises
// up the screen from its cell, so a caller reaches further south than
// elsewhere to keep trees below a unit from covering it. Each feature goes
// down its own successor chain — reclaimed, dead or burnt, whichever plays no
// event animation — until its cell is empty [05 "Removal and successor
// replacement"]; deposits and every other indestructible feature stay. It
// reports how many features it removed.
func nlClear(s *session.Session, x0, z0, x1, z1 int32) int {
	if s.Features == nil {
		return 0
	}
	type cell struct{ cx, cz int }
	var cells []cell
	for _, f := range s.Features.Instances() {
		if f == nil || f.Def == nil || f.Def.Indestructible {
			continue
		}
		fx0, fz0 := int32(f.CX)*16, int32(f.CZ)*16
		fx1, fz1 := fx0+max(f.Def.FootprintX, 1)*16, fz0+max(f.Def.FootprintZ, 1)*16
		if fx0 < x1 && x0 < fx1 && fz0 < z1 && z0 < fz1 {
			cells = append(cells, cell{f.CX, f.CZ})
		}
	}
	for _, c := range cells {
		for hop := 0; hop < 6; hop++ {
			f := s.Features.InstanceAt(c.cx, c.cz)
			if f == nil || f.Def == nil || f.Def.Indestructible {
				break
			}
			cause := features.CauseBurnt // a bare stamp of the burnt successor
			switch {
			case f.Def.SeqNameReclamate == "":
				cause = features.CauseReclaim
			case f.Def.SeqNameDie == "":
				cause = features.CauseDead
			}
			s.Features.RemoveFeatureAt(c.cx, c.cz, cause)
		}
	}
	return len(cells)
}

// sentinels keep both sides in the battle: a side with nothing left has
// lost and the battle stops, so a scene that kills or omits a side's units
// keeps one far-off structure for each.
func (st *nlStage) sentinels() {
	st.sentinel()
	s := st.s
	def, ok := s.Catalog.Unit(st.name("armsolar"))
	if !ok {
		return
	}
	// The viewer's own lies across the map from the anchor in x only, so it
	// never shares the enemy's corner.
	tx, tz := s.World.PlayRight-st.cx, st.cz
	if x, z, y, ok := st.spot(def, tx, tz, 40); ok {
		_, _ = s.Units.Create(def, s.LocalOwner, nlFixed(x), y, nlFixed(z))
	}
}

// storage places metal and energy storage out of frame, so a topped-up
// economy holds enough for a plasma cannon's shot or a builder's rate.
func (st *nlStage) storage() {
	for i, name := range []string{"armmstor", "armestor", "armestor"} {
		st.unit(name, st.s.LocalOwner, st.cx-700-int32(i)*90, st.cz+420, 24)
	}
}

// ------------------------------------------------------------------ naval

// nlStageNaval builds the naval scene at a shoreline — the anchor is deep
// water with land in the frame's corner — where the viewer's underwater
// structures stand about it, submarines and ships of both sides patrol
// across it with aircraft overhead, and a few ships are sunk at the start so
// their wrecks settle on the bottom.
func nlStageNaval(st *nlStage) {
	s := st.s
	if st.roster != nil {
		supported := false
		for side := range st.roster.factions {
			for _, role := range []nlUnitRole{nlShip, nlSubmarine, nlAircraft} {
				if st.roster.pick("", side, role, nil) != "" {
					supported = true
				}
			}
		}
		if !supported {
			// The shoreline anchor can be deep water. A dry-only roster needs
			// its own ordinary ground composition and camera anchor (§3.17).
			x, z, _, err := filmBattleCentre(film.Scene{}, s.World)
			if err != nil {
				st.limit("This content has no naval or combat aircraft units, and this map has no ground area for a compatible battle.")
				return
			}
			st.cx, st.cz = x, z
			st.limit("This content has no naval or combat aircraft units; a ground battle is shown.")
			st.compatibleBattle()
			return
		}
	}
	cx, cz := st.cx, st.cz
	own, foe := s.LocalOwner, s.EnemyOwner
	// Underwater structures ring the anchor; each takes the nearest water
	// deep enough for it.
	for _, p := range []struct {
		name   string
		dx, dz int32
	}{
		{"armuwms", -110, -40}, {"armuwes", 20, -90}, {"armuwmex", 130, -20},
		{"armtl", -40, 70}, {"armsonar", 90, 90}, {"armtl", 200, 60},
	} {
		st.unit(p.name, own, cx+p.dx, cz+p.dz, 10)
	}
	// Submarines patrol loops through the structures.
	for i, name := range []string{"armsub", "armsubk", "armsub"} {
		u := st.unit(name, own, cx-60+int32(i)*90, cz+20, 8)
		nlLoop(s, u, cx+int32(i)*40, cz, 150, i)
	}
	for i, name := range []string{"corsub", "corshark", "corsub"} {
		u := st.unit(name, foe, cx+230+int32(i)*40, cz-60+int32(i)*80, 10)
		nlLoop(s, u, cx+160, cz+int32(i)*30-30, 170, i+2)
	}
	// Ships of both sides cross the frame and fight.
	for i, name := range []string{"armroy", "armcrus", "armroy", "armpt"} {
		u := st.unit(name, own, cx-260, cz-140+int32(i)*90, 10)
		nlLoop(s, u, cx+40, cz-20+int32(i%2)*40, 220, i)
	}
	for i, name := range []string{"corroy", "corcrus", "corroy", "corpt"} {
		u := st.unit(name, foe, cx+380, cz-80+int32(i)*80, 10)
		nlLoop(s, u, cx+120, cz+20-int32(i%2)*40, 220, i+2)
	}
	// Aircraft circle over the water.
	for i, preferred := range []string{"armthund", "armfig", "armthund", "corshad", "corveng", "corshad"} {
		name := st.name(preferred)
		side := own
		if i >= 3 {
			side = foe
		}
		x, z := cx-150+int32(i)*90, cz-160+int32(i%2)*280
		// In flight from the first frame, through the airborne creator and the
		// cruise-altitude seam the film fixture uses [04 §10.1].
		u := st.airOrUnit(name, side, x, z, 10)
		nlLoop(s, u, cx+60+int32(i%3)*30, cz, 170, i)
	}
	// The sunk: ships and a submarine placed over the deepest water near the
	// anchor and destroyed on the first ticks, so their wrecks sink.
	var sunk []*units.Unit
	for i, name := range []string{"corcrus", "armroy", "corsub", "armcrus"} {
		owner := foe
		if i%2 == 1 {
			owner = own
		}
		sunk = append(sunk, st.unit(name, owner, cx-150+int32(i)*110, cz+170-int32(i%2)*260, 8))
	}
	st.at(2, func(s *session.Session, _ int) {
		for _, u := range sunk {
			nlKill(s, u, true)
		}
	})
	st.sentinels()
}

// ---------------------------------------------------------------- defence

// nlStageDefence is laser towers and lightning holding the anchor against
// waves of lasers and flame walking in from the east, with constructors
// building behind the towers. With fire, trees nearby are set alight.
func nlStageDefence(st *nlStage, fire bool) {
	s := st.s
	cx, cz := st.cx, st.cz
	own, foe := s.LocalOwner, s.EnemyOwner
	st.storage()
	for _, p := range []struct {
		name   string
		dx, dz int32
	}{{"armllt", 40, -70}, {"armhlt", 70, 10}, {"armllt", 40, 90}} {
		st.unit(p.name, own, cx+p.dx, cz+p.dz, 6)
	}
	// Lightning in front of the towers, where the waves come within reach.
	for i := int32(0); i < 3; i++ {
		u := st.unit("armzeus", own, cx+150, cz-60+i*60, 6)
		nlHold(u)
	}
	// Constructors behind the line, each beside the structure it raises.
	builderName := st.name("armck")
	bd, _ := s.Catalog.Unit(builderName)
	for i, p := range []struct{ product string }{{"armrad"}, {"armsolar"}, {"armllt"}} {
		product := st.buildProduct(bd, p.product)
		pd, ok := s.Catalog.Unit(product)
		if !ok {
			continue
		}
		z := cz - 100 + int32(i)*100
		sx, sz, _, ok := st.spot(pd, cx-120, z, 4)
		if !ok {
			continue
		}
		b := st.unit(builderName, own, sx+pd.FootprintX*8+max(bd.FootprintX*8, 20), sz, 3)
		if b == nil {
			continue
		}
		st.queueBuild(b, product, sx, sz)
	}
	// Waves walk in from the east and on through the towers.
	waveNames := st.names([]string{"corak", "corpyro", "corak", "corpyro", "corak"})
	wave := func(s *session.Session, tick int) {
		k := int32(tick / 30)
		for i, name := range waveNames {
			x := cx + 380 + int32(i%2)*40 + (k*37)%60
			z := cz - 110 + int32(i)*55
			u := st.unit(name, foe, x, z, 4)
			if u != nil {
				filmOrder(s, u, 2, nlFixed(cx-160), nlFixed(z))
			}
		}
	}
	st.at(1, wave)
	st.every(1, 240, func(s *session.Session, tick int) {
		if tick > 1 {
			wave(s, tick)
		}
	})
	if fire {
		// Copses of the map's own plants above and below the fight, each set
		// alight whole, one every five seconds from just before the scene
		// appears, so there is always one burning beside it: a copse burns for
		// five to six seconds.
		for k, c := range nlLightingCopses {
			nlCopse(s, cx+c[0], cz+c[1], k)
			st.at(120+k*150, func(s *session.Session, _ int) {
				nlIgnite(s, cx+c[0], cz+c[1], nlCopseReach*16+24, len(nlCopseTrees[0]))
			})
		}
	}
	st.sentinels()
}

// nlLightingCopses are the lighting scene's copses about the anchor, in the
// order they are lit: a row above the fight and a row below it, clear of the
// towers and the waves' lanes, at least nlCopseSpacing apart.
var nlLightingCopses = [][2]int32{{190, -150}, {260, 140}, {330, -150}, {120, 140}, {50, -150}, {400, 140}}

// queueBuild queues the builder to raise product with its site centred at
// (x, z).
func (st *nlStage) queueBuild(b *units.Unit, product string, x, z int32) {
	s := st.s
	st.requireUnit(product)
	pd, ok := s.Catalog.Unit(product)
	if !ok || b == nil {
		return
	}
	ax, az := world.PlacementAnchor(nlFixed(x), nlFixed(z), pd.FootprintX, pd.FootprintZ)
	sx, sz := world.PlacementCenter(ax, az, pd.FootprintX, pd.FootprintZ)
	if err := construction.QueueMobileBuild(b, product, sx, sz, 1, s.Catalog); err != nil {
		fmt.Fprintf(os.Stderr, "nanolathe: preview: %s cannot queue %s: %v\n", b.Def.UnitName, product, err)
	}
}

// ------------------------------------------------------------ forest fire

// nlFireCopses are the copses of the fire-shimmer scene about the anchor, in
// the order the fire reaches them, spread over the part of the frame right
// of the hero text. Each is nlCopseReach cells across at most and they stand
// at least nlCopseSpacing apart, so no tree lies within the three cells a
// burning tree's spread tests [05 R-FEAT-01 §11] of another copse's: each
// burns only when the scene lights it, and one is always catching.
var nlFireCopses = [][2]int32{
	{-150, -118}, {-4, -100}, {140, -124}, {284, -104},
	{290, 40}, {150, 28}, {2, 46}, {-146, 30},
	{-72, 184}, {72, 176}, {216, 188},
}

// nlFireStart and nlFirePeriod time the fire-shimmer scene's ignitions: one
// tree at the middle of the next copse every period, from a start inside the
// lead-in so the first copses are well alight when the scene appears.
const (
	nlFireStart  = 2
	nlFirePeriod = 75
)

// nlStageForestFire clears the ground right of the hero text of the map's
// own trees and plants copses of them there, then lights the copses one
// after another, so the fire keeps catching somewhere in frame for the whole
// loop, and spreads through each copse from the tree it starts at. No unit
// is placed near it.
func nlStageForestFire(st *nlStage) {
	cx, cz := st.cx, st.cz
	nlClear(st.s, cx-260, cz-220, cx+400, cz+260)
	for k, c := range nlFireCopses {
		nlCopse(st.s, cx+c[0], cz+c[1], k)
	}
	for k, c := range nlFireCopses {
		// The first two catch together, so the scene opens on fire.
		at := nlFireStart + max(k-1, 0)*nlFirePeriod
		st.at(at, func(s *session.Session, _ int) { nlIgnite(s, cx+c[0], cz+c[1], 24, 1) })
	}
	st.sentinels()
}

// nlCopseTrees are the shapes a copse's trees take about its middle, one
// chosen per copse so neighbours differ: small hexagons, upright and on
// their side, and a lopsided one, each seven trees nlCopseReach cells from
// the middle at most, close enough that a fire started at the middle
// spreads to all of them.
var nlCopseTrees = [][][2]int32{
	{{0, 0}, {-2, 0}, {2, 0}, {-1, -2}, {1, -2}, {-1, 2}, {1, 2}},
	{{0, 0}, {0, -2}, {0, 2}, {-2, -1}, {-2, 1}, {2, -1}, {2, 1}},
	{{0, 0}, {-2, 0}, {2, 1}, {-1, -2}, {1, -2}, {0, 2}, {-2, 2}},
}

// nlCopseReach is how many cells from its middle a copse's trees stand, and
// nlCopseSpacing the least distance between two copses' middles, world
// pixels: two reaches and the four cells past the spread's three-cell window.
const (
	nlCopseReach   = 2
	nlCopseSpacing = (2*nlCopseReach + 4) * 16
)

// nlCopse plants a copse of the map's commonest small flammable feature
// (nlTreeDef) about (x, z) in shape k of nlCopseTrees; a cell already
// holding something is skipped.
func nlCopse(s *session.Session, x, z int32, k int) {
	def := nlTreeDef(s)
	if def == nil {
		return
	}
	for _, c := range nlCopseTrees[k%len(nlCopseTrees)] {
		cx, cz := x/16+c[0], z/16+c[1]
		p := s.World.PlotAt(cx, cz)
		if p == nil || !p.IsEmpty() || p.StructureYard() {
			continue
		}
		s.Features.PlaceAt(int(cx), int(cz), def)
	}
}

// nlTreeDef is the map's commonest small flammable feature, the tree a grove
// or copse is planted from, or nil when the map has none.
func nlTreeDef(s *session.Session) *content.FeatureDef {
	var defs []*content.FeatureDef
	var counts []int
	for _, f := range s.Features.Instances() {
		if f == nil || f.Def == nil || !f.Def.Flamable || f.Def.Indestructible || f.Def.FootprintX > 1 || f.Def.FootprintZ > 1 {
			continue
		}
		i := slices.Index(defs, f.Def)
		if i < 0 {
			defs, counts = append(defs, f.Def), append(counts, 0)
			i = len(defs) - 1
		}
		counts[i]++
	}
	if len(defs) == 0 {
		return nil
	}
	best := 0
	for i := range defs {
		if counts[i] > counts[best] {
			best = i
		}
	}
	return defs[best]
}

// ------------------------------------------------------------- hot wrecks

// nlHotWreckUnits are the units whose wrecks the hot-wrecks scene leaves.
var nlHotWreckUnits = []string{"armbull", "corgol", "armzeus", "correap", "armstump", "corcan", "armfido", "corthud"}

// nlHotWreckSpots are where the wrecks lie about the anchor, scattered.
var nlHotWreckSpots = [][2]int32{{-120, -40}, {-30, -85}, {70, -60}, {150, -10}, {-80, 40}, {15, 15}, {105, 60}, {-10, 100}}

// nlHotWreckPeriod is the ticks between wrecks. A spot comes round every
// len(spots) periods, past the 300 ticks a wreck takes to cool fully
// (DESIGN_GPU_RENDERER §28).
const nlHotWreckPeriod = 40

// nlStageHotWrecks leaves a fresh wreck at one of the spots every period: a
// unit is created and destroyed on the same tick, so none is seen alive,
// and the cold wreck lying at that spot from the last round is cleared
// first. The lead-in fills every spot, so the frame always holds wrecks at
// every stage of cooling and nothing else moves.
func nlStageHotWrecks(st *nlStage) {
	cx, cz := st.cx, st.cz
	names := st.names(nlHotWreckUnits)
	if len(names) == 0 {
		st.sentinels()
		return
	}
	round := len(nlHotWreckSpots) * nlHotWreckPeriod
	for k, spot := range nlHotWreckSpots {
		st.every(2+k*nlHotWreckPeriod, round, func(s *session.Session, tick int) {
			x, z := cx+spot[0], cz+spot[1]
			if f := nlWreckNear(s, x, z, 48); f != nil {
				s.Features.RemoveFeatureAt(f.CX, f.CZ, features.CauseReclaim)
			}
			owner := s.LocalOwner
			if k%2 == 1 {
				owner = s.EnemyOwner
			}
			name := names[(k+tick/round)%len(names)]
			nlKill(s, st.unit(name, owner, x, z, 3), true)
		})
	}
	st.sentinels()
}

// ---------------------------------------------------------------- worksite

// nlWorksite is what each constructor raises: three sites round it, each in
// nanolathe reach of where it stands. The lead-in hides the small approach
// to the site's edge and the script's readiness wait before work starts.
var nlWorksite = [][3]string{
	{"armllt", "armrad", "armllt"},
	{"armrad", "armllt", "armrad"},
	{"armllt", "armrad", "armllt"},
}

// nlCrewRows is where the worksite's and the salvage scene's constructors
// stand: one above another, which suits the tall narrow column of a
// mutator compare.
var nlCrewRows = [3][2]int32{{-10, -95}, {10, 5}, {-10, 105}}

// nlStageWorksite stands the viewer's constructors in a column at the
// anchor, each with its three sites west, east and south of it. Every site
// and every constructor is reserved before anything is created, so no
// constructor stands on a site another is to build. Storage out of frame is
// topped up each second, so the build rate is the builders' own and never
// the economy's.
func nlStageWorksite(st *nlStage) {
	s := st.s
	cx, cz := st.cx, st.cz
	nlClearWorksite(st)
	st.storage()
	builder, ok := s.Catalog.Unit(st.name("armck"))
	if !ok {
		st.compatibleBattle()
		return
	}
	productsByRow := make([][3]string, len(nlWorksite))
	var productNames []string
	for i, row := range nlWorksite {
		for k, preferred := range row {
			productsByRow[i][k] = st.buildProduct(builder, preferred)
			productNames = append(productNames, productsByRow[i][k])
		}
	}
	rows := st.crewRows(builder, productNames)
	nlClear(s, cx-180, cz+rows[0][1]-100, cx+180, cz+rows[2][1]+190)
	for i, products := range productsByRow {
		bx, bz, _, ok := st.spot(builder, cx+rows[i][0], cz+rows[i][1], 4)
		if !ok {
			continue
		}
		type site struct {
			name string
			x, z int32
		}
		var sites []site
		// West, east and south of the constructor, a small gap from its own
		// footprint: well inside the nanolathe's reach [05 R-WORK-01 §2].
		for k, product := range products {
			pd, ok := s.Catalog.Unit(product)
			if !ok {
				continue
			}
			gx := builder.FootprintX*8 + pd.FootprintX*8 + 10
			gz := builder.FootprintZ*8 + pd.FootprintZ*8 + 10
			dx := [3]int32{-gx, gx, 0}[k]
			dz := [3]int32{0, 0, gz}[k]
			sx, sz, _, ok := st.spot(pd, bx+dx, bz+dz, 2)
			if !ok {
				fmt.Fprintf(os.Stderr, "nanolathe: preview: worksite has no ground for %s near %d,%d%s\n", product, bx+dx, bz+dz, nlRefusal(s, pd, bx+dx, bz+dz))
				continue
			}
			sites = append(sites, site{product, sx, sz})
		}
		u := st.create(builder, bx, bz)
		nlHold(u)
		for _, site := range sites {
			st.queueBuild(u, site.name, site.x, site.z)
		}
	}
	st.sentinels()
}

// nlClearWorksite clears the ground the worksite and salvage crews use of
// Greenhaven's trees before anything is placed, so every constructor, site
// and wreck stands in the open: the column of crews with their sites or
// wrecks either side, a margin round it, and more to the south, where a
// tree's sprite would rise over the bottom row.
func nlClearWorksite(st *nlStage) {
	nlClear(st.s, st.cx-130, st.cz-190, st.cx+130, st.cz+290)
}

// create makes one of the viewer's units at a spot already reserved.
func (st *nlStage) create(def *content.UnitDef, x, z int32) *units.Unit {
	s := st.s
	st.requireUnit(def.UnitName)
	fx, fz := nlFixed(x), nlFixed(z)
	h, err := s.Units.Create(def, s.LocalOwner, fx, s.World.HeightAt(fx, fz), fz)
	if err != nil {
		return nil
	}
	u := s.Units.Unit(h)
	if s.Movement != nil {
		s.Movement.EnsureUnit(u)
	}
	s.BindStagedOrderQueue(u)
	return u
}

// ----------------------------------------------------------------- salvage

// nlStageSalvage leaves the wrecks of a short fight at the anchor — enemy
// tanks destroyed on the first ticks — and has the viewer's constructors,
// standing in a column each between its own wrecks, reclaim them one after
// another. The viewer's metal starts empty with storage to spare, so the
// income reads.
func nlStageSalvage(st *nlStage) {
	s := st.s
	cx, cz := st.cx, st.cz
	nlClearWorksite(st)
	st.storage()
	builder, ok := s.Catalog.Unit(st.name("armck"))
	if !ok {
		st.compatibleBattle()
		return
	}
	type crew struct {
		b      *units.Unit
		wrecks []*units.Unit
		places [][2]int32
	}
	var crews []*crew
	for i, row := range nlCrewRows {
		bx, bz, _, ok := st.spot(builder, cx+row[0], cz+row[1], 4)
		if !ok {
			continue
		}
		c := &crew{}
		// A wreck either side, in reach of the constructor between them.
		for k, name := range []string{"correap", "corraid"} {
			if i == 1 {
				name = [2]string{"corthud", "corlevlr"}[k]
			}
			u := st.unit(name, s.EnemyOwner, bx+[2]int32{-50, 50}[k], bz, 2)
			if u != nil {
				c.wrecks = append(c.wrecks, u)
				c.places = append(c.places, [2]int32{int32(u.X.Int()), int32(u.Z.Int())})
			}
		}
		c.b = st.create(builder, bx, bz)
		nlHold(c.b)
		crews = append(crews, c)
	}
	st.at(2, func(s *session.Session, _ int) {
		for _, c := range crews {
			for _, u := range c.wrecks {
				nlKill(s, u, true)
			}
		}
		_ = s.EnqueueHumanCommand(session.HumanCommand{Kind: session.HumanSetResource,
			SetResource: session.HumanSetResourceCommand{Player: int(s.LocalOwner), Resource: economy.Metal, Amount: 0}})
	})
	// The wrecks exist once the deaths are resolved; each crew reclaims the
	// wreck at each of its places in turn. The tanks' deaths blast the
	// constructors beside them, so their health is set back to full: the
	// scene is about the salvage, and a hurt constructor would carry a
	// health bar.
	st.at(6, func(s *session.Session, _ int) {
		for _, c := range crews {
			if c.b == nil {
				continue
			}
			if c.b.Alive {
				c.b.Health = c.b.MaxHealth
			}
			for _, p := range c.places {
				f := nlWreckNear(s, p[0], p[1], 40)
				if f == nil {
					continue
				}
				fx, fz := world.CellToWorld(int32(f.CX)), world.CellToWorld(int32(f.CZ))
				nlPush(s, c.b, "Reclaim", 0, fx, s.World.HeightAt(fx, fz), fz)
			}
		}
	})
	st.sentinels()
}

// nlWreckNear is the reclaimable feature with metal whose anchor cell lies
// nearest (x, z) within radius, or nil.
func nlWreckNear(s *session.Session, x, z, radius int32) *features.Instance {
	var best *features.Instance
	bestD := int64(radius) * int64(radius)
	for _, f := range s.Features.Instances() {
		if f == nil || f.Def == nil || !f.Def.Reclaimable || f.Def.Metal <= 0 {
			continue
		}
		fx := int64(f.CX)*16 + int64(f.Def.FootprintX)*8
		fz := int64(f.CZ)*16 + int64(f.Def.FootprintZ)*8
		d := (fx-int64(x))*(fx-int64(x)) + (fz-int64(z))*(fz-int64(z))
		if d <= bestD {
			best, bestD = f, d
		}
	}
	return best
}

// ------------------------------------------------------------------- blast

// nlBlastGuns are the guns the blast scene may shell its block with, first
// that exists: stock plasma batteries whose weapon authors accuracy 0 and no
// spray, so a gun at full health fires exactly on its solved angles
// [06 R-WPN-03 §4], and whose range reaches the steep end of their 350-pixel-
// per-second arc. The Guardian (areaofeffect 64, 360 damage, a 90-tick
// reload) put its shells closest to the aim point.
var nlBlastGuns = []string{"armguard", "corpun"}

// nlBlastTank is what the block is made of: a light tank, so one shell's
// damage reads as a large share of its health bar and none dies of it.
const nlBlastTank = "corlevlr"

// nlBlastPitch is the block's spacing, world pixels: close enough that a
// shell's splash reaches the tanks round the one it lands on, and more of
// them as the blast-size factor grows.
const nlBlastPitch = 44

// nlStageBlast stands a block of five by three enemy tanks on flat, cleared
// ground with the middle one on the anchor, and one gun beyond the frame's
// west edge firing at a ground point just past that middle tank. The gun
// keeps the order, so a shell lands every reload; the lead-in is sized so
// the first lands under a second after the scene appears. After each hit
// the block's health is set back to full, so every shell reads on its own.
func nlStageBlast(st *nlStage) {
	s := st.s
	cx, cz := st.cx, st.cz
	gunNames := st.names(nlBlastGuns)
	if len(gunNames) == 0 {
		st.compatibleBattle()
		return
	}
	gunName := gunNames[0]
	gd, _ := s.Catalog.Unit(gunName)
	weapon := nlBatteryWeapon(gd)
	tankName := st.name(nlBlastTank)
	if st.roster != nil {
		// Choose from the authored health/damage values before any mutator,
		// so compared scenes contain the same block.
		ad, _ := st.roster.cat.Unit(gunName)
		tankName = st.requireUnit(st.roster.blastTarget(nlBlastTank, nlBatteryWeapon(ad)))
	}
	td, ok := s.Catalog.Unit(tankName)
	if !ok || weapon == nil {
		st.limit("This content has no compatible splash-artillery target block; an ordinary battle is shown.")
		st.compatibleBattle()
		return
	}
	half := max(int32(nlBlastPitch), max(td.FootprintX, td.FootprintZ)*16+12)
	rangeToTarget, lead := int32(nlBlastRange), int32(nlBlastLead)
	if gunName != "armguard" && gunName != "corpun" || weapon.Range < rangeToTarget {
		rangeToTarget, lead = min(int32(nlBlastRange), weapon.Range*3/4), 0
	}
	refillDelay := max(1, int(uint16(weapon.ReloadTime))/2)
	nlClear(s, cx-3*half, cz-2*half-40, cx+3*half, cz+2*half+110)
	var block []*units.Unit
	var mid *units.Unit
	for row := int32(-1); row <= 1; row++ {
		for col := int32(-2); col <= 2; col++ {
			u := st.unit(tankName, s.EnemyOwner, cx+col*half, cz+row*half, 1)
			if u == nil {
				continue
			}
			nlHold(u)
			// Hold fire too: their guns cannot reach the gun, and a target
			// they could not reach would still turn them about.
			u.Flags &^= units.StandingFieldMask << units.StandingFireShift
			block = append(block, u)
			if row == 0 && col == 0 {
				mid = u
			}
		}
	}
	if mid == nil {
		st.sentinels()
		return
	}
	// The anchor moves onto the middle tank, so the still camera and a
	// mutator compare's crops centre on it.
	st.cx, st.cz = int32(mid.X.Int()), int32(mid.Z.Int())
	var gun *units.Unit
	for _, name := range gunNames {
		if gun = st.unit(name, s.LocalOwner, st.cx-rangeToTarget+lead, st.cz, 6); gun != nil {
			break
		}
	}
	if gun != nil {
		tx, tz := mid.X+nlFixed(lead), mid.Z
		st.at(1, func(s *session.Session, _ int) {
			nlPush(s, gun, "Suppress", 0, tx, s.World.HeightAt(tx, tz), tz)
		})
	}
	// Refill the block's health a fixed time after a shell's damage shows.
	refill := 0
	st.every(1, 1, func(s *session.Session, tick int) {
		if refill > 0 && tick >= refill {
			refill = 0
			for _, u := range block {
				if u.Alive && !u.Dying {
					u.Health = u.MaxHealth
				}
			}
		}
		if refill == 0 {
			for _, u := range block {
				if u.Alive && u.Health < u.MaxHealth {
					refill = tick + refillDelay
					break
				}
			}
		}
	})
	st.sentinels()
}

// nlBlastLead is how far past the middle tank the gun aims, world pixels. A
// shell falls from the west at a slant and strikes the first thing its arc
// meets, so one aimed at the ground under the tank hits the tank's west side
// and splashes the west neighbour more than the east. Aimed this far past,
// it comes down through the top of the hull: over 25 shells from the stock
// Guardian at this range every one struck the middle tank's top, within 9
// world pixels of its centre across the line of fire and 12 along it (the
// muzzles alternate, so the arcs differ a little), and the middle tank took
// the most damage every time. Aiming 14 or more past sends some shells over
// the hull into the ground beyond.
const nlBlastLead = 9

// nlBlastRange is the gun's distance from its aim point, world pixels: past
// the frame's west edge at the scene's zoom, inside the Guardian's 1250 range
// and near the far end of its arc, where the shell comes down steeply enough
// to clear the tanks west of the middle one. A few pixels further and the
// arc no longer reaches: the gun never fires.
const nlBlastRange = 1080

// ------------------------------------------------------------------- metal

// nlMetalUnits are the metal scene's units, three rows of three: the
// hovercraft and tanks whose decks catch the most of the finish and the glint
// once they lie along a slope facing the key (DESIGN_GPU_RENDERER §29.1,
// §23.7). Tanks and hovercraft conform to the ground where kbots stand
// upright [04 R-MOV-01 §5a], so on a hillside whose normal is near the key's
// half-vector their whole decks face it, where on flat ground a deck is the
// anchored overhead face that neither switch changes.
//
// Measured, not guessed, with --nl-shot's compare measure: the share of the
// stage (right of the hero text, above the cards) whose colour the switch
// changes by eight levels or more, averaged over a second. The six small
// plated units this scene used to circle on flat Greenhaven at 4x changed
// 1.9% (finish) and 2.6% (glint). A census of the stock land units, turning,
// and structures on flat ground put the Anaconda and Swatter hovercraft near
// the top for both switches; six hovercraft there changed 2.5% and 2.8%. On
// this Great Divide hillside, whose average normal lies about ten degrees
// from the key, the same six changed 4.7% and 9.6%; this set of nine, packed
// three by three, 7.8% and 13.2% at 4x, and 10.5% and 18.2% a little closer
// (the surface at 0.45 of the screen, about 4.4x), where the glint's changed
// pixels are a mean 37 levels brighter and the finish's 10.
var nlMetalUnits = []string{"armanac", "armah", "corah", "corgol", "armmh", "armanac", "corah", "armah", "armanac"}

// nlStageMetal packs the metal units three by three about the anchor, on the
// hillside, each patrolling a small loop of its own so every hull keeps
// turning: the decks stay in the key's lobe while the sides sweep through it.
func nlStageMetal(st *nlStage) {
	s := st.s
	cx, cz := st.cx, st.cz
	nlClear(s, cx-160, cz-140, cx+160, cz+200)
	for i, name := range nlMetalUnits {
		x := cx - 70 + int32(i%3)*70
		z := cz - 58 + int32(i/3)*58
		u := st.unit(name, s.LocalOwner, x, z, 2)
		if u != nil {
			nlLoop(s, u, int32(u.X.Int()), int32(u.Z.Int()), 10, i)
		}
	}
	st.sentinels()
}

func wsAbs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// nlStageSentinel places one enemy structure far from the anchor, the nearest
// open ground to a point across the map from it.
func (st *nlStage) sentinel() {
	s, cx, cz := st.s, st.cx, st.cz
	def, ok := s.Catalog.Unit(st.name("corsolar"))
	if !ok {
		return
	}
	tx := s.World.PlayRight - cx
	tz := s.World.PlayBottom - cz
	for ring := int32(0); ring < 40; ring++ {
		for _, d := range [][2]int32{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
			x, z := tx+d[0]*ring*48, tz+d[1]*ring*48
			if x < 64 || z < 64 || x >= s.World.PlayRight-64 || z >= s.World.PlayBottom-64 {
				continue
			}
			if filmPlaceable(s, def, nlFixed(x), nlFixed(z)) {
				_, _ = s.Units.Create(def, s.EnemyOwner, nlFixed(x), s.World.HeightAt(nlFixed(x), nlFixed(z)), nlFixed(z))
				return
			}
		}
	}
}

// nlRefusal names why def may not stand at (x, z), for a diagnostic.
func nlRefusal(s *session.Session, def *content.UnitDef, x, z int32) string {
	if _, err := nlPlace(s, def, nlFixed(x), nlFixed(z)); err != nil {
		return ": " + err.Error()
	}
	return ": the spot is taken by the scene"
}
