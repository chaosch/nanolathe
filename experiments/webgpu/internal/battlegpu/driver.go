package battlegpu

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type Options struct {
	Width, Height, Frames, Warmup, FPS      int
	Offscreen, VSync, Effects, VisualPasses bool
	OutputDir                               string
	Benchmark                               *BenchmarkOptions
	PrepareAhead                            func(int) bool
}
type BenchmarkOptions struct {
	BeforeMeasure, AfterMeasure func() error
	AfterFrame                  func(BenchmarkFrame)
}
type BenchmarkFrame struct {
	Frame                                                                   int
	CadenceValid, Ahead                                                     bool
	PaceWait, OutsideDraw, DrawWork, Cadence, Source, Packing, Submit, Join float64
}
type drawRow struct {
	NativeEncodeStartSeconds               float64
	SourceStagesMS                         [12]float64
	Index                                  int
	Warmup                                 bool
	Tick                                   uint32
	Alpha                                  float32
	SourceMS, EncodeMS, CallbackMS, PaceMS float64
	LateAcquireMS, CommandEncodeMS         float64
	// Shown is false for a prepared frame whose late drawable was unavailable.
	LateAcquire, Shown                      bool
	JoinMS                                  float64
	PreparedAhead                           bool
	PreviousSubmission, CompletedSubmission uint64
	Camera                                  [3]float32
	SourceSampleAgeMS, SourceReadyAgeMS     float64
	PublishedTick                           uint32
	PublishedTickAvailable                  bool
	Counters                                Counters
	Missing                                 map[string]int
}

// Run is a windowed real-battle experiment, using the production LiveSource.
// Native GoGPU currently lacks usable public GPU/scanout timestamps. These
// observations are CPU and callback cadence only, never GPU/presentation time.
func Run(scene *meshscene.Scene, opts Options) (map[string]any, error) {
	if scene == nil || scene.Live == nil {
		return nil, fmt.Errorf("WebGPU battle requires a production LiveSource")
	}
	if opts.Offscreen {
		return nil, fmt.Errorf("WebGPU battle offscreen host is not qualified; use bounded windowed capture")
	}
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("WebGPU battle requires positive physical dimensions")
	}
	if opts.OutputDir != "" {
		if e := os.MkdirAll(opts.OutputDir, 0755); e != nil {
			return nil, e
		}
	}
	fps := opts.FPS
	if fps <= 0 {
		fps = 120
	}
	period := time.Second / time.Duration(fps)
	_, sourceResizable := scene.Live.(physicalExtentSource)
	// GoGPU sizes windows in logical units. The hint is the platform's usual
	// scale; the first draw corrects the request if the actual scale differs.
	hint := windowScaleHint()
	logicalSize := func(physical int, scale float64) int { return int(math.Round(float64(physical) / scale)) }
	cfg := gogpu.DefaultConfig().WithTitle("Nanolathe — WebGPU prototype").WithSize(logicalSize(opts.Width, hint), logicalSize(opts.Height, hint)).WithResizable(sourceResizable && opts.Benchmark == nil).WithContinuousRender(true).WithVSync(opts.VSync)
	app := gogpu.NewApp(cfg)
	runningApp.Store(app)
	defer runningApp.CompareAndSwap(app, nil)
	// Late acquisition takes the drawable only after source preparation; the
	// stock host acquires lazily on the first encoder or view request.
	lateAcquire := os.Getenv("NANOLATHE_WEBGPU_LATE_ACQUIRE") == "1"
	var lifecycleEpoch atomic.Uint64
	// Frame statistics tell a callback that the host is replaying it after
	// an outdated present (see the replay branch in OnDraw).
	gogpu.EnableStats()
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() { <-signalContext.Done(); app.Quit() }()
	// Pointer input arrives in logical units; the host expects drawable pixels.
	queue := eventQueue{scale: float32(hint)}
	events := app.EventSource()
	events.OnKeyPress(func(k gpucontext.Key, m gpucontext.Modifiers) { queue.key(4, k, m) })
	events.OnKeyRelease(func(k gpucontext.Key, m gpucontext.Modifiers) { queue.key(5, k, m) })
	events.OnTextInput(func(text string) {
		queue.mu.Lock()
		mods := queue.mods
		queue.mu.Unlock()
		for _, c := range text {
			queue.add(meshscene.NativeEvent{Kind: 7, Key: uint32(c), Modifiers: mods})
		}
	})
	// TODO(question): relative pointer capture needs a working pinned macOS
	// SetCursorMode; its darwin implementation is empty. Native click count is
	// also absent from the public event API, so double-click edges stay unqualified.
	events.OnMouseMove(func(x, y float64) { queue.pointer(1, 0, x, y) })
	events.OnMousePress(func(b gpucontext.MouseButton, x, y float64) { queue.pointer(2, uint32(b), x, y) })
	events.OnMouseRelease(func(b gpucontext.MouseButton, x, y float64) { queue.pointer(3, uint32(b), x, y) })
	events.OnFocus(func(f bool) {
		if !f {
			queue.mu.Lock()
			queue.mods = 0
			queue.mu.Unlock()
		}
		key := uint32(0)
		if f {
			key = 1
		}
		queue.add(meshscene.NativeEvent{Kind: 8, Key: key})
		lifecycleEpoch.Add(1)
	})
	if scroll, ok := events.(gpucontext.ScrollEventSource); ok {
		scroll.OnScrollEvent(func(s gpucontext.ScrollEvent) {
			key := uint32(4)
			if s.DeltaMode == gpucontext.ScrollDeltaPixel {
				key |= 1
			}
			if s.IsMomentum {
				key |= 2
			}
			queue.mu.Lock()
			scale := queue.scale
			queue.mu.Unlock()
			queue.add(meshscene.NativeEvent{Kind: 1, Key: key, Button: uint32(int32(-s.DeltaY)), Modifiers: uint32(s.Modifiers) & 15, X: float32(s.X) * scale, Y: float32(s.Y) * scale, WheelX: float32(s.DeltaX), WheelY: float32(s.DeltaY)})
		})
	}
	var actualW, actualH int
	var extentWaits int
	var deviceScale float64
	var graphicsBackend string
	var runtimeModeAtInit map[string]bool
	var renderer *Renderer
	var surfaceFormat gputypes.TextureFormat
	var lastLifecycleEpoch uint64
	var lateUnavailable, lateResized, lateFrames, staleSources, earlyUnavailable uint64
	var sourceRedraws, outdatedRedraws uint64
	// encodedLast says the previous callback encoded a frame into a drawable;
	// redrewLast says that frame was itself a re-presentation.
	var encodedLast, redrewLast bool
	var retainedDescriptors [2]map[string]any
	var failure error
	var capture *battleCapture
	var debugCapture *modelDebugCapture
	var capturePending bool
	var nativeDevice *wgpu.Device
	var submittedAtClose, completedAtClose uint64
	var live meshscene.LiveFrame
	disableStateCache := os.Getenv("NANOLATHE_WEBGPU_DISABLE_STATE_CACHE") == "1"
	disableModelBatching := os.Getenv("NANOLATHE_WEBGPU_DISABLE_MODEL_BATCHING") == "1"
	compactSlotPacking := os.Getenv("NANOLATHE_WEBGPU_COMPACT_SLOT_PACKING") == "1"
	disableCoalesce := os.Getenv("NANOLATHE_WEBGPU_DISABLE_EFFECT_COALESCE") == "1"
	omitReflections := os.Getenv("NANOLATHE_WEBGPU_OMIT_STOCK_REFLECTIONS") == "1"
	omitSeabed := os.Getenv("NANOLATHE_WEBGPU_OMIT_SEABED") == "1"
	omitShadows := os.Getenv("NANOLATHE_WEBGPU_OMIT_MODEL_SHADOWS") == "1"
	omitWater := os.Getenv("NANOLATHE_WEBGPU_OMIT_WATER") == "1"
	omitDistortion := os.Getenv("NANOLATHE_WEBGPU_OMIT_DISTORTION") == "1"
	controlledLens := os.Getenv("NANOLATHE_WEBGPU_PROBE_LENS_CONTROLLED") == "1"
	probeLens := os.Getenv("NANOLATHE_WEBGPU_PROBE_LENS") == "1" || controlledLens
	lensFixture := battleLensFixture{controlled: controlledLens}
	probeDirectLight := os.Getenv("NANOLATHE_WEBGPU_PROBE_DIRECT_LIGHT") == "1"
	directLightFixture := battleDirectLightFixture{disabled: os.Getenv("NANOLATHE_WEBGPU_PROBE_DIRECT_LIGHT_OFF") == "1"}
	debugWritten := false
	var last, next, timeStart, lastReturn, lastSourceSampleAt time.Time
	var rows []drawRow
	var presentationTelemetry any
	var gpuTimingTelemetry any
	gpuTimingRequested := os.Getenv("NANOLATHE_WEBGPU_GPU_TIMING") == "1"
	var input []meshscene.NativeEvent
	neutralPointer := os.Getenv("NANOLATHE_WEBGPU_BENCHMARK_NEUTRAL_POINTER") == "1"
	if neutralPointer && (opts.Benchmark != nil || opts.Frames <= 0 || opts.OutputDir == "") {
		return nil, fmt.Errorf("neutral pointer requires a bounded interactive diagnostic with output")
	}
	sampleInput := func() []meshscene.NativeEvent {
		input = queue.drain(input)
		if neutralPointer {
			queue.mu.Lock()
			mods := queue.mods
			queue.mu.Unlock()
			input = append(input, meshscene.NativeEvent{Kind: 1, X: float32(actualW) / 2, Y: float32(actualH) / 2, Modifiers: mods})
		}
		return input
	}
	var before, after runtime.MemStats
	var cpu *os.File
	startedMeasure := false
	stoppedMeasure := false
	draw := 0
	// Keep the worker explicit until its gain is repeatable across large scenes.
	// Where the page's requestAnimationFrame owns cadence (the browser) a
	// frame callback can never wait on the source, so preparation always runs
	// on the worker there unless explicitly disabled.
	prepareAheadEnv := os.Getenv("NANOLATHE_WEBGPU_PREPARE_AHEAD")
	prepareAhead := opts.PrepareAhead != nil && (prepareAheadEnv == "1" || !hostPacesFrames && prepareAheadEnv != "0")
	var preparer *sourceWorker
	if prepareAhead {
		preparer = newSourceWorker(scene.Live)
	}
	defer preparer.close()

	out := outputWriter{deferred: !callbackFileIO}
	coldStart := time.Now()
	var coldMS float64
	stopMeasure := func() error {
		if !startedMeasure || stoppedMeasure {
			return nil
		}
		stoppedMeasure = true
		runtime.ReadMemStats(&after)
		if cpu != nil {
			pprof.StopCPUProfile()
			cpu.Close()
			cpu = nil
		}
		// Profiles read and stream files (the heap profile reads the process
		// memory map), so only hosts with callback file access record them.
		if opts.OutputDir != "" && callbackFileIO {
			// Flush sampled allocation stacks only after freezing exact counters
			// and stopping CPU timing. Delta profiles include profiler bookkeeping;
			// MemStats above remains the source for exact measured-window totals.
			runtime.GC()
			for _, profile := range []struct{ name, file string }{{"allocs", "alloc.pprof"}, {"heap", "heap.pprof"}} {
				data, e := battleProfile(profile.name)
				if e == nil {
					e = out.write(filepath.Join(opts.OutputDir, profile.file), data)
				}
				if e != nil {
					return e
				}
			}
		}
		if opts.Benchmark != nil && opts.Benchmark.AfterMeasure != nil {
			return opts.Benchmark.AfterMeasure()
		}
		return nil
	}
	callbackFailureLogged := false
	app.OnDraw(func(ctx *gogpu.Context) {
		entry := time.Now()
		defer func() {
			lastReturn = time.Now()
			// The pinned multi-thread host can panic while quitting after a
			// callback error. Preserve the actual renderer failure first.
			if failure != nil && !callbackFailureLogged {
				fmt.Fprintf(os.Stderr, "nanolathe: WebGPU callback failed: logical path presentation/battle, providers searched [pinned GoGPU native host], expected a completed draw: %v\n", failure)
				callbackFailureLogged = true
			}
		}()
		if failure != nil {
			app.Quit()
			return
		}
		// The stock host replays a callback once, at once, when presenting its
		// frame found the surface outdated and reconfigured it. That callback
		// already consumed its source sample and input, so the replay presents
		// the same prepared frame again from renderer-owned state and keeps the
		// pacing deadline. A re-presentation is never itself re-presented.
		redrew := redrewLast
		redrewLast = false
		if renderer != nil && !capturePending && encodedLast && !redrew && ctx.Renderer() != nil && ctx.Renderer().GetFrameStats().PresentSkipped {
			encodedLast = false
			if enc := ctx.CommandEncoder(); enc != nil {
				if view := ctx.SurfaceView(); view != nil {
					if failure = renderer.Redraw(enc, view); failure != nil {
						app.Quit()
						return
					}
					encodedLast, redrewLast = true, true
					outdatedRedraws++
				}
			}
			return
		}
		encodedLast = false
		epoch := lifecycleEpoch.Load()
		interactive := opts.Benchmark == nil
		if lateAcquire && interactive && (epoch != lastLifecycleEpoch || !lastReturn.IsZero() && entry.Sub(lastReturn) > 2*period) {
			// A focus change or stall leaves a prepared sample whose instant has
			// passed. Discard it after its serial Next joins; input and state that
			// Next consumed are never replayed.
			terminal, err := dropPreparedSource(preparer)
			if err != nil || terminal {
				failure = err
				app.Quit()
				return
			}
			staleSources++
			next = entry
		}
		lastLifecycleEpoch = epoch
		w, h := ctx.FramebufferSize()
		acquireLate := lateAcquire && renderer != nil && !capturePending &&
			w == actualW && h == actualH && renderer.width == w && renderer.height == h
		if !acquireLate && renderer != nil && !capturePending {
			// Early acquisition takes the drawable before source preparation.
			enc := ctx.CommandEncoder()
			var view *wgpu.TextureView
			if enc != nil {
				view = ctx.SurfaceView()
			}
			if enc == nil || view == nil {
				// No drawable (outdated, lost or not yet configured) and nothing
				// consumed yet; the host schedules another callback.
				earlyUnavailable++
				return
			}
			w, h = ctx.FramebufferSize()
		}

		if renderer == nil {
			if w != opts.Width || h != opts.Height {
				// The scale hint was wrong for this display: request the logical
				// size that yields the requested drawable once, then allow the
				// platform a bounded number of callbacks to apply it.
				if extentWaits == 0 && ctx.ScaleFactor() > 0 && ctx.ScaleFactor() != hint {
					app.RequestSize(logicalSize(opts.Width, ctx.ScaleFactor()), logicalSize(opts.Height, ctx.ScaleFactor()))
					extentWaits = 1
					return
				}
				if extentWaits > 0 && extentWaits < 120 {
					extentWaits++
					return
				}
				failure = fmt.Errorf("WebGPU physical extent %dx%d; expected %dx%d", w, h, opts.Width, opts.Height)
				app.Quit()
				return
			}
			if scale := ctx.ScaleFactor(); scale > 0 {
				deviceScale = scale
				queue.mu.Lock()
				queue.scale = float32(scale)
				queue.mu.Unlock()
			}
			graphicsBackend = ctx.Backend()
			// GoGPU falls back to its CPU rasterizer when the requested backend
			// offers no adapter (for example MoltenVK hidden by the Vulkan loader).
			// Name that instead of failing later on its unsupported compute passes.
			// Browser adapters carry no backend identity and also report Empty.
			if runtime.GOOS != "js" && (strings.HasSuffix(graphicsBackend, "(Empty)") || strings.HasSuffix(graphicsBackend, "(Software)")) {
				failure = fmt.Errorf("WebGPU battle requires a hardware adapter; GoGPU selected %s because the requested backend offered none", graphicsBackend)
				app.Quit()
				return
			}
			p := app.DeviceProvider()
			nativeDevice = p.Device()
			surfaceFormat = p.SurfaceFormat()
			actualW, actualH = w, h
			renderer, failure = New(p.Device(), p.Queue(), p.SurfaceFormat(), scene)
			if renderer != nil && failure == nil {
				renderer.omitSeabed = omitSeabed
				renderer.paintState.disabled = disableStateCache
				renderer.models.SetSourceBatching(!disableModelBatching)
				renderer.models.SetSourcePacking(compactSlotPacking)
				renderer.layers.coalesceEffects = !disableCoalesce
				runtimeModeAtInit = map[string]bool{"models": renderer.models.runtimeArrays, "layers": renderer.layers.runtimeArrays, "glow": renderer.glow != nil && renderer.glow.runtimeArrays}
			}
			coldMS = float64(time.Since(coldStart).Nanoseconds()) / 1e6
			if failure != nil {
				renderer = nil // Constructor already retired its partial renderer.
				app.Quit()
				return
			}
			return
		}
		if capturePending {
			if debugCapture != nil {
				failure = debugCapture.finish(&out)
				debugCapture.close()
				debugCapture = nil
				if failure != nil {
					app.Quit()
					return
				}
			}
			data, ready, e := capture.poll()
			if e != nil {
				failure = e
				app.Quit()
				return
			}
			if !ready {
				return
			}
			var encoded []byte
			if encoded, failure = capture.encodePNG(data); failure == nil {
				failure = out.write(filepath.Join(opts.OutputDir, "final.png"), encoded)
			}
			capturePending = false
			app.Quit()
			return
		}
		if opts.Frames > 0 && draw >= opts.Warmup+opts.Frames {
			if e := stopMeasure(); e != nil {
				failure = e
				app.Quit()
				return
			}
			if opts.OutputDir != "" {
				// Capture owns a separate, checked completion boundary before any
				// extra untimed upload or encoding reuses the mutable renderer.
				if failure = nativeDevice.WaitIdle(); failure != nil {
					app.Quit()
					return
				}
				data, err := json.MarshalIndent(map[string]any{"tick": live.Tick, "alpha": live.Alpha, "camera": live.Camera, "water": live.Water, "distortions": live.Distortions, "model_groups": len(live.Composition.Groups), "feature_sprites": live.Composition.FeatureSprites, "composition": live.Composition, "shadows": map[string]any{"subjects": live.Shadows.Subjects, "body_draws": len(live.Shadows.BodyDraws), "projected_draws": len(live.Shadows.ProjectedDraws), "atlas": []int{live.Shadows.AtlasWidth, live.Shadows.AtlasHeight}}, "water_sources": len(live.WaterSources), "water_object_scales": live.WaterObjectScales, "world_format": renderer.format}, "", "  ")
				if err == nil {
					err = out.write(filepath.Join(opts.OutputDir, "final-fields.json"), data)
				}
				if err != nil {
					failure = err
					app.Quit()
					return
				}
				// Final capture belongs to the last consumed packet. A native resize
				// after that draw must not reinterpret its composition/shadow bounds.
				captureWidth, captureHeight := renderer.width, renderer.height
				capture, failure = newBattleCapture(nativeDevice, app.DeviceProvider().SurfaceFormat(), captureWidth, captureHeight)
				if failure == nil {
					_, failure = renderer.Encode(ctx.CommandEncoder(), capture.view, captureWidth, captureHeight, live)
				}
				if failure == nil {
					capture.encode(ctx.CommandEncoder())
					if path := os.Getenv("NANOLATHE_WEBGPU_DEBUG_FRAME"); path != "" {
						debugCapture, failure = captureModelDebug(nativeDevice, ctx.CommandEncoder(), renderer, path, &out)
						if failure != nil {
							app.Quit()
							return
						}
					}
					capturePending = true
					return
				}
			}
			app.Quit()
			return
		}
		if w != actualW || h != actualH {
			terminal, err := resizeSource(preparer, scene.Live, w, h)
			if err != nil || terminal {
				failure = err
				app.Quit()
				return
			}
			actualW, actualH = w, h
		}
		next, timeStart = startSourceTimeline(next, timeStart, entry)
		paceStart := time.Now()
		if wait := time.Until(next); wait > 0 && hostPacesFrames {
			time.Sleep(wait)
		}
		start := time.Now()
		paceMS := float64(start.Sub(paceStart).Nanoseconds()) / 1e6
		next = next.Add(period)
		if !next.After(start) {
			next = start.Add(period)
		}
		if draw == opts.Warmup && !startedMeasure {
			if opts.Benchmark != nil && opts.Benchmark.BeforeMeasure != nil {
				if e := opts.Benchmark.BeforeMeasure(); e != nil {
					failure = e
					app.Quit()
					return
				}
			}
			runtime.GC()
			if opts.OutputDir != "" && callbackFileIO {
				data, e := battleProfile("allocs")
				if e == nil {
					e = out.write(filepath.Join(opts.OutputDir, "alloc-base.pprof"), data)
				}
				if e != nil {
					failure = e
					app.Quit()
					return
				}
				cpu, e = os.Create(filepath.Join(opts.OutputDir, "cpu.pprof"))
				if e == nil {
					e = pprof.StartCPUProfile(cpu)
				}
				if e != nil {
					failure = e
					app.Quit()
					return
				}
			}
			runtime.ReadMemStats(&before)
			startedMeasure = true
			// Setup and the warmup-to-measurement interval are outside timing.
			start = time.Now()
			next = start.Add(period)
			last = time.Time{}
		}
		if !hostPacesFrames && preparer != nil {
			// The browser callback starts the source on the worker and returns
			// at once while it is outstanding, presenting the last prepared
			// picture again; the page's event loop, which completes the
			// source's file and fetch reads, runs between callbacks.
			if !preparer.pending {
				input = sampleInput()
				preparer.start(start.Sub(timeStart).Seconds(), meshscene.Input{Events: input, At: start})
			}
			if preparer.waiting() {
				if e := renderer.Redraw(ctx.CommandEncoder(), ctx.SurfaceView()); e != nil {
					failure = e
					app.Quit()
				}
				encodedLast = true
				sourceRedraws++
				return
			}
		}
		prepared, ahead, joinMS := preparer.join()
		if !ahead {
			input = sampleInput()
			sourceStart := time.Now()
			sampleAt := start
			if lateAcquire {
				sampleAt = sourceSampleTime(lastSourceSampleAt, sampleAt)
			}
			lastSourceSampleAt = sampleAt
			prepared.frame, prepared.err = scene.Live.Next(sampleAt.Sub(timeStart).Seconds(), meshscene.Input{Events: input, At: sampleAt})
			prepared.readyAt = time.Now()
			prepared.sampleAt = sampleAt
			prepared.stages = sourcePreparationTimings(scene.Live)
			prepared.milliseconds = float64(time.Since(sourceStart).Nanoseconds()) / 1e6
		}
		live, failure = prepared.frame, prepared.err
		sourceMS := prepared.milliseconds
		// Explicit A/B qualification controls change detached presentation only.
		if probeLens {
			lensFixture.apply(&live)
		}
		if probeDirectLight {
			directLightFixture.apply(&live)
		}
		if omitWater {
			live.Water.Mask[3] = 0
		}
		if omitReflections {
			live.WaterSources = nil
		}
		if omitShadows {
			live.Shadows = meshscene.ShadowComposition{}
		}
		if omitDistortion {
			live.Distortions = nil
		}
		if failure != nil || live.Exit {
			app.Quit()
			return
		}
		if path := os.Getenv("NANOLATHE_WEBGPU_DEBUG_FRAME"); path != "" && !debugWritten {
			meshes := make(map[int]meshscene.Mesh)
			for _, i := range live.Instances {
				if i.Mesh >= 0 && i.Mesh < len(scene.Meshes) {
					meshes[i.Mesh] = scene.Meshes[i.Mesh]
				}
			}
			data, err := json.Marshal(map[string]any{"camera": live.Camera, "alpha": live.Alpha, "generation": live.Generation, "instances": live.Instances, "previous": live.Previous, "current": live.Current, "rules": live.ModelRules, "projections": live.ModelProjections, "composition": live.Composition, "shadows": live.Shadows, "lighting": live.Lighting, "meshes": meshes, "materials": scene.Materials, "texture_frames": scene.TextureFrames, "model_palette": scene.ModelPalette, "atlas": scene.Atlas})
			if err == nil {
				err = out.write(path, data)
			}
			if err != nil {
				failure = err
				app.Quit()
				return
			}
			debugWritten = true
		}
		// No completion wait: buffer writes are staged by the queue and texture
		// rewrites by the renderer, both ordered behind earlier frames' GPU
		// work, and replaced resources are released by wgpu only after the
		// submissions that used them complete.
		encodeStart := time.Now()
		nativeEncodeStart := float64(0)
		if gpuTimingRequested {
			if p, ok := any(nativeDevice).(interface{ NativeGPUClockSeconds() float64 }); ok {
				nativeEncodeStart = p.NativeGPUClockSeconds()
			}
		}
		// Current diagnostics consumed the packet above. Renderer preparation
		// owns every later command operand before this callback starts Next.
		nextDraw := draw + 1
		var sourceReady func()
		if prepareAhead && sourceAheadBoundary(nextDraw, opts.Warmup, opts.Frames) && opts.PrepareAhead(nextDraw) {
			sourceReady = func() {
				input = sampleInput()
				at := time.Now()
				if next.After(at) {
					at = next
				}
				if lateAcquire {
					at = sourceSampleTime(lastSourceSampleAt, at)
				}
				lastSourceSampleAt = at
				preparer.start(at.Sub(timeStart).Seconds(), meshscene.Input{Events: input, At: at})
			}
		}
		var c Counters
		var e error
		unavailable := false
		if acquireLate {
			resized := false
			acquire := func() (*wgpu.CommandEncoder, *wgpu.TextureView, error) {
				enc, view, changed, err := acquireFrame(ctx, w, h, surfaceFormat)
				resized = changed
				return enc, view, err
			}
			c, e = renderer.encodeSourceReadyAcquire(nil, nil, w, h, live, sourceReady, acquire)
			switch {
			case errors.Is(e, errFrameUnavailable):
				// The frame was prepared and its source handed back, but is not
				// shown. Its staged copies stay pending for the next encoder and
				// its prepared state stays valid for Redraw; nothing is reread.
				e, unavailable = nil, true
				lateUnavailable++
			case e == nil:
				lateFrames++
				if resized {
					lateResized++
				}
			}
		} else {
			c, e = renderer.encodeSourceReady(ctx.CommandEncoder(), ctx.SurfaceView(), w, h, live, sourceReady)
		}
		encodedLast = e == nil && !unavailable

		failure = e
		encodeMS := float64(time.Since(encodeStart).Nanoseconds()) / 1e6
		outsideMS := 0.0
		if !last.IsZero() && !lastReturn.IsZero() {
			outsideMS = float64(entry.Sub(lastReturn).Nanoseconds()) / 1e6
		}
		callbackMS := 0.0
		if !last.IsZero() {
			callbackMS = float64(start.Sub(last).Nanoseconds()) / 1e6
		}
		var publishedTick uint32
		var publishedTickAvailable bool
		if source, ok := scene.Live.(interface{ PublicationTick() (uint32, bool) }); ok {
			publishedTick, publishedTickAvailable = source.PublicationTick()
		}
		row := drawRow{NativeEncodeStartSeconds: nativeEncodeStart, Index: draw, Warmup: draw < opts.Warmup, Tick: live.Tick, Alpha: live.Alpha, SourceMS: sourceMS, EncodeMS: encodeMS, LateAcquireMS: c.LateAcquireMS, CommandEncodeMS: c.CommandEncodeMS, LateAcquire: acquireLate, Shown: !unavailable, CallbackMS: callbackMS, PaceMS: paceMS, JoinMS: joinMS, PreparedAhead: ahead, Camera: live.Camera, SourceStagesMS: prepared.stages, SourceSampleAgeMS: float64(encodeStart.Sub(prepared.sampleAt).Nanoseconds()) / 1e6, SourceReadyAgeMS: float64(encodeStart.Sub(prepared.readyAt).Nanoseconds()) / 1e6, PublishedTick: publishedTick, PublishedTickAvailable: publishedTickAvailable, PreviousSubmission: nativeDevice.Queue().LastSubmissionIndex(), CompletedSubmission: nativeDevice.Queue().Poll(), Counters: c, Missing: renderer.Missing()}
		if len(rows) >= 8192 {
			rows[draw%len(rows)] = row
		} else {
			rows = append(rows, row)
		}
		if draw >= opts.Warmup && opts.Benchmark != nil && opts.Benchmark.AfterFrame != nil {
			opts.Benchmark.AfterFrame(BenchmarkFrame{Frame: draw, Ahead: ahead, Join: joinMS, CadenceValid: !last.IsZero(), PaceWait: paceMS, OutsideDraw: outsideMS, Cadence: callbackMS, DrawWork: float64(time.Since(start).Nanoseconds()) / 1e6, Source: sourceMS, Submit: encodeMS})
		}
		last = start
		draw++
		if failure != nil {
			app.Quit()
		}
	})
	app.OnClose(func() {
		if e := preparer.close(); failure == nil {
			failure = e
		}
		if e := stopMeasure(); failure == nil {
			failure = e
		}
		if nativeDevice != nil {
			if e := nativeDevice.WaitIdle(); e != nil {
				if failure == nil {
					failure = e
				}
				return // No capture/renderer destruction with completion unknown.
			}
			if provider, ok := any(nativeDevice).(interface{ NativePresentationTelemetry(bool) any }); ok {
				presentationTelemetry = provider.NativePresentationTelemetry(true)
				if provider, ok := any(nativeDevice).(interface{ NativeGPUTiming(bool) any }); ok {
					gpuTimingTelemetry = provider.NativeGPUTiming(true)
				}
			}
			submittedAtClose = nativeDevice.Queue().LastSubmissionIndex()
			completedAtClose = nativeDevice.Queue().Poll()
		}
		if debugCapture != nil {
			debugCapture.close()
		}
		if capture != nil {
			capture.close()
		}
		if renderer != nil {
			retainedDescriptors[0] = renderer.resourceDescriptors()
			renderer.Close()
			retainedDescriptors[1] = renderer.resourceDescriptors()
		}
	})
	runErr := app.Run()
	if presentationTelemetry == nil && nativeDevice != nil {
		if provider, ok := any(nativeDevice).(interface{ NativePresentationTelemetry(bool) any }); ok {
			presentationTelemetry = provider.NativePresentationTelemetry(true)
			if provider, ok := any(nativeDevice).(interface{ NativeGPUTiming(bool) any }); ok {
				gpuTimingTelemetry = provider.NativeGPUTiming(true)
			}
		}
	}
	// A host that ends without OnClose still needs its CPU-only source and
	// profiling cleanup finished before the result is collected.
	if e := preparer.close(); failure == nil {
		failure = e
	}
	if e := stopMeasure(); failure == nil {
		failure = e
	}
	if failure == nil {
		failure = runErr
	}
	if e := out.flush(); failure == nil {
		failure = e
	}
	if draw > len(rows) && len(rows) > 0 {
		at := draw % len(rows)
		ordered := append([]drawRow(nil), rows[at:]...)
		rows = append(ordered, rows[:at]...)
	}
	report := map[string]any{"backend": "WebGPU native Go", "gogpu_commit": "12620c9d50073b8591f075c97ba0c6f3b6ad92f7", "physical_surface": []int{actualW, actualH}, "target_draw_fps": fps, "draws": draw, "warmup": opts.Warmup, "cold_host_renderer_ms": coldMS, "rows": rows, "gpu_timing_available": false, "present_timing_available": false, "queue_submission_timing_available": false, "prepare_ahead": prepareAhead, "source_ownership": "one serial worker; next source starts after all Prepare CPU reads/uploads and copied paint records, before command encoding; measurement boundaries and final capture stop preparation", "device": map[string]any{"layer_contents_scale": deviceScale, "window_scale_hint": hint, "window_units": "logical DIP"}, "benchmark_memory": map[string]any{"alloc_bytes": after.TotalAlloc - before.TotalAlloc, "mallocs": after.Mallocs - before.Mallocs, "gc": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs, "heap_bytes_start": before.HeapAlloc, "heap_bytes_end": after.HeapAlloc, "go_sys_bytes_start": before.Sys, "go_sys_bytes_end": after.Sys, "scope": "process-wide Go counters during measured source+encode callbacks; excludes final drain and readback"}, "limits": []string{"windowed one battle; fullscreen remains dependency-blocked", "native loss and GPU/presentation timestamps unverified", "pinned GoGPU logs deferred Finish/Submit errors without returning them; qualification requires clean logs and expected pixel readbacks", "debug JSON describes first packet; field texture diagnostics describe final packet", "relative pointer capture is blocked by pinned macOS SetCursorMode stub; native click count is absent from the public event API", "optional source preparation overlaps host submission/pacing; frames are not drained: queue writes and staged texture copies are ordered behind in-flight work and released resources outlive their last submission", "hardware model triangles approximate production integer span coverage; doubled hardware outline lines lack whole-native-pixel row blocks", "immutable grouped source/key composition is active; cached/live seed chronology and exact integer coverage remain approximated", "projectile body barriers and retained solo/projected shadows are active; cached/live shadow chronology and offscreen shadow-only producer coverage remain incomplete", "water surface and radial distortion are active with RGBA16F world; stock reflections are active; model reflections/refraction and mixed source budget remain incomplete", "separate ground-light field clears its owned extent; production uses packed storage and partial clear"}, "input_overflow": queue.overflow, "diagnostic_sources": map[string]bool{"compact_slot_packing": compactSlotPacking, "authored_stock_lens": probeLens, "controlled_lens_stock_replacement": controlledLens, "controlled_direct_light_stock_replacement": probeDirectLight, "controlled_direct_light_off": directLightFixture.disabled}, "diagnostic_omissions": map[string]bool{"water_surface": omitWater, "distortion": omitDistortion, "state_cache": disableStateCache, "model_batching": disableModelBatching, "effect_coalescing": disableCoalesce, "stock_reflections": omitReflections, "seabed": omitSeabed, "model_shadows": omitShadows}}
	report["resizable_source"] = sourceResizable && opts.Benchmark == nil
	report["target"] = runtime.GOOS + "/" + runtime.GOARCH
	report["graphics_backend"] = graphicsBackend
	report["host_paces_frames"] = hostPacesFrames
	report["source_redraws"] = sourceRedraws
	report["profiles_recorded"] = opts.OutputDir != "" && callbackFileIO
	report["late_acquire"] = map[string]any{"requested": lateAcquire, "frames": lateFrames, "unavailable": lateUnavailable, "resized": lateResized, "stale_sources_dropped": staleSources, "early_unavailable": earlyUnavailable, "scope": "stock lazy acquisition: the encoder and drawable are requested only after preparation and the source handoff; setup, resize and capture frames acquire before preparation; an unavailable drawable leaves the prepared frame unshown, its staged copies pending and its source never reread; focus changes and stalls drop a stale prepared sample"}
	report["outdated_redraws"] = map[string]any{"count": outdatedRedraws, "scope": "the stock host replays a callback once after an outdated present reconfigures the surface; the replay re-presents the last prepared frame from renderer-owned state, consuming no source or input"}
	report["encode_timing_scope"] = "EncodeMS/common Submit/DrawWork include late acquisition but exclude upfront early acquisition; CommandEncodeMS excludes preparation/source handoff/acquisition; nested wall durations are not additive"
	report["native_presentation_telemetry"] = presentationTelemetry
	report["native_gpu_timing"] = gpuTimingTelemetry
	report["native_encode_clock_scope"] = "CACurrentMediaTime sampled immediately after Go encodeStart; per-frame clock bridge approximate by getter latency; source sample/ready time inferred from existing monotonic ages; no additional waits"
	report["texture_uploads"] = "rewrites of textures earlier frames sampled are packed into one arena, uploaded with Queue.WriteBuffer and copied on the frame encoder before any pass; textures created this frame are written directly; no per-frame completion wait"
	report["runtime_arrays"] = map[string]any{"requested": os.Getenv("NANOLATHE_WEBGPU_RUNTIME_ARRAYS") == "1", "enabled": runtimeModeAtInit["models"], "renderer_components_at_init": runtimeModeAtInit, "scope": "runtime-sized WGSL arrays need a render-size capability no stock wgpu provides; fixed arrays are used"}
	report["storage_reservation"] = map[string]any{"enabled": storageReserveEnabled(), "model_buffer_floor_bytes": modelStorageReserveBytes, "glow_buffer_floor_bytes": glowStorageReserveBytes, "scope": "opt-in buffer reservation and fixed-array pipeline precompilation; normal growth retained, textures unchanged"}
	report["diagnostic_neutral_pointer"] = neutralPointer
	report["phase_timing_scope"] = map[string]any{"source_stages": []string{"host_step", "pin_presentation", "world_build_or_recull", "visuals", "foreground_hud", "frame_policy_projection", "composition_selection", "shadows", "visual_record_adapter", "visual_lighting_sources", "visual_record", "visual_adapt"}, "renderer_stages": []string{"targets", "models", "glow", "ground_fields", "layers", "distortions", "reflections", "lenses_paint", "commands"}, "scope": "CPU wall durations; source stages copied before starting next Next; overlapping source/render stages are not additive"}
	report["source_age_scope"] = "requested source sample timestamp and preparation completion to encode start; negative sample age is predictive lead; publication tick is latest committed tick, not a wall-age timestamp"
	report["submission_failure_contract"] = "stock dependencies log deferred Finish/Submit failures without returning them; qualification requires clean logs and expected pixel readbacks"
	report["submission_indices_at_close"] = map[string]uint64{"submitted": submittedAtClose, "completed": completedAtClose}
	report["retained_descriptor_snapshots"] = retainedDescriptors
	if failure != nil {
		report["run_error"] = failure.Error()
	}
	if opts.OutputDir != "" {
		data, reportErr := json.MarshalIndent(report, "", "  ")
		if reportErr == nil {
			reportErr = os.WriteFile(filepath.Join(opts.OutputDir, "webgpu-report.json"), append(data, '\n'), 0644)
		}
		if failure == nil {
			failure = reportErr
		}
		if e := writeRows(filepath.Join(opts.OutputDir, "webgpu-frames.csv"), rows); failure == nil {
			failure = e
		}
	}
	return report, failure
}
func writeRows(path string, rows []drawRow) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"frame", "warmup", "tick", "alpha", "source_ms", "encode_ms", "callback_ms", "pace_ms", "shown", "instances", "triangles", "model_draws", "sprite_draws", "upload_bytes", "gpu_ms", "presented_seconds", "prepared_ahead", "source_join_ms", "camera_x", "camera_z", "camera_scale", "source_sample_age_ms", "source_ready_age_ms", "published_tick", "published_tick_available"})
	for _, r := range rows {
		w.Write([]string{strconv.Itoa(r.Index), strconv.FormatBool(r.Warmup), fmt.Sprint(r.Tick), fmt.Sprint(r.Alpha), fmt.Sprint(r.SourceMS), fmt.Sprint(r.EncodeMS), fmt.Sprint(r.CallbackMS), fmt.Sprint(r.PaceMS), strconv.FormatBool(r.Shown), fmt.Sprint(r.Counters.Models.Instances), fmt.Sprint(r.Counters.Models.Triangles), fmt.Sprint(r.Counters.Models.Draws), fmt.Sprint(r.Counters.Layers.Draws), fmt.Sprint(r.Counters.Models.UploadBytes + r.Counters.Layers.UploadBytes), "", "", strconv.FormatBool(r.PreparedAhead), fmt.Sprint(r.JoinMS), fmt.Sprint(r.Camera[0]), fmt.Sprint(r.Camera[1]), fmt.Sprint(r.Camera[2]), fmt.Sprint(r.SourceSampleAgeMS), fmt.Sprint(r.SourceReadyAgeMS), fmt.Sprint(r.PublishedTick), strconv.FormatBool(r.PublishedTickAvailable)})
	}
	w.Flush()
	return w.Error()
}
