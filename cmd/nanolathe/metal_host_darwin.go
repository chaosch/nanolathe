//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/clock"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/platform/benchlock"
	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/metalhud"
	"github.com/nanolathe-gg/nanolathe/internal/session"
)

// This host changes the window and draw executor only. The ordinary shell owns
// battle composition, input service, HUD, audio and asynchronous simulation.
func runMetalPrototype(opts Options, cs *contentSet) error {
	var width, height int
	if n, _ := fmt.Sscanf(opts.MetalSize, "%dx%d", &width, &height); n != 2 || width < 1280 || height < 960 || width%2 != 0 || height%2 != 0 {
		return fmt.Errorf("nanolathe: metal-size requires even dimensions of at least 1280x960")
	}
	if (opts.MetalQuads != 1 && opts.MetalQuads != 2 && opts.MetalQuads != 5) || (opts.MetalTextures != 1 && opts.MetalTextures != 2) || opts.MetalFrames < 0 {
		return fmt.Errorf("nanolathe: invalid Metal prototype geometry, texture scale or frame limit")
	}
	if err := os.Mkdir(opts.MetalPrototype, 0755); err != nil {
		return err
	}
	lockPath, err := benchlock.Path()
	if err != nil {
		return err
	}
	lock, err := benchlock.Acquire(lockPath, func() { fmt.Fprintln(os.Stderr, "nanolathe: waiting for the other battle benchmark to finish") })
	if err != nil {
		return err
	}
	defer lock.Close()
	if opts.Map == "" && opts.Mission == "" {
		opts.Map = "The Pass"
	}
	opts.Renderer, opts.RendererSet = "modern", true
	opts.Arrival, opts.ArrivalSet = false, true
	shell, cl, err := newDirectBattleView(opts, cs)
	if err != nil {
		return err
	}
	defer cl.Close()
	defer shell.releaseAudio()
	defer shell.teardownBattle(cl)
	shell.settingsWritable = false
	battle := shell.battle
	sess := battle.sess
	sess.PublishOpeningFrame()
	defer func() {
		battle.stopSimulation(cl)
		for _, manager := range sess.AI {
			if manager != nil {
				if closer, ok := manager.Ext.(interface{ Close() }); ok {
					closer.Close()
				}
				manager.Ext = nil
			}
		}
	}()
	logicalW, logicalH := width/2, height/2
	cl.Resize(logicalW, logicalH)
	battle.setSurfaceSize(int32(logicalW), int32(logicalH))
	battle.placeEntryCamera(logicalW, logicalH)
	cl.SetEnhanced(true)
	cl.SetInterpolation(true)
	cl.SetAsyncSimulation(true)
	cl.SetFocused(true)
	// Selected-unit quads are drawn in their units' paint slots, not the HUD.
	cl.SetExternalSelection(true)
	display := cl.DisplayPalette()
	worldGamma := cl.GammaFactor()
	scene, retained, err := meshscene.RetainBattle(meshscene.BattleOptions{DisplayPalette: &display, LoadOptions: meshscene.LoadOptions{Map: opts.Map, MaterialSource: metalMaterialSource(cl), QuadMultiplier: opts.MetalQuads, TextureScale: opts.MetalTextures}, Width: width, Height: height, Zoom: 2}, cs.unmappedMount, battle.sess)
	if err != nil {
		return err
	}
	scene.TerrainTiles, err = meshscene.BuildBattleTerrainTiles(cl.BattleTerrainSources(), cl.DisplayPalette())
	if err != nil {
		return err
	}
	prepareMetalScene(scene, sess.World)
	// Diagnostic: 0 rebuilds the world on every camera change, as before.
	retained.SetCameraReuse(os.Getenv("NANOLATHE_METAL_CAMERA_REUSE") != "0")
	host := &metalBattleHost{worldGamma: worldGamma, fps: metalFPS{target: 1000.0 / 120}, scene: scene, shell: shell, cl: cl, battle: battle, sess: sess, world: retained, hud: metalhud.New(cl.PaletteTables(), logicalW, logicalH), input: newMetalInput(cl, 2), width: logicalW, height: logicalH}
	host.visuals.worldPalette = &display
	scene.Live = host
	if path := os.Getenv("NANOLATHE_METAL_INPUT_TRACE"); path != "" {
		if host.trace.out, err = os.Create(path); err != nil {
			return err
		}
		defer host.trace.out.Close()
	}
	scene.Metadata["logical_surface"] = []int{logicalW, logicalH}
	scene.Metadata["prototype_limits"] = "one battle per launch; fixed drawable; native world effects and annotation depth remain prototype approximations"
	renderOptions := metalproto.Options{Width: width, Height: height, Frames: opts.MetalFrames, Warmup: 120, FPS: 120, VSync: true, Effects: true, VisualPasses: true, OutputDir: opts.MetalPrototype, LibraryPath: opts.MetalLibrary}
	// Every match frame is prepared during the previous frame's pacing, as the
	// production window pre-records (DESIGN_GPU_RENDERER §13.10); the simulation
	// already runs on its own goroutine. NANOLATHE_METAL_PIPELINE=0 restores
	// preparation after pacing.
	if os.Getenv("NANOLATHE_METAL_PIPELINE") != "0" {
		renderOptions.PrepareAhead = func(int) bool { return true }
	}
	// Diagnostics: a windowless run for scripted checks, and a +gamma command
	// issued at a host frame ("frame:value").
	if os.Getenv("NANOLATHE_METAL_OFFSCREEN") == "1" {
		renderOptions.Offscreen, renderOptions.FPS, renderOptions.PrepareAhead = true, 0, nil
	}
	if v := os.Getenv("NANOLATHE_METAL_TEST_GAMMA"); v != "" {
		if _, err := fmt.Sscanf(v, "%d:%d", &host.testGammaFrame, &host.testGamma); err != nil {
			return fmt.Errorf("nanolathe: NANOLATHE_METAL_TEST_GAMMA wants frame:value: %w", err)
		}
	}
	if opts.MetalModelCapture != "" {
		renderOptions.PrepareAhead = nil
		scene.Live, err = newMetalModelCapture(scene, retained, sess, opts.MetalModelCapture, width, height, cs.fs)
		if err != nil {
			return err
		}
		renderOptions.Offscreen, renderOptions.FPS = true, 0
		if renderOptions.Frames == 0 {
			renderOptions.Frames = 120
		}
	}
	report, runErr := metalproto.Run(scene, renderOptions)
	summary := scene.Live.Close()
	if report == nil {
		report = map[string]any{}
	}
	report["production_host"] = summary
	if runErr != nil {
		report["run_error"] = runErr.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(opts.MetalPrototype, "report.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "nanolathe: Metal match artifacts:", opts.MetalPrototype)
	return runErr
}

type metalBattleHost struct {
	visuals                       metalVisuals
	fps                           metalFPS
	selection                     metalSelection
	projection                    metalProjection
	subjects                      metalSubjectRules
	trace                         metalInputTrace
	scene                         *meshscene.Scene
	composer                      *metalModelComposer
	shadows                       *metalShadowComposer
	shell                         *gameShell
	cl                            *client.Client
	battle                        *battleSession
	sess                          *session.Session
	world                         *meshscene.RetainedBattle
	hud                           *metalhud.Foreground
	displayPalette                [256][4]byte
	worldGamma                    float32 // display gamma factor the world art was built with
	atlasEpoch, atlasLocalVersion uint64
	input                         *metalInput
	width, height                 int
	started, due, updated         time.Time
	events                        []meshscene.NativeEvent
	frame                         meshscene.LiveFrame
	cachedTick                    uint32
	cachedFrame, cachedPrevious   *frame.Frame
	cached                        bool
	ditheredFog                   bool
	effects                       drawlist.Effects
	steps, inputs                 uint64
	cursorX, cursorY              int
	cursorObserved                bool
	frames                        int
	testGammaFrame, testGamma     int
}

func (h *metalBattleHost) Next(seconds float64, in meshscene.Input) (meshscene.LiveFrame, error) {
	// A frame prepared ahead steps and samples the camera at its predicted
	// draw start rather than when the worker happens to run.
	now := in.At
	if now.IsZero() {
		now = time.Now()
	}
	if h.started.IsZero() {
		h.started = now
		h.due = now
	}
	h.fps.observe(time.Now(), in.Timing)
	if h.frames++; h.testGammaFrame > 0 && h.frames == h.testGammaFrame {
		h.battle.setGammaCommand(h.testGamma)
	}
	h.events = append(h.events, in.Events...)
	for _, event := range in.Events {
		if event.Kind == 1 || event.Kind == 2 || event.Kind == 3 || event.Kind == 9 {
			h.cursorX = int(event.X / 2)
			h.cursorY = int(event.Y / 2)
			h.cursorObserved = true
		}
	}
	h.inputs += uint64(len(in.Events))
	if !now.Before(h.due) {
		h.cl.BumpPresentationEpoch()
		h.input.Apply(h.events, uint32(clock.ScaledNow(uint32(h.due.Sub(monotonicHostStart)/time.Millisecond))))
		trace := h.trace.before(h.battle, h.cl, h.events)
		h.events = h.events[:0]
		h.cl.SetHostStepDue(h.due)
		h.cl.Step(1.0 / 30)
		h.trace.after(trace, h.battle, h.cl)
		if !h.cl.IsFocused() {
			h.cl.SetPointerCaptured(false)
		}
		h.steps++
		h.updated = now
		h.due = h.due.Add(time.Second / 30)
		// A suspended host resumes at wall time; the existing session budget owns
		// the capped catch-up. Never replay one input batch multiple times.
		if now.Sub(h.due) > 5*time.Second/30 {
			h.due = now.Add(time.Second / 30)
		}
	}
	if h.cl.ExitRequested() || h.shell.battle != h.battle {
		return meshscene.LiveFrame{Exit: true}, nil
	}
	h.cl.SetCameraFraction(float32(now.Sub(h.updated).Seconds() * 30))
	h.cl.PinPresentation()
	h.cl.BeginPresentationFrame()
	fraction := h.cl.ResolveTickFraction()
	view := h.cl.CameraViewFor(h.cl.PresentationDigest())
	camera := [3]float32{float32(view.X + float64(h.width)/(2*view.Factor)), float32(view.Z + float64(h.height)/(2*view.Factor)), float32(view.Factor * 2)}
	previous, cur := h.cl.PresentedFramePair()
	if effects := h.cl.Effects(); !h.cached || effects != h.effects {
		h.world.SetSourceEffects(effects)
		h.effects = effects
		h.cached = false
	}
	if dithered := h.cl.DitheredFog(); dithered != h.ditheredFog {
		h.world.SetDitheredFog(dithered)
		h.ditheredFog = dithered
		h.cached = false
	}
	if cur != nil {
		stale := !h.cached || h.cachedTick != cur.Tick || h.cachedFrame != cur || h.cachedPrevious != previous
		// A camera-only change keeps the build's models while its culling
		// region still covers the view, and reselects only the sprites.
		if !stale && h.frame.Camera != camera {
			if h.world.Covers(camera) {
				h.world.Recull(&h.frame, camera)
			} else {
				stale = true
			}
		}
		if stale {
			sourcePrevious := previous
			_, _, blend := h.cl.PresentedTicks()
			if !blend {
				sourcePrevious = cur
			}
			h.frame = h.world.Frame(sourcePrevious, cur, camera)
			h.cached = true
			h.cachedTick = cur.Tick
			h.cachedFrame = cur
			h.cachedPrevious = previous
		}
	}
	h.frame.Camera = camera
	h.frame.Alpha = float32(fraction) / 65536
	// The world art keeps its load-time palette; a later gamma factor scales
	// the finished world, exactly below the palette's clamp.
	h.frame.WorldGain = 0
	if g := h.cl.GammaFactor(); h.worldGamma > 0 && g != h.worldGamma {
		h.frame.WorldGain = g / h.worldGamma
	}
	display := h.cl.DisplayPalette()
	if display != h.displayPalette {
		var pal palette.Tables
		if source := h.cl.PaletteTables(); source != nil {
			pal = *source
		}
		pal.Base = display
		h.hud = metalhud.New(&pal, h.width, h.height)
		h.displayPalette = display
		h.atlasEpoch += h.atlasLocalVersion + 1
		h.atlasLocalVersion = 0
	}
	if err := h.visuals.Prepare(h.cl, &h.frame, h.width, h.height, 2); err != nil {
		return meshscene.LiveFrame{}, err
	}
	list := h.cl.RecordRetainedForeground()
	if h.cursorObserved {
		h.cl.PositionPresentationCursor(list, h.cursorX, h.cursorY)
	}
	quads, texture, version, err := h.hud.Prepare(list)
	if err != nil {
		return meshscene.LiveFrame{}, err
	}
	// +fps: production draws its panel over the finished frame, cursor
	// included; so does this, as the last HUD quad.
	if h.battle.fpsShown() && h.battle.postBattle == nil && h.battle.hud != nil {
		pixels, revision := h.fps.paint(time.Now(), h.battle.hud.developerFont)
		quads, version = h.hud.Image(metalFPSImageKey, revision, max(0, h.width-metalFPSWidth-6), 6, metalFPSWidth, metalFPSHeight, pixels)
	}
	for i := range quads {
		for j := range quads[i].Rect {
			quads[i].Rect[j] *= 2
		}
	}
	h.frame.CursorQuads = nil
	if h.cursorObserved && !h.cl.PointerCaptured() {
		h.frame.CursorQuads, h.frame.CursorAt, h.frame.CursorScale = h.hud.CursorQuads(), [2]int{h.cursorX, h.cursorY}, 2
	}
	dirty := h.hud.DirtyRect()
	h.frame.Overlay = quads
	h.frame.OverlayTexture = texture
	h.atlasLocalVersion = version
	h.frame.OverlayVersion = h.atlasEpoch + version
	h.frame.PointerCaptured = h.cl.PointerCaptured()
	for i := range dirty {
		h.frame.OverlayDirty[i] = uint32(dirty[i])
	}
	prepareMetalFrame(h.cl, h.world, h.scene, &h.frame, cur, h.width*2, h.height*2, &h.subjects)
	if err := h.projection.Prepare(h.cl, h.world, &h.frame, 2); err != nil {
		return meshscene.LiveFrame{}, err
	}
	if h.composer == nil {
		h.composer = newMetalModelComposer(h.scene)
	}
	composition, err := h.composer.Prepare(h.cl, h.world, &h.frame, cur, h.width*2, h.height*2)
	if err != nil {
		return meshscene.LiveFrame{}, err
	}
	h.frame.Composition, h.frame.Composed = composition, true
	h.frame.Annotations = h.selection.Prepare(h.cl, h.hud, h.composer.order, composition, 2)
	if h.shadows == nil {
		h.shadows = newMetalShadowComposer(h.world)
	}
	h.frame.Shadows, err = h.shadows.Prepare(h.cl, h.world, &h.frame, cur, h.width*2, h.height*2, h.composer.order)
	if err != nil {
		return meshscene.LiveFrame{}, err
	}
	return h.frame, nil
}
func (h *metalBattleHost) Close() map[string]any {
	h.battle.stopSimulation(h.cl)
	result := h.world.Report()
	result["host_steps"] = h.steps
	result["selection_unplaced"] = h.selection.unplaced
	result["world_gamma"] = map[string]float32{"built": h.worldGamma, "final": h.cl.GammaFactor(), "gain": h.frame.WorldGain}
	result["native_events"] = h.inputs
	result["elapsed_seconds"] = time.Since(h.started).Seconds()
	if current := h.sess.Snapshot.Current(); current != nil {
		result["final_tick"] = current.Tick
	}
	result["hud"] = "production retailBattleHUD via retained foreground draw list"
	result["input"] = "production client input and gameShell.step"
	return result
}
