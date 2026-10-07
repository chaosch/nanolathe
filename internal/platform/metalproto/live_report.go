package metalproto

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type liveRow struct {
	Seconds                                                                   float64
	Alpha                                                                     float32
	Tick                                                                      uint32
	NextMS, PrepMS                                                            float64
	Instances, Poses, Sprites, Lights, DroppedLights, ModelDraws, SpriteDraws int
	ModelVertices, ModelIndices, UploadBytes                                  uint64
}

func quantities(values []float64) map[string]any {
	if len(values) == 0 {
		return map[string]any{"samples": 0}
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	var total float64
	for _, v := range values {
		total += v
	}
	return map[string]any{"samples": len(values), "min": ordered[0], "max": ordered[len(ordered)-1], "mean": total / float64(len(values)), "p50": ordered[(len(ordered)-1)/2], "p95": ordered[int(float64(len(ordered)-1)*.95)], "total": total}
}
func addLiveReport(report map[string]any, rows []liveRow) {
	next := make([]float64, len(rows))
	prep := make([]float64, len(rows))
	counts := map[string][]float64{}
	for i, r := range rows {
		next[i] = r.NextMS
		prep[i] = r.PrepMS
		for name, value := range map[string]float64{"instances": float64(r.Instances), "poses": float64(r.Poses), "sprites": float64(r.Sprites), "lights": float64(r.Lights), "dropped_lights": float64(r.DroppedLights), "model_draws": float64(r.ModelDraws), "sprite_draws": float64(r.SpriteDraws), "model_vertices": float64(r.ModelVertices), "model_triangles": float64(r.ModelIndices / 3), "sprite_triangles": float64(r.Sprites * 2), "dynamic_upload_bytes": float64(r.UploadBytes)} {
			counts[name] = append(counts[name], value)
		}
	}
	stats := map[string]any{}
	for name, values := range counts {
		stats[name] = quantities(values)
	}
	report["live"] = true
	report["live_source_next"] = percentiles(next)
	report["live_cpu_prep"] = percentiles(prep)
	report["live_frame_counts"] = stats
	report["live_distortion"] = false
	for _, key := range []string{"pose_upload_bytes_per_frame", "triple_pose_buffer_bytes", "model_draw_calls_per_frame", "model_vertices_instanced", "model_indices_drawn", "model_triangles_drawn"} {
		delete(report, key)
	}
	final := rows[len(rows)-1]
	report["instances"] = final.Instances
	report["final_tick"] = final.Tick
	report["final_model_triangles"] = final.ModelIndices / 3
	report["final_sprite_triangles"] = final.Sprites * 2
	report["approximations"] = []string{"LiveSource publishes immutable committed presentation frame pairs; native copies synchronously; Run does not close the source", "Column-wise matrix lerp can lose rigidity; pieces hidden at either endpoint are suppressed", "Live projection uses z minus half-height with independent z+2*y conventional depth; flat terrain backdrop, no retail composition or unit shadow lane", "Depth-tested billboards use source order, premultiplied alpha/additive blending, no depth writes; transparent sprite overlaps are not depth-sorted", "Live lighting uses at most the first 32 supplied sources; actual sprite emission feeds bloom; synthetic sources/rings are disabled; real effect distortion omitted", "Sprite Params.w is normalized clip-depth bias, negative toward camera", "Host timer cap skips missed slots after stalls; vsync/backpressure determine display cadence", "Native CPU encoding includes buffer growth and memory copies; GPU span covers encoded GPU passes; both are separate from LiveSource.Next and Go packing; Objective-C allocation counts are unavailable", "Presentation callbacks have a bounded 250ms drain outside measurement; missing timestamps excluded and counted"}
}
func writeLiveRows(dir string, rows []liveRow, warmup int) error {
	f, err := os.Create(filepath.Join(dir, "live-frames.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write([]string{"frame", "warmup", "tick", "source_seconds", "alpha", "source_next_ms", "cpu_prep_ms", "instances", "poses", "sprites", "lights", "dropped_lights", "model_draws", "sprite_draws", "model_vertices", "model_triangles", "sprite_triangles", "dynamic_upload_bytes"}); err != nil {
		return err
	}
	for i, r := range rows {
		if err = w.Write([]string{strconv.Itoa(i), strconv.FormatBool(i < warmup), fmt.Sprint(r.Tick), strconv.FormatFloat(r.Seconds, 'f', 9, 64), strconv.FormatFloat(float64(r.Alpha), 'f', 9, 64), strconv.FormatFloat(r.NextMS, 'f', 6, 64), strconv.FormatFloat(r.PrepMS, 'f', 6, 64), fmt.Sprint(r.Instances), fmt.Sprint(r.Poses), fmt.Sprint(r.Sprites), fmt.Sprint(r.Lights), fmt.Sprint(r.DroppedLights), fmt.Sprint(r.ModelDraws), fmt.Sprint(r.SpriteDraws), fmt.Sprint(r.ModelVertices), fmt.Sprint(r.ModelIndices / 3), fmt.Sprint(r.Sprites * 2), fmt.Sprint(r.UploadBytes)}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
