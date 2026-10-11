package measure

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMissingClocksAndWarmupBoundary(t *testing.T) {
	rows := []Frame{{Index: 239, CallbackInterval: Number(800)}, {Index: 240, CallbackInterval: Number(900), Prepare: Number(0)}, {Index: 241, CallbackInterval: Number(8.333)}}
	s := Summary(rows, map[string]string{"gpu_ms": "timestamps unavailable"}, 120)
	stats := s["metrics"].(map[string]Stats)
	if stats["callback_interval_ms"].Samples != 1 || *stats["callback_interval_ms"].Max != 8.333 {
		t.Fatal(stats)
	}
	if stats["gpu_ms"].Available || stats["gpu_ms"].P95 != nil || stats["gpu_ms"].Reason == "" {
		t.Fatal(stats)
	}
	b, e := json.Marshal(Value{})
	if e != nil || string(b) != "null" {
		t.Fatal(string(b), e)
	}
	csv, e := CSV(rows)
	if e != nil || strings.Contains(csv, "800") || strings.Contains(csv, "900") || !strings.Contains(csv, "null") {
		t.Fatal(csv, e)
	}
}
func TestCannotSubstitutePartialCPUWorkOrPhysicalSize(t *testing.T) {
	s := Summary([]Frame{{Index: 240, Prepare: Number(.1), Pack: Number(.2), Encode: Number(.3)}}, map[string]string{"cpu_work_ms": "native submission unavailable"}, 120)
	if s["metrics"].(map[string]Stats)["cpu_work_ms"].Available {
		t.Fatal(s)
	}
	if ValidateDimensions(960, 540, 1920, 1080) == nil {
		t.Fatal("accepted CSS dimensions")
	}
}
