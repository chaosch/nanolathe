package battlegpu

import (
	"errors"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"reflect"
	"testing"
	"time"
)

type lateFrameFixture struct {
	calls  []string
	enc    *wgpu.CommandEncoder
	view   *wgpu.TextureView
	w, h   int
	format gputypes.TextureFormat
}

func (f *lateFrameFixture) CommandEncoder() *wgpu.CommandEncoder {
	f.calls = append(f.calls, "encoder")
	return f.enc
}
func (f *lateFrameFixture) SurfaceView() *wgpu.TextureView {
	f.calls = append(f.calls, "view")
	return f.view
}
func (f *lateFrameFixture) FramebufferSize() (int, int) {
	f.calls = append(f.calls, "extent")
	return f.w, f.h
}
func (f *lateFrameFixture) Format() gputypes.TextureFormat {
	f.calls = append(f.calls, "format")
	return f.format
}
func TestLateAcquisitionOrderAndOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		enc, view, resize, format bool
		calls                     []string
		unavailable, resized, ok  bool
	}{
		{"nil encoder", false, true, false, false, []string{"encoder"}, true, false, false},
		{"nil view", true, false, false, false, []string{"encoder", "view"}, true, false, false},
		{"changed format", true, true, false, true, []string{"encoder", "view", "format"}, false, false, false},
		{"changed extent", true, true, true, false, []string{"encoder", "view", "format", "extent"}, false, true, true},
		{"stable", true, true, false, false, []string{"encoder", "view", "format", "extent"}, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &lateFrameFixture{w: 1920, h: 1080, format: gputypes.TextureFormatBGRA8Unorm}
			if tc.enc {
				f.enc = new(wgpu.CommandEncoder)
			}
			if tc.view {
				f.view = new(wgpu.TextureView)
			}
			if tc.resize {
				f.w = 1600
			}
			if tc.format {
				f.format = gputypes.TextureFormatRGBA8Unorm
			}
			enc, view, resized, err := acquireFrame(f, 1920, 1080, gputypes.TextureFormatBGRA8Unorm)
			if !reflect.DeepEqual(f.calls, tc.calls) {
				t.Fatalf("acquisition retried or getter order changed: %v", f.calls)
			}
			if tc.ok {
				// A drawable cannot be handed back once acquired: a resized one
				// still receives the prepared frame.
				if err != nil || enc != f.enc || view != f.view || resized != tc.resized {
					t.Fatal("acquired operands lost")
				}
				return
			}
			if err == nil || enc != nil || view != nil {
				t.Fatal("failed acquisition published operands")
			}
			if errors.Is(err, errFrameUnavailable) != tc.unavailable {
				t.Fatalf("wrong recovery classification: %v", err)
			}
		})
	}
}
func TestDeferredFallbackDropsOneSourceWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		exit bool
		err  error
	}{{false, nil}, {true, nil}, {false, errors.New("source failed")}} {
		f := &workerFixture{entered: make(chan meshscene.Input, 1), release: make(chan struct{}), exit: tc.exit, err: tc.err}
		w := newSourceWorker(f)
		stamp := time.Unix(123, 0)
		w.start(1, meshscene.Input{At: stamp})
		in := <-f.entered
		if !in.At.Equal(stamp) {
			t.Fatal("input time changed")
		}
		close(f.release)
		terminal, err := dropPreparedSource(w)
		if terminal != tc.exit || !errors.Is(err, tc.err) {
			t.Fatalf("terminal/error lost: %v %v", terminal, err)
		}
		if w.pending {
			t.Fatal("pending source not joined")
		}
		if again, e := dropPreparedSource(w); again || e != nil {
			t.Fatal("drop replayed source")
		}
		_ = w.close()
		select {
		case <-f.entered:
			t.Fatal("fallback called Next again")
		default:
		}
	}
}

func TestSourceTimelineSurvivesRecoveryBeforeAndAfterFirstSample(t *testing.T) {
	entry := time.Unix(1000, 0)
	// An early focus recovery already assigned the deadline. The first elapsed
	// source timestamp must still start at zero, rather than a saturated duration.
	next, anchor := startSourceTimeline(entry, time.Time{}, entry)
	if next != entry || entry.Sub(anchor) != 0 {
		t.Fatal("first sample lost its source timeline")
	}
	later := entry.Add(time.Second)
	next, anchor = startSourceTimeline(later, anchor, later)
	if next != later || later.Sub(anchor) != time.Second {
		t.Fatal("recovery reset the established source timeline")
	}
	next, anchor = startSourceTimeline(time.Time{}, anchor, later)
	if next != later || later.Sub(anchor) != time.Second {
		t.Fatal("deadline restart reset the source timeline")
	}
}

func TestSourceSampleClockRetainsConsumedProjectedTimeDuringRecovery(t *testing.T) {
	now := time.Unix(1000, 0)
	projected := now.Add(8 * time.Millisecond)
	accepted := sourceSampleTime(time.Time{}, projected)
	recovered := sourceSampleTime(accepted, now.Add(time.Millisecond))
	if recovered != projected {
		t.Fatal("recovery moved behind a timestamp already consumed by Next")
	}
	later := now.Add(20 * time.Millisecond)
	if sourceSampleTime(recovered, later) != later {
		t.Fatal("source clock did not resume actual later time")
	}
}
