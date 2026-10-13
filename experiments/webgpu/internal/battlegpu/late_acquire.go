package battlegpu

import (
	"errors"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"time"
)

// errFrameUnavailable reports a drawable the host could not provide after the
// frame was prepared (the surface was outdated, lost or not configured). The
// prepared state stays valid for Renderer.Redraw; no borrowed source is reread.
var errFrameUnavailable = errors.New("surface frame unavailable after preparation")

type frameAcquirer interface {
	CommandEncoder() *wgpu.CommandEncoder
	SurfaceView() *wgpu.TextureView
	FramebufferSize() (int, int)
	Format() gputypes.TextureFormat
}

// acquireFrame takes the frame's encoder and drawable view from the stock
// host, which acquires the drawable lazily on the first of these calls. It
// reports whether the drawable's extent differs from the prepared one: the
// drawable cannot be returned once acquired, so the caller still encodes the
// prepared frame into it (one stretched frame during a live resize) and the
// next callback resizes. A changed surface format is a failure.
func acquireFrame(ctx frameAcquirer, width, height int, format gputypes.TextureFormat) (enc *wgpu.CommandEncoder, view *wgpu.TextureView, resized bool, err error) {
	if ctx == nil || width <= 0 || height <= 0 {
		return nil, nil, false, fmt.Errorf("late acquisition requires a positive prepared extent")
	}
	if enc = ctx.CommandEncoder(); enc == nil {
		return nil, nil, false, errFrameUnavailable
	}
	if view = ctx.SurfaceView(); view == nil {
		return nil, nil, false, errFrameUnavailable
	}
	if actual := ctx.Format(); actual != format {
		return nil, nil, false, fmt.Errorf("surface format changed from %v to %v", format, actual)
	}
	w, h := ctx.FramebufferSize()
	return enc, view, w != width || h != height, nil
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
