//go:build darwin

package battlegpu

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
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
	SourceStagesMS                                           [12]float64
	Index                                                    int
	Warmup                                                   bool
	Tick                                                     uint32
	Alpha                                                    float32
	SourceMS, EncodeMS, CallbackMS, PaceMS, CompletionWaitMS float64
	JoinMS                                                   float64
	PreparedAhead                                            bool
	PreviousSubmission, CompletedSubmission                  uint64
	Camera                                                   [3]float32
	SourceSampleAgeMS, SourceReadyAgeMS                      float64
	PublishedTick                                            uint32
	PublishedTickAvailable                                   bool
	Counters                                                 Counters
	Missing                                                  map[string]int
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
	cfg := gogpu.DefaultConfig().WithTitle("Nanolathe — WebGPU prototype").WithSize(opts.Width/2, opts.Height/2).WithResizable(false).WithContinuousRender(true).WithVSync(opts.VSync)
	app := gogpu.NewApp(cfg)
	asyncUploads := os.Getenv("NANOLATHE_WEBGPU_ASYNC_UPLOADS") == "1"
	if asyncUploads {
		contract, ok := any(app).(interface{ SubmissionFailuresReturned() bool })
		if !ok || !contract.SubmissionFailuresReturned() {
			return nil, fmt.Errorf("asynchronous uploads require the reviewed GoGPU submission error patch")
		}
	}
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() { <-signalContext.Done(); app.Quit() }()
	queue := eventQueue{scale: 2}
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
	var renderer *Renderer
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
	var last, next, timeStart, lastReturn time.Time
	var rows []drawRow
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
			input = append(input, meshscene.NativeEvent{Kind: 1, X: float32(opts.Width) / 2, Y: float32(opts.Height) / 2, Modifiers: mods})
		}
		return input
	}
	var before, after runtime.MemStats
	var cpu *os.File
	startedMeasure := false
	stoppedMeasure := false
	draw := 0
	// Keep the worker explicit until its gain is repeatable across large scenes.
	prepareAhead := opts.PrepareAhead != nil && os.Getenv("NANOLATHE_WEBGPU_PREPARE_AHEAD") == "1"
	var preparer *sourceWorker
	if prepareAhead {
		preparer = newSourceWorker(scene.Live)
	}
	defer preparer.close()
	actualW, actualH := 0, 0
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
		if opts.OutputDir != "" {
			// Flush sampled allocation stacks only after freezing exact counters
			// and stopping CPU timing. Delta profiles include profiler bookkeeping;
			// MemStats above remains the source for exact measured-window totals.
			runtime.GC()
			for _, profile := range []struct{ name, file string }{{"allocs", "alloc.pprof"}, {"heap", "heap.pprof"}} {
				if e := writeBattleProfile(filepath.Join(opts.OutputDir, profile.file), profile.name); e != nil {
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
				if abort, ok := any(app).(interface{ AbortFrame(error) }); ok {
					abort.AbortFrame(failure)
				}
				fmt.Fprintf(os.Stderr, "nanolathe: WebGPU callback failed: logical path presentation/battle, providers searched [pinned GoGPU native host], expected a completed draw: %v\n", failure)
				callbackFailureLogged = true
			}
		}()
		if failure != nil {
			app.Quit()
			return
		}
		ctx.CommandEncoder()
		ctx.SurfaceView()
		w, h := ctx.FramebufferSize()
		if renderer == nil {
			p := app.DeviceProvider()
			nativeDevice = p.Device()
			actualW, actualH = w, h
			if w != opts.Width || h != opts.Height {
				failure = fmt.Errorf("WebGPU physical extent %dx%d; expected %dx%d", w, h, opts.Width, opts.Height)
				app.Quit()
				return
			}
			renderer, failure = New(p.Device(), p.Queue(), p.SurfaceFormat(), scene)
			if renderer != nil && failure == nil {
				renderer.omitSeabed = omitSeabed
				renderer.paintState.disabled = disableStateCache
				renderer.models.SetSourceBatching(!disableModelBatching)
				renderer.models.SetSourcePacking(compactSlotPacking)
				renderer.layers.coalesceEffects = !disableCoalesce
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
				failure = debugCapture.finish()
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
			failure = capture.save(filepath.Join(opts.OutputDir, "final.png"), data)
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
					err = os.WriteFile(filepath.Join(opts.OutputDir, "final-fields.json"), data, 0644)
				}
				if err != nil {
					failure = err
					app.Quit()
					return
				}
				capture, failure = newBattleCapture(nativeDevice, app.DeviceProvider().SurfaceFormat(), w, h)
				if failure == nil {
					_, failure = renderer.Encode(ctx.CommandEncoder(), capture.view, w, h, live)
				}
				if failure == nil {
					capture.encode(ctx.CommandEncoder())
					if path := os.Getenv("NANOLATHE_WEBGPU_DEBUG_FRAME"); path != "" {
						debugCapture, failure = captureModelDebug(nativeDevice, ctx.CommandEncoder(), renderer, path)
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
		if next.IsZero() {
			next = entry
			timeStart = entry
		}
		paceStart := time.Now()
		if wait := time.Until(next); wait > 0 {
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
			if opts.OutputDir != "" {
				if e := writeBattleProfile(filepath.Join(opts.OutputDir, "alloc-base.pprof"), "allocs"); e != nil {
					failure = e
					app.Quit()
					return
				}
				var e error
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
		prepared, ahead, joinMS := preparer.join()
		if !ahead {
			input = sampleInput()
			sourceStart := time.Now()
			prepared.frame, prepared.err = scene.Live.Next(start.Sub(timeStart).Seconds(), meshscene.Input{Events: input, At: start})
			prepared.readyAt = time.Now()
			prepared.sampleAt = start
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
				err = os.WriteFile(path, data, 0644)
			}
			if err != nil {
				failure = err
				app.Quit()
				return
			}
			debugWritten = true
		}
		completionMS := 0.0
		if !asyncUploads {
			completionStart := time.Now()
			if failure = nativeDevice.WaitIdle(); failure != nil {
				app.Quit()
				return
			}
			completionMS = float64(time.Since(completionStart).Nanoseconds()) / 1e6
		} else {
			contract, ok := any(nativeDevice).(interface{ CompletionSafeUploads() bool })
			if !ok || !contract.CompletionSafeUploads() {
				failure = fmt.Errorf("asynchronous uploads require the reviewed wgpu ownership/completion patch")
				app.Quit()
				return
			}
		}
		encodeStart := time.Now()
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
				preparer.start(at.Sub(timeStart).Seconds(), meshscene.Input{Events: input, At: at})
			}
		}
		c, e := renderer.encodeSourceReady(ctx.CommandEncoder(), ctx.SurfaceView(), w, h, live, sourceReady)
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
		row := drawRow{Index: draw, Warmup: draw < opts.Warmup, Tick: live.Tick, Alpha: live.Alpha, SourceMS: sourceMS, EncodeMS: encodeMS, CallbackMS: callbackMS, PaceMS: paceMS, CompletionWaitMS: completionMS, JoinMS: joinMS, PreparedAhead: ahead, Camera: live.Camera, SourceStagesMS: prepared.stages, SourceSampleAgeMS: float64(encodeStart.Sub(prepared.sampleAt).Nanoseconds()) / 1e6, SourceReadyAgeMS: float64(encodeStart.Sub(prepared.readyAt).Nanoseconds()) / 1e6, PublishedTick: publishedTick, PublishedTickAvailable: publishedTickAvailable, PreviousSubmission: nativeDevice.Queue().LastSubmissionIndex(), CompletedSubmission: nativeDevice.Queue().Poll(), Counters: c, Missing: renderer.Missing()}
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
	// Quarantined native shutdown skips OnClose. CPU-only source/profiling
	// cleanup must still finish before collecting the result.
	if e := preparer.close(); failure == nil {
		failure = e
	}
	if e := stopMeasure(); failure == nil {
		failure = e
	}
	if failure == nil {
		failure = runErr
	}
	if draw > len(rows) && len(rows) > 0 {
		at := draw % len(rows)
		ordered := append([]drawRow(nil), rows[at:]...)
		rows = append(ordered, rows[:at]...)
	}
	report := map[string]any{"backend": "WebGPU native Go", "gogpu_commit": "12620c9d50073b8591f075c97ba0c6f3b6ad92f7", "physical_surface": []int{actualW, actualH}, "target_draw_fps": fps, "draws": draw, "warmup": opts.Warmup, "cold_host_renderer_ms": coldMS, "rows": rows, "gpu_timing_available": false, "present_timing_available": false, "queue_submission_timing_available": false, "prepare_ahead": prepareAhead, "source_ownership": "one serial worker; next source starts after all Prepare CPU reads/uploads and copied paint records, before command encoding; measurement boundaries and final capture stop preparation", "device": map[string]any{"layer_contents_scale": 2, "window_units": "logical DIP"}, "benchmark_memory": map[string]any{"alloc_bytes": after.TotalAlloc - before.TotalAlloc, "mallocs": after.Mallocs - before.Mallocs, "gc": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs, "heap_bytes_start": before.HeapAlloc, "heap_bytes_end": after.HeapAlloc, "go_sys_bytes_start": before.Sys, "go_sys_bytes_end": after.Sys, "scope": "process-wide Go counters during measured source+encode callbacks; excludes final drain and readback"}, "limits": []string{"windowed one battle; fullscreen remains dependency-blocked", "native loss and GPU/presentation timestamps unverified", "pinned GoGPU logs deferred Finish/Submit errors without returning them; qualification requires clean logs and expected pixel readbacks", "debug JSON describes first packet; field texture diagnostics describe final packet", "relative pointer capture is blocked by pinned macOS SetCursorMode stub; native click count is absent from the public event API", "optional source preparation overlaps host submission/pacing; per-frame GPU completion still protects mutable-resource replacement", "hardware model triangles approximate production integer span coverage; doubled hardware outline lines lack whole-native-pixel row blocks", "immutable grouped source/key composition is active; cached/live seed chronology and exact integer coverage remain approximated", "projectile body barriers and retained solo/projected shadows are active; cached/live shadow chronology and offscreen shadow-only producer coverage remain incomplete", "water surface and radial distortion are active with RGBA16F world; stock reflections are active; model reflections/refraction and mixed source budget remain incomplete", "separate ground-light field clears its owned extent; production uses packed storage and partial clear"}, "input_overflow": queue.overflow, "diagnostic_sources": map[string]bool{"compact_slot_packing": compactSlotPacking, "authored_stock_lens": probeLens, "controlled_lens_stock_replacement": controlledLens, "controlled_direct_light_stock_replacement": probeDirectLight, "controlled_direct_light_off": directLightFixture.disabled}, "diagnostic_omissions": map[string]bool{"water_surface": omitWater, "distortion": omitDistortion, "state_cache": disableStateCache, "model_batching": disableModelBatching, "effect_coalescing": disableCoalesce, "stock_reflections": omitReflections, "seabed": omitSeabed, "model_shadows": omitShadows}}
	report["asynchronous_uploads"] = asyncUploads
	report["storage_reservation"] = map[string]any{"enabled": storageReserveEnabled(), "model_buffer_floor_bytes": modelStorageReserveBytes, "glow_buffer_floor_bytes": glowStorageReserveBytes, "scope": "opt-in buffer reservation and fixed-array pipeline precompilation; normal growth retained, textures unchanged"}
	report["diagnostic_neutral_pointer"] = neutralPointer
	report["phase_timing_scope"] = map[string]any{"source_stages": []string{"host_step", "pin_presentation", "world_build_or_recull", "visuals", "foreground_hud", "frame_policy_projection", "composition_selection", "shadows", "visual_record_adapter", "visual_lighting_sources", "visual_record", "visual_adapt"}, "renderer_stages": []string{"targets", "models", "glow", "ground_fields", "layers", "distortions", "reflections", "lenses_paint", "commands"}, "scope": "CPU wall durations; source stages copied before starting next Next; overlapping source/render stages are not additive"}
	report["source_age_scope"] = "requested source sample timestamp and preparation completion to encode start; negative sample age is predictive lead; publication tick is latest committed tick, not a wall-age timestamp"
	report["submission_failure_contract"] = "stock dependencies log deferred failures; reviewed host patch returns first failure from Run"
	report["submission_indices_at_close"] = map[string]uint64{"submitted": submittedAtClose, "completed": completedAtClose}
	if asyncUploads {
		report["submission_failure_contract"] = "reviewed host returns first Finish/Submit/wait failure; completion unknown preserves native graph"
		report["upload_ownership"] = "one renderer; immutable staged writes retain destinations until real queue completion; no per-draw whole-device drain"
	}
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
func writeBattleProfile(path, name string) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	e = pprof.Lookup(name).WriteTo(f, 0)
	closed := f.Close()
	if e != nil {
		return e
	}
	return closed
}

func writeRows(path string, rows []drawRow) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"frame", "warmup", "tick", "alpha", "source_ms", "encode_ms", "callback_ms", "pace_ms", "completion_wait_ms", "instances", "triangles", "model_draws", "sprite_draws", "upload_bytes", "gpu_ms", "presented_seconds", "prepared_ahead", "source_join_ms", "camera_x", "camera_z", "camera_scale", "source_sample_age_ms", "source_ready_age_ms", "published_tick", "published_tick_available"})
	for _, r := range rows {
		w.Write([]string{strconv.Itoa(r.Index), strconv.FormatBool(r.Warmup), fmt.Sprint(r.Tick), fmt.Sprint(r.Alpha), fmt.Sprint(r.SourceMS), fmt.Sprint(r.EncodeMS), fmt.Sprint(r.CallbackMS), fmt.Sprint(r.PaceMS), fmt.Sprint(r.CompletionWaitMS), fmt.Sprint(r.Counters.Models.Instances), fmt.Sprint(r.Counters.Models.Triangles), fmt.Sprint(r.Counters.Models.Draws), fmt.Sprint(r.Counters.Layers.Draws), fmt.Sprint(r.Counters.Models.UploadBytes + r.Counters.Layers.UploadBytes), "", "", strconv.FormatBool(r.PreparedAhead), fmt.Sprint(r.JoinMS), fmt.Sprint(r.Camera[0]), fmt.Sprint(r.Camera[1]), fmt.Sprint(r.Camera[2]), fmt.Sprint(r.SourceSampleAgeMS), fmt.Sprint(r.SourceReadyAgeMS), fmt.Sprint(r.PublishedTick), strconv.FormatBool(r.PublishedTickAvailable)})
	}
	w.Flush()
	return w.Error()
}
