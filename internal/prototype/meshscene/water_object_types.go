package meshscene

// WaterObject is one float4 per retained instance: reflection admission,
// physical sea Y, BLUE refraction admission and reserved. This contains no
// simulation state or geometry. Factory/cargo groups require isolated member
// images for reflections (§22.4); the native module reports/suppresses those
// groups rather than reflecting merged colour at a child's physical height.
type WaterObject struct{ Params [4]float32 }
