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

// nativeRenderer is the native half of the prototype: the pure-Go Metal
// renderer (gobridge) or the Objective-C dylib it was ported from. Every call
// comes from Run's locked main thread. Pointers borrow Go storage only until
// the call returns; both implementations copy what they keep.
type nativeRenderer interface {
	terrainTiles(upload *nativeTerrainTilesUpload) int32
	pointerCapture(captured int32) int32
	mesh(vertices unsafe.Pointer, vertexCount int32, indices unsafe.Pointer, indexCount int32, offsets unsafe.Pointer, instances int32, edges unsafe.Pointer, edgeCount int32, visuals, centers unsafe.Pointer, primitiveCount int32) int32
	meshFaces(index uint32, upload *NativeMeshFacesUpload) int32
	meshOutlineRows(index uint32, upload *NativeOutlineRowsUpload) int32
	assets(a *nativeAssets) int32
	texture(slot, width, height int32, rgba unsafe.Pointer) int32
	configure(camera, rect unsafe.Pointer, poseCount int32) int32
	frame(previous, next unsafe.Pointer, alpha float32, seconds float64) int32
	liveUpload(u *liveUpload) int32
	livePresent() int32
	input(in *nativeInput)
	events(out []meshscene.NativeEvent) int32
	playable(on int32)
	drain()
	rows(out []nativeRow) int32
	capture(path string) int32
	info() string
	censusReset()
	destroy()
}

type dylibBridge struct {
	handle           uintptr
	meshOutlineRows_ func(uintptr, uint32, unsafe.Pointer) int32
	terrainTiles_    func(uintptr, unsafe.Pointer) int32
	meshFaces_       func(uintptr, uint32, unsafe.Pointer) int32
	pointerCapture_  func(uintptr, int32) int32
	create           func(int32, int32, int32, int32, int32, int32, int32, unsafe.Pointer, unsafe.Pointer, int32) uintptr
	mesh_            func(uintptr, unsafe.Pointer, int32, unsafe.Pointer, int32, unsafe.Pointer, int32, unsafe.Pointer, int32, unsafe.Pointer, unsafe.Pointer, int32) int32
	assets_          func(uintptr, unsafe.Pointer) int32
	texture_         func(uintptr, int32, int32, int32, unsafe.Pointer) int32
	configure_       func(uintptr, unsafe.Pointer, unsafe.Pointer, int32) int32
	frame_           func(uintptr, unsafe.Pointer, unsafe.Pointer, float32, float64) int32
	liveUpload_      func(uintptr, unsafe.Pointer) int32
	livePresent_     func(uintptr) int32
	input_           func(uintptr, unsafe.Pointer)
	events_          func(uintptr, unsafe.Pointer, int32) int32
	playable_        func(uintptr, int32)
	drain_           func(uintptr)
	rows_            func(uintptr, unsafe.Pointer, int32) int32
	capture_         func(uintptr, unsafe.Pointer) int32
	info_            func(uintptr, unsafe.Pointer, int32)
	censusReset_     func(uintptr)
	destroy_         func(uintptr)
}

// bindPerFrame replaces the calls made on every draw with direct symbol
// calls: a RegisterLibFunc wrapper goes through reflection and allocates its
// arguments on each call.
func (b *dylibBridge) bindPerFrame(lib uintptr) error {
	var symbols [5]uintptr
	for i, name := range [...]string{"nm_pointer_capture", "nm_live_upload", "nm_live_present", "nm_input", "nm_events"} {
		symbol, err := purego.Dlsym(lib, name)
		if err != nil {
			return fmt.Errorf("metalproto: bind %s: %w", name, err)
		}
		symbols[i] = symbol
	}
	b.pointerCapture_ = func(handle uintptr, captured int32) int32 {
		r, _, _ := purego.SyscallN(symbols[0], handle, uintptr(captured))
		return int32(r)
	}
	b.liveUpload_ = func(handle uintptr, upload unsafe.Pointer) int32 {
		r, _, _ := purego.SyscallN(symbols[1], handle, uintptr(upload))
		return int32(r)
	}
	b.livePresent_ = func(handle uintptr) int32 {
		r, _, _ := purego.SyscallN(symbols[2], handle)
		return int32(r)
	}
	b.input_ = func(handle uintptr, input unsafe.Pointer) {
		purego.SyscallN(symbols[3], handle, uintptr(input))
	}
	b.events_ = func(handle uintptr, events unsafe.Pointer, count int32) int32 {
		r, _, _ := purego.SyscallN(symbols[4], handle, uintptr(events), uintptr(count))
		return int32(r)
	}
	return nil
}

func newDylibRenderer(libPath string, width, height int32, offscreen, vsync, effects, visualPasses int32, capacity int32, shader string) (nativeRenderer, error) {
	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("metalproto: load %s: %w", libPath, err)
	}
	// Keep the dylib loaded: a drawable's presentation callback may finish after
	// the last command buffer has completed. Unmapping its callback code is unsafe.
	b := &dylibBridge{}
	purego.RegisterLibFunc(&b.create, lib, "nm_create")
	purego.RegisterLibFunc(&b.terrainTiles_, lib, "nm_terrain_tiles")
	purego.RegisterLibFunc(&b.pointerCapture_, lib, "nm_pointer_capture")
	purego.RegisterLibFunc(&b.mesh_, lib, "nm_mesh")
	purego.RegisterLibFunc(&b.meshFaces_, lib, "nm_mesh_faces")
	purego.RegisterLibFunc(&b.meshOutlineRows_, lib, "nm_mesh_outline_rows")
	purego.RegisterLibFunc(&b.assets_, lib, "nm_assets")
	purego.RegisterLibFunc(&b.texture_, lib, "nm_texture")
	purego.RegisterLibFunc(&b.configure_, lib, "nm_configure")
	purego.RegisterLibFunc(&b.frame_, lib, "nm_frame")
	purego.RegisterLibFunc(&b.liveUpload_, lib, "nm_live_upload")
	purego.RegisterLibFunc(&b.livePresent_, lib, "nm_live_present")
	purego.RegisterLibFunc(&b.input_, lib, "nm_input")
	purego.RegisterLibFunc(&b.events_, lib, "nm_events")
	purego.RegisterLibFunc(&b.playable_, lib, "nm_playable")
	purego.RegisterLibFunc(&b.drain_, lib, "nm_drain")
	purego.RegisterLibFunc(&b.rows_, lib, "nm_rows")
	purego.RegisterLibFunc(&b.capture_, lib, "nm_capture")
	purego.RegisterLibFunc(&b.info_, lib, "nm_info")
	purego.RegisterLibFunc(&b.censusReset_, lib, "nm_upload_census_reset")
	purego.RegisterLibFunc(&b.destroy_, lib, "nm_destroy")
	if err := b.bindPerFrame(lib); err != nil {
		return nil, err
	}
	source := cstring(shader)
	errorText := make([]byte, 8192)
	b.handle = b.create(width, height, offscreen, vsync, effects, visualPasses, capacity, pointer(source), pointer(errorText), int32(len(errorText)))
	runtime.KeepAlive(source)
	runtime.KeepAlive(errorText)
	if b.handle == 0 {
		return nil, fmt.Errorf("metalproto: initialize: %s", terminated(errorText))
	}
	return b, nil
}

func (b *dylibBridge) terrainTiles(u *nativeTerrainTilesUpload) int32 {
	return b.terrainTiles_(b.handle, unsafe.Pointer(u))
}
func (b *dylibBridge) pointerCapture(captured int32) int32 {
	return b.pointerCapture_(b.handle, captured)
}
func (b *dylibBridge) mesh(vertices unsafe.Pointer, vertexCount int32, indices unsafe.Pointer, indexCount int32, offsets unsafe.Pointer, instances int32, edges unsafe.Pointer, edgeCount int32, visuals, centers unsafe.Pointer, primitiveCount int32) int32 {
	return b.mesh_(b.handle, vertices, vertexCount, indices, indexCount, offsets, instances, edges, edgeCount, visuals, centers, primitiveCount)
}
func (b *dylibBridge) meshFaces(index uint32, u *NativeMeshFacesUpload) int32 {
	return b.meshFaces_(b.handle, index, unsafe.Pointer(u))
}
func (b *dylibBridge) meshOutlineRows(index uint32, u *NativeOutlineRowsUpload) int32 {
	return b.meshOutlineRows_(b.handle, index, unsafe.Pointer(u))
}
func (b *dylibBridge) assets(a *nativeAssets) int32 { return b.assets_(b.handle, unsafe.Pointer(a)) }
func (b *dylibBridge) texture(slot, width, height int32, rgba unsafe.Pointer) int32 {
	return b.texture_(b.handle, slot, width, height, rgba)
}
func (b *dylibBridge) configure(camera, rect unsafe.Pointer, poseCount int32) int32 {
	return b.configure_(b.handle, camera, rect, poseCount)
}
func (b *dylibBridge) frame(previous, next unsafe.Pointer, alpha float32, seconds float64) int32 {
	return b.frame_(b.handle, previous, next, alpha, seconds)
}
func (b *dylibBridge) liveUpload(u *liveUpload) int32 {
	return b.liveUpload_(b.handle, unsafe.Pointer(u))
}
func (b *dylibBridge) livePresent() int32    { return b.livePresent_(b.handle) }
func (b *dylibBridge) input(in *nativeInput) { b.input_(b.handle, unsafe.Pointer(in)) }
func (b *dylibBridge) events(out []meshscene.NativeEvent) int32 {
	return b.events_(b.handle, pointer(out), int32(len(out)))
}
func (b *dylibBridge) playable(on int32) { b.playable_(b.handle, on) }
func (b *dylibBridge) drain()            { b.drain_(b.handle) }
func (b *dylibBridge) rows(out []nativeRow) int32 {
	return b.rows_(b.handle, pointer(out), int32(len(out)))
}
func (b *dylibBridge) capture(path string) int32 {
	p := cstring(path)
	r := b.capture_(b.handle, pointer(p))
	runtime.KeepAlive(p)
	return r
}
func (b *dylibBridge) info() string {
	out := make([]byte, 8192)
	b.info_(b.handle, pointer(out), int32(len(out)))
	return terminated(out)
}
func (b *dylibBridge) censusReset() { b.censusReset_(b.handle) }
func (b *dylibBridge) destroy()     { b.destroy_(b.handle) }

func pointer[T any](s []T) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(&s[0])
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
	shader := waterShaderSource + "\n" + LightingShaderSource + "\n" + effectsShaderSource + "\n" + GlowShaderSource + "\n" + groundEffectsShaderSource + "\n" + projectionShaderSource + "\n" + facesShaderSource + "\n" + faceReflectionsShaderSource + "\n" + TerrainTilesShaderSource + "\n" + shaderSource + "\n" + groupShaderSource + "\n" + OutlineRowsShaderSource + "\n" + shadowShaderSource + "\n" + WaterTilesShaderSource + "\n" + waterObjectsShaderSource
	capacity := opts.Frames + opts.Warmup
	if interactive {
		capacity = 8192
	}
	nativeCapacity := capacity
	if interactive {
		nativeCapacity = -capacity
	}
	var b nativeRenderer
	var err error
	if libPath := opts.LibraryPath; libPath != "" && libPath != "go" {
		b, err = newDylibRenderer(libPath, int32(opts.Width), int32(opts.Height), boolInt(opts.Offscreen), boolInt(opts.VSync), boolInt(opts.Effects), boolInt(opts.VisualPasses && scene.Live != nil), int32(nativeCapacity), shader)
	} else {
		b, err = newGoRenderer(opts.Width, opts.Height, opts.Offscreen, opts.VSync, opts.Effects, opts.VisualPasses && scene.Live != nil, nativeCapacity, shader)
	}
	if err != nil {
		return nil, err
	}
	defer b.destroy()
	b.playable(boolInt(scene.Playable))
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
		if b.mesh(pointer(m.Vertices), int32(len(m.Vertices)), pointer(m.Indices), int32(len(m.Indices)), pointer(offsets), int32(len(offsets)), pointer(m.EdgeIndices), int32(len(m.EdgeIndices)), pointer(visuals), pointer(m.PrimitiveCenters), int32(len(m.PrimitiveCenters))) == 0 {
			return nil, fmt.Errorf("metalproto: upload mesh %s", m.Name)
		}
		faces, err := PrepareMeshFaces(&m)
		if err != nil {
			return nil, err
		}
		if b.meshFaces(uint32(drawCalls), &faces) == 0 {
			return nil, fmt.Errorf("metalproto: upload authored faces %s", m.Name)
		}
		outline, err := PrepareOutlineRows(&m)
		if err != nil {
			return nil, err
		}
		if b.meshOutlineRows(uint32(drawCalls), &outline.Native) == 0 {
			return nil, fmt.Errorf("metalproto: upload authored outline rings %s", m.Name)
		}
		vertexBytes += uint64(len(outline.Rings)) * 16
		runtime.KeepAlive(outline)
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
	if b.assets(&assets) == 0 {
		return nil, fmt.Errorf("metalproto: upload retained visual assets")
	}
	runtime.KeepAlive(scene)
	runtime.KeepAlive(assets)
	if scene.TerrainTiles != nil {
		upload, err := packTerrainTiles(scene.TerrainTiles)
		if err != nil {
			return nil, err
		}
		if b.terrainTiles(&upload) == 0 {
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
		if b.texture(int32(i), int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload texture %d", i)
		}
		runtime.KeepAlive(t)
	}
	for _, extra := range []struct {
		slot    int32
		texture meshscene.Texture
	}{{5, scene.WaterMask}, {6, scene.ModelPalette}, {7, scene.SpriteDetailAtlas}} {
		t := extra.texture
		if len(t.RGBA) > 0 && b.texture(extra.slot, int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload texture %d", extra.slot)
		}
		runtime.KeepAlive(t)
	}
	if t := scene.FogAtlas; len(t.RGBA) > 0 {
		if b.texture(4, int32(t.Width), int32(t.Height), pointer(t.RGBA)) == 0 {
			return nil, fmt.Errorf("metalproto: upload retained fog masks")
		}
		runtime.KeepAlive(t)
	}
	poseCount := 0
	if scene.Live == nil {
		poseCount = len(scene.Frames[0].Transforms)
	}
	if b.configure(pointer(scene.Camera[:]), pointer(scene.TerrainRect[:]), int32(poseCount)) == 0 {
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
	// A live frame is the source's Next plus packing. With PrepareAhead, the
	// next frame's preparation runs on a worker while this thread presents
	// (drawable wait and encoding) and paces, so the render thread's critical
	// path is the remaining join wait plus native submission. liveUpload
	// copies every caller buffer before it returns and the worker starts only
	// after that, so one set of Go buffers suffices. Native input is polled on
	// this thread before the worker starts: a prepared-ahead frame sees input
	// one draw earlier.
	type preparedLive struct {
		live           meshscene.LiveFrame
		upload         liveUpload
		metrics        liveRow
		source, packed float64
		err            error
	}
	prepareLive := func(seconds float64, in meshscene.Input) (p preparedLive) {
		start := time.Now()
		p.live, p.err = scene.Live.Next(seconds, in)
		p.source = float64(time.Since(start)) / float64(time.Millisecond)
		if p.err != nil || p.live.Exit {
			return p
		}
		start = time.Now()
		p.upload, p.metrics, p.err = packing.prepare(scene, p.live, seconds, opts.VisualPasses, opts.Effects)
		p.packed = float64(time.Since(start)) / float64(time.Millisecond)
		return p
	}
	// Interactive diagnostics: every successful present appends one native
	// row, so the presents counted here name those rows. Go-side phases wait
	// in a small ring until the row's GPU completes and its present resolves.
	var presents, reported uint64
	var goTimes [64][2]float64
	var recent [16]nativeRow
	var timing []meshscene.FrameTiming
	completedTiming := func() []meshscene.FrameTiming {
		timing = timing[:0]
		if !interactive || presents == reported {
			return timing
		}
		n := int(b.rows(recent[:min(presents-reported, uint64(len(recent)))]))
		if n <= 0 || n > len(recent) {
			return timing
		}
		first := presents - uint64(n)
		reported = max(reported, first)
		newest := recent[n-1].Submit
		for i := int(reported - first); i < n; i++ {
			row := recent[i]
			// Rows complete in order. A present normally resolves a few
			// refreshes after completion; one still unresolved after 100 ms
			// was never shown.
			if row.Completed == 0 || row.Presented == 0 && newest-row.Submit < .1 {
				break
			}
			g := goTimes[(first+uint64(i))%uint64(len(goTimes))]
			timing = append(timing, meshscene.FrameTiming{Presented: row.Presented, Next: g[0], Pack: g[1], Encode: row.Encode, Wait: row.Wait + row.DrawableWait, GPU: max(0, row.GPUEnd-row.GPUStart) * 1000})
			reported = first + uint64(i) + 1
		}
		return timing
	}
	pollLive := func(at time.Time) (meshscene.Input, error) {
		var input nativeInput
		b.input(&input)
		eventCount := int(b.events(events))
		runtime.KeepAlive(events)
		if eventCount < 0 || eventCount > len(events) {
			return meshscene.Input{}, fmt.Errorf("metalproto: invalid native event count")
		}
		return meshscene.Input{Events: events[:eventCount], PanX: input.PanX, PanZ: input.PanZ, Zoom: input.Zoom, PauseToggle: input.PauseToggle != 0, At: at, Timing: completedTiming()}, nil
	}
	sourceSeconds := func(frame int, at time.Time) float64 {
		if opts.Benchmark != nil {
			return float64(frame) / float64(opts.FPS)
		}
		return at.Sub(started).Seconds()
	}
	// One persistent worker prepares ahead; the render thread hands it one
	// request at a time and joins before the next.
	type prepareRequest struct {
		seconds float64
		in      meshscene.Input
	}
	var requests chan prepareRequest
	prepared := make(chan preparedLive, 1)
	ahead := false
	if scene.Live != nil && opts.PrepareAhead != nil {
		requests = make(chan prepareRequest)
		go func() {
			for r := range requests {
				prepared <- prepareLive(r.seconds, r.in)
			}
		}()
	}
	defer func() {
		if ahead {
			<-prepared
		}
		if requests != nil {
			close(requests)
		}
	}()
	// One prepared frame outlives the loop, so keeping it alive across the
	// native upload never boxes a copy.
	var p preparedLive
	for i := 0; interactive || i < len(rows); i++ {
		var entry time.Time
		if opts.Benchmark != nil {
			entry = time.Now()
		}
		if i == opts.Warmup && opts.Benchmark != nil {
			if err := measurement.begin(); err != nil {
				return nil, err
			}
			b.censusReset()
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
			// The measurement boundary runs host callbacks, so the first
			// measured frame is never prepared across it; nothing follows the
			// final frame.
			next := i + 1
			prepareNext := opts.PrepareAhead != nil && (interactive || next < len(rows)) &&
				(opts.Benchmark == nil || next != opts.Warmup) && opts.PrepareAhead(next)
			p = preparedLive{}
			var nextInput meshscene.Input
			polled := false
			if ahead {
				joinStart := time.Now()
				p = <-prepared
				ahead = false
				hostRow.Ahead = true
				hostRow.Join = float64(time.Since(joinStart)) / 1e6
				// Poll now when the next frame is prepared ahead: the newest
				// pointer places this frame's cursor, and the same input then
				// feeds the next frame's preparation.
				if prepareNext && p.err == nil && !p.live.Exit {
					var err error
					if nextInput, err = pollLive(time.Time{}); err != nil {
						return nil, err
					}
					polled = true
					lateCursor(&p.live, nextInput.Events)
				}
			} else {
				in, err := pollLive(drawStart)
				if err != nil {
					return nil, err
				}
				p = prepareLive(sourceSeconds(i, drawStart), in)
			}
			hostRow.Source, hostRow.Packing = p.source, p.packed
			if p.err != nil {
				return nil, fmt.Errorf("metalproto: live frame %d: %w", i, p.err)
			}
			if p.live.Exit {
				break
			}
			if b.pointerCapture(boolInt(p.live.PointerCaptured)) == 0 {
				return nil, fmt.Errorf("metalproto: native pointer capture unavailable")
			}
			p.metrics.NextMS, p.metrics.PrepMS = p.source, p.packed
			frameMetrics = p.metrics
			var submitStart time.Time
			if opts.Benchmark != nil {
				submitStart = time.Now()
			}
			status = b.liveUpload(&p.upload)
			runtime.KeepAlive(&p)
			runtime.KeepAlive(&packing)
			if status == 1 {
				if prepareNext {
					at := time.Now()
					if opts.FPS > 0 && deadline.Add(interval).After(at) {
						at = deadline.Add(interval)
					}
					if !polled {
						var err error
						if nextInput, err = pollLive(at); err != nil {
							return nil, err
						}
					}
					nextInput.At = at
					requests <- prepareRequest{sourceSeconds(next, at), nextInput}
					ahead = true
				}
				status = b.livePresent()
				if status == 1 {
					goTimes[presents%uint64(len(goTimes))] = [2]float64{p.source, p.packed}
					presents++
				}
			}
			if opts.Benchmark != nil {
				hostRow.Submit = float64(time.Since(submitStart)) / 1e6
			}
		} else {
			sample := seconds * 30
			tick := int(sample)
			alpha := float32(sample - float64(tick))
			a := scene.Frames[tick%len(scene.Frames)].Transforms
			next := scene.Frames[(tick+1)%len(scene.Frames)].Transforms
			status = b.frame(pointer(a), pointer(next), alpha, seconds)
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
	b.drain()
	if opts.Benchmark != nil {
		after = measurement.after
	} else {
		runtime.ReadMemStats(&after)
	}
	n := int(b.rows(rows))
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
	var device map[string]any
	if err := json.Unmarshal([]byte(b.info()), &device); err != nil {
		return nil, err
	}
	if _, ok := device["native_bridge"]; !ok {
		device["native_bridge"] = "objective-c"
	}
	if b.capture(filepath.Join(opts.OutputDir, "final.png")) == 0 {
		return nil, fmt.Errorf("metalproto: final PNG readback failed")
	}
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
	if t := scene.TerrainTiles; t != nil {
		tileBytes := len(t.Atlas.RGBA) + len(t.Detail.RGBA) + len(t.Lookup)*4
		report["texture_uploaded_bytes"] = report["texture_uploaded_bytes"].(int) + tileBytes
	}
	report["terrain_triangles_drawn"] = 2
	if err := writeArtifacts(opts.OutputDir, report, rows, opts.Warmup, firstFrame); err != nil {
		return nil, err
	}
	return report, nil
}

// lateCursor moves a prepared-ahead frame's movable cursor quads to the newest
// pointer sample in events, where the source's own late positioning would have
// placed them had it run now.
func lateCursor(f *meshscene.LiveFrame, events []meshscene.NativeEvent) {
	if len(f.CursorQuads) == 0 || f.CursorScale <= 0 {
		return
	}
	x, y, found := 0, 0, false
	for _, e := range events {
		if e.Kind == 1 || e.Kind == 2 || e.Kind == 3 || e.Kind == 9 {
			x, y, found = int(e.X/f.CursorScale), int(e.Y/f.CursorScale), true
		}
	}
	if !found {
		return
	}
	dx, dy := float32(x-f.CursorAt[0])*f.CursorScale, float32(y-f.CursorAt[1])*f.CursorScale
	for _, i := range f.CursorQuads {
		if i >= 0 && i < len(f.Overlay) {
			f.Overlay[i].Rect[0] += dx
			f.Overlay[i].Rect[1] += dy
		}
	}
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
