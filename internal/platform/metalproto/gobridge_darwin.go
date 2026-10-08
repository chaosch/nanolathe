//go:build darwin

package metalproto

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"slices"
	"strings"
	"time"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// The pure-Go renderer is a port of native/bridge.m and its includes: the
// same shaders, encoding order and resource lifetimes, with Metal called
// through package mtl instead of a compiled dylib. Completion handlers are
// replaced by polling: a ring slot is reused only after waiting for the
// command buffer that last used it, and each drawable's presentation time is
// read once Core Animation sets it.

// gbUniforms is NMUniforms.
type gbUniforms struct {
	camera, rect, viewport, timing, mode, heightRect, fogRect, visual, composition, lightControl [4]float32
}

const gbUniformsSize = int(unsafe.Sizeof(gbUniforms{}))

// Upload census kinds (NMUp*).
const (
	upPoses = iota
	upTables
	upVisuals
	upMaterials
	upFog
	upSprites
	upLights
	upOverlay
	upOverlayTexture
	upSelectors
	upComposition
	upEffects
	upEffectsAtlas
	upGround
	upShadows
	upGroups
	upProjection
	upWater
	upGlow
	upDistortions
	upCPU
	upKinds
)

var uploadNames = [upKinds]string{"poses", "tables", "visuals", "materials", "fog", "sprites", "lights", "overlay", "overlay_texture", "selectors", "composition", "effects", "effects_atlas", "ground", "shadows", "groups", "projection", "water", "glow", "distortions", "cpu"}

// Resident kinds.
const (
	residentWorld = iota
	residentFog
	residentSprites
	residentShadowFlags
	residentProjectionFlags
	residentGround
)

const gbEventCapacity = 8192
const gbSpanPasses = 9

type gbMesh struct {
	faceMesh                                                       *gbFaceMesh
	outlineRowsMesh                                                *gbOutlineRowsMesh
	vertices, indices, instances, edges, visuals, primitiveCenters *mtl.Buffer
	indexCount, instanceCount, edgeCount                           int
}

type gbRing struct {
	resident         [6]int
	groups           *gbGroupFrame
	faceRing         *gbFaceRing
	outlineRowsRing  *gbOutlineRowsRing
	faceReflections  *gbFaceReflectionFrame
	effectRing       *gbEffectsRing
	groundRing       *gbGroundRing
	shadows          *gbShadowFrame
	waterObjectsRing *gbWaterObjectsRing
	glowVertices     *mtl.Buffer
	glowCount        int
	glowEnabled      bool
	worldGain        float32
	glowParams       gbGlowParameters

	poses, interpolatedPoses, instances, sprites, lights, visuals, distortions, selectors *mtl.Buffer
	overlay, overlayUpload, rules, materialOffsets, materialOverrides                     *mtl.Buffer
	retainedLights, subjectLights                                                         *mtl.Buffer
	spans                                                                                 []uint32
	cloaks, compositionDraws                                                              []cloakDraw
	compositionSlots, compositionSelectors, compositionPaint                              *mtl.Buffer
	annotations                                                                           *mtl.Buffer
	annotationRanges                                                                      []uint32
	annotationRangeCount                                                                  int
	slotRanks                                                                             []uint32
	projectilePaint                                                                       []uint32
	directSlotCount                                                                       int
	compositionPageWidth, compositionPageHeight                                           int
	compositionSlotCount, compositionDrawCount, compositionPaintCount                     int
	composed                                                                              bool
	terrainFlags                                                                          uint32
	projections, projectionFlags                                                          *mtl.Buffer
	projectionView                                                                        [4]float32
	spriteCount, groundSpriteCount, distortionCount, overlayCount, cloakCount, airCount   int
	fog, overlayAtlas                                                                     *mtl.Texture
}

// gbInflight is one submitted frame awaiting completion.
type gbInflight struct {
	cb                   mtl.CommandBuffer
	ring                 *gbRing
	rowIndex, generation int
	drawable             mtl.ID
	timing               *gbTimingFrame
}

type gbPresentation struct {
	drawable             mtl.ID
	rowIndex, generation int
	submitted            float64
}

type gbRenderer struct {
	device mtl.Device
	queue  mtl.Queue
	lib    mtl.ID

	uniforms, lastUniforms, pendingUniforms gbUniforms
	livePending, pendingOverlay             bool
	pendingOverlayRect                      [5]int
	pendingRetainedLights                   uint32
	pendingWait, pendingCopy                float64
	water                                   [20]float32
	emissionDead                            bool

	timingRows     []nativeRow
	rowGenerations []uint64
	eventQueue     [gbEventCapacity]meshscene.NativeEvent
	eventCount     int
	eventOverflow  uint64
	mouseHeld      uint32
	pointerX       float32
	pointerY       float32

	rowCount, rowCapacity, poseCount, frameNumber int
	inflight                                      [3]gbInflight
	presentations                                 []gbPresentation
	presentationDrainTimedOut                     bool
	presentationDrainRowCount                     int

	interactive, isPlayable, wasKey, cursorHidden, pointerCaptured bool
	pointerRestore                                                 [2]float64
	pointerWarpX, pointerWarpY                                     float64
	pointerModifiers                                               uint32
	overlayVersion, overlayTextureUploads, overlayTextureBytes     uint64
	runLoopPumps, runLoopSources                                   uint64
	offscreen, effects, visualPasses                               bool
	held, pressed                                                  [128]bool
	pauseToggle                                                    bool
	wheel                                                          float32

	residentGenerations [6][3]uint64
	residentBuffers     [6][3]*mtl.Buffer
	residentLengths     [6][3]int
	worldCounts         [3][4]uint32
	worldPoses          [3]*mtl.Buffer
	worldInstances      [3]*mtl.Buffer
	worldMaterialOffs   [3]*mtl.Buffer
	worldMaterials      [3]*mtl.Buffer
	worldRules          [3]*mtl.Buffer
	fogTextures         [3]*mtl.Texture

	terrainTileState  *gbTerrainTiles
	effectState       *gbEffectsState
	groundState       *gbGroundState
	shadows           *gbShadowState
	waterObjectsState *gbWaterObjectsState
	productionGlow    *gbGlow
	groups            *gbGroupState
	faceState         *gbFaceState
	outlineRowsState  *gbOutlineRowsState
	faceReflections   *gbFaceReflectionState

	directModel, finalPass, distortionFinal, shadowBody, annotation                   mtl.ID
	model, subjectModel, subjectResolve, atlasModel, atlasModelMetadata, atlasOutline mtl.ID
	shadowOutline, atlasCommit, groundLight, clearDepth, terrain, composite, sprite   mtl.ID
	shadow, outline, world, distortion, fog, overlay, overlayMultiply                 mtl.ID
	blur, interpolatePoses, selectLights                                              mtl.ID
	depth, spriteDepth, alwaysDepth                                                   mtl.ID
	meshes                                                                            []*gbMesh
	poses, interpolatedPoses                                                          []*mtl.Buffer
	rings                                                                             [3]*gbRing
	lastRing, pendingRing                                                             *gbRing
	emptyLights, materials, textureFrames                                             *mtl.Buffer
	spriteAtlas, overlayAtlas, fogAtlas, waterMask, modelPalette                      *mtl.Texture
	atlas, terrainImage, color, emission, blurA, blurB, depthImage, output, heights   *mtl.Texture
	shadowMask, worldSnapshot, postColor, emptyFog, emptyMask                         *mtl.Texture
	subjectColor, subjectEmission, subjectDepth                                       *mtl.Texture
	modelMetadata, modelAtlasColor, modelAtlasEmission, modelAtlasDepth, groundField  *mtl.Texture
	layer, window                                                                     mtl.ID
	width, height                                                                     int
	drawableTexture                                                                   mtl.Texture
	lastError                                                                         string

	uploadBytes  [upKinds]uint64
	uploadFrames uint64

	// Environment switches, read once; the dylib reads several per frame.
	ablate                 map[string]bool
	splitWorld, legacyPost bool
	timing                 *gbTiming
	slabCommitActive       bool
	fxShared, fxExternal   mtl.RenderEncoder
	glowDeferResolve       bool
	sel                    gbSelectors
}

// gbSelectors are the selectors the renderer sends outside package mtl's
// typed wrappers, registered once.
type gbSelectors struct {
	presentedTime, nextDrawable, texture, isVisible, contentView, bounds, contentsScale mtl.SEL
}

func (r *gbRenderer) fail(format string, args ...any) bool {
	r.lastError = fmt.Sprintf(format, args...)
	return false
}

// copyIn copies n bytes of Go memory into a shared buffer, counting them.
func (r *gbRenderer) copyIn(kind int, dst, src unsafe.Pointer, n int) {
	if n <= 0 || src == nil {
		return
	}
	r.uploadBytes[kind] += uint64(n)
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}

func copyRaw(dst, src unsafe.Pointer, n int) {
	if n > 0 && src != nil {
		copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
	}
}

func (r *gbRenderer) ablated(family string) bool { return len(r.ablate) > 0 && r.ablate[family] }

// grow returns old when it holds bytes, else a larger shared buffer.
func (r *gbRenderer) grow(old *mtl.Buffer, bytes int) *mtl.Buffer {
	if old != nil && old.Size >= max(bytes, 32) {
		return old
	}
	capacity := 4096
	if old != nil {
		capacity = max(old.Size, 4096)
	}
	for capacity < bytes {
		capacity *= 2
	}
	b := r.device.NewBuffer(capacity, mtl.ResourceShared)
	if b != nil && old != nil {
		mtl.Release(old.ID)
	}
	return b
}
func (r *gbRenderer) growPoseOutput(old *mtl.Buffer, bytes int) *mtl.Buffer {
	if old != nil && old.Size >= max(bytes, 64) {
		return old
	}
	capacity := 4096
	if old != nil {
		capacity = max(old.Size, 4096)
	}
	for capacity < bytes {
		capacity *= 2
	}
	b := r.device.NewBuffer(capacity, mtl.ResourcePrivate)
	if b != nil && old != nil {
		mtl.Release(old.ID)
	}
	return b
}

// target makes a private texture.
func (r *gbRenderer) target(w, h int, format, usage uint) *mtl.Texture {
	return r.device.NewTexture2D(format, w, h, mtl.StorageModePrivate, usage)
}

// replaceTexture swaps a texture field, releasing the old one. Command
// buffers in flight retain what they use.
func replaceTexture(field **mtl.Texture, t *mtl.Texture) {
	if *field != nil && *field != t {
		mtl.Release((*field).ID)
	}
	*field = t
}

func (r *gbRenderer) residentSlot(kind int, generation uint64) (int, bool) {
	g := &r.residentGenerations[kind]
	if generation != 0 {
		for i := 0; i < 3; i++ {
			if g[i] == generation {
				return i, false
			}
		}
	}
	a, b := r.rings[(r.frameNumber+1)%3], r.rings[(r.frameNumber+2)%3]
	for i := 0; i < 3; i++ {
		if i != a.resident[kind] && i != b.resident[kind] {
			g[i] = 0
			return i, true
		}
	}
	return 0, false
}

// residentBuffer returns kind's copy for generation, filling a free copy for
// a new one (zeros when bytes is nil).
func (r *gbRenderer) residentBuffer(ring *gbRing, kind int, generation uint64, bytes unsafe.Pointer, length int, census int) *mtl.Buffer {
	slot, fresh := r.residentSlot(kind, generation)
	ring.resident[kind] = slot
	if !fresh {
		if r.residentLengths[kind][slot] != length {
			r.lastError = "Resident generation reused with a different length"
			return nil
		}
		return r.residentBuffers[kind][slot]
	}
	b := r.grow(r.residentBuffers[kind][slot], max(length, 16))
	r.residentBuffers[kind][slot] = b
	if b == nil {
		return nil
	}
	if bytes != nil && length > 0 {
		r.copyIn(census, b.Ptr, bytes, length)
	} else {
		clear(b.Bytes(max(length, 16)))
	}
	r.residentLengths[kind][slot] = length
	r.residentGenerations[kind][slot] = generation
	return b
}

func newGoRenderer(width, height int, offscreen, vsync, effects, visualPasses bool, capacity int, shader string) (nativeRenderer, error) {
	if err := mtl.Load(); err != nil {
		return nil, fmt.Errorf("metalproto: load system frameworks: %w", err)
	}
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if !mtl.IsMainThread() {
		return nil, fmt.Errorf("metalproto: initialize: Metal window must be initialized on the process main thread")
	}
	r := &gbRenderer{offscreen: offscreen, effects: effects, visualPasses: visualPasses, width: width, height: height}
	r.sel = gbSelectors{presentedTime: mtl.Sel("presentedTime"), nextDrawable: mtl.Sel("nextDrawable"), texture: mtl.Sel("texture"), isVisible: mtl.Sel("isVisible"), contentView: mtl.Sel("contentView"), bounds: mtl.Sel("bounds"), contentsScale: mtl.Sel("contentsScale")}
	r.device = mtl.Device(mtl.CreateSystemDefaultDevice())
	if r.device == 0 {
		return nil, fmt.Errorf("metalproto: initialize: No Metal device")
	}
	r.ablate = map[string]bool{}
	if v := os.Getenv("NANOLATHE_METAL_ABLATE"); v != "" {
		for _, f := range strings.Split(v, ",") {
			r.ablate[f] = true
		}
	}
	r.splitWorld = os.Getenv("NANOLATHE_METAL_SPLIT_WORLD") != ""
	r.legacyPost = os.Getenv("NANOLATHE_METAL_LEGACY_POST") != ""
	r.timing = newGBTiming(r.device)
	mtl.CountCalls = os.Getenv("NANOLATHE_METAL_CALL_COUNT") != ""
	r.queue = mtl.Queue(r.device.NewCommandQueue())
	for i := range r.rings {
		r.rings[i] = &gbRing{}
	}
	r.emptyLights = r.device.NewBuffer(64, mtl.ResourceShared)
	r.interactive = capacity < 0
	r.rowCapacity = capacity
	if capacity < 0 {
		r.rowCapacity = -capacity
	}
	if r.rowCapacity == 0 {
		return nil, fmt.Errorf("metalproto: initialize: Timing capacity must be positive")
	}
	r.timingRows = make([]nativeRow, r.rowCapacity)
	r.rowGenerations = make([]uint64, r.rowCapacity)
	r.uniforms.viewport = [4]float32{float32(width), float32(height), 0, 0}
	lib, msg := r.device.NewLibrary(shader)
	if lib == 0 {
		return nil, fmt.Errorf("metalproto: initialize: %s", msg)
	}
	r.lib = lib
	var err error
	fail := func(e error) (nativeRenderer, error) {
		if r.lastError != "" {
			return nil, fmt.Errorf("metalproto: initialize: %w (%s)", e, r.lastError)
		}
		return nil, fmt.Errorf("metalproto: initialize: %w", e)
	}
	if r.terrainTileState, err = newGBTerrainTiles(r.device); err != nil {
		return fail(err)
	}
	for _, init := range []func() error{r.effectsInitialize, r.groundInitialize, r.outlineRowsInitialize, r.groupsInitialize, r.facesInitialize, r.faceReflectionsInitialize, r.waterObjectsInitialize} {
		if err = init(); err != nil {
			return fail(err)
		}
	}
	if r.shadows, err = r.shadowsCreate(); err != nil {
		return fail(err)
	}
	if r.productionGlow, err = newGBGlow(r.device, r.lib, mtl.PixelFormatRGBA16Float); err != nil {
		return fail(err)
	}
	pipelines := []struct {
		p                *mtl.ID
		vertex, fragment string
		mrt              bool
	}{
		{&r.groundLight, "ground_light_vertex", "ground_light_fragment", false},
		{&r.shadowOutline, "model_vertex", "shadow_outline", true},
		{&r.atlasModelMetadata, "model_vertex", "model_atlas_fragment", true},
		{&r.directModel, "model_vertex", "model_direct_fragment", true},
		{&r.atlasOutline, "model_vertex", "outline_atlas_fragment", true},
		{&r.atlasModel, "model_vertex", "atlas_model", true},
		{&r.shadowBody, "model_vertex", "shadow_body_fragment", true},
		{&r.atlasCommit, "model_commit_vertex", "model_commit_fragment", true},
		{&r.model, "model_vertex", "model_fragment", true},
		{&r.subjectModel, "model_vertex", "subject_model", true},
		{&r.subjectResolve, "screen_vertex", "subject_resolve_fragment", true},
		{&r.clearDepth, "screen_vertex", "clear_depth_fragment", true},
		{&r.terrain, "terrain_vertex", "terrain_fragment", true},
		{&r.composite, "screen_vertex", "composite_fragment", false},
		{&r.shadow, "shadow_vertex", "shadow_fragment", false},
		{&r.outline, "model_vertex", "outline_fragment", true},
		{&r.world, "screen_vertex", "world_fragment", false},
		{&r.distortion, "distortion_vertex", "distortion_fragment", false},
		{&r.fog, "screen_vertex", "fog_fragment", false},
		{&r.finalPass, "screen_vertex", "final_fragment", false},
		{&r.distortionFinal, "distortion_vertex", "distortion_final_fragment", false},
		{&r.overlay, "overlay_vertex", "overlay_fragment", false},
		{&r.overlayMultiply, "overlay_vertex", "overlay_multiply_fragment", false},
		{&r.sprite, "sprite_vertex", "sprite_fragment", true},
		{&r.annotation, "annotation_vertex", "annotation_fragment", true},
	}
	for _, p := range pipelines {
		if *p.p, err = r.pipeline(p.vertex, p.fragment, p.mrt); err != nil {
			return fail(fmt.Errorf("%s/%s: %w", p.vertex, p.fragment, err))
		}
	}
	r.materials = r.device.NewBuffer(16, mtl.ResourceShared)
	r.textureFrames = r.device.NewBuffer(16, mtl.ResourceShared)
	r.heights = r.device.NewTexture2D(mtl.PixelFormatR32Float, 1, 1, mtl.StorageModeShared, mtl.UsageShaderRead)
	zero := float32(0)
	r.heights.Replace(0, 0, 1, 1, unsafe.Pointer(&zero), 4)
	r.emptyFog = r.device.NewTexture2D(mtl.PixelFormatRGBA8Unorm, 1, 1, mtl.StorageModeShared, mtl.UsageShaderRead)
	white := uint32(0xffffffff)
	r.emptyFog.Replace(0, 0, 1, 1, unsafe.Pointer(&white), 4)
	r.emptyMask = r.device.NewTexture2D(mtl.PixelFormatRGBA8Unorm, 1, 1, mtl.StorageModeShared, mtl.UsageShaderRead)
	clearTexel := uint32(0)
	r.emptyMask.Replace(0, 0, 1, 1, unsafe.Pointer(&clearTexel), 4)
	for _, c := range []struct {
		p    *mtl.ID
		name string
	}{{&r.blur, "blur"}, {&r.selectLights, "select_subject_lights"}, {&r.interpolatePoses, "interpolate_poses"}} {
		if *c.p, err = r.computePipeline(c.name); err != nil {
			return fail(err)
		}
	}
	r.depth = r.device.DepthState(mtl.CompareLessEqual, true)
	r.spriteDepth = r.device.DepthState(mtl.CompareLessEqual, false)
	r.alwaysDepth = r.device.DepthState(mtl.CompareAlways, true)
	rt := uint(mtl.UsageRenderTarget | mtl.UsageShaderRead)
	r.groundField = r.target((width+1)/2, (height+1)/2, mtl.PixelFormatRGBA8Unorm, rt)
	r.color = r.target(width, height, mtl.PixelFormatRGBA16Float, rt)
	r.emission = r.target(width, height, mtl.PixelFormatRGBA16Float, rt)
	rw := uint(mtl.UsageShaderRead | mtl.UsageShaderWrite)
	r.blurA = r.target(max(width/2, 1), max(height/2, 1), mtl.PixelFormatRGBA16Float, rw)
	r.blurB = r.target(max(width/2, 1), max(height/2, 1), mtl.PixelFormatRGBA16Float, rw)
	if visualPasses {
		r.shadowMask = r.target(width, height, mtl.PixelFormatR8Unorm, rt)
		r.worldSnapshot = r.target(width, height, mtl.PixelFormatRGBA16Float, rt)
		r.postColor = r.target(width, height, mtl.PixelFormatRGBA16Float, rt)
	} else {
		r.shadowMask = r.emptyFog
	}
	r.depthImage = r.target(width, height, mtl.PixelFormatDepth32Float, rt)
	r.output = r.target(width, height, mtl.PixelFormatBGRA8Unorm, rt)
	if !offscreen {
		if err := r.openWindow(width, height, vsync); err != nil {
			return fail(err)
		}
	}
	return r, nil
}

// computePipeline compiles a named compute function.
func (r *gbRenderer) computePipeline(name string) (mtl.ID, error) {
	fn := mtl.Function(r.lib, name)
	if fn == 0 {
		return 0, fmt.Errorf("missing compute function %s", name)
	}
	p, msg := r.device.ComputePipeline(fn)
	mtl.Release(fn)
	if p == 0 {
		return 0, fmt.Errorf("%s: %s", name, msg)
	}
	return p, nil
}

// renderPipeline compiles a vertex/fragment pair through configure.
func (r *gbRenderer) renderPipeline(vertex, fragment string, configure func(mtl.PipelineDescriptor)) (mtl.ID, error) {
	d := mtl.NewPipelineDescriptor()
	defer d.Release()
	vfn, ffn := mtl.Function(r.lib, vertex), mtl.Function(r.lib, fragment)
	d.SetVertex(vfn)
	d.SetFragment(ffn)
	mtl.Release(vfn)
	mtl.Release(ffn)
	configure(d)
	p, msg := r.device.RenderPipeline(mtl.ID(d))
	if p == 0 {
		return 0, fmt.Errorf("%s/%s: %s", vertex, fragment, msg)
	}
	return p, nil
}

// pipeline is bridge.m's pipeline(): attachment formats and blending chosen
// by shader name.
func (r *gbRenderer) pipeline(vertex, fragment string, mrt bool) (mtl.ID, error) {
	d := mtl.NewPipelineDescriptor()
	defer d.Release()
	var vfn mtl.ID
	if vertex == "model_vertex" {
		var msg string
		vfn, msg = mtl.FunctionBoolConstant(r.lib, vertex, fragment == "model_direct_fragment")
		if vfn == 0 {
			return 0, fmt.Errorf("%s", msg)
		}
	} else {
		vfn = mtl.Function(r.lib, vertex)
	}
	d.SetVertex(vfn)
	mtl.Release(vfn)
	name := fragment
	switch fragment {
	case "shadow_outline":
		name = "outline_fragment"
	case "subject_model", "atlas_model":
		name = "model_fragment"
	}
	ffn := mtl.Function(r.lib, name)
	d.SetFragment(ffn)
	mtl.Release(ffn)
	a0 := d.Attachment(0)
	if mrt || fragment == "world_fragment" || fragment == "distortion_fragment" || fragment == "shadow_fragment" {
		a0.Format(mtl.PixelFormatRGBA16Float)
	} else {
		a0.Format(mtl.PixelFormatBGRA8Unorm)
	}
	if vertex == "overlay_vertex" {
		a0.Blending(true)
		a0.SourceRGB(mtl.BlendOne)
		a0.DestRGB(mtl.BlendOneMinusSourceAlpha)
		a0.SourceAlpha(mtl.BlendOne)
		a0.DestAlpha(mtl.BlendOneMinusSourceAlpha)
	}
	if fragment == "overlay_multiply_fragment" {
		a0.SourceRGB(mtl.BlendZero)
		a0.DestRGB(mtl.BlendSourceColor)
		a0.SourceAlpha(mtl.BlendZero)
		a0.DestAlpha(mtl.BlendOne)
	}
	switch {
	case vertex == "sprite_vertex", vertex == "annotation_vertex", fragment == "model_fragment", fragment == "outline_fragment", fragment == "subject_model", fragment == "subject_resolve_fragment", fragment == "model_commit_fragment", fragment == "model_direct_fragment":
		for i := 0; i < 2; i++ {
			a := d.Attachment(i)
			a.Blending(true)
			a.SourceRGB(mtl.BlendOne)
			if i == 0 || fragment == "subject_model" || fragment == "model_direct_fragment" {
				a.DestRGB(mtl.BlendOneMinusSourceAlpha)
			} else {
				a.DestRGB(mtl.BlendOne)
			}
			a.SourceAlpha(mtl.BlendOne)
			a.DestAlpha(mtl.BlendOneMinusSourceAlpha)
		}
	}
	if fragment == "shadow_fragment" {
		a0.Format(mtl.PixelFormatR8Unorm)
		a0.Blending(true)
		a0.RGBOperation(mtl.BlendOperationMax)
		a0.AlphaOperation(mtl.BlendOperationMax)
		a0.SourceRGB(mtl.BlendOne)
		a0.DestRGB(mtl.BlendOne)
		a0.SourceAlpha(mtl.BlendOne)
		a0.DestAlpha(mtl.BlendOne)
	}
	if fragment == "ground_light_fragment" {
		a0.Format(mtl.PixelFormatRGBA8Unorm)
		a0.Blending(true)
		a0.SourceRGB(mtl.BlendOne)
		a0.DestRGB(mtl.BlendOneMinusSourceColor)
	}
	if fragment == "model_atlas_fragment" || fragment == "outline_atlas_fragment" {
		d.Attachment(2).Format(mtl.PixelFormatRGBA16Float)
	}
	if mrt {
		d.Attachment(1).Format(mtl.PixelFormatRGBA16Float)
		d.SetDepthFormat(mtl.PixelFormatDepth32Float)
	}
	switch fragment {
	case "model_atlas_fragment", "outline_atlas_fragment", "atlas_model", "shadow_outline", "shadow_body_fragment":
		a0.Format(mtl.PixelFormatRGBA8Unorm)
		d.Attachment(1).Format(mtl.PixelFormatRGBA8Unorm)
	}
	if fragment == "shadow_body_fragment" {
		d.Attachment(1).WriteMask(mtl.ColorWriteNone)
	}
	if fragment == "clear_depth_fragment" {
		a0.WriteMask(mtl.ColorWriteNone)
		d.Attachment(1).WriteMask(mtl.ColorWriteNone)
	}
	p, msg := r.device.RenderPipeline(mtl.ID(d))
	if p == 0 {
		return 0, fmt.Errorf("%s", msg)
	}
	return p, nil
}

func (r *gbRenderer) terrainTiles(u *nativeTerrainTilesUpload) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if err := r.terrainTileState.prepare(u); err != nil {
		r.lastError = err.Error()
		return 0
	}
	return 1
}

func (r *gbRenderer) mesh(vertices unsafe.Pointer, vertexCount int32, indices unsafe.Pointer, indexCount int32, offsets unsafe.Pointer, instances int32, edges unsafe.Pointer, edgeCount int32, visuals, centers unsafe.Pointer, primitiveCount int32) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	m := &gbMesh{}
	m.vertices = r.device.NewBuffer(max(int(vertexCount)*64, 64), mtl.ResourceShared)
	m.indices = r.device.NewBuffer(max(int(indexCount)*4, 4), mtl.ResourceShared)
	m.instances = r.device.NewBuffer(max(int(instances)*4, 4), mtl.ResourceShared)
	m.edges = r.device.NewBuffer(max(int(edgeCount)*4, 4), mtl.ResourceShared)
	m.visuals = r.device.NewBuffer(max(int(instances)*64, 64), mtl.ResourceShared)
	m.primitiveCenters = r.device.NewBuffer(max(int(primitiveCount)*16, 16), mtl.ResourceShared)
	if m.vertices == nil || m.indices == nil || m.instances == nil || m.edges == nil || m.visuals == nil || m.primitiveCenters == nil {
		return 0
	}
	copyRaw(m.primitiveCenters.Ptr, centers, int(primitiveCount)*16)
	copyRaw(m.edges.Ptr, edges, int(edgeCount)*4)
	copyRaw(m.visuals.Ptr, visuals, int(instances)*64)
	copyRaw(m.vertices.Ptr, vertices, int(vertexCount)*64)
	copyRaw(m.indices.Ptr, indices, int(indexCount)*4)
	copyRaw(m.instances.Ptr, offsets, int(instances)*4)
	m.edgeCount, m.indexCount, m.instanceCount = int(edgeCount), int(indexCount), int(instances)
	r.meshes = append(r.meshes, m)
	return 1
}

func (r *gbRenderer) assets(a *nativeAssets) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	mats := r.device.NewBuffer(max(int(a.MaterialCount)*16, 16), mtl.ResourceShared)
	frames := r.device.NewBuffer(max(int(a.FrameCount)*16, 16), mtl.ResourceShared)
	if mats == nil || frames == nil {
		return 0
	}
	mtl.Release(r.materials.ID)
	mtl.Release(r.textureFrames.ID)
	r.materials, r.textureFrames = mats, frames
	copyRaw(mats.Ptr, a.Materials, int(a.MaterialCount)*16)
	copyRaw(frames.Ptr, a.Frames, int(a.FrameCount)*16)
	if a.Width != 0 && a.Height != 0 {
		t := r.device.NewTexture2D(mtl.PixelFormatR32Float, int(a.Width), int(a.Height), mtl.StorageModeShared, mtl.UsageShaderRead)
		if t == nil {
			return 0
		}
		t.Replace(0, 0, int(a.Width), int(a.Height), a.Heights, int(a.Width)*4)
		replaceTexture(&r.heights, t)
		r.uniforms.visual[1] = 1
	}
	r.uniforms.heightRect = a.Rect
	return 1
}

func (r *gbRenderer) texture(slot, width, height int32, rgba unsafe.Pointer) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	t := r.device.NewTexture2D(mtl.PixelFormatRGBA8Unorm, int(width), int(height), mtl.StorageModeShared, mtl.UsageShaderRead)
	if t == nil {
		return 0
	}
	t.Replace(0, 0, int(width), int(height), rgba, int(width)*4)
	switch slot {
	case 0:
		replaceTexture(&r.atlas, t)
	case 1:
		replaceTexture(&r.terrainImage, t)
	case 2:
		replaceTexture(&r.spriteAtlas, t)
	case 3:
		replaceTexture(&r.overlayAtlas, t)
	case 4:
		replaceTexture(&r.fogAtlas, t)
	case 5:
		replaceTexture(&r.waterMask, t)
	case 6:
		replaceTexture(&r.modelPalette, t)
	default:
		mtl.Release(t.ID)
		return 0
	}
	return 1
}

func (r *gbRenderer) configure(camera, rect unsafe.Pointer, count int32) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	c := (*[3]float32)(camera)
	r.uniforms.camera = [4]float32{c[0], c[1], c[2], 0}
	r.uniforms.rect = *(*[4]float32)(rect)
	r.poseCount = int(count)
	for i := 0; i < 3; i++ {
		b := r.device.NewBuffer(max(int(count)*128, 128), mtl.ResourceShared)
		out := r.device.NewBuffer(max(int(count)*64, 64), mtl.ResourcePrivate)
		if b == nil || out == nil {
			return 0
		}
		r.poses = append(r.poses, b)
		r.interpolatedPoses = append(r.interpolatedPoses, out)
	}
	return 1
}

// acquire waits for the frame that last used ring slot frameNumber%3 and
// finishes its bookkeeping, as the dylib's semaphore and completion handler do.
func (r *gbRenderer) acquire() {
	if f := &r.inflight[r.frameNumber%3]; f.cb != 0 {
		r.complete(f)
	}
	r.pollPresentations(false)
}

// complete waits for one in-flight frame and records its row.
func (r *gbRenderer) complete(f *gbInflight) {
	if f.cb.Status() < mtl.CommandBufferStatusComplete {
		f.cb.WaitUntilCompleted()
	}
	status := f.cb.Status()
	if r.rowGenerations[f.rowIndex] == uint64(f.generation) {
		row := &r.timingRows[f.rowIndex]
		row.GPUStart = f.cb.GPUStartTime()
		row.GPUEnd = f.cb.GPUEndTime()
		// With no completion handler, the GPU's end time stands in for the
		// moment completion was delivered.
		row.Completed = max(row.GPUEnd, row.Submit)
		row.Status = int32(status)
	}
	if status == mtl.CommandBufferStatusError {
		r.lastError = f.cb.ErrorText()
	}
	if f.ring != nil {
		r.outlineRowsCompleted(f.ring)
	}
	if f.timing != nil {
		r.timing.complete(f.timing)
	}
	if f.drawable != 0 {
		r.presentations = append(r.presentations, gbPresentation{drawable: f.drawable, rowIndex: f.rowIndex, generation: f.generation, submitted: r.timingRows[f.rowIndex].Submit})
	}
	mtl.Release(mtl.ID(f.cb))
	*f = gbInflight{}
}

// pollPresentations records presentation times that have arrived. Unless
// final, a drawable unpresented for 100 ms is let go as never shown.
func (r *gbRenderer) pollPresentations(final bool) {
	if len(r.presentations) == 0 {
		return
	}
	now := mtl.MediaTime()
	kept := r.presentations[:0]
	for _, p := range r.presentations {
		t := mtl.SendDouble(p.drawable, r.sel.presentedTime)
		if t <= 0 && !final && now-p.submitted < .1 {
			kept = append(kept, p)
			continue
		}
		if t > 0 && r.rowGenerations[p.rowIndex] == uint64(p.generation) {
			r.timingRows[p.rowIndex].Presented = t
		}
		mtl.Release(p.drawable)
	}
	r.presentations = kept
}

// beginRow starts the timing row for the next submission.
func (r *gbRenderer) beginRow() (int, int) {
	r.rowCount++
	generation := r.rowCount
	index := (generation - 1) % r.rowCapacity
	r.timingRows[index] = nativeRow{}
	r.rowGenerations[index] = uint64(generation)
	return index, generation
}

// submit commits cb for the frame in ring slot frameNumber%3.
func (r *gbRenderer) submit(cb mtl.CommandBuffer, ring *gbRing, drawable mtl.ID, rowIndex, generation int, timing *gbTimingFrame) {
	if drawable != 0 {
		cb.Present(drawable)
		mtl.Retain(drawable)
	}
	mtl.Retain(mtl.ID(cb))
	r.inflight[r.frameNumber%3] = gbInflight{cb: cb, ring: ring, rowIndex: rowIndex, generation: generation, drawable: drawable, timing: timing}
	cb.Commit()
	r.frameNumber++
}

func (r *gbRenderer) windowVisible() bool {
	return r.offscreen || r.window.Bool(r.sel.isVisible)
}

func (r *gbRenderer) nextDrawable() mtl.ID {
	return mtl.ID(mtl.SendBlocking(r.layer, r.sel.nextDrawable))
}

func (r *gbRenderer) frame(previous, next unsafe.Pointer, alpha float32, seconds float64) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if !r.interactive && r.rowCount >= r.rowCapacity {
		return 0
	}
	r.pump()
	if !r.windowVisible() {
		return -1
	}
	start := mtl.MediaTime()
	r.acquire()
	acquired := mtl.MediaTime()
	var drawable mtl.ID
	if !r.offscreen {
		if drawable = r.nextDrawable(); drawable == 0 {
			return 0
		}
	}
	ready := mtl.MediaTime()
	rowIndex, generation := r.beginRow()
	row := &r.timingRows[rowIndex]
	row.Wait = (acquired - start) * 1000
	row.DrawableWait = (ready - acquired) * 1000
	slot := r.frameNumber % 3
	poses := r.poses[slot]
	copyRaw(poses.Ptr, previous, r.poseCount*64)
	copyRaw(unsafe.Add(poses.Ptr, r.poseCount*64), next, r.poseCount*64)
	u := r.uniforms
	u.timing = [4]float32{alpha, float32(seconds), float32(r.poseCount), b2f(r.effects)}
	r.lastUniforms = u
	cb := r.queue.CommandBuffer()
	if !r.encode(cb, poses, r.interpolatedPoses[slot], u, r.destination(drawable), nil) {
		return 0
	}
	row.Encode = (mtl.MediaTime() - ready) * 1000
	row.Submit = mtl.MediaTime()
	row.Memory = r.device.CurrentAllocatedSize()
	r.submit(cb, nil, drawable, rowIndex, generation, nil)
	return 1
}

// destination is the drawable's texture, or the offscreen output.
func (r *gbRenderer) destination(drawable mtl.ID) *mtl.Texture {
	if drawable == 0 {
		return r.output
	}
	r.drawableTexture = mtl.Texture{ID: mtl.ID(drawable.Get(r.sel.texture)), Width: r.width, Height: r.height, Format: mtl.PixelFormatBGRA8Unorm, Usage: mtl.UsageRenderTarget}
	return &r.drawableTexture
}

func b2f(v bool) float32 {
	if v {
		return 1
	}
	return 0
}

func (r *gbRenderer) drain() {
	for i := range r.inflight {
		// Oldest first: slot frameNumber%3 holds the oldest submission.
		if f := &r.inflight[(r.frameNumber+i)%3]; f.cb != 0 {
			r.complete(f)
		}
	}
	if r.offscreen || r.presentationDrainRowCount == r.rowCount {
		return
	}
	r.presentationDrainRowCount = r.rowCount
	deadline := time.Now().Add(250 * time.Millisecond)
	for len(r.presentations) > 0 {
		kept := r.presentations[:0]
		for _, p := range r.presentations {
			t := mtl.SendDouble(p.drawable, r.sel.presentedTime)
			if t <= 0 {
				kept = append(kept, p)
				continue
			}
			if r.rowGenerations[p.rowIndex] == uint64(p.generation) {
				r.timingRows[p.rowIndex].Presented = t
			}
			mtl.Release(p.drawable)
		}
		r.presentations = kept
		if len(kept) == 0 {
			break
		}
		if time.Now().After(deadline) {
			r.presentationDrainTimedOut = true
			r.pollPresentations(true)
			break
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *gbRenderer) rows(out []nativeRow) int32 {
	// Fold in frames already finished; completion is observed by polling.
	for i := range r.inflight {
		f := &r.inflight[(r.frameNumber+i)%3]
		if f.cb == 0 {
			continue
		}
		if f.cb.Status() < mtl.CommandBufferStatusComplete {
			break
		}
		r.complete(f)
	}
	r.pollPresentations(false)
	n := min(len(out), min(r.rowCount, r.rowCapacity))
	first := r.rowCount - n
	for i := 0; i < n; i++ {
		out[i] = r.timingRows[(first+i)%r.rowCapacity]
	}
	return int32(n)
}

func (r *gbRenderer) capture(path string) int32 {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	r.drain()
	last := (r.frameNumber + 2) % 3
	u := r.lastUniforms
	cb := r.queue.CommandBuffer()
	var poses, out *mtl.Buffer
	if r.lastRing != nil {
		poses, out = r.lastRing.poses, r.lastRing.interpolatedPoses
	} else if len(r.poses) == 3 {
		poses, out = r.poses[last], r.interpolatedPoses[last]
	}
	if !r.encode(cb, poses, out, u, r.output, r.lastRing) {
		return 0
	}
	width, height := r.output.Width, r.output.Height
	bytesPerRow := (width*4 + 255) &^ 255
	read := r.device.NewBuffer(bytesPerRow*height, mtl.ResourceShared)
	if read == nil {
		return 0
	}
	defer mtl.Release(read.ID)
	blit := cb.Blit()
	blit.CopyTextureToBuffer(r.output, width, height, read, 0, bytesPerRow, bytesPerRow*height)
	blit.End()
	cb.Commit()
	cb.WaitUntilCompleted()
	if cb.Status() == mtl.CommandBufferStatusError {
		return 0
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	src := read.Bytes(bytesPerRow * height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a, b := y*bytesPerRow+x*4, y*img.Stride+x*4
			img.Pix[b], img.Pix[b+1], img.Pix[b+2], img.Pix[b+3] = src[a+2], src[a+1], src[a], 255
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return 0
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return 0
	}
	if f.Close() != nil {
		return 0
	}
	return 1
}

func (r *gbRenderer) censusReset() {
	r.uploadBytes = [upKinds]uint64{}
	r.uploadFrames = 0
}

func (r *gbRenderer) info() string {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	r.timing.write()
	size := func(b *mtl.Buffer) int {
		if b == nil {
			return 0
		}
		return b.Size
	}
	dims := func(t *mtl.Texture) (int, int) {
		if t == nil {
			return 0, 0
		}
		return t.Width, t.Height
	}
	poseOutputBytes := 0
	for _, b := range r.interpolatedPoses {
		poseOutputBytes += size(b)
	}
	var poseBytes, instanceBytes, spriteBytes, lightBytes, visualBytes, distortionBytes, fogBytes, selectorBytes, overlayBytes, overlayUploadBytes int
	for _, ring := range r.rings {
		poseOutputBytes += size(ring.interpolatedPoses)
		overlayUploadBytes += size(ring.overlayUpload)
		overlayBytes += size(ring.overlay)
		poseBytes += size(ring.poses)
		instanceBytes += size(ring.instances)
		spriteBytes += size(ring.sprites)
		lightBytes += size(ring.lights)
		visualBytes += size(ring.visuals)
		distortionBytes += size(ring.distortions)
		w, h := dims(ring.fog)
		fogBytes += w * h * 4
		selectorBytes += size(ring.selectors)
	}
	sw, sh := dims(r.subjectColor)
	mw, mh := dims(r.modelAtlasColor)
	bw, bh := dims(r.shadows.body)
	pw, ph := dims(r.postColor)
	maskW, maskH := dims(r.shadowMask)
	hw, hh := dims(r.heights)
	ow, oh := dims(r.overlayAtlas)
	info := map[string]any{
		"native_bridge":                                   "go",
		"cloak_scratch_bytes":                             sw * sh * 20,
		"model_atlas_bytes":                               mw * mh * 20,
		"model_atlas_dimensions":                          []int{mw, mh},
		"shadow_atlas_bytes":                              bw * bh * 13,
		"shadow_atlas_dimensions":                         []int{bw, bh},
		"model_composition":                               "ordered subjects with isolated atlas byte-key depth",
		"pose_interpolation_method":                       "per-piece shortest-path quaternion slerp",
		"pose_interpolation_output_buffer_capacity_bytes": poseOutputBytes,
		"live_overlay_upload_buffer_capacity_bytes":       overlayUploadBytes,
		"run_loop_service_calls":                          r.runLoopPumps,
		"run_loop_handled_sources":                        r.runLoopSources,
		"pointer_captured":                                r.pointerCaptured,
		"overlay_texture_version":                         r.overlayVersion,
		"overlay_texture_uploads":                         r.overlayTextureUploads,
		"overlay_texture_uploaded_bytes":                  r.overlayTextureBytes,
		"interactive":                                     r.interactive,
		"total_frames_submitted":                          r.rowCount,
		"timing_capacity_frames":                          r.rowCapacity,
		"input_queue_capacity":                            gbEventCapacity,
		"input_queue_overflow":                            r.eventOverflow,
		"live_overlay_buffer_capacity_bytes":              overlayBytes,
		"retained_overlay_atlas_bytes":                    ow * oh * 4,
		"live_selector_buffer_capacity_bytes":             selectorBytes,
		"live_visual_buffer_capacity_bytes":               visualBytes,
		"live_distortion_buffer_capacity_bytes":           distortionBytes,
		"live_fog_texture_capacity_bytes":                 fogBytes,
		"visual_passes":                                   r.visualPasses,
		"visual_hdr_target_bytes":                         pw * ph * 8 * 2,
		"visual_target_bytes":                             pw * ph * 17,
		"retained_height_bytes":                           hw * hh * 4,
		"retained_material_bytes":                         size(r.materials) + size(r.textureFrames),
		"live_pose_buffer_capacity_bytes":                 poseBytes,
		"live_instance_buffer_capacity_bytes":             instanceBytes,
		"live_sprite_buffer_capacity_bytes":               spriteBytes,
		"live_light_buffer_capacity_bytes":                lightBytes,
		"device":                                          r.device.Name(),
		"current_allocated_bytes":                         r.device.CurrentAllocatedSize(),
		"recommended_working_set_bytes":                   r.device.RecommendedWorkingSet(),
		"unified_memory":                                  r.device.UnifiedMemory(),
		"max_buffer_length":                               r.device.MaxBufferLength(),
		"native_error":                                    r.lastError,
		"presentation_drain_timed_out":                    r.presentationDrainTimedOut,
		"presentation_drain_timeout_ms":                   250,
		"drawable_width":                                  r.output.Width,
		"drawable_height":                                 r.output.Height,
		"view_width_points":                               0.0,
		"view_height_points":                              0.0,
		"layer_contents_scale":                            0.0,
	}
	info["visual_shadow_mask_bytes"] = 0
	if r.visualPasses {
		info["visual_shadow_mask_bytes"] = maskW * maskH
	}
	if r.window != 0 {
		bounds := mtl.SendRect(mtl.ID(r.window.Get(r.sel.contentView)), r.sel.bounds)
		info["view_width_points"], info["view_height_points"] = bounds[2], bounds[3]
		info["layer_contents_scale"] = mtl.SendDouble(r.layer, r.sel.contentsScale)
	}
	info["screen_maximum_fps"] = mtl.ID(mtl.GetClass("NSScreen").Get(mtl.Sel("mainScreen"))).Get(mtl.Sel("maximumFramesPerSecond"))
	for _, extra := range []map[string]any{r.effectsInfo(), r.groundInfo(), r.waterObjectsInfo(), r.groupsInfo(), r.terrainTileState.info(), r.faceReflectionsInfo(r.lastRing), r.outlineRowsInfo(r.lastRing), r.waterTilesInfo()} {
		for k, v := range extra {
			info[k] = v
		}
	}
	info["stock_effects_bloom_submitted"] = r.lastRing != nil && r.lastRing.glowEnabled
	kinds := map[string]uint64{}
	for i, name := range uploadNames {
		kinds[name] = r.uploadBytes[i]
	}
	info["upload_bytes_by_kind"] = kinds
	info["upload_frames"] = r.uploadFrames
	data, err := json.Marshal(info)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (r *gbRenderer) destroy() {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	if mtl.CountCalls && r.frameNumber > 0 {
		type entry struct {
			name  string
			count int
		}
		var entries []entry
		total := 0
		for sel, n := range mtl.CallCounts {
			entries = append(entries, entry{mtl.SelectorName(sel), n})
			total += n
		}
		slices.SortFunc(entries, func(a, b entry) int { return b.count - a.count })
		fmt.Fprintf(os.Stderr, "metal calls per frame: %.0f over %d frames\n", float64(total)/float64(r.frameNumber), r.frameNumber)
		for _, e := range entries[:min(len(entries), 30)] {
			fmt.Fprintf(os.Stderr, "  %8.1f %s\n", float64(e.count)/float64(r.frameNumber), e.name)
		}
	}
	r.drain()
	r.restorePointer()
	if r.cursorHidden {
		mtl.GetClass("NSCursor").Send(mtl.Sel("unhide"))
		r.cursorHidden = false
	}
	if r.window != 0 {
		r.window.Send(mtl.Sel("orderOut:"), 0)
		r.window.Send(mtl.Sel("close"))
	}
}
