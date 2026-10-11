package host

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed smoke.wgsl
var smokeWGSL string

// Smoke owns only its test resources. Device, queue and encoder are borrowed.
type Smoke struct {
	device                     *wgpu.Device
	releases                   []func()
	result, read, indices      *wgpu.Buffer
	compute                    *wgpu.ComputePipeline
	render                     *wgpu.RenderPipeline
	computeGroup, textureGroup *wgpu.BindGroup
	pending                    *wgpu.MapPending
}

func NewSmoke(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat) (s *Smoke, err error) {
	s = &Smoke{device: d}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	s.result, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "smoke-result", Size: 16, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.result.Release)
	s.read, err = d.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.read.Release)
	s.indices, err = d.CreateBuffer(&wgpu.BufferDescriptor{Size: 12, Usage: wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.indices.Release)
	bytes := make([]byte, 12)
	binary.LittleEndian.PutUint32(bytes[4:], 1)
	binary.LittleEndian.PutUint32(bytes[8:], 2)
	if err = q.WriteBuffer(s.indices, 0, bytes); err != nil {
		return
	}
	shader, e := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "smoke-shared", WGSL: smokeWGSL})
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, shader.Release)
	cl, e := d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage, MinBindingSize: 16}}}})
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, cl.Release)
	tl, e := d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
		{Binding: 1, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
	}})
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, tl.Release)
	layout, e := d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{cl, tl}})
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, layout.Release)
	s.compute, err = d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: layout, Module: shader, EntryPoint: "smoke"})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.compute.Release)
	s.computeGroup, err = d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: cl, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: s.result, Size: 16}}})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.computeGroup.Release)
	tex, e := d.CreateTexture(&wgpu.TextureDescriptor{Size: wgpu.Extent3D{Width: 2, Height: 2, DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA8Unorm, Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst})
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, tex.Release)
	pixels := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 0, 255}
	if err = q.WriteTexture(&wgpu.ImageCopyTexture{Texture: tex}, pixels, &wgpu.ImageDataLayout{BytesPerRow: 8, RowsPerImage: 2}, &wgpu.Extent3D{Width: 2, Height: 2, DepthOrArrayLayers: 1}); err != nil {
		return
	}
	view, e := d.CreateTextureView(tex, nil)
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, view.Release)
	sampler, e := d.CreateSampler(&wgpu.SamplerDescriptor{AddressModeU: gputypes.AddressModeClampToEdge, AddressModeV: gputypes.AddressModeClampToEdge, AddressModeW: gputypes.AddressModeClampToEdge, MagFilter: gputypes.FilterModeNearest, MinFilter: gputypes.FilterModeNearest, MipmapFilter: gputypes.FilterModeNearest})
	if e != nil {
		err = e
		return
	}
	s.releases = append(s.releases, sampler.Release)
	s.textureGroup, err = d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: tl, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}, {Binding: 1, Sampler: sampler}}})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.textureGroup.Release)
	s.render, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Layout: layout, Vertex: wgpu.VertexState{Module: shader, EntryPoint: "vertex"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}, Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: "fragment", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}}})
	if err != nil {
		return
	}
	s.releases = append(s.releases, s.render.Release)
	return
}

func (s *Smoke) Encode(e *wgpu.CommandEncoder, v *wgpu.TextureView, first bool) error {
	if first {
		p, err := e.BeginComputePass(nil)
		if err != nil {
			return err
		}
		p.SetPipeline(s.compute)
		p.SetBindGroup(0, s.computeGroup, nil)
		p.SetBindGroup(1, s.textureGroup, nil)
		p.Dispatch(1, 1, 1)
		if err = p.End(); err != nil {
			return err
		}
		e.CopyBufferToBuffer(s.result, 0, s.read, 0, 16)
	}
	p, err := e.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: v, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{R: 0.05, G: 0.05, B: 0.05, A: 1}}}})
	if err != nil {
		return err
	}
	p.SetPipeline(s.render)
	p.SetBindGroup(0, s.computeGroup, nil)
	p.SetBindGroup(1, s.textureGroup, nil)
	p.SetIndexBuffer(s.indices, gputypes.IndexFormatUint32, 0)
	p.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: 3, InstanceCount: 1})
	return p.End()
}
func (s *Smoke) Readback() (bool, error) {
	s.device.Poll(wgpu.PollPoll)
	if s.pending == nil {
		p, e := s.read.MapAsync(wgpu.MapModeRead, 0, 16)
		if e != nil {
			return true, e
		}
		s.pending = p
	}
	ready, e := s.pending.Status()
	if !ready || e != nil {
		return ready, e
	}
	r, e := s.read.MappedRange(0, 16)
	if e != nil {
		return true, e
	}
	b := r.Bytes()
	for i := 0; i < 4; i++ {
		if got := binary.LittleEndian.Uint32(b[i*4:]); got != uint32(17+i*3) {
			e = fmt.Errorf("compute lane %d: got %d expected %d", i, got, 17+i*3)
		}
	}
	r.Release()
	s.read.Unmap()
	s.pending.Release()
	return true, e
}
func (s *Smoke) Close() {
	for i := len(s.releases) - 1; i >= 0; i-- {
		s.releases[i]()
	}
	s.releases = nil
}
