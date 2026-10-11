package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gogpu/gogpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/host"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/render"
	"os"
	"runtime"
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
	flag.Parse()
	app := gogpu.NewApp(gogpu.DefaultConfig().WithTitle("Nanolathe WebGPU qualification").WithSize(960, 540).WithResizable(true).WithContinuousRender(true))
	var smoke *host.Smoke
	var retained *render.Renderer
	var authored *fixture.Workload
	var capture *host.Capture
	var capturePending bool
	var captureStarted time.Time
	qualification := []host.Comparison{}
	const cases = 5
	var failure error
	draws := 0
	readback := false
	preparedSurface := false
	start := time.Now()
	app.OnDraw(func(ctx *gogpu.Context) {
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
				authored, failure = fixture.New(*count, *detail, w, h, "animated")
				if failure == nil {
					retained, failure = render.New(p.Device(), p.Queue(), p.SurfaceFormat(), authored.Scene)
				}
				if failure == nil {
					failure = retained.Resize(w, h)
					if *qualify && failure == nil {
						capture, failure = host.NewCapture(p.Device(), *count, w, h)
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
			sample := authored.Sample(tick)
			if *qualify {
				sample = fixture.DiagnosticSnapshot(authored)
			}
			_, failure = retained.Encode(ctx.CommandEncoder(), ctx.SurfaceView(), w, h, authored.Scene.Camera, alpha, sample)
			if *qualify && failure == nil {
				capture.Encode(ctx.CommandEncoder(), retained)
				capturePending = true
				captureStarted = time.Now()
			}
		} else {
			failure = smoke.Encode(ctx.CommandEncoder(), ctx.SurfaceView(), draws == 0)
		}
		draws++
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
		if retained != nil {
			if e := host.Complete(app.DeviceProvider().Device()); e != nil {
				failure = e
			} else {
				if capture != nil {
					capture.Close()
				}
				retained.Close()
				fmt.Printf("resources_after_close=%+v\n", retained.Resources())
			}
		}
		if smoke != nil {
			if e := host.Complete(app.DeviceProvider().Device()); e != nil {
				failure = e
			} else {
				smoke.Close()
			}
		}
	})
	if e := app.Run(); e != nil {
		failure = e
	}
	b, _ := json.Marshal(map[string]any{"event": "result", "smoke_draws": draws, "smoke_compute_pass": readback && !*workload, "retained_submission_only": *workload && !*qualify && failure == nil, "qualification": qualification, "qualification_pass": *qualify && len(qualification) == cases && failure == nil, "failure": fmt.Sprint(failure)})
	fmt.Println(string(b))
	host.Publish(string(b))
	if failure != nil {
		fmt.Fprintln(os.Stderr, failure)
		os.Exit(1)
	}
}
