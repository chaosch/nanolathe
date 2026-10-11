package battlegpu

import (
	"encoding/binary"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestDistortionPackingPreservesOrderedSourcesAndClockOperands(t *testing.T) {
	in := []meshscene.Distortion{
		{Previous: [4]float32{1, 2, 3, 0}, Current: [4]float32{4, 5, 6, 0}, Shape: [4]float32{132, 20, 4, 0}, Params: [4]float32{8, 0, 0, 0}},
		{Previous: [4]float32{-3, -4, -5, 0}, Current: [4]float32{6, 7, 8, 0}, Shape: [4]float32{40, 32, .1375, 1}, Params: [4]float32{123, 217, 10.5, 151}},
		{Previous: [4]float32{1.25, -2.5, 3.75, 0}, Current: [4]float32{1.25, -2.5, 3.75, 0}, Shape: [4]float32{38, 56, 1, 1}, Params: [4]float32{3599, 17, -3.5, 0}},
	}
	before := slices.Clone(in)
	var data [distortionLimit * distortionStride]byte
	if err := distortionPack(data[:], in); err != nil {
		t.Fatal(err)
	}
	for i, source := range in {
		for lane, vector := range [4][4]float32{source.Previous, source.Current, source.Shape, source.Params} {
			for j, v := range vector {
				if got := binary.LittleEndian.Uint32(data[i*64+lane*16+j*4:]); got != math.Float32bits(v) {
					t.Fatalf("source %d lane %d/%d changed: %08x != %08x", i, lane, j, got, math.Float32bits(v))
				}
			}
		}
	}
	if !slices.Equal(in, before) {
		t.Fatal("borrowed publication mutated")
	}
	if err := distortionPack(data[:], make([]meshscene.Distortion, 192)); err != nil {
		t.Fatal("complete producer budget rejected", err)
	}
	if err := distortionPack(data[:], make([]meshscene.Distortion, 193)); err == nil {
		t.Fatal("over-budget publication truncated silently")
	}
	in[0].Shape[3] = 2
	if err := distortionPack(data[:], in); err == nil {
		t.Fatal("unknown source formula guessed")
	}
	in[0].Shape[3], in[0].Params[0] = 0, float32(math.NaN())
	if err := distortionPack(data[:], in); err == nil {
		t.Fatal("nonfinite source time accepted")
	}
}

// This stand-in tests only the frozen final-helper ABI, including water's
// five appended vectors. Root qualifies the real final helper and pixels.
const distortionFinalFixture = `
struct LayerUniform {
 camera:vec4<f32>,viewport:vec4<f32>,rect:vec4<f32>,fogRect:vec4<f32>,tiles:vec4<u32>,flags:vec4<u32>,ground:vec4<f32>,glow:vec4<f32>,lightRect:vec4<f32>,lightValues:vec4<f32>,
 waterPhase:vec4<f32>,waterMask:vec4<f32>,waterControls:vec4<f32>,waterTerrain:vec4<f32>,waterSample:vec4<f32>
}
@group(0) @binding(0) var<uniform> layerU:LayerUniform;
@group(0) @binding(1) var layerWorld:texture_2d<f32>;
@group(0) @binding(2) var layerGrid:texture_2d<f32>;
@group(0) @binding(3) var layerMasks:texture_2d<f32>;
@group(0) @binding(4) var layerGlow:texture_2d<f32>;
fn layerFinalWorld(samplePixel:vec2<f32>,destinationPixel:vec2<f32>)->vec4<f32> {
 return textureLoad(layerWorld,vec2<i32>(samplePixel),0)+textureLoad(layerGlow,vec2<i32>(samplePixel),0)+textureLoad(layerGrid,vec2<i32>(destinationPixel),0)+textureLoad(layerMasks,vec2<i32>(destinationPixel),0);
}`

func TestDistortionCoreShaderABIResourceSlotsAndArithmetic(t *testing.T) {
	source := distortionFinalFixture + "\n" + distortionShader
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
	want := map[string]uint32{"LayerUniform": 240, "DistortionSource": 64}
	boundedSources := false
	for _, typ := range module.Types {
		if size, ok := want[typ.Name]; ok {
			structure, ok := typ.Inner.(ir.StructType)
			if !ok || structure.Span != size {
				t.Fatal("shared source ABI changed", typ.Name, typ.Inner)
			}
			for i, member := range structure.Members {
				if member.Offset != uint32(i*16) {
					t.Fatal("packed lane moved", typ.Name, member)
				}
			}
			delete(want, typ.Name)
		}
		if array, ok := typ.Inner.(ir.ArrayType); ok {
			if array.Size.Constant == nil {
				t.Fatal("bounded sources acquired runtime-array metadata")
			}
			if module.Types[array.Base].Name == "DistortionSource" {
				if *array.Size.Constant != distortionLimit || array.Stride != distortionStride {
					t.Fatal("source storage budget or packing changed", array)
				}
				boundedSources = true
			}
		}
	}
	if len(want) > 0 || !boundedSources {
		t.Fatal("missing source structures", want)
	}
	code, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer || strings.Contains(code, "_buffer_sizes") {
		t.Fatal("native final pass needs an unbound sizes buffer")
	}
	for _, name := range []string{"distortion_vertex", "distortion_fragment"} {
		if info.EntryPointNames[name] == "" {
			t.Fatal("missing entry point", name)
		}
	}
	for _, binding := range []string{"layerU [[buffer(0)]]", "distortionSources [[buffer(1)]]", "layerWorld [[texture(0)]]", "layerGrid [[texture(1)]]", "layerMasks [[texture(2)]]", "layerGlow [[texture(3)]]"} {
		if !strings.Contains(code, binding) {
			t.Fatal("native resource map moved", binding)
		}
	}
	for _, line := range strings.Split(code, "\n") {
		if (strings.Contains(line, ".position =") || strings.Contains(line, "offset =")) && strings.Contains(line, "?") {
			t.Fatal("guarded corner arithmetic reached clip projection", line)
		}
	}
	// Constants and strict comparison boundaries from MIT nmDistortionShift,
	// including the deliberately distinct blast age and heat phase clocks.
	// Emitted decimal literals use the same rounded float32 values as Metal.
	for _, pattern := range []string{
		`= layerU\.ground\.w;`, `clock_? =[^\n]+params\.x[^\n]+- 1\.0`,
		`clock_? >= 15\.0`, `metal::abs\(band_?\) >= 1\.0`, `r_? < 0\.001`,
		`metal::mix\(12\.0,`, `band_? \* envelope_?\) \* envelope_?`, `3\.5`,
		`0\.20943952`, `0\.13`, `0\.58`, `0\.42`, `3\.1415927`,
		`18\.0`, `33\.0`, `2\.5`, `0\.35`, `22\.0`, `0\.24`, `1\.8`,
		`rise_? <= 0\.0`, `rise_? >= 1\.0`, `metal::abs\(across_?\) >= 1\.0`,
		`0\.55`, `age_? / 300\.0`, `layerFinalWorld\(samplePixel(?:_\d+)?, [^\n]+\.position\.xy,`,
	} {
		if !regexp.MustCompile(pattern).MatchString(code) {
			t.Fatalf("emitted distortion arithmetic missing %q", pattern)
		}
	}
	entries := distortionLayoutEntries()
	if len(entries) != 6 || entries[0].Binding != 0 || entries[0].Buffer.MinBindingSize != layerUniformSize || entries[5].Binding != 5 || entries[5].Buffer.MinBindingSize != 192*64 {
		t.Fatal("final shared layout or producer budget changed", entries)
	}
	// Heat's travelling phase intentionally does not share the blast's -1
	// age offset; interpolation and the bottom-edge offset use final uniforms.
	for _, expression := range []string{
		"mix(source.previous.xyz,source.current.xyz,layerU.ground.w)",
		"offset.y+=(source.params.z-source.shape.y)*layerU.camera.z",
		"world.z-layerU.camera.y-world.y*0.5",
		"(input.params.x+layerU.ground.w+input.params.y)*0.20943951",
	} {
		if !strings.Contains(distortionShader, expression) {
			t.Fatal("projection or distinct phase operand changed", expression)
		}
	}
}

// Independent float64 oracle for the artistic contracts in GPU design §§25,
// 27–28; it is not a claim that compiling a shader establishes GPU pixels.
func distortionReferenceShift(shape, params [4]float64, local [2]float64, alpha float64) ([2]float64, bool) {
	clock := params[0] + alpha - 1
	if shape[3] < .5 {
		if clock < 0 || clock >= 15 {
			return [2]float64{}, false
		}
		radius := 12 + (shape[0]-12)*clock/15
		d := [2]float64{local[0] * (shape[0] + shape[1]), local[1] * (shape[0] + shape[1])}
		r := math.Hypot(d[0], d[1])
		band := (r - radius) / max(shape[1], .001)
		if math.Abs(band) >= 1 || r < .001 {
			return [2]float64{}, false
		}
		strength := min(clock, 1) * math.Pow(1-clock/15, 2) * shape[2]
		envelope := 1 - band*band
		v := band * envelope * envelope * 3.5 * strength
		return [2]float64{d[0] / r * v, d[1] / r * v}, true
	}
	rise := (1 - local[1]) * .5
	t := (params[0] + alpha + params[1]) * .20943951
	center := math.Sin(rise*5-t*.5) * .13 * rise
	across := (local[0] - center) / (.58 + .42*rise)
	if rise <= 0 || rise >= 1 || math.Abs(across) >= 1 {
		return [2]float64{}, false
	}
	envelope := math.Pow(1-across*across, 2) * math.Pow(math.Sin(rise*3.14159265), 2)
	wave := math.Sin(rise*18-t*2+across*2.5) + .35*math.Sin(rise*33-t*3-across*4)
	strength := shape[2]
	if params[3] > 0 {
		age := max(params[3]+alpha-1, 0)
		strength = .55 * math.Pow(max(1-age/300, 0), 2)
	}
	return [2]float64{wave * envelope * 1.8 * strength, .24 * math.Sin(rise*22-t*2+across*3) * envelope * 1.8 * strength}, true
}

func TestDistortionAgeBoundariesBipolarRingAndWreckFade(t *testing.T) {
	shape, params := [4]float64{132, 20, 4, 0}, [4]float64{8}
	for _, sign := range []float64{-1, 1} {
		// At age 7.5 radius=72, strength=1. The two band halves yield
		// opposite shifts of .5*.75²*3.5, without a centre displacement.
		x := (72 + sign*10) / 152
		got, visible := distortionReferenceShift(shape, params, [2]float64{x, 0}, .5)
		if !visible || math.Abs(got[0]-sign*.984375) > 1e-12 || got[1] != 0 {
			t.Fatal("radial bipolar profile/age changed", got, visible)
		}
	}
	for _, clock := range []float64{-.01, 15} {
		if _, visible := distortionReferenceShift(shape, [4]float64{clock + 1}, [2]float64{.5, 0}, 0); visible {
			t.Fatal("lifetime comparison changed", clock)
		}
	}
	for _, radius := range []float64{52, 92} {
		if _, visible := distortionReferenceShift(shape, params, [2]float64{radius / 152, 0}, .5); visible {
			t.Fatal("exact ring rim retained", radius)
		}
	}
	// At phase zero and halfway up the plume, centre correction leaves an
	// envelope of one. This closed-form sample checks both wave frequencies.
	local := [2]float64{math.Sin(2.5) * .065, 0}
	base := [2]float64{(math.Sin(9) + .35*math.Sin(16.5)) * 1.8, math.Sin(11) * .24 * 1.8}
	for _, age := range []float64{0, 150, 300} {
		got, visible := distortionReferenceShift([4]float64{40, 32, 7, 1}, [4]float64{0, 0, 10.5, age + 1}, local, 0)
		strength := .55 * math.Pow(max(1-age/300, 0), 2)
		for i := range got {
			if !visible || math.Abs(got[i]-base[i]*strength) > 1e-9 {
				t.Fatal("wreck clock or quadratic strength applied twice", age, got, base)
			}
		}
	}
}

func TestDistortionEmptyFrameAndClosedComponentDoNotDraw(t *testing.T) {
	r := &Distortions{prepared: true, count: 1, counts: DistortionCounters{Draws: 1, Sources: 1, UploadBytes: 64}}
	if err := r.Prepare(meshscene.LiveFrame{}, 121, 67); err != nil {
		t.Fatal(err)
	}
	r.Draw(nil)
	if r.Counters() != (DistortionCounters{}) {
		t.Fatal("empty publication retained stale sources", r.Counters())
	}
	if err := r.Prepare(meshscene.LiveFrame{}, 8193, 67); err == nil {
		t.Fatal("invalid physical extent admitted")
	}
	r.Close()
	r.Close()
	if r.Prepare(meshscene.LiveFrame{}, 121, 67) == nil || r.SetInputs(nil, nil, nil, nil, nil) == nil {
		t.Fatal("closed component admitted work")
	}
}
