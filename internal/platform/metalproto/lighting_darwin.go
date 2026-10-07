//go:build darwin

package metalproto

import _ "embed"

// LightingShaderSource is concatenated before the native entry-point source.
// These helpers allocate no resources or passes; the host owns bindings,
// record-space receiver geometry and placement before fog (GPU design §23,
// §31). Ground field accumulation/resolve follows water and precedes objects.
//
//go:embed native/lighting.metal
var LightingShaderSource string
