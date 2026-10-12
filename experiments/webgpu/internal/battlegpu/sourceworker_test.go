package battlegpu

import (
	"errors"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"testing"
	"time"
)

type workerFixture struct {
	entered chan meshscene.Input
	release chan struct{}
	err     error
	exit    bool
	resized int
}

func (f *workerFixture) Next(_ float64, in meshscene.Input) (meshscene.LiveFrame, error) {
	f.entered <- in
	<-f.release
	return meshscene.LiveFrame{Tick: 7, Alpha: .25, Exit: f.exit}, f.err
}
func (*workerFixture) Close() map[string]any { return nil }
func TestSourceWorkerSerialJoinAndClose(t *testing.T) {
	want := time.Unix(123, 0)
	f := &workerFixture{entered: make(chan meshscene.Input, 1), release: make(chan struct{})}
	w := newSourceWorker(f)
	w.start(1, meshscene.Input{At: want})
	in := <-f.entered
	if !in.At.Equal(want) {
		t.Fatal("lost predicted draw instant")
	}
	joined := make(chan preparedSource, 1)
	go func() {
		p, ahead, _ := w.join()
		if !ahead {
			panic("not ahead")
		}
		joined <- p
	}()
	select {
	case <-joined:
		t.Fatal("joined an unfinished source")
	default:
	}
	close(f.release)
	p := <-joined
	if p.err != nil || p.frame.Tick != 7 || p.frame.Alpha != .25 || !p.sampleAt.Equal(want) || p.readyAt.IsZero() {
		t.Fatalf("result=%+v", p)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.done:
	default:
		t.Fatal("worker not joined")
	}
}
func TestSourceWorkerShutdownDrainsAndReturnsFailure(t *testing.T) {
	sentinel := errors.New("source failed")
	f := &workerFixture{entered: make(chan meshscene.Input, 1), release: make(chan struct{}), err: sentinel}
	w := newSourceWorker(f)
	w.start(0, meshscene.Input{})
	<-f.entered
	stopped := make(chan error, 1)
	go func() { stopped <- w.close() }()
	select {
	case <-stopped:
		t.Fatal("shutdown did not wait for source")
	default:
	}
	close(f.release)
	if err := <-stopped; !errors.Is(err, sentinel) {
		t.Fatalf("shutdown error=%v", err)
	}
}
func TestSourceAheadMeasurementBoundaries(t *testing.T) {
	for _, tc := range []struct {
		next int
		want bool
	}{{239, true}, {240, false}, {241, true}, {959, true}, {960, false}} {
		if got := sourceAheadBoundary(tc.next, 240, 720); got != tc.want {
			t.Fatalf("next %d: %v", tc.next, got)
		}
	}
	if !sourceAheadBoundary(10000, 240, 0) {
		t.Fatal("interactive source stopped")
	}
}

// Initialization can fail before the first request reaches the worker.
func TestSourceWorkerCloseBeforeFirstRequest(t *testing.T) {
	done := make(chan struct{})
	go func() {
		for range 1000 {
			f := &workerFixture{entered: make(chan meshscene.Input, 1), release: make(chan struct{})}
			w := newSourceWorker(f)
			_ = w.close()
			select {
			case <-f.entered:
				panic("unused source called Next")
			default:
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("unused worker failed to join")
	}
}

func (f *workerFixture) ResizePhysicalExtent(int, int) error { f.resized++; return nil }
func TestResizeSourcePreservesTerminalAndFailedPreparedPackets(t *testing.T) {
	for _, tc := range []struct {
		exit bool
		err  error
	}{{true, nil}, {false, errors.New("source failed")}} {
		f := &workerFixture{entered: make(chan meshscene.Input, 1), release: make(chan struct{}), exit: tc.exit, err: tc.err}
		w := newSourceWorker(f)
		w.start(0, meshscene.Input{})
		<-f.entered
		done := make(chan struct{})
		var exit bool
		var err error
		go func() { exit, err = resizeSource(w, f, 800, 600); close(done) }()
		select {
		case <-done:
			t.Fatal("resize raced unfinished Next")
		default:
		}
		close(f.release)
		<-done
		if exit != tc.exit || err != tc.err || f.resized != 0 {
			t.Fatalf("terminal result=%v,%v resize=%d", exit, err, f.resized)
		}
		if err := w.close(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestResizeSourceMutatesOnlyAfterPreparedPacketCompletes(t *testing.T) {
	f := &workerFixture{entered: make(chan meshscene.Input, 1), release: make(chan struct{})}
	w := newSourceWorker(f)
	w.start(0, meshscene.Input{})
	<-f.entered
	close(f.release)
	exit, err := resizeSource(w, f, 800, 600)
	if exit || err != nil || f.resized != 1 || w.pending {
		t.Fatalf("resize=%d exit=%v err=%v pending=%v", f.resized, exit, err, w.pending)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
}
