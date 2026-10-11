package battlegpu

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/wgpu"
)

func modelBodyCompile(t *testing.T, source string) (*ir.Module, string, msl.TranslationInfo) {
	t.Helper()
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
	return module, code, info
}

func modelBodyEntry(t *testing.T, code, name string) string {
	t.Helper()
	pattern := `(?s)(?:vertex|fragment) [[:alnum:]_]+ ` + name + `\(.*?\n\) \{`
	entry := regexp.MustCompile(pattern).FindString(code)
	if entry == "" {
		t.Fatalf("missing emitted body entry %s", name)
	}
	return entry
}

func TestModelBodyReproducesRuntimeSizeGapAndRemovesIt(t *testing.T) {
	// Render HAL compileLibrary uses DefaultOptions and has no sizes upload.
	// Reproduce that exact prior ABI without assigning an implicit native slot.
	runtimeSource := modelBodyShader
	for _, record := range modelBodyArrays {
		prefix := record.name + ":array<" + record.element
		runtimeSource = strings.ReplaceAll(runtimeSource, prefix+",1>", prefix+">")
	}
	_, oldCode, oldInfo := modelBodyCompile(t, runtimeSource)
	if !oldInfo.RequiresSizesBuffer {
		t.Fatal("runtime body no longer reproduces the missing-size ABI")
	}
	for _, entry := range []string{"model_vertex", "model_fragment", "outline_fragment", "model_shadow_fragment", "model_shadow_outline"} {
		signature := modelBodyEntry(t, oldCode, entry)
		if !strings.Contains(signature, "constant _mslBufferSizes& _buffer_sizes") || strings.Contains(signature, "_buffer_sizes [[buffer(") {
			t.Fatalf("%s did not reproduce the unqualified hidden argument", entry)
		}
	}
	for _, field := range []int{0, 1, 2, 6, 7, 8, 9, 11} {
		if !strings.Contains(oldCode, "_buffer_sizes.size"+strconv.Itoa(field)+" -") {
			t.Fatalf("prior body does not guard runtime field %d", field)
		}
	}
	for _, capacities := range [][8]int{{1, 1, 1, 1, 1, 1, 1, 1}, {2, 3, 5, 7, 11, 13, 17, 19}} {
		_, code, info := modelBodyCompile(t, modelBodyShaderSource(capacities))
		if info.RequiresSizesBuffer || strings.Contains(code, "_mslBufferSizes") || strings.Contains(code, "_buffer_sizes") {
			t.Fatal("fixed body entries retain missing runtime-size metadata")
		}
		for i, record := range modelBodyArrays {
			guard := `uint\([^)]+\) < ` + strconv.Itoa(capacities[i]) + ` \? ` + record.name + `\.inner\[`
			if !regexp.MustCompile(guard).MatchString(code) {
				t.Fatalf("%s missing fixed guard capacity %d", record.name, capacities[i])
			}
		}
	}
}

func TestModelBodyFixedCapacityEntryBindings(t *testing.T) {
	capacities := [8]int{2, 3, 5, 7, 11, 13, 17, 19}
	source := modelBodyShaderSource(capacities)
	module, code, _ := modelBodyCompile(t, source)
	layout := modelBodyLayoutEntries()
	if len(module.GlobalVariables) != len(layout) {
		t.Fatal("body declaration prefix does not match its complete layout")
	}
	for i, global := range module.GlobalVariables {
		entry := layout[i]
		if global.Binding == nil || global.Binding.Group != 0 || global.Binding.Binding != uint32(i) || entry.Binding != uint32(i) {
			t.Fatalf("body declaration/layout order differs at %d", i)
		}
		if entry.Buffer != nil && entry.Buffer.Type == gputypes.BufferBindingTypeReadOnlyStorage {
			if entry.Buffer.MinBindingSize != 0 {
				t.Fatal("body storage capacity validation was not deferred")
			}
			array, ok := module.Types[global.Type].Inner.(ir.ArrayType)
			if !ok || array.Size.Constant == nil {
				t.Fatalf("body %s is not a fixed array", global.Name)
			}
			found := false
			for j, record := range modelBodyArrays {
				if global.Name == record.name {
					found = true
					if *array.Size.Constant != uint32(capacities[j]) || array.Stride != uint32(record.stride) {
						t.Fatalf("body %s changed its fixed count or record stride", global.Name)
					}
				}
			}
			if !found {
				t.Fatalf("unaccounted body storage %s", global.Name)
			}
		} else if entry.Buffer != nil && (entry.Buffer.Type != gputypes.BufferBindingTypeUniform || entry.Buffer.MinBindingSize != 80) {
			t.Fatal("uniform layout changed with storage capacity")
		}
	}
	for _, tc := range []struct {
		name      string
		resources []string
	}{
		{"model_vertex", []string{"vertices buffer 0", "poses buffer 1", "instances buffer 2", "u buffer 3", "materials buffer 4", "frames buffer 5", "selected buffer 6", "lights buffer 7", "centers buffer 8"}},
		{"model_fragment", []string{"instances buffer 2", "u buffer 3", "atlas texture 0", "palette texture 1", "nearestSampler sampler 0"}},
		{"outline_fragment", []string{"instances buffer 2", "palette texture 1"}},
		{"model_shadow_fragment", []string{"instances buffer 2", "u buffer 3", "atlas texture 0", "palette texture 1", "nearestSampler sampler 0"}},
		{"model_shadow_outline", []string{"instances buffer 2", "palette texture 1"}},
	} {
		signature := modelBodyEntry(t, code, tc.name)
		if len(regexp.MustCompile(`\[\[(buffer|texture|sampler)\(`).FindAllString(signature, -1)) != len(tc.resources) {
			t.Fatalf("%s has an unexpected native resource argument", tc.name)
		}
		for _, resource := range tc.resources {
			parts := strings.Fields(resource)
			pattern := fmt.Sprintf(`\b%s \[\[%s\(%s\)\]\]`, parts[0], parts[1], parts[2])
			if !regexp.MustCompile(pattern).MatchString(signature) {
				t.Fatalf("%s does not preserve %s", tc.name, resource)
			}
		}
	}
	// Specialization changes declarations only; key, projection and shading
	// equations and all ordinary/solo-shadow entry bodies remain verbatim.
	if source[strings.Index(source, "struct Out {"):] != modelBodyShader[strings.Index(modelBodyShader, "struct Out {"):] {
		t.Fatal("body specialization changed executable equations")
	}
}

func TestModelBodyCapacitiesFollowWholeAllocatedRecords(t *testing.T) {
	if got := modelBodyCapacities([8]uint64{}); got != [8]int{1, 1, 1, 1, 1, 1, 1, 1} {
		t.Fatal("initial unbound dynamic arrays do not have valid single-record types")
	}
	for _, tier := range []uint64{512, 1024, 4096, modelCoreBufferLimit} {
		var bytes [8]uint64
		for i := range bytes {
			bytes[i] = tier
		}
		capacities := modelBodyCapacities(bytes)
		for i, record := range modelBodyArrays {
			if uint64(capacities[i])*record.stride > tier || uint64(capacities[i]+1)*record.stride <= tier {
				t.Fatalf("%s count %d differs from whole buffer records in tier %d", record.name, capacities[i], tier)
			}
		}
	}
	// Immutable meshes use exact spans, while dynamic instances/poses use
	// geometric tiers, including the appended independent solo-shadow records.
	bytes := [8]uint64{11 * 80, 2048, 8192, 7 * 16, 13 * 16, 4096, 512, 19 * 16}
	if got := modelBodyCapacities(bytes); got != [8]int{11, 25, 19, 7, 13, 85, 8, 19} {
		t.Fatalf("immutable/grown capacity mapping=%v", got)
	}
}

func TestModelBodyPipelineReplacementFailureAndReleaseOrder(t *testing.T) {
	failure := errors.New("authored pipeline creation failure")
	for failAt := 0; failAt <= 5; failAt++ {
		var released []int
		old := modelBodyPipelines{module: &wgpu.ShaderModule{}, capacities: [8]int{3}}
		for i := range old.releases {
			old.releases[i] = func() { released = append(released, 10+i) }
		}
		current := old
		err := current.modelReplace(func(next *modelBodyPipelines) error {
			next.module, next.capacities = &wgpu.ShaderModule{}, [8]int{7}
			for i := range next.releases {
				if i == failAt {
					return failure
				}
				next.releases[i] = func() { released = append(released, i) }
			}
			return failure
		})
		if !errors.Is(err, failure) || current.module != old.module || current.capacities != old.capacities {
			t.Fatal("failed body creation replaced the old current set")
		}
		var want []int
		for i := min(failAt, 5) - 1; i >= 0; i-- {
			want = append(want, i)
		}
		if !reflect.DeepEqual(released, want) {
			t.Fatalf("failure %d releases=%v want reverse partial %v", failAt, released, want)
		}
	}
	var released []int
	var current modelBodyPipelines
	for generation := 0; generation < 2; generation++ {
		if err := current.modelReplace(func(next *modelBodyPipelines) error {
			next.module, next.capacities = &wgpu.ShaderModule{}, [8]int{generation + 1}
			for i := range next.releases {
				next.releases[i] = func() { released = append(released, generation*10+i) }
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(released, []int{4, 3, 2, 1, 0}) || current.capacities != [8]int{2} {
		t.Fatal("successful replacement retained or prematurely closed a current set")
	}
	current.modelClose()
	current.modelClose()
	if !reflect.DeepEqual(released, []int{4, 3, 2, 1, 0, 14, 13, 12, 11, 10}) || current.module != nil {
		t.Fatal("body Close leaked, double-released or closed its module before pipelines")
	}
}
