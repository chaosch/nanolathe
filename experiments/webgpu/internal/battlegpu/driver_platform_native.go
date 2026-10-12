//go:build !darwin && !(js && wasm)

package battlegpu

// Linux and Windows windows commonly start at scale 1. Run reads the actual
// platform scale on the first draw and requests the logical size that yields
// the requested drawable when they differ. Runtime is unqualified here.
func windowScaleHint() float64 { return 1 }

// Native hosts pace to the source deadline inside the draw callback.
const hostPacesFrames = true

// GoGPU reads the Win32, X11 and Wayland clipboards; it is unqualified here.
const clipboardReadable = true
