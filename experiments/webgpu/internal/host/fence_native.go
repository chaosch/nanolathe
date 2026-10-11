//go:build !js

package host

import "github.com/gogpu/wgpu"

// FrameFence is polled in a subsequent draw callback, after the host submitted the borrowed encoder.
type FrameFence struct {
	d    *wgpu.Device
	done bool
}

func NewFrameFence(d *wgpu.Device) *FrameFence { return &FrameFence{d: d} }
func (f *FrameFence) Poll() (bool, error) {
	if f.done {
		return true, nil
	}
	e := f.d.WaitIdle()
	f.done = e == nil
	return f.done, e
}
