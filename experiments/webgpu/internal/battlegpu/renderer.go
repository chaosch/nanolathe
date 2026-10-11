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
	glow                 *Glow
	groundLight          *GroundLight
	distortions          *Distortions
	lenses               *Lenses
	reflections          *StockReflections
	world, depth         *wgpu.Texture
	worldView, depthView *wgpu.TextureView
	width, height        int
	paint                []battlePaint
	paintState           battleRenderState
}
type Counters struct {
	Models      ModelCounters
	Layers      LayerCounters
	Glow        GlowCounters
	GroundLight GroundLightCounters
	Distortions DistortionCounters
	Lenses      LensCounters
	Reflections ReflectionCounters
}

func New(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, scene *meshscene.Scene) (r *Renderer, err error) {
	// Production keeps effect accumulation in HDR until final fog/gain resolve.
	worldFormat := gputypes.TextureFormatRGBA16Float
	r = &Renderer{d: d, format: worldFormat}
	defer func() {
		if err != nil {
			r.Close()
		}
	}()
	if r.models, err = NewModels(d, q, worldFormat, scene); err != nil {
		return
	}
	r.layers, err = newLayersFormats(d, q, worldFormat, format, scene)
	if err == nil {
		r.layers.SetFeaturePaintOrder(true)
		r.layers.projectilePaintOrder = true
		r.layers.lensPaintOrder = true
		r.layers.coalesceEffects = true
		r.layers.state = &r.paintState
		r.models.state = &r.paintState
	}
	if err == nil {
		r.glow, err = NewGlow(d, q, scene)
		if err == nil {
			r.groundLight, err = NewGroundLight(d, q, scene)
		}
	}
	if err == nil {
		r.distortions, err = NewDistortions(d, q, format, layerFogShader)
	}
	if err == nil {
		r.lenses, err = NewLenses(d, q, worldFormat)
	}
	if err == nil {
		r.reflections, err = NewStockReflections(d, q, worldFormat)
	}
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
		t, e := r.d.CreateTexture(&wgpu.TextureDescriptor{Label: v.name, Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: v.format, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc})
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
	if e := r.glow.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	var glowValues [4]float32
	if r.glow.Enabled() {
		glowValues = [4]float32{f.StockEffects.Glow.Parameters.Blur[2], f.StockEffects.Glow.Parameters.Blur[3], 1, 0}
	}
	if e := r.groundLight.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	var lightValues [4]float32
	if r.groundLight.Enabled() {
		lightValues = [4]float32{float32((width + 1) / 2), float32((height + 1) / 2), 1, 0}
	}
	if e := r.layers.SetFields(r.glow.Output(), r.groundLight.Output(), glowValues, r.groundLight.Rect(), lightValues); e != nil {
		return Counters{}, e
	}
	if e := r.layers.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	if e := r.distortions.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	if e := r.distortions.SetInputs(r.layers.uniform, r.worldView, r.layers.fogGrid.v, r.layers.fogAtlas.v, r.layers.layerGlowTexture()); e != nil {
		return Counters{}, e
	}
	delete(r.layers.missing, "enhanced_distortion")
	if e := r.reflections.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	if e := r.reflections.SetInputs(r.layers.uniform, r.layers.effectAtlas.v, r.layers.waterMask.v); e != nil {
		return Counters{}, e
	}
	delete(r.layers.missing, "water_reflections_and_model_refraction")
	if f.Water.Mask[3] > .5 {
		r.layers.missing["model_water_reflection_refraction_and_seabed_promotion"] = 1
	}
	if e := r.lenses.Prepare(f, width, height); e != nil {
		return Counters{}, e
	}
	if e := r.models.Compute(enc); e != nil {
		return Counters{}, e
	}
	if e := r.groundLight.Encode(enc); e != nil {
		return Counters{}, e
	}
	if e := r.reflections.Encode(enc); e != nil {
		return Counters{}, e
	}
	beginWorld := func(load gputypes.LoadOp) (*wgpu.RenderPassEncoder, error) {
		return enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: r.worldView, LoadOp: load, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{A: 1}}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: r.depthView, DepthLoadOp: load, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	}
	pass, e := beginWorld(gputypes.LoadOpClear)
	if e != nil {
		return Counters{}, e
	}
	r.paintState.reset(pass)
	defer r.paintState.reset(nil)
	r.paint = battlePaintOrder(r.paint, f)
	paintPhase := func(phase uint32) {
		for _, p := range r.paint {
			if p.phase != phase {
				continue
			}
			if p.feature {
				r.layers.DrawFeature(pass, p.source)
			} else {
				r.models.DrawShadowSlot(pass, p.slot)
				r.layers.DrawAnnotations(pass, p.slot)
				r.models.DrawSlot(pass, p.slot)
			}
		}
	}
	r.layers.DrawTerrain(pass)
	r.reflections.Draw(pass)
	r.paintState.reset(pass)
	drawEffects := func(layer uint32) error {
		next, err := r.layers.drawEffectsReplay(pass, layer, func(slot uint32) { r.models.DrawSlot(pass, slot) }, func(index uint32) (*wgpu.RenderPassEncoder, error) {
			if err := pass.End(); err != nil {
				return pass, err
			}
			if err := r.lenses.Snapshot(enc, r.world, layer, index); err != nil {
				return pass, err
			}
			resumed, err := beginWorld(gputypes.LoadOpLoad)
			if err != nil {
				return pass, err
			}
			pass = resumed
			r.lenses.Draw(pass, layer, index)
			r.paintState.reset(pass)
			return pass, nil
		})
		pass = next
		return err
	}
	if e := drawEffects(0); e != nil {
		return Counters{}, e
	}
	r.layers.DrawLooseSprites(pass, 0)
	paintPhase(meshscene.ModelPhaseShortFeatures)
	if e := drawEffects(1); e != nil {
		return Counters{}, e
	}
	paintPhase(meshscene.ModelPhaseGround)
	r.layers.DrawLooseSprites(pass, 1)
	// Unrecorded fallback subjects retain their ordinary effects phase.
	// Stock projectile subjects commit at their original effect markers.
	paintPhase(meshscene.ModelPhaseEffects)
	if e := drawEffects(2); e != nil {
		return Counters{}, e
	}
	paintPhase(meshscene.ModelPhaseAir)
	if e := drawEffects(3); e != nil {
		return Counters{}, e
	}
	if e := drawEffects(4); e != nil {
		return Counters{}, e
	}
	if e := pass.End(); e != nil {
		return Counters{}, e
	}
	if e := r.glow.Encode(enc, r.worldView); e != nil {
		return Counters{}, e
	}
	final, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: output, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{A: 1}}}})
	if e != nil {
		return Counters{}, e
	}
	r.layers.DrawFog(final)
	r.distortions.Draw(final)
	r.paintState.reset(final)
	r.layers.DrawOverlay(final)
	if e := final.End(); e != nil {
		return Counters{}, e
	}
	return Counters{Models: r.models.Counters(), Layers: r.layers.Counters(), Glow: r.glow.Counters(), GroundLight: r.groundLight.Counters(), Distortions: r.distortions.Counters(), Lenses: r.lenses.Counters(), Reflections: r.reflections.Counters()}, nil
}
func (r *Renderer) Missing() map[string]int {
	if r == nil || r.layers == nil {
		return nil
	}
	out := make(map[string]int)
	for k, v := range r.layers.Missing() {
		out[k] = v
	}
	if r.groundLight != nil {
		for k, v := range r.groundLight.Missing() {
			out[k] += v
		}
	}
	return out
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
	if r.reflections != nil {
		r.reflections.Close()
		r.reflections = nil
	}
	if r.lenses != nil {
		r.lenses.Close()
		r.lenses = nil
	}
	if r.distortions != nil {
		r.distortions.Close()
		r.distortions = nil
	}
	r.closeTargets()
	if r.groundLight != nil {
		r.groundLight.Close()
		r.groundLight = nil
	}
	if r.glow != nil {
		r.glow.Close()
		r.glow = nil
	}
	if r.layers != nil {
		r.layers.Close()
		r.layers = nil
	}
	if r.models != nil {
		r.models.Close()
		r.models = nil
	}
}
