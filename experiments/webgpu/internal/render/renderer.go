package render

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed poses.wgsl
var poseShader string

//go:embed world.wgsl
var worldShader string

//go:embed composite.wgsl
var compositeShader string

type Counters struct{ Draws, Dispatches, Instances, Pieces, Triangles, UploadBytes, EndpointBytes, PersistentCreations int }
type Resources struct {
	Count int
	Bytes uint64
}
type release struct {
	close func()
	bytes uint64
}
type batch struct {
	firstIndex, indexCount, firstInstance, instanceCount uint32
	baseVertex                                           int32
}
type Renderer struct {
	d                                                                  *wgpu.Device
	q                                                                  *wgpu.Queue
	format                                                             gputypes.TextureFormat
	scene                                                              meshscene.Scene
	owned, targets                                                     []release
	live                                                               Resources
	creations                                                          uint64
	closed                                                             bool
	width, height                                                      int
	generation                                                         uint64
	endpoints, poses, instances, uniform, vertices, indices, materials *wgpu.Buffer
	compute                                                            *wgpu.ComputePipeline
	world, composite, overlay                                          *wgpu.RenderPipeline
	computeGroup, worldGroup, compositeGroup                           *wgpu.BindGroup
	compositeLayout                                                    *wgpu.BindGroupLayout
	color, identity, depth                                             *wgpu.Texture
	colorView, idView, depthView                                       *wgpu.TextureView
	batches                                                            []batch
	poseBytes, instanceBytes                                           []byte
	uniformBytes                                                       [uniformSize]byte
}

func (r *Renderer) retain(fn func(), bytes uint64, target bool) {
	r.creations++
	x := release{fn, bytes}
	if target {
		r.targets = append(r.targets, x)
	} else {
		r.owned = append(r.owned, x)
	}
	r.live.Count++
	r.live.Bytes += bytes
}
func (r *Renderer) buffer(label string, size uint64, usage gputypes.BufferUsage, data []byte) (*wgpu.Buffer, error) {
	b, e := r.d.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: size, Usage: usage})
	if e != nil {
		return nil, e
	}
	r.retain(b.Release, size, false)
	if len(data) > 0 {
		e = r.q.WriteBuffer(b, 0, data)
	}
	return b, e
}
func (r *Renderer) texture(label string, width, height int, format gputypes.TextureFormat, usage gputypes.TextureUsage, target bool) (*wgpu.Texture, *wgpu.TextureView, error) {
	t, e := r.d.CreateTexture(&wgpu.TextureDescriptor{Label: label, Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: usage})
	if e != nil {
		return nil, nil, e
	}
	r.retain(t.Release, uint64(width)*uint64(height)*4, target)
	v, e := r.d.CreateTextureView(t, nil)
	if e == nil {
		r.retain(v.Release, 0, target)
	}
	return t, v, e
}
func (r *Renderer) layout(entries []gputypes.BindGroupLayoutEntry) (*wgpu.BindGroupLayout, *wgpu.PipelineLayout, error) {
	b, e := r.d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if e != nil {
		return nil, nil, e
	}
	r.retain(b.Release, 0, false)
	p, e := r.d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{b}})
	if e == nil {
		r.retain(p.Release, 0, false)
	}
	return b, p, e
}
func bufferEntry(binding uint32, stages gputypes.ShaderStages, kind gputypes.BufferBindingType, size uint64) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stages, Buffer: &gputypes.BufferBindingLayout{Type: kind, MinBindingSize: size}}
}
func texEntry(binding uint32) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}}
}
func (r *Renderer) module(source string) (*wgpu.ShaderModule, error) {
	s, e := r.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: source})
	if e == nil {
		r.retain(s.Release, 0, false)
	}
	return s, e
}
func (r *Renderer) pipeline(s *wgpu.ShaderModule, l *wgpu.PipelineLayout, vertex, fragment string, formats []gputypes.TextureFormat, depth bool) (*wgpu.RenderPipeline, error) {
	targets := []gputypes.ColorTargetState{}
	for _, f := range formats {
		targets = append(targets, gputypes.ColorTargetState{Format: f, WriteMask: gputypes.ColorWriteMaskAll})
	}
	desc := &wgpu.RenderPipelineDescriptor{Layout: l, Vertex: wgpu.VertexState{Module: s, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}, Fragment: &wgpu.FragmentState{Module: s, EntryPoint: fragment, Targets: targets}}
	if depth {
		desc.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLess}
	}
	p, e := r.d.CreateRenderPipeline(desc)
	if e == nil {
		r.retain(p.Release, 0, false)
	}
	return p, e
}
func (r *Renderer) group(l *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry, target bool) (*wgpu.BindGroup, error) {
	g, e := r.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: l, Entries: entries})
	if e == nil {
		r.retain(g.Release, 0, target)
	}
	return g, e
}

func New(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, scene meshscene.Scene) (r *Renderer, err error) {
	r = &Renderer{d: d, q: q, format: format, scene: scene}
	count := len(scene.Instances)
	if count == 0 || len(scene.Meshes) != 3 {
		return r, fmt.Errorf("fixture topology must contain three meshes and nonzero instances")
	}
	var vertices []meshscene.Vertex
	var indexBytes []byte
	firstInstance := uint32(0)
	for mi, m := range scene.Meshes {
		b := batch{firstIndex: uint32(len(indexBytes) / 4), indexCount: uint32(len(m.Indices)), baseVertex: int32(len(vertices)), firstInstance: firstInstance}
		for _, v := range scene.Instances {
			if v.Mesh == mi {
				b.instanceCount++
			}
		}
		firstInstance += b.instanceCount
		r.batches = append(r.batches, b)
		vertices = append(vertices, m.Vertices...)
		for _, i := range m.Indices {
			indexBytes = binary.LittleEndian.AppendUint32(indexBytes, i)
		}
	}
	vb := packVertices(vertices)
	if r.vertices, err = r.buffer("resident-vertices", uint64(len(vb)), wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst, vb); err != nil {
		return
	}
	if r.indices, err = r.buffer("resident-indices", uint64(len(indexBytes)), wgpu.BufferUsageIndex|wgpu.BufferUsageCopyDst, indexBytes); err != nil {
		return
	}
	r.poseBytes = make([]byte, count*3*poseStride*2)
	r.instanceBytes = make([]byte, count*instanceStride)
	if r.endpoints, err = r.buffer("endpoints", uint64(len(r.poseBytes)), wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst, nil); err != nil {
		return
	}
	if r.poses, err = r.buffer("interpolated", uint64(len(r.poseBytes)/2), wgpu.BufferUsageStorage|wgpu.BufferUsageCopySrc, nil); err != nil {
		return
	}
	if r.instances, err = r.buffer("instances", uint64(len(r.instanceBytes)), wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst, nil); err != nil {
		return
	}
	if r.uniform, err = r.buffer("frame-uniforms", uniformSize, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst, nil); err != nil {
		return
	}
	materialBytes := make([]byte, 48)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			putFloat(materialBytes, i*16+j*4, 1)
		}
		putFloat(materialBytes, i*16+12, float32((i+1)*64))
	}
	if r.materials, err = r.buffer("material-identities", 48, wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst, materialBytes); err != nil {
		return
	}
	atlas, view, e := r.texture("authored-atlas", scene.Atlas.Width, scene.Atlas.Height, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopyDst, false)
	if e != nil {
		err = e
		return
	}
	if err = q.WriteTexture(&wgpu.ImageCopyTexture{Texture: atlas}, scene.Atlas.RGBA, &wgpu.ImageDataLayout{BytesPerRow: uint32(scene.Atlas.Width * 4), RowsPerImage: uint32(scene.Atlas.Height)}, &wgpu.Extent3D{Width: uint32(scene.Atlas.Width), Height: uint32(scene.Atlas.Height), DepthOrArrayLayers: 1}); err != nil {
		return
	}
	sampler, e := d.CreateSampler(&wgpu.SamplerDescriptor{AddressModeU: gputypes.AddressModeClampToEdge, AddressModeV: gputypes.AddressModeClampToEdge, AddressModeW: gputypes.AddressModeClampToEdge, MinFilter: gputypes.FilterModeNearest, MagFilter: gputypes.FilterModeNearest, MipmapFilter: gputypes.FilterModeNearest})
	if e != nil {
		err = e
		return
	}
	r.retain(sampler.Release, 0, false)
	cl, cp, e := r.layout([]gputypes.BindGroupLayoutEntry{bufferEntry(0, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeReadOnlyStorage, 64), bufferEntry(1, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeStorage, 64), bufferEntry(2, wgpu.ShaderStageCompute, gputypes.BufferBindingTypeUniform, 64)})
	if e != nil {
		err = e
		return
	}
	cs, e := r.module(poseShader)
	if e != nil {
		err = e
		return
	}
	r.compute, err = d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: cp, Module: cs, EntryPoint: "interpolate"})
	if err != nil {
		return
	}
	r.retain(r.compute.Release, 0, false)
	r.computeGroup, err = r.group(cl, []wgpu.BindGroupEntry{{Binding: 0, Buffer: r.endpoints}, {Binding: 1, Buffer: r.poses}, {Binding: 2, Buffer: r.uniform, Size: uniformSize}}, false)
	if err != nil {
		return
	}
	wl, wp, e := r.layout([]gputypes.BindGroupLayoutEntry{bufferEntry(0, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 48), bufferEntry(1, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 64), bufferEntry(2, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 16), bufferEntry(3, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeUniform, 64), texEntry(4), {Binding: 5, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}}, bufferEntry(6, wgpu.ShaderStageFragment, gputypes.BufferBindingTypeReadOnlyStorage, 16)})
	if e != nil {
		err = e
		return
	}
	ws, e := r.module(worldShader)
	if e != nil {
		err = e
		return
	}
	r.world, err = r.pipeline(ws, wp, "vertex", "fragment", []gputypes.TextureFormat{wgpu.TextureFormatRGBA8Unorm, wgpu.TextureFormatRGBA8Unorm}, true)
	if err != nil {
		return
	}
	r.worldGroup, err = r.group(wl, []wgpu.BindGroupEntry{{Binding: 0, Buffer: r.vertices}, {Binding: 1, Buffer: r.poses}, {Binding: 2, Buffer: r.instances}, {Binding: 3, Buffer: r.uniform, Size: uniformSize}, {Binding: 4, TextureView: view}, {Binding: 5, Sampler: sampler}, {Binding: 6, Buffer: r.materials}}, false)
	if err != nil {
		return
	}
	r.compositeLayout, wp, e = r.layout([]gputypes.BindGroupLayoutEntry{texEntry(0), texEntry(1), bufferEntry(2, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeUniform, 64)})
	if e != nil {
		err = e
		return
	}
	ss, e := r.module(compositeShader)
	if e != nil {
		err = e
		return
	}
	r.composite, err = r.pipeline(ss, wp, "composite_vertex", "composite_fragment", []gputypes.TextureFormat{format}, false)
	if err != nil {
		return
	}
	r.overlay, err = r.pipeline(ss, wp, "overlay_vertex", "overlay_fragment", []gputypes.TextureFormat{format}, false)
	return
}
func (r *Renderer) Resize(width, height int) error {
	if r.closed {
		return fmt.Errorf("renderer closed")
	}
	if width == r.width && height == r.height {
		return nil
	}
	r.releaseList(r.targets)
	r.targets = nil
	r.width, r.height = width, height
	if width == 0 || height == 0 {
		return nil
	}
	var e error
	r.color, r.colorView, e = r.texture("world-color", width, height, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopySrc, true)
	if e != nil {
		return e
	}
	r.identity, r.idView, e = r.texture("world-identity", width, height, wgpu.TextureFormatRGBA8Unorm, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding|wgpu.TextureUsageCopySrc, true)
	if e != nil {
		return e
	}
	r.depth, r.depthView, e = r.texture("world-depth", width, height, wgpu.TextureFormatDepth32Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageCopySrc, true)
	if e != nil {
		return e
	}
	r.compositeGroup, e = r.group(r.compositeLayout, []wgpu.BindGroupEntry{{Binding: 0, TextureView: r.colorView}, {Binding: 1, TextureView: r.idView}, {Binding: 2, Buffer: r.uniform, Size: uniformSize}}, true)
	return e
}
func (r *Renderer) Encode(encoder *wgpu.CommandEncoder, view *wgpu.TextureView, width, height int, camera [3]float32, alpha float32, s fixture.Snapshot) (c Counters, err error) {
	beforeCreations := r.creations
	defer func() { c.PersistentCreations = int(r.creations - beforeCreations) }()
	if r.closed {
		return c, fmt.Errorf("renderer closed")
	}
	if width == 0 || height == 0 {
		return
	}
	if width != r.width || height != r.height {
		return c, fmt.Errorf("host must establish completion and Resize before extent change")
	}
	if len(s.Instances) != len(r.scene.Instances) || len(s.Previous) != len(s.Instances)*3 || len(s.Current) != len(s.Previous) {
		return c, fmt.Errorf("snapshot topology changed")
	}
	if s.Generation != r.generation {
		packPoses(r.poseBytes, s.Previous, s.Current)
		packInstances(r.instanceBytes, s.Instances)
		if err = r.q.WriteBuffer(r.endpoints, 0, r.poseBytes); err != nil {
			return
		}
		if err = r.q.WriteBuffer(r.instances, 0, r.instanceBytes); err != nil {
			return
		}
		r.generation = s.Generation
		c.EndpointBytes = len(r.poseBytes) + len(r.instanceBytes)
		c.UploadBytes = c.EndpointBytes
	}
	for i, x := range camera {
		putFloat(r.uniformBytes[:], i*4, x)
	}
	putFloat(r.uniformBytes[:], 16, float32(width))
	putFloat(r.uniformBytes[:], 20, float32(height))
	putFloat(r.uniformBytes[:], 32, alpha)
	putFloat(r.uniformBytes[:], 36, float32(len(s.Previous)))
	if err = r.q.WriteBuffer(r.uniform, 0, r.uniformBytes[:]); err != nil {
		return
	}
	c.UploadBytes += uniformSize
	p, e := encoder.BeginComputePass(nil)
	if e != nil {
		err = e
		return
	}
	p.SetPipeline(r.compute)
	p.SetBindGroup(0, r.computeGroup, nil)
	p.Dispatch(uint32((len(s.Previous)+63)/64), 1, 1)
	if err = p.End(); err != nil {
		return
	}
	c.Dispatches = 1
	pass, e := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: r.colorView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{A: 1}}, {View: r.idView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: r.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	if e != nil {
		err = e
		return
	}
	pass.SetPipeline(r.world)
	pass.SetBindGroup(0, r.worldGroup, nil)
	pass.SetIndexBuffer(r.indices, gputypes.IndexFormatUint32, 0)
	for _, b := range r.batches {
		pass.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: b.indexCount, InstanceCount: b.instanceCount, FirstIndex: b.firstIndex, BaseVertex: b.baseVertex, FirstInstance: b.firstInstance})
		c.Draws++
		c.Triangles += int(b.indexCount * b.instanceCount / 3)
	}
	if err = pass.End(); err != nil {
		return
	}
	pass, e = encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: view, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if e != nil {
		err = e
		return
	}
	pass.SetPipeline(r.composite)
	pass.SetBindGroup(0, r.compositeGroup, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	if err = pass.End(); err != nil {
		return
	}
	c.Draws++
	pass, e = encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: view, LoadOp: gputypes.LoadOpLoad, StoreOp: gputypes.StoreOpStore}}})
	if e != nil {
		err = e
		return
	}
	pass.SetPipeline(r.overlay)
	pass.SetBindGroup(0, r.compositeGroup, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1})
	err = pass.End()
	c.Draws++
	c.Instances = len(s.Instances)
	c.Pieces = c.Instances * 3
	return
}
func (r *Renderer) Resources() Resources { return r.live }

// DiagnosticResources borrows targets for untimed asynchronous readback; no ownership transfer.
func (r *Renderer) DiagnosticResources() (*wgpu.Buffer, *wgpu.Texture, *wgpu.Texture, *wgpu.Texture) {
	return r.poses, r.color, r.identity, r.depth
}
func (r *Renderer) releaseList(xs []release) {
	for i := len(xs) - 1; i >= 0; i-- {
		xs[i].close()
		r.live.Count--
		r.live.Bytes -= xs[i].bytes
	}
}
func (r *Renderer) Close() {
	if r.closed {
		return
	}
	r.closed = true
	r.releaseList(r.targets)
	r.releaseList(r.owned)
	r.targets = nil
	r.owned = nil
	r.poseBytes = nil
	r.instanceBytes = nil
}
