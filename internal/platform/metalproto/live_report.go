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
	GroundSprites                                                                   int
	OverlayQuads, OverlayBytes, OverlayTextureBytes                                 int
	Seconds                                                                         float64
	Alpha                                                                           float32
	Tick                                                                            uint32
	NextMS, PrepMS                                                                  float64
	Instances, Poses, Sprites, Lights, DroppedLights, ModelDraws, SpriteDraws       int
	ModelVertices, ModelIndices, UploadBytes, ShadowIndices, OutlineIndices         uint64
	Distortions, FogBytes, Constructing, ShadowInstances, ShadowDraws, OutlineDraws int
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
		for name, value := range map[string]float64{"overlay_texture_upload_bytes": float64(r.OverlayTextureBytes), "overlay_quads": float64(r.OverlayQuads), "overlay_upload_bytes": float64(r.OverlayBytes), "distortions": float64(r.Distortions), "fog_upload_bytes": float64(r.FogBytes), "constructing_instances": float64(r.Constructing), "shadow_instances": float64(r.ShadowInstances), "shadow_draws": float64(r.ShadowDraws), "outline_draws": float64(r.OutlineDraws), "shadow_triangles": float64(r.ShadowIndices / 3), "outline_edges": float64(r.OutlineIndices / 2), "instances": float64(r.Instances), "poses": float64(r.Poses), "sprites": float64(r.Sprites), "ground_feature_sprites": float64(r.GroundSprites), "lights": float64(r.Lights), "dropped_lights": float64(r.DroppedLights), "model_draws": float64(r.ModelDraws), "sprite_draws": float64(r.SpriteDraws), "model_vertices": float64(r.ModelVertices), "model_triangles": float64(r.ModelIndices / 3), "sprite_triangles": float64(r.Sprites * 2), "dynamic_upload_bytes": float64(r.UploadBytes)} {
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
	report["live_distortion"] = report["visual_passes"] == true && report["effects"] == true
	for _, key := range []string{"pose_upload_bytes_per_frame", "triple_pose_buffer_bytes", "model_draw_calls_per_frame", "model_vertices_instanced", "model_indices_drawn", "model_triangles_drawn"} {
		delete(report, key)
	}
	final := rows[len(rows)-1]
	report["instances"] = final.Instances
	report["final_tick"] = final.Tick
	report["final_model_triangles"] = final.ModelIndices / 3
	report["final_sprite_triangles"] = final.Sprites * 2
	report["approximations"] = []string{"LiveSource publishes immutable committed presentation frame pairs; native copies synchronously; Run does not close the source", "GPU computes rigid shortest-path quaternion rotation once per piece; composed origins still interpolate linearly; pieces hidden at either endpoint are suppressed", "Live projection uses z minus half-height and shared scene depth with integer model-height ties; flat painted TNT without depth writes; max-blend geometric terrain shadows, no retail wrapped-byte quad composition", "Model base shading reuses production SHD normals, row arithmetic and mobile/piece bypass; float normal transforms, triangle lanes and quantization of subdivision corners remain approximate", "Short sprite features draw in source order before models; remaining depth-tested billboards sort by interpolated biased anchor depth, not per-pixel depth, with stable ties and no depth writes", "Live lighting uses at most the first 32 supplied sources; bright supplied sprite emission and model cooling emission feed bloom; synthetic sources/rings disabled; real bounded distortion quads sample one immutable post-bloom world snapshot before fog", "VisualPasses false disables model state, shadow, outlines, real distortion and fog; team material lookup remains; old lighting/bloom composition remains", "Construction reveal is continuous world-Y clipping; outlines use authored retained edges, not retail row endpoints", "Cloak is premultiplied surface alpha with ordinary mesh depth, not one resolved subject-image ALP blend; geometry order can expose alpha overlap", "Geometric shadow is fixed 35 percent coverage with five-world-pixel X offset and sampled terrain receiver; maximum blending prevents stacked darkness; no texture-alpha silhouette or retail punch/placement", "Real distortion allocates two full-frame HDR post targets plus an R8 shadow mask, and copies the entire immutable post-bloom image when sources exist; one bounded quad batch, no per-pixel effect loop; later source coverage wins; fog samples original projected coordinates afterward", "Fog uses retained full-resolution authored masks, integer cell operations and production true-colour desaturation; composite/out-of-tile fog assets fail preparation", "Sprite Params.w is normalized clip-depth bias, negative toward camera", "Host timer cap skips missed slots after stalls; vsync/backpressure determine display cadence", "Native CPU encoding includes buffer growth and memory copies; GPU span covers encoded GPU passes; both are separate from LiveSource.Next and Go packing; Objective-C allocation counts are unavailable", "Presentation callbacks have a bounded 250ms drain outside measurement; missing timestamps excluded and counted"}
}
func writeLiveRows(dir string, rows []liveRow, warmup, firstFrame int) error {
	f, err := os.Create(filepath.Join(dir, "live-frames.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write([]string{"frame", "warmup", "tick", "source_seconds", "alpha", "source_next_ms", "cpu_prep_ms", "instances", "poses", "sprites", "lights", "dropped_lights", "model_draws", "sprite_draws", "model_vertices", "model_triangles", "sprite_triangles", "dynamic_upload_bytes", "distortions", "fog_upload_bytes", "constructing_instances", "shadow_instances", "shadow_draws", "outline_draws", "shadow_triangles", "outline_edges", "overlay_quads", "overlay_upload_bytes", "overlay_texture_upload_bytes"}); err != nil {
		return err
	}
	for i, r := range rows {
		if err = w.Write([]string{strconv.Itoa(i + firstFrame), strconv.FormatBool(i+firstFrame < warmup), fmt.Sprint(r.Tick), strconv.FormatFloat(r.Seconds, 'f', 9, 64), strconv.FormatFloat(float64(r.Alpha), 'f', 9, 64), strconv.FormatFloat(r.NextMS, 'f', 6, 64), strconv.FormatFloat(r.PrepMS, 'f', 6, 64), fmt.Sprint(r.Instances), fmt.Sprint(r.Poses), fmt.Sprint(r.Sprites), fmt.Sprint(r.Lights), fmt.Sprint(r.DroppedLights), fmt.Sprint(r.ModelDraws), fmt.Sprint(r.SpriteDraws), fmt.Sprint(r.ModelVertices), fmt.Sprint(r.ModelIndices / 3), fmt.Sprint(r.Sprites * 2), fmt.Sprint(r.UploadBytes), fmt.Sprint(r.Distortions), fmt.Sprint(r.FogBytes), fmt.Sprint(r.Constructing), fmt.Sprint(r.ShadowInstances), fmt.Sprint(r.ShadowDraws), fmt.Sprint(r.OutlineDraws), fmt.Sprint(r.ShadowIndices / 3), fmt.Sprint(r.OutlineIndices / 2), fmt.Sprint(r.OverlayQuads), fmt.Sprint(r.OverlayBytes), fmt.Sprint(r.OverlayTextureBytes)}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
