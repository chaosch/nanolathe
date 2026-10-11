package battlegpu

import "github.com/nanolathe-gg/nanolathe/internal/meshscene"

// This authored presentation probe is opt-in and never mutates a borrowed
// stock packet. It inserts one cropped lens midway through the effects phase,
// so the real native copy/resume path observes both earlier and later paint.
type battleLensFixture struct {
	stock      meshscene.StockFrame
	layers     []meshscene.EffectLayer
	lenses     []meshscene.EffectLens
	ops        []meshscene.EffectOp
	samples    [1]meshscene.EffectLensSample
	controlled bool
	quads      [3]meshscene.OverlayQuad
	art        [4]byte
}

func (p *battleLensFixture) apply(f *meshscene.LiveFrame) {
	if f.StockEffects == nil || len(f.StockEffects.Layers) < 3 || f.StockEffects.Scale <= 0 {
		return
	}
	if p.controlled {
		// Authored source/destination gutters exclude ordinary world subjects. The
		// left half is HDR; the right half matches the lens key and must retain the
		// destination. This packet deliberately replaces stock replay for diagnosis.
		p.art = [4]byte{255, 255, 255, 255}
		p.layers = append(p.layers[:0], make([]meshscene.EffectLayer, 5)...)
		uv := [4]float32{0, 0, 1, 1}
		p.quads = [3]meshscene.OverlayQuad{
			{Rect: [4]float32{0, 0, 120, 60}, UV: uv, Color: [4]float32{.125, .25, .375, 1}},
			{Rect: [4]float32{0, 1010, 60, 60}, UV: uv, Color: [4]float32{2.5, .25, 1.5, 1}},
			{Rect: [4]float32{60, 1010, 60, 60}, UV: uv, Color: [4]float32{12.0 / 255, 23.0 / 255, 34.0 / 255, 1}},
		}
		p.samples[0] = meshscene.EffectLensSample{Rect: [4]float32{0, 0, 120, 60}, Source: [4]float32{0, 1010, 120, 60}}
		p.lenses = append(p.lenses[:0], meshscene.EffectLens{Samples: p.samples[:], ReadRect: p.samples[0].Source, KeyRGB: [3]uint8{12, 23, 34}})
		p.ops = append(p.ops[:0], meshscene.EffectOp{Kind: meshscene.EffectOpQuads, First: 0, Count: 3}, meshscene.EffectOp{Kind: meshscene.EffectOpLens, First: 0, Count: 1})
		p.layers[2] = meshscene.EffectLayer{Quads: p.quads[:], Ops: p.ops, Lenses: p.lenses}
		p.stock = meshscene.StockFrame{Layers: p.layers, Atlas: meshscene.Texture{Width: 1, Height: 1, RGBA: p.art[:]}, Version: 0x7fffffffffffffff, Scale: 1}
		f.StockEffects = &p.stock
		return
	}
	p.stock = *f.StockEffects
	p.layers = append(p.layers[:0], p.stock.Layers...)
	layer := &p.layers[2]
	scale := p.stock.Scale
	p.samples[0] = meshscene.EffectLensSample{Rect: [4]float32{360 / scale, 120 / scale, 200 / scale, 100 / scale}, Source: [4]float32{640 / scale, 180 / scale, 200 / scale, 100 / scale}}
	p.lenses = append(p.lenses[:0], layer.Lenses...)
	index := len(p.lenses)
	p.lenses = append(p.lenses, meshscene.EffectLens{Samples: p.samples[:], ReadRect: p.samples[0].Source, KeyRGB: [3]uint8{12, 23, 34}})
	split := len(layer.Ops) / 2
	p.ops = append(p.ops[:0], layer.Ops[:split]...)
	p.ops = append(p.ops, meshscene.EffectOp{Kind: meshscene.EffectOpLens, First: index, Count: 1})
	p.ops = append(p.ops, layer.Ops[split:]...)
	layer.Lenses, layer.Ops = p.lenses, p.ops
	layer.Counts.Lenses++
	p.stock.Layers = p.layers
	f.StockEffects = &p.stock
}
