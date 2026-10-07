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
	create    func(int32, int32, int32, int32, int32, int32, int32, uintptr, uintptr, int32) uintptr
	mesh      func(uintptr, uintptr, int32, uintptr, int32, uintptr, int32, uintptr, int32, uintptr) int32
	assets    func(uintptr, uintptr) int32
	texture   func(uintptr, int32, int32, int32, uintptr) int32
	configure func(uintptr, uintptr, uintptr, int32) int32
	frame     func(uintptr, uintptr, uintptr, float32, float64) int32
	liveFrame func(uintptr, uintptr) int32
	input     func(uintptr, uintptr)
	drain     func(uintptr)
	rows      func(uintptr, uintptr, int32) int32
	capture   func(uintptr, uintptr) int32
	info      func(uintptr, uintptr, int32)
	destroy   func(uintptr)
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
	if opts.Frames == 0 {
		opts.Frames = 600
	}
	if opts.Width < 1 || opts.Height < 1 || opts.Frames < 1 || opts.Warmup < 0 || opts.FPS < 0 || opts.Frames+opts.Warmup > math.MaxInt32 {
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
	purego.RegisterLibFunc(&b.mesh, lib, "nm_mesh")
	purego.RegisterLibFunc(&b.assets, lib, "nm_assets")
	purego.RegisterLibFunc(&b.texture, lib, "nm_texture")
	purego.RegisterLibFunc(&b.configure, lib, "nm_configure")
	purego.RegisterLibFunc(&b.frame, lib, "nm_frame")
	purego.RegisterLibFunc(&b.liveFrame, lib, "nm_live_frame")
	purego.RegisterLibFunc(&b.input, lib, "nm_input")
	purego.RegisterLibFunc(&b.drain, lib, "nm_drain")
	purego.RegisterLibFunc(&b.rows, lib, "nm_rows")
	purego.RegisterLibFunc(&b.capture, lib, "nm_capture")
	purego.RegisterLibFunc(&b.info, lib, "nm_info")
	purego.RegisterLibFunc(&b.destroy, lib, "nm_destroy")
	shader := cstring(shaderSource)
	errorText := make([]byte, 8192)
	handle := b.create(int32(opts.Width), int32(opts.Height), boolInt(opts.Offscreen), boolInt(opts.VSync), boolInt(opts.Effects), boolInt(opts.VisualPasses && scene.Live != nil), int32(opts.Frames+opts.Warmup), pointer(shader), pointer(errorText), int32(len(errorText)))
	runtime.KeepAlive(shader)
	runtime.KeepAlive(errorText)
	if handle == 0 {
		return nil, fmt.Errorf("metalproto: initialize: %s", terminated(errorText))
	}
	defer b.destroy(handle)
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
		if b.mesh(handle, pointer(m.Vertices), int32(len(m.Vertices)), pointer(m.Indices), int32(len(m.Indices)), pointer(offsets), int32(len(offsets)), pointer(m.EdgeIndices), int32(len(m.EdgeIndices)), pointer(visuals)) == 0 {
			return nil, fmt.Errorf("metalproto: upload mesh %s", m.Name)
		}
		runtime.KeepAlive(m)
		runtime.KeepAlive(offsets)
		runtime.KeepAlive(visuals)
		vertexBytes += uint64(len(m.Vertices)) * 64
		indexBytes += uint64(len(m.Indices)+len(m.EdgeIndices)) * 4
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
	textures := []meshscene.Texture{scene.Atlas, scene.Terrain}
	if scene.Live != nil {
		textures = append(textures, scene.SpriteAtlas)
	}
	for i, t := range textures {
		if b.texture(handle, int32(i), int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload texture %d", i)
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
	rows := make([]nativeRow, opts.Frames+opts.Warmup)
	var liveRows []liveRow
	var packing livePacking
	if scene.Live != nil {
		liveRows = make([]liveRow, len(rows))
		packing.spans = make([]meshSpan, len(scene.Meshes)*3)
		packing.cursor = make([]uint32, len(scene.Meshes))
	}
	var before, after runtime.MemStats
	started := time.Now()
	measuredStart := started
	actual := 0
	deadline := started
	var interval time.Duration
	if opts.FPS > 0 {
		interval = time.Duration(float64(time.Second) / float64(opts.FPS))
	}
	for i := 0; i < len(rows); i++ {
		if opts.FPS > 0 && i > 0 {
			// Keep animation on started, but skip missed presentation slots.
			// A stall must not be repaid by submitting a catch-up burst.
			deadline = deadline.Add(interval)
			now := time.Now()
			if now.After(deadline) {
				deadline = now
			}
			if delay := time.Until(deadline); delay > 0 {
				time.Sleep(delay)
			}
		}
		if i == opts.Warmup {
			runtime.ReadMemStats(&before)
			measuredStart = time.Now()
		}
		seconds := time.Since(started).Seconds()
		var status int32
		if scene.Live != nil {
			var input nativeInput
			b.input(handle, uintptr(unsafe.Pointer(&input)))
			nextStart := time.Now()
			live, err := scene.Live.Next(seconds, meshscene.Input{PanX: input.PanX, PanZ: input.PanZ, Zoom: input.Zoom, PauseToggle: input.PauseToggle != 0})
			nextMS := float64(time.Since(nextStart)) / float64(time.Millisecond)
			if err != nil {
				return nil, fmt.Errorf("metalproto: live frame %d: %w", i, err)
			}
			prepStart := time.Now()
			upload, metrics, err := packing.prepare(scene, live, seconds, opts.VisualPasses)
			if err != nil {
				return nil, err
			}
			metrics.NextMS = nextMS
			metrics.PrepMS = float64(time.Since(prepStart)) / float64(time.Millisecond)
			liveRows[i] = metrics
			status = b.liveFrame(handle, uintptr(unsafe.Pointer(&upload)))
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
		actual++
	}
	submittedAt := time.Now()
	b.drain(handle)
	runtime.ReadMemStats(&after)
	n := int(b.rows(handle, pointer(rows), int32(len(rows))))
	runtime.KeepAlive(rows)
	rows = rows[:n]
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
	measured := rows[opts.Warmup:]
	report := makeReport(measured, opts, scene, device)
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
	report["visual_passes"] = opts.VisualPasses
	report["retained_material_bytes"] = len(scene.Materials)*16 + len(scene.TextureFrames)*16
	report["retained_height_bytes"] = len(scene.HeightField.Values) * 4
	report["retained_vertex_bytes"] = vertexBytes
	report["retained_index_bytes"] = indexBytes
	report["retained_instance_bytes"] = instanceBytes
	report["pose_upload_bytes_per_frame"] = uint64(poseCount) * 128
	report["triple_pose_buffer_bytes"] = uint64(poseCount) * 128 * 3
	report["model_draw_calls_per_frame"] = drawCalls
	report["model_vertices_instanced"] = drawnVertices
	report["model_indices_drawn"] = drawnIndices
	report["model_triangles_drawn"] = drawnIndices / 3
	report["texture_uploaded_bytes"] = len(scene.Atlas.RGBA) + len(scene.Terrain.RGBA)
	if scene.Live != nil {
		report["texture_uploaded_bytes"] = len(scene.Atlas.RGBA) + len(scene.Terrain.RGBA) + len(scene.SpriteAtlas.RGBA)
		addLiveReport(report, liveRows[opts.Warmup:n])
		if err := writeLiveRows(opts.OutputDir, liveRows[:n], opts.Warmup); err != nil {
			return nil, err
		}
	}
	report["terrain_triangles_drawn"] = 2
	if err := writeArtifacts(opts.OutputDir, report, rows, opts.Warmup); err != nil {
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
	}
	for _, t := range textures {
		if t.Width <= 0 || t.Height <= 0 || t.Width > math.MaxInt32 || t.Height > math.MaxInt32 || int64(t.Width)*int64(t.Height)*4 != int64(len(t.RGBA)) {
			return fmt.Errorf("metalproto: invalid RGBA texture")
		}
	}
	return nil
}
