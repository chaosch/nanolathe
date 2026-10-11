package battlegpu

import (
	"encoding/binary"
	"math"
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
	got, bands := layerOrderSprites(f)
	if len(got) != 3 || bands != [2][2]uint32{{0, 1}, {1, 2}} || got[0].Current[0] != 2 || got[1].Current[0] != 4 || got[2].Current[0] != 1 {
		t.Fatal(got, bands)
	}
	for i, x := range sprites {
		if x.Current[0] != float32(i+1) {
			t.Fatal("publication reordered in place")
		}
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
	if layerUniformSize != 7*16 || layerSmokeStride != 9*16 {
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
