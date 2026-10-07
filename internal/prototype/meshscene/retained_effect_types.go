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
	EffectOpRetainedModel
	EffectOpGround
	EffectOpSmoke
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
	Vertices                []EffectVertex
	Suppressed              string
	QuadSpanFaces           uint32
	SupersampleIgnored      bool
}

// EffectVertex is a 48-byte projected triangle corner: physical packing scales
// PositionUV.xy once, leaves normalized UVs and texture bounds unchanged, and
// premultiplies Color only in the shader. Bounds prevent an out-of-frame model
// texel sample from reaching a neighboring retained atlas resource.
type EffectVertex struct {
	PositionUV, Color, Bounds [4]float32
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
	Quads           []OverlayQuad
	Ops             []EffectOp
	Lenses          []EffectLens
	Models          []EffectGeometry
	RetainedModels  []drawlist.Model
	ProjectileOrder []EffectProjectileModel // aligned to RetainedModels, preserved through culling
	Sources         []EffectSources
	Counts          EffectCounts
	Ground          []EffectGround
	Smoke           []EffectSmoke
}

// EffectSmoke preserves the production tinted-smoke receiver (§23.2). Quad
// contains the four clipped RECORD corners after the world replay transform,
// before framebuffer clipping; its alpha remains one half. Selection carries
// the transformed sprite anchor XY, max frame extent and absolute height.
// Bounds is the full art's normalized UV rectangle, so filtered taps cannot
// read neighboring atlas resources. Quad.Params.x selects the production
// fractional-detail filter; Params.yz retains the physical clipped span before
// world replay's minimum one-pixel raster expansion. Positions, spans and
// heights are logical pixels until native packing. Light selection is
// independent of glow and terrain controls.
type EffectSmoke struct {
	Quad              OverlayQuad
	Selection, Bounds [4]float32
}

// EffectProjectileModel names an actual recorded standalone call. Member is
// parent=0 or first header child=1. The retained projectile slot contains both
// pieces: resolve it at the parent marker once, then acknowledge the child
// marker without resolving the combined slot a second time [03 §5.4].
type EffectProjectileModel struct {
	ID     uint64
	Member uint32
}

// EffectGround is an 80-byte replay operand. CentreAxis is centre/along vector,
// CrossKind is cross vector/kind/reserved (0 trail, 1 scorch, 2 surface wake).
// Values retains age, opacity/strength, variant and shape. Mapping is painted
// map origin XY, inverse presentation scale and reserved; the native mask step
// comes from the already-uploaded production water mask. Clip is framebuffer
// xywh. All geometry is logical framebuffer pixels until native packing (§15,
// §26, §29); local quad coordinates and tuning are evaluated by the shader.
type EffectGround struct {
	CentreAxis, CrossKind, Values, Mapping, Clip [4]float32
}
