package client

import (
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/sim/rng"
)

func retainedBlendClient(t *testing.T, serial bool) *Client {
	t.Helper()
	c := &Client{buffer: frame.NewBuffer(), cam: &camera.Camera{ViewW: 64, ViewH: 64, MapW: 4096, MapH: 4096}}
	c.Resize(64, 64)
	c.SetInterpolation(true)
	stream := rng.NewCRT(7)
	c.SetPresentationCRT(&stream)
	if serial {
		c.recordPool = newRecordPool(1)
	}
	t.Cleanup(c.Close)
	return c
}
func publishRetainedWalk(t *testing.T, c *Client, tick uint32, n int) {
	t.Helper()
	slots := make([]pool.Handle, n)
	for i := range slots {
		slots[i] = pool.Handle(i + 1)
	}
	sample := walkFrame(tick, slots...)
	f := c.buffer.BeginWrite()
	f.Units = append(f.Units, sample.Units...)
	if err := c.buffer.Publish(tick); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedFirstReadUsesJoinedPoolWithoutChangingFrame(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	sequential, parallel := retainedBlendClient(t, true), retainedBlendClient(t, false)
	var a, b RetainedEffectStorage
	for tick := uint32(1); tick <= 9; tick++ {
		for _, c := range []*Client{sequential, parallel} {
			publishRetainedWalk(t, c, tick, 300)
			c.SetTickFraction(.5)
		}
		want, got := sequential.RecordRetainedEffectsInto(&a), parallel.RecordRetainedEffectsInto(&b)
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(parallel.interp.view, sequential.interp.view) {
			t.Fatalf("retained outputs/frame differ at tick%d", tick)
		}
		if *parallel.crt != *sequential.crt || parallel.parallelRecord || parallel.recordModelGeometry || parallel.geometryOnlyModels {
			t.Fatal("retained recording changed CRT or left temporary flags")
		}
	}
	if runtime.GOARCH != "wasm" && runtime.NumCPU() > 1 {
		if parallel.recordPool == nil || len(parallel.recordPool.wake) == 0 || parallel.interp.blendRangeFn == nil {
			t.Fatal("first retained blend never reached the existing pool")
		}
		if len(parallel.recordPool.workers)+1 > runtime.GOMAXPROCS(0) {
			t.Fatal("retained pool oversubscribed active Go parallelism")
		}
	}
	parallel.Close()
	if parallel.recordPool != nil {
		t.Fatal("close retained worker pool")
	}
}
func TestRetainedBlendKeepsSmallAndSingleParticipantSerial(t *testing.T) {
	for _, settings := range []struct{ participants, units int }{{4, parallelBlendFloor - 1}, {1, 300}} {
		previous := runtime.GOMAXPROCS(settings.participants)
		c := retainedBlendClient(t, false)
		publishRetainedWalk(t, c, 1, settings.units)
		publishRetainedWalk(t, c, 2, settings.units)
		c.SetTickFraction(.5)
		c.RecordRetainedEffects()
		if c.recordPool != nil {
			t.Fatal("serial case created retained helpers")
		}
		c.Close()
		runtime.GOMAXPROCS(previous)
	}
}

// A modern geometry recorder may have created a larger pool before the active
// Go budget changes. Blend dispatch must leave its extra helpers parked.
func TestRetainedBlendDispatchCapsExistingPoolAfterBudgetReduction(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	p := newRecordPool(8)
	defer p.close()
	runtime.GOMAXPROCS(2)
	var active, peak, completed atomic.Int32
	p.forEach(256, 1, func(lo, hi int) {
		current := active.Add(1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		// Yield while inside a task so excess awakened helpers remain observable
		// even when the runtime executes only two OS threads at once.
		for i := 0; i < 16; i++ {
			runtime.Gosched()
		}
		completed.Add(int32(hi - lo))
		active.Add(-1)
	})
	if completed.Load() != 256 || active.Load() != 0 {
		t.Fatalf("dispatch did not join all tasks: completed=%d active=%d", completed.Load(), active.Load())
	}
	if peak.Load() > 2 {
		t.Fatalf("existing pool used %d participants with GOMAXPROCS=2", peak.Load())
	}
}
