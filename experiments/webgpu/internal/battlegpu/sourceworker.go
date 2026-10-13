package battlegpu

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"time"
)

type sourceRequest struct {
	seconds float64
	input   meshscene.Input
}
type preparedSource struct {
	stages            [20]float64
	frame             meshscene.LiveFrame
	milliseconds      float64
	err               error
	sampleAt, readyAt time.Time
}

// sourceWorker has exactly one request in flight. LiveSource permits serial
// Next on a worker, with borrowed output valid until the next Next. The host
// starts the next request only after Prepare finishes all CPU reads/uploads;
// the worker never touches a renderer, queue, encoder or GPU resource.
type sourceWorker struct {
	requests chan sourceRequest
	results  chan preparedSource
	done     chan struct{}
	pending  bool
}

func newSourceWorker(source meshscene.LiveSource) *sourceWorker {
	w := &sourceWorker{requests: make(chan sourceRequest), results: make(chan preparedSource, 1), done: make(chan struct{})}
	requests, results, done := w.requests, w.results, w.done
	go func() {
		defer close(done)
		for r := range requests {
			start := time.Now()
			f, err := source.Next(r.seconds, r.input)
			ready := time.Now()
			results <- preparedSource{frame: f, milliseconds: float64(ready.Sub(start).Nanoseconds()) / 1e6, err: err, sampleAt: r.input.At, readyAt: ready, stages: sourcePreparationTimings(source)}
		}
	}()
	return w
}
func (w *sourceWorker) start(seconds float64, in meshscene.Input) {
	if w == nil || w.pending || w.requests == nil {
		panic("invalid WebGPU source prepare-ahead ownership")
	}
	w.requests <- sourceRequest{seconds, in}
	w.pending = true
}
func (w *sourceWorker) join() (p preparedSource, ahead bool, joinMS float64) {
	if w == nil || !w.pending {
		return
	}
	start := time.Now()
	p = <-w.results
	w.pending = false
	return p, true, float64(time.Since(start).Nanoseconds()) / 1e6
}

// waiting reports a started source whose result is not ready yet; it never
// blocks. A browser frame callback must not wait for it: the page's event
// loop, which the callback holds, completes the source's file reads.
func (w *sourceWorker) waiting() bool {
	return w != nil && w.pending && len(w.results) == 0
}

func (w *sourceWorker) close() error {
	if w == nil || w.requests == nil {
		return nil
	}
	p, _, _ := w.join()
	close(w.requests)
	w.requests = nil
	<-w.done
	return p.err
}

// Do not prepare across exact measurement boundaries or beyond the last
// frame: callbacks/capture must observe the final consumed source state.
func sourceAheadBoundary(next, warmup, frames int) bool {
	return next != warmup && (frames <= 0 || next < warmup+frames)
}

// Optional diagnostic values are copied while this worker still owns Next.
func sourcePreparationTimings(source meshscene.LiveSource) [20]float64 {
	if diagnostic, ok := source.(interface{ PreparationTimings() [20]float64 }); ok {
		return diagnostic.PreparationTimings()
	}
	return [20]float64{}
}

// physicalExtentSource is optional: fixed-frame sources retain their fixed
// window contract. Mutation is serial with Next, after joining any worker.
type physicalExtentSource interface{ ResizePhysicalExtent(int, int) error }

func resizeSource(w *sourceWorker, source meshscene.LiveSource, width, height int) (exit bool, err error) {
	if old, pending, _ := w.join(); pending {
		if old.err != nil || old.frame.Exit {
			return old.frame.Exit, old.err
		}
	}
	resizable, ok := source.(physicalExtentSource)
	if !ok {
		return false, fmt.Errorf("WebGPU live source lacks resize extent protocol")
	}
	return false, resizable.ResizePhysicalExtent(width, height)
}
