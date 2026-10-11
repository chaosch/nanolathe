package battlegpu

import (
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestModelSupersampleBodyPackingAndCoreBound(t *testing.T) {
	input := []meshscene.ModelSlot{
		{Screen: [4]float32{-3, 7, 1023, 5}, Atlas: [4]float32{41, 23, 9, 9}, Params: [4]float32{.5, 1, 1}},
		{Screen: [4]float32{9, -1, 3, 7}, Params: [4]float32{1, 2}},
		{Screen: [4]float32{1, 2, 2.25, 1.25}, Params: [4]float32{1, 3}},
	}
	before := append([]meshscene.ModelSlot(nil), input...)
	dst := make([]meshscene.ModelSlot, 0, len(input))
	backing := dst[:cap(dst)]
	slots, width, height, err := modelPackSlotsInto(dst, input)
	if err != nil {
		t.Fatal(err)
	}
	if &slots[0] != &backing[0] || !reflect.DeepEqual(input, before) {
		t.Fatal("body packing lost owned reuse or modified borrowed input")
	}
	for i, slot := range slots {
		if slot.Screen != input[i].Screen || slot.Params != input[i].Params {
			t.Fatal("body packing changed placement, opacity or paint decisions")
		}
		want := [2]float32{2 * float32(math.Ceil(float64(slot.Screen[2]))), 2 * float32(math.Ceil(float64(slot.Screen[3])))}
		if slot.Atlas[2] != want[0] || slot.Atlas[3] != want[1] {
			t.Fatalf("slot %d extent=%v want doubled %v", i, slot.Atlas, want)
		}
		for _, v := range slot.Atlas {
			if int(v)%2 != 0 {
				t.Fatalf("slot %d splits a 2x2 block: %v", i, slot.Atlas)
			}
		}
		if slot.Atlas[0]+slot.Atlas[2] > float32(width) || slot.Atlas[1]+slot.Atlas[3] > float32(height) {
			t.Fatal("doubled slot escaped its atlas")
		}
	}
	if slots[1].Atlas[1] == 0 || slots[1].Atlas[0] != 0 {
		t.Fatal("doubled slots did not wrap into their own even row")
	}
	for _, size := range [][2]float32{{4096, 1}, {1, 4096}} {
		_, w, h, err := modelPackSlots([]meshscene.ModelSlot{{Screen: [4]float32{0, 0, size[0], size[1]}}})
		if err != nil || w > 8192 || h > 8192 {
			t.Fatalf("last core-sized doubled body rejected: %v %d %d", err, w, h)
		}
	}
	for _, size := range [][2]float32{{4096.25, 1}, {1, 4096.25}} {
		if _, _, _, err := modelPackSlots([]meshscene.ModelSlot{{Screen: [4]float32{0, 0, size[0], size[1]}}}); err == nil {
			t.Fatal("body requiring more than the core doubled atlas accepted")
		}
	}
}

// This authored color reference expresses §17's premultiplied coverage law.
// Shader compilation below checks its executable operators; neither check
// establishes hardware triangle coverage or native pixel parity.
func modelCoverageReference(samples [4][4]float32, filtered bool, opacity float32) (out [4]float32) {
	if !filtered {
		out = samples[0]
	} else {
		for _, sample := range samples {
			for i, channel := range sample {
				out[i] += channel / 4
			}
		}
	}
	for i := range out {
		out[i] *= opacity
	}
	return
}

func TestModelSupersampleCoverageAndTopLeft(t *testing.T) {
	red, blue := [4]float32{1, 0, 0, 1}, [4]float32{0, 0, 1, 1}
	for _, tc := range []struct {
		name     string
		samples  [4][4]float32
		filtered bool
		opacity  float32
		want     [4]float32
	}{
		{"one covered corner", [4][4]float32{red}, true, 1, [4]float32{.25, 0, 0, .25}},
		{"fully covered color mean", [4][4]float32{red, blue, red, blue}, true, 1, [4]float32{.5, 0, .5, 1}},
		{"transparent contributes zero", [4][4]float32{{}, blue, red}, true, 1, [4]float32{.25, 0, .25, .5}},
		{"opacity after quarter coverage", [4][4]float32{red}, true, .5, [4]float32{.125, 0, 0, .125}},
		{"off selects whole top left", [4][4]float32{red, blue, blue, blue}, false, .5, [4]float32{.5, 0, 0, .5}},
		{"off ignores covered other corners", [4][4]float32{{}, blue, red, blue}, false, 1, [4]float32{}},
		{"premult sample is not multiplied by alpha again", [4][4]float32{{.5, 0, 0, .5}}, true, .5, [4]float32{.0625, 0, 0, .0625}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelCoverageReference(tc.samples, tc.filtered, tc.opacity); got != tc.want {
				t.Fatalf("resolved=%v want %v", got, tc.want)
			}
		})
	}
}

func TestModelSupersampleResolveEmittedMetal(t *testing.T) {
	source := modelResolveShaderSource(8)
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
		t.Fatal("doubled resolve requires missing runtime-size operands")
	}
	reads, means, opacity := 0, 0, 0
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, "bodies.read(") {
			reads++
			if !strings.Contains(line, "= (") || !strings.HasSuffix(line, ");") || strings.Contains(line, "* 0.25") {
				t.Fatalf("guarded texture load participates in averaging: %s", line)
			}
		}
		if strings.Contains(line, "color = ") && strings.Contains(line, "* 0.25;") {
			means++
			if strings.Count(line, "+") != 3 || strings.Contains(line, "?") {
				t.Fatalf("four premult samples do not share one quarter weight: %s", line)
			}
		}
		if strings.Contains(line, "color = ") && strings.Contains(line, ".params.x") {
			opacity++
		}
	}
	if reads != 4 || means != 1 || opacity != 1 {
		t.Fatalf("resolve operators: reads=%d mean=%d opacity=%d", reads, means, opacity)
	}
	for _, pattern := range []string{
		`\bu \[\[buffer\(2\)\]\]`, `\bbodies \[\[texture\(0\)\]\]`,
		`if \(\(naga_f2u32\(_e[0-9]+\) & 8u\) != 0u\)`,
		`metal::floor\(local \* 0\.5\) \* 2\.0`,
		`metal::int2\(1, 0\)`, `metal::int2\(0, 1\)`, `metal::int2\(1, 1\)`,
		`c00[_0-9]* = _e[0-9]+;`,
	} {
		if !regexp.MustCompile(pattern).MatchString(code) {
			t.Fatalf("missing ordinary resolve contract %s", pattern)
		}
	}
	gate := strings.Index(code, "& 8u")
	mean := strings.Index(code, "* 0.25;")
	opacityAt := strings.Index(code, ".params.x)")
	if gate < 0 || mean <= gate || opacityAt <= mean {
		t.Fatal("opacity must follow the optional mean, leaving top-left selection outside its gate")
	}
}

func TestModelSupersampleIndependentShadowPacking(t *testing.T) {
	body, _, _, err := modelPackSlots([]meshscene.ModelSlot{{Screen: [4]float32{2, 3, 5, 7}}})
	if err != nil {
		t.Fatal(err)
	}
	original := make([]byte, modelInstanceStride)
	modelVec(original, 240, body[0].Screen)
	modelVec(original, 256, body[0].Atlas)
	for _, scale := range []float32{1, 2} {
		slot := meshscene.ModelSlot{Screen: [4]float32{4, 6, 5, 7}, Atlas: [4]float32{20, 8, 5 * scale, 7 * scale}}
		f := meshscene.LiveFrame{Instances: []meshscene.Instance{{}}, Shadows: meshscene.ShadowComposition{Slots: []meshscene.ModelSlot{slot}, Subjects: make([]meshscene.ShadowSubject, 1), Tint: [4]float32{0, 0, 0, scale}}}
		for _, soloBody := range []bool{true, false} {
			cloned, draw, err := modelShadowClone(append([]byte(nil), original...), f, meshscene.ShadowDraw{Slot: 1}, meshscene.ModelRules{}, soloBody)
			if err != nil {
				t.Fatal(err)
			}
			at := int(draw.instance) * modelInstanceStride
			if modelShadowReadVec(cloned, at+240) != slot.Screen || modelShadowReadVec(cloned, at+256) != slot.Atlas || f.Shadows.Tint[3] != scale {
				t.Fatal("ordinary body doubling leaked into independent shadow raster operands")
			}
		}
	}
}
