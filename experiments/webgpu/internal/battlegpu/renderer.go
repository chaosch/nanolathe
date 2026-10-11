// Package battlegpu is the isolated WebGPU real-battle executor. It reads only
// detached presentation data; the production client retains simulation/input.
package battlegpu

import (
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

type Renderer struct {
	d                    *wgpu.Device
	format               gputypes.TextureFormat
	models               *Models
	layers               *Layers
	world, depth         *wgpu.Texture
	worldView, depthView *wgpu.TextureView
	width, height        int
}
type Counters struct {
	Models ModelCounters
	Layers LayerCounters
}

func New(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, scene *meshscene.Scene) (r *Renderer, err error) {
	r = &Renderer{d: d, format: format}
	if r.models, err = NewModels(d, q, format, scene); err != nil {
		return
	}
	r.layers, err = NewLayers(d, q, format, scene)
	return
}
func (r *Renderer) targets(width, height int) error {
	if r.width == width && r.height == height {
		return nil
	}
	// Windowed prototype resize owns a completion boundary; no borrowed view
	// can survive target destruction. Steady-state draws take no idle wait.
	if r.world != nil {
		if err := r.d.WaitIdle(); err != nil {
			return err
		}
		r.closeTargets()
	}
	for _, v := range []struct {
		name    string
		format  gputypes.TextureFormat
		texture **wgpu.Texture
		view    **wgpu.TextureView
	}{{"battle-world", r.format, &r.world, &r.worldView}, {"battle-depth", wgpu.TextureFormatDepth32Float, &r.depth, &r.depthView}} {
		t, e := r.d.CreateTexture(&wgpu.TextureDescriptor{Label: v.name, Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: v.format, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding})
		if e != nil {
			return e
		}
		*v.texture = t
		tv, e := r.d.CreateTextureView(t, nil)
		if e != nil {
			return e
		}
		*v.view = tv
	}
	r.width, r.height = width, height
	return r.layers.SetWorldTexture(r.worldView)
}
func (r *Renderer) Encode(enc *wgpu.CommandEncoder, output *wgpu.TextureView, width, height int, f meshscene.LiveFrame) (Counters, error) {
	if width <= 0 || height <= 0 {
		return Counters{}, nil
	}
	if r == nil || r.models == nil || r.layers == nil {
		return Counters{}, fmt.Errorf("incomplete battle renderer")
	}
	if e := r.targets(width, height); e != nil {
		return Counters{}, e
	}
	if e := r.models.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	if e := r.layers.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	if e := r.models.Compute(enc); e != nil {
		return Counters{}, e
	}
	pass, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: r.worldView, LoadOp: wgpu.LoadOpClear, StoreOp: wgpu.StoreOpStore, ClearValue: wgpu.Color{A: 1}}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: r.depthView, DepthLoadOp: wgpu.LoadOpClear, DepthStoreOp: wgpu.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	if e != nil {
		return Counters{}, e
	}
	r.layers.DrawTerrain(pass)
	r.layers.DrawEffects(pass, 0)
	r.layers.DrawSprites(pass, 0)
	r.models.DrawShadows(pass)
	r.models.Draw(pass, meshscene.PhaseGround)
	r.layers.DrawSprites(pass, 1)
	r.layers.DrawEffects(pass, 1)
	r.layers.DrawEffects(pass, 2)
	r.models.Draw(pass, meshscene.PhaseEffects)
	r.layers.DrawEffects(pass, 3)
	r.models.Draw(pass, meshscene.PhaseAir)
	r.layers.DrawEffects(pass, 4)
	pass.End()
	pass.Release()
	final, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: output, LoadOp: wgpu.LoadOpClear, StoreOp: wgpu.StoreOpStore, ClearValue: wgpu.Color{A: 1}}}})
	if e != nil {
		return Counters{}, e
	}
	r.layers.DrawFog(final)
	r.layers.DrawOverlay(final)
	final.End()
	final.Release()
	return Counters{r.models.Counters(), r.layers.Counters()}, nil
}
func (r *Renderer) Missing() map[string]int {
	if r == nil || r.layers == nil {
		return nil
	}
	return r.layers.Missing()
}
func (r *Renderer) closeTargets() {
	if r.worldView != nil {
		r.worldView.Release()
		r.worldView = nil
	}
	if r.depthView != nil {
		r.depthView.Release()
		r.depthView = nil
	}
	if r.world != nil {
		r.world.Release()
		r.world = nil
	}
	if r.depth != nil {
		r.depth.Release()
		r.depth = nil
	}
}
func (r *Renderer) Close() {
	if r == nil {
		return
	}
	r.closeTargets()
	if r.layers != nil {
		r.layers.Close()
		r.layers = nil
	}
	if r.models != nil {
		r.models.Close()
		r.models = nil
	}
}
