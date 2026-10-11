package battlegpu

import (
	"errors"
	"os"
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

func layerABICompileTest(t *testing.T, source string) (*ir.Module, string) {
	t.Helper()
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
	code, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer || strings.Contains(code, "_mslBufferSizes") || strings.Contains(code, "_buffer_sizes") {
		t.Fatal("render module retains an unbound hidden size operand")
	}
	uniform := false
	for _, typ := range module.Types {
		if typ.Name == "LayerUniform" {
			uniform = true
			structure, ok := typ.Inner.(ir.StructType)
			if !ok || structure.Span != layerUniformSize {
				t.Fatal("shared 240-byte uniform ABI changed", typ.Inner)
			}
		}
	}
	if !uniform {
		t.Fatal("shared uniform declaration absent")
	}
	return module, code
}

func layerABIEntryTest(t *testing.T, code, name string) string {
	t.Helper()
	start := regexp.MustCompile(`(?m)^(vertex|fragment) [^\n]+ ` + name + `\(`).FindStringIndex(code)
	if start == nil {
		t.Fatal("native entry absent", name)
	}
	entry := code[start[0]:]
	return entry[:strings.Index(entry, "\n}")]
}

func layerABIResourcesTest(t *testing.T, code string, expected [][2]string) {
	t.Helper()
	if count := len(regexp.MustCompile(`(?m)^(vertex|fragment) [^\n]+ layer_\w+\(`).FindAllString(code, -1)); count != len(expected) {
		t.Fatal("reachable layer entry inventory changed", count, len(expected))
	}
	resource := regexp.MustCompile(`(\w+) \[\[(buffer|texture)\((\d+)\)\]\]`)
	for _, item := range expected {
		entry := layerABIEntryTest(t, code, item[0])
		signature := entry[:strings.Index(entry, ") {")]
		var resources []string
		for _, m := range resource.FindAllStringSubmatch(signature, -1) {
			resources = append(resources, m[1]+":"+m[2][:1]+m[3])
		}
		if got := strings.Join(resources, ","); got != item[1] {
			t.Fatalf("%s native resources=%s want %s", item[0], got, item[1])
		}
	}
}

func TestLayerABIOverlayEveryNativeEntryAndAllocationGuard(t *testing.T) {
	resources := [][2]string{
		{"layer_annotation_vertex", "layerU:b0,layerQuads:b1"}, {"layer_annotation_fragment", ""},
		{"layer_quad_vertex", "layerU:b0,layerQuads:b1"}, {"layer_triangle_vertex", "layerU:b0,layerVertices:b2"},
		{"layer_quad_fragment", "layerAtlas:t0,layerTerrain:t1,layerFog:t2"}, {"layer_geometry_fragment", "layerAtlas:t0"},
		{"layer_multiply_fragment", "layerAtlas:t0,layerTerrain:t1,layerFog:t2"}, {"layer_overlay_multiply_fragment", "layerAtlas:t0,layerTerrain:t1,layerFog:t2"},
		{"layer_ground_vertex", "layerU:b0,layerGrounds:b3"}, {"layer_trail_fragment", ""}, {"layer_ground_fragment", "layerU:b0,layerWaterMask:t3"},
		{"layer_smoke_vertex", "layerU:b0,layerSmokes:b4,layerLights:b5"}, {"layer_smoke_fragment", "layerAtlas:t0"},
	}
	for _, capacities := range [][5]int{{1, 1, 1, 1, 1}, {2, 4, 8, 16, 32}, {128, 64, 32, 16, 8}, {256, 1, 1, 1, 1}} {
		module, code := layerABICompileTest(t, layerABIOverlaySource(capacities))
		if len(module.EntryPoints) != len(resources) {
			t.Fatal("reachable overlay entry inventory changed")
		}
		layerABIResourcesTest(t, code, resources)
		for i, name := range [...]string{"layerQuads", "layerVertices", "layerGrounds", "layerSmokes", "layerLights"} {
			found := false
			for _, global := range module.GlobalVariables {
				if global.Name != name {
					continue
				}
				found = true
				array, ok := module.Types[global.Type].Inner.(ir.ArrayType)
				stride := [5]uint32{64, layerDirectVertexStride, 80, layerSmokeStride, 64}[i]
				if !ok || array.Size.Constant == nil || *array.Size.Constant != uint32(capacities[i]) || array.Stride != stride || global.Binding == nil || global.Binding.Group != 0 || global.Binding.Binding != uint32(i+1) {
					t.Fatal("fixed storage declaration no longer matches allocated records", name, global, array)
				}
			}
			if !found || !strings.Contains(code, "< "+strconv.Itoa(capacities[i])+" ? "+name+".inner[") {
				t.Fatal("native load does not use allocated tier", name, capacities)
			}
		}
		for _, vertex := range []string{"layer_annotation_vertex", "layer_quad_vertex", "layer_triangle_vertex", "layer_ground_vertex", "layer_smoke_vertex"} {
			for _, line := range strings.Split(layerABIEntryTest(t, code, vertex), "\n") {
				if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
					t.Fatal("conditional storage read reached position arithmetic", vertex, line)
				}
			}
		}
	}
}

func TestLayerABISpriteTerrainFogEveryNativeEntry(t *testing.T) {
	for _, capacity := range []int{1, 2, 32, 256} {
		_, code := layerABICompileTest(t, layerABISpriteSource(capacity))
		layerABIResourcesTest(t, code, [][2]string{{"layer_sprite_vertex", "layerU:b0,layerSprites:b1"}, {"layer_sprite_fragment", "layerAtlas:t0,layerDetail:t1"}})
		if !strings.Contains(code, "< "+strconv.Itoa(capacity)+" ? layerSprites.inner[") {
			t.Fatal("sprite guard differs from its allocated tier", capacity)
		}
	}
	_, terrain := layerABICompileTest(t, layerTerrainShader)
	layerABIResourcesTest(t, terrain, [][2]string{
		{"layer_terrain_vertex", "layerU:b0"},
		{"layer_seabed_fragment", "layerU:b0,layerTiles:t0,layerLookup:t1,layerDetail:t2,layerFallback:t3"},
		{"layer_terrain_fragment", "layerU:b0,layerTiles:t0,layerLookup:t1,layerDetail:t2,layerFallback:t3,layerGroundField:t4,layerWaterMask:t5,layerSeabed:t6"},
	})
	_, fog := layerABICompileTest(t, layerFogShader)
	layerABIResourcesTest(t, fog, [][2]string{{"layer_fog_vertex", ""}, {"layer_fog_fragment", "layerU:b0,layerWorld:t0,layerGrid:t1,layerMasks:t2,layerGlow:t3"}})
}

func TestLayerABIUsesAllocatedTiersAndSeparateQuadOwners(t *testing.T) {
	l := Layers{
		overlayQuads: layerBuffer{size: 64 * 32}, effectQuads: layerBuffer{size: 64 * 2}, annotations: layerBuffer{size: 64 * 8},
		effectVertices: layerBuffer{size: layerDirectVertexStride * 4}, effectGround: layerBuffer{size: 80 * 8}, effectSmoke: layerBuffer{size: layerSmokeStride * 16}, lights: layerBuffer{size: 64 * 64},
	}
	if got := l.layerABIOverlayCapacities(&l.overlayQuads); got != [5]int{32, 4, 8, 16, 64} {
		t.Fatal("foreground uses another quad owner or byte count", got)
	}
	if got := l.layerABIOverlayCapacities(&l.effectQuads); got != [5]int{2, 4, 8, 16, 64} {
		t.Fatal("effects use another quad owner or truncate lights to the selected eight", got)
	}
	if got := l.layerABIOverlayCapacities(&l.annotations); got[0] != 8 {
		t.Fatal("annotation quad tier aliases another owner", got)
	}
}

func TestLayerABISpriteKeyKeepsBedLazyAndRefreshesCreatedBedOnGrowth(t *testing.T) {
	l := Layers{sprites: layerBuffer{size: 96 * 4}}
	if got := l.layerABISpriteKey(); got != (layerABIKey{capacities: [5]int{4}}) {
		t.Fatal("inactive bed creates a native sprite variant", got)
	}
	l.seabed.active = true
	if got := l.layerABISpriteKey(); got != (layerABIKey{capacities: [5]int{4}, seabed: true}) {
		t.Fatal("first admitted bed uses a different sprite tier", got)
	}
	l.seabed.active, l.seabed.sprite = false, &wgpu.RenderPipeline{}
	l.sprites.size *= 2
	if got := l.layerABISpriteKey(); got != (layerABIKey{capacities: [5]int{8}, seabed: true}) {
		t.Fatal("ordinary, feature and existing bed variants no longer grow together", got)
	}
}

func TestLayerABIFailedReplacementKeepsCurrentAndFreesCandidate(t *testing.T) {
	for failAfter := 0; failAfter <= 7; failAfter++ {
		var events []string
		old := &wgpu.RenderPipeline{}
		current := layerABIBundle{key: layerABIKey{capacities: [5]int{1}}, pipelines: [7]*wgpu.RenderPipeline{old}, releases: []func(){func() { events = append(events, "old module") }, func() { events = append(events, "old pipeline") }}}
		failure := errors.New("authored creation failure")
		err := layerABIReplace(&current, layerABIKey{capacities: [5]int{2}}, func(b *layerABIBundle) error {
			for i := 0; i < failAfter; i++ {
				b.releases = append(b.releases, func() { events = append(events, strconv.Itoa(i)) })
			}
			return failure
		}, func(*layerABIBundle) { t.Fatal("failed family published") })
		if err != failure || current.key.capacities[0] != 1 || current.pipelines[0] != old || len(current.releases) != 2 {
			t.Fatal("failed creation destroyed or replaced current family", err, current)
		}
		var want []string
		for i := failAfter - 1; i >= 0; i-- {
			want = append(want, strconv.Itoa(i))
		}
		if !reflect.DeepEqual(events, want) {
			t.Fatal("partial candidate cleanup order changed", events, want)
		}
		current.close()
		current.close()
		want = append(want, "old pipeline", "old module")
		if !reflect.DeepEqual(events, want) {
			t.Fatal("current owner leaked or released twice", events, want)
		}
	}
}

func TestLayerABISuccessPublishesBeforeFreeAndKeepsOnlyCurrent(t *testing.T) {
	var events []string
	key := layerABIKey{capacities: [5]int{2}}
	current := layerABIBundle{key: layerABIKey{capacities: [5]int{1}}, releases: []func(){func() { events = append(events, "old") }}}
	for tier := 2; tier <= 32; tier *= 2 {
		key.capacities[0] = tier
		if err := layerABIReplace(&current, key, func(b *layerABIBundle) error {
			b.releases = append(b.releases, func() { events = append(events, "module") }, func() { events = append(events, "pipeline") })
			return nil
		}, func(b *layerABIBundle) {
			if b.key != key || current.key != key || len(current.releases) != 2 {
				t.Fatal("new owner not installed before publication")
			}
			events = append(events, "publish")
		}); err != nil {
			t.Fatal(err)
		}
		if len(current.releases) != 2 {
			t.Fatal("old tiers retained by current family")
		}
	}
	want := []string{"publish", "old"}
	for i := 0; i < 4; i++ {
		want = append(want, "publish", "pipeline", "module")
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatal("release precedes publication or old tiers retained", events, want)
	}
	if err := layerABIReplace(&current, key, func(*layerABIBundle) error { t.Fatal("unchanged tier rebuilt"); return nil }, func(*layerABIBundle) { t.Fatal("unchanged family republished") }); err != nil {
		t.Fatal(err)
	}
	l := Layers{layerABI: layerABIFamilies{effects: current}, owned: []func(){func() { events = append(events, "static") }}}
	l.Close()
	l.Close()
	want = append(want, "pipeline", "module", "static")
	if !reflect.DeepEqual(events, want) {
		t.Fatal("Close leaks, repeats, or frees shared layouts before current pipelines", events, want)
	}
}

func TestLayerABIUpdatesAfterUploadsAndKeepsSeabedLazy(t *testing.T) {
	data, err := os.ReadFile("layers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	prepare := source[strings.Index(source, "func (l *Layers) Prepare("):]
	prepare = prepare[:strings.Index(prepare, "\nfunc ")]
	for _, ordered := range [][2]string{{"l.layerUpload(&l.sprites", "l.layerPrepareSeabed("}, {"l.layerPrepareSeabed(", "l.layerUpdateSpritePipelines()"}, {"l.layerUpload(&l.overlayQuads", "l.layerUpdateOverlayPipelines()"}, {"l.layerPrepareEffects(f)", "l.layerUpdateOverlayPipelines()"}} {
		a, b := strings.Index(prepare, ordered[0]), strings.Index(prepare, ordered[1])
		if a < 0 || b < 0 || a >= b {
			t.Fatal("fixed tier published before its upload/admission", ordered)
		}
	}
	seabed, err := os.ReadFile("seabed.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(seabed), "layerModule(layerSpriteShader)") || strings.Contains(source, "layerModule(layerOverlayShader)") || strings.Contains(source, "layerModule(layerSpriteShader)") {
		t.Fatal("a render path bypasses current fixed-capacity families")
	}
}
