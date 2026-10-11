package battlegpu

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func modelGroupFixture() meshscene.LiveFrame {
	f := meshscene.LiveFrame{Composed: true, Instances: make([]meshscene.Instance, 4), ModelRules: make([]meshscene.ModelRules, 4)}
	f.ModelRules[0].Clip = [4]float32{2, 60, 1, 125}
	f.Composition = meshscene.ModelComposition{
		InstanceSlots: []uint32{3, 4, 5, 2}, MemberGroup: []uint32{1, 1, 1, 0},
		Slots: make([]meshscene.ModelSlot, 5), Paint: []meshscene.ModelPaint{{Slot: 1}, {Slot: 2}},
		Groups: []meshscene.ModelGroup{{ParentInstance: 0, ParentSlot: 3, FinalSlot: 1, Keyed: true, PreserveHoles: true,
			Members: []meshscene.ModelGroupMember{{Instance: 0, Slot: 3}, {Instance: 1, Slot: 4, KeyDelta: -257}, {Instance: 2, Slot: 5, KeyDelta: math.MaxInt32}}}},
	}
	return f
}

func TestModelGroupPackingImmutableSelectorsAndSignedDelta(t *testing.T) {
	f := modelGroupFixture()
	before, _ := json.Marshal(f)
	var p modelGroupWork
	if e := p.modelPrepare(f); e != nil {
		t.Fatal(e)
	}
	after, _ := json.Marshal(f)
	if string(before) != string(after) {
		t.Fatal("group preparation modified borrowed frame")
	}
	word := func(b []byte, at int) uint32 { return binary.LittleEndian.Uint32(b[at:]) }
	if p.count != 1 || len(p.groups) != 48 || len(p.members) != 48 || word(p.groups, 0) != 0 || word(p.groups, 4) != 3 || word(p.groups, 8) != 1 || word(p.groups, 12) != 0 {
		t.Fatalf("group source/final selectors: %v", p.groups)
	}
	if word(p.groups, 16) != 3 || word(p.groups, 20) != 1 || word(p.groups, 24) != 1 || word(p.groups, 28) != 1 || math.Float32frombits(word(p.groups, 36)) != 60 {
		t.Fatalf("group count/policy/carrier clip: %v", p.groups)
	}
	if word(p.members, 20) != 4 || int32(word(p.members, 24)) != -257 || int32(word(p.members, 40)) != math.MaxInt32 {
		t.Fatalf("ordered source slots or signed deltas narrowed: %v", p.members)
	}
	if p.roles[0] != 2 || p.roles[1] != 0 || p.roles[2] != 1 || p.operands[0] != [4]int32{1, 0, 0, 0} || p.operands[1] != [4]int32{1, 1, -257, 0} || p.clips[2] != f.ModelRules[0].Clip {
		t.Fatalf("source policy: roles=%v operands=%v clips=%v", p.roles, p.operands, p.clips)
	}
	f.Composition.Groups[0].Keyed, f.Composition.Groups[0].PreserveHoles = false, false
	if e := p.modelPrepare(f); e != nil || p.operands[0][1] != 2 || p.operands[1][1] != 2 || word(p.groups, 20) != 0 || word(p.groups, 24) != 0 {
		t.Fatalf("keyless projectile pair acquired unit policy: %v %v", p, e)
	}
	f.Composition = meshscene.ModelComposition{}
	f.Composed = false
	if e := p.modelPrepare(f); e != nil || p.count != 0 || p.operands[1] != [4]int32{} || p.clips[1] != [4]float32{} {
		t.Fatalf("later standalone frame retained old group policy: %v %v", p, e)
	}
}

func TestModelGroupRejectsAliasingAndInconsistentProducer(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*meshscene.LiveFrame)
	}{
		{"source is final", func(f *meshscene.LiveFrame) { f.Composition.Groups[0].FinalSlot = 4 }},
		{"source painted", func(f *meshscene.LiveFrame) { f.Composition.Paint[0].Slot = 3 }},
		{"source selector differs", func(f *meshscene.LiveFrame) { f.Composition.InstanceSlots[1] = 5 }},
		{"missing membership", func(f *meshscene.LiveFrame) { f.Composition.MemberGroup[1] = 0 }},
		{"phantom membership", func(f *meshscene.LiveFrame) { f.Composition.MemberGroup[3] = 1 }},
		{"standalone targets final", func(f *meshscene.LiveFrame) { f.Composition.InstanceSlots[3] = 1 }},
		{"standalone targets member source", func(f *meshscene.LiveFrame) { f.Composition.InstanceSlots[3] = 3 }},
		{"missing exported group", func(f *meshscene.LiveFrame) { f.Composition.Groups = nil }},
		{"parent not first", func(f *meshscene.LiveFrame) {
			m := f.Composition.Groups[0].Members
			m[0], m[1] = m[1], m[0]
		}},
		{"parent key shifted", func(f *meshscene.LiveFrame) { f.Composition.Groups[0].Members[0].KeyDelta = 1 }},
		{"empty group", func(f *meshscene.LiveFrame) { f.Composition.Groups[0].Members = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := modelGroupFixture()
			tc.edit(&f)
			var p modelGroupWork
			if e := p.modelPrepare(f); e == nil {
				t.Fatal("unsupported group accepted")
			}
		})
	}
}

func TestModelGroupSourceKeepsOwnRevealAndDefersCarrierErase(t *testing.T) {
	own := meshscene.ModelRules{Reveal: [4]float32{70, 50, 1, 0}, Below: [4]float32{0, 0, 0, -2}, Band: [4]float32{1, 0, 0, 32}, Above: [4]float32{0, 0, 0, -1}, Clip: [4]float32{2, 60, 1, 125}}
	for _, operand := range [][4]int32{{}, {1, 2}} {
		if got := modelGroupSourceRule(own, operand); got != own {
			t.Fatalf("standalone/keyless source acquired carrier rule: %v", got)
		}
	}
	parent := modelGroupSourceRule(own, [4]int32{1, 0})
	child := modelGroupSourceRule(own, [4]int32{1, 1, -40})
	if parent.Clip != [4]float32{0, 60, 0, 125} || parent.Reveal != own.Reveal || child.Reveal != [4]float32{70, 50, 1, 1} || child.Below != own.Below || child.Band != own.Band || child.Above != own.Above {
		t.Fatalf("source hook replaced own reveal or lost key base: parent=%v child=%v", parent, child)
	}
}

func TestModelGroupAllowsAdmittedChildrenWithoutParentSource(t *testing.T) {
	f := modelGroupFixture()
	f.Composition.Groups[0].ParentSlot = 0
	f.Composition.Groups[0].Members = f.Composition.Groups[0].Members[1:]
	f.Composition.MemberGroup[0], f.Composition.InstanceSlots[0] = 0, 0
	var p modelGroupWork
	if e := p.modelPrepare(f); e != nil {
		t.Fatal(e)
	}
	if p.count != 1 || p.operands[0][0] != 0 || p.operands[1][1] != 1 || binary.LittleEndian.Uint32(p.groups[4:]) != 0 || binary.LittleEndian.Uint32(p.groups[16:]) != 2 {
		t.Fatalf("child-only group manufactured a parent source: %v", p)
	}
}

// Authored pixel oracle for native qualification. Widening to int64 here
// independently represents current Modern/Metal §22.4 saturation, rather than
// the retail widened-compare/byte-store law;
// the shader's extreme-delta branches are checked in emitted Metal below.
type modelGroupPixel struct {
	key             uint8
	color           uint32
	covered, parent bool
	delta           int32
}

func modelGroupPixelOracle(pixels []modelGroupPixel, keyed, holes bool) (uint8, uint32) {
	var key uint8
	var color uint32
	for _, p := range pixels {
		delta := int64(p.delta)
		if !keyed {
			delta = 0
		}
		shifted := min(max(int64(p.key)+delta, 0), 255)
		hole := keyed && holes && p.covered && int64(key) < shifted
		visible := p.color != 0 && (!keyed || int64(key) <= shifted)
		if p.parent || hole || visible {
			key, color = uint8(shifted), p.color
		}
	}
	return key, color
}

func TestModelGroupByteStoreCoverageAndMemberOrder(t *testing.T) {
	for _, tc := range []struct {
		name         string
		pixels       []modelGroupPixel
		keyed, holes bool
		key          uint8
		color        uint32
	}{
		{"equal visible child wins", []modelGroupPixel{{key: 50, color: 1, parent: true}, {key: 45, color: 2, delta: 5}}, true, true, 50, 2},
		{"equal erased child preserves", []modelGroupPixel{{key: 50, color: 1, parent: true}, {key: 45, covered: true, delta: 5}}, true, true, 50, 1},
		{"ordinary erased higher key blocks sibling", []modelGroupPixel{{key: 50, color: 1, parent: true}, {key: 80, covered: true}, {key: 75, color: 3}}, true, true, 80, 0},
		{"construction hole preserves carrier and sibling", []modelGroupPixel{{key: 50, color: 1, parent: true}, {key: 80, covered: true}, {key: 75, color: 3}}, true, false, 75, 3},
		{"untouched higher key is not an erased pixel", []modelGroupPixel{{key: 50, color: 1, parent: true}, {key: 80}}, true, true, 50, 1},
		{"positive shifted key saturates before storing", []modelGroupPixel{{key: 250, color: 1, parent: true}, {key: 250, color: 2, delta: 16}}, true, true, 255, 2},
		{"later child sees saturated prior", []modelGroupPixel{{key: 250, color: 1, parent: true}, {key: 250, color: 2, delta: 16}, {key: 20, color: 3}}, true, true, 255, 2},
		{"negative shifted key saturates below prior", []modelGroupPixel{{key: 1, color: 1, parent: true}, {key: 5, color: 2, delta: -10}}, true, true, 1, 1},
		{"largest signed delta does not overflow", []modelGroupPixel{{key: 255, color: 1, parent: true}, {key: 255, color: 2, delta: math.MaxInt32}}, true, true, 255, 2},
		{"smallest signed delta rejects", []modelGroupPixel{{key: 1, color: 1, parent: true}, {key: 255, color: 2, delta: math.MinInt32}}, true, true, 1, 1},
		{"parent erased seed retains its key", []modelGroupPixel{{key: 80, covered: true, parent: true}, {key: 75, color: 2}}, true, false, 80, 0},
		{"keyless child ignores delta and parent key", []modelGroupPixel{{key: 80, color: 1, parent: true}, {key: 1, color: 2, delta: math.MinInt32}, {key: 255, covered: true}}, false, true, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, color := modelGroupPixelOracle(tc.pixels, tc.keyed, tc.holes)
			if key != tc.key || color != tc.color {
				t.Fatalf("key=%d color=%d want %d %d", key, color, tc.key, tc.color)
			}
		})
	}
}

func TestModelGroupCoreShaderABIAndEmittedMerge(t *testing.T) {
	source := modelGroupShaderSource([3]int{2, 8, 10})
	ast, e := naga.Parse(source)
	if e != nil {
		t.Fatal(e)
	}
	module, e := naga.LowerWithCapabilities(ast, source, 0)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]uint32{"Group": modelGroupStride, "Member": modelMemberStride, "Slot": modelSlotStride, "Uniforms": 80}
	for _, typ := range module.Types {
		if span, ok := want[typ.Name]; ok {
			s, ok := typ.Inner.(ir.StructType)
			if !ok || s.Span != span {
				t.Fatalf("%s ABI=%v want %d", typ.Name, typ.Inner, span)
			}
			stride := uint32(16)
			if typ.Name == "Member" {
				stride = 4
			}
			for j, member := range s.Members {
				if member.Offset != uint32(j)*stride {
					t.Fatalf("%s.%s offset=%d", typ.Name, member.Name, member.Offset)
				}
			}
			delete(want, typ.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing ABI types: %v", want)
	}
	code, _, e := msl.Compile(module, msl.DefaultOptions())
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(code, "_mslBufferSizes") || strings.Contains(code, "_buffer_sizes") {
		t.Fatal("group stage requires unbound runtime-array metadata")
	}
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, "corners.inner[") && strings.Contains(line, "*") {
			t.Fatalf("conditional corner guard participates in scaling: %s", line)
		}
	}
	for _, pattern := range []string{
		`if \(delta[_0-9]* >= 256\)`, `if \(delta[_0-9]* <= -256\)`,
		`prior[_0-9]* < shifted[_0-9]*`, `prior[_0-9]* <= shifted[_0-9]*`,
		`metal::clamp\(`,
		`metadata[_0-9]*\.y > 0\.5`, `nextColor[_0-9]*\.w > 0\.0`,
		`k[_0-9]* <= clip[_0-9]*\.y`, `k[_0-9]* <= clip[_0-9]*\.w`,
	} {
		if !regexp.MustCompile(pattern).MatchString(code) {
			t.Fatalf("missing emitted merge arithmetic %q in:\n%s", pattern, code)
		}
	}
}

func TestModelGroupedSourceMetalKeepsCoveredErasedKeys(t *testing.T) {
	ast, e := naga.Parse(modelBodyShader)
	if e != nil {
		t.Fatal(e)
	}
	module, e := naga.LowerWithCapabilities(ast, modelBodyShader, 0)
	if e != nil {
		t.Fatal(e)
	}
	code, _, e := msl.Compile(module, msl.DefaultOptions())
	if e != nil {
		t.Fatal(e)
	}
	for _, pattern := range []string{
		`metadata \[\[color\(1\)\]\]`,
		`groupOperand\.x == 0`,
		`out[_0-9]*\.metadata = metal::float4\([[:alnum:]_]+ / 255\.0, 1\.0, 0\.0, 0\.0\)`,
		`out[_0-9]*\.color = [^\n]+\? metal::float4\(0\.0\) : metal::float4`,
	} {
		if !regexp.MustCompile(pattern).MatchString(code) {
			var relevant []string
			for _, line := range strings.Split(code, "\n") {
				if strings.Contains(line, ".metadata =") || strings.Contains(line, ".color =") {
					relevant = append(relevant, line)
				}
			}
			t.Fatalf("missing source erased-key contract %q in %v", pattern, relevant)
		}
	}
}
