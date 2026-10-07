package metalproto

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type nativeRow struct {
	Submit, Encode, Wait, DrawableWait, GPUStart, GPUEnd, Completed, Presented float64
	Memory                                                                     uint64
	Status, Reserved                                                           int32
}

func percentiles(values []float64) map[string]any {
	if len(values) == 0 {
		return map[string]any{"samples": 0, "available": false}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	at := func(q float64) float64 { return sorted[int(float64(len(sorted)-1)*q)] }
	return map[string]any{"samples": len(sorted), "available": true, "p50_ms": at(.5), "p95_ms": at(.95), "p99_ms": at(.99), "max_ms": sorted[len(sorted)-1]}
}
func makeReport(rows []nativeRow, o Options, s *meshscene.Scene, device map[string]any) map[string]any {
	var encode, wait, drawable, gpu, interval, present, latency []float64
	var maxMemory uint64
	presentedRows := 0
	for i, r := range rows {
		encode = append(encode, r.Encode)
		wait = append(wait, r.Wait)
		drawable = append(drawable, r.DrawableWait)
		if r.GPUStart > 0 && r.GPUEnd >= r.GPUStart {
			gpu = append(gpu, (r.GPUEnd-r.GPUStart)*1000)
		}
		if i > 0 {
			interval = append(interval, (r.Submit-rows[i-1].Submit)*1000)
		}
		if r.Presented > 0 {
			presentedRows++
			latency = append(latency, (r.Presented-r.Submit)*1000)
			if i > 0 && rows[i-1].Presented > 0 {
				present = append(present, (r.Presented-rows[i-1].Presented)*1000)
			}
		}
		if r.Memory > maxMemory {
			maxMemory = r.Memory
		}
	}
	return map[string]any{
		"backend": "retained Metal prototype via purego Objective-C bridge", "scene": s.Name, "scene_metadata": s.Metadata, "width": o.Width, "height": o.Height, "frames": len(rows), "warmup_frames": o.Warmup, "fps_cap": o.FPS, "vsync": o.VSync, "offscreen": o.Offscreen, "effects": o.Effects, "device": device,
		"cpu_encode": percentiles(encode), "inflight_wait": percentiles(wait), "drawable_wait": percentiles(drawable), "gpu_execution": percentiles(gpu), "submission_interval": percentiles(interval), "presentation_interval": percentiles(present), "submit_to_present": percentiles(latency), "peak_device_allocated_bytes": maxMemory,
		"presented_rows": presentedRows, "missing_presentation_rows": len(rows) - presentedRows, "presentation_available_fraction": float64(presentedRows) / float64(len(rows)), "presentation_drain_timeout_ms": 250,
		"output_dir": o.OutputDir, "max_inflight": 3, "pose_samples_hz": 30, "pose_frames": len(s.Frames), "instances": len(s.Instances),
		"approximations": []string{"Immutable sampled pose loop, no live battle or simulation writes; last-to-first pose wrap can be discontinuous", "Column-wise matrix lerp in vertex shader; large rotations are not rigid quaternion interpolation; pieces hidden at either endpoint are suppressed, so visibility snaps by up to one sample", "Conventional orthographic depth on independent z+y axis and directional light; no retail palette composition or model shadows", "Effects are eight synthetic point lights, two half-resolution bloom blur passes, four synthetic shock rings", "FPS uses host timer pacing with missed slots skipped after stalls; CAMetalLayer vsync and drawable backpressure determine actual display cadence", "GPU execution is command-buffer GPU start/end, not per-pass hardware counters", "Native allocations are reported through Metal currentAllocatedSize; Go allocations use runtime.MemStats; Objective-C allocation counts unavailable", "Presentation callbacks receive a bounded 250 ms drain outside measurement; missing/zero timestamps are counted and excluded from intervals; offscreen has no presentation timestamps"},
	}
}
func writeArtifacts(dir string, report map[string]any, rows []nativeRow, warmup, firstFrame int) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "report.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "frames.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write([]string{"frame", "warmup", "submit_seconds", "encode_ms", "inflight_wait_ms", "drawable_wait_ms", "gpu_start_seconds", "gpu_end_seconds", "gpu_ms", "completed_seconds", "presented_seconds", "device_allocated_bytes", "command_status"}); err != nil {
		return err
	}
	number := func(v float64) string { return strconv.FormatFloat(v, 'f', 9, 64) }
	for i, r := range rows {
		if err = w.Write([]string{strconv.Itoa(i + firstFrame), strconv.FormatBool(i+firstFrame < warmup), number(r.Submit), number(r.Encode), number(r.Wait), number(r.DrawableWait), number(r.GPUStart), number(r.GPUEnd), number((r.GPUEnd - r.GPUStart) * 1000), number(r.Completed), number(r.Presented), fmt.Sprint(r.Memory), fmt.Sprint(r.Status)}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
