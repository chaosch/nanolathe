//go:build darwin

package metalproto

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

//go:embed native/shaders.metal
var shaderSource string

type bridge struct {
	terrainTiles   func(uintptr, uintptr) int32
	meshFaces      func(uintptr, uint32, uintptr) int32
	pointerCapture func(uintptr, int32) int32
	create         func(int32, int32, int32, int32, int32, int32, int32, uintptr, uintptr, int32) uintptr
	mesh           func(uintptr, uintptr, int32, uintptr, int32, uintptr, int32, uintptr, int32, uintptr, uintptr, int32) int32
	assets         func(uintptr, uintptr) int32
	texture        func(uintptr, int32, int32, int32, uintptr) int32
	configure      func(uintptr, uintptr, uintptr, int32) int32
	frame          func(uintptr, uintptr, uintptr, float32, float64) int32
	liveFrame      func(uintptr, uintptr) int32
	input          func(uintptr, uintptr)
	events         func(uintptr, uintptr, int32) int32
	playable       func(uintptr, int32)
	drain          func(uintptr)
	rows           func(uintptr, uintptr, int32) int32
	capture        func(uintptr, uintptr) int32
	info           func(uintptr, uintptr, int32)
	destroy        func(uintptr)
}

func pointer[T any](s []T) uintptr {
	if len(s) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&s[0]))
}
func cstring(s string) []byte { return append([]byte(s), 0) }
func boolInt(v bool) int32 {
	if v {
		return 1
	}
	return 0
}
func terminated(s []byte) string {
	for i, b := range s {
		if b == 0 {
			return string(s[:i])
		}
	}
	return string(s)
}

// Run must be called on the process main OS thread. A CLI should lock that
// thread in init, as Ebitengine does; locking an arbitrary goroutine here cannot
// turn its current thread into Cocoa's main thread. The bridge checks this.
func Run(scene *meshscene.Scene, opts Options) (map[string]any, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := validate(scene); err != nil {
		return nil, err
	}
	if opts.Width == 0 {
		opts.Width = 1920
	}
	if opts.Height == 0 {
		opts.Height = 1080
	}
	interactive := opts.Frames == 0
	if opts.Benchmark != nil && (interactive || opts.FPS != 30 && opts.FPS != 60 && opts.FPS != 120 || opts.Warmup != 2*opts.FPS) {
		return nil, fmt.Errorf("metalproto: benchmark requires bounded frames, 30/60/120 draws per second and two seconds of warmup")
	}
	if opts.Width < 1 || opts.Height < 1 || opts.Frames < 0 || opts.Warmup < 0 || opts.FPS < 0 || opts.Frames > math.MaxInt32-opts.Warmup || interactive && opts.Offscreen {
		return nil, fmt.Errorf("metalproto: invalid dimensions or frame counts")
	}
	if opts.OutputDir == "" {
		var err error
		opts.OutputDir, err = os.MkdirTemp("", "nanolathe-metal-")
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, err
	}
	libPath := opts.LibraryPath
	if libPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		libPath = filepath.Join(filepath.Dir(exe), "libnanometal.dylib")
	}
	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("metalproto: load %s: %w", libPath, err)
	}
	// Keep the dylib loaded: a drawable's presentation callback may finish after
	// the last command buffer has completed. Unmapping its callback code is unsafe.
	var b bridge
	purego.RegisterLibFunc(&b.create, lib, "nm_create")
	purego.RegisterLibFunc(&b.terrainTiles, lib, "nm_terrain_tiles")
	purego.RegisterLibFunc(&b.pointerCapture, lib, "nm_pointer_capture")
	purego.RegisterLibFunc(&b.mesh, lib, "nm_mesh")
	purego.RegisterLibFunc(&b.meshFaces, lib, "nm_mesh_faces")
	purego.RegisterLibFunc(&b.assets, lib, "nm_assets")
	purego.RegisterLibFunc(&b.texture, lib, "nm_texture")
	purego.RegisterLibFunc(&b.configure, lib, "nm_configure")
	purego.RegisterLibFunc(&b.frame, lib, "nm_frame")
	purego.RegisterLibFunc(&b.liveFrame, lib, "nm_live_frame")
	purego.RegisterLibFunc(&b.input, lib, "nm_input")
	purego.RegisterLibFunc(&b.events, lib, "nm_events")
	purego.RegisterLibFunc(&b.playable, lib, "nm_playable")
	purego.RegisterLibFunc(&b.drain, lib, "nm_drain")
	purego.RegisterLibFunc(&b.rows, lib, "nm_rows")
	purego.RegisterLibFunc(&b.capture, lib, "nm_capture")
	purego.RegisterLibFunc(&b.info, lib, "nm_info")
	purego.RegisterLibFunc(&b.destroy, lib, "nm_destroy")
	shader := cstring(waterShaderSource + "\n" + LightingShaderSource + "\n" + effectsShaderSource + "\n" + GlowShaderSource + "\n" + groundEffectsShaderSource + "\n" + facesShaderSource + "\n" + faceReflectionsShaderSource + "\n" + TerrainTilesShaderSource + "\n" + shaderSource + "\n" + groupShaderSource + "\n" + shadowShaderSource + "\n" + WaterTilesShaderSource + "\n" + waterObjectsShaderSource)
	errorText := make([]byte, 8192)
	capacity := opts.Frames + opts.Warmup
	if interactive {
		capacity = 8192
	}
	nativeCapacity := capacity
	if interactive {
		nativeCapacity = -capacity
	}
	handle := b.create(int32(opts.Width), int32(opts.Height), boolInt(opts.Offscreen), boolInt(opts.VSync), boolInt(opts.Effects), boolInt(opts.VisualPasses && scene.Live != nil), int32(nativeCapacity), pointer(shader), pointer(errorText), int32(len(errorText)))
	runtime.KeepAlive(shader)
	runtime.KeepAlive(errorText)
	if handle == 0 {
		return nil, fmt.Errorf("metalproto: initialize: %s", terminated(errorText))
	}
	defer b.destroy(handle)
	b.playable(handle, boolInt(scene.Playable))
	var vertexBytes, indexBytes, instanceBytes, drawnVertices, drawnIndices, drawCalls uint64
	for i, m := range scene.Meshes {
		var offsets []uint32
		var visuals []meshscene.ModelVisual
		for _, instance := range scene.Instances {
			if instance.Mesh == i {
				offsets = append(offsets, uint32(instance.PoseOffset))
				visuals = append(visuals, instance.Visual)
			}
		}
		if len(offsets) == 0 && scene.Live == nil {
			continue
		}
		if b.mesh(handle, pointer(m.Vertices), int32(len(m.Vertices)), pointer(m.Indices), int32(len(m.Indices)), pointer(offsets), int32(len(offsets)), pointer(m.EdgeIndices), int32(len(m.EdgeIndices)), pointer(visuals), pointer(m.PrimitiveCenters), int32(len(m.PrimitiveCenters))) == 0 {
			return nil, fmt.Errorf("metalproto: upload mesh %s", m.Name)
		}
		faces, err := PrepareMeshFaces(&m)
		if err != nil {
			return nil, err
		}
		if b.meshFaces(handle, uint32(drawCalls), uintptr(unsafe.Pointer(&faces))) == 0 {
			return nil, fmt.Errorf("metalproto: upload authored faces %s", m.Name)
		}
		runtime.KeepAlive(faces)
		runtime.KeepAlive(m)
		runtime.KeepAlive(offsets)
		runtime.KeepAlive(visuals)
		vertexBytes += uint64(len(m.Vertices)+len(m.FaceCorners))*64 + uint64(len(m.Faces))*32
		indexBytes += uint64(len(m.Indices)+len(m.EdgeIndices)+len(m.FaceIndices)) * 4
		instanceBytes += uint64(len(offsets)) * 68
		drawnVertices += uint64(len(m.Vertices)) * uint64(len(offsets))
		drawnIndices += uint64(len(m.Indices)) * uint64(len(offsets))
		drawCalls++
	}
	assets := nativeAssets{Materials: pointer(scene.Materials), Frames: pointer(scene.TextureFrames), Heights: pointer(scene.HeightField.Values), MaterialCount: uint32(len(scene.Materials)), FrameCount: uint32(len(scene.TextureFrames)), Width: uint32(scene.HeightField.Width), Height: uint32(scene.HeightField.Height), Rect: scene.HeightField.Rect}
	if b.assets(handle, uintptr(unsafe.Pointer(&assets))) == 0 {
		return nil, fmt.Errorf("metalproto: upload retained visual assets")
	}
	runtime.KeepAlive(scene)
	runtime.KeepAlive(assets)
	if scene.TerrainTiles != nil {
		upload, err := packTerrainTiles(scene.TerrainTiles)
		if err != nil {
			return nil, err
		}
		if b.terrainTiles(handle, uintptr(unsafe.Pointer(&upload))) == 0 {
			return nil, fmt.Errorf("metalproto: terrain tile upload failed")
		}
		runtime.KeepAlive(scene.TerrainTiles)
	}
	textures := []meshscene.Texture{scene.Atlas, scene.Terrain}
	if scene.Live != nil {
		textures = append(textures, scene.SpriteAtlas)
		if len(scene.OverlayAtlas.RGBA) > 0 {
			textures = append(textures, scene.OverlayAtlas)
		}
	}
	for i, t := range textures {
		if b.texture(handle, int32(i), int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload texture %d", i)
		}
		runtime.KeepAlive(t)
	}
	for _, extra := range []struct {
		slot    int32
		texture meshscene.Texture
	}{{5, scene.WaterMask}, {6, scene.ModelPalette}} {
		t := extra.texture
		if len(t.RGBA) > 0 && b.texture(handle, extra.slot, int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload texture %d", extra.slot)
		}
		runtime.KeepAlive(t)
	}
	if t := scene.FogAtlas; len(t.RGBA) > 0 {
		if b.texture(handle, 4, int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload retained fog masks")
		}
		runtime.KeepAlive(t)
	}
	poseCount := 0
	if scene.Live == nil {
		poseCount = len(scene.Frames[0].Transforms)
	}
	if b.configure(handle, pointer(scene.Camera[:]), pointer(scene.TerrainRect[:]), int32(poseCount)) == 0 {
		return nil, fmt.Errorf("metalproto: allocate pose buffers")
	}
	runtime.KeepAlive(scene)
	rows := make([]nativeRow, capacity)
	events := make([]meshscene.NativeEvent, 8192)
	var liveRows []liveRow
	var packing livePacking
	if scene.Live != nil {
		liveRows = make([]liveRow, len(rows))
		packing.spans = make([]meshSpan, len(scene.Meshes)*nativeSpanPasses)
		packing.cursor = make([]uint32, len(scene.Meshes))
	}
	var before, after runtime.MemStats
	measurement := benchmarkMeasurement{directory: opts.OutputDir, options: opts.Benchmark}
	defer measurement.stop()
	var hostTiming benchmarkHostTiming
	started := time.Now()
	measuredStart := started
	actual := 0
	deadline := started
	var interval time.Duration
	if opts.FPS > 0 {
		interval = time.Duration(float64(time.Second) / float64(opts.FPS))
	}
	for i := 0; interactive || i < len(rows); i++ {
		var entry time.Time
		if opts.Benchmark != nil {
			entry = time.Now()
		}
		if i == opts.Warmup && opts.Benchmark != nil {
			if err := measurement.begin(); err != nil {
				return nil, err
			}
			hostTiming = benchmarkHostTiming{}
			deadline = time.Now()
			entry = deadline
		}
		waitStart := time.Now()
		if opts.FPS > 0 && i > 0 {
			// Keep animation on started, but skip missed presentation slots.
			// A stall must not be repaid by submitting a catch-up burst.
			if i != opts.Warmup || opts.Benchmark == nil {
				deadline = deadline.Add(interval)
			}
			now := time.Now()
			if now.After(deadline) {
				deadline = now
			}
			if delay := time.Until(deadline); delay > 0 {
				time.Sleep(delay)
			}
		}
		drawStart := time.Now()
		if i == opts.Warmup {
			if opts.Benchmark != nil {
				before = measurement.before
			} else {
				runtime.ReadMemStats(&before)
			}
			measuredStart = time.Now()
		}
		seconds := time.Since(started).Seconds()
		if opts.Benchmark != nil {
			seconds = float64(i) / float64(opts.FPS)
		}
		var hostRow BenchmarkFrame
		if opts.Benchmark != nil {
			hostRow = hostTiming.beginPaced(i, entry, waitStart, drawStart)
		}
		var status int32
		var frameMetrics liveRow
		if scene.Live != nil {
			var input nativeInput
			b.input(handle, uintptr(unsafe.Pointer(&input)))
			eventCount := int(b.events(handle, pointer(events), int32(len(events))))
			runtime.KeepAlive(events)
			if eventCount < 0 || eventCount > len(events) {
				return nil, fmt.Errorf("metalproto: invalid native event count")
			}
			nextStart := time.Now()
			live, err := scene.Live.Next(seconds, meshscene.Input{Events: events[:eventCount], PanX: input.PanX, PanZ: input.PanZ, Zoom: input.Zoom, PauseToggle: input.PauseToggle != 0})
			nextMS := float64(time.Since(nextStart)) / float64(time.Millisecond)
			hostRow.Source = nextMS
			if err != nil {
				return nil, fmt.Errorf("metalproto: live frame %d: %w", i, err)
			}
			if live.Exit {
				break
			}
			if b.pointerCapture(handle, boolInt(live.PointerCaptured)) == 0 {
				return nil, fmt.Errorf("metalproto: native pointer capture unavailable")
			}
			prepStart := time.Now()
			upload, metrics, err := packing.prepare(scene, live, seconds, opts.VisualPasses, opts.Effects)
			if err != nil {
				return nil, err
			}
			metrics.NextMS = nextMS
			metrics.PrepMS = float64(time.Since(prepStart)) / float64(time.Millisecond)
			hostRow.Packing = metrics.PrepMS
			frameMetrics = metrics
			var submitStart time.Time
			if opts.Benchmark != nil {
				submitStart = time.Now()
			}
			status = b.liveFrame(handle, uintptr(unsafe.Pointer(&upload)))
			if opts.Benchmark != nil {
				hostRow.Submit = float64(time.Since(submitStart)) / 1e6
			}
			runtime.KeepAlive(live)
			runtime.KeepAlive(packing)
			runtime.KeepAlive(upload)
		} else {
			sample := seconds * 30
			tick := int(sample)
			alpha := float32(sample - float64(tick))
			a := scene.Frames[tick%len(scene.Frames)].Transforms
			next := scene.Frames[(tick+1)%len(scene.Frames)].Transforms
			status = b.frame(handle, pointer(a), pointer(next), alpha, seconds)
			runtime.KeepAlive(a)
			runtime.KeepAlive(next)
		}
		if status < 0 {
			break
		}
		if status == 0 {
			return nil, fmt.Errorf("metalproto: frame %d could not be encoded", i)
		}
		if scene.Live != nil {
			liveRows[i%capacity] = frameMetrics
		}
		actual++
		if opts.Benchmark != nil {
			hostTiming.end(&hostRow, drawStart)
			if i >= opts.Warmup && opts.Benchmark.AfterFrame != nil {
				opts.Benchmark.AfterFrame(hostRow)
			}
		}
	}
	submittedAt := time.Now()
	if opts.Benchmark != nil {
		if actual != opts.Warmup+opts.Frames {
			return nil, fmt.Errorf("metalproto: benchmark closed before measurement completed")
		}
		if err := measurement.end(); err != nil {
			return nil, err
		}
	}
	b.drain(handle)
	if opts.Benchmark != nil {
		after = measurement.after
	} else {
		runtime.ReadMemStats(&after)
	}
	n := int(b.rows(handle, pointer(rows), int32(len(rows))))
	runtime.KeepAlive(rows)
	if n < 0 || n > len(rows) || n > actual {
		return nil, fmt.Errorf("metalproto: invalid native timing count")
	}
	rows = rows[:n]
	firstFrame := actual - n
	if scene.Live != nil {
		// Native and Go records share the submission sequence; rotate only once,
		// after exit, rather than draining the GPU while the match is running.
		ordered := make([]liveRow, n)
		for i := range ordered {
			ordered[i] = liveRows[(firstFrame+i)%capacity]
		}
		liveRows = ordered
	}
	for i, r := range rows {
		if r.Status != 4 {
			return nil, fmt.Errorf("metalproto: command buffer %d failed with status %d", i, r.Status)
		}
	}
	if actual <= opts.Warmup {
		return nil, fmt.Errorf("metalproto: window closed before measurement")
	}
	deviceBytes := make([]byte, 8192)
	b.info(handle, pointer(deviceBytes), int32(len(deviceBytes)))
	runtime.KeepAlive(deviceBytes)
	var device map[string]any
	if err := json.Unmarshal([]byte(terminated(deviceBytes)), &device); err != nil {
		return nil, err
	}
	capture := cstring(filepath.Join(opts.OutputDir, "final.png"))
	if b.capture(handle, pointer(capture)) == 0 {
		return nil, fmt.Errorf("metalproto: final PNG readback failed")
	}
	runtime.KeepAlive(capture)
	skipWarmup := max(opts.Warmup-firstFrame, 0)
	measured := rows[skipWarmup:]
	report := makeReport(measured, opts, scene, device)
	if opts.Benchmark != nil {
		report["benchmark_memory"] = measurement.report()
		report["benchmark_version"] = 2
		report["simulation_tps"] = 30
		report["draws_per_tick"] = opts.FPS / 30
		report["warmup_draws"] = opts.Warmup
		report["source_clock"] = "deterministic draw index divided by target draw rate"
	}
	report["interactive"] = interactive
	report["total_frames_submitted"] = actual
	report["retained_frames"] = n
	report["first_retained_frame"] = firstFrame
	report["timing_capacity_frames"] = capacity
	report["retained_overlay_atlas_bytes"] = device["retained_overlay_atlas_bytes"]
	report["submission_elapsed_seconds"] = submittedAt.Sub(measuredStart).Seconds()
	report["go_allocations"] = after.Mallocs - before.Mallocs
	report["go_allocated_bytes"] = after.TotalAlloc - before.TotalAlloc
	report["go_heap_bytes_before"] = before.HeapAlloc
	report["go_heap_bytes_after"] = after.HeapAlloc
	report["go_heap_reserved_bytes_after"] = after.HeapSys
	report["go_gc_cycles"] = after.NumGC - before.NumGC
	report["go_gc_pause_ms"] = float64(after.PauseTotalNs-before.PauseTotalNs) / 1e6
	report["go_allocation_scope"] = "process-wide Go allocations during measured submission plus final drain"
	if scene.Live != nil {
		report["go_allocation_scope"] = "process-wide Go allocations including concurrent live-source worker during measured submission plus final drain"
	}
	if opts.Benchmark != nil {
		report["go_allocation_scope"] = "process-wide Go allocations in measured host work; excludes final drain, snapshots, profile encoding and capture"
	}
	report["visual_passes"] = opts.VisualPasses && scene.Live != nil
	report["retained_material_bytes"] = len(scene.Materials)*16 + len(scene.TextureFrames)*16
	report["retained_height_bytes"] = len(scene.HeightField.Values) * 4
	report["retained_vertex_bytes"] = vertexBytes
	report["retained_index_bytes"] = indexBytes
	report["retained_instance_bytes"] = instanceBytes
	report["pose_upload_bytes_per_frame"] = uint64(poseCount) * 128
	report["triple_pose_buffer_bytes"] = uint64(poseCount) * 128 * 3
	report["model_draw_calls_per_frame"] = drawCalls
	report["model_vertices_instanced"] = drawnVertices
	report["vertex_counting"] = "retained vertex records for submitted instances, including edge-only records; not hardware vertex shader invocations"
	report["model_indices_drawn"] = drawnIndices
	report["model_triangles_drawn"] = drawnIndices / 3
	report["texture_uploaded_bytes"] = len(scene.Atlas.RGBA) + len(scene.Terrain.RGBA)
	if scene.Live != nil {
		report["texture_uploaded_bytes"] = len(scene.Atlas.RGBA) + len(scene.Terrain.RGBA) + len(scene.SpriteAtlas.RGBA) + len(scene.OverlayAtlas.RGBA) + len(scene.FogAtlas.RGBA)
		addLiveReport(report, liveRows[skipWarmup:])
		if err := writeLiveRows(opts.OutputDir, liveRows, opts.Warmup, firstFrame); err != nil {
			return nil, err
		}
	}
	report["terrain_triangles_drawn"] = 2
	if err := writeArtifacts(opts.OutputDir, report, rows, opts.Warmup, firstFrame); err != nil {
		return nil, err
	}
	return report, nil
}

func validate(s *meshscene.Scene) error {
	if s == nil {
		return fmt.Errorf("metalproto: nil scene")
	}
	n := 0
	if s.Live == nil {
		if len(s.Frames) == 0 || len(s.Frames[0].Transforms) == 0 {
			return fmt.Errorf("metalproto: scene has no sampled poses")
		}
		n = len(s.Frames[0].Transforms)
		if n > math.MaxInt32 {
			return fmt.Errorf("metalproto: pose count too large")
		}
		for _, f := range s.Frames {
			if len(f.Transforms) != n {
				return fmt.Errorf("metalproto: pose topology changes across frames")
			}
		}
	}
	if unsafe.Sizeof(meshscene.Vertex{}) != 64 {
		return fmt.Errorf("metalproto: vertex ABI is not 64 bytes")
	}
	for i, m := range s.Meshes {
		if len(m.Vertices) > math.MaxInt32 || len(m.Indices) > math.MaxInt32 || len(m.Indices)%3 != 0 || len(m.EdgeIndices) > math.MaxInt32 || len(m.EdgeIndices)%2 != 0 {
			return fmt.Errorf("metalproto: mesh %d has invalid counts", i)
		}
		for _, index := range append(append([]uint32(nil), m.Indices...), m.EdgeIndices...) {
			if uint64(index) >= uint64(len(m.Vertices)) {
				return fmt.Errorf("metalproto: mesh %d has invalid index", i)
			}
		}
		for _, v := range m.Vertices {
			if v.Material != 0 && int(v.Material) >= len(s.Materials) {
				return fmt.Errorf("metalproto: invalid material")
			}
			if uint64(v.Piece) >= uint64(m.Pieces) {
				return fmt.Errorf("metalproto: mesh %d has invalid piece", i)
			}
		}
	}
	if s.Live == nil {
		for _, instance := range s.Instances {
			if instance.Mesh < 0 || instance.Mesh >= len(s.Meshes) || instance.PoseOffset < 0 || instance.PoseOffset+s.Meshes[instance.Mesh].Pieces > n {
				return fmt.Errorf("metalproto: instance has invalid mesh or pose span")
			}
		}
	}
	if unsafe.Sizeof(meshscene.Material{}) != 16 || unsafe.Sizeof(meshscene.ModelVisual{}) != 64 || unsafe.Sizeof(nativeAssets{}) != 56 {
		return fmt.Errorf("metalproto: retained assets ABI mismatch")
	}
	for index, m := range s.Materials {
		if index != 0 && m.FrameCount == 0 {
			return fmt.Errorf("metalproto: empty material frame span")
		}
		if uint64(m.FirstFrame)+uint64(m.FrameCount) > uint64(len(s.TextureFrames)) {
			return fmt.Errorf("metalproto: material frame span")
		}
	}
	h := s.HeightField
	if h.Width < 0 || h.Height < 0 || uint64(h.Width)*uint64(h.Height) != uint64(len(h.Values)) {
		return fmt.Errorf("metalproto: invalid height field")
	}
	textures := []meshscene.Texture{s.Atlas, s.Terrain}
	if s.Live != nil {
		textures = append(textures, s.SpriteAtlas)
		if len(s.OverlayAtlas.RGBA) > 0 {
			textures = append(textures, s.OverlayAtlas)
		}
	}
	if len(s.FogAtlas.RGBA) > 0 {
		if s.FogAtlas.Width != 896 || s.FogAtlas.Height != 513 {
			return fmt.Errorf("metalproto: invalid authored fog atlas dimensions")
		}
		textures = append(textures, s.FogAtlas)
	}
	for _, t := range textures {
		if t.Width <= 0 || t.Height <= 0 || t.Width > math.MaxInt32 || t.Height > math.MaxInt32 || int64(t.Width)*int64(t.Height)*4 != int64(len(t.RGBA)) {
			return fmt.Errorf("metalproto: invalid RGBA texture")
		}
	}
	return nil
}
