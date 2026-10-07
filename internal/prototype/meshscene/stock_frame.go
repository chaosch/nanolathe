package meshscene

// StockFrame borrows one production recording through synchronous native upload.
type StockFrame struct {
	Layers  []EffectLayer
	Atlas   Texture
	Version uint64
	Dirty   [4]uint32
	Scale   float32
	Glow    RetainedGlowFrame
}
