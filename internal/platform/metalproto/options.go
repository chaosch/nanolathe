// Package metalproto is a throwaway retained-mesh Metal experiment. It consumes
// immutable presentation samples and never reaches a simulation session.
package metalproto

// Options selects host presentation and measurement, not gameplay behavior.
type Options struct {
	Width, Height, Frames, Warmup, FPS int
	Offscreen, VSync, Effects          bool
	OutputDir, LibraryPath             string
}
