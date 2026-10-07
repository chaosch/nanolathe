package meshscene

// RetainedLight is the 64-byte production lighting transfer contract. Its lane
// shape permits conversion from gpurender.RetainedLight without a dependency on
// that executor. It replaces the prototype's independently selected 32-source
// Light payload when the host supplies production lighting (GPU design §23,
// §31). Do not truncate the production 64-family budget to the old limit.
//
// PositionRadius = record X, unsheared record Y, absolute scaled height, radius.
// ColorGroundGain = displayed emission RGB, terrain-only gain (already includes
// family share, age/fade and ground strength). GroundAgeFadeKind = source ground
// height, age, fade, family (explosion/nano/fire/projectile/wreck/spark).
// Flags = model receiver enabled, ground enabled, age known, fade known.
// The host must map native receivers into the SAME recording scale and camera
// origin before evaluation; projected Y = unsheared Y - height/2. Applying the
// world replay transform to both sources and receivers once preserves distances.
type RetainedLight struct {
	PositionRadius, ColorGroundGain, GroundAgeFadeKind [4]float32
	Flags                                              [4]uint32
}

// RetainedLightingFrame is host-owned upload storage; it is not authoritative
// state. Sources are gathered from one pinned production draw list and remain
// immutable through upload. The native bridge must bind their complete count.
type RetainedLightingFrame struct{ Lights []RetainedLight }
