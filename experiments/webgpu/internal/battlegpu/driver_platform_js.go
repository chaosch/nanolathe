//go:build js && wasm

package battlegpu

import "syscall/js"

// The canvas's CSS size is logical; its backing store follows
// devicePixelRatio on every frame.
func windowScaleHint() float64 {
	if dpr := js.Global().Get("devicePixelRatio"); dpr.Type() == js.TypeNumber && dpr.Float() > 0 {
		return dpr.Float()
	}
	return 1
}

// requestAnimationFrame owns the browser cadence. A Go sleep inside its
// callback would hold the page's event loop, which the timers it needs run on.
const hostPacesFrames = false

// GoGPU's browser clipboard read always returns an empty string: the async
// Clipboard API cannot be awaited from inside a frame callback. Report the
// clipboard as unavailable rather than as an empty successful read.
const clipboardReadable = false
