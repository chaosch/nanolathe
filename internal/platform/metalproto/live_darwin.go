//go:build darwin

package metalproto

import (
	"fmt"
	"math"
	"slices"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

type nativeInput struct {
	PanX, PanZ, Zoom float32
	PauseToggle      uint32
}
type meshSpan struct{ Start, Count uint32 }

const nativeSpanPasses = 9 // all, shadows, reserved, three body phases, three outlines
// Native CPU draw records refer to the mesh-packed instance table.
type cloakDraw struct{ Mesh, Instance, Group, Phase uint32 }
type cloakSubject struct {
	id    uint64
	depth float32
	phase uint32
}
type cloakMember struct {
	draw    cloakDraw
	subject uint64
	order   uint32
}

// Same scalar/pointer layout as NMLiveUpload. Slices are copied synchronously;
// the native ring never retains a pointer into Go memory after the call.
type nativeAssets struct {
	Materials, Frames, Heights               uintptr
	MaterialCount, FrameCount, Width, Height uint32
	Rect                                     [4]float32
}
type liveUpload struct {
	Previous, Current, Offsets, Spans, Sprites, Lights, Visuals, Distortions, Fog, Selectors, Overlay                uintptr
	Camera, FogRect                                                                                                  [4]float32
	Alpha                                                                                                            float32
	PoseCount, InstanceCount, SpriteCount, LightCount, DistortionCount, FogWidth, FogHeight, OverlayCount            uint32
	Seconds                                                                                                          float64
	OverlayTexture                                                                                                   uintptr
	OverlayVersion                                                                                                   uint64
	OverlayWidth, OverlayHeight                                                                                      uint32
	OverlayDirty                                                                                                     [4]uint32
	GroundSpriteCount                                                                                                uint32
	Cloaks                                                                                                           uintptr
	CloakCount, AirCount                                                                                             uint32
	Water                                                                                                            meshscene.WaterUniform
	Rules, MaterialOffsets, Materials                                                                                uintptr
	RulesCount, MaterialCount                                                                                        uint32
	CompositionSlots, CompositionSelectors, CompositionDraws, CompositionPaint                                       uintptr
	CompositionSlotCount, CompositionDrawCount, CompositionPaintCount, CompositionWidth, CompositionHeight, Composed uint32
	RetainedLights                                                                                                   uintptr
	RetainedLightCount, LightControls                                                                                uint32
	Effects                                                                                                          uintptr
	GlowVertices                                                                                                     uintptr
	GlowCount, GlowEnabled                                                                                           uint32
	GlowParams                                                                                                       meshscene.RetainedGlowParameters
	GroundEffects                                                                                                    uintptr
	Shadows                                                                                                          nativeShadowUpload
	WaterObjects                                                                                                     uintptr
	WaterSources                                                                                                     uintptr
}
type livePacking struct {
	waterPacking                                     waterObjectsPacking
	waterUpload                                      nativeWaterObjectsUpload
	waterSourcesUpload                               nativeWaterSourcesUpload
	waterObjects                                     []meshscene.WaterObject
	seabed                                           []uint32
	groundPacking                                    groundEffectsPacking
	groundUpload                                     nativeGroundUpload
	shadowPacking                                    ShadowPacking
	stockPacking                                     effectsPacking
	stockUpload                                      nativeEffectsUpload
	compositionSelectors, sourcePacked, spritePacked []uint32
	compositionDraws                                 []cloakDraw
	compositionPaint                                 [][4]uint32
	rules                                            []meshscene.ModelRules
	materialOffsets                                  []uint32
	materials                                        []meshscene.NativeModelMaterial
	phases                                           []uint32
	cloaks                                           []cloakDraw
	cloakSubjects                                    []cloakSubject
	cloakMembers                                     []cloakMember
	cloakIndices                                     map[uint64]int
	spriteOrder                                      []orderedSprite
	sprites                                          []meshscene.Sprite
	spans                                            []meshSpan
	cursor, offsets                                  []uint32
	visuals                                          []meshscene.ModelVisual
	selectors                                        []uint32
	overlayVersion                                   uint64
	overlayWidth, overlayHeight                      int
}

func (p *livePacking) prepare(scene *meshscene.Scene, f meshscene.LiveFrame, seconds float64, visualPasses, effects bool) (liveUpload, liveRow, error) {
	var u liveUpload
	var r liveRow
	if !visualPasses || !effects {
		f.Distortions = nil
	}
	if !visualPasses {
		f.Fog = meshscene.FogFrame{}
	}
	if unsafe.Sizeof(u) != 632 || unsafe.Sizeof(cloakDraw{}) != 16 || unsafe.Sizeof(meshscene.OverlayQuad{}) != 64 || unsafe.Sizeof(meshscene.NativeEvent{}) != 32 || unsafe.Sizeof(meshscene.Distortion{}) != 64 || unsafe.Sizeof(meshscene.Sprite{}) != 96 || unsafe.Sizeof(meshscene.Light{}) != 32 || unsafe.Sizeof(nativeInput{}) != 16 {
		return u, r, fmt.Errorf("metalproto: live GPU ABI mismatch")
	}
	if len(f.ModelRules) != 0 && len(f.ModelRules) != len(f.Instances) {
		return u, r, fmt.Errorf("metalproto: model rule count differs from instances")
	}
	if len(f.Previous) != len(f.Current) || len(f.Current) > math.MaxInt32 || len(f.Instances) > math.MaxInt32 || len(f.Sprites) > math.MaxInt32 || len(f.Overlay) > math.MaxInt32 || f.Alpha < 0 || f.Alpha > 1 || math.IsNaN(float64(f.Alpha)) {
		return u, r, fmt.Errorf("metalproto: invalid live frame topology or alpha")
	}
	clear(p.spans)
	clear(p.cursor)
	for _, instance := range f.Instances {
		if instance.Mesh < 0 || instance.Mesh >= len(scene.Meshes) || instance.PoseOffset < 0 || instance.PoseOffset > len(f.Current) || scene.Meshes[instance.Mesh].Pieces > len(f.Current)-instance.PoseOffset {
			return u, r, fmt.Errorf("metalproto: live instance has invalid mesh or pose span")
		}
		p.spans[instance.Mesh].Count++
	}
	var start uint32
	for i := range p.cursor {
		p.spans[i].Start = start
		p.cursor[i] = start
		start += p.spans[i].Count
	}
	if cap(p.offsets) < len(f.Instances) {
		p.offsets = make([]uint32, len(f.Instances))
		p.visuals = make([]meshscene.ModelVisual, len(f.Instances))
	} else {
		p.offsets = p.offsets[:len(f.Instances)]
		p.visuals = p.visuals[:len(f.Instances)]
	}
	p.phases = slices.Grow(p.phases[:0], len(f.Instances))[:len(f.Instances)]
	p.rules = slices.Grow(p.rules[:0], len(f.Instances))[:len(f.Instances)]
	p.materialOffsets = slices.Grow(p.materialOffsets[:0], len(f.Instances))[:len(f.Instances)]
	clear(p.rules)
	clear(p.materialOffsets)
	p.materials = p.materials[:0]
	p.sourcePacked = slices.Grow(p.sourcePacked[:0], len(f.Instances))[:len(f.Instances)]
	for sourceIndex, instance := range f.Instances {
		at := p.cursor[instance.Mesh]
		p.sourcePacked[sourceIndex] = at
		if sourceIndex < len(f.ModelRules) {
			p.rules[at] = f.ModelRules[sourceIndex]
		}
		if len(instance.Materials) > 0 {
			p.materialOffsets[at] = uint32(len(p.materials)) + 1
			p.materials = append(p.materials, instance.Materials...)
		}
		if instance.Phase > meshscene.PhaseAir {
			return u, r, fmt.Errorf("metalproto: invalid model phase")
		}
		p.phases[p.cursor[instance.Mesh]] = instance.Phase
		p.offsets[p.cursor[instance.Mesh]] = uint32(instance.PoseOffset)
		p.visuals[p.cursor[instance.Mesh]] = instance.InterpolatedVisual(f.Alpha)
		if len(f.ModelRules) == len(f.Instances) {
			p.visuals[at].State[3] = 0
			if p.rules[at].Shadow[0] > 0 {
				p.visuals[at].State[3] = 1
			}
			p.visuals[at].Outline = p.rules[at].Outline
		}
		p.cursor[instance.Mesh]++
	}
	lights := f.Lights
	if len(lights) > 32 {
		lights = lights[:32]
	}
	if len(f.Distortions) > 192 || f.Fog.Width < 0 || f.Fog.Height < 0 || uint64(f.Fog.Width)*uint64(f.Fog.Height)*4 != uint64(len(f.Fog.RGBA)) {
		return u, r, fmt.Errorf("metalproto: invalid visual payload")
	}
	if len(f.Fog.RGBA) > 0 && (scene.FogAtlas.Width != 896 || scene.FogAtlas.Height != 513) {
		return u, r, fmt.Errorf("metalproto: fog cell grid requires retained authored masks")
	}
	for _, q := range f.Overlay {
		for _, lane := range [4][4]float32{q.Rect, q.UV, q.Color, q.Params} {
			for _, value := range lane {
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					return u, r, fmt.Errorf("metalproto: non-finite overlay quad")
				}
			}
		}
		if q.Params[0] != 0 && q.Params[0] != 1 && q.Params[0] != 2 {
			return u, r, fmt.Errorf("metalproto: invalid overlay texture selector")
		}
		if q.Params[1] != 0 && q.Params[1] != 1 {
			return u, r, fmt.Errorf("metalproto: invalid overlay blend selector")
		}
	}
	groundSprites := 0
	if f.Composed {
		groundSprites = p.orderCompositionSprites(f)
	} else {
		groundSprites = p.orderSprites(f.Sprites, f.Alpha, f.Camera)
	}
	u = liveUpload{Rules: pointer(p.rules), MaterialOffsets: pointer(p.materialOffsets), Materials: pointer(p.materials), RulesCount: uint32(len(f.ModelRules)), MaterialCount: uint32(len(p.materials)), Water: f.Water, GroundSpriteCount: uint32(groundSprites), Overlay: pointer(f.Overlay), OverlayCount: uint32(len(f.Overlay)), Visuals: pointer(p.visuals), Distortions: pointer(f.Distortions), DistortionCount: uint32(len(f.Distortions)), Fog: pointer(f.Fog.RGBA), FogWidth: uint32(f.Fog.Width), FogHeight: uint32(f.Fog.Height), FogRect: f.Fog.Rect, Previous: pointer(f.Previous), Current: pointer(f.Current), Offsets: pointer(p.offsets), Spans: pointer(p.spans), Sprites: pointer(p.sprites), Lights: pointer(lights), Camera: [4]float32{f.Camera[0], f.Camera[1], f.Camera[2], 0}, Alpha: f.Alpha, PoseCount: uint32(len(f.Current)), InstanceCount: uint32(len(f.Instances)), SpriteCount: uint32(len(p.sprites)), LightCount: uint32(len(lights)), Seconds: seconds}
	if f.OverlayVersion != 0 && f.OverlayVersion != p.overlayVersion {
		t := f.OverlayTexture
		if t.Width <= 0 || t.Height <= 0 || t.Width > math.MaxInt32 || t.Height > math.MaxInt32 || uint64(t.Width)*uint64(t.Height)*4 != uint64(len(t.RGBA)) {
			return u, r, fmt.Errorf("metalproto: invalid replacement overlay texture")
		}
		u.OverlayTexture = pointer(t.RGBA)
		u.OverlayVersion = f.OverlayVersion
		u.OverlayWidth = uint32(t.Width)
		u.OverlayHeight = uint32(t.Height)
		dirty := f.OverlayDirty
		if p.overlayVersion == 0 || p.overlayWidth != t.Width || p.overlayHeight != t.Height || dirty[2] == 0 || dirty[3] == 0 {
			dirty = [4]uint32{0, 0, uint32(t.Width), uint32(t.Height)}
		}
		if uint64(dirty[0])+uint64(dirty[2]) > uint64(t.Width) || uint64(dirty[1])+uint64(dirty[3]) > uint64(t.Height) {
			return u, r, fmt.Errorf("metalproto: replacement overlay dirty region is outside texture")
		}
		u.OverlayDirty = dirty
		r.OverlayTextureBytes = int(uint64(dirty[2]) * uint64(dirty[3]) * 4)
		p.overlayWidth, p.overlayHeight = t.Width, t.Height
		p.overlayVersion = f.OverlayVersion
	}
	r.Seconds = seconds
	r.Alpha = f.Alpha
	r.Tick = f.Tick
	r.Instances = len(f.Instances)
	r.Poses = len(f.Current)
	r.Sprites = len(f.Sprites)
	r.GroundSprites = groundSprites
	r.Lights = len(lights)
	r.DroppedLights = len(f.Lights) - len(lights)
	r.Distortions = len(f.Distortions)
	r.FogBytes = len(f.Fog.RGBA)
	r.OverlayQuads = len(f.Overlay)
	r.OverlayBytes = len(f.Overlay) * 64
	r.UploadBytes = uint64(len(p.rules))*112 + uint64(len(p.materialOffsets))*4 + uint64(len(p.materials))*48 + uint64(len(f.Current))*128 + uint64(len(f.Instances))*68 + uint64(len(f.Distortions))*64 + uint64(r.FogBytes) + uint64(r.OverlayBytes) + uint64(r.OverlayTextureBytes) + uint64(len(f.Sprites))*96 + uint64(len(lights))*32

	p.cloaks = p.cloaks[:0]
	p.cloakSubjects = p.cloakSubjects[:0]
	if visualPasses && !f.Composed {
		p.prepareCloaks(f)
	}
	u.Cloaks, u.CloakCount = pointer(p.cloaks), uint32(len(p.cloaks))
	if stock := f.StockEffects; stock != nil {
		var err error
		p.stockUpload, err = p.stockPacking.Prepare(stock.Layers, stock.Atlas, stock.Version, stock.Dirty, stock.Scale)
		if err != nil {
			return u, r, err
		}
		p.groundUpload, err = p.groundPacking.Prepare(stock.Layers, stock.Scale)
		if err != nil {
			return u, r, err
		}
		u.GroundEffects = uintptr(unsafe.Pointer(&p.groundUpload))
		u.Effects = uintptr(unsafe.Pointer(&p.stockUpload))
		u.GlowVertices = pointer(stock.Glow.Vertices)
		u.GlowCount = uint32(len(stock.Glow.Vertices))
		u.GlowEnabled = 1
		u.GlowParams = stock.Glow.Parameters
	}
	u.RetainedLights = pointer(f.Lighting.Lights)
	u.RetainedLightCount = uint32(len(f.Lighting.Lights))
	u.LightControls = f.LightControls
	if u.RetainedLightCount > 64 {
		return u, r, fmt.Errorf("metalproto: production light count exceeds shared budget")
	}
	if err := p.prepareComposition(f, &u); err != nil {
		return u, r, err
	}
	r.UploadBytes += uint64(len(f.Composition.Slots)*48 + len(p.compositionSelectors)*4 + len(p.compositionDraws)*16 + len(p.compositionPaint)*16)
	if f.Composed {
		if len(f.WaterObjects) != len(f.Instances) {
			return u, r, fmt.Errorf("metalproto: water objects differ from instances")
		}
		p.waterObjects = slices.Grow(p.waterObjects[:0], len(f.Instances))[:len(f.Instances)]
		for i, o := range f.WaterObjects {
			p.waterObjects[p.sourcePacked[i]] = o
		}
		var err error
		p.waterUpload, err = p.waterPacking.Prepare(p.waterObjects, p.seabed, f.WaterObjectScales[0], f.WaterObjectScales[1], f.WaterObjectScales[2])
		if err != nil {
			return u, r, err
		}
		u.WaterObjects = uintptr(unsafe.Pointer(&p.waterUpload))
		p.waterSourcesUpload, err = p.waterPacking.PrepareSources(f.WaterSources, f.WaterObjectScales[2])
		if err != nil {
			return u, r, err
		}
		u.WaterSources = uintptr(unsafe.Pointer(&p.waterSourcesUpload))
	}
	var shadowErr error
	u.Shadows, shadowErr = p.shadowPacking.Prepare(f.Shadows, p.sourcePacked)
	if shadowErr != nil {
		return u, r, shadowErr
	}
	if f.LightControls&1 != 0 {
		r.Lights = len(f.Lighting.Lights)
		r.DroppedLights = 0
	}
	r.UploadBytes += uint64(len(f.Lighting.Lights)) * 64
	r.CloakSubjects = len(p.cloakSubjects)
	for _, phase := range p.phases {
		if phase == meshscene.PhaseAir {
			u.AirCount++
			r.AirInstances++
		}
	}
	for i, span := range p.spans[:len(p.cursor)] {
		r.ModelVertices += uint64(len(scene.Meshes[i].Vertices)) * uint64(span.Count)
		r.ModelIndices += uint64(len(scene.Meshes[i].Indices)) * uint64(span.Count)
	}
	p.selectors = p.selectors[:0]
	for pass := 1; pass < nativeSpanPasses; pass++ {
		for mesh, span := range p.spans[:len(p.cursor)] {
			out := meshSpan{Start: uint32(len(p.selectors))}
			for j := uint32(0); j < span.Count; j++ {
				at := span.Start + j
				v := p.visuals[at]
				phase := p.phases[at]
				opaque := !visualPasses || v.State[1] >= 1 || v.State == [4]float32{}
				eligible := false
				if pass == 1 {
					eligible = visualPasses && v.State[3] > 0
				}
				if pass >= 3 && pass <= 5 {
					eligible = opaque && phase == uint32(pass-3)
				}
				if pass >= 6 {
					eligible = visualPasses && opaque && phase == uint32(pass-6) && v.State[2] > 0 && v.Outline[3] > 0
				}
				if eligible {
					p.selectors = append(p.selectors, j)
					out.Count++
				}
			}
			p.spans[pass*len(p.cursor)+mesh] = out
			if pass == 1 {
				r.ShadowInstances += int(out.Count)
				r.ShadowIndices += uint64(len(scene.Meshes[mesh].Indices)) * uint64(out.Count)
				if out.Count > 0 && len(scene.Meshes[mesh].Indices) > 0 {
					r.ShadowDraws++
				}
			} else if pass >= 6 {
				r.Constructing += int(out.Count)
				r.OutlineIndices += uint64(len(scene.Meshes[mesh].EdgeIndices)) * uint64(out.Count)
				if out.Count > 0 && len(scene.Meshes[mesh].EdgeIndices) > 0 {
					r.OutlineDraws++
				}
			} else if pass >= 3 && out.Count > 0 && len(scene.Meshes[mesh].Indices) > 0 {
				r.ModelDraws++
			}
		}
	}
	for _, d := range p.cloaks {
		if len(scene.Meshes[d.Mesh].Indices) > 0 {
			r.ModelDraws++
		}
		v := p.visuals[d.Instance]
		if visualPasses && v.State[2] > 0 && v.Outline[3] > 0 {
			r.Constructing++
			r.OutlineIndices += uint64(len(scene.Meshes[d.Mesh].EdgeIndices))
			if len(scene.Meshes[d.Mesh].EdgeIndices) > 0 {
				r.OutlineDraws++
			}
		}
	}
	u.Selectors = pointer(p.selectors)
	r.UploadBytes += uint64(len(p.selectors))*4 + uint64(len(p.cloaks))*16
	if r.GroundSprites > 0 {
		r.SpriteDraws++
	}
	if r.Sprites > r.GroundSprites {
		r.SpriteDraws++
	}
	return u, r, nil
}
