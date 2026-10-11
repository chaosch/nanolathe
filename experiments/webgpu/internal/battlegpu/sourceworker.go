package battlegpu

import (
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"time"
)

type sourceRequest struct {
	seconds float64
	input   meshscene.Input
}
type preparedSource struct {
	frame        meshscene.LiveFrame
	milliseconds float64
	err          error
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
			results <- preparedSource{f, float64(time.Since(start).Nanoseconds()) / 1e6, err}
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
