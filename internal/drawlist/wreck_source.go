package drawlist

import "image"

// WreckSource is the minimal production model metadata the retained wreck
// light gather reads. Bounds must be the production composition rectangle in
// projected record pixels (modelWorldBounds), not an approximate native mesh
// box. Emission is the already-resolved cooling/arrival RGB; consumers add no
// cooling curve, visibility decision or lifetime of their own (GPU design §28,
// §31). It lives here, below both the client and the GPU renderer, so the
// client records it without importing the renderer.
type WreckSource struct {
	Bounds             image.Rectangle
	Emission           [3]float32
	WorldHeight, Scale float32
	ShadowOnly         bool
}
