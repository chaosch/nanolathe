package battlegpu

import (
	"sync/atomic"

	"github.com/gogpu/gogpu"
)

// runningApp is the window host of the battle Run is drawing, if any.
var runningApp atomic.Pointer[gogpu.App]

// ClipboardText reads the platform clipboard through the running window host
// for builds whose battle host has no native pasteboard code of its own. ok is
// false when no battle window is running, the read fails, or the platform
// cannot read synchronously. A successful read of an empty clipboard and an
// absent text item are indistinguishable through GoGPU; both return "", true.
func ClipboardText() (text string, ok bool) {
	if !clipboardReadable {
		return "", false
	}
	app := runningApp.Load()
	if app == nil {
		return "", false
	}
	text, err := app.ClipboardRead()
	if err != nil {
		return "", false
	}
	return text, true
}
