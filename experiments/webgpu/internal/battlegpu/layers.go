package battlegpu

import (
	"cmp"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed terrain.wgsl
var layerTerrainSource string

//go:embed water.wgsl
var layerWaterSource string

var layerTerrainShader = layerTerrainSource + "\n" + layerWaterSource

//go:embed sprites.wgsl
var layerSpriteShader string

//go:embed fog.wgsl
var layerFogShader string

//go:embed overlay.wgsl
var layerOverlayShader string

const layerUniformSize = 240
const layerSmokeStride = 144

type LayerCounters struct{ Draws, Sprites, Overlay, UploadBytes int }

type layerTexture struct {
	t    *wgpu.Texture
	v    *wgpu.TextureView
	w, h int
}

func (t *layerTexture) close() {
	if t.v != nil {
		t.v.Release()
	}
	if t.t != nil {
		t.t.Release()
	}
	*t = layerTexture{}
}

type layerBuffer struct {
	b    *wgpu.Buffer
	size int
}

func (b *layerBuffer) close() {
	if b.b != nil {
		b.b.Release()
	}
	*b = layerBuffer{}
}

type layerDraw struct{ kind, first, count, mode uint32 }

// Layers borrows the host device, queue, attachments and synchronous LiveFrame
// input. Prepare and resource replacement require the host's completion boundary.
// Its stock operations replay the production packets (GPU design C-G3, C-G7);
// it never makes emitters or simulation state of its own.
type Layers struct {
	d                                                                                           *wgpu.Device
	q                                                                                           *wgpu.Queue
	format, outputFormat                                                                        gputypes.TextureFormat
	owned                                                                                       []func()
	closed                                                                                      bool
	state                                                                                       *battleRenderState
	uniform                                                                                     *wgpu.Buffer
	terrain, sprite, feature, annotation, fog                                                   *wgpu.RenderPipeline
	overlay                                                                                     [2]*wgpu.RenderPipeline
	fx                                                                                          [3]*wgpu.RenderPipeline
	geometry, ground, trail, smoke                                                              *wgpu.RenderPipeline
	terrainLayout, spriteLayout, annotationLayout, fogLayout, overlayLayout                     *wgpu.BindGroupLayout
	terrainGroup, spriteGroup, annotationGroup, fogGroup, overlayGroup, effectGroup             *wgpu.BindGroup
	terrainImage, tileAtlas, tileDetail, lookup, spriteAtlas, spriteDetail, fogAtlas, waterMask layerTexture
	overlayAtlas, effectAtlas, fogGrid                                                          layerTexture
	sprites, overlayQuads, effectQuads, effectVertices, effectGround, effectSmoke, lights       layerBuffer
	annotations                                                                                 layerBuffer
	world                                                                                       *wgpu.TextureView // borrowed; never released by Layers
	glowView, groundLightView                                                                   *wgpu.TextureView // borrowed component outputs
	glowValues, groundLightRect, groundLightValues                                              [4]float32
	uniformBytes                                                                                [layerUniformSize]byte
	rect                                                                                        [4]float32
	tileInfo                                                                                    [4]uint32
	tilesEnabled                                                                                bool
	waterStep                                                                                   int
	fogAtlasReady                                                                               bool
	counts                                                                                      LayerCounters
	spriteBands                                                                                 [2][2]uint32
	spriteRemap                                                                                 []uint32 // original source -> packed instance plus one; zero means filtered
	featureSources, featureDrawn                                                                []bool
	looseSpriteRuns                                                                             [2][]layerDraw
	featurePaintOrder                                                                           bool
	projectilePaintOrder                                                                        bool
	lensPaintOrder                                                                              bool
	featurePaintCount                                                                           int
	annotationRanges                                                                            [][2]uint32 // one-based composition slot -> first/count
	overlayRuns                                                                                 []layerDraw
	effectLayers                                                                                [][]layerDraw
	effectBytes                                                                                 [5][]byte
	geometrySpans                                                                               []layerDraw
	spriteBytes, overlayBytes                                                                   []byte
	orderedSprites                                                                              []meshscene.Sprite
	upperSprites                                                                                []layerSpriteEntry
	overlayVersion, effectVersion, fogGeneration                                                uint64
	missing                                                                                     map[string]int
}

func layerError(expected string, args ...any) error {
	return fmt.Errorf("nanolathe: WebGPU layer operation failed: logical path presentation/layers, providers searched [retained scene], expected %s", fmt.Sprintf(expected, args...))
}

func layerFloat(b []byte, at int, x float32) {
	binary.LittleEndian.PutUint32(b[at:], math.Float32bits(x))
}
func layerVec(b []byte, at int, v [4]float32) {
	for i, x := range v {
		layerFloat(b, at+i*4, x)
	}
}
func layerPackQuad(dst []byte, q meshscene.OverlayQuad, scale float32) {
	for i := range q.Rect {
		q.Rect[i] *= scale
	}
	layerVec(dst, 0, q.Rect)
	layerVec(dst, 16, q.UV)
	layerVec(dst, 32, q.Color)
	layerVec(dst, 48, q.Params)
}

// Production annotations already contain physical rectangles and straight RGBA.
// Only their storage order changes: stable slot grouping leaves each slot's
// authored sequence intact (Metal composition annotation ranges, GPU C-G5).
func layerPackAnnotations(in []meshscene.Annotation, slotCount int) ([]byte, [][2]uint32) {
	order := slices.Clone(in)
	slices.SortStableFunc(order, func(a, b meshscene.Annotation) int { return cmp.Compare(a.Slot, b.Slot) })
	ranges := make([][2]uint32, slotCount)
	data := make([]byte, 0, len(in)*64)
	for _, a := range order {
		if a.Slot == 0 || uint64(a.Slot) > uint64(slotCount) {
			continue
		}
		r := &ranges[a.Slot-1]
		if r[1] == 0 {
			r[0] = uint32(len(data) / 64)
		}
		r[1]++
		first := len(data)
		data = append(data, make([]byte, 64)...)
		layerVec(data, first, a.Rect)
		layerVec(data, first+32, a.Color)
	}
	return data, ranges
}

func layerValidateTexture(t meshscene.Texture) error {
	if t.Width <= 0 || t.Height <= 0 || t.Width > 8192 || t.Height > 8192 || int64(t.Width)*int64(t.Height)*4 != int64(len(t.RGBA)) {
		return layerError("texture must contain complete RGBA8 pixels within core 8192 dimension limit")
	}
	return nil
}

func (l *Layers) layerTexture(label string, t meshscene.Texture, format gputypes.TextureFormat) (out layerTexture, err error) {
	if err = layerValidateTexture(t); err != nil {
		return
	}
	out.w, out.h = t.Width, t.Height
	out.t, err = l.d.CreateTexture(&wgpu.TextureDescriptor{Label: label, Size: wgpu.Extent3D{Width: uint32(t.Width), Height: uint32(t.Height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst})
	if err != nil {
		return
	}
	out.v, err = l.d.CreateTextureView(out.t, nil)
	if err == nil {
		err = l.layerWriteTexture(&out, t, [4]uint32{})
	}
	if err != nil {
		out.close()
	}
	return
}

func (l *Layers) layerWriteTexture(dst *layerTexture, t meshscene.Texture, dirty [4]uint32) error {
	x, y, w, h := int(dirty[0]), int(dirty[1]), int(dirty[2]), int(dirty[3])
	if w == 0 || h == 0 {
		x, y, w, h = 0, 0, t.Width, t.Height
	}
	if x+w > t.Width || y+h > t.Height {
		return layerError("atlas dirty rectangle outside texture")
	}
	// Queue.WriteTexture permits tight rows; unlike encoder copies it requires
	// no 256-byte row padding. Copy only the detached rectangle synchronously.
	data := t.RGBA[(y*t.Width+x)*4:]
	if err := l.q.WriteTexture(&wgpu.ImageCopyTexture{Texture: dst.t, Origin: wgpu.Origin3D{X: uint32(x), Y: uint32(y)}}, data, &wgpu.ImageDataLayout{BytesPerRow: uint32(t.Width * 4), RowsPerImage: uint32(h)}, &wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}); err != nil {
		return err
	}
	l.counts.UploadBytes += w * h * 4
	return nil
}

func (l *Layers) layerUpdateTexture(dst *layerTexture, t meshscene.Texture, version uint64, old *uint64, dirty [4]uint32) (bool, error) {
	if version != 0 && version == *old && dst.w == t.Width && dst.h == t.Height {
		return false, nil
	}
	if err := layerValidateTexture(t); err != nil {
		return false, err
	}
	if dst.w != t.Width || dst.h != t.Height {
		n, err := l.layerTexture("live-layer-atlas", t, wgpu.TextureFormatRGBA8Unorm)
		if err != nil {
			return false, err
		}
		dst.close()
		*dst = n
		*old = version
		return true, nil
	}
	if *old == 0 {
		dirty = [4]uint32{}
	}
	if err := l.layerWriteTexture(dst, t, dirty); err != nil {
		return false, err
	}
	*old = version
	return false, nil
}

func layerBufferEntry(binding uint32, stages gputypes.ShaderStages, kind gputypes.BufferBindingType, size uint64) gputypes.BindGroupLayoutEntry {
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: stages, Buffer: &gputypes.BufferBindingLayout{Type: kind, MinBindingSize: size}}
}
func layerTextureEntry(binding uint32, integer bool) gputypes.BindGroupLayoutEntry {
	kind := gputypes.TextureSampleTypeFloat
	if integer {
		kind = gputypes.TextureSampleTypeUint
	}
	return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: kind, ViewDimension: gputypes.TextureViewDimension2D}}
}

func (l *Layers) layerLayout(entries []gputypes.BindGroupLayoutEntry) (*wgpu.BindGroupLayout, *wgpu.PipelineLayout, error) {
	b, e := l.d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if e != nil {
		return nil, nil, e
	}
	l.owned = append(l.owned, b.Release)
	p, e := l.d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{b}})
	if e != nil {
		return nil, nil, e
	}
	l.owned = append(l.owned, p.Release)
	return b, p, nil
}

func layerBlend(mode int) *gputypes.BlendState {
	if mode < 0 {
		return nil
	}
	b := &gputypes.BlendState{Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}, Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd}}
	if mode == 1 {
		b.Color.SrcFactor = gputypes.BlendFactorZero
		b.Color.DstFactor = gputypes.BlendFactorSrc
		b.Alpha.SrcFactor = gputypes.BlendFactorZero
		b.Alpha.DstFactor = gputypes.BlendFactorOne
	}
	if mode == 2 {
		b.Color.DstFactor = gputypes.BlendFactorOne
	}
	return b
}

func (l *Layers) layerPipeline(s *wgpu.ShaderModule, layout *wgpu.PipelineLayout, vertex, fragment string, blend int, depth int) (*wgpu.RenderPipeline, error) {
	format := l.format
	if depth == 0 {
		format = l.outputFormat
	}
	d := &wgpu.RenderPipelineDescriptor{Layout: layout, Vertex: wgpu.VertexState{Module: s, EntryPoint: vertex}, Fragment: &wgpu.FragmentState{Module: s, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: format, Blend: layerBlend(blend), WriteMask: gputypes.ColorWriteMaskAll}}}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone}, Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xffffffff}}
	if depth != 0 {
		compare := gputypes.CompareFunctionAlways
		if depth == 2 {
			compare = gputypes.CompareFunctionLessEqual
		}
		d.DepthStencil = &wgpu.DepthStencilState{Format: wgpu.TextureFormatDepth32Float, DepthCompare: compare, DepthWriteEnabled: false}
	}
	p, e := l.d.CreateRenderPipeline(d)
	if e == nil {
		l.owned = append(l.owned, p.Release)
	}
	return p, e
}
func (l *Layers) layerModule(source string) (*wgpu.ShaderModule, error) {
	s, e := l.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: source})
	if e == nil {
		l.owned = append(l.owned, s.Release)
	}
	return s, e
}
func (l *Layers) layerGroup(dst **wgpu.BindGroup, layout *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry) error {
	g, e := l.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: entries})
	if e != nil {
		return e
	}
	if *dst != nil {
		(*dst).Release()
	}
	*dst = g
	return nil
}
func (l *Layers) layerUpload(dst *layerBuffer, data []byte, minimum int) (bool, error) {
	n := max(len(data), minimum)
	changed := false
	if dst.b == nil || dst.size < n {
		size := minimum
		for size < n {
			size *= 2
		}
		if size > 128<<20 {
			return false, layerError("storage exceeds core 128 MiB binding limit")
		}
		b, e := l.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "live-layer-storage", Size: uint64(size), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
		if e != nil {
			return false, e
		}
		dst.close()
		dst.b = b
		dst.size = size
		changed = true
	}
	if len(data) > 0 {
		if e := l.q.WriteBuffer(dst.b, 0, data); e != nil {
			return changed, e
		}
		l.counts.UploadBytes += len(data)
	}
	return changed, nil
}

func NewLayers(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat, scene *meshscene.Scene) (*Layers, error) {
	return newLayersFormats(d, q, format, format, scene)
}
func newLayersFormats(d *wgpu.Device, q *wgpu.Queue, format, outputFormat gputypes.TextureFormat, scene *meshscene.Scene) (l *Layers, err error) {
	l = &Layers{d: d, q: q, format: format, outputFormat: outputFormat, missing: make(map[string]int)}
	defer func() {
		if err != nil {
			l.Close()
		}
	}()
	if d == nil || q == nil || scene == nil {
		return l, layerError("device, queue and scene are required")
	}
	l.rect = scene.TerrainRect
	l.waterStep = scene.WaterMaskStep
	white := meshscene.Texture{Width: 1, Height: 1, RGBA: []byte{255, 255, 255, 255}}
	empty := meshscene.Texture{Width: 1, Height: 1, RGBA: []byte{0, 0, 0, 255}}
	for _, item := range []struct {
		dst   *layerTexture
		src   meshscene.Texture
		label string
	}{
		{&l.terrainImage, scene.Terrain, "terrain-fallback"}, {&l.spriteAtlas, scene.SpriteAtlas, "sprites"}, {&l.spriteDetail, scene.SpriteDetailAtlas, "sprite-detail"}, {&l.overlayAtlas, scene.OverlayAtlas, "hud-atlas"}, {&l.fogAtlas, scene.FogAtlas, "fog-masks"}, {&l.waterMask, scene.WaterMask, "water-mask"},
		{&l.tileAtlas, white, "empty-tiles"}, {&l.tileDetail, white, "empty-detail"}, {&l.lookup, empty, "empty-lookup"}, {&l.effectAtlas, white, "empty-effects"}, {&l.fogGrid, empty, "empty-fog"},
	} {
		src := item.src
		if src.Width == 0 && src.Height == 0 && len(src.RGBA) == 0 {
			src = white
		}
		f := wgpu.TextureFormatRGBA8Unorm
		if item.dst == &l.lookup {
			f = gputypes.TextureFormatR32Uint
		}
		*item.dst, err = l.layerTexture(item.label, src, f)
		if err != nil {
			return
		}
	}
	l.fogAtlasReady = scene.FogAtlas.Width >= 14*64 && scene.FogAtlas.Height >= 513
	if t := scene.TerrainTiles; t != nil {
		if t.Width <= 0 || t.Height <= 0 || t.Width > 8192 || t.Height > 8192 || len(t.Lookup) != t.Width*t.Height || t.Atlas.Width%32 != 0 || t.Atlas.Height%32 != 0 {
			return l, layerError("invalid terrain tile grid")
		}
		for _, id := range t.Lookup {
			if int(id) >= t.TileCount {
				return l, layerError("terrain lookup outside retained tile set")
			}
		}
		l.tileAtlas.close()
		l.tileAtlas, err = l.layerTexture("terrain-native", t.Atlas, wgpu.TextureFormatRGBA8Unorm)
		if err != nil {
			return
		}
		if t.Detail.Width > 0 {
			if t.Detail.Width%64 != 0 || t.Detail.Height%64 != 0 {
				return l, layerError("incomplete detail tile atlas")
			}
			l.tileDetail.close()
			l.tileDetail, err = l.layerTexture("terrain-detail", t.Detail, wgpu.TextureFormatRGBA8Unorm)
			if err != nil {
				return
			}
			l.tileInfo[3] = uint32(t.Detail.Width / 64)
		}
		bytes := make([]byte, len(t.Lookup)*4)
		for i, id := range t.Lookup {
			binary.LittleEndian.PutUint32(bytes[i*4:], id)
		}
		l.lookup.close()
		l.lookup, err = l.layerTexture("terrain-lookup", meshscene.Texture{Width: t.Width, Height: t.Height, RGBA: bytes}, gputypes.TextureFormatR32Uint)
		if err != nil {
			return
		}
		l.rect = t.Rect
		l.tileInfo[0], l.tileInfo[1], l.tileInfo[2] = uint32(t.Width), uint32(t.Height), uint32(t.Atlas.Width/32)
		l.tilesEnabled = true
	}
	l.uniform, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "layer-uniforms", Size: layerUniformSize, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
	if err != nil {
		return
	}
	l.owned = append(l.owned, l.uniform.Release)
	for _, b := range []struct {
		dst *layerBuffer
		n   int
	}{{&l.sprites, 96}, {&l.annotations, 64}, {&l.overlayQuads, 64}, {&l.effectQuads, 64}, {&l.effectVertices, 48}, {&l.effectGround, 80}, {&l.effectSmoke, layerSmokeStride}, {&l.lights, 64}} {
		_, err = l.layerUpload(b.dst, nil, b.n)
		if err != nil {
			return
		}
	}
	stages := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	uniform := layerBufferEntry(0, stages, gputypes.BufferBindingTypeUniform, layerUniformSize)
	var pl *wgpu.PipelineLayout
	var shader *wgpu.ShaderModule
	l.terrainLayout, pl, err = l.layerLayout([]gputypes.BindGroupLayoutEntry{uniform, layerTextureEntry(1, false), layerTextureEntry(2, true), layerTextureEntry(3, false), layerTextureEntry(4, false), layerTextureEntry(5, false), layerTextureEntry(6, false)})
	if err != nil {
		return
	}
	shader, err = l.layerModule(layerTerrainShader)
	if err != nil {
		return
	}
	l.terrain, err = l.layerPipeline(shader, pl, "layer_terrain_vertex", "layer_terrain_fragment", -1, 1)
	if err != nil {
		return
	}
	l.spriteLayout, pl, err = l.layerLayout([]gputypes.BindGroupLayoutEntry{uniform, layerBufferEntry(1, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 96), layerTextureEntry(2, false), layerTextureEntry(3, false)})
	if err != nil {
		return
	}
	shader, err = l.layerModule(layerSpriteShader)
	if err != nil {
		return
	}
	l.sprite, err = l.layerPipeline(shader, pl, "layer_sprite_vertex", "layer_sprite_fragment", 0, 2)
	if err != nil {
		return
	}
	// A feature's production paint entry owns its coverage; scene depth must
	// not replace that order (GPU design C-G3, C-G5; Metal design §4).
	l.feature, err = l.layerPipeline(shader, pl, "layer_sprite_vertex", "layer_sprite_fragment", 0, 1)
	if err != nil {
		return
	}
	l.fogLayout, pl, err = l.layerLayout([]gputypes.BindGroupLayoutEntry{uniform, layerTextureEntry(1, false), layerTextureEntry(2, false), layerTextureEntry(3, false), layerTextureEntry(4, false)})
	if err != nil {
		return
	}
	shader, err = l.layerModule(layerFogShader)
	if err != nil {
		return
	}
	l.fog, err = l.layerPipeline(shader, pl, "layer_fog_vertex", "layer_fog_fragment", -1, 0)
	if err != nil {
		return
	}
	l.overlayLayout, pl, err = l.layerLayout([]gputypes.BindGroupLayoutEntry{uniform, layerBufferEntry(1, stages, gputypes.BufferBindingTypeReadOnlyStorage, 64), layerBufferEntry(2, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 48), layerBufferEntry(3, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 80), layerBufferEntry(4, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, layerSmokeStride), layerBufferEntry(5, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 64), layerTextureEntry(6, false), layerTextureEntry(7, false), layerTextureEntry(8, false), layerTextureEntry(9, false)})
	if err != nil {
		return
	}
	shader, err = l.layerModule(layerOverlayShader)
	if err != nil {
		return
	}
	for i := range l.overlay {
		frag := "layer_quad_fragment"
		if i == 1 {
			frag = "layer_overlay_multiply_fragment"
		}
		l.overlay[i], err = l.layerPipeline(shader, pl, "layer_quad_vertex", frag, i, 0)
		if err != nil {
			return
		}
	}
	for i := range l.fx {
		frag := "layer_quad_fragment"
		if i == 1 {
			frag = "layer_multiply_fragment"
		}
		l.fx[i], err = l.layerPipeline(shader, pl, "layer_quad_vertex", frag, i, 1)
		if err != nil {
			return
		}
	}
	l.geometry, err = l.layerPipeline(shader, pl, "layer_triangle_vertex", "layer_quad_fragment", 0, 1)
	if err != nil {
		return
	}
	l.ground, err = l.layerPipeline(shader, pl, "layer_ground_vertex", "layer_ground_fragment", 0, 1)
	if err != nil {
		return
	}
	l.trail, err = l.layerPipeline(shader, pl, "layer_ground_vertex", "layer_trail_fragment", 1, 1)
	if err != nil {
		return
	}
	l.smoke, err = l.layerPipeline(shader, pl, "layer_smoke_vertex", "layer_smoke_fragment", 0, 1)
	if err != nil {
		return
	}
	l.annotationLayout, pl, err = l.layerLayout([]gputypes.BindGroupLayoutEntry{layerBufferEntry(0, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeUniform, layerUniformSize), layerBufferEntry(1, wgpu.ShaderStageVertex, gputypes.BufferBindingTypeReadOnlyStorage, 64)})
	if err != nil {
		return
	}
	l.annotation, err = l.layerPipeline(shader, pl, "layer_annotation_vertex", "layer_annotation_fragment", 0, 1)
	if err != nil {
		return
	}
	err = l.layerGroup(&l.terrainGroup, l.terrainLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, TextureView: l.tileAtlas.v}, {Binding: 2, TextureView: l.lookup.v}, {Binding: 3, TextureView: l.tileDetail.v}, {Binding: 4, TextureView: l.terrainImage.v}, {Binding: 5, TextureView: l.terrainImage.v}, {Binding: 6, TextureView: l.waterMask.v}})
	if err != nil {
		return
	}
	err = l.layerBindSprites()
	if err != nil {
		return
	}
	err = l.layerBindAnnotations()
	if err != nil {
		return
	}
	err = l.layerBindOverlay(false)
	if err != nil {
		return
	}
	err = l.layerBindOverlay(true)
	l.counts = LayerCounters{}
	return
}

func (l *Layers) layerBindSprites() error {
	return l.layerGroup(&l.spriteGroup, l.spriteLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, Buffer: l.sprites.b}, {Binding: 2, TextureView: l.spriteAtlas.v}, {Binding: 3, TextureView: l.spriteDetail.v}})
}
func (l *Layers) layerBindAnnotations() error {
	return l.layerGroup(&l.annotationGroup, l.annotationLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, Buffer: l.annotations.b}})
}
func (l *Layers) layerBindOverlay(effects bool) error {
	quads, atlas, dst := l.overlayQuads.b, l.overlayAtlas.v, &l.overlayGroup
	if effects {
		quads, atlas, dst = l.effectQuads.b, l.effectAtlas.v, &l.effectGroup
	}
	return l.layerGroup(dst, l.overlayLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, Buffer: quads}, {Binding: 2, Buffer: l.effectVertices.b}, {Binding: 3, Buffer: l.effectGround.b}, {Binding: 4, Buffer: l.effectSmoke.b}, {Binding: 5, Buffer: l.lights.b}, {Binding: 6, TextureView: atlas}, {Binding: 7, TextureView: l.terrainImage.v}, {Binding: 8, TextureView: l.fogGrid.v}, {Binding: 9, TextureView: l.waterMask.v}})
}

// SetWorldTexture borrows the completed world image. DrawFog reads it in a
// DIFFERENT attachment, including the plain world copy when fog is disabled.
func (l *Layers) SetWorldTexture(view *wgpu.TextureView) error {
	if l.closed || view == nil {
		return layerError("completed world texture required")
	}
	l.world = view
	return l.layerGroup(&l.fogGroup, l.fogLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, TextureView: view}, {Binding: 2, TextureView: l.fogGrid.v}, {Binding: 3, TextureView: l.fogAtlas.v}, {Binding: 4, TextureView: l.layerGlowTexture()}})
}

// SetFields borrows component outputs after the host completion boundary.
// Only view replacement rebinds; frame flags/active extents live in uniforms.
func (l *Layers) SetFields(glow, ground *wgpu.TextureView, glowValues, rect, values [4]float32) error {
	l.glowValues, l.groundLightRect, l.groundLightValues = glowValues, rect, values
	if l.glowView != glow {
		l.glowView = glow
		if l.world != nil {
			if err := l.SetWorldTexture(l.world); err != nil {
				return err
			}
		}
	}
	if l.groundLightView != ground {
		l.groundLightView = ground
		if ground == nil {
			ground = l.terrainImage.v
		}
		return l.layerGroup(&l.terrainGroup, l.terrainLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, TextureView: l.tileAtlas.v}, {Binding: 2, TextureView: l.lookup.v}, {Binding: 3, TextureView: l.tileDetail.v}, {Binding: 4, TextureView: l.terrainImage.v}, {Binding: 5, TextureView: ground}, {Binding: 6, TextureView: l.waterMask.v}})
	}
	return nil
}
func (l *Layers) layerGlowTexture() *wgpu.TextureView {
	if l.glowView != nil {
		return l.glowView
	}
	return l.terrainImage.v
}

// Adjacent instances can share one draw only within the same ordered stream.
// Snapshot and retained-model sentinels remain barriers even when not drawn.
func layerAppendDraw(out []layerDraw, run layerDraw) []layerDraw {
	if len(out) > 0 && run.count > 0 {
		previous := &out[len(out)-1]
		if previous.count > 0 && previous.kind == run.kind && previous.mode == run.mode && previous.first+previous.count == run.first {
			previous.count += run.count
			return out
		}
	}
	return append(out, run)
}
func layerRunsInto(out []layerDraw, quads []meshscene.OverlayQuad, kind, base uint32) []layerDraw {
	for at := 0; at < len(quads); {
		mode := uint32(quads[at].Params[1])
		end := at + 1
		for end < len(quads) && uint32(quads[end].Params[1]) == mode {
			end++
		}
		out = layerAppendDraw(out, layerDraw{kind: kind, first: base + uint32(at), count: uint32(end - at), mode: mode})
		at = end
	}
	return out
}
func layerRuns(quads []meshscene.OverlayQuad, kind uint32) []layerDraw {
	return layerRunsInto(nil, quads, kind, 0)
}
func layerBytes(dst []byte, size int) []byte {
	dst = slices.Grow(dst[:0], size)[:size]
	clear(dst)
	return dst
}

type layerSpriteEntry struct {
	index int
	depth float32
}

func layerOrderSprites(f meshscene.LiveFrame) ([]meshscene.Sprite, [2][2]uint32, []uint32) {
	return new(Layers).layerOrderSprites(f)
}
func (l *Layers) layerOrderSprites(f meshscene.LiveFrame) ([]meshscene.Sprite, [2][2]uint32, []uint32) {
	out := l.orderedSprites[:0]
	remap := slices.Grow(l.spriteRemap[:0], len(f.Sprites))[:len(f.Sprites)]
	clear(remap)
	upper := l.upperSprites[:0]
	for i, s := range f.Sprites {
		// The stock recorder replaces fallback effects, rather than painting
		// the older art adapter's approximation a second time.
		if f.StockEffects != nil && i < len(f.Composition.SpriteSources) && f.Composition.SpriteSources[i].Kind == meshscene.SpriteSourceFallbackEffect {
			continue
		}
		if s.Current[3] > .5 {
			remap[i] = uint32(len(out)) + 1
			out = append(out, s)
			continue
		}
		y := s.Previous[1] + (s.Current[1]-s.Previous[1])*f.Alpha
		z := s.Previous[2] + (s.Current[2]-s.Previous[2])*f.Alpha
		depth := min(max(float32(.5)-((z-f.Camera[1])+2*y)*float32(.00001), float32(.001)), float32(.999))
		depth = min(max(depth+s.Params[3], float32(.0001)), float32(.9998))
		upper = append(upper, layerSpriteEntry{i, depth})
	}
	ground := uint32(len(out))
	slices.SortStableFunc(upper, func(a, b layerSpriteEntry) int { return cmp.Compare(b.depth, a.depth) })
	for _, e := range upper {
		remap[e.index] = uint32(len(out)) + 1
		out = append(out, f.Sprites[e.index])
	}
	l.orderedSprites, l.upperSprites, l.spriteRemap = out, upper, remap
	return out, [2][2]uint32{{0, ground}, {ground, uint32(len(out)) - ground}}, remap
}

// Source identities stay in publication coordinates, including original source
// zero. The loose spans index the packed array and exclude every retained paint
// reference, even when a body/shadow pair was separated by depth sorting.
func layerFeatureLayout(remap []uint32, bands [2][2]uint32, paint []meshscene.FeatureSpritePaint) ([]bool, [2][]layerDraw) {
	features := make([]bool, len(remap))
	packedFeatures := make([]bool, bands[0][1]+bands[1][1])
	for _, p := range paint {
		if uint64(p.Source) >= uint64(len(remap)) || remap[p.Source] == 0 {
			continue
		}
		features[p.Source] = true
		packedFeatures[remap[p.Source]-1] = true
	}
	var runs [2][]layerDraw
	for phase, band := range bands {
		for at, end := band[0], band[0]+band[1]; at < end; {
			if packedFeatures[at] {
				at++
				continue
			}
			first := at
			for at < end && !packedFeatures[at] {
				at++
			}
			runs[phase] = append(runs[phase], layerDraw{first: first, count: at - first})
		}
	}
	return features, runs
}

// SetFeaturePaintOrder opts into the coordinator's Phase/Rank paint walk.
// Configure it before the frame's first draw. DrawSprites then draws only loose
// art; DrawFeature supplies each production feature body/shadow at its barrier.
// The bulk fallback keeps its explicit painter-order limitation by default.
func (l *Layers) SetFeaturePaintOrder(enabled bool) {
	l.featurePaintOrder = enabled
	if enabled {
		delete(l.missing, "feature_subject_painter_order")
	} else if l.featurePaintCount > 0 {
		l.missing["feature_subject_painter_order"] = l.featurePaintCount
	}
}

func (l *Layers) layerTakeFeatureSource(originalSource uint32) (uint32, bool) {
	if !l.featurePaintOrder || uint64(originalSource) >= uint64(len(l.spriteRemap)) || !l.featureSources[originalSource] || l.featureDrawn[originalSource] {
		return 0, false
	}
	packed := l.spriteRemap[originalSource]
	if packed == 0 {
		return 0, false
	}
	l.featureDrawn[originalSource] = true
	return packed - 1, true
}

func (l *Layers) Prepare(f meshscene.LiveFrame, width, height int) error {
	if l.closed {
		return layerError("closed")
	}
	if width <= 0 || height <= 0 || !(f.Camera[2] > 0) {
		return layerError("positive viewport and camera zoom required")
	}
	l.counts = LayerCounters{}
	clear(l.missing)
	layerVec(l.uniformBytes[:], 0, [4]float32{f.Camera[0], f.Camera[1], f.Camera[2], min(max(f.Alpha, 0), 1)})
	layerVec(l.uniformBytes[:], 16, [4]float32{float32(width), float32(height), f.WorldGain, 1})
	layerVec(l.uniformBytes[:], 32, l.rect)
	layerVec(l.uniformBytes[:], 48, f.Fog.Rect)
	for i, x := range l.tileInfo {
		binary.LittleEndian.PutUint32(l.uniformBytes[64+i*4:], x)
	}
	clear(l.uniformBytes[80:96])
	binary.LittleEndian.PutUint32(l.uniformBytes[80:], f.TerrainFlags)
	if l.tilesEnabled {
		binary.LittleEndian.PutUint32(l.uniformBytes[84:], 1)
	}
	if len(f.Fog.RGBA) > 0 {
		if !l.fogAtlasReady {
			return layerError("fog grid requires authored mask atlas")
		}
		binary.LittleEndian.PutUint32(l.uniformBytes[88:], 1)
	}
	layerVec(l.uniformBytes[:], 96, [4]float32{float32(l.waterStep), drawlist.ScorchFadeStartTicks, drawlist.ScorchLifeTicks, f.Alpha})
	layerVec(l.uniformBytes[:], 112, l.glowValues)
	layerVec(l.uniformBytes[:], 128, l.groundLightRect)
	layerVec(l.uniformBytes[:], 144, l.groundLightValues)
	for i, v := range [][4]float32{f.Water.Phase, f.Water.Mask, f.Water.Controls, f.Water.TerrainRect, f.Water.SampleRect} {
		layerVec(l.uniformBytes[:], 160+i*16, v)
	}
	sprites, bands, remap := l.layerOrderSprites(f)
	l.spriteBands = bands
	l.spriteRemap = remap
	l.featureSources, l.looseSpriteRuns = layerFeatureLayout(remap, bands, f.Composition.FeatureSprites)
	l.featureDrawn = slices.Grow(l.featureDrawn[:0], len(remap))[:len(remap)]
	clear(l.featureDrawn)
	l.featurePaintCount = 0
	if f.Composed {
		l.featurePaintCount = len(f.Composition.FeatureSprites)
	}
	l.spriteBytes = layerBytes(l.spriteBytes, len(sprites)*96)
	data := l.spriteBytes
	for i, s := range sprites {
		for j, v := range [][4]float32{s.Previous, s.Current, s.Rect, s.UV, s.Color, s.Params} {
			layerVec(data, i*96+j*16, v)
		}
	}
	changed, err := l.layerUpload(&l.sprites, data, 96)
	if err != nil {
		return err
	}
	if changed {
		if err = l.layerBindSprites(); err != nil {
			return err
		}
	}
	data, l.annotationRanges = layerPackAnnotations(f.Annotations, len(f.Composition.Slots))
	changed, err = l.layerUpload(&l.annotations, data, 64)
	if err != nil {
		return err
	}
	if changed {
		if err = l.layerBindAnnotations(); err != nil {
			return err
		}
	}
	fogChanged := false
	if len(f.Fog.RGBA) > 0 {
		fogChanged, err = l.layerUpdateTexture(&l.fogGrid, meshscene.Texture{Width: f.Fog.Width, Height: f.Fog.Height, RGBA: f.Fog.RGBA}, f.Fog.Generation, &l.fogGeneration, [4]uint32{})
		if err != nil {
			return err
		}
	}
	overlayChanged := false
	if f.OverlayVersion != 0 {
		overlayChanged, err = l.layerUpdateTexture(&l.overlayAtlas, f.OverlayTexture, f.OverlayVersion, &l.overlayVersion, f.OverlayDirty)
		if err != nil {
			return err
		}
	}
	l.overlayBytes = layerBytes(l.overlayBytes, len(f.Overlay)*64)
	data = l.overlayBytes
	for i, q := range f.Overlay {
		layerPackQuad(data[i*64:], q, 1)
	}
	l.overlayRuns = layerRunsInto(l.overlayRuns[:0], f.Overlay, meshscene.EffectOpQuads, 0)
	changed, err = l.layerUpload(&l.overlayQuads, data, 64)
	if err != nil {
		return err
	}
	overlayChanged = overlayChanged || changed
	effectChanged, err := l.layerPrepareEffects(f)
	if err != nil {
		return err
	}
	if fogChanged || effectChanged || overlayChanged {
		if err = l.layerBindOverlay(false); err != nil {
			return err
		}
		if err = l.layerBindOverlay(true); err != nil {
			return err
		}
	}
	if fogChanged && l.world != nil {
		if err = l.SetWorldTexture(l.world); err != nil {
			return err
		}
	}
	if err = l.q.WriteBuffer(l.uniform, 0, l.uniformBytes[:]); err != nil {
		return err
	}
	l.counts.UploadBytes += layerUniformSize
	if len(f.Distortions) > 0 {
		l.missing["enhanced_distortion"] = len(f.Distortions)
	}
	if !l.featurePaintOrder && l.featurePaintCount > 0 {
		l.missing["feature_subject_painter_order"] = l.featurePaintCount
	}
	if f.Water.Mask[3] > .5 && f.Water.Controls[3] > .5 {
		l.missing["water_reflections_and_model_refraction"] = 1
	}
	for _, light := range f.Lighting.Lights {
		if light.Flags[1] != 0 && light.ColorGroundGain[3] > 0 && l.groundLightValues[2] == 0 {
			l.missing["bounded_terrain_lighting"]++
		}
	}
	return nil
}

// Production's stable eight-source selection: equal scores retain earlier
// sources; replacement uses the first least score (GPU design §23.2).
func layerSelectSmoke(lights []meshscene.RetainedLight, center [2]float32, extent float32) (indices [8]uint32, count uint32) {
	var scores [8]float32
	for j, light := range lights {
		if light.Flags[0] == 0 {
			continue
		}
		x, y := light.PositionRadius[0]-center[0], light.PositionRadius[1]-light.PositionRadius[2]*.5-center[1]
		r := light.PositionRadius[3]
		reach := r*1.5 + extent
		d2 := x*x + y*y
		if d2 > reach*reach || r <= 0 {
			continue
		}
		score := max(light.ColorGroundGain[0], light.ColorGroundGain[1], light.ColorGroundGain[2]) / (1 + d2/(r*r))
		at := count
		if at == 8 {
			at = 0
			for i := uint32(1); i < count; i++ {
				if scores[i] < scores[at] {
					at = i
				}
			}
			if score <= scores[at] {
				continue
			}
		} else {
			count++
		}
		indices[at] = uint32(j)
		scores[at] = score
	}
	return
}

func (l *Layers) layerPrepareEffects(f meshscene.LiveFrame) (changed bool, err error) {
	qb, vb, gb, sb, lb := l.effectBytes[0][:0], l.effectBytes[1][:0], l.effectBytes[2][:0], l.effectBytes[3][:0], l.effectBytes[4][:0]
	defer func() { l.effectBytes = [5][]byte{qb, vb, gb, sb, lb} }()
	if len(f.Lighting.Lights) > 64 {
		return false, layerError("production lighting exceeds shared 64-source budget")
	}
	for _, light := range f.Lighting.Lights {
		at := len(lb)
		lb = append(lb, make([]byte, 64)...)
		layerVec(lb, at, light.PositionRadius)
		layerVec(lb, at+16, light.ColorGroundGain)
		layerVec(lb, at+32, light.GroundAgeFadeKind)
		for i, x := range light.Flags {
			binary.LittleEndian.PutUint32(lb[at+48+i*4:], x)
		}
	}
	stock := f.StockEffects
	bodySlot := 0
	if stock != nil {
		scale := stock.Scale
		if !(scale > 0) || math.IsInf(float64(scale), 0) {
			return false, layerError("valid stock presentation scale required")
		}
		changed, err = l.layerUpdateTexture(&l.effectAtlas, stock.Atlas, stock.Version, &l.effectVersion, stock.Dirty)
		if err != nil {
			return
		}
		if len(stock.Glow.Vertices) > 0 && l.glowValues[2] == 0 {
			l.missing["enhanced_glow"] = len(stock.Glow.Vertices) / 3
		}
		for layerIndex, src := range stock.Layers {
			qbase, gbase, sbase := len(qb)/64, len(gb)/80, len(sb)/layerSmokeStride
			for _, q := range src.Quads {
				// A normalized fallback cannot retain production HDR stream range
				// (GPU design §13.3). The combined renderer owns an RGBA16F world.
				if l.format != gputypes.TextureFormatRGBA16Float && (q.Params[1] == 2 || q.Params[2] > .5) {
					l.missing["normalized_effect_accumulation"]++
				}
				at := len(qb)
				qb = append(qb, make([]byte, 64)...)
				layerPackQuad(qb[at:], q, scale)
			}
			l.geometrySpans = slices.Grow(l.geometrySpans[:0], len(src.Models))[:len(src.Models)]
			clear(l.geometrySpans)
			spans := l.geometrySpans
			for i, m := range src.Models {
				if m.Suppressed != "" {
					l.missing["source_suppressed_geometry"]++
					continue
				}
				if len(m.Vertices)%3 != 0 {
					return false, layerError("effect geometry must contain whole triangles")
				}
				spans[i] = layerDraw{kind: meshscene.EffectOpGeometry, first: uint32(len(vb) / 48), count: uint32(len(m.Vertices))}
				for _, v := range m.Vertices {
					v.PositionUV[0] *= scale
					v.PositionUV[1] *= scale
					at := len(vb)
					vb = append(vb, make([]byte, 48)...)
					layerVec(vb, at, v.PositionUV)
					layerVec(vb, at+16, v.Color)
					layerVec(vb, at+32, v.Bounds)
				}
			}
			for _, g := range src.Ground {
				for i := range g.CentreAxis {
					g.CentreAxis[i] *= scale
				}
				g.CrossKind[0] *= scale
				g.CrossKind[1] *= scale
				g.Mapping[2] /= scale
				for i := range g.Clip {
					g.Clip[i] *= scale
				}
				if g.CrossKind[2] == 1 && g.Values[3] == 0 {
					whole := float32(math.Round(float64(g.Values[0] - f.Alpha)))
					if whole+f.Alpha == g.Values[0] {
						g.Values[0], g.Values[3] = whole, 1
					}
				}
				at := len(gb)
				gb = append(gb, make([]byte, 80)...)
				for i, v := range [][4]float32{g.CentreAxis, g.CrossKind, g.Values, g.Mapping, g.Clip} {
					layerVec(gb, at+i*16, v)
				}
			}
			for _, s := range src.Smoke {
				for i := range s.Selection {
					s.Selection[i] *= scale
				}
				s.Quad.Params[1] *= scale
				s.Quad.Params[2] *= scale
				at := len(sb)
				sb = append(sb, make([]byte, layerSmokeStride)...)
				layerPackQuad(sb[at:], s.Quad, scale)
				layerVec(sb, at+64, s.Selection)
				layerVec(sb, at+80, s.Bounds)
				indices, count := layerSelectSmoke(f.Lighting.Lights, [2]float32{s.Selection[0], s.Selection[1]}, s.Selection[2])
				for i, x := range indices {
					binary.LittleEndian.PutUint32(sb[at+96+i*4:], x)
				}
				binary.LittleEndian.PutUint32(sb[at+128:], count)
			}
			if layerIndex >= len(l.effectLayers) {
				l.effectLayers = append(l.effectLayers, nil)
			}
			ops := l.effectLayers[layerIndex][:0]
			l.missing["source_unsupported_effects"] += src.Counts.Unsupported
			for _, op := range src.Ops {
				bound := 0
				switch op.Kind {
				case meshscene.EffectOpQuads:
					bound = len(src.Quads)
				case meshscene.EffectOpGeometry:
					bound = len(src.Models)
				case meshscene.EffectOpGround:
					bound = len(src.Ground)
				case meshscene.EffectOpSmoke:
					bound = len(src.Smoke)
				case meshscene.EffectOpLens:
					bound = len(src.Lenses)
				case meshscene.EffectOpRetainedModel:
					bound = len(src.RetainedModels)
				default:
					return false, layerError("unknown stock operation %d", op.Kind)
				}
				if op.First < 0 || op.Count < 0 || op.First > bound || op.Count > bound-op.First {
					return false, layerError("stock operation %d exceeds its layer", op.Kind)
				}
				switch op.Kind {
				case meshscene.EffectOpQuads:
					ops = layerRunsInto(ops, src.Quads[op.First:op.First+op.Count], op.Kind, uint32(qbase+op.First))
				case meshscene.EffectOpGeometry:
					ops = append(ops, spans[op.First:op.First+op.Count]...)
				case meshscene.EffectOpGround:
					for i := op.First; i < op.First+op.Count; i++ {
						mode := uint32(0)
						if src.Ground[i].CrossKind[2] < .5 {
							mode = 1
						}
						ops = append(ops, layerDraw{kind: op.Kind, first: uint32(gbase + i), count: 1, mode: mode})
					}
				case meshscene.EffectOpSmoke:
					ops = append(ops, layerDraw{kind: op.Kind, first: uint32(sbase + op.First), count: uint32(op.Count)})
				case meshscene.EffectOpLens:
					if l.lensPaintOrder {
						for i := op.First; i < op.First+op.Count; i++ {
							ops = append(ops, layerDraw{kind: op.Kind, first: uint32(i), count: 1})
						}
					} else {
						ops = append(ops, layerDraw{kind: op.Kind})
						l.missing["lens_snapshot"] += op.Count
					}
				case meshscene.EffectOpRetainedModel:
					if !l.projectilePaintOrder {
						ops = append(ops, layerDraw{kind: op.Kind})
						l.missing["retained_projectile_marker_order"] += op.Count
						continue
					}
					if len(src.ProjectileOrder) != len(src.RetainedModels) {
						return false, layerError("projectile identities aligned to retained markers")
					}
					for i := op.First; i < op.First+op.Count; i++ {
						identity := src.ProjectileOrder[i]
						if identity.ID == 0 || identity.Member > 1 {
							return false, layerError("valid projectile identity/member")
						}
						marker := layerDraw{kind: op.Kind, count: 1, mode: identity.Member}
						if src.RetainedModels[i].ShadowOnly {
							l.missing["retained_projectile_shadow_marker"]++
							marker.mode = 2
						} else {
							if bodySlot >= len(f.Composition.ProjectileSlots) {
								return false, layerError("retained projectile body marker within composition slots")
							}
							marker.first = f.Composition.ProjectileSlots[bodySlot]
							if int(marker.first) > len(f.Composition.Slots) {
								return false, layerError("projectile body slot within final atlas")
							}
							bodySlot++
						}
						ops = append(ops, marker)
					}
				}
			}
			l.effectLayers[layerIndex] = ops
		}
	}
	if stock != nil && l.projectilePaintOrder && bodySlot != len(f.Composition.ProjectileSlots) {
		return false, layerError("all composed projectile body markers consumed")
	}
	if stock == nil {
		l.effectLayers = l.effectLayers[:0]
	} else {
		l.effectLayers = l.effectLayers[:len(stock.Layers)]
	}
	for _, item := range []struct {
		dst  *layerBuffer
		data []byte
		min  int
	}{{&l.effectQuads, qb, 64}, {&l.effectVertices, vb, 48}, {&l.effectGround, gb, 80}, {&l.effectSmoke, sb, layerSmokeStride}, {&l.lights, lb, 64}} {
		var c bool
		c, err = l.layerUpload(item.dst, item.data, item.min)
		changed = changed || c
		if err != nil {
			return
		}
	}
	return
}

func (l *Layers) DrawTerrain(pass *wgpu.RenderPassEncoder) {
	if l.closed {
		return
	}
	l.state.selectResources(pass, l.terrain, l.terrainGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1})
	l.counts.Draws++
}
func (l *Layers) DrawSprites(pass *wgpu.RenderPassEncoder, phase uint32) {
	if l.closed || phase > 1 {
		return
	}
	if l.featurePaintOrder {
		l.DrawLooseSprites(pass, phase)
		return
	}
	s := l.spriteBands[phase]
	if s[1] == 0 {
		return
	}
	l.state.selectResources(pass, l.sprite, l.spriteGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: s[1], FirstInstance: s[0]})
	l.counts.Draws++
	l.counts.Sprites += int(s[1])
}

// DrawFeature accepts an ORIGINAL publication source, never a private packed
// index. The coordinator calls it in FeatureSpritePaint.Phase/Rank order,
// shadow before body; source filtering/reordering cannot change that identity.
// False means the source is absent, not a feature paint, already drawn, or the
// explicit feature paint mode is disabled.
func (l *Layers) DrawFeature(pass *wgpu.RenderPassEncoder, originalSource uint32) bool {
	if l.closed || pass == nil {
		return false
	}
	packed, ok := l.layerTakeFeatureSource(originalSource)
	if !ok {
		return false
	}
	l.state.selectResources(pass, l.feature, l.spriteGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: packed})
	l.counts.Draws++
	l.counts.Sprites++
	return true
}

// DrawLooseSprites uses the original two bands (0 short, 1 upper), excluding
// all valid FeatureSprites references so the coordinator paints them once.
func (l *Layers) DrawLooseSprites(pass *wgpu.RenderPassEncoder, phase uint32) {
	if l.closed || pass == nil || phase > 1 {
		return
	}
	l.state.selectResources(pass, l.sprite, l.spriteGroup)
	for _, span := range l.looseSpriteRuns[phase] {
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: span.count, FirstInstance: span.first})
		l.counts.Draws++
		l.counts.Sprites += int(span.count)
	}
}

// DrawAnnotations consumes the producer's one-based slot records verbatim.
// The coordinator calls it after that slot's shadow, immediately before its
// model body, so annotation coverage follows the composition paint order.
func (l *Layers) DrawAnnotations(pass *wgpu.RenderPassEncoder, originalSlot uint32) {
	if l.closed || pass == nil || originalSlot == 0 || uint64(originalSlot) > uint64(len(l.annotationRanges)) {
		return
	}
	r := l.annotationRanges[originalSlot-1]
	if r[1] == 0 {
		return
	}
	l.state.selectResources(pass, l.annotation, l.annotationGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: r[1], FirstInstance: r[0]})
	l.counts.Draws++
}

// DrawEffects replays a production barrier layer (0..4), preserving spans and
// blend stream order. Unsupported snapshot/model operations appear in Missing.
func (l *Layers) DrawEffects(pass *wgpu.RenderPassEncoder, layer uint32) {
	l.DrawEffectsOrdered(pass, layer, nil)
}

// The parent marker commits its combined retained slot once. The header-child
// marker consumes its identity without a second body commit [03 §5.4].
func (l *Layers) DrawEffectsOrdered(pass *wgpu.RenderPassEncoder, layer uint32, model func(uint32)) {
	_, _ = l.drawEffectsReplay(pass, layer, model, nil)
}

// A lens replaces the encoder at its snapshot barrier. Every subsequent
// operation consumes the resumed pass, preserving the stock painter order.
func (l *Layers) drawEffectsReplay(pass *wgpu.RenderPassEncoder, layer uint32, model func(uint32), lens func(uint32) (*wgpu.RenderPassEncoder, error)) (*wgpu.RenderPassEncoder, error) {
	if l.closed || int(layer) >= len(l.effectLayers) {
		return pass, nil
	}

	for _, op := range l.effectLayers[layer] {
		if op.kind == meshscene.EffectOpLens {
			if op.count != 0 {
				if lens == nil {
					l.missing["lens_snapshot"]++
				} else {
					next, err := lens(op.first)
					if err != nil {
						return pass, err
					}
					pass = next
				}
			}
			continue
		}
		if op.kind == meshscene.EffectOpRetainedModel {
			if op.mode == 0 && op.first != 0 && op.count != 0 {
				if model == nil {
					l.missing["retained_projectile_marker_order"]++
				} else {
					model(op.first)
				}
			}
			continue
		}
		if op.count == 0 {
			continue
		}
		p := l.fx[min(op.mode, 2)]
		switch op.kind {
		case meshscene.EffectOpGeometry:
			p = l.geometry
		case meshscene.EffectOpGround:
			p = l.ground
			if op.mode == 1 {
				p = l.trail
			}
		case meshscene.EffectOpSmoke:
			p = l.smoke
		}
		l.state.selectResources(pass, p, l.effectGroup)
		args := gputypes.DrawArgs{VertexCount: 6, InstanceCount: op.count, FirstInstance: op.first}
		if op.kind == meshscene.EffectOpGeometry {
			args = gputypes.DrawArgs{VertexCount: op.count, InstanceCount: 1, FirstVertex: op.first}
		}
		pass.Draw(args)
		l.counts.Draws++
	}
	return pass, nil
}
func (l *Layers) DrawFog(pass *wgpu.RenderPassEncoder) {
	if l.closed || l.fogGroup == nil {
		l.missing["completed_world_texture"] = 1
		return
	}
	l.state.selectResources(pass, l.fog, l.fogGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	l.counts.Draws++
}
func (l *Layers) DrawOverlay(pass *wgpu.RenderPassEncoder) {
	if l.closed {
		return
	}
	for _, op := range l.overlayRuns {
		l.state.selectResources(pass, l.overlay[min(op.mode, 1)], l.overlayGroup)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: op.count, FirstInstance: op.first})
		l.counts.Draws++
		l.counts.Overlay += int(op.count)
	}
}
func (l *Layers) Counters() LayerCounters { return l.counts }

// Missing reports current-frame operations without silently declaring parity.
func (l *Layers) Missing() map[string]int {
	out := make(map[string]int, len(l.missing))
	for k, v := range l.missing {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}
func (l *Layers) Close() {
	if l == nil || l.closed {
		return
	}
	l.closed = true
	for _, g := range []*wgpu.BindGroup{l.terrainGroup, l.spriteGroup, l.annotationGroup, l.fogGroup, l.overlayGroup, l.effectGroup} {
		if g != nil {
			g.Release()
		}
	}
	for _, b := range []*layerBuffer{&l.sprites, &l.annotations, &l.overlayQuads, &l.effectQuads, &l.effectVertices, &l.effectGround, &l.effectSmoke, &l.lights} {
		b.close()
	}
	for _, t := range []*layerTexture{&l.terrainImage, &l.tileAtlas, &l.tileDetail, &l.lookup, &l.spriteAtlas, &l.spriteDetail, &l.fogAtlas, &l.waterMask, &l.overlayAtlas, &l.effectAtlas, &l.fogGrid} {
		t.close()
	}
	for i := len(l.owned) - 1; i >= 0; i-- {
		l.owned[i]()
	}
	l.owned = nil
	l.world = nil
}
