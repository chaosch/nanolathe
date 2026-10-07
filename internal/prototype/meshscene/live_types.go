package meshscene

// LiveSource publishes presentation data from a running ordinary session.
// Next is called on the render thread; Close stops and joins its worker.
// Returned slices remain immutable through the next Next call.
type LiveSource interface {
	Next(seconds float64, input Input) (LiveFrame, error)
	Close() map[string]any
}

type Input struct {
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
	Previous, Current []Mat4
	Instances         []Instance
	Sprites           []Sprite
	Lights            []Light
	Camera            [3]float32
	Alpha             float32
	Tick              uint32
}

type BattleOptions struct {
	LoadOptions
	ArmySize, Preticks int
	Seed               uint32
	Width, Height      int
	Zoom               float32
}
