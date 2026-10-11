//go:build js && wasm

package host

import "github.com/gogpu/wgpu"

// Browser diagnostics are asynchronous and initiated by the authored harness.
func Diagnostics(_ *wgpu.Device) error { return nil }
