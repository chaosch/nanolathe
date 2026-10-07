//go:build darwin

package metalproto

import _ "embed"

// Concatenate after facesShaderSource: the ordered budget pass reads the same
// prepared face records; no pose/corner upload or CPU flag readback is needed.
//
//go:embed native/face_reflections.metal
var faceReflectionsShaderSource string
