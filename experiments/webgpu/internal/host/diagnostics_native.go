//go:build !js

package host

import (
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

func Diagnostics(d *wgpu.Device) error {
	d.PushErrorScope(wgpu.ErrorFilterValidation)
	shader, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "deliberately-invalid", WGSL: "@compute @workgroup_size(1) fn invalid() { let broken = ; }"})
	scoped := d.PopErrorScope()
	if shader != nil {
		shader.Release()
	}
	fmt.Printf("invalid_shader returned=%v scoped=%v\n", err, scoped)
	if err == nil && scoped == nil {
		return fmt.Errorf("invalid shader escaped diagnostics")
	}
	layout, err := d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage, MinBindingSize: 32}}}})
	if err != nil {
		return err
	}
	defer layout.Release()
	b, err := d.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageStorage})
	if err != nil {
		return err
	}
	defer b.Release()
	d.PushErrorScope(wgpu.ErrorFilterValidation)
	group, err := d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: b, Size: 16}}})
	scoped = d.PopErrorScope()
	if group != nil {
		group.Release()
	}
	fmt.Printf("invalid_binding returned=%v scoped=%v\n", err, scoped)
	if err == nil && scoped == nil {
		return fmt.Errorf("invalid binding escaped diagnostics")
	}
	return nil
}
