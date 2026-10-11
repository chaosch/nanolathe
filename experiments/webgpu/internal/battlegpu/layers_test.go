package battlegpu

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestLayerShadersCoreWGSL(t *testing.T) {
	for _, shader := range []struct{ name, source string }{{"terrain", layerTerrainShader}, {"sprites", layerSpriteShader}, {"fog", layerFogShader}, {"overlay-effects", layerOverlayShader}} {
		t.Run(shader.name, func(t *testing.T) {
			if _, err := naga.Compile(shader.source); err != nil {
				t.Fatal(err)
			}
			ast, err := naga.Parse(shader.source)
			if err != nil {
				t.Fatal(err)
			}
			module, err := naga.Lower(ast)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = msl.Compile(module, msl.DefaultOptions()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLayerSpriteBandsSkipReplacedEffectsWithoutMutatingPublication(t *testing.T) {
	sprites := []meshscene.Sprite{
		{Current: [4]float32{1, 0, 30, 0}},
		{Current: [4]float32{2, 0, 10, 1}},
		{Current: [4]float32{3, 0, 10, 0}},
		{Current: [4]float32{4, 0, 20, 0}},
	}
	f := meshscene.LiveFrame{Sprites: sprites, Alpha: 1, StockEffects: &meshscene.StockFrame{}, Composition: meshscene.ModelComposition{SpriteSources: []meshscene.SpriteSource{{Kind: meshscene.SpriteSourceFeatureBody}, {Kind: meshscene.SpriteSourceFeatureBody}, {Kind: meshscene.SpriteSourceFallbackEffect}, {Kind: meshscene.SpriteSourceFeatureBody}}}}
	got, bands, _ := layerOrderSprites(f)
	if len(got) != 3 || bands != [2][2]uint32{{0, 1}, {1, 2}} || got[0].Current[0] != 2 || got[1].Current[0] != 4 || got[2].Current[0] != 1 {
		t.Fatal(got, bands)
	}
	for i, x := range sprites {
		if x.Current[0] != float32(i+1) {
			t.Fatal("publication reordered in place")
		}
	}
}

func TestLayerFeaturePaintRemapsOriginalSourcesAndExcludesLooseDuplicates(t *testing.T) {
	sprites := []meshscene.Sprite{
		{Current: [4]float32{1, 0, 30, 0}}, // original source zero remains a valid body
		{Current: [4]float32{2, 0, 0, 0}},  // replaced fallback effect
		{Current: [4]float32{3, 0, 0, 1}},  // short feature
		{Current: [4]float32{4, 0, 0, 1}},  // loose short art
		{Current: [4]float32{5, 0, 10, 0}}, // loose upper art
		{Current: [4]float32{6, 0, 20, 0}}, // shadow, separated from body by depth sorting
	}
	paint := []meshscene.FeatureSpritePaint{
		{Source: 5, Rank: 4, Phase: meshscene.ModelPhaseGround, Shadow: true},
		{Source: 0, Rank: 4, Phase: meshscene.ModelPhaseGround},
		{Source: 2, Phase: meshscene.ModelPhaseShortFeatures},
		{Source: 1}, // filtered sources do not name a packed instance
		{Source: math.MaxUint32},
	}
	f := meshscene.LiveFrame{Sprites: sprites, Alpha: 1, StockEffects: &meshscene.StockFrame{}, Composition: meshscene.ModelComposition{FeatureSprites: paint, SpriteSources: make([]meshscene.SpriteSource, len(sprites))}}
	f.Composition.SpriteSources[1].Kind = meshscene.SpriteSourceFallbackEffect
	packed, bands, remap := layerOrderSprites(f)
	wantRemap := []uint32{5, 0, 1, 2, 3, 4}
	for i, x := range remap {
		if x != wantRemap[i] {
			t.Fatal("original identity changed", remap)
		}
	}
	features, loose := layerFeatureLayout(remap, bands, paint)
	l := Layers{spriteRemap: remap, featureSources: features, featureDrawn: make([]bool, len(remap)), featurePaintOrder: true}
	if len(loose[0]) != 1 || loose[0][0].first != 1 || loose[0][0].count != 1 || len(loose[1]) != 1 || loose[1][0].first != 2 || loose[1][0].count != 1 {
		t.Fatal(loose)
	}
	draws := make([]int, len(packed))
	for _, phase := range loose {
		for _, span := range phase {
			for at := span.first; at < span.first+span.count; at++ {
				draws[at]++
			}
		}
	}
	for i, source := range []uint32{5, 0, 2} {
		at, ok := l.layerTakeFeatureSource(source)
		if !ok || at != []uint32{3, 4, 0}[i] {
			t.Fatal("paint order/source remap changed", source, at, ok)
		}
		draws[at]++
	}
	for _, n := range draws {
		if n != 1 {
			t.Fatal("packed source omitted or drawn twice", draws)
		}
	}
	for _, source := range []uint32{0, 1, 4, math.MaxUint32} {
		if _, ok := l.layerTakeFeatureSource(source); ok {
			t.Fatal("duplicate/filtered/loose/invalid source admitted", source)
		}
	}
	for i, s := range sprites {
		if s.Current[0] != float32(i+1) {
			t.Fatal("publication mutated")
		}
	}
}

func TestLayerFeaturePaintModeKeepsFallbackLimitationUntilEnabled(t *testing.T) {
	l := Layers{spriteRemap: []uint32{1}, featureSources: []bool{true}, featureDrawn: []bool{false}, featurePaintCount: 2, missing: map[string]int{"feature_subject_painter_order": 2}}
	if _, ok := l.layerTakeFeatureSource(0); ok {
		t.Fatal("feature mode enabled implicitly")
	}
	if l.Missing()["feature_subject_painter_order"] != 2 {
		t.Fatal("fallback limitation disappeared")
	}
	l.SetFeaturePaintOrder(true)
	if _, ok := l.Missing()["feature_subject_painter_order"]; ok {
		t.Fatal("explicit seam still reports fallback ordering")
	}
	if l.DrawFeature(nil, 0) {
		t.Fatal("nil pass accepted")
	}
	if at, ok := l.layerTakeFeatureSource(0); !ok || at != 0 {
		t.Fatal("source zero or nil-pass handling changed", at, ok)
	}
	l.SetFeaturePaintOrder(false)
	if l.Missing()["feature_subject_painter_order"] != 2 {
		t.Fatal("disabling seam hid fallback limitation")
	}
}

func TestLayerAnnotationRangesPreservePhysicalRectsColorsAndSlotOrder(t *testing.T) {
	in := []meshscene.Annotation{
		{Slot: 3, Rect: [4]float32{31.5, 41.25, 7.5, 2.5}, Color: [4]float32{.25, .5, .75, .5}},
		{Slot: 0, Rect: [4]float32{100, 100, 100, 100}},
		{Slot: 1, Rect: [4]float32{11.5, 12.25, .5, 1.5}, Color: [4]float32{.75, .25, .5, .25}},
		{Slot: 3, Rect: [4]float32{32.5, 42.25, 8.5, 3.5}, Color: [4]float32{.5, .75, .25, .75}},
		{Slot: math.MaxUint32},
		{Slot: 5},
	}
	before := slices.Clone(in)
	data, ranges := layerPackAnnotations(in, 4)
	want := [][2]uint32{{0, 1}, {}, {1, 2}, {}}
	if !slices.Equal(ranges, want) || len(data) != 3*64 {
		t.Fatal("annotation ranges changed", ranges, len(data))
	}
	for packed, original := range []int{2, 0, 3} {
		for _, lane := range []struct {
			at    int
			value [4]float32
		}{{0, in[original].Rect}, {32, in[original].Color}} {
			for component, x := range lane.value {
				at := packed*64 + lane.at + component*4
				if got := math.Float32frombits(binary.LittleEndian.Uint32(data[at:])); got != x {
					t.Fatalf("annotation %d lane %d/%d: %g != %g", packed, lane.at, component, got, x)
				}
			}
		}
		for _, at := range []int{16, 20, 24, 28, 48, 52, 56, 60} {
			if binary.LittleEndian.Uint32(data[packed*64+at:]) != 0 {
				t.Fatal("annotation gained texture/blend operands")
			}
		}
	}
	if !slices.Equal(in, before) {
		t.Fatal("annotation publication mutated")
	}
}

func TestLayerAnnotationEmptySlotsAndAbsentPassDoNotDraw(t *testing.T) {
	data, ranges := layerPackAnnotations([]meshscene.Annotation{{Slot: 0}, {Slot: 2}}, 1)
	if len(data) != 0 || !slices.Equal(ranges, [][2]uint32{{}}) {
		t.Fatal("invalid annotation slot admitted", ranges, len(data))
	}
	l := Layers{annotationRanges: [][2]uint32{{0, 1}}}
	for _, slot := range []uint32{0, 1, 2, math.MaxUint32} {
		l.DrawAnnotations(nil, slot)
	}
	if l.counts.Draws != 0 {
		t.Fatal("annotations drawn without a pass")
	}
}

func TestLayerQuadScaleLeavesArtAndBlendOperands(t *testing.T) {
	q := meshscene.OverlayQuad{Rect: [4]float32{2, 3, 5, 7}, UV: [4]float32{.125, .25, .5, .75}, Color: [4]float32{.25, .5, .75, .5}, Params: [4]float32{0, 1, 1, 0}}
	out := make([]byte, 64)
	layerPackQuad(out, q, 2)
	for lane, want := range [][4]float32{{4, 6, 10, 14}, q.UV, q.Color, q.Params} {
		for i, x := range want {
			if got := math.Float32frombits(binary.LittleEndian.Uint32(out[lane*16+i*4:])); got != x {
				t.Fatalf("lane %d/%d: %g != %g", lane, i, got, x)
			}
		}
	}
	if layerUniformSize != 15*16 || layerSmokeStride != 9*16 {
		t.Fatal("WGSL aligned vector strides changed")
	}
}

func TestLayerBlendPreservesPremultiplicationAndDestinationAlpha(t *testing.T) {
	if layerBlend(-1) != nil {
		t.Fatal("opaque pass gained blending")
	}
	b := layerBlend(0)
	if b.Color.SrcFactor != gputypes.BlendFactorOne || b.Color.DstFactor != gputypes.BlendFactorOneMinusSrcAlpha {
		t.Fatal("premultiplied sprite arithmetic changed")
	}
	b = layerBlend(1)
	if b.Color.SrcFactor != gputypes.BlendFactorZero || b.Color.DstFactor != gputypes.BlendFactorSrc || b.Alpha.SrcFactor != gputypes.BlendFactorZero || b.Alpha.DstFactor != gputypes.BlendFactorOne {
		t.Fatal("multiply must preserve destination alpha")
	}
	b = layerBlend(2)
	if b.Color.DstFactor != gputypes.BlendFactorOne {
		t.Fatal("stock additive replaced destination RGB")
	}
}

func TestLayerRunsNeverCrossBlendBarrier(t *testing.T) {
	quads := make([]meshscene.OverlayQuad, 6)
	for i, x := range []float32{0, 0, 1, 2, 2, 0} {
		quads[i].Params[1] = x
	}
	got := layerRuns(quads, 0)
	want := []layerDraw{{first: 0, count: 2, mode: 0}, {first: 2, count: 1, mode: 1}, {first: 3, count: 2, mode: 2}, {first: 5, count: 1, mode: 0}}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("run %d: %+v != %+v", i, got[i], want[i])
		}
	}
}

func TestLayerSmokeSelectionStableTiesAndStrictReach(t *testing.T) {
	makeLight := func(x, power float32) meshscene.RetainedLight {
		return meshscene.RetainedLight{PositionRadius: [4]float32{x, 0, 0, 10}, ColorGroundGain: [4]float32{power, power, power, 0}, Flags: [4]uint32{1, 0, 0, 0}}
	}
	lights := make([]meshscene.RetainedLight, 9)
	for i := range lights {
		lights[i] = makeLight(0, 1)
	}
	indices, n := layerSelectSmoke(lights, [2]float32{}, 0)
	if n != 8 {
		t.Fatal(n)
	}
	for i, x := range indices {
		if x != uint32(i) {
			t.Fatal("equal source displaced an earlier source", indices)
		}
	}
	lights[8] = makeLight(0, 2)
	indices, n = layerSelectSmoke(lights, [2]float32{}, 0)
	if n != 8 || indices[0] != 8 || indices[1] != 1 {
		t.Fatal("stronger source must replace first tied minimum", indices)
	}
	lights = []meshscene.RetainedLight{makeLight(15, 1), makeLight(15.01, 3)}
	indices, n = layerSelectSmoke(lights, [2]float32{}, 0)
	if n != 1 || indices[0] != 0 {
		t.Fatal("reach equality must remain admitted", indices, n)
	}
	lights[0].Flags[0] = 0
	_, n = layerSelectSmoke(lights, [2]float32{}, 0)
	if n != 0 {
		t.Fatal("disabled model light selected", n)
	}
}

func TestLayerTextureValidationRejectsTruncatedAndBeyondCore(t *testing.T) {
	if layerValidateTexture(meshscene.Texture{Width: 1, Height: 1, RGBA: make([]byte, 4)}) != nil {
		t.Fatal("valid authored texture rejected")
	}
	for _, x := range []meshscene.Texture{{Width: 1, Height: 1, RGBA: make([]byte, 3)}, {Width: 8193, Height: 1, RGBA: make([]byte, 8193*4)}, {Width: -1, Height: 1}} {
		if layerValidateTexture(x) == nil {
			t.Fatal("invalid texture accepted", x.Width, x.Height)
		}
	}
}

func TestLayerCoalescingPreservesOrderedBlendAndSnapshotBarriers(t *testing.T) {
	quads := make([]meshscene.OverlayQuad, 4)
	quads[3].Params[1] = 1
	out := layerRunsInto(nil, quads[:2], meshscene.EffectOpQuads, 5)
	out = layerRunsInto(out, quads[2:], meshscene.EffectOpQuads, 7)
	if len(out) != 2 || out[0].first != 5 || out[0].count != 3 || out[1].mode != 1 {
		t.Fatal(out)
	}
	for _, barrier := range []uint32{meshscene.EffectOpLens, meshscene.EffectOpRetainedModel} {
		blocked := []layerDraw{{kind: meshscene.EffectOpQuads, first: 0, count: 1}, {kind: barrier}}
		blocked = layerRunsInto(blocked, quads[:1], meshscene.EffectOpQuads, 1)
		if len(blocked) != 3 {
			t.Fatal("crossed snapshot/model barrier", blocked)
		}
	}
	separated := layerRunsInto([]layerDraw{{kind: meshscene.EffectOpQuads, first: 0, count: 1}}, quads[:1], meshscene.EffectOpQuads, 2)
	if len(separated) != 2 {
		t.Fatal("joined noncontiguous instances", separated)
	}
}

func TestEffectStreamCoalescingKeepsPainterBarriers(t *testing.T) {
	for _, kind := range []uint32{meshscene.EffectOpGeometry, meshscene.EffectOpGround, meshscene.EffectOpSmoke} {
		out := layerCoalesceEffects([]layerDraw{{kind: kind, first: 3, count: 6}, {kind: kind, first: 9, count: 3}})
		if len(out) != 1 || out[0].first != 3 || out[0].count != 9 {
			t.Fatal("lost contiguous primitive order", out)
		}
		for _, barrier := range []layerDraw{{kind: meshscene.EffectOpLens}, {kind: meshscene.EffectOpRetainedModel}, {kind: kind, mode: 1, first: 9, count: 3}, {kind: kind, first: 12, count: 3}} {
			out = layerCoalesceEffects([]layerDraw{{kind: kind, first: 3, count: 6}, barrier, {kind: kind, first: 9, count: 3}})
			if len(out) != 3 {
				t.Fatal("crossed painter barrier", out)
			}
		}
	}
}
