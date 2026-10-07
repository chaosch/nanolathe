package meshscene

// LiveSource publishes presentation data from a running ordinary session.
// Next is called on the render thread; Close stops and joins its worker.
// Returned slices remain immutable through the next Next call.
type LiveSource interface {
	Next(seconds float64, input Input) (LiveFrame, error)
	Close() map[string]any
}

type Input struct {
	Events      []NativeEvent
	PanX, PanZ  float32 // held arrow/WASD axes, -1..1
	Zoom        float32 // accumulated wheel delta
	PauseToggle bool
}

// Sprite is a 96-byte GPU instance. Positions are world x,y,z; Rect is
// screen-space offset x,y,width,height in world pixels before camera zoom.
// Live screen Y projects world z-y/2; UV is u0,v0,u1,v1. Color is straight RGBA; the shader premultiplies it.
// Params: emission, additive (0/1), screen rotation radians, normalized
// clip-depth bias (negative brings the sprite toward the camera).
type Sprite struct {
	Previous, Current       [4]float32
	Rect, UV, Color, Params [4]float32
}

// Light is 32 bytes. The native prototype accepts at most 32 per frame.
type Light struct {
	PositionRadius [4]float32
	ColorStrength  [4]float32
}

type LiveFrame struct {
	// Optional replacement of the retained UI atlas. The native host uploads it
	// only when this nonzero version changes; slices live until Next returns.
	OverlayTexture    Texture
	OverlayVersion    uint64
	Overlay           []OverlayQuad
	Previous, Current []Mat4
	Instances         []Instance
	Sprites           []Sprite
	Lights            []Light
	Distortions       []Distortion
	Fog               FogFrame
	Camera            [3]float32
	Alpha             float32
	Tick              uint32
}

type BattleOptions struct {
	Playable   bool
	Difficulty int
	ComputerAI string // classic or modern; host selection only
	LoadOptions
	ArmySize, Preticks int
	Seed               uint32
	Width, Height      int
	Zoom               float32
	ViewPlayer         int // -1 omniscient; otherwise committed perspective 0..9
}

// Material is a 16-byte immutable texture binding. Index zero is reserved.
// Kind 1 selects the clamped instance team frame; Kind 0 selects its first frame.
// UVs in TextureFrames include the atlas texel-centre convention.
type Material struct{ FirstFrame, FrameCount, Kind, Reserved uint32 }

// ModelVisual is 64 bytes copied with each instance. State is team frame,
// opacity, build remaining, shadow enabled. Bounds is world min Y, max Y,
// ground Y, reserved. Outline is RGBA; Emission is RGB plus strength.
// The all-zero value means a plain opaque instance with no extra passes.
type ModelVisual struct{ State, Bounds, Outline, Emission [4]float32 }

// HeightField is load-time immutable terrain height in world units, one sample
// per grid cell. Rect maps sample centres into world X/Z; no sim pointer escapes.
type HeightField struct {
	Width, Height int
	Values        []float32
	Rect          [4]float32
}

// FogFrame carries detached RGBA8 fog modulation: RGB multiply final world
// colour, A is unused. Empty data disables fog. Rect is in projected world X,
// Z-minus-half-height coordinates, matching the committed fog cache.
type FogFrame struct {
	Width, Height int
	RGBA          []byte
	Rect          [4]float32
}

// Distortion is 64 bytes. Positions are world xyz, with GPU interpolation.
// Kind Shape.w=0 is a blast: Shape.xyz=end radius, band half-width, base strength;
// Params.x is committed age in ticks. Kind 1 is a heat plume: Shape.xyz=half
// width, half height, strength; Params.xyz=committed clock ticks, phase, bottom
// screen-Y offset in world pixels from the projected anchor. The plume rises
// two half-heights from this bottom edge.
// The GPU samples clock/age at Params.x+Alpha-1, freezing when Alpha is 1.
// Source order is blast rings, wreck heat, vegetation heat; later coverage wins.
type Distortion struct{ Previous, Current, Shape, Params [4]float32 }
