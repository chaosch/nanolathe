package main

import (
	"fmt"
	"image"
	"math"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/platform/gpurender"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// The Nanolathe screen's live background: a small real battle staged the way
// the film route stages its shots, stepped at the simulation's own 30 Hz and
// presented through a renderer of its own, so every setting previews on the
// installed content's units with the draft value applied
// (docs/DESIGN_INTERFACE_HUD_INPUT.md §3.17). It is presentation only: the
// preview session is never saved, never networked and never seen by the
// window's own battle.

// nlSceneKey names one staged scene. Rules and mutators are part of it
// because they change what the simulation does; everything else is applied
// per frame.
type nlSceneKey struct {
	preset   string
	gameplay gameplay.Mode
	mutators string
	w, h     int
	// paired stages a twin of the scene under twinMutators beside it, for a
	// mutator compare: same map, seed and lead-in, differing only in the
	// factor, stepped and framed in lockstep.
	paired       bool
	twinMutators string
}

// nlPreset is one staged scene and where the camera holds on it. The scenes
// themselves are in nlscreen_scenes.go.
type nlPreset struct {
	scene film.Scene
	// camera returns the view centre relative to the fixture anchor (or to
	// the framed fight, for a tracking scene) and the zoom factor at t
	// seconds into the scene. Every scene's camera is still: a slow pan moves
	// pixel art in uneven one-pixel steps, which reads as wobble.
	camera func(t float64) (x, z, zoom float64)
	// loop restarts the scene after this many seconds, before the armies
	// have finished each other off.
	loop float64
	// track frames the fight rather than the fixture anchor: the camera
	// follows the middle of it through the lead-in and holds that frame once
	// the scene is on screen (nlPreviewInstance.track).
	track bool
	// trackOwn frames only the viewing player's units, for a fogged scene
	// whose sight is where they are; the camera offset then holds the ground
	// their march crosses.
	trackOwn bool
	// circular stages the battle under Circular line of sight instead of the
	// skirmish default True. The True raster's table index stops at the last
	// reachable LOS.TDF table, eight cells or sightdistance 256, which the
	// armour roster already reaches or nearly reaches at ×1, so Sight above
	// ×1 barely moves its edge there. The Circular mask stops at fourteen
	// cells (448), so the Sight card's edge visibly moves with the factor
	// [03 §3.2][03 R-COMP-02 §1].
	circular bool
	// stage composes the scene's own units, features and events about the
	// anchor after the film fixture has placed its armies; it may move the
	// anchor.
	stage func(st *nlStage)
	// topUp tops the viewer's storage up each second, so structures that
	// spend energy to fire and builders that spend both are never starved.
	topUp bool
	// surface renders at this fraction of the screen and scales the picture
	// up, for a closer look than the detail view's own 2x; zero is 1.
	surface float64
	// shot is the second of the scene's clock an --nl-shot capture is taken
	// at, for a scene whose moment matters; zero is the default.
	shot float64
}

// nlEffectArtLimit is the preview's cap on effect art, in authored pixels
// (client.SetEffectArtLimit).
const nlEffectArtLimit = 512

// still is a camera that holds one frame for the whole scene: the view
// centre at (x, z) from the anchor or the framed fight, at zoom.
func still(x, z, zoom float64) func(float64) (float64, float64, float64) {
	return func(float64) (float64, float64, float64) { return x, z, zoom }
}

// nlRender is what one presented frame of the preview shows.
type nlRender struct {
	effects         drawlist.Effects
	glow            bool
	glowStrength    int
	trailStrength   int
	arrival         bool
	placementRanges bool
	classic         bool
	// fps throttles background refresh. The Frame rate card overrides the
	// ordinary 30 FPS budget; zero refreshes on every display frame.
	fps int
	// zoom multiplies the preset's own zoom, for the view-scale preview.
	zoom float64
	// The ground light and blast ring strengths, percentages.
	groundLightStrength int
	blastRingStrength   int
}

// Both pictures are refreshed together. Keeping the parameters beside the
// retained images lets an edit bypass the background's cadence without
// changing the recorder's effect histories (DESIGN_INTERFACE_HUD_INPUT §3.17).
type nlRenderSignature struct {
	primary, alt nlRender
	compare      bool
}

func nlRenderInputs(primary nlRender, alt *nlRender, paired bool) nlRenderSignature {
	sig := nlRenderSignature{primary: primary}
	if paired {
		sig.alt, sig.compare = primary, true
	} else if alt != nil {
		sig.alt, sig.compare = *alt, true
	}
	return sig
}

type nlPreviewInstance struct {
	key         nlSceneKey
	preset      nlPreset
	cl          *client.Client
	b           *battleSession
	advance     func()
	anchorX     int32
	anchorZ     int32
	surfaceW    int
	surfaceH    int
	ticks       int
	acc         float64
	arrivalUnit *frame.UnitView
	opening     bool
	started     time.Time
	effects     drawlist.Effects
	enhanced    bool
	snap        bool // a Classic picture is wanted: hold the camera on a step

	sess   *session.Session
	twin   *nlPreviewInstance // the paired scene, when the key asks for one
	slaved bool               // a twin: takes its camera focus from its pair
	marks  []nlMark           // live units after the last tick, for the demos
	// economy is the viewing player's committed economy row after the last
	// tick, for a card that reads resources off the scene (salvage).
	economy   frame.EconomyView
	firstDraw bool    // the first render has been timed
	focusX    float64 // the framed middle of the fight, world pixels
	focusZ    float64
	focusSeen bool
	// framed is set once the lead-in is over: the focus holds from then on.
	framed bool
	closed bool

	placementDef           *content.UnitDef
	placementX, placementZ int32
	previewLimit           string // authored capabilities missing from this fixture
	// The staging worker's share of the scene's renderer sources: the
	// projected water/shadow mask and the feature rest art, prepared each
	// time the scene takes a renderer (nlPreview.render).
	waterMask *gpurender.PreparedWaterMask
	sprites   []*formats.GAFFrame
}

type nlPreviewResult struct {
	inst *nlPreviewInstance
	err  error
}

// A paused scene keeps its session, client and last pictures. Its renderer
// sources are prepared again on a revisit (nlPreview.render).
type nlCachedPreview struct {
	inst       *nlPreviewInstance
	frame, alt *ebiten.Image
}

// Bound retained battles rather than cards: a paired mutator compare owns two.
// The current scene and the recent-scene cache retain at most three sessions.
const nlPreviewCacheSessions = 3

// nlPreviewRenderers bounds the primary renderers kept by surface size: the
// common size and the one closer-look scene that renders smaller.
const nlPreviewRenderers = 2

type nlPreview struct {
	opts Options
	cs   *contentSet

	cur     *nlPreviewInstance
	want    nlSceneKey
	loading bool
	loadKey nlSceneKey
	// loadStarted is when the scene now arriving was asked for.
	loadStarted        time.Time
	results            chan nlPreviewResult
	lastErr            string
	restarts           int
	cache              []nlCachedPreview // least recently used first; paused, never stepped
	cacheHits          int
	presentNeeded      bool
	cacheResumeStarted time.Time

	// gpus are the renderers every scene draws through, most recently used
	// first (nlPreviewRenderers), and twinGPU the paired scene's own: a
	// renderer's sources belong to one client at a time, and the pair draws
	// in the same frame. A renderer outlives its scenes; a different scene
	// resets its sources and prepares its own (DESIGN_INTERFACE_HUD_INPUT
	// §3.17 "Scene reuse").
	gpus    []*nlGPU
	twinGPU nlGPU
	// pages is the device memory every renderer above draws through: they
	// execute one after another on the UI goroutine, so a renderer made for a
	// new card size or the first compare takes the model pages and recycled
	// source pages the others hold instead of allocating its own
	// (gpurender.SharedPages).
	pages *gpurender.SharedPages

	frame      *ebiten.Image // the last presented picture, world viewport only
	alt        *ebiten.Image // the compare picture, when asked for
	fade       *ebiten.Image // the outgoing scene's last picture
	fadeLeft   float64
	lastDrawn  time.Time
	cadence    nlCadence
	lastRender nlRenderSignature
}

func newNLPreview(opts Options, cs *contentSet) *nlPreview {
	return &nlPreview{opts: opts, cs: cs, results: make(chan nlPreviewResult, 4)}
}

// Ready reports whether a scene is on screen.
func (p *nlPreview) Ready() bool { return p != nil && p.cur != nil }

// Loading reports whether a scene is being staged.
func (p *nlPreview) Loading() bool { return p != nil && p.loading && p.loadKey == p.want }

// request asks for a scene; its previous picture stays while it loads.
func (p *nlPreview) request(key nlSceneKey) {
	p.want = key
	if p.cur != nil && p.cur.key == key {
		return
	}
	if cached, ok := p.takeCached(key); ok {
		started := time.Now()
		p.activate(cached)
		p.cacheHits++
		p.cacheResumeStarted = started
		fmt.Fprintf(os.Stderr, "nanolathe: preview: %s resumed cached scene in %v\n", key.preset, time.Since(started).Round(time.Millisecond))
		return
	}
	if p.loading {
		return // the arrival handler starts the next load if the wish moved on
	}
	p.startLoad(key)
}

func (p *nlPreview) startLoad(key nlSceneKey) {
	if p.loading {
		return // one outstanding result owns the retirement/wait handshake
	}
	p.loading, p.loadKey, p.loadStarted = true, key, time.Now()
	opts, cs := p.opts, p.cs
	go func() {
		inst, err := buildNLPreview(opts, cs, key)
		p.results <- nlPreviewResult{inst: inst, err: err}
	}()
}

// buildNLPreview stages a scene off the game goroutine: session, fixture,
// client and lead-in ticks. Nothing here touches the graphics device.
func buildNLPreview(opts Options, cs *contentSet, key nlSceneKey) (*nlPreviewInstance, error) {
	inst, err := buildNLPreviewScene(opts, cs, key, key.mutators)
	if err != nil || !key.paired {
		return inst, err
	}
	twin, err := buildNLPreviewScene(opts, cs, key, key.twinMutators)
	if err != nil {
		inst.close()
		return nil, err
	}
	// The twin frames the pair's ground from its first visible tick.
	twin.slaved = true
	twin.focusX, twin.focusZ, twin.focusSeen = inst.focusX, inst.focusZ, inst.focusSeen
	twin.applyCamera(0)
	twin.cl.SnapCameraBlend()
	inst.twin = twin
	return inst, nil
}

// buildNLPreviewScene stages one scene of key under the given mutators.
func buildNLPreviewScene(opts Options, cs *contentSet, key nlSceneKey, mutators string) (inst *nlPreviewInstance, err error) {
	defer func() {
		if r := recover(); r != nil {
			inst, err = nil, fmt.Errorf("nanolathe: preview %s: %v", key.preset, r)
		}
	}()
	started := time.Now()
	preset, ok := nlPresets[key.preset]
	if !ok {
		return nil, fmt.Errorf("nanolathe: preview: no scene %q", key.preset)
	}
	scene := preset.scene
	st, composed, err := stageNLSession(opts, cs, preset, key.gameplay, mutators)
	if err != nil {
		return nil, err
	}
	sess, anchorX, anchorZ, events := st.s, st.cx, st.cz, st.events
	fixture := time.Now()
	if !scene.Fog {
		revealFilmScene(sess)
	}
	if !sess.NoShake() {
		sess.ToggleNoShake()
	}
	if scene.Opening {
		filmOpeningMex(sess)
		if !scene.Fog {
			sess.RevealStagedMap()
		}
	}
	surfaceW, surfaceH := key.w+film.ChromeInsetX, key.h+2*film.ChromeInsetY
	var (
		b  *battleSession
		cl *client.Client
	)
	cl, err = client.New(client.Options{
		Buffer: sess.Snapshot,
		Width:  surfaceW,
		Height: surfaceH,
		Step:   func(delta float64) { b.viewerStep(delta, cl) },
	})
	if err != nil {
		return nil, err
	}
	cl.SetAntiAlias(true)
	cl.SetFeatureShadows(true)
	cl.SetShadowOptions(true, true, true)
	// The recorder keeps every effect on so its histories survive a compare;
	// the executor applies the draft's selection (DESIGN_GPU_RENDERER §30).
	cl.SetEffects(drawlist.AllEffects())
	cl.SetGlow(true)
	// Effect art above 512 pixels draws nothing here. Every stock entry is far
	// smaller (the commander's death blast, the largest, is 252 by 227), but a
	// content pack may size an entry for something else: TA: Escalation's
	// explode2 to explode4 are its 760–1140 pixel shield bubbles, which its
	// stock weapons' hits and deaths also name, and a preview battle full of
	// them covered the screen and slowed it to a crawl.
	cl.SetEffectArtLimit(nlEffectArtLimit)
	cl.SetEnhanced(true)
	cl.SetInterpolation(true)
	detailOpts := opts
	detailOpts.Zoom = camera.Zoom(2 * camera.ZoomUnit)
	restoreStart := len(sess.World.FeatureDefs)
	if sess.Features != nil {
		restoreStart = sess.Features.DefinitionRestoreStart()
	}
	models, err := client.NewPreviewModelTextureRegistry(cs.nlPreviewModelAssets(), sess.Catalog, sess.World, restoreStart, st.assetUnitNames(), sess.Meteor.WeaponName)
	if err != nil {
		cl.Close()
		return nil, fmt.Errorf("nanolathe: preview %s: model textures: %w", key.preset, err)
	}
	b, err = composeBattleEntryDetachedWithModels(sess, sess.Catalog, cs, nil, nil, models)
	if err != nil {
		cl.Close()
		return nil, err
	}
	b.preview = true
	b.detail = captureDetailArt(detailOpts, cs, sess.World)
	installBattleClient(cl, b)
	cl.SetEffects(drawlist.AllEffects())
	cl.SetFocused(true)
	cl.ConfigureMessageLines(1, 0)
	// The stand-alone battle clock is the player's preference for a battle,
	// not part of a preview.
	b.clockVisible = false
	b.cam.ViewW, b.cam.ViewH = int32(surfaceW), int32(surfaceH)
	b.cam.JumpToBattleViewCenter(anchorX, anchorZ)
	b.cam.Clamp()
	millis := &shotMillisSource{}
	b.millisSource = millis
	step := uint32(0)
	inst = &nlPreviewInstance{key: key, preset: preset, cl: cl, b: b, anchorX: anchorX, anchorZ: anchorZ,
		surfaceW: surfaceW, surfaceH: surfaceH, effects: drawlist.AllEffects(), enhanced: true,
		sess: sess, previewLimit: st.previewLimit()}
	inst.advance = func() {
		step++
		millis.step = step
		nlScriptTick(inst.preset, events, sess, int(step))
		// Preview choreography is applied only while recording, so stepping
		// cannot hold gameplay or play the real arrival impact cue.
		if inst.arrivalUnit != nil {
			cl.ClearArrival()
		}
		placement := b.battleState().Input
		cl.Step(1.0 / film.SimulationTPS)
		if key.preset == "placement" {
			b.battleState().Input = placement
		}
		cl.ObserveCommittedTick()
		inst.track()
		inst.mark()
	}
	staged := time.Now()
	for i := 0; i < scene.PreTicks; i++ {
		inst.applyCamera(0)
		inst.advance()
		// The lead-in is silent. It runs while the scene stages, often behind
		// the main menu before the screen has opened, so its shots must not
		// reach the speakers; its committed events are discarded rather than
		// left for the first shown tick to play all at once.
		sess.Snapshot.DrainCommittedEvents(nil)
	}
	// The frame is chosen at the end of the lead-in and held from the first
	// visible tick, with no blend in from the lead-in's own camera.
	inst.framed = true
	inst.applyCamera(0)
	cl.SnapCameraBlend()
	if scene.Opening && b.beginArrival(cl) {
		inst.opening = true
	}
	if key.preset == "arrival" {
		// A single host step can dispatch without producing a tick. Publish
		// the ordinary opening frame before binding the commander.
		sess.PublishOpeningFrame()
		if u, ok := arrivalCommander(sess.Snapshot.Current(), sess.Catalog); ok {
			inst.arrivalUnit = &u
		}
	}
	if key.preset == "placement" {
		inst.stagePlacement(st)
	}
	leadInDone := time.Now()
	// The staged battle has one owner until it crosses the result channel.
	// Build the projected water/shadow pixels and gather the feature rest art
	// here rather than in the first UI draw; the renderer uploads them later
	// (DESIGN_GPU_RENDERER §14.8, §26.1).
	inst.waterMask = nlWaterMask(sess.World)
	inst.sprites = cl.BattleSpriteFrames()
	own, foe := 0, 0
	for _, m := range inst.marks {
		if m.own {
			own++
		} else {
			foe++
		}
	}
	fmt.Fprintf(os.Stderr, "nanolathe: preview: %s on %s at %d,%d: %d own and %d enemy units; battle %v, fixture %v, client %v, %d lead-in ticks %v, mask %v\n",
		key.preset, scene.Map, anchorX, anchorZ, own, foe, composed.Sub(started).Round(time.Millisecond),
		fixture.Sub(composed).Round(time.Millisecond), staged.Sub(fixture).Round(time.Millisecond), scene.PreTicks, leadInDone.Sub(staged).Round(time.Millisecond), time.Since(leadInDone).Round(time.Millisecond))
	return inst, nil
}

// stageNLSession composes a preset's battle under the given rules and
// mutators and stages it: the film fixture's armies, then the preset's own
// composition. It reports when the battle was composed, before staging.
func stageNLSession(opts Options, cs *contentSet, preset nlPreset, rules gameplay.Mode, mutators string) (*nlStage, time.Time, error) {
	scene := preset.scene
	opts.Map, opts.Seed = scene.Map, int64(scene.Seed)
	opts.Gameplay, opts.GameplaySet = rules, true
	// A scene shows how a setting looks on the full catalog: unit
	// restrictions change which units exist, not how a scene looks, and its
	// fixture stages units by name (docs/DESIGN_MODS_MUTATORS.md §15.9).
	opts.Restrictions = content.Restrictions{}
	opts.Mutators = content.Mutators{}
	if mutators != "" {
		m, err := content.ParseMutators(parseMutatorPairs(mutators))
		if err == nil {
			opts.Mutators = m
		}
	}
	request, _, err := headlessFreshBattleRequest(opts, cs, newBattleSeedSource(opts))
	if err != nil {
		return nil, time.Time{}, err
	}
	request.value.Catalog, err = cs.nlPreviewCatalog()
	if err != nil {
		return nil, time.Time{}, err
	}
	if request.value.FS == cs.fs {
		request.value.SimArt = cs.nlPreviewSimArt(request.value.Catalog)
	}
	if preset.circular {
		// Zero is a lobby value, so it is set after the defaults
		// [08 "Skirmish configuration"].
		request.value.Skirmish.LOSType = 0
	}
	// Keep the authored catalog available to roster selection while every
	// preview, including the unmutated twin, owns its battle catalog.
	battleRequest := request
	battleRequest.value.Catalog = request.value.Catalog.Clone()
	authoritative, err := composeAuthoritativeBattle(battleRequest)
	if err != nil {
		return nil, time.Time{}, err
	}
	sess := authoritative.Session
	composed := time.Now()
	// The computer player never decides, as the Survival attacker does not
	// (docs/DESIGN_SURVIVAL.md §4.1): its units do only what the scene orders
	// them, and it builds nothing and sends nothing into the frame. Weapon
	// upkeep still runs, so its units still fire at what comes in range.
	for _, mgr := range sess.AI {
		if mgr != nil {
			mgr.Passive = true
		}
	}
	st, err := stageNLPreviewFixture(preset, sess, request.value.Catalog)
	if err != nil {
		return nil, composed, err
	}
	return st, composed, nil
}

// nlWaterMasks keeps the last few water masks staged, by the fingerprint of
// their inputs (gpurender.WaterMaskInputs). A mask costs a few hundred
// milliseconds of staging on the preview maps and depends only on the map's
// static terrain, so a restart, a compare twin or a revisit on the same map
// binds the one already built. Entries are kept unbound, so the cache never
// holds a retired battle's terrain.
var nlWaterMasks struct {
	sync.Mutex
	keys  []uint64
	masks []*gpurender.PreparedWaterMask
}

const nlWaterMaskCache = 4

// nlWaterMask returns t's water mask, built or bound from the cache. The
// staging worker calls it while it exclusively owns t, after the lead-in.
func nlWaterMask(t *world.Terrain) *gpurender.PreparedWaterMask {
	key := gpurender.WaterMaskInputs(t)
	c := &nlWaterMasks
	c.Lock()
	for i, k := range c.keys {
		if k == key {
			m := c.masks[i]
			c.Unlock()
			return m.ForTerrain(t)
		}
	}
	c.Unlock()
	m := gpurender.BuildWaterMask(t)
	c.Lock()
	defer c.Unlock()
	if !slices.Contains(c.keys, key) {
		if len(c.keys) == nlWaterMaskCache {
			c.keys, c.masks = c.keys[1:], c.masks[1:]
		}
		c.keys, c.masks = append(c.keys, key), append(c.masks, m.ForTerrain(nil))
	}
	return m
}

// nlScriptTick is what the preview does before simulation tick step: the
// storage top-up and the scene's own events.
func nlScriptTick(preset nlPreset, events []nlEvent, sess *session.Session, step int) {
	if preset.topUp && step%30 == 1 {
		_ = sess.EnqueueHumanCommand(session.HumanCommand{Kind: session.HumanATM})
	}
	nlRunEvents(events, sess, step)
}

func (inst *nlPreviewInstance) seconds() float64 {
	return float64(inst.ticks) / film.SimulationTPS
}

// track frames the middle of the fight: halfway between the two sides'
// median ground positions, or the viewer's own median for a trackOwn scene.
// It follows through the lead-in, which nobody sees, and holds from the end
// of it, so the camera never pans on screen. The preview's simulation runs
// synchronously on this goroutine, so the units are read between ticks.
func (inst *nlPreviewInstance) track() {
	if !inst.preset.track || inst.sess == nil || inst.slaved || inst.framed {
		return
	}
	var sides [2][2][]int32 // own, enemy; x, z
	for _, u := range inst.sess.Units.Iter() {
		if u == nil || !u.Alive || u.Def == nil || u.Def.CanFly || u.Def.BMCode == 0 {
			continue
		}
		k := 1
		if u.Owner == inst.sess.LocalOwner {
			k = 0
		} else if inst.preset.trackOwn {
			continue
		}
		sides[k][0] = append(sides[k][0], int32(u.X.Int()))
		sides[k][1] = append(sides[k][1], int32(u.Z.Int()))
	}
	var xs, zs []float64
	for _, side := range sides {
		if len(side[0]) < 3 {
			continue
		}
		slices.Sort(side[0])
		slices.Sort(side[1])
		xs = append(xs, float64(side[0][len(side[0])/2]))
		zs = append(zs, float64(side[1][len(side[1])/2]))
	}
	if len(xs) == 0 {
		return
	}
	var x, z float64
	for i := range xs {
		x += xs[i] / float64(len(xs))
		z += zs[i] / float64(len(zs))
	}
	inst.focusX, inst.focusZ, inst.focusSeen = x, z, true
}

// nlMark is one live unit as a demo overlay sees it: world position, unit
// name and whether the viewer owns it.
type nlMark struct {
	x, y, z float64
	def     string
	name    string // the unit's display name
	own     bool
	ground  bool
	// building is the construction fraction still to go, 0 when complete.
	building float32
	// health and maxHealth are the unit's hit points, for a health bar.
	health, maxHealth int32
}

// mark records the live units after a tick, read between ticks like track,
// and the viewing player's economy as the tick committed it.
func (inst *nlPreviewInstance) mark() {
	inst.marks = inst.marks[:0]
	if inst.sess == nil {
		return
	}
	if cur := inst.sess.Snapshot.Current(); cur != nil {
		for _, row := range cur.Economy {
			if row.Player == inst.sess.LocalOwner {
				inst.economy = row
			}
		}
	}
	for _, u := range inst.sess.Units.Iter() {
		if u == nil || !u.Alive || u.Def == nil {
			continue
		}
		inst.marks = append(inst.marks, nlMark{
			x: float64(u.X.Int()), y: float64(u.Y.Int()), z: float64(u.Z.Int()),
			def: u.Def.UnitName, name: u.Def.Name, own: u.Owner == inst.sess.LocalOwner, ground: !u.Def.CanFly && u.Def.BMCode != 0,
			building: u.Remaining, health: u.Health, maxHealth: u.MaxHealth,
		})
	}
}

func (inst *nlPreviewInstance) applyCamera(zoomScale float64) {
	x, z, zoom := inst.preset.camera(inst.seconds() + 1/film.SimulationTPS)
	if zoomScale > 0 {
		zoom *= zoomScale
	}
	if inst.snap {
		// Classic has no free zoom: it draws the native or the 2x step.
		if zoom >= 1.5 {
			zoom = 2
		} else {
			zoom = 1
		}
	}
	cam := inst.b.cam
	zoom = min(max(zoom, camera.ZoomFloor.Float()), camera.ZoomMax.Float())
	// The hero text covers the left of the screen and the cards its foot, so
	// the fight — or the anchor, for a scene that does not track — is framed
	// right of centre and a little high.
	fx, fz := float64(inst.anchorX), float64(inst.anchorZ)
	if inst.focusSeen {
		fx, fz = inst.focusX, inst.focusZ
	}
	if t := inst.sess.World; t != nil {
		// World objects are drawn half their height up the screen, so on
		// high ground the camera frames the ground at the focus, not the
		// sea-level point below it [03 §2.5].
		h := max(t.HeightAt(nlFixed(int32(fx)), nlFixed(int32(fz))), t.SeaLevelWorld())
		fz -= float64(h.Int()) / 2
	}
	x += fx - 0.16*float64(inst.key.w)/zoom
	z += fz + 0.08*float64(inst.key.h)/zoom
	vx := x - float64(cam.ViewW+camera.OriginX)/(2*zoom)
	vz := z - float64(cam.ViewH)/(2*zoom)
	if inst.snap {
		// Classic draws from the camera's whole world pixel, the floor of the
		// view; an Enhanced picture beside it must frame the same pixel, or it
		// sits up to a world pixel — two screen pixels at 2x — up and left of
		// the Classic one. update() also drops the camera blend while snapped.
		vx, vz = math.Floor(vx), math.Floor(vz)
	}
	cam.SetPresentationView(camera.PresentationView{X: vx, Z: vz, Factor: zoom})
	inst.b.zoom.Reset()
}

func (inst *nlPreviewInstance) close() {
	if inst == nil || inst.closed {
		return
	}
	inst.waterMask, inst.sprites = nil, nil
	inst.closed = true
	inst.twin.close()
	inst.b.teardown(inst.cl)
	inst.cl.Close()
}

// update advances the scene clock by dt and steps the simulation on each
// 30 Hz boundary it crosses, at most two ticks per display frame.
func (inst *nlPreviewInstance) update(dt float64, zoomScale float64) (stepped bool) {
	inst.acc += dt * film.SimulationTPS
	n := 0
	for inst.acc >= 1 && n < 2 {
		inst.acc--
		inst.applyCamera(zoomScale)
		inst.advance()
		if inst.snap {
			// Classic shows the tick's own camera; Enhanced would blend from
			// the previous tick's and lag the Classic half by up to a tick of
			// camera motion.
			inst.cl.SnapCameraBlend()
		}
		inst.cl.TickPresentationAudio()
		inst.ticks++
		n++
		stepped = true
	}
	if inst.acc >= 1 {
		inst.acc = 0
	}
	if inst.opening {
		seconds := inst.seconds() + inst.acc/film.SimulationTPS + 0.3
		inst.cl.SetArrivalSeconds(float32(seconds))
		if float32(seconds) >= drawlist.ArrivalCoolingEndSeconds {
			inst.opening = false
		}
	}
	inst.movePlacement()
	return stepped
}

func (c nlCachedPreview) sessions() int {
	if c.inst == nil {
		return 0
	}
	if c.inst.twin != nil {
		return 2
	}
	return 1
}

// close retires a scene. Its renderer sources stay until another scene takes
// the renderer.
func (c nlCachedPreview) close() {
	c.inst.close()
}

func (p *nlPreview) takeCached(key nlSceneKey) (nlCachedPreview, bool) {
	for i, c := range p.cache {
		if c.inst.key == key {
			p.cache = slices.Delete(p.cache, i, i+1)
			return c, true
		}
	}
	return nlCachedPreview{}, false
}

func (p *nlPreview) retain(c nlCachedPreview) {
	if c.inst == nil {
		return
	}
	// A scene near the end of its loop needs a fresh opening, not an exhausted
	// fight from the cache. Never retain two clients for the same scene key.
	if c.inst.seconds() >= c.inst.preset.loop || (p.cur != nil && c.inst.key == p.cur.key) {
		c.close()
		return
	}
	if old, ok := p.takeCached(c.inst.key); ok {
		old.close()
	}
	p.cache = append(p.cache, c)
	p.trimCache()
}

func (p *nlPreview) trimCache() {
	n := (nlCachedPreview{inst: p.cur}).sessions()
	for _, c := range p.cache {
		n += c.sessions()
	}
	for n > nlPreviewCacheSessions && len(p.cache) > 0 {
		old := p.cache[0]
		p.cache = slices.Delete(p.cache, 0, 1)
		n -= old.sessions()
		old.close()
	}
}

func (p *nlPreview) activate(next nlCachedPreview) {
	old := nlCachedPreview{inst: p.cur, frame: p.frame, alt: p.alt}
	if old.frame != nil && old.inst != nil {
		if p.fade == nil || p.fade.Bounds() != old.frame.Bounds() {
			p.fade = ebiten.NewImage(old.frame.Bounds().Dx(), old.frame.Bounds().Dy())
		}
		p.fade.Clear()
		p.fade.DrawImage(old.frame, nil)
		p.fadeLeft = 1
	}
	p.cur = next.inst
	p.frame, p.alt = next.frame, next.alt
	p.cur.started = time.Now()
	p.lastDrawn, p.presentNeeded, p.lastErr = time.Time{}, true, ""
	p.cacheResumeStarted = time.Time{}
	p.retain(old)
	p.trimCache()
}

// renderDue shares one cadence for both pictures (nlCadence), with an
// immediate redraw on an edit or activation. Only the background is throttled;
// the screen's input, controls and cursor still run on every host frame
// (DESIGN_INTERFACE_HUD_INPUT §3.17).
func (p *nlPreview) renderDue(now time.Time, sig nlRenderSignature, stepped bool) bool {
	if p.presentNeeded || p.lastRender != sig || p.frame == nil || (sig.compare && p.alt == nil) {
		return true
	}
	if sig.primary.classic && !stepped && !sig.compare {
		return false // Classic presents once per 30 Hz tick.
	}
	return p.cadence.due(now, p.lastDrawn, sig.primary.fps)
}

// Frame advances the preview and refreshes both pictures when due. Staging
// leaves the old scene paused, including its twin; the display-frame dt is
// discarded rather than accumulated for a catch-up after loading
// (DESIGN_INTERFACE_HUD_INPUT §3.17).
func (p *nlPreview) Frame(dt float64, primary nlRender, alt *nlRender) {
	// Loading, arrival and the fade-in are slow on their own; they measure
	// the refresh but cast no vote on the cadence's back-off.
	p.cadence.observe(time.Now(), p.loading || p.presentNeeded || p.fadeLeft > 0)
	select {
	case res := <-p.results:
		p.loading = false
		if res.err != nil {
			p.lastErr = res.err.Error()
			fmt.Fprintf(os.Stderr, "%v\n", res.err)
		} else if res.inst.key == p.want {
			p.activate(nlCachedPreview{inst: res.inst})
		} else {
			p.retain(nlCachedPreview{inst: res.inst})
		}
		if p.cur == nil || p.cur.key != p.want {
			p.request(p.want)
		}
	default:
	}
	if p.loading {
		return
	}
	p.fadeLeft = max(0, p.fadeLeft-dt/0.6)
	inst := p.cur
	if inst == nil {
		return
	}
	zoom := primary.zoom
	inst.snap = primary.classic || (alt != nil && alt.classic)
	if twin := inst.twin; twin != nil {
		// The twin steps on the same clock and frames the same ground: the
		// pair's own focus, the same lead-in and the same accumulator.
		twin.snap, twin.acc = inst.snap, inst.acc
		twin.focusX, twin.focusZ, twin.focusSeen = inst.focusX, inst.focusZ, inst.focusSeen
		twin.update(dt, zoom)
	}
	stepped := inst.update(dt, zoom)
	if !p.loading && inst.seconds() > inst.preset.loop && inst.key == p.want {
		// Restart before the fight burns out; the new copy fades in over it.
		p.restarts++
		p.startLoad(inst.key)
		return
	}
	sig := nlRenderInputs(primary, alt, inst.twin != nil)
	if !p.renderDue(time.Now(), sig, stepped) {
		return
	}
	drawStart := time.Now()
	gpu := p.primaryGPU(inst)
	// Only a repaint with nothing to prepare measures the steady cost.
	ordinary := !p.presentNeeded && gpu.owner == inst && (inst.twin == nil || p.twinGPU.owner == inst.twin)
	p.render(gpu, inst, primary, &p.frame)
	switch {
	case inst.twin != nil:
		// A mutator compare: the twin under the other factor, drawn the same.
		p.render(&p.twinGPU, inst.twin, primary, &p.alt)
	case alt != nil:
		p.render(gpu, inst, *alt, &p.alt)
	}
	p.presentNeeded, p.lastRender = false, sig
	p.lastDrawn = time.Now()
	if ordinary {
		p.cadence.spent(p.lastDrawn.Sub(drawStart))
	}
	if !p.cacheResumeStarted.IsZero() {
		fmt.Fprintf(os.Stderr, "nanolathe: preview: %s cached frame %v, %v after revisit\n", inst.key.preset,
			p.lastDrawn.Sub(drawStart).Round(time.Millisecond), p.lastDrawn.Sub(p.cacheResumeStarted).Round(time.Millisecond))
		p.cacheResumeStarted = time.Time{}
	}
	if !inst.firstDraw {
		inst.firstDraw = true
		fmt.Fprintf(os.Stderr, "nanolathe: preview: %s first frame %v, %v after it was requested\n", inst.key.preset,
			p.lastDrawn.Sub(drawStart).Round(time.Millisecond), p.lastDrawn.Sub(p.loadStarted).Round(time.Millisecond))
	}
}

// samePalette reports whether two palette installs hold the same tables.
func samePalette(a, b *palette.Tables) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

// nlGPU is one Enhanced renderer and the Classic staging image a scene draws
// through, and the scene whose sources the renderer holds.
type nlGPU struct {
	r       *gpurender.Renderer
	pal     *palette.Tables
	w, h    int
	classic *ebiten.Image
	owner   *nlPreviewInstance
}

// primaryGPU is the renderer for a scene's surface size, most recently used
// first; a third size replaces the least recently used.
func (p *nlPreview) primaryGPU(inst *nlPreviewInstance) *nlGPU {
	for i, g := range p.gpus {
		if g.w == inst.surfaceW && g.h == inst.surfaceH {
			copy(p.gpus[1:i+1], p.gpus[:i])
			p.gpus[0] = g
			return g
		}
	}
	g := &nlGPU{}
	if len(p.gpus) >= nlPreviewRenderers {
		// The evicted renderer's source pages go back to the shared pool for
		// the one about to be made.
		p.gpus[nlPreviewRenderers-1].r.ResetSources()
		p.gpus = p.gpus[:nlPreviewRenderers-1]
	}
	p.gpus = append([]*nlGPU{g}, p.gpus...)
	return g
}

func (p *nlPreview) render(g *nlGPU, inst *nlPreviewInstance, r nlRender, into **ebiten.Image) {
	cl := inst.cl
	if u := inst.arrivalUnit; u != nil {
		cl.ClearArrival()
		if r.arrival {
			cl.StartArrival(*u)
			cl.SetArrivalSeconds(float32(inst.seconds() + inst.acc/film.SimulationTPS))
		}
	}
	inst.b.hostPresentation.PlacementWeaponRanges = boolInt(r.placementRanges)
	w, h := inst.key.w, inst.key.h
	if *into == nil || (*into).Bounds().Dx() != w || (*into).Bounds().Dy() != h {
		*into = ebiten.NewImage(w, h)
	}
	frac := float32(inst.acc)
	cl.SetTickFraction(frac)
	cl.SetCameraFraction(frac)
	if r.classic {
		if inst.enhanced {
			cl.SetEnhanced(false)
			cl.SetInterpolation(false)
			inst.enhanced = false
		}
		rgba := cl.ComposeFrame()
		if g.classic == nil || g.classic.Bounds().Dx() != inst.surfaceW || g.classic.Bounds().Dy() != inst.surfaceH {
			g.classic = ebiten.NewImage(inst.surfaceW, inst.surfaceH)
		}
		g.classic.WritePixels(rgba.Pix)
		(*into).Clear()
		sub := g.classic.SubImage(image.Rect(film.ChromeInsetX, film.ChromeInsetY, film.ChromeInsetX+w, film.ChromeInsetY+h)).(*ebiten.Image)
		(*into).DrawImage(sub, nil)
		return
	}
	if !inst.enhanced {
		cl.SetEnhanced(true)
		cl.SetInterpolation(true)
		inst.enhanced = true
	}
	// The recorder keeps every effect family on so its history (trails,
	// wakes) survives a compare; the executor applies the selection, which is
	// where each family's look is composed (DESIGN_GPU_RENDERER §30).
	list := recordNLPreviewFrame(cl, r.trailStrength)
	if list == nil {
		return
	}
	if pal := cl.PaletteTables(); g.r == nil || !samePalette(g.pal, pal) || g.w != inst.surfaceW || g.h != inst.surfaceH {
		gpu, err := gpurender.NewChecked(pal, inst.surfaceW, inst.surfaceH)
		if err != nil {
			p.lastErr = err.Error()
			return
		}
		// A preview's camera holds still, so it packs only the tiles in view
		// rather than the whole map inside its first frame.
		gpu.SetSparseTerrain(true)
		if p.pages == nil {
			p.pages = gpurender.NewSharedPages()
		}
		gpu.SharePages(p.pages)
		*g = nlGPU{r: gpu, pal: pal, w: inst.surfaceW, h: inst.surfaceH, classic: g.classic}
	}
	if g.owner != inst {
		// Another scene's sources go, and this one's are prepared before its
		// first picture: the staging worker built the water mask, and the
		// feature rest art is placed before the frame rather than inside it.
		// The renderer keeps its surfaces, layers, raster pages and recycled
		// source pages, so a scene change allocates almost nothing.
		if g.owner != nil {
			g.r.ResetSources()
		}
		g.owner = inst
		g.r.PrepareWaterMask(inst.waterMask)
		g.r.PrepareSprites(inst.sprites)
	}
	g.r.SetDisplayPalette(cl.DisplayPalette())
	g.r.SetGlow(r.glow)
	g.r.SetGlowStrength(r.glowStrength)
	g.r.SetGroundLightStrength(r.groundLightStrength)
	g.r.SetBlastRingStrength(r.blastRingStrength)
	g.r.SetGlowFamilies(cl.GlowFamilies())
	g.r.SetEffects(r.effects)
	img := g.r.Execute(list, inst.surfaceW, inst.surfaceH)
	if img == nil {
		return
	}
	(*into).Clear()
	sub := img.SubImage(image.Rect(film.ChromeInsetX, film.ChromeInsetY, film.ChromeInsetX+w, film.ChromeInsetY+h)).(*ebiten.Image)
	// A sub-image draws with its own top-left at the origin.
	(*into).DrawImage(sub, nil)
}

// recordNLPreviewFrame keeps one trail history for both pictures. Setting the
// client's strength to zero retires that history (GPU design §30), so an Off
// comparison hides only this record's marks; the next record rebuilds their
// strengths from the retained world-space marks (interface design §3.17).
func recordNLPreviewFrame(cl *client.Client, trailStrength int) *drawlist.List {
	cl.SetTrailStrength(max(1, trailStrength))
	cl.BeginPresentationFrame()
	list := cl.RecordModernFrame()
	if list != nil && trailStrength <= 0 {
		list.Replay(nlPreviewHideTrails{})
	}
	return list
}

// Replay lends the per-frame mark arena, not the client's retained trail
// history. Other command families are untouched and keep their ordinary order.
type nlPreviewHideTrails struct{}

func (nlPreviewHideTrails) Clear()                   {}
func (nlPreviewHideTrails) Terrain(drawlist.Terrain) {}
func (nlPreviewHideTrails) Sprite(drawlist.Sprite)   {}
func (nlPreviewHideTrails) Glyphs(drawlist.Glyphs)   {}
func (nlPreviewHideTrails) Fill(drawlist.Fill)       {}
func (nlPreviewHideTrails) Line(drawlist.Line)       {}
func (nlPreviewHideTrails) Points(drawlist.Points)   {}
func (nlPreviewHideTrails) Flash(drawlist.Flash)     {}
func (nlPreviewHideTrails) Halo(drawlist.Halo)       {}
func (nlPreviewHideTrails) Model(drawlist.Model)     {}
func (nlPreviewHideTrails) Fog(drawlist.Fog)         {}
func (nlPreviewHideTrails) Surface(drawlist.Surface) {}
func (nlPreviewHideTrails) Cursor(drawlist.Cursor)   {}
func (nlPreviewHideTrails) Expand()                  {}
func (nlPreviewHideTrails) Trails(batch drawlist.Trails) {
	for i := range batch.Marks {
		batch.Marks[i].Strength = 0
	}
}

// Close retires every staged scene. A scene still being staged is closed
// when it arrives, so the goroutine never outlives the screen's resources.
func (p *nlPreview) Close() {
	if p == nil {
		return
	}
	p.cur.close()
	p.cur = nil
	for _, c := range p.cache {
		c.close()
	}
	p.cache = nil
	// Closing the screen keeps no renderer: their surfaces go with the screen.
	for _, g := range p.gpus {
		g.r.ResetSources()
	}
	p.twinGPU.r.ResetSources()
	p.gpus, p.twinGPU, p.pages = nil, nlGPU{}, nil
	if p.loading {
		go func(ch chan nlPreviewResult) {
			if res := <-ch; res.inst != nil {
				res.inst.close()
			}
		}(p.results)
		p.loading = false
	}
}

// stagePlacement arms a real prospective tower at a validated, snapped site.
// The battle's own ghost and shared range overlay draw the preview.
func (inst *nlPreviewInstance) stagePlacement(st *nlStage) {
	def, ok := st.s.Catalog.Unit(st.placementUnitName())
	if !ok || def == nil {
		return
	}
	x, z, y, ok := st.spot(def, st.cx+96, st.cz, 24)
	if !ok {
		return
	}
	b := inst.b
	b.armPlacement(def)
	state := &b.battleState().Input
	state.BuildCellX, state.BuildCellZ = world.PlacementAnchor(nlFixed(x), nlFixed(z), state.BuildFootX, state.BuildFootZ)
	state.BuildSiteH, state.BuildOK = int32(y.Int()), true
	// No physical pointer drives a preview. Keep the battle's admission gate
	// in its viewport and preserve the validated site while the scene steps.
	state.PointerX, state.PointerY = int32(inst.surfaceW/2), int32(inst.surfaceH/2)
	inst.anchorX, inst.anchorZ = x, z
	inst.placementDef, inst.placementX, inst.placementZ = def, x, z
	inst.applyCamera(0)
}

// This is preview choreography. Move the prospective tower on the same
// footprint grid and through the same placement validator used to stage it;
// the ordinary ghost and range overlay read the one resulting site together.
func (inst *nlPreviewInstance) movePlacement() {
	def := inst.placementDef
	if def == nil {
		return
	}
	phase := (inst.seconds() + inst.acc/film.SimulationTPS) * math.Pi / 4
	x := inst.placementX + int32(96*math.Sin(phase))
	z := inst.placementZ + int32(48*math.Sin(2*phase))
	ax, az := world.PlacementAnchor(nlFixed(x), nlFixed(z), def.FootprintX, def.FootprintZ)
	fx, fz := world.PlacementCenter(ax, az, def.FootprintX, def.FootprintZ)
	y, ok := nlPlacement(inst.sess, def, fx, fz)
	if !ok {
		return // hold the last valid preview site across blocked ground
	}
	state := &inst.b.battleState().Input
	state.BuildCellX, state.BuildCellZ = ax, az
	state.BuildSiteH, state.BuildOK = int32(y.Int()), true
}
