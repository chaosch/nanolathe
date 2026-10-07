package meshscene

// NativeEvent preserves pointer/key edge order between native event pumps.
// Coordinates are top-left-origin target pixels; modifiers are Shift=1,
// Control=2, Alt=4, Command=8. Buttons are left=0, right=1, middle=2.
// Kind is move=1, down=2, up=3, key-down=4. Key is uppercase ASCII, or
// Escape=27, Enter=13, Backspace=8. Native key repeat may repeat key-down.
type NativeEvent struct {
	Kind, Key, Button, Modifiers uint32
	X, Y, WheelX, WheelY         float32
}

// PlayInput captures the camera associated with a native input sample so a
// queued click is not reinterpreted after subsequent camera movement.
type PlayInput struct {
	Event  NativeEvent
	Camera [3]float32
}

// OverlayQuad is a 64-byte display-only quad. Rect is target-pixel x/y/w/h;
// UV is normalized corners; Color is straight RGBA. Params.x selects the
// retained texture: zero OverlayAtlas, one terrain, two fog. Other params
// Params.y selects normal premultiplied blending (0) or destination RGB
// multiplication (1). Overlay runs after fog and never writes world depth.
type OverlayQuad struct{ Rect, UV, Color, Params [4]float32 }
