package meshscene

// RetainedGlowVertex is 48 bytes; identical to gpurender's CPU upload shape.
// PositionUV = framebuffer XY and atlas TEXEL XY, or record-local halo offsets.
// Color = solid displayed RGB, source gain. Params = operation (solid0,
// keyed1, row-byte flash2, halo3), brightness threshold, radius², halo high lane.
type RetainedGlowVertex struct{ PositionUV, Color, Params [4]float32 }
type RetainedGlowParameters struct {
	Weights [8]float32
	Blur    [4]float32
}

// Blur = near spacing, far sigma, near/far weights including global strength.
// Vertices/parameters are presentation-only; Encode reads the current completed
// composite for dynamic row emission, then resolves before ordinary fog.
type RetainedGlowFrame struct {
	Vertices   []RetainedGlowVertex
	Parameters RetainedGlowParameters
}
