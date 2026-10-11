package battlegpu

import "github.com/nanolathe-gg/nanolathe/internal/meshscene"

// This authored presentation probe is opt-in and never mutates a borrowed
// stock packet. It inserts one cropped lens midway through the effects phase,
// so the real native copy/resume path observes both earlier and later paint.
type battleLensFixture struct {
	stock   meshscene.StockFrame
	layers  []meshscene.EffectLayer
	lenses  []meshscene.EffectLens
	ops     []meshscene.EffectOp
	samples [1]meshscene.EffectLensSample
}

func (p *battleLensFixture) apply(f *meshscene.LiveFrame) {
	if f.StockEffects == nil || len(f.StockEffects.Layers) < 3 || f.StockEffects.Scale <= 0 {
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
