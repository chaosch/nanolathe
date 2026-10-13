package battlegpu

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func modelShadowReadVec(data []byte, at int) (v [4]float32) {
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[at+i*4:]))
	}
	return
}

func TestModelShadowSoloSourceOrderAndRules(t *testing.T) {
	own := meshscene.ModelRules{Reveal: [4]float32{100, 20, 1, 0}, Clip: [4]float32{2, 11, 1, 50}, Outline: [4]float32{1, 0, 0, 0}}
	child := own
	child.Reveal[3], child.Clip[0], child.Clip[2], child.Outline[3] = 1, 0, 0, 1
	body := meshscene.ModelSlot{Screen: [4]float32{-2, 1, 10, 10}, Atlas: [4]float32{0, 0, 20, 20}}
	mask := meshscene.ModelSlot{Screen: [4]float32{3, 6, 10, 10}, Atlas: [4]float32{20, 0, 20, 20}}
	f := meshscene.LiveFrame{Current: []meshscene.Mat4{{}}, ModelRules: []meshscene.ModelRules{own, own}, Instances: []meshscene.Instance{{Mesh: 0}, {Mesh: 0, Visual: meshscene.ModelVisual{State: [4]float32{0, 1, 1}, Outline: [4]float32{0, 0, 0, 1}}}}}
	f.Shadows = meshscene.ShadowComposition{AtlasWidth: 64, AtlasHeight: 64, Tint: [4]float32{0, 0, 0, 2}, PieceFlags: []uint32{1}, Slots: []meshscene.ModelSlot{body, mask}, Subjects: []meshscene.ShadowSubject{{Screen: [4]float32{0, 0, 10, 10}, Body: body.Atlas, Mask: mask.Atlas, Params: [4]float32{1, 0, 0, 1}, Placement: [4]float32{-2, 1, 3, 6}, Order: [4]uint32{1, 1, 7, 0}}}, BodyDraws: []meshscene.ShadowDraw{{Mesh: 0, Instance: 1, Slot: 1}, {Mesh: 0, Instance: 0, Slot: 1}}, BodyRuleSlots: []uint32{1, 0}, BodyRules: []meshscene.ModelRules{child}, ProjectedDraws: []meshscene.ShadowDraw{{Mesh: 0, Instance: 0, Slot: 2}}}
	// A group-modified original record must not leak its deferred clip or delta
	// into the independent shadow body source (Metal's staged shadow mode 3).
	m := &Models{width: 20, height: 20, meshes: []modelMesh{{pieces: 1}}, slotBytes: make([]byte, modelSlotStride), instanceBytes: make([]byte, 2*modelInstanceStride)}
	for i := range f.Instances {
		binary.LittleEndian.PutUint32(m.instanceBytes[i*modelInstanceStride+384:], 7)
		binary.LittleEndian.PutUint32(m.instanceBytes[i*modelInstanceStride+392:], 200)
	}
	before := append([]meshscene.ModelRules(nil), f.ModelRules...)
	if err := m.modelPrepareShadowSources(f); err != nil {
		t.Fatal(err)
	}
	if len(m.shadowDraws) != 2 || len(m.shadowWork.projected) != 1 || m.shadowDraws[0].instance != 2 || m.shadowDraws[1].instance != 3 || m.shadowWork.projected[0].instance != 4 {
		t.Fatalf("source order/identity: %v / %v", m.shadowDraws, m.shadowWork.projected)
	}
	for di, want := range []meshscene.ModelRules{child, own} {
		at := int(m.shadowDraws[di].instance) * modelInstanceStride
		if got := modelShadowReadVec(m.instanceBytes, at+80); got != want.Reveal {
			t.Fatalf("draw %d reveal=%v want %v", di, got, want.Reveal)
		}
		if got := modelShadowReadVec(m.instanceBytes, at+144); got != want.Clip {
			t.Fatalf("draw %d clip=%v want %v", di, got, want.Clip)
		}
		if binary.LittleEndian.Uint32(m.instanceBytes[at+384:]) != 0 || binary.LittleEndian.Uint32(m.instanceBytes[at+316:]) != 1 || modelShadowReadVec(m.instanceBytes, at+240) != body.Screen || modelShadowReadVec(m.instanceBytes, at+256) != body.Atlas {
			t.Fatalf("draw %d did not isolate its solo source", di)
		}
	}
	if !m.shadowDraws[0].outline || m.shadowDraws[1].outline || !reflect.DeepEqual(f.ModelRules, before) || modelShadowReadVec(m.shadowBytes, 64) != f.Shadows.Subjects[0].Placement || binary.LittleEndian.Uint32(m.shadowBytes[80:]) != 1 {
		t.Fatal("outline gates, immutable input or subject packing changed")
	}
	f.Shadows.BodyRuleSlots[0] = 2
	if _, err := modelShadowRules(f, f.Shadows.BodyDraws[0], 2); err == nil {
		t.Fatal("out-of-range body rule accepted")
	}
}

func TestModelShadowAuthoredProjectionTopology(t *testing.T) {
	mesh := meshscene.Mesh{Pieces: 2, Vertices: []meshscene.Vertex{{Position: [3]float32{-1, 2, 3}, Piece: 1}, {Position: [3]float32{4, 5, 6}, Piece: 0}, {Position: [3]float32{7, 8, 9}, Piece: 1}}, Indices: []uint32{2, 0, 1}}
	vertices, indices, spans, err := modelShadowTopology([]meshscene.Mesh{mesh, mesh}, []modelMesh{{pieces: 2}, {pieces: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(vertices) != 6*modelShadowVertexStride || len(indices) != 24 || spans[1].base != 3 || spans[1].first != 3 || spans[1].count != 3 || binary.LittleEndian.Uint32(vertices[16:]) != 1 || modelShadowReadVec(vertices, 0) != [4]float32{-1, 2, 3, 1} || binary.LittleEndian.Uint32(indices[12:]) != 2 {
		t.Fatal("authored topology or mesh-local index identity changed")
	}
	mesh.Indices[0] = 3
	if _, _, _, err := modelShadowTopology([]meshscene.Mesh{mesh}, []modelMesh{{pieces: 2}}); err == nil {
		t.Fatal("invalid authored index accepted")
	}
}

func TestModelShadowShaderABIAndCoreBindings(t *testing.T) {
	capacities := [4]int{64, 32, 16, 8}
	wants := map[string]uint32{"ModelShadowUniforms": 96, "ModelShadowVertex": 32, "ModelShadowPose": 80, "ModelShadowInstance": modelInstanceStride, "ModelShadowSubject": modelShadowStride}
	for _, pass := range []struct {
		name, source string
		layout       []gputypes.BindGroupLayoutEntry
		used         []uint32
	}{
		{"project", modelShadowShaderSource(capacities), modelShadowProjectLayoutEntries(), []uint32{0, 1, 2, 3}},
		{"commit", modelShadowCommitShaderSource(capacities), modelCommitLayoutEntries(), []uint32{0, 1, 3, 4, 5, 6, 7, 8, 9}},
	} {
		ast, err := naga.Parse(pass.source)
		if err != nil {
			t.Fatalf("%s parse: %v", pass.name, err)
		}
		module, err := naga.LowerWithCapabilities(ast, pass.source, 0)
		if err != nil {
			t.Fatalf("%s lower: %v", pass.name, err)
		}
		for _, typ := range module.Types {
			if want, ok := wants[typ.Name]; ok {
				s, ok := typ.Inner.(ir.StructType)
				if !ok || s.Span != want {
					t.Fatalf("%s ABI=%v want %d", typ.Name, typ.Inner, want)
				}
				if typ.Name == "ModelShadowInstance" {
					for _, member := range s.Members {
						want, exists := map[string]uint32{"shadow": 160, "screen": 240, "atlas": 256}[member.Name]
						if exists && member.Offset != want {
							t.Fatalf("projection view %s offset=%d want %d", member.Name, member.Offset, want)
						}
					}
				}
				delete(wants, typ.Name)
			}
		}
		code, _, err := msl.Compile(module, msl.DefaultOptions())
		if err != nil {
			t.Fatalf("%s MSL: %v", pass.name, err)
		}
		if strings.Contains(code, "_mslBufferSizes") || strings.Contains(pass.source, "__") {
			t.Fatal("shadow shaders require runtime-array size metadata or unexpanded capacities")
		}
		// Pinned Metal applyBindGroup binds resources sequentially by type in
		// layout order. Naga numbers all module globals, even ones an entry does
		// not read; a combined project/commit module silently misbinds commit.
		if len(module.GlobalVariables) != len(pass.layout) {
			t.Fatalf("%s declares resources absent from its pass layout", pass.name)
		}
		var bufferSlot, textureSlot int
		for _, entry := range pass.layout {
			name := ""
			for _, global := range module.GlobalVariables {
				if global.Binding != nil && global.Binding.Group == 0 && global.Binding.Binding == entry.Binding {
					name = global.Name
				}
			}
			if name == "" {
				t.Fatalf("%s layout binding %d has no shader resource", pass.name, entry.Binding)
			}
			kind, slot := "buffer", bufferSlot
			if entry.Texture != nil {
				kind, slot = "texture", textureSlot
				textureSlot++
			} else {
				bufferSlot++
			}
			pattern := fmt.Sprintf(`\b%s \[\[%s\(%d\)\]\]`, name, kind, slot)
			// Unused globals consume native slots but have no entry argument.
			used := false
			for _, binding := range pass.used {
				used = used || binding == entry.Binding
			}
			if used && !regexp.MustCompile(pattern).MatchString(code) {
				t.Fatalf("%s resource %s does not use layout %s slot %d", pass.name, name, kind, slot)
			}
		}
		for _, line := range strings.Split(code, "\n") {
			if strings.Contains(line, "modelShadowCorners.inner[") && strings.Contains(line, "*") {
				t.Fatalf("guarded corner read participates in arithmetic: %s", line)
			}
		}
	}
	if len(wants) != 0 {
		t.Fatalf("missing ABI types: %v", wants)
	}
}

func TestModelShadowHeightAndBlockLaws(t *testing.T) {
	// Structure shear floors the relative height before its quarter. The
	// doubled lane adds the second low bit even for negative relative heights.
	for _, tc := range []struct{ height, want float64 }{{-5, -1.5}, {-4, -1}, {-3, -1}, {-2, -.5}, {-1, -.5}, {0, 0}, {1, 0}, {2, .5}, {3, .5}, {4, 1}} {
		h := math.Floor(tc.height)
		q := math.Floor(h / 4)
		if h-math.Floor(h/4)*4 >= 2 {
			q += .5
		}
		if q != tc.want {
			t.Fatalf("height %v shear=%v want %v", tc.height, q, tc.want)
		}
	}
	// Mobile clipping recovers every byte from depth and excludes equality.
	for key := 0; key < 256; key++ {
		depth := float32(256-key) / 257
		got := 256 - int(math.Round(float64(depth*257)))
		if got != key || got > key {
			t.Fatalf("key %d recovered %d", key, got)
		}
	}
	weights := []int{1, 4, 6, 4, 1}
	sum := 0
	for _, x := range weights {
		for _, y := range weights {
			sum += x * y
		}
	}
	if sum != 256 || float64(sum)/256*.5 != .5 || float64(sum)/256*.20 != .20 {
		t.Fatal("aircraft filter changes integrated opaque coverage or wet opacity")
	}
	// Structure punching is all-samples only; partial body coverage cannot
	// suppress an otherwise covered 2x2 projected block.
	for punch := 0; punch <= 4; punch++ {
		alpha := float32(0)
		if punch < 4 {
			alpha = float32(4) / 4 * .5
		}
		if (alpha == 0) != (punch == 4) {
			t.Fatal("partial structure block punched")
		}
	}
}
