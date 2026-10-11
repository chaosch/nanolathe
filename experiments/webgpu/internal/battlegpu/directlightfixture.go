package battlegpu

import (
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

// Opt-in authored light gutters qualify the direct color entry independently
// of whether a natural final packet happens to contain a lit debris face.
// Detached replay replacement preserves every retained projectile marker.
type battleDirectLightFixture struct {
	stock           meshscene.StockFrame
	layers          []meshscene.EffectLayer
	art             [4]byte
	quad            [1]meshscene.OverlayQuad
	models          [3]meshscene.EffectGeometry
	lights          [3]meshscene.RetainedLight
	ready, disabled bool
}

func (p *battleDirectLightFixture) apply(f *meshscene.LiveFrame) {
	if f.StockEffects == nil || len(f.StockEffects.Layers) < 3 {
		return
	}
	if !p.ready {
		p.art = [4]byte{255, 255, 255, 255}
		p.quad[0] = meshscene.OverlayQuad{Rect: [4]float32{0, 0, 120, 40}, UV: [4]float32{0, 0, 1, 1}, Color: [4]float32{0, 0, 0, 1}}
		for i, normal := range [][3]float32{{0, 0, 1}, {0, 0, -1}, {0, 0, 0}} {
			x := float32(4 + 36*i)
			face := drawlist.ModelFace{Color: 2, Normal: normal, Vertices: []drawlist.ModelVertex{{X: 0, Y: 0}, {X: 24, Y: 0}, {X: 24, Y: 24}, {X: 0, Y: 24}}}
			g := &drawlist.ModelGeometry{Width: 24, Height: 24, Scale: 1, Eligible: true, Faces: []drawlist.ModelFace{face}}
			m := meshscene.EffectGeometry{Model: drawlist.Model{Geometry: g}, Scale: 1, OffsetX: x, OffsetY: 4}
			for _, corner := range []int{0, 1, 2, 0, 2, 3} {
				v := face.Vertices[corner]
				m.Vertices = append(m.Vertices, meshscene.EffectVertex{PositionUV: [4]float32{x + float32(v.X) + .5, 4 + float32(v.Y) + .5, .5, .5}, Color: [4]float32{.25, .5, .125, .5}, Bounds: [4]float32{0, 0, 1, 1}})
			}
			p.models[i] = m
			enabled := uint32(1)
			if p.disabled {
				enabled = 0
			}
			p.lights[i] = meshscene.RetainedLight{PositionRadius: [4]float32{x + 12, 18.5, 5, 10}, ColorGroundGain: [4]float32{.25, .5, 1, 0}, Flags: [4]uint32{enabled}}
		}
		p.ready = true
	}
	p.layers = append(p.layers[:0], make([]meshscene.EffectLayer, len(f.StockEffects.Layers))...)
	for i, original := range f.StockEffects.Layers {
		layer := &p.layers[i]
		layer.RetainedModels, layer.ProjectileOrder = original.RetainedModels, original.ProjectileOrder
		for _, op := range original.Ops {
			if op.Kind == meshscene.EffectOpRetainedModel {
				layer.Ops = append(layer.Ops, op)
			}
		}
	}
	layer := &p.layers[2]
	layer.Quads, layer.Models = p.quad[:], p.models[:]
	layer.Ops = append(layer.Ops, meshscene.EffectOp{Kind: meshscene.EffectOpQuads, First: 0, Count: 1}, meshscene.EffectOp{Kind: meshscene.EffectOpGeometry, First: 0, Count: 3})
	p.stock = meshscene.StockFrame{Layers: p.layers, Atlas: meshscene.Texture{Width: 1, Height: 1, RGBA: p.art[:]}, Version: 0x7ffffffffffffffe, Scale: 1}
	f.StockEffects = &p.stock
	f.WaterSources = nil
	f.Lighting.Lights = p.lights[:]
	f.LightControls |= 1
}
