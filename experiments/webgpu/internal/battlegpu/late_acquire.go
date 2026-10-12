package battlegpu

import (
	"errors"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"time"
)

// Deferred operations are render-owner-only and require the private owned host.
type deferredFrameContext interface {
	DeferredAcquireState() (int, int, gputypes.TextureFormat, bool)
	DisableDeferredFrameReplay() bool
	DiscardDeferredAcquire() bool
}

var errDeferredFrameChanged = errors.New("deferred frame unavailable or changed before command encoding")

func lateAcquireSteadyFrame(draw, warmup, frames int) bool {
	return draw > 0 && draw != warmup-1 && draw != warmup && (frames <= 0 || draw < warmup+frames-1)
}

type fixedFrameAcquirer interface {
	CommandEncoder() *wgpu.CommandEncoder
	SurfaceView() *wgpu.TextureView
	FramebufferSize() (int, int)
	Format() gputypes.TextureFormat
}

func acquireFixedFrame(ctx fixedFrameAcquirer, width, height int, format gputypes.TextureFormat) (*wgpu.CommandEncoder, *wgpu.TextureView, error) {
	if ctx == nil || width <= 0 || height <= 0 {
		return nil, nil, fmt.Errorf("deferred acquisition requires positive prepared extent")
	}
	enc := ctx.CommandEncoder()
	if enc == nil {
		return nil, nil, errDeferredFrameChanged
	}
	view := ctx.SurfaceView()
	if view == nil {
		return nil, nil, errDeferredFrameChanged
	}
	w, h := ctx.FramebufferSize()
	actualFormat := ctx.Format()
	if actualFormat != format {
		return nil, nil, fmt.Errorf("deferred surface format changed from %v to %v", format, actualFormat)
	}
	if w != width || h != height {
		return nil, nil, errDeferredFrameChanged
	}
	return enc, view, nil
}

// This consumes only the existing pending request. It never starts Next or
// re-applies input; authoritative work from the sample has already happened.
func dropPreparedSource(w *sourceWorker) (bool, error) {
	p, pending, _ := w.join()
	if !pending {
		return false, nil
	}
	return p.frame.Exit, p.err
}

// A recovery can reset the deadline before the first source sample. The source
// timeline is initialized independently and remains anchored across recovery.
func startSourceTimeline(next, anchor, entry time.Time) (time.Time, time.Time) {
	if next.IsZero() {
		next = entry
	}
	if anchor.IsZero() {
		anchor = entry
	}
	return next, anchor
}

// A pending Next may already have consumed a projected pacing timestamp when
// recovery drops its presentation sample. Fresh input must not move the source
// clock behind that authoritative call. Equal sample times remain valid.
func sourceSampleTime(previous, candidate time.Time) time.Time {
	if candidate.Before(previous) {
		return previous
	}
	return candidate
}
