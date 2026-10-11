//go:build !js

package host

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func ProcessMemory() map[string]any {
	out, e := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if e != nil {
		return map[string]any{"rss_bytes": nil, "reason": e.Error()}
	}
	n, e := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if e != nil {
		return map[string]any{"rss_bytes": nil, "reason": e.Error()}
	}
	return map[string]any{"rss_bytes": n * 1024, "source": "ps resident KiB"}
}
