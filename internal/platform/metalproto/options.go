// Package metalproto is a throwaway retained-mesh Metal experiment. It consumes
// immutable presentation samples through a caller-owned source; it never writes
// simulation state.
package metalproto

// Frames zero runs until window close and retains the last 8192 timing rows.
// Options selects host presentation and measurement, not gameplay behavior.
type Options struct {
	Width, Height, Frames, Warmup, FPS      int
	Offscreen, VSync, Effects, VisualPasses bool
	OutputDir, LibraryPath                  string
	Benchmark                               *BenchmarkOptions
}
