package meshscene

import "github.com/nanolathe-gg/nanolathe/internal/drawlist"

// EffectOp is an ordered native replay span. Quads cannot cross a lens or
// geometry packet: lenses read the picture completed by all earlier commands
// [DESIGN_GPU_RENDERER C-G3]. First/Count index the corresponding layer slice.
type EffectOp struct {
	Kind         uint32
	First, Count int
}

const (
	EffectOpQuads uint32 = iota
	EffectOpLens
	EffectOpGeometry
)

// EffectLensSample carries the production executor's cropped destination and
// source spans, in logical framebuffer pixels. Before executing a lens, copy
// ReadRect from the completed composite; all of this lens's samples read that
// snapshot. KeyRGB compares rounded display RGB bytes, matching Enhanced's
// documented palette-identity limitation [DESIGN_GPU_RENDERER §2.5].
type EffectLensSample struct {
	Rect, Source [4]float32
}

type EffectLens struct {
	Samples  []EffectLensSample
	ReadRect [4]float32
	KeyRGB   [3]uint8
}

// EffectGeometry retains the production direct fragment/debris polygon packet
// and its recording-space transform. Geometry is independently owned by the
// recorded batch; the native host must draw it or report an explicit omission.
type EffectGeometry struct {
	Model                   drawlist.Model
	Scale, OffsetX, OffsetY float32
}

// EffectSources keeps the original Enhanced operands, including whole composite
// art before leaf blits. These are inputs for the existing glow/lighting/blast
// contracts (§19/23/25), not a second emitter or lifetime implementation.
// Art contains complete light sources; Sprites contains replayed leaves for
// glow and tinted receiver lighting. Do not gather both as independent lights.
type EffectSources struct {
	World   drawlist.WorldSpace
	Art     []drawlist.Sprite
	Sprites []drawlist.Sprite
	Lines   []drawlist.Line
	Nano    []drawlist.Fill
	Flashes []drawlist.Flash
	Halos   []drawlist.Halo
}

// EffectCounts counts recorded commands, including offscreen admitted commands.
// ProjectileModels are intentionally delegated; Geometry packets still require
// a native consumer. Unsupported families also return a preparation error.
type EffectCounts struct {
	Sprites, Lines, Fills, Flashes, Halos, Lenses, Geometry int
	ProjectileModels, Unsupported                           int
}

// EffectLayer is an ordered 2D recording between production world barriers.
// Quads use logical client pixels and the effects atlas. Params.y=1 is RGB
// multiplication; Params.z=1 decodes the atlas red byte as an LHT row, with
// factor min(2,1+row/30) and coverage in alpha. Normal sprites retain their
// ordinary palette colors and half-alpha. No glow is baked into body pixels.
type EffectLayer struct {
	Quads   []OverlayQuad
	Ops     []EffectOp
	Lenses  []EffectLens
	Models  []EffectGeometry
	Sources []EffectSources
	Counts  EffectCounts
}
