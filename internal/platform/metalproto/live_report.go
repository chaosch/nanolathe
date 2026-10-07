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
	GroundSprites, AirInstances, CloakSubjects                                      int
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
	return map[string]any{"samples": len(values), "min": ordered[0], "max": ordered[len(ordered)-1], "mean": total / float64(len(values)), "p50": ordered[(len(ordered)-1)/2], "p95": ordered[int(float64(len(ordered)-1)*.95)], "p99": ordered[int(float64(len(ordered)-1)*.99)], "total": total}
}
func addLiveReport(report map[string]any, rows []liveRow) {
	next := make([]float64, len(rows))
	prep := make([]float64, len(rows))
	counts := map[string][]float64{}
	for i, r := range rows {
		next[i] = r.NextMS
		prep[i] = r.PrepMS
		for name, value := range map[string]float64{"overlay_texture_upload_bytes": float64(r.OverlayTextureBytes), "overlay_quads": float64(r.OverlayQuads), "overlay_upload_bytes": float64(r.OverlayBytes), "distortions": float64(r.Distortions), "fog_upload_bytes": float64(r.FogBytes), "constructing_instances": float64(r.Constructing), "shadow_instances": float64(r.ShadowInstances), "shadow_draws": float64(r.ShadowDraws), "outline_draws": float64(r.OutlineDraws), "shadow_triangles": float64(r.ShadowIndices / 3), "outline_edges": float64(r.OutlineIndices / 2), "instances": float64(r.Instances), "poses": float64(r.Poses), "sprites": float64(r.Sprites), "ground_feature_sprites": float64(r.GroundSprites), "air_instances": float64(r.AirInstances), "cloak_subjects": float64(r.CloakSubjects), "lights": float64(r.Lights), "dropped_lights": float64(r.DroppedLights), "model_draws": float64(r.ModelDraws), "sprite_draws": float64(r.SpriteDraws), "model_vertices": float64(r.ModelVertices), "model_triangles": float64(r.ModelIndices / 3), "sprite_triangles": float64(r.Sprites * 2), "dynamic_upload_bytes": float64(r.UploadBytes)} {
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
	report["render_parity_verified"] = false
	report["native_fidelity_scope"] = "retained native raster with production CPU presentation policies; matching simulation state does not establish pixel parity"
	report["approximations"] = []string{
		"LiveSource pins committed presentation frame pairs; native copies synchronously; Run does not close the source",
		"Production-composed live frames use the client orderer, isolated subject atlases and ordered painter commits; native depth is confined to each subject rather than shared across the composed battle",
		"Rigid pose interpolation uses shortest-path quaternion rotation once per piece; composed origins interpolate linearly and pieces hidden at either endpoint are suppressed",
		"Model shading consumes production SHD normals, row arithmetic, material identity and receiver controls; authored-face mapping is independent of stress subdivision; native triangle coverage, floating transforms and projection/cache staging remain potential differences",
		"Production source gathering retains the exact 64-source family budget, stable selection and nano clusters; subjects select at most eight nearby sources; smoke uses clipped-corner scattering and ground light uses its separate receiver gate",
		"Stock effects replay original ordered sprites, rows, lines, points, lenses and projectile identity markers; each lens reads its earlier completed composite; unsupported or suppressed records remain visible in native counters",
		"Production glow source gains and the quarter/eighth near/far kernels are exported without rerecording a full draw list; destination-dependent rows read the completed effect composite; settings and source atlas must come from the same presentation owner",
		"Composed shadows consume production unit/feature eligibility, placement, coverage, softness and alpha-silhouette inputs; native atlas raster and palette-key handling still require capture review; the uncomposed fallback retains geometric shadows",
		"Construction, outline, cloak, reveal and waterline consume production model rules; ordered subject opacity resolves once and attached cargo shares its parent; native true-colour blending replaces destination-palette ALP",
		"Water, promoted seabed sprites, model/stock reflections, grouped source/merged-key ownership and underwater BLUE/refraction are integrated; a GPU face budget preserves the production source cap independently of stress subdivision; exact historical cached/live staging remains a parity gap",
		"Fog uses authored masks and production integer cell/desaturation inputs; native true-colour composition and composite/out-of-tile assets remain subject to preparation limits",
		"Legacy uncomposed live frames retain shared scene-depth, geometric shadow and bounded post-bloom distortion paths; these are not evidence that the production-composed path uses those approximations",
		"Native CPU encoding includes buffer growth, copies and queue/drawable backpressure; LiveSource.Next and Go packing are separate host work; command-buffer GPU execution and presentation callbacks are separate clocks, not comparable CPU headline numbers",
		"Deadline pacing skips missed slots after stalls; vsync and backpressure determine display cadence; presentation callbacks drain for at most 250ms outside measurement and missing timestamps are counted and excluded",
		"Go memory uses measured runtime counters; Metal memory uses currentAllocatedSize; Objective-C allocation counts are unavailable",
	}
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
