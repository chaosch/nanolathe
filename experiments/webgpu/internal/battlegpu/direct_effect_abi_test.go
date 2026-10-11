package battlegpu

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
)

func TestLayerDirectVertexFixedCapacityMetalABI(t *testing.T) {
	for _, capacity := range []int{1, 2, 8, 32, 128} {
		source := layerDirectShaderSource(capacity)
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
		for _, typ := range module.Types {
			if typ.Name == "LayerVertex" {
				s, ok := typ.Inner.(ir.StructType)
				if !ok || s.Span != layerDirectVertexStride {
					t.Fatal("direct corner ABI changed", typ.Inner)
				}
			}
		}
		code, _, err := msl.Compile(module, msl.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(code, "vertex layer_triangle_vertexOutput layer_triangle_vertex(")
		if start < 0 {
			t.Fatal("native direct vertex entry absent")
		}
		entry := code[start:]
		entry = entry[:strings.Index(entry, "\n}")]
		signature := entry[:strings.Index(entry, ") {")]
		if strings.Contains(entry, "_buffer_sizes") || !strings.Contains(signature, "layerU [[buffer(0)]]") || !strings.Contains(signature, "layerVertices [[buffer(2)]]") || strings.Contains(signature, "[[buffer(5)]]") {
			t.Fatal("native direct resources do not match the shared layout", signature)
		}
		if !strings.Contains(entry, "< "+strconv.Itoa(capacity)+" ? layerVertices.inner[") {
			t.Fatal("native direct load is not guarded by the uploaded buffer tier", entry)
		}
		for _, line := range strings.Split(entry, "\n") {
			if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
				t.Fatal("conditional corner load reached position arithmetic", line)
			}
		}
	}
}
