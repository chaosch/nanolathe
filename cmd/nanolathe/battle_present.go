package main

import (
	"math"
	"time"
)

// The asynchronous window's presentation clock (docs/DESIGN_GPU_RENDERER.md
// §13.5, §13.13).
//
// The window presents one continuous world time T, in ticks, that trails the
// tick budget's own clock by a lag:
//
//	T(t) = F + carry + (t − t_F) × rate − lag − margin × rate
//
// F is the global tick after the last host step's release, carry the budget's
// remainder just after it [01 §4.2] and t_F that step's IDEAL host millisecond,
// the instant the window's 30 Hz host clock meant it for; rate is 30 × the
// effective speed (active × 0.1 [01 §4.2]) ticks per second. Every prepared
// host step rebases F + carry, including one that released nothing, so a speed
// change moves the rate from the step it took effect. The presented pair is
// the tick after floor(T) and the tick before it, blended at T's fraction, so
// every presented frame moves the world by the wall time since the last one,
// at whatever speed and however many ticks a host step releases.
//
// A step's body runs when the window gets to it, which trails the ideal
// instant by anything from nothing to a refresh or two: a step lands on the
// Update nearest its instant, and a modern window runs the body at a Draw's
// tail, a refresh later again when a cap skips the Draw in between (§13.10).
// Rebased at the body, every change in that lateness moved T by the same
// amount — at 60 FPS on a 120 Hz panel a dropped refresh swapped the bodies
// between the two Draws of a present interval, and the world held for a
// refresh and then leapt one, over and over while the panel dropped refreshes.
// Rebased at the ideal instant, T is a function of wall time alone. What the
// lateness still decides is when the batch T needs is joined, so the margin
// holds T back by the lateness recently seen (presentMarginCeiling).
//
// Measured from one tick instead, as the blend used to be, a host step that
// released two ticks presented only the second: at 2x the world eased across
// the older tick in half a step, held for the other half, and leapt the tick
// it never showed — a third of a tick, then one and two-thirds, frame after
// frame. Any speed above 1x did a version of the same.
type presentClock struct {
	// base is F + carry at the last prepared host step, and baseMS that step's
	// host millisecond. lag is the lag at the same instant; it eases down
	// toward a smaller target (presentLagEase) and jumps up to a larger one.
	base   float64
	baseMS float64
	lag    float64
	set    bool
	// margin is the lateness margin in host milliseconds as of marginMS, the
	// host millisecond of the body that set it; it releases from there
	// (presentMarginAt).
	margin   float64
	marginMS float64
	// floor is the last position a Draw presented: the clock never presents
	// earlier than that, so a lag that grows with the speed holds the world
	// still until the clock reaches it again rather than replaying ticks.
	floor    float64
	floorSet bool
	// tick and fraction are the last sample a Draw presented, returned again
	// while the battle is paused so the blend stays frozen.
	tick     uint32
	fraction float32
	sampled  bool
}

// presentLagEase is how fast, in ticks per host millisecond, a lag larger than
// its speed needs shrinks back to it: three ticks a second, so the world runs a
// tenth fast for a moment after a speed drop instead of skipping ticks.
const presentLagEase = 3.0 / 1000

// presentMarginCeiling bounds the lateness margin, in host milliseconds, at one
// 60 Hz refresh. The lateness a running window produces is at most about that:
// a step taken up to a refresh either side of its instant, and a body deferred
// past a cap-skipped Draw. Anything later is a stall, which no margin short of
// its own length would hide, and which must not leave the world lagging a
// stall behind for seconds afterwards.
//
// presentMarginRelease is how fast, in milliseconds per host millisecond, the
// margin gives back lateness that has stopped recurring: a refresh's worth in
// two seconds, so the world runs under one percent fast while it does. Dropped
// refreshes come in bursts: replaying a traced fullscreen session's timing, a
// margin released at the lag's own rate (presentLagEase) left twice the world
// motion errors over 4 ms that this one does (95 against 49 in two minutes).
const (
	presentMarginCeiling = 1000.0 / 60
	presentMarginRelease = 1.0 / 120
)

// presentationLag is the smallest lag, in ticks, at which T never passes the
// newest tick the host has joined.
//
// A host step joins the batch the previous step released, so during a step
// that released K ticks the newest presentable tick is F − K. Across that step
// the budget clock runs from B = F + carry to B + k, k being the ticks the
// budget accrues per step (active / 10), and T must stay at or below F − K =
// floor(B − k). That holds when lag ≥ 2k + the carry the previous step kept.
// A speed's carries are multiples of gcd(active, 10) / 10, so the largest is
// 1 − gcd(active, 10) / 10 and the lag is 2k plus that. At the nominal speed
// it is exactly two ticks with no carry — the pair the window has always
// presented — and at 2x it is four: the two ticks each step releases are then
// shown one per 60 Hz frame.
func presentationLag(active int32) float64 {
	active = min(max(active, 1), 20)
	g, b := active, int32(10)
	for b != 0 {
		g, b = b, g%b
	}
	return 2*float64(active)/10 + 1 - float64(g)/10
}

// presentMillis is the host millisecond presentation reads. The wall clock is
// read below the millisecond, so the presented position does not step in
// whole milliseconds between frames; an injected source is read as it is.
func (b *battleSession) presentMillis() float64 {
	if src, ok := b.millisSource.(*monotonicMillisSource); ok && src != nil {
		return float64(time.Since(src.start)) / float64(time.Millisecond)
	}
	if b.millisSource == nil {
		b.millisSource = newMonotonicMillisSource()
		return b.presentMillis()
	}
	return float64(b.millisSource.Millis32())
}

// presentStepMillis is a host step's ideal host millisecond and how late its
// body is running against it (negative when early), from the instant the
// window's host clock named for it. Without one — an injected millisecond
// source, or a host that names none — the body's own millisecond is the ideal
// and the step is on time.
func (b *battleSession) presentStepMillis(due time.Time) (ideal, late float64) {
	now := b.presentMillis()
	src, ok := b.millisSource.(*monotonicMillisSource)
	if !ok || src == nil || due.IsZero() {
		return now, 0
	}
	ideal = float64(due.Sub(src.start)) / float64(time.Millisecond)
	return ideal, now - ideal
}

// notePresentStep rebases the clock at a prepared host step, on the game
// goroutine (prepareSimulationStep): ms is the step's ideal host millisecond
// and late how far behind it the body runs, base the global tick after the
// step's release plus the budget's carry, and active the speed the next step's
// budget accrues at. The lag in effect is carried across, so T is continuous
// at the step whatever the lag is doing; a lateness beyond the margin raises
// it at once, which holds the world rather than moving it back.
func (b *battleSession) notePresentStep(ms, late, base float64, active int32) {
	p := &b.present
	at := ms + max(late, 0)
	p.margin = max(b.presentMarginAt(at), min(max(late, 0), presentMarginCeiling))
	p.marginMS = at
	p.lag = b.presentLagAt(ms, active)
	p.base, p.baseMS, p.set = base, ms, true
}

// presentMarginAt is the lateness margin, in host milliseconds, at host
// millisecond ms.
func (b *battleSession) presentMarginAt(ms float64) float64 {
	p := &b.present
	if !p.set {
		return 0
	}
	return max(p.margin-max(ms-p.marginMS, 0)*presentMarginRelease, 0)
}

// presentLagAt is the lag at host millisecond ms for the given speed.
func (b *battleSession) presentLagAt(ms float64, active int32) float64 {
	target := presentationLag(active)
	p := &b.present
	if !p.set || p.lag <= target {
		return target
	}
	return max(target, p.lag-(ms-p.baseMS)*presentLagEase)
}

// presentationAt names the committed tick the modern window presents under the
// asynchronous simulation, and the blend fraction toward it, for the instant
// ahead of now: zero for a Draw, the predicted present for a pre-record. It
// never names a tick the host has not joined, and at the nominal speed it names
// the tick before the last release throughout a step, as it always has.
//
// A command applied at the paused-input boundary republishes the committed
// tick; that republication is presented at once, and unblended, because the
// buffer pairs no previous frame with a publication that repeats its tick.
// A terminal publication also presents at once after its join: no later tick
// will release the delay, and both the end title and ENDMSN must see the same
// frozen result as the host's post-battle controller [07 §11][08 R-CAMP-01 §6].
func (b *battleSession) presentationAt(ahead time.Duration) (uint32, float32, bool) {
	if b == nil {
		return 0, 0, false
	}
	return b.presentationSample(b.presentMillis()+float64(ahead)/float64(time.Millisecond), ahead == 0)
}

// presentationAtInstant is presentationAt for a frame a host prepared ahead of
// its display: at is that frame's predicted instant, and the sample is the
// frame's own, so it holds the floor as a Draw's does.
func (b *battleSession) presentationAtInstant(at time.Time) (uint32, float32, bool) {
	if b == nil {
		return 0, 0, false
	}
	now := b.presentMillis()
	if src, ok := b.millisSource.(*monotonicMillisSource); ok && src != nil {
		now = float64(at.Sub(src.start)) / float64(time.Millisecond)
	}
	return b.presentationSample(now, true)
}

func (b *battleSession) presentationSample(now float64, own bool) (uint32, float32, bool) {
	if b == nil || b.sim == nil || !b.sim.observedValid {
		return 0, 0, false
	}
	r := b.sim
	joined := r.observedTick
	p := &b.present
	switch {
	case r.republished, r.terminal:
		return joined, 1, true
	case b.simPaused && p.sampled:
		return min(p.tick, joined), p.fraction, true
	case !p.set:
		return joined, 0, true
	}
	active := min(max(b.simActive, 1), 20)
	rate := float64(active) * 3 / 1000 // 30 × active × 0.1 ticks per second
	t := p.base + (now-p.baseMS)*rate - b.presentLagAt(now, active) - b.presentMarginAt(now)*rate
	if p.floorSet && t < p.floor {
		t = p.floor
	}
	t = max(t, 0)
	whole := math.Floor(t)
	tick, fraction := uint32(whole)+1, float32(t-whole)
	if tick > joined {
		// Only a late host step gets here: the clock ran on while the batch it
		// would present had not been joined yet, so the newest joined tick is
		// held until it is.
		tick, fraction, t = joined, 1, float64(joined)
	}
	if own {
		p.floor, p.floorSet = t, true
		p.tick, p.fraction, p.sampled = tick, fraction, true
	}
	return tick, fraction, true
}
