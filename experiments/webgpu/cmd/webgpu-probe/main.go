package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/host"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/measure"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/render"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

func main() {
	runtime.LockOSThread()
	frames := flag.Int("frames", 120, "smoke draws before close; 0 keeps the window open")
	diagnostics := flag.Bool("diagnostics", false, "controlled invalid shader and binding diagnostics")
	workload := flag.Bool("workload", false, "retained authored workload")
	count := flag.Int("count", 64, "instances")
	detail := flag.Int("detail", 200, "triangles per instance")
	qualify := flag.Bool("qualify", false, "untimed GPU pose/color/depth/visibility diagnostics")
	cycles := flag.Int("cycles", 0, "completion-checked lifecycle cycles")
	mode := flag.String("mode", "animated", "animated/static/camera fixture")
	width := flag.Int("width", 960, "logical window width; verify physical extent")
	height := flag.Int("height", 540, "logical window height; verify physical extent")
	out := flag.String("out", "", "outside-repository diagnostic image directory")
	measurement := flag.Bool("measure", false, "collect 240 warmup + 3600 measured draws; P0/P2/P3 qualification required before launch")
	refresh := flag.Float64("refresh", 0, "verified display refresh Hz; 0 means unavailable")
	wantW := flag.Int("physical-width", 1920, "required physical extent for measurements")
	wantH := flag.Int("physical-height", 1080, "required physical extent for measurements")
	flag.Parse()
	if *measurement {
		if !*workload || *qualify || *cycles > 0 {
			fmt.Fprintln(os.Stderr, "measure requires plain retained workload")
			os.Exit(2)
		}
		*frames = measure.Warmup + measure.Measured
	}
	var observations []measure.Frame
	if *measurement {
		observations = make([]measure.Frame, 0, measure.Warmup+measure.Measured)
	}
	var cached fixture.Snapshot
	var previousCallback time.Time
	var beforeMemory, afterMemory runtime.MemStats
	var measuredStarted bool
	var pendingWaitMS float64
	var coldMS float64
	app := gogpu.NewApp(gogpu.DefaultConfig().WithTitle("Nanolathe WebGPU qualification").WithSize(*width, *height).WithResizable(true).WithContinuousRender(true))
	var smoke *host.Smoke
	var retained *render.Renderer
	var authored *fixture.Workload
	var capture *host.Capture
	var capturePending bool
	var captureStarted time.Time
	qualification := []host.Comparison{}
	var qualificationCounters []render.Counters
	const cases = 5
	var failure error
	var fence *host.FrameFence
	var fenceStarted time.Time
	var extentW, extentH int
	var resizePending [2]int
	var inputAt atomic.Int64
	var inputSerial atomic.Uint64
	var reflectedInput uint64
	var inputLog []map[string]any
	app.EventSource().OnKeyPress(func(key gpucontext.Key, _ gpucontext.Modifiers) {
		inputAt.Store(time.Now().UnixNano())
		inputSerial.Add(1)
		switch key {
		case gpucontext.KeyQ:
			app.Quit()
		case gpucontext.KeyF:
			app.ToggleFullscreen()
		case gpucontext.KeyR:
			app.PrimaryWindow().SetSize(800, 450)
		case gpucontext.KeyM:
			app.Minimize()
		}
	})
	var cycleDraws, cycleCount int
	var lifecycle []map[string]any
	app.OnResize(func(w, h int) {
		b, _ := json.Marshal(map[string]any{"event": "resize", "width": w, "height": h})
		fmt.Println(string(b))
	})
	app.OnFocus(func(f bool) {
		b, _ := json.Marshal(map[string]any{"event": "focus", "focused": f})
		fmt.Println(string(b))
	})
	draws := 0
	readback := false
	preparedSurface := false
	start := time.Now()
	app.OnDraw(func(ctx *gogpu.Context) {
		callback := time.Now()
		waitMS := pendingWaitMS
		pendingWaitMS = 0
		interval := measure.Value{}
		if !previousCallback.IsZero() {
			interval = measure.Number(float64(callback.Sub(previousCallback).Nanoseconds()) / 1e6)
		}
		if failure != nil {
			return
		}
		if host.StopRequested() {
			app.Quit()
			return
		}
		if lost := host.DeviceLost(); lost != "" {
			failure = fmt.Errorf("device lost: %s", lost)
			app.Quit()
			return
		}
		if fence != nil {
			waitStart := time.Now()
			ready, e := fence.Poll()
			waitMS += float64(time.Since(waitStart).Nanoseconds()) / 1e6
			if e != nil || (!ready && time.Since(fenceStarted) > 5*time.Second) {
				failure = errors.Join(e, fmt.Errorf("frame completion unavailable or timed out"))
				app.Quit()
				return
			}
			if !ready {
				pendingWaitMS = waitMS
				return
			}
			fence = nil
			if resizePending[0] > 0 {
				failure = retained.Resize(resizePending[0], resizePending[1])
				extentW, extentH = resizePending[0], resizePending[1]
				resizePending = [2]int{}
				b, _ := json.Marshal(map[string]any{"event": "targets-resized", "physical_width": extentW, "physical_height": extentH, "resources": retained.Resources()})
				fmt.Println(string(b))
				if failure != nil {
					app.Quit()
					return
				}
			}
			if *cycles > 0 && cycleDraws > 0 {
				before := retained.Resources()
				retained.Close()
				after := retained.Resources()
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				cycleCount++
				entry := map[string]any{"process_memory": host.ProcessMemory(), "cycle": cycleCount, "before": before, "after": after, "heap_allocated_bytes": memory.HeapAlloc, "heap_system_bytes": memory.HeapSys, "allocation_total_bytes": memory.TotalAlloc, "completion_wait_ms": float64(time.Since(fenceStarted).Nanoseconds()) / 1e6}
				lifecycle = append(lifecycle, entry)
				b, _ := json.Marshal(entry)
				fmt.Println("lifecycle=" + string(b))
				if after.Count != 0 || after.Bytes != 0 {
					failure = fmt.Errorf("resource baseline not restored")
					app.Quit()
					return
				}
				retained = nil
				cycleDraws = 0
				if cycleCount == *cycles {
					app.Quit()
					return
				}
			}
		}
		if *workload {
			ctx.CommandEncoder()
			ctx.SurfaceView()
		}
		if *workload && !preparedSurface {
			preparedSurface = true
			return
		}
		if smoke == nil && retained == nil {
			p := app.DeviceProvider()
			if *diagnostics {
				failure = host.Diagnostics(p.Device())
				if failure != nil {
					app.Quit()
					return
				}
			}
			b, _ := json.Marshal(map[string]any{"event": "device", "go": runtime.Version(), "features": p.Device().Features(), "limits": p.Device().Limits(), "format": p.SurfaceFormat()})
			fmt.Println(string(b))
			if *workload {
				w, h := ctx.FramebufferSize()
				if *measurement {
					failure = measure.ValidateDimensions(w, h, *wantW, *wantH)
					if failure != nil {
						app.Quit()
						return
					}
				}
				coldStart := time.Now()
				cached = fixture.Snapshot{}
				authored, failure = fixture.New(*count, *detail, w, h, *mode)
				if failure == nil {
					retained, failure = render.New(p.Device(), p.Queue(), p.SurfaceFormat(), authored.Scene)
				}
				if failure == nil {
					failure = retained.Resize(w, h)
					extentW, extentH = w, h
					coldMS = float64(time.Since(coldStart).Nanoseconds()) / 1e6
					if *qualify && failure == nil {
						capture, failure = host.NewCapture(p.Device(), *count, w, h, p.SurfaceFormat())
					}
				}
			} else {
				smoke, failure = host.NewSmoke(p.Device(), p.Queue(), p.SurfaceFormat())
			}
			if failure != nil {
				fmt.Println("FAIL:", failure)
				app.Quit()
				return
			}
		}
		w, h := ctx.FramebufferSize()
		if w == 0 || h == 0 {
			return
		}
		if retained != nil && (w != extentW || h != extentH) {
			if *measurement {
				failure = fmt.Errorf("physical extent changed during controlled run")
				app.Quit()
				return
			}
			resizePending = [2]int{w, h}
			fence = host.NewFrameFence(app.DeviceProvider().Device())
			fenceStarted = time.Now()
			return
		}
		if capturePending {
			if time.Since(captureStarted) > 5*time.Second {
				failure = fmt.Errorf("diagnostic map completion timeout")
				app.Quit()
				return
			}
			data, ready, e := capture.Poll()
			failure = e
			if ready && failure == nil {
				result, e := capture.Compare(data, authored, fixture.DiagnosticSnapshot(authored), float32(len(qualification))/4)
				failure = e
				result.Resources = retained.Resources()
				if failure == nil && *count == 64 {
					png, e := capture.PNG(data)
					if e == nil {
						e = host.SaveCapture(*out, fmt.Sprintf("count-%d-alpha-%.2f.png", *count, result.Alpha), png)
					}
					failure = e
				}
				qualification = append(qualification, result)
				b, _ := json.Marshal(result)
				fmt.Println("qualification=" + string(b))
				capturePending = false
				if len(qualification) == cases || failure != nil {
					app.Quit()
					return
				}
			}
			if failure != nil {
				app.Quit()
			}
			return
		}
		if retained != nil {
			tick := uint64(draws / 4)
			alpha := float32(draws%4) / 4
			if *qualify {
				tick = 17
				alpha = float32(len(qualification)) / 4
			}
			prepareStart := time.Now()
			if cached.Generation == 0 || authored.Mode == "animated" && cached.Generation != tick+1 {
				cached = authored.Sample(tick)
			}
			sample := cached
			if *qualify {
				sample = fixture.DiagnosticSnapshot(authored)
			}
			prepareMS := float64(time.Since(prepareStart).Nanoseconds()) / 1e6
			camera := authored.Scene.Camera
			if authored.Mode == "camera" {
				camera[0] += .015 * float32(draws%240) / 240
			}
			if serial := inputSerial.Load(); serial != reflectedInput {
				reflectedInput = serial
				inputLog = append(inputLog, map[string]any{"event_serial": serial, "callback_to_encode_start_ms": float64(time.Now().UnixNano()-inputAt.Load()) / 1e6, "submission_ms": nil, "presentation_ms": nil, "reason": "borrowed host encoder has no native submit/presentation timestamp"})
			}
			camera[0] += .001 * float32(reflectedInput)
			if *measurement && draws == measure.Warmup {
				runtime.ReadMemStats(&beforeMemory)
				measuredStarted = true
			}
			encodeStart := time.Now()
			var counters render.Counters
			counters, failure = retained.Encode(ctx.CommandEncoder(), ctx.SurfaceView(), w, h, camera, alpha, sample)
			encodeMS := float64(time.Since(encodeStart).Nanoseconds()) / 1e6
			if *measurement && failure == nil {
				r := retained.Resources()
				observations = append(observations, measure.Frame{Index: draws, Generation: sample.Generation, Prepare: measure.Number(prepareMS), Encode: measure.Number(encodeMS), FencePollCPU: measure.Number(waitMS), CallbackInterval: interval, Draws: counters.Draws, Dispatches: counters.Dispatches, Instances: counters.Instances, Pieces: counters.Pieces, Triangles: counters.Triangles, UploadBytes: counters.UploadBytes, EndpointBytes: counters.EndpointBytes, PersistentCreations: counters.PersistentCreations, LiveResources: r.Count, LiveBytes: r.Bytes})
			}
			if *qualify && failure == nil {
				qualificationCounters = append(qualificationCounters, counters)
				if counters.PersistentCreations != 0 || (len(qualificationCounters) > 1 && counters.EndpointBytes != 0) {
					failure = fmt.Errorf("retention or generation upload contract failed")
					app.Quit()
					return
				}
				if _, e := retained.Encode(ctx.CommandEncoder(), capture.FinalView(), w, h, authored.Scene.Camera, alpha, sample); e != nil {
					failure = e
					app.Quit()
					return
				}
				if e := capture.Encode(ctx.CommandEncoder(), retained); e != nil {
					failure = e
					app.Quit()
					return
				}
				capturePending = true
				captureStarted = time.Now()
			}
		} else {
			failure = smoke.Encode(ctx.CommandEncoder(), ctx.SurfaceView(), draws == 0)
		}
		previousCallback = callback
		host.MarkEncoded(draws, reflectedInput)
		draws++
		cycleDraws++
		if retained != nil && !*qualify && (*cycles > 0 || draws%2 == 0) {
			fence = host.NewFrameFence(app.DeviceProvider().Device())
			fenceStarted = time.Now()
		}
		if smoke != nil && draws > 1 && !readback {
			var ready bool
			ready, failure = smoke.Readback()
			if ready {
				readback = failure == nil
				fmt.Printf("compute_readback=%v error=%v\n", readback, failure)
			}
		}
		if failure != nil || (*frames > 0 && draws >= *frames) {
			app.Quit()
		}
		if smoke != nil && !readback && time.Since(start) > 5*time.Second {
			failure = fmt.Errorf("compute readback timeout")
			app.Quit()
		}
	})
	app.OnClose(func() {
		if *measurement {
			runtime.ReadMemStats(&afterMemory)
		}
		if retained != nil {
			if e := host.Complete(app.DeviceProvider().Device()); e != nil {
				failure = errors.Join(failure, e)
			} else {
				if fence != nil {
					_, e := fence.Poll()
					failure = errors.Join(failure, e)
					fence = nil
				}
				if capture != nil {
					capture.Close()
				}
				retained.Close()
				fmt.Printf("resources_after_close=%+v\n", retained.Resources())
			}
		}
		if smoke != nil {
			if e := host.Complete(app.DeviceProvider().Device()); e != nil {
				failure = errors.Join(failure, e)
			} else {
				smoke.Close()
			}
		}
	})
	if e := app.Run(); e != nil {
		failure = e
	}
	if *measurement && authored != nil {
		reasons := map[string]string{"ring_wait_ms": "fence_poll_cpu_ms measures CPU call duration; elapsed asynchronous ring wait is not independently observed", "cpu_pack_ms": "packing and upload are included in encode; no independent observation", "cpu_submit_ms": "public borrowed context provides no native submit hook; browser raw queue records it separately", "cpu_work_ms": "requires separate packing/submission observations", "gpu_ms": "optional timestamps unavailable at default requested features", "queue_completion_latency_ms": "native public context has no submit timestamp; browser raw observer is separate", "presented_ms": "GoGPU context exposes no actual display timestamp", "input_to_submit_ms": "native unavailable; browser raw first-reflected event is separate"}
		summary := measure.Summary(observations, reasons, *refresh)
		summary["cold_fixture_pipeline_targets_ms"] = coldMS
		summary["go_allocated_bytes"] = nil
		summary["go_allocations"] = nil
		if measuredStarted {
			summary["go_allocated_bytes"] = afterMemory.TotalAlloc - beforeMemory.TotalAlloc
			summary["go_allocations"] = afterMemory.Mallocs - beforeMemory.Mallocs
		}
		summary["run_failure"] = fmt.Sprint(failure)
		summary["allocation_scope"] = "process-wide, from immediately before first measured encode to OnClose before drain; includes measurement collector and framework"
		csv, _ := measure.CSV(observations)
		text, _ := json.Marshal(map[string]any{"summary": summary, "frames_csv": csv, "frames": observations, "hashes": authored.Hashes, "actual_physical_width": extentW, "actual_physical_height": extentH, "process_memory": host.ProcessMemory()})
		failure = errors.Join(failure, host.PublishMeasurement(*out, string(text)))
	}
	b, _ := json.Marshal(map[string]any{"event": "result", "smoke_draws": draws, "smoke_compute_pass": readback && !*workload, "retained_submission_only": *workload && !*qualify && failure == nil, "input": inputLog, "lifecycle": lifecycle, "max_authored_frames_in_flight": 2, "qualification_counters": qualificationCounters, "qualification": qualification, "qualification_pass": *qualify && len(qualification) == cases && failure == nil, "failure": fmt.Sprint(failure)})
	fmt.Println(string(b))
	host.Publish(string(b))
	if failure != nil {
		fmt.Fprintln(os.Stderr, failure)
		os.Exit(1)
	}
}
