package main

import (
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/platform/screenkit"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// The preview's nanolathe spray reproduces the battle's strip-6 emitter
// [03 R-P0-19-P][03 R-STRIP-01 §2]: one record per accepted work step, from the
// QueryNanoPiece origin into the target box narrowed per axis to its
// 4/11..7/11 span, five particles on each of its two spawn ticks, four world
// units of travel per tick and a palette nibble shimmering up 0xa1..0xa7.
// Positions are root-local model coordinates of the previewed unit, the frame
// the attachment's placement uses, so the spray turns with the orbit as one
// rigid body with the factory and its product (DESIGN_DEVELOPER_TOOLS §7).
//
// The random picks come from a private viewer generator, never the
// simulation or CRT stream: a battle's CRT position is part of its
// deterministic state [I4], and the preview has no battle.

const (
	unitViewerSprayParticles = 5   // per spawn tick [03 R-P0-19-P]
	unitViewerSprayRecords   = 401 // the strip bound: evict past 400 [03 R-STRIP-01 §1]
)

// unitViewerSprayRandom is the private generator's fixed seed, so a capture's
// spray is reproducible. Viewer policy, not a retail value.
const unitViewerSprayRandom = 0x6e616e6f

type unitViewerSpray struct {
	records []unitViewerSprayRecord
	random  *rand.Rand
}

type unitViewerSprayRecord struct {
	src, srcExtent, dst, dstExtent [3]numeric.Fixed
	windowEnd, nextSpawn           uint32
	particles                      []unitViewerParticle
}

type unitViewerParticle struct {
	pos, vel [3]numeric.Fixed
	expiry   uint32
	color    uint8
}

func newUnitViewerSpray() unitViewerSpray {
	return unitViewerSpray{random: rand.New(rand.NewPCG(unitViewerSprayRandom, 0))}
}

// pick is one draw in 0..0x7fff, the range of the battle's CRT value.
func (s *unitViewerSpray) pick() int64 { return int64(s.random.IntN(0x8000)) }

// emit creates one record for an accepted work step at tick and spawns its
// first five particles at once; its window closes one tick later
// [03 R-P0-19-P]. The source is a point; the target is a box.
func (s *unitViewerSpray) emit(tick uint32, src, dstMin, dstMax [3]numeric.Fixed) {
	if len(s.records) >= unitViewerSprayRecords {
		// The pre-insert count past 400 evicts the oldest record.
		s.records = append(s.records[:0], s.records[1:]...)
	}
	r := unitViewerSprayRecord{windowEnd: tick + 1, nextSpawn: tick + 1}
	r.src, r.srcExtent = unitViewerNarrow(src, src)
	r.dst, r.dstExtent = unitViewerNarrow(dstMin, dstMax)
	s.spawn(&r, tick)
	s.records = append(s.records, r)
}

// update is the strip pass after the unit visit: a record whose list is empty
// is removed before its update; otherwise every particle advances, its colour
// steps up the ramp, expired particles drop, and the spawn gate fires once at
// the window's end [03 R-STRIP-01 §2].
func (s *unitViewerSpray) update(tick uint32) {
	kept := s.records[:0]
	for _, r := range s.records {
		if len(r.particles) == 0 {
			continue
		}
		live := r.particles[:0]
		for _, p := range r.particles {
			for axis := range p.pos {
				p.pos[axis] = p.pos[axis].Add(p.vel[axis])
			}
			n := p.color&0x0f + 1
			if n > 7 {
				n = 1
			}
			p.color = 0xa0 | n
			if tick > p.expiry {
				continue
			}
			live = append(live, p)
		}
		r.particles = live
		if r.nextSpawn <= tick && r.nextSpawn <= r.windowEnd {
			s.spawn(&r, tick)
			r.nextSpawn++
		}
		kept = append(kept, r)
	}
	clear(s.records[len(kept):])
	s.records = kept
}

// spawn adds five particles: six picks each, a source point then a landing
// point, each origin + pick×extent/0x8000. The lifetime is trunc(distance/4)
// ticks; a zero-length hop is discarded. The colour nibble starts at
// 1 + (index mod 7) [03 R-P0-19-P].
func (s *unitViewerSpray) spawn(r *unitViewerSprayRecord, tick uint32) {
	for i := range unitViewerSprayParticles {
		var from, to [3]numeric.Fixed
		for axis := range from {
			from[axis] = r.src[axis] + numeric.Fixed(r.srcExtent[axis].Raw()*s.pick()/0x8000)
		}
		for axis := range to {
			to[axis] = r.dst[axis] + numeric.Fixed(r.dstExtent[axis].Raw()*s.pick()/0x8000)
		}
		life := unitViewerSprayLife(from, to)
		if life <= 0 {
			continue
		}
		p := unitViewerParticle{pos: from, expiry: tick + uint32(life), color: 0xa0 | uint8(1+i%7)}
		for axis := range p.vel {
			p.vel[axis], _ = to[axis].Sub(from[axis]).Div(numeric.FixedFromInt(int64(life)))
		}
		r.particles = append(r.particles, p)
	}
}

// unitViewerSprayLife is trunc(distance/4) in whole ticks: four world units
// of travel per tick [03 R-P0-19-P]. Presentation-only float.
func unitViewerSprayLife(a, b [3]numeric.Fixed) int32 {
	var sum float64
	for axis := range a {
		d := float64(b[axis].Raw()-a[axis].Raw()) / 65536
		sum += d * d
	}
	return int32(math.Sqrt(sum)) / 4
}

// unitViewerNarrow narrows a box per axis to its 4/11..7/11 span, as origin
// and extent [03 R-P0-19-P].
func unitViewerNarrow(lo, hi [3]numeric.Fixed) (origin, extent [3]numeric.Fixed) {
	for axis := range lo {
		d := hi[axis].Raw() - lo[axis].Raw()
		origin[axis] = lo[axis] + numeric.Fixed(d*4/11)
		extent[axis] = numeric.Fixed(lo[axis].Raw()+d*7/11) - origin[axis]
	}
	return origin, extent
}

// unitViewerRootLocal is a piece origin in the root piece's own frame under
// poses: the hierarchy below the root composed with the root itself at rest,
// the arithmetic PiecePlacement uses for a pad (DESIGN_GPU_RENDERER §22.5).
// The root piece is the frame's origin.
func unitViewerRootLocal(mdl *model.Model, poses []frame.PieceView, piece int) ([3]numeric.Fixed, bool) {
	if mdl == nil || piece < 0 || piece >= len(mdl.Pieces) || mdl.Root < 0 || mdl.Root >= len(mdl.Pieces) {
		return [3]numeric.Fixed{}, false
	}
	root := mdl.Root
	if piece == root {
		return [3]numeric.Fixed{}, true
	}
	states := make([]model.PieceState, len(mdl.Pieces))
	for _, p := range poses {
		if p.Index >= 0 && p.Index < len(states) && p.Index != root {
			states[p.Index] = model.PieceState{RotX: p.RotX, RotY: p.RotY, RotZ: p.RotZ, Trans: [3]numeric.Fixed{p.Tx, p.Ty, p.Tz}}
		}
	}
	origin := model.Compose(mdl, states, piece).Origin
	t := mdl.Pieces[root].Translate
	return [3]numeric.Fixed{origin[0].Sub(t[0]), origin[1].Sub(t[1]), origin[2].Sub(t[2])}, true
}

// unitViewerTargetBox is a unit target's box about a root-local point: the
// definition's footprint and model-top extents [05 R-WORK-01 §8]. Those are
// world-space extents; model space mirrors Z [03 R-RAST-01 §2], so the Z
// bounds swap sign.
func unitViewerTargetBox(def *content.UnitDef, at [3]numeric.Fixed) (lo, hi [3]numeric.Fixed) {
	mn, mx := def.BoundingExtents()
	mn[2], mx[2] = -mx[2], -mn[2]
	for axis := range at {
		lo[axis] = at[axis] + numeric.Fixed(mn[axis])
		hi[axis] = at[axis] + numeric.Fixed(mx[axis])
	}
	return lo, hi
}

// unitViewerMobileTarget is the mobile Build preview's stand-in work target:
// on the ground one fit radius from the builder's origin along the 45-degree
// StartBuilding bearing, the direction the builder's script turns to face.
// Heading zero faces model +Z, and a turn by the bearing about Y maps it to
// (-sin, 0, cos). Its box is the builder's own footprint and model height, a
// product of the builder's size. Viewer policy (DESIGN_DEVELOPER_TOOLS §7).
func unitViewerMobileTarget(def *content.UnitDef, reach float64) (lo, hi [3]numeric.Fixed) {
	sin, cos := math.Sincos(float64(unitViewerBuildHeading) * 2 * math.Pi / 65536)
	at := [3]numeric.Fixed{numeric.Fixed(math.Round(-sin * reach * 65536)), 0, numeric.Fixed(math.Round(cos * reach * 65536))}
	return unitViewerTargetBox(def, at)
}

// drawSpray paints the live particles over the stage. Each is projected
// through the last record's projection: the root piece's current state with
// the view orientation folded in, the pivot and the raster scale, exactly as
// the model's own vertices are [03 §2.5] (DESIGN_GPU_RENDERER §22.5). A mark
// is the battle's two-by-two pixel square [03 R-P0-19-P] with its pixels at
// the preview's magnification: a battle pixel is one world unit, so a mark
// spans two world units of the projection, from the particle's point right
// and down. That coverage is what makes the stream read as a spray. The
// spray draws over the model with no depth test, as strip 6 draws over
// grounded units and structures.
func (m *unitViewerModel) drawSpray(dst *ebiten.Image, stage screenkit.Rect) {
	v := m.view
	if !v.ok || m.anim == nil || m.geometry == nil || m.palette == nil || v.w <= 0 || v.h <= 0 || len(m.anim.spray.records) == 0 {
		return
	}
	project, ok := m.rootLocalProjector()
	if !ok {
		return
	}
	sx, sy := stage.W/float64(v.w), stage.H/float64(v.h)
	side := max(1, 2*v.projection.PixelsPerUnit*sx)
	for _, r := range m.anim.spray.records {
		for _, p := range r.particles {
			x, y := project(p.pos)
			if math.IsNaN(x) || math.IsNaN(y) {
				continue
			}
			red, green, blue, _ := m.palette.RGBA(p.color)
			screenkit.Fill(dst, screenkit.Rect{X: stage.X + x*sx, Y: stage.Y + y*sy, W: side, H: side}, color.RGBA{red, green, blue, 255})
		}
	}
}

// rootLocalProjector maps a root-local point to the last record's canvas
// pixels: the root piece's current state with the view orientation folded in
// [03 §2.4] C24, then the preview projection with its model Z mirror and
// half-height shear about the pivot, anchored at the canvas centre
// (DESIGN_GPU_RENDERER §22.5). Piece origins map where ProjectedPieces puts
// them.
func (m *unitViewerModel) rootLocalProjector() (func([3]numeric.Fixed) (float64, float64), bool) {
	v := m.view
	if !v.ok || m.geometry == nil || v.w <= 0 || v.h <= 0 {
		return nil, false
	}
	root := m.geometry.Root
	if root < 0 || root >= len(m.geometry.Pieces) {
		return nil, false
	}
	states := make([]model.PieceState, root+1)
	for _, p := range m.poses {
		if p.Index == root {
			states[root] = model.PieceState{RotX: p.RotX, RotY: p.RotY, RotZ: p.RotZ, Trans: [3]numeric.Fixed{p.Tx, p.Ty, p.Tz}}
		}
	}
	model.FoldRootAngles(states, root, v.heading, v.tilt, v.bank)
	transform := model.Compose(m.geometry, states, root)
	ppu, pivot := v.projection.PixelsPerUnit, v.projection.Pivot
	ax, ay := float64(v.w/2), float64(v.h/2)
	return func(p [3]numeric.Fixed) (float64, float64) {
		q := transform.Apply(p)
		x := float64(q[0]-pivot[0])/65536*ppu + ax
		y := (-float64(q[2]-pivot[2])-float64(q[1]-pivot[1])/2)/65536*ppu + ay
		return x, y
	}, true
}
