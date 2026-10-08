//go:build darwin && arm64

package metalproto

import (
	"fmt"
	"os"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// gbTiming is the per-pass GPU timing diagnostic (timing.inc). The pure-Go
// renderer does not sample pass counters yet; NANOLATHE_METAL_PASS_TIMING
// needs the Objective-C bridge (--metal-library).
type gbTiming struct{ enabled bool }
type gbTimingFrame struct{}

func newGBTiming(mtl.Device) *gbTiming {
	if os.Getenv("NANOLATHE_METAL_PASS_TIMING") != "" {
		fmt.Fprintln(os.Stderr, "nanolathe: Metal pass timing needs the Objective-C bridge; the pure-Go renderer records whole-frame GPU time only")
	}
	return &gbTiming{}
}
func (t *gbTiming) attachRender(mtl.PassDescriptor, string) {}
func (t *gbTiming) compute(mtl.CommandBuffer, string, uint) (mtl.ComputeEncoder, bool) {
	return 0, false
}
func (t *gbTiming) blit(mtl.CommandBuffer, string) (mtl.BlitEncoder, bool) { return 0, false }
func (t *gbTiming) begin(mtl.Device, int) *gbTimingFrame                   { return nil }
func (t *gbTiming) complete(*gbTimingFrame)                                {}
func (t *gbTiming) write()                                                 {}

func lookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
