//go:build darwin

package metalproto

import _ "embed"

// WaterTilesShaderSource must precede the water-object resolve entry point.
// Its helpers operate entirely in physical source-plane pixels. Concatenating
// after shared shader types is sufficient; it requires no renderer uniforms.
// The native bridge includes water_tiles.h/.inc and owns state plus one frame
// object per acquired ring. Encode after ending reflection-source rendering,
// then Bind before resolving on that same command buffer. All tile words are
// rewritten by the classification dispatch; ring lifetime prevents reuse races.
//
//go:embed native/water_tiles.metal
var WaterTilesShaderSource string
