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
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestGlowSourceShaderLowersToCoreSPIRVAndMSLWithPackedABI(t *testing.T) {
	if _, err := naga.Compile(glowShader); err != nil {
		t.Fatal(err)
	}
	ast, err := naga.Parse(glowShader)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.Lower(ast)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GlowUniform", "GlowVertex"} {
		found := false
		for _, typ := range module.Types {
			if typ.Name != name {
				continue
			}
			found = true
			structure, ok := typ.Inner.(ir.StructType)
			want := uint32(glowVertexSize)
			if name == "GlowUniform" {
				want = glowUniformSize
			}
			if !ok || structure.Span != want {
				t.Fatal("packed shader stride changed", name, structure)
			}
			for i, member := range structure.Members {
				if member.Offset != uint32(i*16) {
					t.Fatal("packed vector offset changed", name, member)
				}
			}
		}
		if !found {
			t.Fatal("missing packed shader type", name)
		}
	}
	source, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"glow_source_vertex", "glow_source_fragment"} {
		if info.EntryPointNames[name] == "" {
			t.Fatal("missing shader entry point", name)
		}
	}
	if len(info.EntryPointNames) != 2 {
		t.Fatal("compute entry point reached source resource map", info.EntryPointNames)
	}
	// Match native render's default, per-type resource numbering: the layout
	// has uniform/vertices and atlas/completed in precisely these slots.
	for _, binding := range []string{"glowU [[buffer(0)]]", "glowVertices [[buffer(1)]]", "glowAtlas [[texture(0)]]", "glowCompleted [[texture(1)]]"} {
		if !strings.Contains(source, binding) {
			t.Fatal("native source binding moved", binding)
		}
	}
	// The pinned compiler's bounds-check conditional has insufficient
	// precedence when an indexed vector feeds arithmetic. Loading the record
	// into a local isolates that conditional from the clip-space expression.
	for _, line := range strings.Split(source, "\n") {
		if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
			t.Fatal("indexed conditional reached position arithmetic", line)
		}
	}
}

func TestGlowBlurNativeMSLResourceMapContainsOnlyComputeBindings(t *testing.T) {
	if _, err := naga.Compile(glowBlurShader); err != nil {
		t.Fatal(err)
	}
	ast, err := naga.Parse(glowBlurShader)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.Lower(ast)
	if err != nil {
		t.Fatal(err)
	}
	// Native compute binds a sizes buffer after the layout's one uniform
	// only when the module has runtime arrays. Source globals in this module
	// previously collided at buffer 1 and shifted input/output to textures
	// 2/3 even though the compute encoder bound its two textures at 0/1.
	if len(module.GlobalVariables) != 3 {
		t.Fatal("unrelated globals reached compute resource map", module.GlobalVariables)
	}
	for _, typ := range module.Types {
		if array, ok := typ.Inner.(ir.ArrayType); ok && array.Size.Constant == nil {
			t.Fatal("source runtime array reached compute module")
		}
		if typ.Name == "GlowUniform" {
			structure, ok := typ.Inner.(ir.StructType)
			if !ok || structure.Span != glowUniformSize {
				t.Fatal("compute uniform ABI changed", structure)
			}
		}
	}
	source, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer || strings.Contains(source, "_mslBufferSizes") || strings.Contains(source, "_buffer_sizes") {
		t.Fatal("compute requested unrelated runtime-array metadata")
	}
	if strings.Contains(source, "glow_source_vertex") || strings.Contains(source, "glow_source_fragment") || len(info.EntryPointNames) != 3 {
		t.Fatal("source entry point reached compute resource map", info.EntryPointNames)
	}
	for _, name := range []string{"glow_shrink", "glow_across", "glow_down"} {
		if info.EntryPointNames[name] == "" {
			t.Fatal("missing compute entry point", name)
		}
	}
	for _, binding := range []string{"glowU [[buffer(0)]]", "glowInput [[texture(0)]]", "glowDestination [[texture(1)]]"} {
		if !strings.Contains(source, binding) {
			t.Fatal("native compute binding differs from its group", binding)
		}
	}
}

func TestGlowPublicationCornerPacketsAndOperandsRemainVerbatim(t *testing.T) {
	in := make([]meshscene.RetainedGlowVertex, 16)
	for i := range in {
		in[i] = meshscene.RetainedGlowVertex{PositionUV: [4]float32{float32(i) + .5, -2.25, 7.5, 13}, Color: [4]float32{.25, .5, .75, .16}, Params: [4]float32{float32(i / 4), .65, 25, .5}}
	}
	before := slices.Clone(in)
	data, err := glowPackVertices(in)
	if err != nil || len(data) != len(in)*48 {
		t.Fatal("production corner packet changed", len(data), err)
	}
	for i, v := range in {
		for lane, value := range [3][4]float32{v.PositionUV, v.Color, v.Params} {
			for component, x := range value {
				if got := math.Float32frombits(binary.LittleEndian.Uint32(data[i*48+lane*16+component*4:])); got != x {
					t.Fatal("source operand changed", i, lane, component, got, x)
				}
			}
		}
	}
	if !slices.Equal(in, before) {
		t.Fatal("borrowed publication mutated")
	}
	if _, err := glowPackVertices(in[:6]); err == nil {
		t.Fatal("uncompacted recorder topology silently accepted")
	}
	in[7].Params[0] = 4
	if _, err := glowPackVertices(in); err == nil || !strings.Contains(err.Error(), "1 unsupported source vertices") {
		t.Fatal("unsupported operation not reported", err)
	}
}

func TestGlowUniformRetainsKernelAndBorderedOctaveSizes(t *testing.T) {
	p := meshscene.RetainedGlowParameters{Weights: [8]float32{.1, .2, .3, .4, .5, .6, .7, .8}, Blur: [4]float32{.75, 4.65, .55, .425}}
	u := glowPackUniform(p, 17, 9)
	for i, x := range append(p.Weights[:], p.Blur[:]...) {
		if got := math.Float32frombits(binary.LittleEndian.Uint32(u[i*4:])); got != x {
			t.Fatal("producer kernel operand changed", i, got, x)
		}
	}
	want := [4]uint32{5, 3, 3, 2}
	if glowSizes(17, 9) != want {
		t.Fatal("partial edge block lost")
	}
	for i, x := range want {
		if binary.LittleEndian.Uint32(u[64+i*4:]) != x {
			t.Fatal("bordered octave shape changed")
		}
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(u[48:])) != 17 || math.Float32frombits(binary.LittleEndian.Uint32(u[52:])) != 9 {
		t.Fatal("physical viewport changed")
	}
}

func TestGlowEmptyFrameCostsNoPassAndDisablesStaleOutput(t *testing.T) {
	g := &Glow{enabled: true}
	if err := g.Prepare(meshscene.LiveFrame{}, 17, 9); err != nil {
		t.Fatal(err)
	}
	if g.Enabled() || g.Output() != nil || g.Counters() != (GlowCounters{}) {
		t.Fatal("empty frame retains active output", g.Counters())
	}
	if err := g.Encode(nil, nil); err != nil {
		t.Fatal("empty frame encoded a pass", err)
	}
	g.Close()
	if g.Enabled() || g.Encode(nil, nil) == nil {
		t.Fatal("closed component admitted encode")
	}
}
