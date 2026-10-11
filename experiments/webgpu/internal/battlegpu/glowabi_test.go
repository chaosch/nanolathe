package battlegpu

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/msl"
)

func TestGlowSourceNativeCapacityAndBindings(t *testing.T) {
	for _, capacity := range []int{1, 4, 512, 4096} {
		source := glowSourceShader(capacity)
		ast, err := naga.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		ir, err := naga.LowerWithCapabilities(ast, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		code, info, err := msl.Compile(ir, msl.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		if info.RequiresSizesBuffer || strings.Contains(code, "_buffer_sizes") || strings.Contains(code, "_mslBufferSizes") {
			t.Fatal("glow source requires unbound render metadata")
		}
		for _, binding := range []string{"glowU [[buffer(0)]]", "glowVertices [[buffer(1)]]", "glowAtlas [[texture(0)]]", "glowCompleted [[texture(1)]]"} {
			if !strings.Contains(code, binding) {
				t.Fatal("native glow binding changed", binding)
			}
		}
		if !strings.Contains(code, "< "+strconv.Itoa(capacity)+" ? glowVertices.inner[") {
			t.Fatal("guard does not use allocated tier")
		}
	}
}
