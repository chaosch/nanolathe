package battlegpu

import (
	"encoding/binary"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func groundLightFixture() meshscene.RetainedLight {
	return meshscene.RetainedLight{PositionRadius: [4]float32{100, 200, 104, 20}, ColorGroundGain: [4]float32{1, .5, .2, .45}, GroundAgeFadeKind: [4]float32{104, 100, 0, 1}, Flags: [4]uint32{0, 1, 1, 1}}
}
func groundLightFrame(lights ...meshscene.RetainedLight) meshscene.LiveFrame {
	return meshscene.LiveFrame{LightControls: 1, Lighting: meshscene.RetainedLightingFrame{Lights: lights}, Camera: [3]float32{10000, -20000, 4}}
}

func TestGroundLightProjectionGainAndReceiverGate(t *testing.T) {
	light := groundLightFixture()
	f := groundLightFrame(light)
	discs, rect, e := groundLightPlan(f, 400, 300)
	if e != nil {
		t.Fatal(e)
	}
	// Absolute height controls projected placement; equal producer ground
	// removes attenuation. Camera replay was already applied by the host.
	if len(discs) != 1 || discs[0].rect != [4]float32{40, 64, 20, 20} || rect != [4]float32{80, 128, 40, 40} {
		t.Fatalf("disc=%v resolve=%v", discs, rect)
	}
	if f.Lighting.Lights[0] != light {
		t.Fatal("ground planning changed borrowed source or model emission")
	}
	// Age 100/fade 0 would omit this light if applied again. The supplied gain
	// is the complete source policy; both those metadata lanes remain untouched.
	var b [groundLightSourceStride]byte
	groundLightPackSource(b[:], light)
	if got := math.Float32frombits(binary.LittleEndian.Uint32(b[28:])); got != light.ColorGroundGain[3] {
		t.Fatalf("terrain gain applied again: %v", got)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*meshscene.RetainedLight)
	}{
		{"ground disabled", func(l *meshscene.RetainedLight) { l.Flags[1] = 0 }},
		{"strength zero", func(l *meshscene.RetainedLight) { l.ColorGroundGain[3] = 0 }},
		{"datum too far", func(l *meshscene.RetainedLight) { l.GroundAgeFadeKind[0] = 0 }},
		{"height equals radius", func(l *meshscene.RetainedLight) { l.GroundAgeFadeKind[0] = l.PositionRadius[2] - l.PositionRadius[3] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := light
			tc.mutate(&copy)
			discs, rect, e := groundLightPlan(groundLightFrame(copy), 400, 300)
			if e != nil || len(discs) != 0 || rect != [4]float32{} {
				t.Fatalf("disabled/empty source retained a pass: %v %v %v", discs, rect, e)
			}
		})
	}
}

func TestGroundLightOddEdgesAndCompleteBudget(t *testing.T) {
	light := groundLightFixture()
	light.PositionRadius = [4]float32{60.5, 33.5, 0, 4096}
	lights := make([]meshscene.RetainedLight, groundLightLimit)
	for i := range lights {
		lights[i] = light
		lights[i].GroundAgeFadeKind[3] = float32(i % 6)
	}
	before := slices.Clone(lights)
	discs, rect, e := groundLightPlan(groundLightFrame(lights...), 121, 67)
	if e != nil {
		t.Fatal(e)
	}
	if len(discs) != groundLightLimit || rect != [4]float32{0, 0, 121, 67} {
		t.Fatalf("complete budget/physical resolve bounds: %d %v", len(discs), rect)
	}
	for i, d := range discs {
		if d.source != uint32(i) || d.rect != [4]float32{0, 0, 61, 34} {
			t.Fatalf("source order or outward last-texel rounding: %v", d)
		}
	}
	if !slices.Equal(lights, before) {
		t.Fatal("planning modified borrowed source family data")
	}
	if _, _, e = groundLightPlan(groundLightFrame(append(lights, light)...), 121, 67); e == nil {
		t.Fatal("over-budget source list was truncated silently")
	}
	f := groundLightFrame(lights...)
	f.LightControls = 0
	if d, r, e := groundLightPlan(f, 121, 67); e != nil || len(d) != 0 || r != [4]float32{} {
		t.Fatalf("absent production lighting retained a field: %v %v %v", d, r, e)
	}
}

func TestGroundLightRejectsUnrepresentableEmission(t *testing.T) {
	light := groundLightFixture()
	light.ColorGroundGain[3] = float32(math.Inf(1))
	if _, _, e := groundLightPlan(groundLightFrame(light), 400, 300); e == nil {
		t.Fatal("nonfinite source gain accepted")
	}
	light = groundLightFixture()
	light.ColorGroundGain[0], light.ColorGroundGain[3] = math.MaxFloat32, 2
	if _, _, e := groundLightPlan(groundLightFrame(light), 400, 300); e == nil {
		t.Fatal("overflowed emission accepted")
	}
}

func TestGroundLightEmptyFrameDropsResolveAndReportsLegacy(t *testing.T) {
	// No device is touched for an empty/unsupported frame, even following a
	// lit frame's state. The host must not sample a retained old field then.
	g := &GroundLight{enabled: true, prepared: true, rect: [4]float32{1, 2, 3, 4}, counters: GroundLightCounters{Draws: 1, UploadBytes: 100}}
	f := meshscene.LiveFrame{Lights: []meshscene.Light{{PositionRadius: [4]float32{100, 100, 0, 40}}}}
	if e := g.Prepare(f, 400, 300); e != nil {
		t.Fatal(e)
	}
	if g.Enabled() || g.Rect() != [4]float32{} || g.Counters() != (GroundLightCounters{}) {
		t.Fatalf("empty frame retained terrain resolve: %v %v %v", g.Enabled(), g.Rect(), g.Counters())
	}
	missing := g.Missing()
	if missing["legacy_ground_lights"] != 1 {
		t.Fatalf("legacy receiver metadata loss unreported: %v", missing)
	}
	missing["legacy_ground_lights"] = 0
	if g.Missing()["legacy_ground_lights"] != 1 {
		t.Fatal("caller changed component diagnostics")
	}
}

func TestGroundLightCoreABIAndMetalArithmetic(t *testing.T) {
	ast, e := naga.Parse(groundLightShader)
	if e != nil {
		t.Fatal(e)
	}
	module, e := naga.LowerWithCapabilities(ast, groundLightShader, 0)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]uint32{"GroundSource": 64, "GroundDisc": 32, "GroundUniform": 16}
	for _, typ := range module.Types {
		if size, ok := want[typ.Name]; ok {
			s, ok := typ.Inner.(ir.StructType)
			if !ok || s.Span != size {
				t.Fatalf("%s ABI: %v", typ.Name, typ.Inner)
			}
			for i, member := range s.Members {
				if member.Offset != uint32(i*16) {
					t.Fatalf("%s.%s offset=%d", typ.Name, member.Name, member.Offset)
				}
			}
			delete(want, typ.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing ABI structures: %v", want)
	}
	code, _, e := msl.Compile(module, msl.DefaultOptions())
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(code, "_buffer_sizes") {
		t.Fatal("fixed budget became a runtime-array metadata dependency")
	}
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, "corners.inner[") && strings.Contains(line, "*") {
			t.Fatalf("guarded corner load scales only fallback: %s", line)
		}
	}
	// These artistic constants and strict rim comparison belong to §31.8,
	// not retail behavior. Inspect emitted arithmetic, not WGSL spelling alone.
	for _, arithmetic := range []string{"(3.0 - (2.0 *", "12.92", "2.4", "0.04045", "metal::exp(-(energy))"} {
		if !strings.Contains(code, arithmetic) {
			t.Fatalf("missing emitted ground-law arithmetic %q", arithmetic)
		}
	}
	for _, arithmetic := range []string{`if \(d2_? >= r2_?\)`, `energy =[^\n]+\* 2\.0`} {
		if !regexp.MustCompile(arithmetic).MatchString(code) {
			t.Fatalf("missing emitted ground-law arithmetic %q", arithmetic)
		}
	}
}
