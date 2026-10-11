package battlegpu

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestModelResolveMetalScalesLoadedCorner(t *testing.T) {
	ast, e := naga.Parse(modelResolveShader)
	if e != nil {
		t.Fatal(e)
	}
	module, e := naga.LowerWithCapabilities(ast, modelResolveShader, 0)
	if e != nil {
		t.Fatal(e)
	}
	code, _, e := msl.Compile(module, msl.DefaultOptions())
	if e != nil {
		t.Fatal(e)
	}
	loads, screenScales, atlasScales := 0, 0, 0
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, "corners.inner[") {
			loads++
			// The pinned writer does not parenthesize a robust array-read
			// conditional when used inside multiplication. Native readback
			// proved that this collapsed the resolved subject to one pixel.
			if strings.Contains(line, "*") || !strings.Contains(line, "= uint(") {
				t.Fatalf("corner guard participates in arithmetic: %s", line)
			}
		}
		if strings.Contains(line, "* s_") && strings.Contains(line, ".screen.zw") {
			screenScales++
		}
		if strings.Contains(line, "* s_") && strings.Contains(line, ".atlas.zw") {
			atlasScales++
		}
	}
	if loads != 2 || screenScales != 2 || atlasScales != 1 {
		t.Fatalf("resolve scales: loads=%d screen=%d atlas=%d", loads, screenScales, atlasScales)
	}
}

func TestModelShadersParseAndLower(t *testing.T) {
	for name, source := range map[string]string{"poses": modelPoseShader, "models": modelBodyShader, "resolve": modelResolveShader} {
		t.Run(name, func(t *testing.T) {
			ast, e := naga.Parse(source)
			if e != nil {
				t.Fatal(e)
			}
			module, e := naga.LowerWithCapabilities(ast, source, 0)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = msl.Compile(module, msl.DefaultOptions()); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestModelShaderLayouts(t *testing.T) {
	expected := map[string]uint32{"Vertex": modelVertexStride, "Instance": modelInstanceStride, "Pose": modelPoseStride, "Uniforms": 80, "Material": 16, "Selected": 48, "Light": 64}
	ast, e := naga.Parse(modelBodyShader)
	if e != nil {
		t.Fatal(e)
	}
	module, e := naga.LowerWithSource(ast, modelBodyShader)
	if e != nil {
		t.Fatal(e)
	}
	for _, typ := range module.Types {
		want, ok := expected[typ.Name]
		if !ok {
			continue
		}
		s, ok := typ.Inner.(ir.StructType)
		if !ok || s.Span != want {
			t.Fatalf("%s layout=%v want %d", typ.Name, typ.Inner, want)
		}
		delete(expected, typ.Name)
		if typ.Name == "Instance" {
			for i, member := range s.Members {
				if member.Offset != uint32(i*16) {
					t.Fatalf("Instance.%s offset=%d", member.Name, member.Offset)
				}
			}
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing shader ABI structures: %v", expected)
	}
}

func TestModelSlotIsolationAndBorrowedInput(t *testing.T) {
	input := []meshscene.ModelSlot{{Screen: [4]float32{10, 20, 60, 50}, Params: [4]float32{1, 1, 1}}, {Screen: [4]float32{10, 20, 60, 50}, Params: [4]float32{.5, 1, 0}}}
	before := append([]meshscene.ModelSlot(nil), input...)
	slots, w, h, e := modelPackSlots(input)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("packer modified borrowed composition")
	}
	if slots[0].Atlas == slots[1].Atlas {
		t.Fatal("overlapping world subjects share a height-key plane")
	}
	if w > 8192 || h > 8192 || slots[0].Params != input[0].Params {
		t.Fatalf("packing changed production decisions: %v", slots)
	}
	_, _, _, e = modelPackSlots([]meshscene.ModelSlot{{Screen: [4]float32{0, 0, 8193, 1}}})
	if e == nil {
		t.Fatal("unsupported core-limit rectangle accepted")
	}
}

func TestModelAtlasGeometricGrowth(t *testing.T) {
	current, allocated := 0, 0
	for need := 1; need <= 8192; need++ {
		next := modelAtlasExtent(need, current)
		if next < need || next < current || next > 8192 || next&(next-1) != 0 {
			t.Fatalf("need=%d current=%d next=%d", need, current, next)
		}
		if next != current {
			allocated += next
		}
		current = next
		if allocated >= 2*current {
			t.Fatalf("growth tiers exceed twice current extent: total=%d current=%d", allocated, current)
		}
	}
	if got := modelAtlasExtent(64, current); got != current {
		t.Fatalf("smaller frame shrank resident atlas: %d", got)
	}
}

func TestModelRetainedLightSelection(t *testing.T) {
	lights := make([]meshscene.RetainedLight, 10)
	for i := range lights {
		lights[i] = meshscene.RetainedLight{PositionRadius: [4]float32{50, 50, 0, 100}, ColorGroundGain: [4]float32{float32(i + 1), 0, 0, 1}, Flags: [4]uint32{1}}
	}
	lights[9].Flags[0] = 0
	ids, n := modelSelectLights(lights, [4]float32{0, 0, 100, 100})
	if n != 8 {
		t.Fatalf("count=%d", n)
	}
	seen := make(map[uint32]bool)
	for _, id := range ids {
		seen[id] = true
	}
	if seen[0] || seen[9] || !seen[8] {
		t.Fatalf("selection=%v", ids)
	}
	// Equal-score newcomers never displace an existing eighth source.
	for i := range lights {
		lights[i].ColorGroundGain[0] = 1
		lights[i].Flags[0] = 1
	}
	ids, n = modelSelectLights(lights, [4]float32{0, 0, 100, 100})
	if n != 8 || ids != [8]uint32{0, 1, 2, 3, 4, 5, 6, 7} {
		t.Fatalf("equal-score order=%v", ids)
	}
}
