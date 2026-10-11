package battlegpu

import (
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestSeabedPublishedOrderSurvivesFilteringAndPrivatePacking(t *testing.T) {
	f := meshscene.LiveFrame{
		Composed: true, Alpha: 1, StockEffects: &meshscene.StockFrame{},
		Water: meshscene.WaterUniform{Mask: [4]float32{1, 64, 64, 1}},
		Sprites: []meshscene.Sprite{
			{Current: [4]float32{1, 0, 30, 0}}, // original body source zero
			{Current: [4]float32{2, 0, 0, 0}},  // replaced fallback effect
			{Current: [4]float32{3, 0, 0, 1}},  // ordinary short feature
			{Current: [4]float32{4, 0, 0, 1}},  // loose short art
			{Current: [4]float32{5, 0, 10, 0}}, // loose upper art
			{Current: [4]float32{6, 0, 20, 0}}, // shadow separated by depth sorting
		},
		Composition: meshscene.ModelComposition{FeatureSprites: []meshscene.FeatureSpritePaint{
			{Source: 5, Rank: 9, Phase: 2, Shadow: true, Submerged: true},
			{Source: 0, Rank: 9, Phase: 2, Submerged: true},
			{Source: 2, Rank: 0},
			{Source: 1, Submerged: true}, // filtered out of private packing
			{Source: math.MaxUint32, Submerged: true},
			{Source: 0, Submerged: true}, // defensive duplicate consumes no second draw
		}, SpriteSources: make([]meshscene.SpriteSource, 6)},
	}
	f.Composition.SpriteSources[1].Kind = meshscene.SpriteSourceFallbackEffect
	published := slices.Clone(f.Sprites)
	paint := slices.Clone(f.Composition.FeatureSprites)
	packed, bands, remap := layerOrderSprites(f)
	features, loose := layerFeatureLayout(remap, bands, f.Composition.FeatureSprites)
	sources := layerSeabedSources(nil, f, remap)
	if !slices.Equal(sources, []uint32{5, 0, 0}) {
		t.Fatalf("published seabed sequence changed: %v", sources)
	}
	if !slices.Equal(published, f.Sprites) || !slices.Equal(paint, f.Composition.FeatureSprites) {
		t.Fatal("publication changed while deriving seabed")
	}
	f.Composition.FeatureSprites[0].Source = 2
	if sources[0] != 5 {
		t.Fatal("retained a mutable publication reference")
	}
	l := Layers{spriteRemap: remap, featureSources: features, featureDrawn: make([]bool, len(remap)), featurePaintOrder: true}
	draws := make([]int, len(packed))
	for i, source := range sources {
		at, ok := l.layerTakeSeabedSource(source)
		if i == 2 {
			if ok {
				t.Fatal("seabed duplicated a source")
			}
			continue
		}
		if !ok || at != []uint32{3, 4}[i] {
			t.Fatal("seabed changed source identity", source, at, ok)
		}
		draws[at]++
	}
	for _, source := range []uint32{5, 0} {
		if _, ok := l.layerTakeFeatureSource(source); ok {
			t.Fatal("ordinary paint duplicated promoted source", source)
		}
	}
	if at, ok := l.layerTakeFeatureSource(2); !ok || at != 0 {
		t.Fatal("ordinary feature disappeared", at, ok)
	} else {
		draws[at]++
	}
	for _, phase := range loose {
		for _, span := range phase {
			for at := span.first; at < span.first+span.count; at++ {
				draws[at]++
			}
		}
	}
	for _, count := range draws {
		if count != 1 {
			t.Fatalf("ordered replay lost or duplicated art: %v", draws)
		}
	}
	// Bulk replay is supported independently of the explicit feature walk.
	clear(l.featureDrawn)
	l.featurePaintOrder = false
	for _, source := range sources {
		l.layerTakeSeabedSource(source)
	}
	clear(draws)
	for source, drawn := range l.featureDrawn {
		if drawn {
			draws[remap[source]-1]++
		}
	}
	remaining := layerSeabedRemaining(remap, bands, l.featureDrawn)
	for _, phase := range remaining {
		for _, span := range phase {
			for at := span.first; at < span.first+span.count; at++ {
				draws[at]++
			}
		}
	}
	for _, count := range draws {
		if count != 1 {
			t.Fatalf("bulk replay lost or duplicated art: %v", draws)
		}
	}
}

func TestSeabedInactiveSurfaceKeepsOrdinaryReplayAndNoWork(t *testing.T) {
	f := meshscene.LiveFrame{Composed: true, Composition: meshscene.ModelComposition{FeatureSprites: []meshscene.FeatureSpritePaint{{Submerged: true}}}}
	for _, active := range []float32{0, .5, 1} {
		f.Water.Mask[3] = active
		sources := layerSeabedSources(nil, f, []uint32{1})
		if (active > .5) != (len(sources) == 1) {
			t.Fatal("surface admission boundary changed", active, sources)
		}
	}
	f.Composed = false
	if len(layerSeabedSources(nil, f, []uint32{1})) != 0 {
		t.Fatal("uncomposed source promoted")
	}
	l := new(Layers)
	if err := l.layerPrepareSeabed(f, 8193, 2); err != nil {
		t.Fatal("inactive bed allocated or validated resources", err)
	}
	if err := l.EncodeSeabed(nil, nil); err != nil {
		t.Fatal("inactive bed required a pass", err)
	}
	if l.counts != (LayerCounters{}) || binary.LittleEndian.Uint32(l.uniformBytes[92:96]) != 0 {
		t.Fatal("inactive bed emitted work or changed flags", l.counts)
	}
	if texture, width, height := l.DebugSeabedResource(); texture != nil || width != 0 || height != 0 {
		t.Fatal("inactive bed exposed stale resource")
	}
	l.seabed.active = true
	if err := l.EncodeSeabed(nil, nil); err == nil {
		t.Fatal("active bed accepted absent attachments")
	}
}

func TestSeabedCoreABIAndNativeSamplingPath(t *testing.T) {
	ast, err := naga.Parse(layerTerrainShader)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.LowerWithCapabilities(ast, layerTerrainShader, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := naga.Compile(layerTerrainShader); err != nil {
		t.Fatal(err)
	}
	for _, typ := range module.Types {
		if typ.Name == "LayerUniform" {
			uniform := typ.Inner.(ir.StructType)
			if uniform.Span != layerUniformSize || uniform.Members[5].Offset != 80 {
				t.Fatalf("shared flags ABI changed: %v", uniform)
			}
		}
	}
	bed := module.GlobalVariables[7]
	if bed.Name != "layerSeabed" || bed.Binding.Group != 0 || bed.Binding.Binding != 7 {
		t.Fatalf("bed binding changed: %v", bed)
	}
	code, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer || strings.Contains(code, "_mslBufferSizes") {
		t.Fatal("bed sampling introduced runtime-array resource metadata")
	}
	if !strings.Contains(code, "layerSeabed [[texture(6)]]") || !strings.Contains(code, "layerU [[buffer(0)]]") || info.EntryPointNames["layer_seabed_fragment"] == "" {
		t.Fatal("native bed resource map differs from group layout")
	}

	rawStart := strings.Index(code, "fragment layer_seabed_fragmentOutput layer_seabed_fragment(")
	if rawStart < 0 {
		t.Fatal("missing native raw fragment signature")
	}
	rawLength := strings.Index(code[rawStart:], "\n}")
	if rawLength < 0 {
		t.Fatal("unterminated native raw fragment")
	}
	rawEnd := rawStart + rawLength
	raw := code[rawStart:rawEnd]
	for _, unwanted := range []string{"layerSeabed", "layerWaterMask", "layerGroundField", "layerWaterColor", "layerGroundPixel"} {
		if strings.Contains(raw, unwanted) {
			t.Fatal("native raw entry reads deferred resource", unwanted)
		}
	}
	// Pinned native lowering must keep corner access out of arithmetic's
	// conditional expression, which previously collapsed fullscreen quads.
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
			t.Fatalf("guarded corner load reached position arithmetic: %s", line)
		}
	}
	for _, function := range []string{"layer_seabed_fragment", "layerRawTerrainColor", "layerRawTerrainAt"} {
		body := layerWaterFunction(t, layerTerrainSource, function)
		for _, unwanted := range []string{"layerSeabedAt", "layerWaterColor", "layerGroundPixel", "layerTerrainAt("} {
			if strings.Contains(body, unwanted) {
				t.Fatalf("raw bed applied deferred treatment or sampled itself: %s %s", function, unwanted)
			}
		}
	}
	sample := layerWaterCanonical(layerWaterFunction(t, layerTerrainSource, "layerSeabedAt"))
	for _, contract := range []string{"pixel=(world-layerU.camera.xy)*layerU.camera.z+layerU.viewport.xy*0.5", "q=pixel-0.5", "hi=vec2<i32>(textureDimensions(layerSeabed))-1"} {
		if !strings.Contains(sample, contract) {
			t.Fatalf("viewport/centre/clamp sampling changed: %s", contract)
		}
	}
	if strings.Contains(sample, "layerU.rect") || strings.Contains(sample, "waterTerrain") || strings.Count(sample, "textureLoad(layerSeabed") != 5 {
		t.Fatal("bed is sampled using map bounds or lost four-tap filtering")
	}
	for _, function := range []string{"layerTerrainAt", "layerTerrainColor"} {
		body := layerWaterCanonical(layerWaterFunction(t, layerTerrainSource, function))
		if !strings.HasPrefix(body, "if(layerU.flags.w!=0u){returnlayerSeabedAt(") || !strings.Contains(body, "returnlayerRawTerrain") {
			t.Fatal("inactive terrain path changed", function, body)
		}
	}
}

func TestSeabedWorldSamplingMatchesNativeViewportOverride(t *testing.T) {
	// Production overrides painted texture bounds with camera +/- half-view
	// before water sampling. The shader's direct pixel mapping is equivalent,
	// including non-unit zoom and ripple movement clamped at pixel centres.
	camera := [3]float32{127, 347, 1.75}
	view := [2]float32{320, 180}
	lo := [2]float32{camera[0] - view[0]*.5/camera[2], camera[1] - view[1]*.5/camera[2]}
	hi := [2]float32{camera[0] + view[0]*.5/camera[2], camera[1] + view[1]*.5/camera[2]}
	for _, pixel := range [][2]float32{{.5, .5}, {319.5, 179.5}, {40.75, 80.25}, {160, 90}} {
		for axis := 0; axis < 2; axis++ {
			world := camera[axis] + (pixel[axis]-view[axis]*.5)/camera[2]
			native := (world - lo[axis]) / (hi[axis] - lo[axis]) * view[axis]
			shader := (world-camera[axis])*camera[2] + view[axis]*.5
			if math.Abs(float64(native-shader)) > .0001 || math.Abs(float64(shader-pixel[axis])) > .0001 {
				t.Fatal("world-to-bed mapping differs from viewport", pixel, native, shader)
			}
		}
	}
}

func TestSeabedRawGroupNeverBindsItsOwnAttachment(t *testing.T) {
	fallback, bed, ground := new(wgpu.TextureView), new(wgpu.TextureView), new(wgpu.TextureView)
	l := Layers{terrainImage: layerTexture{v: fallback}}
	l.seabed.target.v = bed
	raw := l.layerTerrainEntries(nil, nil)
	final := l.layerTerrainEntries(bed, ground)
	if len(raw) != 8 || len(final) != 8 || raw[7].TextureView != fallback || raw[5].TextureView != fallback || final[7].TextureView != bed || final[5].TextureView != ground {
		t.Fatal("raw/final resources alias or changed binding ABI", raw, final)
	}
	for i := range raw {
		if raw[i].Binding != uint32(i) || final[i].Binding != uint32(i) {
			t.Fatal("group layouts differ", raw, final)
		}
	}
}
