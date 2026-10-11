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
	Index                                                    int
	Warmup                                                   bool
	Tick                                                     uint32
	Alpha                                                    float32
	SourceMS, EncodeMS, CallbackMS, PaceMS, CompletionWaitMS float64
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
	var failure error
	var capture *battleCapture
	var debugCapture *modelDebugCapture
	var capturePending bool
	var nativeDevice *wgpu.Device
	var live meshscene.LiveFrame
	debugWritten := false
	var last, next, timeStart, lastReturn time.Time
	var rows []drawRow
	var input []meshscene.NativeEvent
	var before, after runtime.MemStats
	var cpu *os.File
	startedMeasure := false
	stoppedMeasure := false
	draw := 0
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
		if opts.Benchmark != nil && opts.Benchmark.AfterMeasure != nil {
			return opts.Benchmark.AfterMeasure()
		}
		return nil
	}
	app.OnDraw(func(ctx *gogpu.Context) {
		entry := time.Now()
		defer func() { lastReturn = time.Now() }()
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
			coldMS = float64(time.Since(coldStart).Nanoseconds()) / 1e6
			if failure != nil {
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
		input = queue.drain(input)
		sourceStart := time.Now()
		live, failure = scene.Live.Next(start.Sub(timeStart).Seconds(), meshscene.Input{Events: input, At: start})
		sourceMS := float64(time.Since(sourceStart).Nanoseconds()) / 1e6
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
		completionStart := time.Now()
		if e := nativeDevice.WaitIdle(); e != nil {
			failure = e
			app.Quit()
			return
		}
		completionMS := float64(time.Since(completionStart).Nanoseconds()) / 1e6
		encodeStart := time.Now()
		c, e := renderer.Encode(ctx.CommandEncoder(), ctx.SurfaceView(), w, h, live)
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
		row := drawRow{draw, draw < opts.Warmup, live.Tick, live.Alpha, sourceMS, encodeMS, callbackMS, paceMS, completionMS, c, renderer.Missing()}
		if len(rows) >= 8192 {
			rows[draw%len(rows)] = row
		} else {
			rows = append(rows, row)
		}
		if draw >= opts.Warmup && opts.Benchmark != nil && opts.Benchmark.AfterFrame != nil {
			opts.Benchmark.AfterFrame(BenchmarkFrame{Frame: draw, CadenceValid: !last.IsZero(), PaceWait: paceMS, OutsideDraw: outsideMS, Cadence: callbackMS, DrawWork: float64(time.Since(start).Nanoseconds()) / 1e6, Source: sourceMS, Submit: encodeMS})
		}
		last = start
		draw++
		if failure != nil {
			app.Quit()
		}
	})
	app.OnClose(func() {
		if e := stopMeasure(); failure == nil {
			failure = e
		}
		if nativeDevice != nil {
			if e := nativeDevice.WaitIdle(); failure == nil {
				failure = e
			}
		}
		if debugCapture != nil {
			debugCapture.close()
		}
		if capture != nil {
			capture.close()
		}
		if renderer != nil {
			renderer.Close()
		}
	})
	runErr := app.Run()
	if failure == nil {
		failure = runErr
	}
	if draw > len(rows) && len(rows) > 0 {
		at := draw % len(rows)
		ordered := append([]drawRow(nil), rows[at:]...)
		rows = append(ordered, rows[:at]...)
	}
	report := map[string]any{"backend": "WebGPU native Go", "gogpu_commit": "12620c9d50073b8591f075c97ba0c6f3b6ad92f7", "physical_surface": []int{actualW, actualH}, "target_draw_fps": fps, "draws": draw, "warmup": opts.Warmup, "cold_host_renderer_ms": coldMS, "rows": rows, "gpu_timing_available": false, "present_timing_available": false, "queue_submission_timing_available": false, "prepare_ahead": false, "device": map[string]any{"layer_contents_scale": 2, "window_units": "logical DIP"}, "benchmark_memory": map[string]any{"alloc_bytes": after.TotalAlloc - before.TotalAlloc, "mallocs": after.Mallocs - before.Mallocs, "gc": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs, "heap_bytes_start": before.HeapAlloc, "heap_bytes_end": after.HeapAlloc, "go_sys_bytes_start": before.Sys, "go_sys_bytes_end": after.Sys, "scope": "process-wide Go counters during measured source+encode callbacks; excludes final drain and readback"}, "limits": []string{"windowed one battle; fullscreen remains dependency-blocked", "native loss and GPU/presentation timestamps unverified", "pinned GoGPU logs deferred Finish/Submit errors without returning them; qualification requires clean logs and expected pixel readbacks", "debug JSON describes first packet; field texture diagnostics describe final packet", "relative pointer capture and double-click count not yet forwarded", "synchronous source preparation and per-frame completion before mutable-resource replacement", "hardware model triangles approximate production integer span coverage", "grouped model solo chronology and erased holes remain approximated", "projectile body barriers, aircraft shadow silhouettes and penumbra remain approximated", "water surface/reflections, distortion and final depth-band smoothing omitted; RGBA8 world lacks production HDR headroom", "separate ground-light field clears its owned extent; production uses packed storage and partial clear"}, "input_overflow": queue.overflow}
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
	w.Write([]string{"frame", "warmup", "tick", "alpha", "source_ms", "encode_ms", "callback_ms", "pace_ms", "completion_wait_ms", "instances", "triangles", "model_draws", "sprite_draws", "upload_bytes", "gpu_ms", "presented_seconds"})
	for _, r := range rows {
		w.Write([]string{strconv.Itoa(r.Index), strconv.FormatBool(r.Warmup), fmt.Sprint(r.Tick), fmt.Sprint(r.Alpha), fmt.Sprint(r.SourceMS), fmt.Sprint(r.EncodeMS), fmt.Sprint(r.CallbackMS), fmt.Sprint(r.PaceMS), fmt.Sprint(r.CompletionWaitMS), fmt.Sprint(r.Counters.Models.Instances), fmt.Sprint(r.Counters.Models.Triangles), fmt.Sprint(r.Counters.Models.Draws), fmt.Sprint(r.Counters.Layers.Draws), fmt.Sprint(r.Counters.Models.UploadBytes + r.Counters.Layers.UploadBytes), "", ""})
	}
	w.Flush()
	return w.Error()
}
