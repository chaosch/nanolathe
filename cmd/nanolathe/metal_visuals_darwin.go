//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/platform/gpurender"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/metalhud"
)

// One recorder/atlas per presentation owner; its source batches feed both the
// ordinary stock passes and production Enhanced lighting/glow without rerunning
// presentation RNG or compiling the retained unit meshes a second time [I6].
type metalVisuals struct {
	adapter        *metalhud.Foreground
	lighting       *gpurender.RetainedLighting
	glow           *gpurender.RetainedGlow
	palette        [256][4]byte
	epoch, version uint64
	frame          meshscene.StockFrame
	layers         [5]meshscene.EffectLayer
	lights         []meshscene.RetainedLight
	vertices       []meshscene.RetainedGlowVertex
}

func (v *metalVisuals) Prepare(cl *client.Client, live *meshscene.LiveFrame, width, height int, scale float32) error {
	display := cl.DisplayPalette()
	if v.adapter == nil || display != v.palette {
		var pal palette.Tables
		if source := cl.PaletteTables(); source != nil {
			pal = *source
		}
		pal.Base = display
		v.adapter = metalhud.New(&pal, width, height)
		v.palette = display
		v.epoch += v.version + 1
		v.version = 0
	}
	recorded, err := recordMetalEffects(cl, v.adapter)
	if err != nil {
		return err
	}
	v.layers = [5]meshscene.EffectLayer{recorded.BeforeFeatures, recorded.BeforeGround, recorded.AfterGround, recorded.AfterAir, recorded.AfterLabels}
	v.frame.Layers = v.layers[:]
	v.frame.Atlas = recorded.Atlas
	v.frame.Version = v.epoch + recorded.Version
	v.version = recorded.Version
	v.frame.Scale = scale
	for i, d := range recorded.Dirty {
		v.frame.Dirty[i] = uint32(d)
	}
	var world drawlist.WorldSpace
	for _, layer := range v.layers {
		if len(layer.Sources) > 0 {
			world = layer.Sources[0].World
			break
		}
	}
	weapons, nano, ground := cl.GlowFamilies()
	families := [3]int{weapons, nano, ground}
	settings := gpurender.RetainedLightingSettings{Effects: cl.Effects(), GroundStrength: cl.GroundLightStrength(), Families: families, Width: width, Height: height}
	if v.lighting == nil {
		v.lighting = gpurender.NewRetainedLighting(display, settings)
	} else {
		v.lighting.SetPalette(display)
		v.lighting.SetSettings(settings)
	}
	glowSettings := gpurender.RetainedGlowSettings{On: cl.Glow(), Strength: cl.GlowStrength(), Effects: cl.Effects(), Families: families, Width: width, Height: height, Palette: display}
	if v.glow == nil {
		v.glow = gpurender.NewRetainedGlow(glowSettings)
	} else {
		v.glow.SetSettings(glowSettings)
	}
	v.lighting.BeginSources(world)
	v.lighting.AppendWrecks(recorded.LightSources.Wrecks)
	v.glow.Begin(world)
	for _, layer := range v.layers {
		for _, source := range layer.Sources {
			v.lighting.AppendSources(source.Art, source.Lines, source.Nano, nil)
			if err := v.glow.Append(source.Sprites, source.Lines, source.Nano, source.Flashes, source.Halos, v.adapter.GlowAtlas()); err != nil {
				return err
			}
		}
	}
	k := float32(1)
	step := float32(camera.ZoomOf(world.Step)) / float32(camera.ZoomUnit)
	if step > 0 {
		if world.Factor > 0 {
			k = world.Factor / step
		} else if world.Zoom > 0 {
			k = float32(world.Zoom) / float32(camera.ZoomUnit) / step
		}
	}
	live.WaterObjectScales = [3]float32{step, step * k, scale}
	k *= scale
	v.lights = v.lights[:0]
	for _, source := range v.lighting.GatherSources() {
		l := meshscene.RetainedLight(source)
		l.PositionRadius[0] = l.PositionRadius[0]*k + world.OffsetX*scale
		l.PositionRadius[1] = l.PositionRadius[1]*k + world.OffsetY*scale
		l.PositionRadius[2] *= k
		l.PositionRadius[3] *= k
		l.GroundAgeFadeKind[0] *= k
		v.lights = append(v.lights, l)
	}
	live.Lighting.Lights = v.lights
	live.LightControls |= 1
	vertices, parameters := v.glow.Frame()
	v.vertices = v.vertices[:0]
	for _, vertex := range vertices {
		a := meshscene.RetainedGlowVertex(vertex)
		a.PositionUV[0] *= scale
		a.PositionUV[1] *= scale
		v.vertices = append(v.vertices, a)
	}
	v.frame.Glow.Vertices = v.vertices
	v.frame.Glow.Parameters = meshscene.RetainedGlowParameters(parameters)
	v.frame.Glow.Parameters.Blur[0] *= scale
	v.frame.Glow.Parameters.Blur[1] = min(v.frame.Glow.Parameters.Blur[1]*scale, float32(24.0/2.5))
	live.StockEffects = &v.frame
	return nil
}
