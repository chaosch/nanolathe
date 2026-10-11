package battlegpu

import (
	_ "embed"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
)

// These are independent of the host's concatenation so the bounded shader
// unit can be checked before layers.go adopts its new uniform and binding.
//
//go:embed terrain.wgsl
var layerWaterTestTerrain string

//go:embed water.wgsl
var layerWaterTestSurface string

func TestWaterSurfaceCoreABIAndNativeResourceMap(t *testing.T) {
	source := layerWaterTestTerrain + "\n" + layerWaterTestSurface
	if _, err := naga.Compile(source); err != nil {
		t.Fatal(err)
	}
	ast, err := naga.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.LowerWithCapabilities(ast, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, typ := range module.Types {
		if typ.Name != "LayerUniform" {
			continue
		}
		found = true
		s, ok := typ.Inner.(ir.StructType)
		if !ok || s.Span != 240 || len(s.Members) != 15 {
			t.Fatalf("water uniform ABI: %v", typ.Inner)
		}
		for i, member := range s.Members {
			if member.Offset != uint32(i*16) {
				t.Fatalf("%s offset=%d", member.Name, member.Offset)
			}
		}
		want := []string{"waterPhase", "waterMask", "waterControls", "waterTerrain", "waterSample"}
		for i, name := range want {
			if s.Members[10+i].Name != name {
				t.Fatalf("published water lane %d became %q", i, s.Members[10+i].Name)
			}
		}
	}
	if !found {
		t.Fatal("missing terrain uniform")
	}
	if len(module.GlobalVariables) != 7 {
		t.Fatalf("unexpected resources: %v", module.GlobalVariables)
	}
	for i, global := range module.GlobalVariables {
		if global.Binding == nil || global.Binding.Group != 0 || global.Binding.Binding != uint32(i) {
			t.Fatalf("terrain/water resource moved: %v", global)
		}
		if i == 6 {
			image, ok := module.Types[global.Type].Inner.(ir.ImageType)
			if global.Name != "layerWaterMask" || !ok || image.Dim != ir.Dim2D || image.Class != ir.ImageClassSampled || image.SampledKind != ir.ScalarFloat || image.Multisampled {
				t.Fatalf("water mask is not a core float texture: %v %v", global, image)
			}
		}
	}
	code, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer || strings.Contains(code, "_mslBufferSizes") {
		t.Fatal("water introduced runtime-array metadata")
	}
	// Native rendering numbers resources independently by type, in layout
	// order. New group binding 6 is the sixth texture, not texture slot 6.
	for _, binding := range []string{"layerU [[buffer(0)]]", "layerTiles [[texture(0)]]", "layerLookup [[texture(1)]]", "layerDetail [[texture(2)]]", "layerFallback [[texture(3)]]", "layerGroundField [[texture(4)]]", "layerWaterMask [[texture(5)]]"} {
		if !strings.Contains(code, binding) {
			t.Fatalf("native terrain binding changed: %q", binding)
		}
	}
	for _, entry := range []string{"layer_terrain_vertex", "layer_terrain_fragment"} {
		if info.EntryPointNames[entry] == "" {
			t.Fatalf("missing entry point %q", entry)
		}
	}
	if len(info.EntryPointNames) != 2 {
		t.Fatalf("unrelated water pass reached terrain module: %v", info.EntryPointNames)
	}
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
			t.Fatalf("guarded corner load reached position arithmetic: %s", line)
		}
	}
}

func TestWaterSurfaceKeepsNativeFieldAndShoreArithmetic(t *testing.T) {
	native, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "platform", "metalrender", "shaders", "water.metal"))
	if err != nil {
		t.Fatal(err)
	}
	// These are authored Enhanced contracts, not retail evidence. Compare
	// the MIT primary implementation's operation order as well as constants.
	for _, function := range []string{"Field", "Offset", "Shade"} {
		want := layerWaterCanonical(layerWaterFunction(t, string(native), "nmWater"+function))
		got := layerWaterCanonical(layerWaterFunction(t, layerWaterTestSurface, "layerWater"+function))
		if got != want {
			t.Fatalf("%s arithmetic differs from native:\n%s\n%s", function, got, want)
		}
	}
	// Noise uses different declaration grouping, while the surface receives
	// its original texel from the terrain caller. Lock the complete numeric
	// sequence of both kernels despite those interface differences.
	for _, function := range []string{"Noise", "Color"} {
		want := layerWaterNumbers(t, layerWaterFunction(t, string(native), "nmWater"+function))
		got := layerWaterNumbers(t, layerWaterFunction(t, layerWaterTestSurface, "layerWater"+function))
		if !slices.Equal(got, want) {
			t.Fatalf("%s constants/order changed: %v != %v", function, got, want)
		}
	}
	for _, pair := range [][2]string{
		{"nmPatternSize", "layerWaterPatternSize"},
		{"nmSurfaceOpacity", "layerWaterSurfaceOpacity"},
		{"nmCurrentScale", "layerWaterCurrentScale"},
		{"nmRippleDeformation", "layerWaterRippleDeformation"},
		{"nmSurfaceEnergy", "layerWaterSurfaceEnergy"},
		{"nmShoreFoamOpacity", "layerWaterShoreFoamOpacity"},
		{"nmEdgeFadePixels", "layerWaterEdgeFadePixels"},
	} {
		value := func(source, name string) string {
			pattern := regexp.MustCompile(regexp.QuoteMeta(name) + ":?(?:f32)?=(\\d+(?:\\.\\d+)?)")
			match := pattern.FindStringSubmatch(source)
			if len(match) != 2 {
				t.Fatalf("missing surface constant %s", name)
			}
			return match[1]
		}
		if value(string(native), pair[0]) != value(layerWaterTestSurface, pair[1]) {
			t.Fatalf("surface tuning changed: %v", pair)
		}
	}
}

func TestWaterSurfaceAdmissionClampsAndTerrainLightOrder(t *testing.T) {
	color := layerWaterCanonical(layerWaterFunction(t, layerWaterTestSurface, "layerWaterColor"))
	if !strings.HasPrefix(color, "if(layerU.waterMask.w<0.5){returnoriginal;}") {
		t.Fatal("inactive surface reads or changes the painted terrain")
	}
	for _, contract := range []string{
		"layerWaterMaskAt(world/layerU.waterMask.x)",
		"layerWaterField(pattern,drift*layerU.waterControls.x,t*layerU.waterControls.x)",
		"clamp(world+offset,layerU.waterSample.xy,layerU.waterSample.zw)",
		"layerTerrainAt(sample,true)",
		"returnfloat4(surface,base.a)",
	} {
		if !strings.Contains(color, contract) {
			t.Fatalf("surface input/filter/alpha contract changed: %s", contract)
		}
	}
	mask := layerWaterCanonical(layerWaterFunction(t, layerWaterTestSurface, "layerWaterMaskTexel"))
	if !strings.Contains(mask, "if(at.x<0||at.y<0||at.x>=size.x||at.y>=size.y){returnfloat4(0);}") || strings.Contains(mask, "clamp(") {
		t.Fatal("outside-mask reads extend the edge medium")
	}
	maskAt := layerWaterCanonical(layerWaterFunction(t, layerWaterTestSurface, "layerWaterMaskAt"))
	if !strings.Contains(maskAt, "q=p-float2(0.5)") || strings.Count(maskAt, "layerWaterMaskTexel(") != 4 {
		t.Fatal("mask lost pixel-centre four-tap interpolation")
	}
	tile := layerWaterCanonical(layerWaterFunction(t, layerWaterTestTerrain, "layerTileTexel"))
	if !strings.Contains(tile, "clamp(p,vec2<i32>(0),vec2<i32>(i32(side)-1))") {
		t.Fatal("water sample crosses an authored atlas tile")
	}
	fragment := layerWaterCanonical(layerWaterFunction(t, layerWaterTestTerrain, "layer_terrain_fragment"))
	water := strings.Index(fragment, "layerWaterColor(in.world,layerTerrainColor(in))")
	light := strings.Index(fragment, "layerGroundPixel(pixel)")
	if water < 0 || light <= water {
		t.Fatal("water moved after terrain illumination")
	}
}

func layerWaterFunction(t *testing.T, source, name string) string {
	t.Helper()
	at := strings.Index(source, name+"(")
	if at < 0 {
		t.Fatalf("missing shader helper %s", name)
	}
	start := strings.Index(source[at:], "{") + at
	depth := 1
	for end := start + 1; end < len(source); end++ {
		switch source[end] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start+1 : end]
			}
		}
	}
	t.Fatalf("unterminated shader helper %s", name)
	return ""
}

func layerWaterCanonical(body string) string {
	body = regexp.MustCompile("(?m)//.*$").ReplaceAllString(body, "")
	body = strings.NewReplacer(
		"vec2<f32>", "float2", "vec3<f32>", "float3", "vec4<f32>", "float4",
		"nmWaterNoise", "layerWaterNoise", "nmRippleDeformation", "layerWaterRippleDeformation",
		"nmSurfaceEnergy", "layerWaterSurfaceEnergy",
	).Replace(body)
	body = regexp.MustCompile("\\b(?:float[234]?|let|var)\\s+(\\w+)").ReplaceAllString(body, "$1")
	return regexp.MustCompile("\\s+").ReplaceAllString(body, "")
}

func layerWaterNumbers(t *testing.T, body string) []float64 {
	t.Helper()
	body = regexp.MustCompile("(?m)//.*$").ReplaceAllString(body, "")
	// Only the native sampler API has an explicit base-mip argument.
	body = strings.ReplaceAll(body, "level(0)", "")
	numbers := regexp.MustCompile("\\b\\d+(?:\\.\\d+)?\\b").FindAllString(body, -1)
	out := make([]float64, len(numbers))
	for i, number := range numbers {
		x, err := strconv.ParseFloat(number, 64)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = x
	}
	return out
}
