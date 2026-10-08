//go:build darwin

package metalproto

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// Stock effects (effects.inc). The renderer retains the immutable atlas;
// only an acquired ring's staging, geometry and lens snapshot are rewritten.
type gbEffectsState struct {
	color, multiply, additive, triangles, lens, smoke                                          mtl.ID
	selectSmoke                                                                                mtl.ID
	emptyLights                                                                                *mtl.Buffer
	atlas                                                                                      *mtl.Texture
	version, atlasUploads, atlasBytes, preparedFrames, drawnLayers, emptyLayers                uint64
	drawnLenses, emptyLenses, drawnModels                                                      uint64
	modelSubmitted, modelSuppressed, projectileModels, sourceCount, quadSpanFaces, supersample uint32
	atlasNeedsFullUpload                                                                       bool
	retainedSubmitted, retainedSuppressed, retainedChildren, groundMarkerSuppressed            uint64
	retainedBodyMarkers, retainedShadowMarkers                                                 uint32
	smokeReceiversDrawn                                                                        uint64
}

type gbEffectsRing struct {
	quads, vertices, samples, atlasUpload              *mtl.Buffer
	smoke, smokeSubjects, lights                       *mtl.Buffer
	smokeCount, lightCount                             uint32
	lenses                                             []nativeEffectsLens
	ops                                                []nativeEffectsOp
	layers                                             []nativeEffectsLayer
	atlas, snapshot                                    *mtl.Texture
	quadCount, vertexCount, sampleCount, lensCount     int
	layerCount                                         int
	uploadX, uploadY, uploadW, uploadH, uploadRowBytes int
	version                                            uint64
	uploadPending                                      bool
}

// effectsPipeline: blend -1 none, 0 premultiplied over, 1 multiply, 2 additive.
func (r *gbRenderer) effectsPipeline(vertex, fragment string, blend int) (mtl.ID, error) {
	return r.renderPipeline(vertex, fragment, func(d mtl.PipelineDescriptor) {
		a := d.Attachment(0)
		a.Format(mtl.PixelFormatRGBA16Float)
		if blend >= 0 {
			a.Blending(true)
			a.SourceRGB(mtl.BlendOne)
			if blend == 2 {
				a.DestRGB(mtl.BlendOne)
			} else {
				a.DestRGB(mtl.BlendOneMinusSourceAlpha)
			}
			a.SourceAlpha(mtl.BlendOne)
			a.DestAlpha(mtl.BlendOneMinusSourceAlpha)
			if blend == 1 {
				a.SourceRGB(mtl.BlendZero)
				a.DestRGB(mtl.BlendSourceColor)
				a.SourceAlpha(mtl.BlendZero)
				a.DestAlpha(mtl.BlendOne)
			}
		}
		b := d.Attachment(1)
		b.Format(mtl.PixelFormatRGBA16Float)
		b.WriteMask(mtl.ColorWriteNone)
		d.SetDepthFormat(mtl.PixelFormatDepth32Float)
	})
}

func (r *gbRenderer) effectsInitialize() error {
	s := &gbEffectsState{}
	var err error
	for _, p := range []struct {
		p                *mtl.ID
		vertex, fragment string
		blend            int
	}{
		{&s.color, "nm_fx_quad_vertex", "nm_fx_color_fragment", 0},
		{&s.multiply, "nm_fx_quad_vertex", "nm_fx_multiply_fragment", 1},
		{&s.additive, "nm_fx_quad_vertex", "nm_fx_color_fragment", 2},
		{&s.triangles, "nm_fx_triangle_vertex", "nm_fx_color_fragment", 0},
		{&s.lens, "nm_fx_lens_vertex", "nm_fx_lens_fragment", -1},
		{&s.smoke, "nm_fx_smoke_vertex", "nm_fx_smoke_fragment", 0},
	} {
		if *p.p, err = r.effectsPipeline(p.vertex, p.fragment, p.blend); err != nil {
			return err
		}
	}
	if s.selectSmoke, err = r.computePipeline("nm_fx_select_smoke_lights"); err != nil {
		return err
	}
	if s.emptyLights = r.device.NewBuffer(64, mtl.ResourceShared); s.emptyLights == nil {
		return fmt.Errorf("effects light fallback allocation failed")
	}
	r.effectState = s
	return nil
}

func (r *gbRenderer) effectsSetLighting(ring *gbRing, lights *mtl.Buffer, count uint32) {
	f := ring.effectRing
	if f == nil {
		return
	}
	f.lights = lights
	f.lightCount = 0
	if lights != nil {
		f.lightCount = min(count, 64)
	}
}

func (r *gbRenderer) effectsPrepare(ring *gbRing, u *nativeEffectsUpload) bool {
	f := ring.effectRing
	if u == nil {
		if f != nil {
			f.layerCount, f.uploadPending, f.smokeCount, f.lightCount, f.lights = 0, false, 0, 0, nil
		}
		return true
	}
	s := r.effectState
	if f == nil {
		f = &gbEffectsRing{}
		ring.effectRing = f
	}
	f.layerCount, f.uploadPending, f.lights, f.lightCount, f.smokeCount = 0, false, nil, 0, 0
	counts := [6]uint32{u.QuadCount, u.VertexCount, u.SampleCount, u.LensCount, u.OpCount, u.LayerCount}
	pointers := [6]unsafe.Pointer{u.Quads, u.Vertices, u.Samples, u.Lenses, u.Ops, u.Layers}
	for i := range counts {
		if counts[i] > math.MaxInt32 || (counts[i] > 0 && pointers[i] == nil) {
			return r.fail("Invalid stock-effects upload span")
		}
	}
	if u.SmokeCount > math.MaxInt32 || (u.SmokeCount > 0 && u.Smoke == nil) {
		return r.fail("Invalid smoke receiver upload span")
	}
	if u.Atlas == nil || u.Width == 0 || u.Height == 0 || u.Width > math.MaxInt32 || u.Height > math.MaxInt32 {
		return r.fail("Invalid stock-effects atlas")
	}
	layers := unsafe.Slice((*nativeEffectsLayer)(u.Layers), u.LayerCount)
	ops := unsafe.Slice((*nativeEffectsOp)(u.Ops), u.OpCount)
	lenses := unsafe.Slice((*nativeEffectsLens)(u.Lenses), u.LensCount)
	for _, l := range layers {
		if uint64(l.First)+uint64(l.Count) > uint64(u.OpCount) {
			return r.fail("Stock-effects layer exceeds op storage")
		}
	}
	for _, op := range ops {
		var bound uint32
		switch op.Kind {
		case 0:
			bound = u.QuadCount
		case 1:
			bound = u.LensCount
		case 2:
			bound = u.VertexCount
		case 4:
			bound = math.MaxInt32
		case 5:
			bound = u.SmokeCount
		default:
			bound = u.ProjectileModels
		}
		if op.Kind > 5 || uint64(op.First)+uint64(op.Count) > uint64(bound) || (op.Kind == 2 && op.Count%3 != 0) || (op.Kind == 3 && (op.Reserved > 2 || op.Count != 1)) {
			return r.fail("Invalid ordered stock-effects command")
		}
	}
	for _, l := range lenses {
		if uint64(l.First)+uint64(l.Count) > uint64(u.SampleCount) {
			return r.fail("Stock-effects lens exceeds sample storage")
		}
	}
	qb, vb, sb := int(u.QuadCount)*64, int(u.VertexCount)*48, int(u.SampleCount)*48
	if max(qb, vb, sb) > r.device.MaxBufferLength() {
		return r.fail("Stock-effects device buffer limit")
	}
	f.quads, f.vertices, f.samples = r.grow(f.quads, qb), r.grow(f.vertices, vb), r.grow(f.samples, sb)
	if f.quads == nil || f.vertices == nil || f.samples == nil {
		return r.fail("Stock-effects buffer allocation failed")
	}
	r.copyIn(upEffects, f.quads.Ptr, u.Quads, qb)
	r.copyIn(upEffects, f.vertices.Ptr, u.Vertices, vb)
	r.copyIn(upEffects, f.samples.Ptr, u.Samples, sb)
	smokeBytes, subjectBytes := int(u.SmokeCount)*96, int(u.SmokeCount)*36
	if max(smokeBytes, subjectBytes) > r.device.MaxBufferLength() {
		return r.fail("Smoke receiver device buffer limit")
	}
	f.smoke, f.smokeSubjects = r.grow(f.smoke, max(smokeBytes, 96)), r.grow(f.smokeSubjects, max(subjectBytes, 36))
	if f.smoke == nil || f.smokeSubjects == nil {
		return r.fail("Smoke receiver allocation failed")
	}
	r.copyIn(upEffects, f.smoke.Ptr, u.Smoke, smokeBytes)
	f.smokeCount = u.SmokeCount
	f.lenses = append(f.lenses[:0], lenses...)
	f.ops = append(f.ops[:0], ops...)
	f.layers = append(f.layers[:0], layers...)
	r.uploadBytes[upCPU] += uint64(len(lenses)*32 + len(ops)*16 + len(layers)*16)
	if s.atlas == nil || s.atlas.Width != int(u.Width) || s.atlas.Height != int(u.Height) {
		t := r.target(int(u.Width), int(u.Height), mtl.PixelFormatRGBA8Unorm, mtl.UsageShaderRead)
		if t == nil {
			return r.fail("Stock-effects atlas allocation failed")
		}
		replaceTexture(&s.atlas, t)
		s.atlasNeedsFullUpload = true
	}
	f.atlas, f.version = s.atlas, u.Version
	if s.atlasNeedsFullUpload || s.version != u.Version {
		x, y, w, h := int(u.Dirty[0]), int(u.Dirty[1]), int(u.Dirty[2]), int(u.Dirty[3])
		if s.atlasNeedsFullUpload || w == 0 || h == 0 {
			x, y, w, h = 0, 0, int(u.Width), int(u.Height)
		}
		if x+w > int(u.Width) || y+h > int(u.Height) {
			return r.fail("Stock-effects atlas dirty rectangle out of bounds")
		}
		rowBytes := (w*4 + 255) &^ 255
		if rowBytes*h > r.device.MaxBufferLength() {
			return r.fail("Stock-effects upload buffer limit")
		}
		if f.atlasUpload = r.grow(f.atlasUpload, rowBytes*h); f.atlasUpload == nil {
			return r.fail("Stock-effects staging allocation failed")
		}
		for row := 0; row < h; row++ {
			r.copyIn(upEffectsAtlas, unsafe.Add(f.atlasUpload.Ptr, row*rowBytes), unsafe.Add(u.Atlas, ((y+row)*int(u.Width)+x)*4), w*4)
		}
		f.uploadX, f.uploadY, f.uploadW, f.uploadH, f.uploadRowBytes, f.uploadPending = x, y, w, h, rowBytes, true
	}
	if u.LensCount > 0 && (f.snapshot == nil || f.snapshot.Width != r.color.Width || f.snapshot.Height != r.color.Height) {
		t := r.target(r.color.Width, r.color.Height, mtl.PixelFormatRGBA16Float, mtl.UsageShaderRead)
		if t == nil {
			return r.fail("Stock-effects lens snapshot allocation failed")
		}
		replaceTexture(&f.snapshot, t)
	}
	f.quadCount, f.vertexCount, f.sampleCount, f.lensCount, f.layerCount = int(u.QuadCount), int(u.VertexCount), int(u.SampleCount), int(u.LensCount), int(u.LayerCount)
	s.modelSubmitted, s.modelSuppressed, s.projectileModels, s.sourceCount, s.quadSpanFaces, s.supersample = u.ModelSubmitted, u.ModelSuppressed, u.ProjectileModels, u.SourceCount, u.QuadSpanFaces, u.SupersampleModels
	s.preparedFrames++
	s.retainedBodyMarkers, s.retainedShadowMarkers = 0, 0
	for _, op := range ops {
		if op.Kind == 3 {
			if op.Reserved&1 != 0 {
				s.retainedShadowMarkers++
			} else {
				s.retainedBodyMarkers++
			}
		}
	}
	return true
}

func (r *gbRenderer) effectsBegin(ring *gbRing, cb mtl.CommandBuffer) {
	f := ring.effectRing
	if f == nil {
		return
	}
	s := r.effectState
	if f.smokeCount > 0 {
		lightCount := f.lightCount
		e := r.computeEncoder(cb, "effects.nmEffectsBegin#1")
		e.Pipeline(s.selectSmoke)
		e.Buffer(f.smoke, 0, 0)
		e.Buffer(orEmpty(f.lights, s.emptyLights), 0, 16)
		e.Buffer(f.smokeSubjects, 0, 17)
		e.Bytes(unsafe.Pointer(&lightCount), 4, 18)
		e.DispatchThreads(int(f.smokeCount), 1, 1, min(64, mtl.MaxThreads(s.selectSmoke)), 1, 1)
		e.End()
	}
	if !f.uploadPending {
		return
	}
	blit := r.blitEncoder(cb, "effects.nmEffectsBegin#2")
	blit.CopyBufferToTexture(f.atlasUpload, 0, f.uploadRowBytes, f.uploadRowBytes*f.uploadH, f.uploadW, f.uploadH, f.atlas, f.uploadX, f.uploadY)
	blit.End()
	f.uploadPending = false
	s.version, s.atlasNeedsFullUpload = f.version, false
	s.atlasUploads++
	s.atlasBytes += uint64(f.uploadW * f.uploadH * 4)
}

// Effects share the world pass format so delegated projectile commits and
// ground marks draw into the open encoder instead of splitting the layer.
func (r *gbRenderer) effectsEncoder(cb mtl.CommandBuffer, u gbUniforms) mtl.RenderEncoder {
	if r.fxExternal != 0 {
		r.effectsRestore(r.fxExternal, u)
		return r.fxExternal
	}
	enc := r.renderEncoder(cb, r.worldPass(false), "effects.nmEffectsEncoder#1")
	enc.Cull(mtl.CullNone)
	enc.VertexBytes(unsafe.Pointer(&u.viewport), 16, 3)
	return enc
}
func (r *gbRenderer) effectsRestore(enc mtl.RenderEncoder, u gbUniforms) {
	enc.Cull(mtl.CullNone)
	enc.DepthState(0)
	enc.VertexBytes(unsafe.Pointer(&u.viewport), 16, 3)
}

func (r *gbRenderer) effectsDraw(ring *gbRing, cb mtl.CommandBuffer, layerIndex int, u gbUniforms) {
	f, s := ring.effectRing, r.effectState
	if f == nil || layerIndex >= f.layerCount {
		return
	}
	layer := f.layers[layerIndex]
	if layer.Count == 0 {
		s.emptyLayers++
		return
	}
	s.drawnLayers++
	quads := unsafe.Slice((*float32)(f.quads.Ptr), f.quadCount*16)
	var enc mtl.RenderEncoder
	for i := int(layer.First); i < int(layer.First+layer.Count); i++ {
		op := f.ops[i]
		if op.Count == 0 {
			continue
		}
		switch op.Kind {
		case 4:
			if enc == 0 {
				enc = r.effectsEncoder(cb, u)
			}
			r.fxShared = enc
			drawn := r.groundDraw(ring, cb, u, op.First, op.Count)
			r.fxShared = 0
			r.effectsRestore(enc, u)
			if !drawn {
				s.groundMarkerSuppressed += uint64(op.Count)
			}
			continue
		case 3:
			if enc == 0 {
				enc = r.effectsEncoder(cb, u)
			}
			r.fxShared = enc
			drawn := r.retainedProjectile(ring, cb, u, op.First, op.Reserved)
			r.fxShared = 0
			r.effectsRestore(enc, u)
			switch {
			case !drawn:
				s.retainedSuppressed++
			case op.Reserved&2 != 0:
				s.retainedChildren++
			default:
				s.retainedSubmitted++
			}
			continue
		case 1:
			if enc != 0 && enc != r.fxExternal {
				enc.End()
				enc = 0
			}
			for _, l := range f.lenses[op.First : op.First+op.Count] {
				x, y := max(0, int(math.Floor(float64(l.Read[0])))), max(0, int(math.Floor(float64(l.Read[1]))))
				ex := min(r.color.Width, int(math.Ceil(float64(l.Read[0]+l.Read[2]))))
				ey := min(r.color.Height, int(math.Ceil(float64(l.Read[1]+l.Read[3]))))
				if l.Count == 0 || x >= ex || y >= ey {
					s.emptyLenses++
					continue
				}
				// Each lens reads its own earlier composite; the same command
				// buffer orders the copy before this lens's keyed writes.
				blit := r.blitEncoder(cb, "effects.nmEffectsDraw#1")
				blit.CopyTexture(r.color, x, y, ex-x, ey-y, f.snapshot, x, y)
				blit.End()
				lens := r.effectsEncoder(cb, u)
				lens.Pipeline(s.lens)
				lens.VertexBuffer(f.samples, 0, 0)
				lens.FragmentTexture(f.snapshot, 0)
				lens.DrawBase(mtl.PrimitiveTriangle, 0, 6, int(l.Count), int(l.First))
				lens.End()
				s.drawnLenses++
			}
			continue
		}
		if enc == 0 {
			enc = r.effectsEncoder(cb, u)
		}
		enc.FragmentTexture(f.atlas, 0)
		switch op.Kind {
		case 5:
			enc.Pipeline(s.smoke)
			enc.VertexBuffer(f.smoke, 0, 0)
			enc.VertexBuffer(orEmpty(f.lights, s.emptyLights), 0, 16)
			enc.VertexBuffer(f.smokeSubjects, 0, 17)
			enc.DrawBase(mtl.PrimitiveTriangle, 0, 6, int(op.Count), int(op.First))
			s.smokeReceiversDrawn += uint64(op.Count)
			continue
		case 2:
			enc.Pipeline(s.triangles)
			enc.VertexBuffer(f.vertices, 0, 0)
			enc.Draw(mtl.PrimitiveTriangle, int(op.First), int(op.Count))
			s.drawnModels++
			continue
		}
		enc.VertexBuffer(f.quads, 0, 0)
		for j, last := int(op.First), int(op.First+op.Count); j < last; {
			mode := quads[j*16+13]
			end := j + 1
			for end < last && quads[end*16+13] == mode {
				end++
			}
			switch mode {
			case 1:
				enc.Pipeline(s.multiply)
			case 2:
				enc.Pipeline(s.additive)
			default:
				enc.Pipeline(s.color)
			}
			enc.DrawBase(mtl.PrimitiveTriangle, 0, 6, end-j, j)
			j = end
		}
	}
	if enc != 0 && enc != r.fxExternal {
		enc.End()
	}
}

func (r *gbRenderer) effectsInfo() map[string]any {
	s := r.effectState
	return map[string]any{"stock_effects_enabled": s.preparedFrames != 0, "stock_effects_prepared_frames": s.preparedFrames, "stock_effects_drawn_layers": s.drawnLayers, "stock_effects_empty_layers": s.emptyLayers, "stock_effects_drawn_lenses": s.drawnLenses, "stock_effects_empty_lenses": s.emptyLenses, "stock_effects_drawn_models": s.drawnModels, "stock_effects_prepared_models": s.modelSubmitted, "stock_effects_suppressed_models": s.modelSuppressed, "stock_effects_delegated_projectile_models": s.projectileModels, "stock_effects_retained_body_markers": s.retainedBodyMarkers, "stock_effects_retained_shadow_markers": s.retainedShadowMarkers, "stock_effects_retained_markers_submitted": s.retainedSubmitted, "stock_effects_retained_children_handled": s.retainedChildren, "stock_effects_retained_markers_suppressed": s.retainedSuppressed, "stock_effects_source_count": s.sourceCount, "stock_effects_fan_quad_faces": s.quadSpanFaces, "stock_effects_unused_supersample_models": s.supersample, "stock_effects_atlas_version": s.version, "stock_effects_atlas_uploads": s.atlasUploads, "stock_effects_atlas_uploaded_bytes": s.atlasBytes, "stock_effects_ground_markers_suppressed": s.groundMarkerSuppressed, "stock_effects_smoke_receivers_drawn": s.smokeReceiversDrawn, "stock_effects_bloom_submitted": false}
}

// Ground marks (ground_effects.inc).
type gbGroundState struct {
	color, multiply             mtl.ID
	prepared, drawn, suppressed uint64
}
type gbGroundRing struct {
	marks, frames *mtl.Buffer
	count         int
	ages          [4]float32
}

func (r *gbRenderer) groundInitialize() error {
	s := &gbGroundState{}
	var err error
	if s.color, err = r.effectsPipeline("nm_ground_vertex", "nm_ground_color", 0); err != nil {
		return err
	}
	if s.multiply, err = r.effectsPipeline("nm_ground_vertex", "nm_ground_trail", 1); err != nil {
		return err
	}
	r.groundState = s
	return nil
}

func (r *gbRenderer) groundPrepare(ring *gbRing, u *nativeGroundUpload, generation uint64) bool {
	f := ring.groundRing
	if u == nil {
		if f != nil {
			f.count = 0
		}
		return true
	}
	if u.Count > math.MaxInt32 || (u.Count > 0 && u.Marks == nil) {
		return r.fail("Invalid ground-effects upload")
	}
	if f == nil {
		f = &gbGroundRing{}
		ring.groundRing = f
	}
	f.count = 0
	bytes := int(u.Count) * 48
	if bytes > r.device.MaxBufferLength() {
		return r.fail("Ground-effects buffer limit")
	}
	if u.FrameCount > math.MaxInt32 || (u.Count > 0 && (u.FrameCount == 0 || u.Frames == nil)) {
		return r.fail("Invalid ground-effects frame table")
	}
	for _, m := range unsafe.Slice((*groundMark)(u.Marks), u.Count) {
		k := m.CrossKind[3]
		if !(k >= 0 && k < float32(u.FrameCount)) || k != float32(math.Floor(float64(k))) {
			return r.fail("Ground mark frame index outside its table")
		}
	}
	frameBytes := int(u.FrameCount) * 32
	if f.frames = r.grow(f.frames, max(frameBytes, 32)); f.frames == nil {
		return r.fail("Ground-effects frame allocation failed")
	}
	r.copyIn(upGround, f.frames.Ptr, u.Frames, frameBytes)
	if f.marks = r.residentBuffer(ring, residentGround, generation, u.Marks, bytes, upGround); f.marks == nil {
		return r.fail("Ground-effects buffer allocation failed")
	}
	f.count, f.ages = int(u.Count), u.Ages
	r.groundState.prepared++
	return true
}

func (r *gbRenderer) groundDraw(ring *gbRing, cb mtl.CommandBuffer, u gbUniforms, first, count uint32) bool {
	f, s := ring.groundRing, r.groundState
	if f == nil || int(first)+int(count) > f.count {
		s.suppressed += uint64(count)
		return false
	}
	marks := unsafe.Slice((*groundMark)(f.marks.Ptr), f.count)
	var enc mtl.RenderEncoder
	submitted := false
	for i, last := int(first), int(first+count); i < last; {
		trail := marks[i].CrossKind[2] < .5
		end := i + 1
		for end < last && (marks[end].CrossKind[2] < .5) == trail {
			end++
		}
		if !trail && (r.waterMask == nil || r.water[4] <= 0) {
			s.suppressed += uint64(end - i)
			i = end
			continue
		}
		if enc == 0 {
			enc = r.fxShared
			if enc == 0 {
				enc = r.effectsEncoder(cb, u)
			}
		}
		if trail {
			enc.Pipeline(s.multiply)
		} else {
			enc.Pipeline(s.color)
		}
		enc.VertexBuffer(f.marks, 0, 0)
		enc.VertexBuffer(f.frames, 0, 30)
		if !trail {
			control := [4]float32{r.water[4], f.ages[0], f.ages[1], f.ages[2]}
			enc.FragmentBytes(unsafe.Pointer(&control), 16, 0)
			enc.FragmentTexture(r.waterMask, 0)
		}
		enc.DrawBase(mtl.PrimitiveTriangle, 0, 6, end-i, i)
		s.drawn += uint64(end - i)
		submitted = true
		i = end
	}
	if enc != 0 && enc != r.fxShared {
		enc.End()
	}
	return submitted
}

func (r *gbRenderer) groundInfo() map[string]any {
	s := r.groundState
	return map[string]any{"ground_effects_enabled": true, "ground_effects_prepared_frames": s.prepared, "ground_effects_drawn_marks": s.drawn, "ground_effects_suppressed_marks": s.suppressed}
}

// Production glow (glow.inc).
type gbGlowParameters struct {
	weights [8]float32
	blur    [4]float32
}
type gbGlow struct {
	device                       mtl.Device
	source, resolve              mtl.ID
	shrink, across, down         mtl.ID
	quarter, horizontal, octaves *mtl.Texture
	quadIndices                  *mtl.Buffer
	width, height, quadCapacity  int
	destinationFormat            uint
}

func newGBGlow(device mtl.Device, lib mtl.ID, format uint) (*gbGlow, error) {
	g := &gbGlow{device: device, destinationFormat: format}
	pipeline := func(vertex, fragment string, format uint, screen bool) (mtl.ID, error) {
		d := mtl.NewPipelineDescriptor()
		defer d.Release()
		vfn, ffn := mtl.Function(lib, vertex), mtl.Function(lib, fragment)
		d.SetVertex(vfn)
		d.SetFragment(ffn)
		mtl.Release(vfn)
		mtl.Release(ffn)
		a := d.Attachment(0)
		a.Format(format)
		a.Blending(true)
		a.SourceRGB(mtl.BlendOne)
		if screen {
			a.DestRGB(mtl.BlendOneMinusSourceColor)
			a.SourceAlpha(mtl.BlendZero)
		} else {
			a.DestRGB(mtl.BlendOne)
			a.SourceAlpha(mtl.BlendOne)
		}
		a.DestAlpha(mtl.BlendOne)
		p, msg := device.RenderPipeline(mtl.ID(d))
		if p == 0 {
			return 0, fmt.Errorf("%s/%s: %s", vertex, fragment, msg)
		}
		return p, nil
	}
	compute := func(name string) (mtl.ID, error) {
		fn := mtl.Function(lib, name)
		p, msg := device.ComputePipeline(fn)
		mtl.Release(fn)
		if p == 0 {
			return 0, fmt.Errorf("%s: %s", name, msg)
		}
		return p, nil
	}
	var err error
	if g.source, err = pipeline("nm_glow_source_vertex", "nm_glow_source_fragment", mtl.PixelFormatRGBA16Float, false); err != nil {
		return nil, err
	}
	if g.resolve, err = pipeline("nm_glow_resolve_vertex", "nm_glow_resolve_fragment", format, true); err != nil {
		return nil, err
	}
	for _, c := range []struct {
		p    *mtl.ID
		name string
	}{{&g.shrink, "nm_glow_shrink"}, {&g.across, "nm_glow_across"}, {&g.down, "nm_glow_down"}} {
		if *c.p, err = compute(c.name); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (g *gbGlow) target(w, h int) *mtl.Texture {
	return g.device.NewTexture2D(mtl.PixelFormatRGBA16Float, w, h, mtl.StorageModePrivate, mtl.UsageShaderRead|mtl.UsageShaderWrite)
}

func (g *gbGlow) prepare(w, h int) bool {
	if w == 0 || h == 0 {
		return false
	}
	if g.width == w && g.height == h && g.octaves != nil {
		return true
	}
	qw, qh, ew := (w+3)/4, (h+3)/4, (w+7)/8
	replaceTexture(&g.quarter, g.target(qw, qh))
	replaceTexture(&g.horizontal, g.target(qw+ew, qh))
	replaceTexture(&g.octaves, g.target(qw+ew+3, qh+2))
	if g.quarter == nil || g.horizontal == nil || g.octaves == nil {
		return false
	}
	g.width, g.height = w, h
	return true
}

// quadIndicesFor grows the shared 0,1,2,1,2,3 index buffer. It is replaced,
// never rewritten, so earlier command buffers keep their own.
func (g *gbGlow) quadIndicesFor(quads int) bool {
	if g.quadCapacity >= quads && g.quadIndices != nil {
		return true
	}
	capacity := max(g.quadCapacity, 1024)
	for capacity < quads {
		capacity *= 2
	}
	b := g.device.NewBuffer(capacity*24, mtl.ResourceShared)
	if b == nil {
		return false
	}
	at := unsafe.Slice((*uint32)(b.Ptr), capacity*6)
	for q := uint32(0); q < uint32(capacity); q++ {
		v := q * 4
		copy(at[q*6:], []uint32{v, v + 1, v + 2, v + 1, v + 2, v + 3})
	}
	if g.quadIndices != nil {
		mtl.Release(g.quadIndices.ID)
	}
	g.quadIndices, g.quadCapacity = b, capacity
	return true
}

func (g *gbGlow) dispatch(e mtl.ComputeEncoder, destination *mtl.Texture) {
	e.DispatchThreads(destination.Width, destination.Height, 1, 16, 16, 1)
	e.End()
}

func (g *gbGlow) encode(r *gbRenderer, cb mtl.CommandBuffer, emission, completed, atlas, destination *mtl.Texture, vertices *mtl.Buffer, n int, u gbGlowParameters) bool {
	if n == 0 {
		return true
	}
	if g == nil || emission == nil || completed == nil || atlas == nil || destination == nil || vertices == nil || n%4 != 0 || n > vertices.Size/48 || emission.Format != mtl.PixelFormatRGBA16Float || destination.Format != g.destinationFormat || completed.Width != emission.Width || completed.Height != emission.Height || destination.Width != emission.Width || destination.Height != emission.Height {
		return false
	}
	if !g.prepare(emission.Width, emission.Height) || !g.quadIndicesFor(n/4) {
		return false
	}
	size := [2]float32{float32(g.width), float32(g.height)}
	sizes := [4]uint32{uint32(g.width+3) / 4, uint32(g.height+3) / 4, uint32(g.width+7) / 8, uint32(g.height+7) / 8}
	p := mtl.NewPass()
	p.Color(0, emission, mtl.LoadClear, mtl.StoreStore)
	enc := r.renderEncoder(cb, p, "glow.nmGlowEncode#1")
	enc.Pipeline(g.source)
	enc.VertexBuffer(vertices, 0, 0)
	enc.VertexBytes(unsafe.Pointer(&size), 8, 1)
	enc.FragmentTexture(atlas, 0)
	enc.FragmentTexture(completed, 1)
	enc.DrawIndexed(mtl.PrimitiveTriangle, n/4*6, g.quadIndices, 0)
	enc.End()
	e := r.computeEncoder(cb, "glow.nmGlowEncode#2")
	e.Pipeline(g.shrink)
	e.Texture(emission, 0)
	e.Texture(g.quarter, 1)
	g.dispatch(e, g.quarter)
	e = r.computeEncoder(cb, "glow.nmGlowEncode#3")
	e.Pipeline(g.across)
	e.Texture(g.quarter, 0)
	e.Texture(g.horizontal, 1)
	e.Bytes(unsafe.Pointer(&u), int(unsafe.Sizeof(u)), 0)
	g.dispatch(e, g.horizontal)
	e = r.computeEncoder(cb, "glow.nmGlowEncode#4")
	e.Pipeline(g.down)
	e.Texture(g.horizontal, 0)
	e.Texture(g.octaves, 1)
	e.Bytes(unsafe.Pointer(&u), int(unsafe.Sizeof(u)), 0)
	e.Bytes(unsafe.Pointer(&sizes), 16, 1)
	g.dispatch(e, g.octaves)
	if r.glowDeferResolve {
		return true
	}
	p = mtl.NewPass()
	p.Color(0, destination, mtl.LoadLoad, mtl.StoreStore)
	enc = r.renderEncoder(cb, p, "glow.nmGlowEncode#5")
	enc.Pipeline(g.resolve)
	enc.FragmentTexture(g.octaves, 0)
	enc.FragmentBytes(unsafe.Pointer(&u), int(unsafe.Sizeof(u)), 0)
	enc.FragmentBytes(unsafe.Pointer(&sizes), 16, 1)
	enc.Draw(mtl.PrimitiveTriangle, 0, 3)
	enc.End()
	return true
}

// Water objects (water_objects.inc) and reflection tiles (water_tiles.inc).
type gbWaterObjectsState struct {
	source, stock, resolve, commit, surface                                            mtl.ID
	tiles                                                                              *gbWaterTilesState
	reflectionColor, reflectionSoft                                                    *mtl.Texture
	frames, reflectedModels, reflectionVertices, groupSuppressed, capSuppressed        uint64
	seabedDraws, underwaterSlots, reflectedSprites, reflectedLines, stockCapSuppressed uint64
}
type gbWaterObjectsRing struct {
	objects, slotFlags, seabed, sources                 *mtl.Buffer
	tiles                                               *gbWaterTilesFrame
	painted                                             *mtl.Texture
	objectCount, seabedCount, slotCount, sourceVertices int
	controls                                            [4]float32
}
type gbWaterTilesState struct {
	classify                                     mtl.ID
	enabled                                      bool
	encodedFrames, classifiedTiles, bypassFrames uint64
}
type gbWaterTilesFrame struct {
	flags                        *mtl.Buffer
	width, height, columns, rows int
	encoded                      bool
}

const gbWaterTileSide, gbWaterTileThreads = 64, 128

func (r *gbRenderer) waterObjectsPipeline(vertex, fragment string, outputs int, source, blend bool) (mtl.ID, error) {
	return r.renderPipeline(vertex, fragment, func(d mtl.PipelineDescriptor) {
		if fragment == "nm_wo_commit_fragment" {
			d.SetDepthFormat(mtl.PixelFormatDepth32Float)
		}
		for i := 0; i < outputs; i++ {
			a := d.Attachment(i)
			if source {
				a.Format(mtl.PixelFormatRGBA8Unorm)
			} else {
				a.Format(mtl.PixelFormatRGBA16Float)
			}
			if blend {
				a.Blending(true)
				a.SourceRGB(mtl.BlendOne)
				if !source && i == 1 {
					a.DestRGB(mtl.BlendOne)
				} else {
					a.DestRGB(mtl.BlendOneMinusSourceAlpha)
				}
				a.SourceAlpha(mtl.BlendOne)
				a.DestAlpha(mtl.BlendOneMinusSourceAlpha)
			}
		}
	})
}

func (r *gbRenderer) waterObjectsInitialize() error {
	s := &gbWaterObjectsState{}
	classify, err := r.computePipeline("nm_water_tiles_classify")
	if err != nil {
		return err
	}
	if mtl.MaxThreads(classify) < gbWaterTileThreads {
		return fmt.Errorf("Reflection tile classification needs 128 threads per group")
	}
	s.tiles = &gbWaterTilesState{classify: classify, enabled: true}
	if v, ok := lookupEnv("NANOLATHE_METAL_FULL_REFLECTIONS"); ok && v == "1" {
		s.tiles.enabled = false
	}
	for _, p := range []struct {
		p                *mtl.ID
		vertex, fragment string
		outputs          int
		source, blend    bool
	}{
		{&s.source, "nm_wo_reflection_vertex", "nm_wo_reflection_source", 2, true, true},
		{&s.stock, "nm_wo_stock_vertex", "nm_wo_stock_source", 2, true, true},
		{&s.resolve, "nm_wo_screen_vertex", "nm_wo_reflection_resolve", 1, false, true},
		{&s.commit, "nm_wo_commit_vertex", "nm_wo_commit_fragment", 2, false, true},
		{&s.surface, "nm_wo_screen_vertex", "nm_wo_surface_fragment", 1, false, false},
	} {
		if *p.p, err = r.waterObjectsPipeline(p.vertex, p.fragment, p.outputs, p.source, p.blend); err != nil {
			return err
		}
	}
	r.waterObjectsState = s
	return nil
}

func (r *gbRenderer) waterObjectsPrepare(ring *gbRing, u *nativeWaterObjectsUpload) bool {
	f := ring.waterObjectsRing
	if u == nil {
		if f != nil {
			f.objectCount, f.seabedCount, f.slotCount, f.sourceVertices = 0, 0, 0, 0
		}
		return true
	}
	if u.ObjectCount > math.MaxInt32 || u.SeabedCount > math.MaxInt32 || (u.ObjectCount > 0 && u.Objects == nil) || (u.SeabedCount > 0 && u.Seabed == nil) {
		return r.fail("Invalid water-object upload")
	}
	if int(u.ObjectCount)*4 > ring.instances.Size || int(u.ObjectCount)*4 > ring.compositionSelectors.Size {
		return r.fail("Water-object instances exceed retained model span")
	}
	if f == nil {
		f = &gbWaterObjectsRing{}
		ring.waterObjectsRing = f
	}
	f.objectCount, f.seabedCount, f.slotCount, f.sourceVertices = 0, 0, 0, 0
	ob, sb, fb := int(u.ObjectCount)*16, int(u.SeabedCount)*4, ring.compositionSlotCount*16
	if max(ob, sb, fb) > r.device.MaxBufferLength() {
		return r.fail("Water-object device buffer limit")
	}
	f.objects, f.seabed, f.slotFlags = r.grow(f.objects, ob), r.grow(f.seabed, sb), r.grow(f.slotFlags, fb)
	if f.objects == nil || f.seabed == nil || f.slotFlags == nil {
		return r.fail("Water-object allocation failed")
	}
	r.copyIn(upWater, f.objects.Ptr, u.Objects, ob)
	r.copyIn(upWater, f.seabed.Ptr, u.Seabed, sb)
	for _, s := range unsafe.Slice((*uint32)(f.seabed.Ptr), u.SeabedCount) {
		if int(s) >= ring.spriteCount {
			return r.fail("Seabed sprite exceeds retained sprite span")
		}
	}
	flags := unsafe.Slice((*[4]float32)(f.slotFlags.Ptr), ring.compositionSlotCount)
	clear(flags)
	selectors := unsafe.Slice((*uint32)(ring.compositionSelectors.Ptr), ring.compositionSelectors.Size/4)
	objects := unsafe.Slice((*[4]float32)(f.objects.Ptr), u.ObjectCount)
	// Isolated source pages remain available to reflections. Only the
	// parent's water policy admits the final group to the underwater pass.
	for i := range objects {
		slot := selectors[i]
		if slot == 0 {
			continue
		}
		if int(slot) > ring.compositionSlotCount {
			return r.fail("Water-object selector exceeds subject atlas")
		}
		final := r.groupFinalSlot(ring, i, slot)
		objects[i][3] = float32(final)
		if final == slot {
			flags[slot-1][0] = objects[i][2]
		}
	}
	if groups := ring.groups; groups != nil && groups.groupCount > 0 {
		for _, group := range unsafe.Slice((*nativeGroup)(groups.descriptors.Ptr), groups.groupCount) {
			if group.Info[0] >= u.ObjectCount || group.Info[2] == 0 || int(group.Info[2]) > ring.compositionSlotCount {
				return r.fail("Water group exceeds parent/slot span")
			}
			flags[group.Info[2]-1][0] = objects[group.Info[0]][2]
		}
	}
	for i := range flags {
		if flags[i][0] > .5 {
			r.waterObjectsState.underwaterSlots++
		}
	}
	f.objectCount, f.seabedCount, f.slotCount, f.controls = int(u.ObjectCount), int(u.SeabedCount), ring.compositionSlotCount, u.Controls
	r.waterObjectsState.frames++
	return true
}

func (r *gbRenderer) waterObjectsPrepareSources(ring *gbRing, u *nativeWaterSourcesUpload) bool {
	f := ring.waterObjectsRing
	if u == nil {
		if f != nil {
			f.sourceVertices = 0
		}
		return true
	}
	if f == nil || u.VertexCount > math.MaxInt32 || u.VertexCount%4 != 0 || (u.VertexCount > 0 && u.Sources == nil) {
		return r.fail("Invalid stock water-source upload")
	}
	f.sourceVertices = 0
	bytes := int(u.VertexCount) * 64
	if bytes > r.device.MaxBufferLength() {
		return r.fail("Stock water-source buffer limit")
	}
	if f.sources = r.grow(f.sources, bytes); f.sources == nil {
		return r.fail("Stock water-source allocation failed")
	}
	r.copyIn(upWater, f.sources.Ptr, u.Sources, bytes)
	f.sourceVertices = int(u.VertexCount)
	return true
}

func waterPass(color *mtl.Texture, clearing bool) mtl.PassDescriptor {
	p := mtl.NewPass()
	if clearing {
		p.Color(0, color, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
	} else {
		p.Color(0, color, mtl.LoadLoad, mtl.StoreStore)
	}
	return p
}

func (r *gbRenderer) waterObjectsUniforms(f *gbWaterObjectsRing, enc mtl.RenderEncoder, u gbUniforms) {
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.VertexBytes(unsafe.Pointer(&r.water), 80, 9)
	enc.FragmentBytes(unsafe.Pointer(&r.water), 80, 9)
	control := f.controls
	enc.VertexBytes(unsafe.Pointer(&control), 16, 19)
	enc.FragmentBytes(unsafe.Pointer(&control), 16, 19)
}

// waterObjectsReflecting: reflections run when water lies within a block of
// the view (water[11], as production's visibleWater) and a source is admitted.
func (r *gbRenderer) waterObjectsReflecting(ring *gbRing) bool {
	f := ring.waterObjectsRing
	if f == nil || (f.objectCount == 0 && f.sourceVertices == 0) || r.waterMask == nil || r.water[4] <= 0 || r.water[11] < .5 {
		return false
	}
	if f.sourceVertices > 0 {
		return true
	}
	objects := unsafe.Slice((*[4]float32)(f.objects.Ptr), f.objectCount)
	selectors := unsafe.Slice((*uint32)(ring.compositionSelectors.Ptr), ring.compositionSelectors.Size/4)
	for _, d := range ring.compositionDraws[:ring.compositionDrawCount] {
		if r.modelMetadata != nil && int(d.Instance) < f.objectCount && selectors[d.Instance] != 0 && objects[d.Instance][0] > .5 {
			return true
		}
	}
	return false
}

func (r *gbRenderer) waterObjectsReflections(ring *gbRing, cb mtl.CommandBuffer, poses *mtl.Buffer, u gbUniforms) bool {
	if !r.waterObjectsReflecting(ring) {
		return true
	}
	f, s := ring.waterObjectsRing, r.waterObjectsState
	objects := unsafe.Slice((*[4]float32)(f.objects.Ptr), f.objectCount)
	selectors := unsafe.Slice((*uint32)(ring.compositionSelectors.Ptr), ring.compositionSelectors.Size/4)
	if s.reflectionColor == nil || s.reflectionColor.Width != r.color.Width || s.reflectionColor.Height != r.color.Height {
		rt := uint(mtl.UsageRenderTarget | mtl.UsageShaderRead)
		replaceTexture(&s.reflectionColor, r.target(r.color.Width, r.color.Height, mtl.PixelFormatRGBA8Unorm, rt))
		replaceTexture(&s.reflectionSoft, r.target(r.color.Width, r.color.Height, mtl.PixelFormatRGBA8Unorm, rt))
	}
	if s.reflectionColor == nil || s.reflectionSoft == nil {
		return r.fail("Reflection source allocation failed")
	}
	pass := waterPass(s.reflectionColor, true)
	pass.Color(1, s.reflectionSoft, mtl.LoadClear, mtl.StoreStore).Clear(0, 0, 0, 0)
	enc := r.renderEncoder(cb, pass, "water_objects.nmWaterObjectsReflections#1")
	r.modelBindings(enc, poses, ring, u)
	r.waterObjectsUniforms(f, enc, u)
	enc.Pipeline(s.source)
	enc.DepthState(0)
	enc.Cull(mtl.CullNone)
	r.faceReflectionsBind(ring, enc)
	enc.FragmentTexture(r.modelAtlasColor, 0)
	enc.FragmentTexture(r.modelMetadata, 1)
	vertices := 0
	for _, d := range ring.compositionDraws[:ring.compositionDrawCount] {
		if r.modelMetadata == nil || int(d.Instance) >= f.objectCount || selectors[d.Instance] == 0 || objects[d.Instance][0] < .5 {
			continue
		}
		faceMesh := r.meshes[d.Mesh].faceMesh
		if faceMesh == nil || faceMesh.indexCount == 0 {
			continue
		}
		instance := int(d.Instance)
		r.projectionBind(ring, enc, instance)
		r.facesBind(ring, enc, instance)
		enc.VertexBuffer(ring.groups.instanceGroups, instance*16, 20)
		vertices += faceMesh.indexCount
		enc.VertexBuffer(faceMesh.corners, 0, 0)
		enc.VertexBuffer(ring.instances, instance*4, 1)
		enc.VertexBuffer(ring.visuals, instance*64, 5)
		enc.VertexBuffer(ring.rules, instance*112, 10)
		enc.VertexBuffer(ring.compositionSelectors, instance*4, 14)
		enc.VertexBuffer(f.objects, instance*16, 18)
		enc.DrawIndexed(mtl.PrimitiveTriangle, faceMesh.indexCount, faceMesh.indices, 0)
		s.reflectedModels++
	}
	// Production appends stock billboards, then strokes, after model faces.
	// Both consume four source corners; the GPU budget rejects excess quads.
	stock := f.sourceVertices
	if stock > 0 {
		sources := unsafe.Slice((*[16]float32)(f.sources.Ptr), stock)
		sprite := false
		for i := 0; i < stock; i += 4 {
			if sources[i][3] < 1.5 {
				sprite = true
				break
			}
		}
		atlas := r.effectState.atlas
		if sprite && atlas == nil {
			enc.End()
			return r.fail("Reflected billboard has no uploaded effects atlas")
		}
		enc.Pipeline(s.stock)
		enc.VertexBuffer(f.sources, 0, 0)
		enc.FragmentTexture(orTexture(atlas, r.emptyFog), 0)
		enc.DrawInstanced(mtl.PrimitiveTriangle, 0, 6, stock/4)
		for i := 0; i < stock; i += 4 {
			if sources[i][3] < 1.5 {
				s.reflectedSprites++
			} else {
				s.reflectedLines++
			}
		}
	}
	s.reflectionVertices += uint64(vertices + stock)
	enc.End()
	if f.tiles == nil {
		f.tiles = &gbWaterTilesFrame{}
	}
	if err := r.waterTilesEncode(s.tiles, f.tiles, cb, s.reflectionColor, s.reflectionSoft); err != nil {
		return r.fail("%s", err.Error())
	}
	enc = r.renderEncoder(cb, waterPass(r.color, false), "water_objects.nmWaterObjectsReflections#2")
	r.waterObjectsUniforms(f, enc, u)
	enc.Pipeline(s.resolve)
	if !f.tiles.encoded || f.tiles.flags == nil {
		enc.End()
		return r.fail("Reflection tile bind failed")
	}
	enc.FragmentBuffer(f.tiles.flags, 0, 23)
	enc.FragmentTexture(s.reflectionColor, 0)
	enc.FragmentTexture(s.reflectionSoft, 1)
	enc.FragmentTexture(r.waterMask, 3)
	enc.Draw(mtl.PrimitiveTriangle, 0, 3)
	enc.End()
	return true
}

// waterTilesEncode classifies the reflection planes into 64-pixel tiles. The
// dispatch overwrites every tile; disabled, every flag is 3 (full filter).
func (r *gbRenderer) waterTilesEncode(state *gbWaterTilesState, frame *gbWaterTilesFrame, cb mtl.CommandBuffer, color, soft *mtl.Texture) error {
	frame.encoded = false
	if color == nil || soft == nil || color.Format != mtl.PixelFormatRGBA8Unorm || soft.Format != mtl.PixelFormatRGBA8Unorm || color.Width == 0 || color.Height == 0 || color.Width != soft.Width || color.Height != soft.Height {
		return fmt.Errorf("Invalid reflection tile source planes")
	}
	columns, rows := (color.Width+gbWaterTileSide-1)/gbWaterTileSide, (color.Height+gbWaterTileSide-1)/gbWaterTileSide
	bytes := columns * rows * 4
	if bytes > r.device.MaxBufferLength() {
		return fmt.Errorf("Reflection tile buffer exceeds device limit")
	}
	if frame.flags == nil || frame.flags.Size < bytes {
		b := r.device.NewBuffer(bytes, mtl.ResourceShared)
		if b == nil {
			return fmt.Errorf("Reflection tile allocation failed")
		}
		if frame.flags != nil {
			mtl.Release(frame.flags.ID)
		}
		frame.flags = b
	}
	if state.enabled {
		e := r.computeEncoder(cb, "water_tiles.nmWaterTilesEncode#1")
		e.Pipeline(state.classify)
		e.Texture(color, 0)
		e.Texture(soft, 1)
		e.Buffer(frame.flags, 0, 0)
		e.DispatchGroups(columns, rows, 1, gbWaterTileThreads, 1, 1)
		e.End()
		state.classifiedTiles += uint64(columns * rows)
	} else {
		// The ring is acquired, so no GPU reader still owns it. Write WORD 3,
		// not byte 3, to keep the shader's bit contract.
		flags := unsafe.Slice((*uint32)(frame.flags.Ptr), columns*rows)
		for i := range flags {
			flags[i] = 3
		}
		state.bypassFrames++
	}
	frame.width, frame.height, frame.columns, frame.rows, frame.encoded = color.Width, color.Height, columns, rows, true
	state.encodedFrames++
	return nil
}

// waterObjectsBindCommit replaces the ordinary atlas-commit pipeline, leaving
// slot/paint bindings and painter order in compositionCommit unchanged.
func (r *gbRenderer) waterObjectsBindCommit(ring *gbRing, enc mtl.RenderEncoder, u gbUniforms) {
	f := ring.waterObjectsRing
	if f == nil || f.slotCount == 0 || r.modelMetadata == nil || r.waterMask == nil {
		return
	}
	enc.Pipeline(r.waterObjectsState.commit)
	r.waterObjectsUniforms(f, enc, u)
	enc.VertexBuffer(f.slotFlags, 0, 18)
	enc.FragmentTexture(r.modelMetadata, 2)
	enc.FragmentTexture(r.waterMask, 3)
}

func (r *gbRenderer) waterObjectsSeabed(ring *gbRing, enc mtl.RenderEncoder, u gbUniforms) {
	f := ring.waterObjectsRing
	if f == nil || f.seabedCount == 0 {
		return
	}
	enc.Pipeline(r.sprite)
	enc.DepthState(0)
	enc.VertexBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentBytes(unsafe.Pointer(&u), gbUniformsSize, 3)
	enc.FragmentTexture(r.spriteAtlas, 0)
	for _, index := range unsafe.Slice((*uint32)(f.seabed.Ptr), f.seabedCount) {
		enc.VertexBuffer(ring.sprites, int(index)*96, 0)
		enc.Draw(mtl.PrimitiveTriangle, 0, 6)
		r.waterObjectsState.seabedDraws++
	}
}

// waterObjectsSurface is the active surface path: base terrain defers water,
// ground-light resolve and shadow multiplication to this draw.
func (r *gbRenderer) waterObjectsSurface(ring *gbRing, cb mtl.CommandBuffer, u gbUniforms) bool {
	f := ring.waterObjectsRing
	if f == nil || r.waterMask == nil || r.water[7] < .5 {
		return true
	}
	if f.painted == nil || f.painted.Width != r.color.Width || f.painted.Height != r.color.Height {
		replaceTexture(&f.painted, r.target(r.color.Width, r.color.Height, mtl.PixelFormatRGBA16Float, mtl.UsageShaderRead))
	}
	if f.painted == nil {
		return r.fail("Painted seabed snapshot allocation failed")
	}
	blit := r.blitEncoder(cb, "water_objects.nmWaterObjectsSurface#1")
	blit.CopyTexture(r.color, 0, 0, r.color.Width, r.color.Height, f.painted, 0, 0)
	blit.End()
	water := r.water
	water[12] = u.camera[0] - u.viewport[0]*.5/u.camera[2]
	water[13] = u.camera[1] - u.viewport[1]*.5/u.camera[2]
	water[14] = u.camera[0] + u.viewport[0]*.5/u.camera[2]
	water[15] = u.camera[1] + u.viewport[1]*.5/u.camera[2]
	enc := r.renderEncoder(cb, waterPass(r.color, false), "water_objects.nmWaterObjectsSurface#2")
	r.waterObjectsUniforms(f, enc, u)
	enc.FragmentBytes(unsafe.Pointer(&water), 80, 9)
	enc.Pipeline(r.waterObjectsState.surface)
	enc.FragmentTexture(f.painted, 0)
	enc.FragmentTexture(r.waterMask, 3)
	enc.FragmentTexture(r.emptyMask, 2)
	enc.FragmentTexture(orTexture(r.groundField, r.emptyFog), 5)
	enc.Draw(mtl.PrimitiveTriangle, 0, 3)
	enc.End()
	return true
}

func (r *gbRenderer) waterObjectsInfo() map[string]any {
	s := r.waterObjectsState
	return map[string]any{"water_objects_enabled": true, "water_object_frames": s.frames, "water_reflected_models": s.reflectedModels, "water_reflection_submitted_raster_vertices": s.reflectionVertices, "water_group_suppressed": s.groupSuppressed, "water_cap_suppressed": s.capSuppressed, "water_seabed_draws": s.seabedDraws, "water_underwater_slots": s.underwaterSlots, "water_stock_reflection_sources_supported": true, "water_reflected_sprites": s.reflectedSprites, "water_reflected_lines": s.reflectedLines, "water_stock_cap_suppressed": s.stockCapSuppressed}
}

func (r *gbRenderer) waterTilesInfo() map[string]any {
	state := r.waterObjectsState.tiles
	var frame gbWaterTilesFrame
	if r.lastRing != nil && r.lastRing.waterObjectsRing != nil && r.lastRing.waterObjectsRing.tiles != nil {
		frame = *r.lastRing.waterObjectsRing.tiles
	}
	flagBytes := 0
	if frame.flags != nil {
		flagBytes = frame.flags.Size
	}
	return map[string]any{"water_reflection_tiles_enabled": state.enabled, "water_reflection_full_filter_frames": state.bypassFrames, "water_reflection_tile_side_physical": gbWaterTileSide, "water_reflection_tile_frames": state.encodedFrames, "water_reflection_tiles_classified": state.classifiedTiles, "water_reflection_tile_columns": frame.columns, "water_reflection_tile_rows": frame.rows, "water_reflection_tile_buffer_bytes": flagBytes, "water_reflection_tile_content_rule": "any RGBA channel > 0", "water_reflection_tile_soft_rule": "soft.r > 0"}
}
