package main

import (
	"fmt"
	"os"

	"github.com/nanolathe-gg/nanolathe/internal/ui"
)

// measureProbe stages a restored battle for the renderer comparison on this
// measurement-only branch: PROBE_UNPAUSE resumes it (saves load paused) and
// PROBE_CAM=x,z jumps the camera. PROBE_NOEDGE (battle.go) pins edge scroll.
func measureProbe(b *battleSession) {
	if b == nil {
		return
	}
	if os.Getenv("PROBE_UNPAUSE") != "" {
		b.applyBattleSchedule(ui.BattleScheduleIntent{PauseSet: true, Pause: false})
	}
	if v := os.Getenv("PROBE_CAM"); v != "" && b.cam != nil {
		var x, z int32
		if _, err := fmt.Sscanf(v, "%d,%d", &x, &z); err == nil {
			b.cam.JumpTo(x, z)
			fmt.Fprintf(os.Stderr, "probe: camera %d,%d -> %d,%d zoom %v\n", x, z, b.cam.X, b.cam.Z, b.cam.Zoom)
		}
	}
}
