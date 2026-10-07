//go:build darwin

package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/platform/ebitenapp"
	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/metalhud"
)

type metalBenchmarkRow struct {
	Frame                                                          int
	CadenceValid, SimulationStep                                   bool
	TickPhase                                                      int
	Step, Record, Submit, Cadence, PaceWait, OutsideDraw, DrawWork float64
	Source, Packing, CensusMS                                      float64
	Census                                                         any
}

// The benchmark shares fixture, viewer step and census with both production
// executors. Only translation, submission and the platform window differ.
func runMetalBattleBenchmark(opts Options, cs *contentSet, b *battleSession, c *client.Client, step func(), census func() any, options ebitenapp.BenchmarkOptions) error {
	scene, world, err := meshscene.RetainBattle(meshscene.BattleOptions{
		LoadOptions: meshscene.LoadOptions{Map: opts.Map, MaterialSource: metalMaterialSource(c), QuadMultiplier: opts.MetalQuads, TextureScale: opts.MetalTextures},
		Width:       options.Width, Height: options.Height, Zoom: float32(viewZoomOf(b).Float()),
	}, cs.unmappedMount, b.sess)
	if err != nil {
		return err
	}
	prepareMetalScene(scene, b.sess.World)
	scene.Playable = false
	metadata := maps.Clone(options.Metadata)
	metadata["benchmark_version"] = 2
	metadata["simulation_tps"] = 30
	metadata["draws_per_tick"] = options.TPS / 30
	metadata["warmup_draws"] = options.TPS * 2
	metadata["pacing"] = "draw-deadline"
	metadata["gpu_timing_available"] = true
	metadata["present_timing_available"] = true
	metadata["geometry_multiplier"] = opts.MetalQuads
	metadata["texture_multiplier"] = opts.MetalTextures
	metadata["logical_surface"] = []int{options.Width, options.Height}
	metadata["physical_surface"] = []int{options.Width, options.Height}
	metadata["native_scope"] = "prototype retained world with production foreground; native fidelity approximations remain in report.json"
	scene.Metadata["benchmark"] = metadata
	host := &metalBenchmarkSource{b: b, c: c, world: world, scene: scene,
		hud:  metalhud.New(c.PaletteTables(), options.Width, options.Height),
		step: step, census: census, options: options,
		rows: make([]metalBenchmarkRow, 0, options.Frames)}
	scene.Live = host
	report, err := metalproto.Run(scene, metalproto.Options{
		Width: options.Width, Height: options.Height, Frames: options.Frames, Warmup: options.TPS * 2, FPS: options.TPS,
		VSync: true, Effects: true, VisualPasses: true, OutputDir: options.Directory, LibraryPath: opts.MetalLibrary,
		Benchmark: &metalproto.BenchmarkOptions{BeforeMeasure: options.BeforeMeasure, AfterMeasure: options.AfterMeasure,
			AfterFrame: host.afterFrame},
	})
	if err != nil {
		return err
	}
	report["production_benchmark"] = host.Close()
	if err := battleBenchmarkWriteJSON(filepath.Join(options.Directory, "report.json"), report); err != nil {
		return err
	}
	memory, _ := report["benchmark_memory"].(map[string]any)
	if err := battleBenchmarkWriteJSON(filepath.Join(options.Directory, "memory.json"), memory); err != nil {
		return err
	}
	build, _ := debug.ReadBuildInfo()
	common := map[string]any{"tps": options.TPS, "metadata": metadata, "renderer": "metal",
		"go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0),
		"build": build, "revision": os.Getenv("NANOLATHE_BENCH_REVISION"), "rows": host.rows,
		"alloc_bytes": memory["alloc_bytes"], "mallocs": memory["mallocs"], "gc": memory["gc"], "gc_pause_ns": memory["gc_pause_ns"],
		"timing_scope": "host wall work and start-of-work cadence; Submit includes native backpressure; native GPU and presented timestamps live in report.json and frames.csv"}
	if err := battleBenchmarkWriteJSON(filepath.Join(options.Directory, "frames.json"), common); err != nil {
		return err
	}
	// Run's native readback follows all measured work. Preserve its original
	// filename, and offer the production benchmark's common capture name.
	image, err := os.ReadFile(filepath.Join(options.Directory, "final.png"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(options.Directory, "battle.png"), image, 0644)
}

type metalBenchmarkSource struct {
	visuals                  metalVisuals
	b                        *battleSession
	c                        *client.Client
	world                    *meshscene.RetainedBattle
	scene                    *meshscene.Scene
	composer                 *metalModelComposer
	shadows                  *metalShadowComposer
	hud                      *metalhud.Foreground
	step                     func()
	census                   func() any
	options                  ebitenapp.BenchmarkOptions
	draw                     int
	frame                    meshscene.LiveFrame
	previous, current        *frame.Frame
	camera                   [3]float32
	palette                  [256][4]byte
	atlasEpoch, atlasVersion uint64
	row                      metalBenchmarkRow
	rows                     []metalBenchmarkRow
}

func benchmarkElapsed(start time.Time) float64 { return float64(time.Since(start)) / 1e6 }

func (h *metalBenchmarkSource) Next(_ float64, _ meshscene.Input) (meshscene.LiveFrame, error) {
	phase, drawsPerTick := h.draw%(h.options.TPS/30), h.options.TPS/30
	h.row = metalBenchmarkRow{Frame: h.draw, SimulationStep: phase == 0, TickPhase: phase}
	if phase == 0 {
		start := time.Now()
		before := h.b.sess.Clock.GlobalTick
		h.step()
		if after := h.b.sess.Clock.GlobalTick; after != before+1 {
			return meshscene.LiveFrame{}, fmt.Errorf("nanolathe: Metal benchmark simulation stopped advancing: expected tick %d, got %d (state %s)", before+1, after, h.b.sess.State)
		}
		h.c.BumpPresentationEpoch()
		h.row.Step = benchmarkElapsed(start)
	}
	h.c.SetTickFraction(float32(phase) / float32(drawsPerTick))
	start := time.Now()
	h.c.BeginPresentationFrame()
	fraction := h.c.ResolveTickFraction()
	view := h.c.CameraViewFor(h.c.PresentationDigest())
	camera := [3]float32{float32(view.X + float64(h.options.Width)/(2*view.Factor)), float32(view.Z + float64(h.options.Height)/(2*view.Factor)), float32(view.Factor)}
	previous, current := h.c.PresentedFramePair()
	// Settings are host inputs. Apply them on each completed source sample;
	// this does not create an independent simulation or presentation clock.
	h.world.SetSourceEffects(h.c.Effects())
	h.world.SetDitheredFog(h.c.DitheredFog())
	if h.current != current || h.previous != previous || h.camera != camera || current != nil && h.frame.Tick != current.Tick {
		sourcePrevious := previous
		_, _, blend := h.c.PresentedTicks()
		if !blend {
			sourcePrevious = current
		}
		h.frame = h.world.Frame(sourcePrevious, current, camera)
		h.current, h.previous, h.camera = current, previous, camera
	}
	h.frame.Alpha = float32(fraction) / 65536
	display := h.c.DisplayPalette()
	if display != h.palette {
		var pal palette.Tables
		if source := h.c.PaletteTables(); source != nil {
			pal = *source
		}
		pal.Base = display
		h.hud = metalhud.New(&pal, h.options.Width, h.options.Height)
		h.palette = display
		h.atlasEpoch += h.atlasVersion + 1
		h.atlasVersion = 0
	}
	if err := h.visuals.Prepare(h.c, &h.frame, h.options.Width, h.options.Height, 1); err != nil {
		return meshscene.LiveFrame{}, err
	}
	quads, texture, version, err := h.hud.Prepare(h.c.RecordRetainedForeground())
	if err != nil {
		return meshscene.LiveFrame{}, err
	}
	h.frame.Overlay, h.frame.OverlayTexture, h.frame.OverlayVersion = quads, texture, h.atlasEpoch+version
	h.atlasVersion = version
	for i, value := range h.hud.DirtyRect() {
		h.frame.OverlayDirty[i] = uint32(value)
	}
	prepareMetalFrame(h.c, h.world, h.scene, &h.frame, current, h.options.Width, h.options.Height)
	if h.composer == nil {
		h.composer = newMetalModelComposer(h.scene)
	}
	composition, err := h.composer.Prepare(h.c, h.world, &h.frame, current, h.options.Width, h.options.Height)
	if err != nil {
		return meshscene.LiveFrame{}, err
	}
	h.frame.Composition, h.frame.Composed = composition, true
	if h.shadows == nil {
		h.shadows = newMetalShadowComposer(h.world)
	}
	h.frame.Shadows, err = h.shadows.Prepare(h.c, h.world, &h.frame, current, h.options.Width, h.options.Height, h.composer.order)
	if err != nil {
		return meshscene.LiveFrame{}, err
	}
	h.row.Record = benchmarkElapsed(start)
	if h.draw >= h.options.TPS*2 {
		start := time.Now()
		h.row.Census = h.census()
		h.row.CensusMS = benchmarkElapsed(start)
	}
	h.draw++
	return h.frame, nil
}

func (h *metalBenchmarkSource) afterFrame(t metalproto.BenchmarkFrame) {
	row := h.row
	row.CadenceValid, row.Cadence = t.CadenceValid, t.Cadence
	row.PaceWait, row.OutsideDraw, row.DrawWork = t.PaceWait, t.OutsideDraw, t.DrawWork
	row.Record += t.Packing
	row.Submit, row.Source, row.Packing = t.Submit, t.Source, t.Packing
	h.rows = append(h.rows, row)
}

func (h *metalBenchmarkSource) Close() map[string]any {
	return map[string]any{"final_tick": h.b.sess.Clock.GlobalTick, "draws": h.draw, "world": h.world.Report(),
		"host": "production viewerStep with synchronous full simulation and fixed draws per tick", "input": "ignored"}
}
