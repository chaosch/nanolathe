package meshscene

import "time"

// LiveSource publishes presentation data from a running ordinary session.
// Next is called on the render thread or on the host's single prepare-ahead
// worker, never concurrently; Close stops and joins its worker. Returned
// slices remain immutable through the next Next call.
type LiveSource interface {
	Next(seconds float64, input Input) (LiveFrame, error)
	Close() map[string]any
}

type Input struct {
	Events      []NativeEvent
	PanX, PanZ  float32 // held arrow/WASD axes, -1..1
	Zoom        float32 // accumulated wheel delta
	PauseToggle bool
	// At is the instant the frame is prepared for. A frame prepared ahead
	// is sampled at its predicted draw start, not when its worker runs.
	At time.Time
	// Timing lists frames completed since the previous Input, oldest first,
	// for host diagnostics such as the +fps panel. Interactive runs only.
	Timing []FrameTiming
}

// FrameTiming is one submitted frame's measured phases in milliseconds.
// Presented is the display time in seconds on the host media clock, zero when
// the drawable was never shown. Next and Pack run on the prepare worker;
// Encode (staging copies plus command encoding) and Wait (ring and drawable
// waits) on the render thread; GPU is the command buffer's execution span.
type FrameTiming struct {
	Presented                     float64
	Next, Pack, Encode, Wait, GPU float64
}

// Annotation is one physical-pixel rectangle drawn in a composition slot's
// paint band before that slot's body, as presentUnit draws the selected-unit
// quad immediately before the model [03 R-WATER-01 §1]. Slot is one-based.
type Annotation struct {
	Slot  uint32
	Rect  [4]float32
	Color [4]float32
}

// Sprite is a 96-byte GPU instance. Positions are world x,y,z; Rect is
// screen-space offset x,y,width,height in world pixels before camera zoom.
// Live screen Y projects world z-y/2; UV is u0,v0,u1,v1. Color is straight RGBA; the shader premultiplies it.
// Current.w marks a short-feature sprite for the pre-model ground pass (1);
// otherwise zero. Previous.w is reserved.
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
	ModelProjections  ModelProjectionFrame
	TerrainFlags      uint32 // production fractional filtering (1), detail record step (2)
	StockEffects      *StockFrame
	Lighting          RetainedLightingFrame
	LightControls     uint32 // production lighting present, finish, glint, supersample
	Composition       ModelComposition
	Shadows           ShadowComposition
	WaterObjects      []WaterObject
	WaterSources      []WaterSource
	WaterObjectScales [3]float32
	Composed          bool

	Water      WaterUniform
	ModelRules []ModelRules

	PointerCaptured bool // production host requests relative pointer motion

	Exit bool // host requested an orderly window close after this input service
	// Optional replacement of the retained UI atlas. The native host uploads it
	// only when this nonzero version changes; slices live until Next returns.
	OverlayDirty   [4]uint32 // changed atlas rectangle x/y/width/height; empty means full texture
	OverlayTexture Texture
	OverlayVersion uint64
	Overlay        []OverlayQuad
	// CursorQuads index Overlay quads placed for the logical pointer CursorAt;
	// a host may move them to a later pointer sample. Event positions divide
	// by CursorScale, and quads move by whole logical pixels times it.
	CursorQuads       []int
	CursorAt          [2]int
	CursorScale       float32
	Annotations       []Annotation
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
	// DisplayPalette, when set, replaces PALETTE.PAL for all retained art so
	// models, features and effects carry the same gamma as the terrain.
	DisplayPalette *[256][4]byte
}

// Material is a 16-byte immutable texture binding. Index zero is reserved.
// Kind 1 selects the clamped instance team frame; Kind 0 selects its first frame.
// UVs in TextureFrames include the atlas texel-centre convention.
type Material struct{ FirstFrame, FrameCount, Kind, Reserved uint32 }

// ModelVisual is 64 bytes copied with each instance. State is team frame,
// opacity, build remaining, shadow enabled. Bounds is world min Y, max Y,
// ground Y, model origin Y. Outline is RGBA; Emission is RGB plus strength.
// The all-zero value means a plain opaque instance with no extra passes.
type ModelVisual struct{ State, Bounds, Outline, Emission [4]float32 }

// HeightField is load-time immutable terrain height in world units, one sample
// per grid cell. Rect maps sample centres into world X/Z; no sim pointer escapes.
type HeightField struct {
	Width, Height int
	Values        []float32
	Rect          [4]float32
}

// FogFrame carries one detached RGBA8 operation texel per committed fog cell.
// R encodes channel one (1 gray fill, 2 checker fill, 3+slot gray mask,
// 59+slot checker mask); G encodes channel zero (1 dark fill, 2+slot black mask).
// A is 255 and B is zero. Slots are variant*14+frame within each atlas family.
// Empty data disables fog. Rect is in projected world X, Z-minus-half-height
// coordinates and retains the full committed cache bounds, including its +16
// cell-origin offset. Dithered participates in the cached encoding's identity.
type FogFrame struct {
	Width, Height int
	RGBA          []byte
	Rect          [4]float32
	Dithered      bool
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
