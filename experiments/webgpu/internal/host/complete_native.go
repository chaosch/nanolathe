//go:build !js

package host

import "github.com/gogpu/wgpu"

func Complete(d *wgpu.Device) error { return d.WaitIdle() }
func Publish(report string)         {}
func DeviceLost() string            { return "" }

func StopRequested() bool { return false }
