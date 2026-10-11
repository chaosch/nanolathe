package battlegpu

import (
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestModelCommitMetalUnionBindings(t *testing.T) {
	ast, err := naga.Parse(modelResolveShaderSource(8))
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.LowerWithCapabilities(ast, modelResolveShaderSource(8), 0)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	layout := modelCommitLayoutEntries()
	if len(module.GlobalVariables) != len(layout) || strings.Contains(code, "_mslBufferSizes") {
		t.Fatal("world union requires absent resources or runtime-size metadata")
	}
	// The resolve's unused subject buffer consumes buffer slot 1. Its main
	// uniform must therefore occupy 2, while its color remains texture slot 0.
	for _, pattern := range []string{`\bslots \[\[buffer\(0\)\]\]`, `\bu \[\[buffer\(2\)\]\]`, `\bbodies \[\[texture\(0\)\]\]`} {
		if !regexp.MustCompile(pattern).MatchString(code) {
			t.Fatalf("resolve does not match common Metal layout: %s", pattern)
		}
	}
	storage, uniforms, textures := 0, 0, 0
	for i, entry := range layout {
		if entry.Binding != uint32(i) || module.GlobalVariables[i].Binding.Binding != entry.Binding {
			t.Fatalf("union binding order differs at %d", i)
		}
		if entry.Texture != nil {
			textures++
		} else if entry.Buffer != nil && entry.Buffer.Type == gputypes.BufferBindingTypeUniform {
			uniforms++
		} else if entry.Buffer != nil && entry.Buffer.Type == gputypes.BufferBindingTypeReadOnlyStorage {
			storage++
		} else {
			t.Fatalf("unexpected union resource at %d", i)
		}
	}
	if storage != 2 || uniforms != 2 || textures != 5 {
		t.Fatalf("core union resources: storage=%d uniforms=%d textures=%d", storage, uniforms, textures)
	}
}

func TestModelCommitViewsAndReplacement(t *testing.T) {
	m := &Models{}
	m.atlas.colorView, m.atlas.mergedView = &wgpu.TextureView{}, &wgpu.TextureView{}
	m.shadowWork.waterView = &wgpu.TextureView{}
	for _, atlas := range []*modelShadowAtlas{&m.shadowWork.dummy, &m.shadowWork.atlas} {
		atlas.bodyView, atlas.depthView, atlas.maskView = &wgpu.TextureView{}, &wgpu.TextureView{}, &wgpu.TextureView{}
	}
	views := m.modelCommitTextureViews()
	if views != [5]*wgpu.TextureView{m.atlas.colorView, m.shadowWork.dummy.bodyView, m.shadowWork.dummy.depthView, m.shadowWork.dummy.maskView, m.shadowWork.waterView} {
		t.Fatal("absent shadows do not bind independent dummy planes")
	}
	group := &wgpu.BindGroup{}
	m.commitGroup, m.commitViews = group, views
	if err := m.modelPrepareCommitGroup(); err != nil || m.commitGroup != group {
		t.Fatal("unchanged resources rebuilt the union group")
	}
	for _, change := range []func(){
		func() { m.shadowCount = 1 },
		func() { m.groupWork.count = 1 },
		func() { m.shadowWork.atlas.depthView = &wgpu.TextureView{} },
		func() { m.shadowCount = 0 },
	} {
		m.commitGroup, m.commitViews = &wgpu.BindGroup{}, m.modelCommitTextureViews()
		change()
		m.modelSyncCommitViews(m.modelCommitTextureViews())
		if m.commitGroup != nil || m.commitViews != [5]*wgpu.TextureView{} {
			t.Fatal("changed atlas family retained a borrowed union group")
		}
	}
	m.slots.buffer, m.shadowRecords.buffer, m.uniform, m.shadowWork.uniform = &wgpu.Buffer{}, &wgpu.Buffer{}, &wgpu.Buffer{}, &wgpu.Buffer{}
	entries, layout := m.modelCommitEntries(m.modelCommitTextureViews()), modelCommitLayoutEntries()
	for i, entry := range entries {
		if entry.Binding != layout[i].Binding || (entry.Buffer != nil) != (layout[i].Buffer != nil) || (entry.TextureView != nil) != (layout[i].Texture != nil) {
			t.Fatalf("native group resource order differs from union layout at %d", i)
		}
	}
	if entries[0].Buffer != m.slots.buffer || entries[1].Buffer != m.shadowRecords.buffer || entries[2].Buffer != m.uniform || entries[4].Buffer != m.shadowWork.uniform || entries[2].Size != 80 || entries[4].Size != 96 {
		t.Fatal("commit operands or uniform spans changed")
	}
	m.modelReleaseCommitGroup()
	m.modelReleaseCommitGroup()
}

func TestModelOutlineSubmissionMatchesShaderDiscard(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		state, visualAlpha, ruleAlpha float32
		ruled, want                   bool
	}{
		{"ordinary zero alpha", 1, 0, 1, false, false},
		{"ordinary outline", 1, 1, 0, false, true},
		{"rule erases visual outline", 1, 1, 0, true, false},
		{"rule supplies outline", 1, 0, 1, true, true},
		{"inactive construction", 0, 1, 1, true, false},
		{"negative state", -1, 1, 1, true, false},
		{"negative alpha", 1, -1, 1, false, false},
		// The shader's ordered comparisons do not discard NaN. Do not broaden
		// the CPU optimization to a positive-only predicate that changes this.
		{"unordered state retained", float32(math.NaN()), 1, 0, false, true},
		{"unordered alpha retained", 1, float32(math.NaN()), 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			visual := meshscene.ModelVisual{State: [4]float32{0, 1, tc.state}, Outline: [4]float32{0, 0, 0, tc.visualAlpha}}
			rule := meshscene.ModelRules{Outline: [4]float32{0, 0, 0, tc.ruleAlpha}}
			if got := modelOutlineNeeded(visual, rule, tc.ruled); got != tc.want {
				t.Fatalf("submitted=%v want %v", got, tc.want)
			}
		})
	}
}

func TestModelResolveMetalScalesLoadedCorner(t *testing.T) {
	ast, e := naga.Parse(modelResolveShaderSource(8))
	if e != nil {
		t.Fatal(e)
	}
	module, e := naga.LowerWithCapabilities(ast, modelResolveShaderSource(8), 0)
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
		if strings.Contains(line, "*") && strings.Contains(line, ".screen.zw") {
			screenScales++
		}
		if strings.Contains(line, "*") && strings.Contains(line, ".atlas.zw") {
			atlasScales++
		}
	}
	if loads != 1 || screenScales != 1 || atlasScales != 1 {
		t.Fatalf("resolve scales: loads=%d screen=%d atlas=%d", loads, screenScales, atlasScales)
	}
}

func TestModelShadersParseAndLower(t *testing.T) {
	for name, source := range map[string]string{"poses": modelPoseShader, "models": modelBodyShader, "resolve": modelResolveShaderSource(8)} {
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

func TestModelOutlineUsesSelectedBluePalette(t *testing.T) {
	at := strings.Index(modelBodyShader, "fn modelOutline(")
	if at < 0 || !strings.Contains(modelBodyShader[at:], "if modelBlue(i,key){color=textureLoad(palette,vec2<i32>(i32(i.shadow.w),1),0).rgb;}") {
		t.Fatal("retained BLUE outline does not select the rule's palette row")
	}
}
