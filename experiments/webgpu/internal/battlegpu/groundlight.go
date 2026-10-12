package battlegpu

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed groundlight.wgsl
var groundLightShader string

const (
	groundLightLimit        = 64
	groundLightSourceStride = 64
	groundLightDiscStride   = 32
	groundLightDivisor      = 2
)

type GroundLightCounters struct{ Draws, Dispatches, UploadBytes int }
type groundLightDisc struct {
	rect   [4]float32
	source uint32
}

// GroundLight owns the Enhanced half-resolution terrain field (§31.8). Prepare
// and Close require host completion of prior work. Output is borrowed until the
// next Prepare; a resized output requires the host to refresh its bindings.
type GroundLight struct {
	d                           *wgpu.Device
	q                           rendererQueue
	owned                       []func()
	closed, prepared, enabled   bool
	texture                     *wgpu.Texture
	view                        *wgpu.TextureView
	textureWidth, textureHeight int
	sources, discs, uniform     *wgpu.Buffer
	pipeline                    *wgpu.RenderPipeline
	group                       *wgpu.BindGroup
	discCount                   int
	rect                        [4]float32
	legacyMissing               int
	counters                    GroundLightCounters
	sourceBytes                 [groundLightLimit * groundLightSourceStride]byte
	discBytes                   [groundLightLimit * groundLightDiscStride]byte
}

func groundLightError(reason string) error {
	return fmt.Errorf("nanolathe: WebGPU ground lighting failed: logical path retained terrain lights, providers searched [meshscene production lighting], expected %s", reason)
}
func groundLightFinite(value float32) bool { return math.Float32bits(value)&0x7f800000 != 0x7f800000 }
func groundLightVec(b []byte, at int, value [4]float32) {
	for i, v := range value {
		binary.LittleEndian.PutUint32(b[at+i*4:], math.Float32bits(v))
	}
}
func groundLightPackSource(b []byte, light meshscene.RetainedLight) {
	groundLightVec(b, 0, light.PositionRadius)
	groundLightVec(b, 16, light.ColorGroundGain)
	groundLightVec(b, 32, light.GroundAgeFadeKind)
	for i, flag := range light.Flags {
		binary.LittleEndian.PutUint32(b[48+i*4:], flag)
	}
}
func groundLightCapacity(need, current int) int {
	size := max(64, current)
	for size < need {
		size *= 2
	}
	return size
}

// groundLightPlan mirrors the production clipped disc union and outward field
// rounding (§31.3, §31.8). Retained sources have already undergone the one world
// replay transform; their gain includes all timing, family and player settings.
func groundLightPlan(f meshscene.LiveFrame, width, height int) ([]groundLightDisc, [4]float32, error) {
	var rect [4]float32
	if len(f.Lighting.Lights) > groundLightLimit {
		return nil, rect, groundLightError("the complete production 64-source budget")
	}
	if f.LightControls&1 == 0 {
		return nil, rect, nil
	}
	var discs []groundLightDisc
	xMin, yMin, xMax, yMax := float32(width), float32(height), float32(0), float32(0)
	for i, light := range f.Lighting.Lights {
		if light.Flags[1] == 0 {
			continue
		}
		for _, lane := range [][4]float32{light.PositionRadius, light.ColorGroundGain} {
			for _, v := range lane {
				if !groundLightFinite(v) {
					return nil, rect, groundLightError("finite retained source position, radius, colour and gain")
				}
			}
		}
		if !groundLightFinite(light.GroundAgeFadeKind[0]) {
			return nil, rect, groundLightError("finite producer ground height")
		}
		if light.ColorGroundGain[3] <= 0 {
			continue
		}
		if light.ColorGroundGain[0] < 0 || light.ColorGroundGain[1] < 0 || light.ColorGroundGain[2] < 0 {
			return nil, rect, groundLightError("nonnegative displayed source emission")
		}
		if max(light.ColorGroundGain[0], max(light.ColorGroundGain[1], light.ColorGroundGain[2])) <= 0 {
			continue
		}
		x, y := light.PositionRadius[0], light.PositionRadius[1]-light.PositionRadius[2]*0.5
		radius := light.PositionRadius[3]
		h := max(light.PositionRadius[2]-light.GroundAgeFadeKind[0], float32(0))
		if !groundLightFinite(y) || !groundLightFinite(h) || !groundLightFinite(radius*radius) {
			return nil, rect, groundLightError("representable projected position and squared distance")
		}
		for _, value := range light.ColorGroundGain[:3] {
			if !groundLightFinite(value * light.ColorGroundGain[3]) {
				return nil, rect, groundLightError("representable source energy after terrain gain")
			}
		}
		if radius <= 0 || h >= radius {
			continue
		}
		x0 := max(float32(math.Floor(float64(x-radius))), float32(0))
		y0 := max(float32(math.Floor(float64(y-radius))), float32(0))
		x1 := min(float32(math.Ceil(float64(x+radius))), float32(width))
		y1 := min(float32(math.Ceil(float64(y+radius))), float32(height))
		if x0 >= x1 || y0 >= y1 {
			continue
		}
		xMin, yMin, xMax, yMax = min(xMin, x0), min(yMin, y0), max(xMax, x1), max(yMax, y1)
		fx0, fy0 := float32(math.Floor(float64(x0/groundLightDivisor))), float32(math.Floor(float64(y0/groundLightDivisor)))
		fx1, fy1 := float32(math.Ceil(float64(x1/groundLightDivisor))), float32(math.Ceil(float64(y1/groundLightDivisor)))
		discs = append(discs, groundLightDisc{[4]float32{fx0, fy0, fx1 - fx0, fy1 - fy0}, uint32(i)})
	}
	if len(discs) > 0 {
		rect = [4]float32{xMin, yMin, xMax - xMin, yMax - yMin}
	}
	return discs, rect, nil
}

func NewGroundLight(d *wgpu.Device, q *wgpu.Queue, scene *meshscene.Scene) (g *GroundLight, err error) {
	if d == nil || q == nil || scene == nil {
		return nil, groundLightError("device, queue and retained scene")
	}
	g = &GroundLight{d: d, q: q}
	defer func() {
		if err != nil {
			g.Close()
			g = nil
		}
	}()
	buffer := func(label string, size uint64, usage gputypes.BufferUsage) (*wgpu.Buffer, error) {
		b, e := d.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: size, Usage: usage | wgpu.BufferUsageCopyDst})
		if e == nil {
			g.owned = append(g.owned, b.Release)
		}
		return b, e
	}
	if g.sources, err = buffer("battle-ground-light-sources", groundLightLimit*groundLightSourceStride, wgpu.BufferUsageStorage); err != nil {
		return
	}
	if g.discs, err = buffer("battle-ground-light-discs", groundLightLimit*groundLightDiscStride, wgpu.BufferUsageStorage); err != nil {
		return
	}
	if g.uniform, err = buffer("battle-ground-light-uniforms", 256, wgpu.BufferUsageUniform); err != nil {
		return
	}
	entries := []gputypes.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage, MinBindingSize: groundLightLimit * groundLightSourceStride}},
		{Binding: 1, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage, MinBindingSize: groundLightLimit * groundLightDiscStride}},
		{Binding: 2, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: 16}},
	}
	layout, err := d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if err != nil {
		return
	}
	g.owned = append(g.owned, layout.Release)
	pl, err := d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{layout}})
	if err != nil {
		return
	}
	g.owned = append(g.owned, pl.Release)
	module, err := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: groundLightShader})
	if err != nil {
		return
	}
	g.owned = append(g.owned, module.Release)
	// Production RGBA8 screen accumulation rounds after each stable-order source.
	target := gputypes.ColorTargetState{Format: wgpu.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll, Blend: &gputypes.BlendState{
		Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrc, Operation: gputypes.BlendOperationAdd},
		Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
	}}
	g.pipeline, err = d.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Layout: pl, Vertex: wgpu.VertexState{Module: module, EntryPoint: "ground_vertex"}, Fragment: &wgpu.FragmentState{Module: module, EntryPoint: "ground_fragment", Targets: []gputypes.ColorTargetState{target}}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}})
	if err != nil {
		return
	}
	g.owned = append(g.owned, g.pipeline.Release)
	g.group, err = d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.sources}, {Binding: 1, Buffer: g.discs}, {Binding: 2, Buffer: g.uniform, Size: 16}}})
	if err != nil {
		return
	}
	g.owned = append(g.owned, g.group.Release)
	return
}

func (g *GroundLight) groundLightReleaseTarget() {
	if g.view != nil {
		g.view.Release()
		g.view = nil
	}
	if g.texture != nil {
		g.texture.Release()
		g.texture = nil
	}
}
func (g *GroundLight) groundLightResize(width, height int) error {
	w, h := groundLightCapacity(width, g.textureWidth), groundLightCapacity(height, g.textureHeight)
	if g.view != nil && w == g.textureWidth && h == g.textureHeight {
		return nil
	}
	t, e := g.d.CreateTexture(&wgpu.TextureDescriptor{Label: "battle-ground-light-field", Size: wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA8Unorm, Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc})
	if e != nil {
		return e
	}
	v, e := g.d.CreateTextureView(t, nil)
	if e != nil {
		t.Release()
		return e
	}
	g.groundLightReleaseTarget()
	g.texture, g.view, g.textureWidth, g.textureHeight = t, v, w, h
	return nil
}

func (g *GroundLight) Prepare(f meshscene.LiveFrame, width, height int) error {
	if g == nil || g.closed {
		return groundLightError("open ground-light component")
	}
	g.prepared, g.enabled = false, false
	g.counters = GroundLightCounters{}
	g.rect = [4]float32{}
	g.discCount, g.legacyMissing = 0, 0
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return groundLightError("positive viewport within the default core 8192 texture dimensions")
	}
	discs, rect, e := groundLightPlan(f, width, height)
	if e != nil {
		return e
	}
	if f.LightControls&1 == 0 {
		g.legacyMissing = len(f.Lights)
	}
	if len(discs) == 0 {
		g.prepared = true
		return nil
	}
	if e = g.groundLightResize((width+1)/groundLightDivisor, (height+1)/groundLightDivisor); e != nil {
		return e
	}
	for i, l := range f.Lighting.Lights {
		at := i * groundLightSourceStride
		groundLightPackSource(g.sourceBytes[at:at+groundLightSourceStride], l)
	}
	clear(g.discBytes[:])
	for i, d := range discs {
		groundLightVec(g.discBytes[:], i*groundLightDiscStride, d.rect)
		binary.LittleEndian.PutUint32(g.discBytes[i*groundLightDiscStride+16:], d.source)
	}
	var u [16]byte
	groundLightVec(u[:], 0, [4]float32{float32(g.textureWidth), float32(g.textureHeight), 0, 0})
	for _, upload := range []struct {
		buffer *wgpu.Buffer
		data   []byte
	}{{g.sources, g.sourceBytes[:len(f.Lighting.Lights)*groundLightSourceStride]}, {g.discs, g.discBytes[:len(discs)*groundLightDiscStride]}, {g.uniform, u[:]}} {
		if e = g.q.WriteBuffer(upload.buffer, 0, upload.data); e != nil {
			return e
		}
		g.counters.UploadBytes += len(upload.data)
	}
	g.discCount, g.rect, g.enabled, g.prepared = len(discs), rect, true, true
	return nil
}

func (g *GroundLight) Encode(enc *wgpu.CommandEncoder) error {
	if g == nil || g.closed || !g.prepared || enc == nil {
		return groundLightError("prepared terrain lights and borrowed encoder")
	}
	if !g.enabled {
		return nil
	}
	// Clear the entire owned field, including spare capacity. This independent
	// prototype does not pack the field beside a world copy; no stale texel can
	// reach the resolve's clamped taps after the light bounds shrink (§31.8).
	p, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: g.view, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if e != nil {
		return e
	}
	p.SetPipeline(g.pipeline)
	p.SetBindGroup(0, g.group, nil)
	p.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(g.discCount)})
	e = p.End()
	if e == nil {
		g.counters.Draws++
	}
	return e
}

// Output is RGBA8Unorm Q=1-exp(-sum(E)). At physical pixel p, sample q=p/2-0.5
// with four bilinear textureLoad taps clamped to the ACTIVE ceil(viewport/2)
// extent, never the geometrically larger textureDimensions. Alpha is not terrain
// coverage. No output exists before the first lit Prepare; skip it when disabled.
func (g *GroundLight) Output() *wgpu.TextureView {
	if g == nil || g.closed {
		return nil
	}
	return g.view
}

// Rect is the clipped disc union in physical pixels: x,y,width,height. The host
// applies the §31.8 colour resolve once there after water and before objects/fog.
func (g *GroundLight) Rect() [4]float32 {
	if g == nil || g.closed {
		return [4]float32{}
	}
	return g.rect
}
func (g *GroundLight) Enabled() bool { return g != nil && !g.closed && g.prepared && g.enabled }
func (g *GroundLight) Counters() GroundLightCounters {
	if g == nil {
		return GroundLightCounters{}
	}
	return g.counters
}

// Missing identifies the older 32-byte lights, which carry neither ground height
// nor production terrain-only gain. They cannot substitute for retained sources.
func (g *GroundLight) Missing() map[string]int {
	out := map[string]int{}
	if g != nil && g.legacyMissing > 0 {
		out["legacy_ground_lights"] = g.legacyMissing
	}
	return out
}
func (g *GroundLight) Close() {
	if g == nil || g.closed {
		return
	}
	g.closed = true
	g.groundLightReleaseTarget()
	for i := len(g.owned) - 1; i >= 0; i-- {
		g.owned[i]()
	}
	g.owned = nil
}
