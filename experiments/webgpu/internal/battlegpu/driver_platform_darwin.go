//go:build darwin

package battlegpu

// The production Metal host draws its HUD at half the drawable
// (DESIGN_METAL_RENDERER §6); a Retina window supplies that doubled drawable
// for a half-size logical window. Run corrects the request once if the
// window's actual scale differs.
func windowScaleHint() float64 { return 2 }

// Native hosts pace to the source deadline inside the draw callback.
const hostPacesFrames = true

// The pasteboard read stays in the production host's own darwin code.
const clipboardReadable = true
