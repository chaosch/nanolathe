package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gogpu/gogpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/host"
	"os"
	"runtime"
	"time"
)

func main() {
	runtime.LockOSThread()
	frames := flag.Int("frames", 120, "smoke draws before close; 0 keeps the window open")
	diagnostics := flag.Bool("diagnostics", false, "controlled invalid shader and binding diagnostics")
	flag.Parse()
	app := gogpu.NewApp(gogpu.DefaultConfig().WithTitle("Nanolathe WebGPU qualification").WithSize(640, 480).WithResizable(true).WithContinuousRender(true))
	var smoke *host.Smoke
	var failure error
	draws := 0
	readback := false
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
		if smoke == nil {
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
			smoke, failure = host.NewSmoke(p.Device(), p.Queue(), p.SurfaceFormat())
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
		failure = smoke.Encode(ctx.CommandEncoder(), ctx.SurfaceView(), draws == 0)
		draws++
		if draws > 1 && !readback {
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
		if !readback && time.Since(start) > 5*time.Second {
			failure = fmt.Errorf("compute readback timeout")
			app.Quit()
		}
	})
	app.OnClose(func() {
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
	b, _ := json.Marshal(map[string]any{"event": "result", "smoke_draws": draws, "compute_pass": readback, "failure": fmt.Sprint(failure)})
	fmt.Println(string(b))
	host.Publish(string(b))
	if failure != nil {
		fmt.Fprintln(os.Stderr, failure)
		os.Exit(1)
	}
}
