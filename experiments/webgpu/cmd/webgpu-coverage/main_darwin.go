//go:build darwin

// Command webgpu-coverage qualifies the native model coverage resolve using
// authored retained packets, an offscreen HDR target and actual GPU readbacks.
// A window hosts GoGPU; the acquired surface is not a diagnostic source.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/battlegpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/host"
)

type qualificationReport struct {
	Passed          bool                 `json:"passed"`
	Completed       int                  `json:"completed_cases"`
	Expected        int                  `json:"expected_cases"`
	OutputSize      [2]int               `json:"output_size"`
	WorldFormat     string               `json:"world_format"`
	Backend         string               `json:"backend"`
	Adapter         gputypes.AdapterInfo `json:"adapter"`
	HostSurfaceSize [2]int               `json:"host_surface_size"`
	Scope           string               `json:"scope"`
	Cases           []caseComparison     `json:"cases"`
	FrameworkErrors []string             `json:"framework_errors"`
	Failure         string               `json:"failure,omitempty"`
}

func main() {
	runtime.LockOSThread()
	out := flag.String("out", "", "new outside-repository directory for raw native pixels and result.json")
	timeout := flag.Duration("timeout", 30*time.Second, "whole-process watchdog; stalled initialization/completion is a failure")
	flag.Parse()
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "coverage timeout must be positive")
		os.Exit(2)
	}
	directory, err := prepareOutput(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	// WaitIdle is a blocking public call on this pin. A process watchdog bounds
	// it as well as window startup; expiry cannot print a successful result.
	watchdog := time.AfterFunc(*timeout, func() {
		message := "coverage qualification process timeout; completion unverified"
		data, _ := json.Marshal(map[string]any{"passed": false, "failure": message})
		_ = os.WriteFile(filepath.Join(directory, "timeout.json"), data, 0644)
		fmt.Fprintln(os.Stderr, message)
		os.Exit(1)
	})
	defer watchdog.Stop()
	report, runError := runQualification(directory)
	if runError != nil {
		report.Failure = runError.Error()
	}
	data, marshalError := json.MarshalIndent(report, "", "  ")
	if marshalError == nil {
		marshalError = os.WriteFile(filepath.Join(directory, "result.json"), data, 0644)
		if marshalError != nil {
			report.Passed = false
			report.Failure = errors.Join(runError, marshalError).Error()
			data, _ = json.MarshalIndent(report, "", "  ")
		}
		fmt.Println(string(data))
	}
	if err = errors.Join(runError, marshalError); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prepareOutput(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("coverage requires -out NEW_OUTSIDE_REPOSITORY_DIRECTORY")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("coverage output parent must already exist: %w", err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	working, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for root := working; ; root = filepath.Dir(root) {
		if _, e := os.Stat(filepath.Join(root, ".git")); e == nil {
			root, e = filepath.EvalSymlinks(root)
			if e != nil {
				return "", e
			}
			rel, e := filepath.Rel(root, abs)
			if e != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("coverage artifacts must be outside repository %s", root)
			}
			break
		}
		if root == filepath.Dir(root) {
			return "", fmt.Errorf("run coverage from its experiment checkout so output isolation can be verified")
		}
	}
	for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
		if _, e := os.Stat(filepath.Join(ancestor, ".git")); e == nil {
			return "", fmt.Errorf("coverage artifacts must not be inside another checkout: %s", ancestor)
		}
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	if err = os.Mkdir(abs, 0755); err != nil {
		return "", err
	}
	return abs, nil
}

func runQualification(out string) (report qualificationReport, failure error) {
	report = qualificationReport{Expected: len(coverageCases()), OutputSize: [2]int{fixtureWidth, fixtureHeight},
		WorldFormat: "RGBA16Float", Scope: "authored native hardware raster and retained model resolve; no retail span-raster, timing or scanout claim"}
	logErrors := &errorRecords{}
	logger := slog.New(errorHandler{base: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}), errors: logErrors})
	oldDefault, oldGPU := slog.Default(), gogpu.Logger()
	slog.SetDefault(logger)
	gogpu.SetLogger(logger)
	defer func() { gogpu.SetLogger(oldGPU); slog.SetDefault(oldDefault) }()
	app := gogpu.NewApp(gogpu.DefaultConfig().WithTitle("Authored WebGPU model coverage").
		WithSize(64, 64).WithResizable(false).WithContinuousRender(true))
	var d *wgpu.Device
	var models *battlegpu.Models
	var target *fixtureTarget
	var atlasRead, worldRead *textureRead
	var atlasData, worldData []byte
	var slots []slotRecord
	var fence *host.FrameFence
	var submitted time.Time
	cases := coverageCases()
	index := 0
	app.OnDraw(func(ctx *gogpu.Context) {
		if index == len(cases) {
			app.Quit()
			return
		}
		if recorded := logErrors.snapshot(); len(recorded) > 0 {
			failure = errors.Join(failure, fmt.Errorf("native framework error: %s", strings.Join(recorded, "; ")))
		}
		if failure != nil {
			app.Quit()
			return
		}
		if models == nil {
			// Match the pinned probe bootstrap: materialize context encoder and
			// surface before asking DeviceProvider for its initialized device.
			ctx.CommandEncoder()
			ctx.SurfaceView()
			w, h := ctx.FramebufferSize()
			report.HostSurfaceSize = [2]int{w, h}
			provider := app.DeviceProvider()
			d = provider.Device()
			report.Backend = ctx.Backend()
			adapter := wgpu.AdapterFromHandle(app.GPUContextProvider().Adapter())
			if adapter == nil {
				failure = fmt.Errorf("native adapter metadata unavailable")
				app.Quit()
				return
			}
			report.Adapter = adapter.Info()
			if report.Adapter.Backend != gputypes.BackendMetal || report.Adapter.DeviceType == gputypes.DeviceTypeCPU {
				failure = fmt.Errorf("coverage requires actual native Metal hardware; adapter=%+v", report.Adapter)
				app.Quit()
				return
			}
			models, failure = battlegpu.NewModels(d, provider.Queue(), gputypes.TextureFormatRGBA16Float, coverageScene())
			if failure == nil {
				target, failure = newFixtureTarget(d)
			}
			if failure != nil {
				app.Quit()
			}
			return
		}
		if fence != nil {
			if time.Since(submitted) > 5*time.Second {
				failure = fmt.Errorf("%s: frame completion/map timeout", cases[index].Name)
				app.Quit()
				return
			}
			ready, err := fence.Poll()
			if err != nil {
				failure = err
				app.Quit()
				return
			}
			if !ready {
				return
			}
			if atlasData == nil {
				atlasData, _, failure = atlasRead.poll()
			}
			if failure == nil && worldData == nil {
				worldData, _, failure = worldRead.poll()
			}
			if failure != nil {
				app.Quit()
				return
			}
			if atlasData == nil || worldData == nil {
				return
			}
			comparison, compareError := comparePixels(cases[index], slots, atlasData, atlasRead.width, atlasRead.height, atlasRead.row, worldData, worldRead.row)
			report.Cases = append(report.Cases, comparison)
			base := filepath.Join(out, cases[index].Name)
			failure = errors.Join(compareError, os.WriteFile(base+".atlas.rgba8", atlasData, 0644),
				os.WriteFile(base+".world.rgba16f", worldData, 0644))
			data, err := json.Marshal(comparison)
			failure = errors.Join(failure, err)
			if err == nil {
				fmt.Println("comparison=" + string(data))
			}
			atlasRead.close()
			worldRead.close()
			atlasRead, worldRead, atlasData, worldData, fence = nil, nil, nil, nil, nil
			if failure == nil {
				report.Completed++
				index++
			}
			if failure != nil || index == len(cases) {
				app.Quit()
			}
			return
		}
		f := cases[index].frame()
		failure = models.Prepare(f, fixtureWidth, fixtureHeight)
		if failure != nil {
			app.Quit()
			return
		}
		instances, packed := models.DebugPacking()
		slots, failure = readSlots(packed, f)
		base := filepath.Join(out, cases[index].Name)
		failure = errors.Join(failure, os.WriteFile(base+".slots.bin", packed, 0644), os.WriteFile(base+".instances.bin", instances, 0644))
		color, _, _, aw, ah, _ := models.DebugResources()
		if failure == nil {
			atlasRead, failure = newTextureRead(d, aw, ah, 4)
		}
		if failure == nil {
			worldRead, failure = newTextureRead(d, fixtureWidth, fixtureHeight, 8)
		}
		if failure != nil {
			app.Quit()
			return
		}
		enc := ctx.CommandEncoder()
		failure = models.Compute(enc)
		if failure == nil {
			var pass *wgpu.RenderPassEncoder
			pass, failure = enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
				ColorAttachments: []wgpu.RenderPassColorAttachment{{View: target.colorView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}},
				DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: target.depthView,
					DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
			if failure == nil {
				for _, paint := range f.Composition.Paint {
					models.DrawSlot(pass, paint.Slot)
				}
				failure = pass.End()
			}
		}
		if failure != nil {
			app.Quit()
			return
		}
		atlasRead.encode(enc, color, wgpu.TextureUsageTextureBinding)
		worldRead.encode(enc, target.color, wgpu.TextureUsageRenderAttachment)
		// Framework owns Finish/Submit. Poll only in a later callback, and never
		// Prepare or release these sources before this fence and both maps finish.
		fence = host.NewFrameFence(d)
		submitted = time.Now()
	})
	app.OnClose(func() {
		if d == nil {
			return
		}
		if err := host.Complete(d); err != nil {
			failure = errors.Join(failure, err)
			return // completion is unknown; preserve resources through teardown
		}
		atlasRead.close()
		worldRead.close()
		target.close()
		if models != nil {
			models.Close()
		}
	})
	runError := app.Run()
	failure = errors.Join(failure, runError)
	report.FrameworkErrors = logErrors.snapshot()
	if len(report.FrameworkErrors) > 0 {
		failure = errors.Join(failure, fmt.Errorf("framework error records: %s", strings.Join(report.FrameworkErrors, "; ")))
	}
	if failure == nil && report.Completed != report.Expected {
		failure = fmt.Errorf("qualification closed after %d/%d verified cases", report.Completed, report.Expected)
	}
	report.Passed = failure == nil && report.Completed == report.Expected
	return
}
