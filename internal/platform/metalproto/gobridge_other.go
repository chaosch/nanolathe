//go:build darwin && !arm64 && !amd64

package metalproto

import "fmt"

func newGoRenderer(width, height int, offscreen, vsync, effects, visualPasses bool, capacity int, shader string) (nativeRenderer, error) {
	return nil, fmt.Errorf("metalproto: the pure-Go Metal renderer needs arm64 or amd64; pass --metal-library with the Objective-C bridge")
}
