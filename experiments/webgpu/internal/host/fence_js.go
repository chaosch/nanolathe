//go:build js && wasm

package host

import (
	"fmt"
	"github.com/gogpu/wgpu"
	"syscall/js"
)

// The bridge's microtask runs after this RAF/Go callback returns and GoGPU submits the encoder.
type FrameFence struct {
	token int
	done  bool
}

func NewFrameFence(_ *wgpu.Device) *FrameFence {
	return &FrameFence{token: js.Global().Get("probeBridge").Call("beginFrameCompletion").Int()}
}
func (f *FrameFence) Poll() (bool, error) {
	if f.done {
		return true, nil
	}
	v := js.Global().Get("probeBridge").Call("completion", f.token)
	if v.Type() != js.TypeString {
		return false, nil
	}
	if v.String() != "done" {
		return false, fmt.Errorf("queue completion: %s", v.String())
	}
	f.done = true
	return true, nil
}
