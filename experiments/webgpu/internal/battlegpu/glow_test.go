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

func TestGlowShaderLowersToCoreSPIRVAndMSLWithPackedABI(t *testing.T) {
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
	for _, name := range []string{"glow_source_vertex", "glow_source_fragment", "glow_shrink", "glow_across", "glow_down"} {
		if info.EntryPointNames[name] == "" {
			t.Fatal("missing shader entry point", name)
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
