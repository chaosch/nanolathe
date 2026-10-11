package battlegpu

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func reflectionTestSource(kind float32) meshscene.WaterSource {
	var out meshscene.WaterSource
	for i := range out.Vertices {
		out.Vertices[i] = meshscene.WaterSourceVertex{
			PositionHeight: [4]float32{float32(i) + .25, -float32(i) - .5, float32(i) * 32, kind},
			UV:             [4]float32{float32(i) + 4, float32(i) + 8, 0, 0},
			Color:          [4]float32{13.0 / 255, 73.0 / 255, 229.0 / 255, 1},
			Bounds:         [4]float32{4, 8, 32, 48},
		}
	}
	return out
}

func TestReflectionFourCornersPreserveHeightAtlasPaletteAndOrder(t *testing.T) {
	in := []meshscene.WaterSource{reflectionTestSource(1), reflectionTestSource(2)}
	before := slices.Clone(in)
	bytes, count, err := reflectionPack(nil, in, 2.5)
	if err != nil || count != 2 || len(bytes) != 512 {
		t.Fatal("four-corner packet packing", count, len(bytes), err)
	}
	for i, source := range in {
		for corner, vertex := range source.Vertices {
			vertex.PositionHeight[0] *= 2.5
			vertex.PositionHeight[1] *= 2.5
			for lane, values := range [4][4]float32{vertex.PositionHeight, vertex.UV, vertex.Color, vertex.Bounds} {
				for j, want := range values {
					got := binary.LittleEndian.Uint32(bytes[(i*4+corner)*64+lane*16+j*4:])
					if got != math.Float32bits(want) {
						t.Fatalf("source=%d corner=%d lane=%d component=%d: %08x != %08x", i, corner, lane, j, got, math.Float32bits(want))
					}
				}
			}
		}
	}
	if !slices.Equal(in, before) {
		t.Fatal("immutable publication was changed")
	}
	indices := reflectionIndices(2)
	for i, want := range []uint32{0, 1, 2, 0, 2, 3, 4, 5, 6, 4, 6, 7} {
		if got := binary.LittleEndian.Uint32(indices[i*4:]); got != want {
			t.Fatal("native fan/corner order changed", i, got, want)
		}
	}
	control, err := reflectionControls([3]float32{.75, .6875, 2})
	if err != nil || control != [4]float32{1.5, 1.375, 2, 0} {
		t.Fatal("record/effective/output scaling changed", control, err)
	}
}

func TestReflectionStockOnlyCapAdmitsEqualityAndRetainsPrefix(t *testing.T) {
	in := make([]meshscene.WaterSource, reflectionSourceCap+1)
	for i := range in {
		in[i] = reflectionTestSource(1)
		for j := range in[i].Vertices {
			in[i].Vertices[j].PositionHeight[0] = float32(i)
		}
	}
	bytes, count, err := reflectionPack(nil, in, 1)
	if err != nil || count*4 != 32768 || len(bytes) != 32768*64 {
		t.Fatal("stock-only zero-model-cost cap changed", count, len(bytes), err)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(bytes[(count-1)*256:])); got != float32(count-1) {
		t.Fatal("overflow sources reordered the admitted prefix", got)
	}
	// Native packing validates even the records later rejected by GPU budget.
	in[len(in)-1].Vertices[0].UV[0] = float32(math.NaN())
	if _, _, err = reflectionPack(bytes, in, 1); err == nil {
		t.Fatal("nonfinite overflow record silently discarded")
	}
	for _, scale := range []float32{0, -1, float32(math.Inf(1)), float32(math.NaN())} {
		if _, _, err := reflectionPack(nil, nil, scale); err == nil {
			t.Fatal("invalid output scale accepted", scale)
		}
	}
	bad := reflectionTestSource(1)
	bad.Vertices[3].PositionHeight[3] = 2
	if _, _, err := reflectionPack(nil, []meshscene.WaterSource{bad}, 1); err == nil {
		t.Fatal("mixed quad kind admitted")
	}
}

func TestReflectionEmptyOrInvisibleFrameDoesNoGPUWork(t *testing.T) {
	r := &StockReflections{count: 3, encoded: true}
	for _, f := range []meshscene.LiveFrame{
		{},
		{WaterSources: []meshscene.WaterSource{reflectionTestSource(1)}},
		{WaterSources: []meshscene.WaterSource{reflectionTestSource(1)}, Water: meshscene.WaterUniform{Controls: [4]float32{0, 0, 0, 1}}},
	} {
		if err := r.Prepare(f, 65, 129); err != nil {
			t.Fatal(err)
		}
		if r.count != 0 || r.encoded || r.Counters() != (ReflectionCounters{}) || r.vertices != nil || r.control != nil || r.color.v != nil {
			t.Fatal("inactive frame allocated/uploaded/retained an active source")
		}
		if err := r.Encode(nil); err != nil {
			t.Fatal("inactive frame tried to encode a pass", err)
		}
		r.Draw(nil)
	}
	r.Close()
	if r.Encode(nil) == nil || r.Prepare(meshscene.LiveFrame{}, 65, 129) == nil {
		t.Fatal("closed component accepts work")
	}
}

func reflectionTestModule(t *testing.T, source string) (*ir.Module, string, msl.TranslationInfo) {
	t.Helper()
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
	code, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer || strings.Contains(code, "_mslBufferSizes") {
		t.Fatal("unexpected native runtime-array sizes argument")
	}
	return module, code, info
}

func TestReflectionCoreShadersHaveExactABIAndNativeResourceMaps(t *testing.T) {
	source, resolve, ok := strings.Cut(reflectionShader, reflectionModuleSplit)
	if !ok {
		t.Fatal("source/resolve module separator missing")
	}
	for _, fixture := range []struct {
		name, source string
		bindings     []string
		entries      []string
	}{
		{"source", source, []string{"reflectionU [[buffer(0)]]", "reflectionAtlas [[texture(0)]]"}, []string{"reflection_stock_vertex", "reflection_stock_fragment"}},
		{"resolve", resolve, []string{"reflectionU [[buffer(0)]]", "reflectionControl [[buffer(1)]]", "reflectionColor [[texture(0)]]", "reflectionSoft [[texture(1)]]", "reflectionMask [[texture(2)]]", "reflectionFlags [[texture(3)]]"}, []string{"reflection_screen_vertex", "reflection_resolve"}},
		{"tiles", reflectionTileShader, []string{"reflectionColor [[texture(0)]]", "reflectionSoft [[texture(1)]]", "reflectionFlags [[texture(2)]]"}, []string{"reflection_classify"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			module, code, info := reflectionTestModule(t, fixture.source)
			resources := 0
			for _, global := range module.GlobalVariables {
				if global.Binding != nil {
					resources++
				}
			}
			if len(info.EntryPointNames) != len(fixture.entries) || resources != len(fixture.bindings) {
				t.Fatal("unrelated resources/entry points reached native layout", info.EntryPointNames, module.GlobalVariables)
			}
			for _, binding := range fixture.bindings {
				if !strings.Contains(code, binding) {
					t.Fatal("native resource map differs from pipeline layout", binding)
				}
			}
			for _, entry := range fixture.entries {
				if info.EntryPointNames[entry] == "" {
					t.Fatal("missing native entry point", entry)
				}
			}
			if fixture.name != "tiles" {
				found := false
				vertexFound := fixture.name != "source"
				for _, typ := range module.Types {
					if typ.Name == "ReflectionVertex" {
						vertexFound = true
						v, ok := typ.Inner.(ir.StructType)
						if !ok || v.Span != 64 || len(v.Members) != 4 {
							t.Fatal("physical four-vector vertex ABI changed", typ.Inner)
						}
						for i, member := range v.Members {
							if member.Offset != uint32(i*16) {
								t.Fatal("source vertex lane offset changed", member)
							}
						}
					}
					if typ.Name == "ReflectionUniform" {
						found = true
						v, ok := typ.Inner.(ir.StructType)
						if !ok || v.Span != 240 || len(v.Members) != 15 {
							t.Fatal("shared water uniform changed", typ.Inner)
						}
						for i, member := range v.Members {
							if member.Offset != uint32(i*16) {
								t.Fatal("shared uniform offset changed", member)
							}
						}
					}
				}
				if !found || !vertexFound {
					t.Fatal("missing shared uniform/source vertex")
				}
			}
			for _, line := range strings.Split(code, "\n") {
				if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
					t.Fatal("pinned indexed-vector conditional reached position arithmetic", line)
				}
			}
			if fixture.name == "tiles" {
				if !strings.Contains(code, "texture2d<uint, metal::access::write>") && !strings.Contains(code, "texture2d<uint, access::write>") {
					t.Fatal("tile bits are no longer an exact core integer write texture")
				}
				for _, ep := range module.EntryPoints {
					if ep.Workgroup != [3]uint32{128, 1, 1} {
						t.Fatal("production tile lane count changed", ep.Workgroup)
					}
				}
			}
		})
	}
}

func TestReflectionNativeHeightClipRippleFilterAndSourceQuantization(t *testing.T) {
	native, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "platform", "metalrender", "shaders", "water_objects.metal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range reflectionSourceTargets() {
		if target.Format != gputypes.TextureFormatRGBA8Unorm || target.WriteMask != gputypes.ColorWriteMaskAll || target.Blend == nil {
			t.Fatal("source/soft planes lost per-source RGBA8 quantization", target)
		}
		for _, component := range []gputypes.BlendComponent{target.Blend.Color, target.Blend.Alpha} {
			if component.SrcFactor != gputypes.BlendFactorOne || component.DstFactor != gputypes.BlendFactorOneMinusSrcAlpha || component.Operation != gputypes.BlendOperationAdd {
				t.Fatal("weighted overlapping source/soft composition changed", component)
			}
		}
	}
	source, resolve, _ := strings.Cut(reflectionShader, reflectionModuleSplit)
	stock := layerWaterCanonical(layerWaterFunction(t, source, "reflection_stock_fragment"))
	for _, contract := range []string{
		"sprite=in.kind<1.5", "(sprite&&in.height<0.0)||(!sprite&&in.height<=0.0)",
		"in.uv.x>=in.bounds.z", "in.uv.y>=in.bounds.w", "if(tex.a<0.5)", "c=float4(tex.rgb,1.0)",
		"c*=1.0-smoothstep(64.0,160.0,in.height)", "out.soft=float4(0.0,0.0,0.0,c.a)",
	} {
		if !strings.Contains(stock, contract) {
			t.Fatal("stock strict clip/key/fade contract changed", contract)
		}
	}
	// Compare the primary MIT ripple and displaced-coordinate expressions,
	// including multiplication order and scale use, rather than only constants.
	nativeResolve := layerWaterFunction(t, string(native), "nm_wo_reflection_resolve")
	ours := layerWaterFunction(t, resolve, "reflection_resolve")
	for _, name := range []string{"t", "ripple", "q"} {
		extract := func(body string) string {
			t.Helper()
			pattern := regexp.MustCompile(`\b` + name + `\s*=([^;]+);`)
			match := pattern.FindStringSubmatch(body)
			if len(match) != 2 {
				t.Fatal("missing native reflection expression", name)
			}
			return reflectionCanonicalExpression(match[1])
		}
		if want, got := extract(nativeResolve), extract(ours); got != want {
			t.Fatalf("%s native arithmetic differs:\n%s\n%s", name, got, want)
		}
	}
	for _, weights := range []struct{ name, native, ours string }{
		{"horizontal", ".12,.09,.06,.03", "0.12,0.09,0.06,0.03"},
		{"vertical", ".06,.015", "0.06,0.015"},
	} {
		if !strings.Contains(nativeResolve, "={"+weights.native+"}") || !strings.Contains(ours, ">("+weights.ours+")") {
			t.Fatal("filled cross kernel differs from native", weights.name)
		}
	}
	if !strings.Contains(nativeResolve, "c.rgb*=float3(.76,.88,.94);return c*(.25*coverage)") || strings.Count(ours, "c.rgb*vec3<f32>(0.76,0.88,0.94)") != 2 || strings.Count(ours, "return c*(0.25*coverage)") != 2 {
		t.Fatal("native tint and premultiplied resolve opacity changed")
	}
	for _, name := range []string{"reflectionSample", "reflectionColorSample", "reflectionMaskAt"} {
		body := layerWaterCanonical(layerWaterFunction(t, resolve, name))
		if !strings.Contains(body, "floor(p-0.5)+0.5") || !strings.Contains(body, "fract(p-0.5)") || strings.Count(body, "mix(") != 3 {
			t.Fatal("manual pixel-centre bilinear order changed", name)
		}
	}
	_, code, _ := reflectionTestModule(t, source)
	for _, pattern := range []string{`height < 0\.0`, `height <= 0\.0`, `\.w < 0\.5`, `metal::smoothstep\(64\.0, 160\.0,`} {
		if !regexp.MustCompile(pattern).MatchString(code) {
			t.Fatal("strict source arithmetic did not survive native lowering", pattern)
		}
	}
}

func reflectionCanonicalExpression(expression string) string {
	expression = strings.NewReplacer(
		"vec2<f32>", "float2", "reflectionU.waterPhase", "water.phase",
		"reflectionU.waterControls", "water.controls", "reflectionControl", "control",
	).Replace(expression)
	expression = regexp.MustCompile(`(?:\b\d+\.\d+|\.\d+|\b\d+\b)`).ReplaceAllStringFunc(expression, func(s string) string {
		x, _ := strconv.ParseFloat(s, 64)
		return strconv.FormatFloat(x, 'g', -1, 64)
	})
	return regexp.MustCompile(`\s+`).ReplaceAllString(expression, "")
}

func TestReflectionTileRejectionConservativelyContainsEveryNativeTap(t *testing.T) {
	_, resolve, _ := strings.Cut(reflectionShader, reflectionModuleSplit)
	flags := layerWaterCanonical(layerWaterFunction(t, resolve, "reflectionResolveFlags"))
	for _, contract := range []string{
		"max(abs(reflectionControl.y)+1.0,4.0*abs(reflectionControl.z)+1.0)",
		"max(1.0,2.0*abs(reflectionControl.z)+1.0)", "return3u", "hi.x<0.0", "lo.x>=size.x", "/64", "bits==3u",
	} {
		if !strings.Contains(flags, contract) {
			t.Fatal("exact conservative native rejection boundary changed", contract)
		}
	}
	classify := layerWaterCanonical(layerWaterFunction(t, reflectionTileShader, "reflection_classify"))
	for _, contract := range []string{"i<4096u", "i+=128u", "c.r>0.0||c.g>0.0||c.b>0.0||c.a>0.0", ").r>0.0", "stride=64u", "workgroupBarrier()"} {
		if !strings.Contains(classify, contract) {
			t.Fatal("actual quantized plane classification changed", contract)
		}
	}
	// Independent tap-footprint enumeration includes partial tiles, fractional
	// camera/output scales, outside-source taps and alpha-zero RGB content.
	for _, size := range [][2]int{{1, 1}, {65, 129}, {1920, 1080}} {
		for _, scales := range [][2]float64{{.0625, 1}, {.6875, 2}, {2.75, 1.5}} {
			rx, ry := max(math.Abs(scales[0])+1, 4*math.Abs(scales[1])+1), max(1.0, 2*math.Abs(scales[1])+1)
			for _, q := range [][2]float64{{-.5, 64.25}, {63.999, .5}, {64.001, 127.75}, {float64(size[0]) + .25, float64(size[1]) - .5}} {
				for _, d := range [][2]float64{{0, 0}, {scales[0], 0}, {-scales[0], 0}, {4 * scales[1], 0}, {-4 * scales[1], 0}, {0, 2 * scales[1]}, {0, -2 * scales[1]}} {
					for _, neighbor := range [][2]float64{{-.5, -.5}, {.5, -.5}, {-.5, .5}, {.5, .5}} {
						x, y := math.Floor(q[0]+d[0]+neighbor[0]), math.Floor(q[1]+d[1]+neighbor[1])
						if x < 0 || y < 0 || x >= float64(size[0]) || y >= float64(size[1]) {
							continue
						}
						firstX := int(math.Floor(max(0, q[0]-rx))) / 64
						firstY := int(math.Floor(max(0, q[1]-ry))) / 64
						lastX := int(math.Floor(min(float64(size[0]-1), q[0]+rx))) / 64
						lastY := int(math.Floor(min(float64(size[1]-1), q[1]+ry))) / 64
						if int(x)/64 < firstX || int(x)/64 > lastX || int(y)/64 < firstY || int(y)/64 > lastY {
							t.Fatal("native filter texel escaped classified union", size, scales, q, x, y)
						}
					}
				}
			}
		}
	}
}
