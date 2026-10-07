package metalproto

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
)

// BenchmarkOptions selects the production benchmark's fixed draw schedule and
// measurement boundaries. Callbacks execute synchronously on the render host;
// they must not launch a competing simulation or presentation reader [I6].
// With PrepareAhead, AfterFrame for one frame may run while the source
// prepares the next on the worker.
type BenchmarkOptions struct {
	BeforeMeasure, AfterMeasure func() error
	AfterFrame                  func(BenchmarkFrame)
}

// BenchmarkFrame brackets the complete Go/native host call after pacing.
// Submit includes native queue/drawable waits and encoding; it is never GPU
// execution or a presentation timestamp. Native CSV keeps those separately.
// Ahead marks a frame prepared during the previous frame's pacing; its Source
// and Packing ran on the worker, and only Join (the remaining wait) is in
// DrawWork.
type BenchmarkFrame struct {
	Frame                                    int
	CadenceValid, Ahead                      bool
	PaceWait, OutsideDraw, DrawWork, Cadence float64
	Source, Packing, Submit, Join            float64
}

// Match the production windowed benchmark's forced-GC and profiling protocol.
// Snapshots, profile encoding and screenshots stay outside exact window deltas.
type benchmarkMeasurement struct {
	directory              string
	options                *BenchmarkOptions
	cpu                    *os.File
	before, after, afterGC runtime.MemStats
}

func (m *benchmarkMeasurement) file(name string, write func(*os.File) error) error {
	f, err := os.Create(filepath.Join(m.directory, name))
	if err != nil {
		return err
	}
	err = write(f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (m *benchmarkMeasurement) begin() error {
	if m.options.BeforeMeasure != nil {
		if err := m.options.BeforeMeasure(); err != nil {
			return err
		}
	}
	runtime.GC()
	if err := m.file("alloc-base.pprof", func(f *os.File) error { return pprof.Lookup("allocs").WriteTo(f, 0) }); err != nil {
		return err
	}
	var err error
	m.cpu, err = os.Create(filepath.Join(m.directory, "cpu.pprof"))
	if err != nil {
		return err
	}
	if err := pprof.StartCPUProfile(m.cpu); err != nil {
		m.cpu.Close()
		m.cpu = nil
		return err
	}
	runtime.ReadMemStats(&m.before)
	return nil
}

func (m *benchmarkMeasurement) stop() {
	if m.cpu != nil {
		pprof.StopCPUProfile()
		m.cpu.Close()
		m.cpu = nil
	}
}

func (m *benchmarkMeasurement) end() error {
	runtime.ReadMemStats(&m.after)
	m.stop()
	runtime.GC()
	runtime.ReadMemStats(&m.afterGC)
	for _, name := range []string{"alloc", "heap"} {
		profile := name
		if name == "alloc" {
			profile = "allocs"
		}
		if err := m.file(name+".pprof", func(f *os.File) error { return pprof.Lookup(profile).WriteTo(f, 0) }); err != nil {
			return err
		}
	}
	if err := m.file("goroutine.txt", func(f *os.File) error { return pprof.Lookup("goroutine").WriteTo(f, 2) }); err != nil {
		return err
	}
	if m.options.AfterMeasure != nil {
		return m.options.AfterMeasure()
	}
	return nil
}

func (m *benchmarkMeasurement) report() map[string]any {
	return map[string]any{
		"before": m.before, "after": m.after, "after_gc": m.afterGC,
		"alloc_bytes": m.after.TotalAlloc - m.before.TotalAlloc,
		"mallocs":     m.after.Mallocs - m.before.Mallocs,
		"gc":          m.after.NumGC - m.before.NumGC,
		"gc_pause_ns": m.after.PauseTotalNs - m.before.PauseTotalNs,
		"scope":       "process-wide Go counters from measured host work, excluding snapshots, profile encoding, final drain and capture",
	}
}
