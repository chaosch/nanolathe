//go:build js && wasm

package host

import (
	"fmt"
	"github.com/gogpu/wgpu"
	"syscall/js"
	"time"
)

// Complete runs only outside a draw/JS callback. Polling yields to the browser.
// It uses the real queue promise; wgpu's browser WaitIdle/Poll are not completion.
func Complete(_ *wgpu.Device) error {
	b := js.Global().Get("probeBridge")
	token := b.Call("beginCompletion").Int()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v := b.Call("completion", token)
		if v.Type() == js.TypeString {
			if v.String() == "done" {
				return nil
			}
			return fmt.Errorf("queue completion: %s", v.String())
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("queue completion timed out; resources cannot be safely released")
}
func Publish(report string) { js.Global().Get("probeBridge").Call("publish", report) }
func DeviceLost() string    { return js.Global().Get("probeBridge").Get("lost").String() }

func StopRequested() bool { return js.Global().Get("probeBridge").Get("stop").Bool() }
