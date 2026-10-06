package survival

import (
	"github.com/nanolathe-gg/nanolathe/internal/aikit"
	"github.com/nanolathe-gg/nanolathe/internal/aikit/core"
)

// The tower ring. The survivor keeps TowerShare percent of what it has
// received as towers of its own planning, spread over the sectors it owns
// by their weight (every owned sector, more where a wave is warned or
// attackers have come from). The next tower goes to the sector furthest
// behind its share, just beyond the survivor's outermost building on that
// bearing — or, on the human's side, beyond the human's room — and is the
// tower the builder can make that buys the most ground firepower for its
// cost at the current income, an anti-air tower when aircraft threaten.
// Short wall segments go in front of completed ground towers that cover them.
// These are Modern AI decisions (DESIGN_SURVIVAL §16.8).

type defense struct {
	// failed counts recent placement failures per sector; a failed site
	// moves out a step each time and the count fades.
	failed                 [numSectors]int32
	failAt                 [numSectors]uint32
	wallAt                 [numSectors]uint32 // tick of the last wall segment ordered
	wallN                  [numSectors]int32  // segments ordered per sector
	wallFootX, wallFootZ   int32              // the selected wall's finished footprint
	wallBuildX, wallBuildZ int32              // construction-grid centre alignment
}

// towerStart is the tick before which the survivor plans no towers of its
// own: the opening belongs to the economy (the first waves are a few
// units, which the commander and the first army meet).
const towerStart = 2 * 1800

// towerWant sets each owned sector's wanted tower value for this think.
func (st *state) towerWant() int64 {
	total := st.income * int64(st.p.TowerShare) / 100
	// Full stores are resources nobody is spending — on a poor or cramped
	// map the economy may have nothing it can place — and a full store
	// wastes the wave rewards: what stands above half of both stores is
	// owed as towers too.
	if m, e := &st.stock.metal, &st.stock.energy; m.Stock*2 >= m.Cap && e.Stock*2 >= e.Cap {
		total += int64(m.Stock-m.Cap/2) + int64(e.Stock-e.Cap/2)/aikit.EnergyPerMetal
	}
	if st.tick < towerStart {
		total = 0
	}
	// The total is spread by weight, so a warned direction draws the next
	// towers to its sectors ahead of its arrival.
	var wsum int64
	for s := range st.weight {
		wsum += st.weight[s]
	}
	for s := range st.want {
		st.want[s] = 0
		if wsum > 0 {
			st.want[s] = total * st.weight[s] / wsum
		}
	}
	return total
}

// nextTower picks the sector most behind its share, and the tower and site
// for builder u, once the owned sectors together are at least two thirds
// of a tower behind. ok is false when nothing is owed or u builds no tower.
func (st *state) nextTower(b *core.Board, u *aikit.OwnUnit) (sector int, prod *aikit.UnitInfo, x, z int32, ok bool) {
	def, owed := st.deficits()
	if owed <= 0 {
		return -1, nil, 0, 0, false
	}
	// The sectors most behind first; one whose site will not do this think
	// gives way to the next.
	var tried [numSectors]bool
	for range numSectors {
		best := -1
		for s := range def {
			if st.mine[s] && st.siteable[s] && !tried[s] && (best < 0 || def[s] > def[best]) {
				best = s
			}
		}
		if best < 0 || def[best] <= 0 {
			break
		}
		tried[best] = true
		p := st.pickTowerBudget(b, u.Info, false, owed*3/2)
		if st.air && st.towersAA(best) == 0 && st.towers[best] > 0 {
			if aa := st.pickTowerBudget(b, u.Info, true, owed*3/2); aa != nil {
				p = aa
			}
		}
		if p == nil {
			continue
		}
		if x, z, ok = st.towerSite(b, best, p); ok {
			return best, p, x, z, true
		}
		st.defense.failed[best] = min(st.defense.failed[best]+1, 4)
		st.defense.failAt[best] = st.tick
	}
	return -1, nil, 0, 0, false
}

// deficits is the tower value each owned sector with ground to build on
// is behind its share — its own towers standing, framed or ordered, and
// the allied towers on its bearing, which already cover that lane
// (allyCover) — and their sum.
func (st *state) deficits() (def [numSectors]int64, owed int64) {
	for s := range st.want {
		if !st.mine[s] || !st.siteable[s] {
			continue
		}
		def[s] = st.want[s] - st.have[s] - st.defense.pending(st, s)
		def[s] -= st.allyCover(s, def[s])
		owed += def[s]
	}
	return def, owed
}

// towersAA counts own anti-air towers in sector s this think.
func (st *state) towersAA(s int) int32 {
	return st.aaTowers[s]
}

// towerSite is the point a sector's next tower is requested at: on the
// sector's bearing, perimeterAhead beyond its outermost building of ours
// (at least beyond the human's room), shifted sideways for every other
// tower so a sector's towers form a short line across the approach rather
// than a column, and moved in or out a step for each recent failure there.
// The point must be ground this survivor's builders reach from its start;
// ok is false when none is found along the bearing.
func (st *state) towerSite(b *core.Board, s int, prod *aikit.UnitInfo) (int32, int32, bool) {
	n := int64(st.towers[s] + st.defense.pendingN(st, s))
	lat := (n + 1) / 2 * 72
	if n%2 == 1 {
		lat = -lat
	}
	var fx, fz int32
	if prod != nil {
		fx, fz = prod.FootX, prod.FootZ
	}
	return st.siteOnBearingFor(b, s, max(int64(st.radius[s]), towerRoom)+failStep(st.defense.failed[s]), lat, fx, fz)
}

// towerRoom is the nearest to the site a tower or wall is planned. A wave
// goes for the building nearest it, so a tower drawn close in — on the
// human's side, or pulled in along a narrow valley — brings the fight to
// the human's commander; out here the fight stays clear of it.
const towerRoom = 640

// failStep is the radial shift after f recent failures: out, in, further
// out, further in.
func failStep(f int32) int64 {
	step := int64((f+1)/2) * 96
	if f%2 == 0 {
		step = -step
	}
	return step
}

// siteOnBearing is the point r out from the site on sector s's bearing and
// lat to its side, pulled in step by step until it stands on the home
// region — never nearer the site than towerRoom; ok is false when it never
// does. A point beside an allied building or in an allied factory's exit
// lane (allies.go) is moved round it or out in front of it (allySteps)
// when that clears it; otherwise a point beside an ordinary building stands
// (the placement search keeps the footprints apart — on a cramped map the
// team's buildings fill the ring's ground, and a ring given up there left
// the human's side bare), and one in a lane is pulled in further. Allied
// defensive tower reservations are absolute (DESIGN_SURVIVAL §16.8).
func (st *state) siteOnBearing(b *core.Board, s int, r, lat int64) (int32, int32, bool) {
	return st.siteOnBearingFor(b, s, r, lat, 0, 0)
}

func (st *state) siteOnBearingFor(b *core.Board, s int, r, lat int64, fx, fz int32) (int32, int32, bool) {
	m := b.K.Map
	dx, dz := sectorDir[s][0], sectorDir[s][1]
	at := func(r, l int64) (int32, int32) {
		return clampWorld(int64(st.cx)+dx*r/1000-dz*l/1000, m.WorldW), clampWorld(int64(st.cz)+dz*r/1000+dx*l/1000, m.WorldH)
	}
	for ; r >= towerRoom; r -= 64 {
		x, z := at(r, lat)
		if !st.reachable(x, z) {
			continue
		}
		if st.clearOfAllies(x, z) && st.clearOfAllyTowers(b, x, z, fx, fz) {
			return x, z, true
		}
		for _, sd := range allySteps {
			x, z := at(r+sd[0], lat+sd[1])
			if st.reachable(x, z) && st.clearOfAllies(x, z) && st.clearOfAllyTowers(b, x, z, fx, fz) {
				return x, z, true
			}
		}
		x, z = at(r, lat)
		if st.outOfLanes(x, z) && st.clearOfAllyTowers(b, x, z, fx, fz) {
			return x, z, true
		}
	}
	return 0, 0, false
}

// clearOfAllyTowers tests the full candidate footprint against each observed
// allied tower's reservation: 128 wu beyond its footprint, and its outward
// firing lane. The executor repeats this on the final searched site.
func (st *state) clearOfAllyTowers(b *core.Board, x, z, fx, fz int32) bool {
	if b.O == nil {
		return true
	}
	hx, hz := int64(max(fx, 0))*8, int64(max(fz, 0))*8
	for i := range b.O.Allies {
		u := &b.O.Allies[i]
		if u.Info == nil || !u.Info.Role.Has(aikit.RoleDefense) || u.Info.Role.Has(aikit.RoleMobile) {
			continue
		}
		vx, vz := int64(x-u.X), int64(z-u.Z)
		if absI(vx) < int64(u.Info.FootX)*8+128+hx && absI(vz) < int64(u.Info.FootZ)*8+128+hz {
			return false
		}
		dx, dz := int64(u.X-st.cx), int64(u.Z-st.cz)
		r := aikit.ISqrt64(dx*dx + dz*dz)
		if r == 0 {
			continue
		}
		dx, dz = dx*1000/r, dz*1000/r
		forward, side := vx*dx+vz*dz, vx*dz-vz*dx
		along, across := absI(dx)*hx+absI(dz)*hz, absI(dz)*hx+absI(dx)*hz
		tx, tz := int64(max(u.Info.FootX, 1))*8, int64(max(u.Info.FootZ, 1))*8
		lane := absI(dx)*tx + absI(dz)*tz + 256*1000
		width := absI(dz)*tx + absI(dx)*tz + 64*1000
		if forward+along > 0 && forward-along < lane && absI(side) < width+across {
			return false
		}
	}
	return true
}

func absI(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// allySteps are the shifts, world units outward and sideways, a site tries
// when an allied building or factory lane covers it: to either side, then
// out in front.
var allySteps = [...][2]int64{{0, 96}, {0, -96}, {0, 192}, {0, -192}, {64, 0}, {128, 0}, {192, 0}, {256, 0}, {128, 96}, {128, -96}, {256, 96}, {256, -96}}

// reachable reports whether (x, z) is on the ground this survivor's start
// stands on, for its commander's movement class (true when that is not
// known).
func (st *state) reachable(x, z int32) bool {
	if st.reach == nil || st.homeRegion == 0 {
		return true
	}
	return st.reach.At(x, z) == st.homeRegion
}

// pickTowerBudget is the tower builder can make that buys the most firepower
// against ground units (anti-air: against aircraft) per cost, with heavier
// and longer-ranged towers taking over as income grows; nil when it can
// make none it can afford. It also keeps a larger upgrade from postponing coverage when
// the configured tower share is only owed enough for a smaller tower.
func (st *state) pickTowerBudget(b *core.Board, builder *aikit.UnitInfo, aa bool, budget int64) *aikit.UnitInfo {
	inc := int64(b.Metal.Income) + int64(b.Energy.Income)/aikit.EnergyPerMetal
	// Income sets the reference cost; feasibility below admits upgrades as
	// the economy grows, without increasing the configured tower share.
	vref := 150 + 25*inc
	// Affordability: no tower dearer than a minute and a half of income or
	// what the stores hold, whichever is more (at least a light tower's
	// worth).
	limit := min(budget, max(400, inc*90, int64(b.Metal.Stock)+int64(b.Energy.Stock)/aikit.EnergyPerMetal))
	mLimit := max(int64(b.Metal.Stock), int64(b.Metal.Income)*90)
	eLimit := max(int64(b.Energy.Stock), int64(b.Energy.Income)*90)
	var best *aikit.UnitInfo
	var bestScore int64
	for _, p := range builder.Builds {
		d := st.tab.of(p)
		if aa && !d.aa || !aa && !d.tower || st.allowance(b.K, p) <= 0 {
			continue
		}
		v := int64(p.Value)
		if v <= 0 || v > limit || int64(p.Metal) > mLimit || int64(p.Energy) > eLimit {
			continue
		}
		dps, reach := int64(d.gdps), int64(d.grange)
		if aa {
			dps = int64(p.AirDPS)
			reach = int64(p.Range)
		}
		// One blended cost penalty preserves affordable heavy upgrades.
		// Dividing by cost twice kept light towers winning even after the
		// economy could support better coverage (DESIGN_SURVIVAL §16.8).
		rng := clampI(reach, 200, 1200)
		score := dps * int64(p.HP) / (v + vref) * (600 + rng)
		if best == nil || score > bestScore {
			best, bestScore = p, score
		}
	}
	return best
}

// nextWall picks a sector owed a wall segment: one with a completed ground
// tower covering its pieces, warned or attacked, whose last segment was
// ordered long enough ago, and the wall piece builder can make. The segment crosses the bearing
// wallAhead beyond the towers' line, three pieces wide, so every
// neighbouring sector's approach stays open (the corridors).
func (st *state) nextWall(b *core.Board, u *aikit.OwnUnit) (sector int, prod *aikit.UnitInfo, ok bool) {
	if st.p.Walls == 0 {
		return -1, nil, false
	}
	wp := st.wallPiece(u.Info)
	if wp == nil || int64(wp.Metal)*wallPieces > max(int64(b.Metal.Stock), int64(b.Metal.Income)*90) ||
		int64(wp.Energy)*wallPieces > max(int64(b.Energy.Stock), int64(b.Energy.Income)*90) {
		return -1, nil, false
	}
	st.defense.wallFootX = max(wp.FootX, wp.FinishedFeature.FootprintX)
	st.defense.wallFootZ = max(wp.FootZ, wp.FinishedFeature.FootprintZ)
	st.defense.wallBuildX, st.defense.wallBuildZ = wp.FootX, wp.FootZ
	best := -1
	var bestW int64
	var pts [wallPieces][2]int32
	for s := range st.weight {
		if !st.mine[s] || !st.siteable[s] || st.defense.wallN[s] >= maxWallSegments {
			continue
		}
		if st.defense.wallAt[s] != 0 && st.tick-st.defense.wallAt[s] < wallEvery {
			continue
		}
		if st.defense.wallN[s] >= (st.groundTowers(b, s)+1)/2 {
			continue
		}
		if w := st.weight[s]; w > 1000 && w > bestW && len(st.wallSite(b, s, pts[:0])) > 0 {
			best, bestW = s, w
		}
	}
	if best < 0 {
		return -1, nil, false
	}
	return best, wp, true
}

// wallPiece is the first wall piece builder makes whose restriction cap
// leaves an allowance (caps.go), nil when none.
func (st *state) wallPiece(builder *aikit.UnitInfo) *aikit.UnitInfo {
	for _, p := range builder.Builds {
		if st.tab.of(p).wall && st.allowance(st.kit, p) > 0 {
			return p
		}
	}
	return nil
}

// Walls: a segment every wallEvery per sector at most, maxWallSegments in
// all per sector, wallAhead beyond the towers' line, wallPieces pieces
// across; a second segment stands a row further out, offset sideways.
const (
	wallEvery       = 2700
	maxWallSegments = 3
	wallAhead       = 128
	wallPieces      = 3
	wallCorridor    = 64
)

// wallSite returns the points of sector s's next wall segment's pieces,
// none when the segment's centre is not on home ground.
func (st *state) wallSite(b *core.Board, s int, dst [][2]int32) [][2]int32 {
	m := b.K.Map
	n := int64(st.defense.wallN[s])
	r := max(int64(st.radius[s]), towerRoom) + wallAhead + n*64
	fx, fz := max(st.defense.wallFootX, 1), max(st.defense.wallFootZ, 1)
	bx, bz := max(st.defense.wallBuildX, 1), max(st.defense.wallBuildZ, 1)
	width := int64(max(fx, fz)) * 16
	dx, dz := sectorDir[s][0], sectorDir[s][1]
	axis := max(absI(dx), absI(dz))
	spacing := (width*1000 + axis - 1) / axis
	across := (absI(dz)*int64(fx)*8 + absI(dx)*int64(fz)*8 + 999) / 1000
	along := (absI(dx)*int64(fx)*8 + absI(dz)*int64(fz)*8 + 999) / 1000
	shift := (n%2*2 - 1) * n * 48 // later rows stagger sideways
	// Limit each segment to its sector's middle, leaving a 64 wu corridor
	// between neighbouring sectors even when later rows shift toward it.
	half := spacing*(wallPieces-1)/2 + across
	// Leave room for a cell's diagonal snap before choosing the row's offset.
	// The final per-piece test below still owns the corridor boundary.
	sideMax := (r-along)*sin1000(sectorAngle/2)/cos1000(sectorAngle/2) - wallCorridor/2 - half - 24
	if sideMax < 0 {
		return dst
	}
	shift = clampI(shift, -sideMax, sideMax)
	cx, cz, ok := st.siteOnBearingFor(b, s, r, shift, fx, fz)
	if !ok {
		return dst
	}
	for k := int64(0); k < wallPieces; k++ {
		lat := (k - (wallPieces-1)/2) * spacing
		x := int32(int64(cx) - dz*lat/1000)
		z := int32(int64(cz) + dx*lat/1000)
		// Validate the centre the ordinary executor will actually use, so
		// grid alignment cannot narrow the planned 64 wu corridor (§16.8).
		x = (x/16-bx/2)*16 + bx*8
		z = (z/16-bz/2)*16 + bz*8
		// Completion anchors the finished feature at the unit's cell origin.
		// Check the union of both footprints there, rather than assuming the
		// larger finished feature is centred on the construction unit.
		ux, uz := x-bx*8+fx*8, z-bz*8+fz*8
		vx, vz := int64(ux-st.cx), int64(uz-st.cz)
		ahead, side := (vx*dx+vz*dz)/1000, (vz*dx-vx*dz)/1000
		if ux >= 64 && ux <= m.WorldW-64 && uz >= 64 && uz <= m.WorldH-64 &&
			absI(side)+across <= (ahead-along)*sin1000(sectorAngle/2)/cos1000(sectorAngle/2)-wallCorridor/2 &&
			st.reachable(x, z) && st.clearOfAllies(ux, uz) && st.clearOfAllyTowers(b, ux, uz, fx, fz) && st.wallCovered(b, ux, uz) {
			dst = append(dst, [2]int32{x, z})
		}
	}
	return dst
}

// groundTowers counts completed ground towers on the sector's ring ground;
// anti-air towers and unfinished frames cannot yet support a wall segment.
func (st *state) groundTowers(b *core.Board, s int) int32 {
	var n int32
	count := func(info *aikit.UnitInfo, x, z int32, built bool) {
		vx, vz := int64(x-st.cx), int64(z-st.cz)
		if built && st.tab.of(info).tower && vx*vx+vz*vz >= allyCoverMin*allyCoverMin && sectorOf(vx, vz) == s {
			n++
		}
	}
	for i := range b.O.Own {
		u := &b.O.Own[i]
		count(u.Info, u.X, u.Z, u.Built)
	}
	for i := range b.O.Allies {
		u := &b.O.Allies[i]
		count(u.Info, u.X, u.Z, u.Built)
	}
	return n
}

func (st *state) wallCovered(b *core.Board, x, z int32) bool {
	covered := func(info *aikit.UnitInfo, tx, tz int32, built bool) bool {
		d := st.tab.of(info)
		return built && d.tower && d.grange > 0 && aikit.Dist2(x, z, tx, tz) <= int64(d.grange)*int64(d.grange)
	}
	for i := range b.O.Own {
		u := &b.O.Own[i]
		if covered(u.Info, u.X, u.Z, u.Built) {
			return true
		}
	}
	for i := range b.O.Allies {
		u := &b.O.Allies[i]
		if covered(u.Info, u.X, u.Z, u.Built) {
			return true
		}
	}
	return false
}

// pending is the tower value ordered in sector s whose frame does not
// stand yet.
func (d *defense) pending(st *state, s int) int64 {
	var v int64
	for i := range st.jobs.list {
		j := &st.jobs.list[i]
		if j.kind == jobTower && j.sector == s && !j.framed && j.prod != nil {
			v += int64(j.prod.Value)
		}
	}
	return v
}

// pendingN counts the towers ordered in sector s whose frame does not
// stand yet.
func (d *defense) pendingN(st *state, s int) int32 {
	var n int32
	for i := range st.jobs.list {
		j := &st.jobs.list[i]
		if j.kind == jobTower && j.sector == s && !j.framed {
			n++
		}
	}
	return n
}

// fade forgets placement failures a minute after the last one.
func (d *defense) fade(tick uint32) {
	for s := range d.failed {
		if d.failed[s] > 0 && tick-d.failAt[s] > 1800 {
			d.failed[s]--
			d.failAt[s] = tick
		}
	}
}
