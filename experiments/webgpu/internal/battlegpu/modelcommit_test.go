package battlegpu

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/wgpu"
)

func modelCommitCompile(t *testing.T, capacities [2]int) (*ir.Module, string) {
	t.Helper()
	source := modelCommitShaderSource(capacities)
	if strings.Contains(source, "__") {
		t.Fatal("unexpanded common commit capacity or helpers")
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
	if info.RequiresSizesBuffer || strings.Contains(code, "_buffer_sizes") || strings.Contains(code, "_mslBufferSizes") {
		t.Fatal("common render commit requires unavailable runtime-size operands")
	}
	return module, code
}

func TestModelCommonCommitMetalUnionAndCapacities(t *testing.T) {
	for _, capacities := range [][2]int{{1, 1}, {8, 5}, {21, 10}} {
		module, code := modelCommitCompile(t, capacities)
		layout := modelCommitLayoutEntries()
		if len(module.GlobalVariables) != len(layout) || len(module.EntryPoints) != 2 {
			t.Fatal("common commit must declare only the complete union and its two entry points")
		}
		buffer, texture := 0, 0
		for i, global := range module.GlobalVariables {
			if global.Binding == nil || global.Binding.Group != 0 || global.Binding.Binding != uint32(i) || layout[i].Binding != uint32(i) {
				t.Fatalf("common commit binding order differs at %d", i)
			}
			kind, slot := "buffer", buffer
			if layout[i].Texture != nil {
				kind, slot = "texture", texture
				texture++
			} else {
				buffer++
			}
			pattern := fmt.Sprintf(`\b%s \[\[%s\(%d\)\]\]`, global.Name, kind, slot)
			for _, entry := range []string{"model_commit_vertex", "model_commit_fragment"} {
				// The vertex consumes both storage/uniform families; the fragment
				// consumes all nine resources. Texture indices still include every
				// declaration even though the vertex reads none of them.
				if entry == "model_commit_vertex" && kind == "texture" {
					continue
				}
				if !regexp.MustCompile(pattern).MatchString(modelBodyEntry(t, code, entry)) {
					t.Fatalf("%s does not match dense layout %s", entry, pattern)
				}
			}
		}
		if buffer != 4 || texture != 6 {
			t.Fatalf("common native resources: buffers=%d textures=%d", buffer, texture)
		}
		for i, global := range module.GlobalVariables[:2] {
			array, ok := module.Types[global.Type].Inner.(ir.ArrayType)
			if !ok || array.Size.Constant == nil || int(*array.Size.Constant) != capacities[i] || int(array.Stride) != []int{modelSlotStride, modelShadowStride}[i] {
				t.Fatalf("fixed family %d does not follow its allocated record capacity", i)
			}
		}
		for _, index := range []int{2, 4} {
			uniform, ok := module.Types[module.GlobalVariables[index].Type].Inner.(ir.StructType)
			if !ok || uint64(uniform.Span) != layout[index].Buffer.MinBindingSize {
				t.Fatalf("common uniform binding %d differs from its existing 80/96-byte span", index)
			}
		}
		// Slots are read by the body vertex and fragment and by a reused
		// shadow subject's fragment; subjects by the shadow vertex and fragment.
		for i, name := range []string{"slots", "subjects"} {
			guard := `uint\([^)]+\) < ` + strconv.Itoa(capacities[i]) + ` \? ` + name + `\.inner\[`
			if len(regexp.MustCompile(guard).FindAllString(code, -1)) != []int{3, 2}[i] {
				t.Fatalf("%s vertex/fragment guards do not use capacity %d", name, capacities[i])
			}
		}
	}
}

func TestModelCommonCommitRetainsCallableEquations(t *testing.T) {
	for _, tc := range []struct {
		name, canonical, marker, callable string
		undo                              []string
	}{
		{"body", modelResolveShader, "const corners=", modelCommitBodyHelpers(), []string{
			"fn modelCommitBodyVertex(vid:u32,iid:u32)", "@vertex fn resolve_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)",
			"fn modelCommitBodyFragment(input:Out)->vec4<f32>", "@fragment fn resolve_fragment(input:Out)->@location(0) vec4<f32>",
			"mainU.", "u.",
		}},
		{"shadow", modelShadowCommitShader, "const modelShadowCorners=", modelCommitShadowHelpers(), []string{
			"fn modelCommitShadowVertex(vid:u32,iid:u32)", "@vertex fn model_shadow_commit_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)",
			"fn modelCommitShadowFragment(input:ModelShadowCommitOut)->vec4<f32>", "@fragment fn model_shadow_commit_fragment(input:ModelShadowCommitOut)->@location(0) vec4<f32>",
			"shadowU.", "u.",
		}},
	} {
		_, tail, found := strings.Cut(tc.canonical, tc.marker)
		if !found || strings.Contains(tc.callable, "@vertex") || strings.Contains(tc.callable, "@fragment") || strings.Contains(tc.callable, "@group") {
			t.Fatalf("%s helpers include entry declarations or lack canonical equations", tc.name)
		}
		if got := strings.NewReplacer(tc.undo...).Replace(tc.callable); got != tc.marker+tail {
			t.Fatalf("%s helper conversion changed executable equations", tc.name)
		}
	}
}

func TestModelCommonCommitDispatchBeforeRecordLoads(t *testing.T) {
	_, code := modelCommitCompile(t, [2]int{8, 5})
	for _, name := range []string{"model_commit_vertex", "model_commit_fragment"} {
		at := strings.Index(code, name+"(")
		if at < 0 {
			t.Fatal("missing common entry", name)
		}
		entry := code[at:]
		if end := strings.Index(entry, "\n}\n"); end >= 0 {
			entry = entry[:end]
		}
		branch := strings.Index(entry, "< modelCommitSlotCapacity")
		bodyCall := strings.Index(entry, "modelCommitBody")
		shadowCall := strings.Index(entry, "modelCommitShadow")
		if branch < 0 || bodyCall < branch || shadowCall < bodyCall || !strings.Contains(entry, "- modelCommitSlotCapacity") || strings.Contains(entry, ".inner[") || !strings.Contains(code, "modelCommitSlotCapacity = 8u;") {
			t.Fatalf("%s must decode capacity before invoking guarded family helpers:\n%s", name, entry)
		}
	}
	for _, line := range strings.Split(code, "\n") {
		if (strings.Contains(line, "corners.inner[") || strings.Contains(line, "modelShadowCorners.inner[")) && strings.Contains(line, "*") {
			t.Fatal("common pipeline reintroduced guarded-corner precedence failure", line)
		}
	}
}

func TestModelCommonCommitFirstInstancePartition(t *testing.T) {
	for _, bytes := range [][2]uint64{{0, 0}, {64, 128}, {1024, 512}, {modelCoreBufferLimit, modelCoreBufferLimit}} {
		capacities := modelCommitCapacities(bytes[0], bytes[1])
		for i, stride := range []int{modelSlotStride, modelShadowStride} {
			if bytes[i] != 0 && (uint64(capacities[i]*stride) > bytes[i] || uint64((capacities[i]+1)*stride) <= bytes[i]) {
				t.Fatalf("family %d capacity %d does not follow whole allocated records", i, capacities[i])
			}
		}
		// Body callers keep slot-1. The final legal body record is strictly
		// below the discriminator; the first and last subject decode unchanged.
		bodyLast := uint32(capacities[0] - 1)
		shadowFirst := modelCommitShadowIndex(capacities[0], 0)
		shadowLast := modelCommitShadowIndex(capacities[0], uint32(capacities[1]-1))
		if bodyLast >= shadowFirst || shadowFirst != uint32(capacities[0]) || shadowLast-shadowFirst != uint32(capacities[1]-1) || uint64(shadowLast) != uint64(capacities[0])+uint64(capacities[1])-1 {
			t.Fatal("capacity discriminator changed identities, overlapped or overflowed")
		}
	}
}

func TestModelCommonCommitReplacementFailureAndLifetime(t *testing.T) {
	failure := errors.New("authored common commit creation failure")
	for failAt := 0; failAt <= 2; failAt++ {
		var released []int
		old := modelCommitPipeline{module: &wgpu.ShaderModule{}, pipeline: &wgpu.RenderPipeline{}, capacities: [2]int{8, 5}}
		for i := range old.releases {
			old.releases[i] = func() { released = append(released, 10+i) }
		}
		current := old
		err := current.modelReplace(func(next *modelCommitPipeline) error {
			next.capacities = [2]int{21, 10}
			for i := range next.releases {
				if i == failAt {
					return failure
				}
				next.releases[i] = func() { released = append(released, i) }
			}
			return failure
		})
		if !errors.Is(err, failure) || current.module != old.module || current.pipeline != old.pipeline || current.capacities != old.capacities {
			t.Fatal("failed creation changed the current common pipeline")
		}
		var want []int
		for i := failAt - 1; i >= 0; i-- {
			want = append(want, i)
		}
		if !reflect.DeepEqual(released, want) {
			t.Fatalf("failure %d releases=%v want reverse partial %v", failAt, released, want)
		}
	}
	var released []int
	var current modelCommitPipeline
	// Exercise separate family capacity growth, not a shared arbitrary tag.
	for generation, capacities := range [][2]int{{1, 1}, {8, 1}, {8, 5}} {
		if err := current.modelReplace(func(next *modelCommitPipeline) error {
			next.module, next.pipeline, next.capacities = &wgpu.ShaderModule{}, &wgpu.RenderPipeline{}, capacities
			for i := range next.releases {
				next.releases[i] = func() { released = append(released, generation*10+i) }
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(released, []int{1, 0, 11, 10}) || current.capacities != [2]int{8, 5} {
		t.Fatal("growth retained obsolete pairs or prematurely released the current pair")
	}
	current.modelClose()
	current.modelClose()
	if !reflect.DeepEqual(released, []int{1, 0, 11, 10, 21, 20}) || current.module != nil || current.pipeline != nil {
		t.Fatal("Close leaked, double-released or released module before pipeline")
	}
}
