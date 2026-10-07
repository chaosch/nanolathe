//go:build darwin

package metalproto

import _ "embed"

//go:embed native/water.metal
var waterShaderSource string
