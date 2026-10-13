package battlegpu

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

// The pinned Metal writer numbers every module global densely per resource
// kind in declaration order; the air layout must follow that order exactly.
func TestModelAirShaderMetalLayout(t *testing.T) {
	source := modelAirShaderSource([2]int{12, 7})
	ast, err := naga.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.LowerWithCapabilities(ast, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	layout := modelAirLayoutEntries()
	if len(module.GlobalVariables) != len(layout) || strings.Contains(code, "_mslBufferSizes") || strings.Contains(source, "__") {
		t.Fatal("air module declares resources outside its layout or needs runtime sizes")
	}
	counts := map[string]int{}
	for i, entry := range layout {
		global := module.GlobalVariables[i]
		if global.Binding == nil || global.Binding.Binding != entry.Binding {
			t.Fatalf("air binding order differs at %d", i)
		}
		kind := "buffer"
		if entry.Texture != nil {
			kind = "texture"
		} else if entry.Sampler != nil {
			kind = "sampler"
		}
		pattern := fmt.Sprintf(`\b%s \[\[%s\(%d\)\]\]`, global.Name, kind, counts[kind])
		counts[kind]++
		if !regexp.MustCompile(pattern).MatchString(code) {
			t.Fatalf("air resource %s does not use %s slot %d", global.Name, kind, counts[kind]-1)
		}
	}
}

// A slot texel spans the largest power of two not above the radius, so the
// binomial taps sit one to two slot texels apart; the commit's slot position
// for a source point is the inverse of the passes' texel-centre mapping.
func TestModelAirScaleAndMapping(t *testing.T) {
	for _, tc := range []struct{ radius, want float64 }{{0, 1}, {0.9, 1}, {1.9, 1}, {2, 2}, {3.9, 2}, {4, 4}, {9, 8}, {40, 16}} {
		if got := modelAirScale(tc.radius); got != tc.want {
			t.Fatalf("radius %v scale=%v want %v", tc.radius, got, tc.want)
		}
	}
	m := &Models{packedSlots: []meshscene.ModelSlot{{Screen: [4]float32{100, 50, 30, 20}, Atlas: [4]float32{400, 200, 60, 40}}}}
	c := meshscene.ShadowComposition{Tint: [4]float32{0, 0, 0, 2}, Subjects: []meshscene.ShadowSubject{
		{Params: [4]float32{2, 0, 0, 2}, Order: [4]uint32{1}},
		{Params: [4]float32{3, 0, 9, 2}, Order: [4]uint32{1}},
	}}
	if err := m.modelPrepareAir(c); err != nil {
		t.Fatal(err)
	}
	if m.air.count != 1 || modelShadowReadVec(m.air.bytes, 0) != [4]float32{} {
		t.Fatal("only soft subjects take air slots")
	}
	place, source := modelShadowReadVec(m.air.bytes, modelAirStride), modelShadowReadVec(m.air.bytes, modelAirStride+16)
	// Reused body: raster 2, so radius 9 * 0.5 * 2 = 9 source texels, D = 8.
	if source[2] != 8 || source[3] != 9 || source[0] != float32(math.Floor(400-19)) || source[1] != float32(math.Floor(200-19)) {
		t.Fatalf("air source %v", source)
	}
	if place[0] != 1 || place[1] != 1 || place[2] != float32(math.Ceil((60+38)/8.0)) || place[3] != float32(math.Ceil((40+38)/8.0)) {
		t.Fatalf("air place %v", place)
	}
	for texel := float32(0); texel < place[2]; texel++ {
		q := source[0] + (texel+0.5)*source[2]
		if at := place[0] + (q-source[0])/source[2]; at != place[0]+texel+0.5 {
			t.Fatalf("slot texel %v maps back to %v", texel, at)
		}
	}
	if m.air.needW != 1024 || m.air.needH != int(place[3])+2 {
		t.Fatalf("air atlas need %dx%d", m.air.needW, m.air.needH)
	}
}

// Corner wetness reads the liquid mask at each commit quad corner's world
// point, as the commit's former per-pixel lookup did.
func TestModelCornerWetness(t *testing.T) {
	p := &modelShadowWork{hasWater: true, liquidExtent: [2]int{4, 4}, liquid: make([]byte, 16)}
	for x := 2; x < 4; x++ {
		for y := range 4 {
			p.liquid[y*4+x] = 255
		}
	}
	// Camera centred on world (32, 32) at zoom 2 in a 64x64 view: screen x
	// 0, 32 and 64 are world 16, 32 and 48, mask coordinates 0.5, 1.5 and 2.5
	// after the texel-centre offset. 1.5 lies halfway onto the wet columns,
	// below the 0.8 grade; 2.5 lies between two wet texels.
	wet := p.modelCornerWetness([4]float32{0, 0, 32, 32}, [3]float32{32, 32, 2}, 16, 64, 64)
	if wet != [4]float32{} {
		t.Fatalf("half-covered corners must stay below the 0.8 grade: %v", wet)
	}
	wet = p.modelCornerWetness([4]float32{32, 0, 32, 32}, [3]float32{32, 32, 2}, 16, 64, 64)
	if wet != [4]float32{0, 1, 0, 1} {
		t.Fatalf("corners over open water grade fully wet: %v", wet)
	}
	if (&modelShadowWork{}).modelCornerWetness([4]float32{0, 0, 1, 1}, [3]float32{0, 0, 1}, 16, 1, 1) != [4]float32{} {
		t.Fatal("no liquid mask grades nothing")
	}
}
