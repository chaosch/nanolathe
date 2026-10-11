package battlegpu

import (
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func layerDirectTestPacket() meshscene.EffectGeometry {
	// The quad's asymmetric ring makes its original four-corner centroid differ
	// from the mean of the six packed fan vertices. Rejected rings stay between
	// admitted faces so they cannot accidentally advance the packing cursor.
	quad := drawlist.ModelFace{Normal: [3]float32{0, 0, 1}, Vertices: []drawlist.ModelVertex{{X: 0, Y: 0, Height: 2}, {X: 8, Y: 0, Height: 4}, {X: 5, Y: 6, Height: 6}, {X: 0, Y: 2, Height: 8}}}
	g := &drawlist.ModelGeometry{Width: 12, Height: 10, AnchorX: 20, AnchorY: 30, OriginX: 4, OriginY: 6, WorldHeight: 3, Scale: 1, Eligible: true,
		Faces: []drawlist.ModelFace{quad,
			{Vertices: []drawlist.ModelVertex{{X: 0, Y: 0}, {X: 0, Y: 2}, {X: 2, Y: 0}}},  // negative
			{Vertices: []drawlist.ModelVertex{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}}},  // zero
			{Vertices: []drawlist.ModelVertex{{X: 0, Y: 0}, {X: 2, Y: 0}}},                // short
			{Vertices: []drawlist.ModelVertex{{X: 9, Y: 0}, {X: 12, Y: 0}, {X: 9, Y: 3}}}, // zero-normal fragment
		}}
	packet := meshscene.EffectGeometry{Model: drawlist.Model{Geometry: g}, Scale: .5, OffsetX: 7, OffsetY: 9}
	// These authored fan corners lock 0/1/2, 0/2/3 followed by the next face.
	for i, xy := range [][2]float32{{15.25, 21.25}, {19.25, 21.25}, {17.75, 24.25}, {15.25, 21.25}, {17.75, 24.25}, {15.25, 22.25}, {19.75, 21.25}, {21.25, 21.25}, {19.75, 22.75}} {
		packet.Vertices = append(packet.Vertices, meshscene.EffectVertex{PositionUV: [4]float32{xy[0], xy[1], float32(i) / 16, float32(i) / 32}, Color: [4]float32{1.25, .5, .75, .5}, Bounds: [4]float32{.125, .25, .75, 1}})
	}
	return packet
}

func layerDirectTestLight(x, y, height float32) meshscene.RetainedLight {
	return meshscene.RetainedLight{PositionRadius: [4]float32{x, y, height, 10}, ColorGroundGain: [4]float32{.25, .5, 1, 999}, Flags: [4]uint32{1}}
}

func TestLayerDirectGeometryFaceAdmissionCentroidAndBorrowedPacking(t *testing.T) {
	packet := layerDirectTestPacket()
	original := append([]meshscene.EffectVertex(nil), packet.Vertices...)
	// Physical scale 2 cancels the world scale .5. The original quad centroid
	// is (3.25,2), absolute height 8; no raster half-pixel bias enters BL2.
	light := layerDirectTestLight(33.25, 48, 13)
	packed, err := layerAppendDirectGeometry(make([]byte, 64), packet, []meshscene.RetainedLight{light}, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(packed) != 64+9*layerDirectVertexStride || !reflect.DeepEqual(packet.Vertices, original) {
		t.Fatal("borrowed vertices mutated or admitted fan size changed")
	}
	// Distance five, normal facing it: (.75)^2 * 3.25, then BL4 digits 29/58/116.
	wantCode := float32(29 | 58<<7 | 116<<14)
	for i, v := range original {
		at := 64 + i*layerDirectVertexStride
		for lane, want := range [][4]float32{{v.PositionUV[0] * 2, v.PositionUV[1] * 2, v.PositionUV[2], v.PositionUV[3]}, v.Color, v.Bounds} {
			for j, value := range want {
				if got := math.Float32frombits(binary.LittleEndian.Uint32(packed[at+lane*16+j*4:])); got != value {
					t.Fatalf("corner %d lane %d/%d: %g != %g", i, lane, j, got, value)
				}
			}
		}
		code := math.Float32frombits(binary.LittleEndian.Uint32(packed[at+48:]))
		if i < 6 && code != wantCode || i >= 6 && code != 0 {
			t.Fatalf("face-constant code at corner %d: %g", i, code)
		}
		if binary.LittleEndian.Uint32(packed[at+52:]) != 0 || binary.LittleEndian.Uint32(packed[at+56:]) != 0 || binary.LittleEndian.Uint32(packed[at+60:]) != 0 {
			t.Fatal("reserved lighting lanes changed")
		}
	}
	center, extent := layerDirectSubjectBound(packet, 2)
	if center != [2]float32{36, 47} || extent != 11 {
		t.Fatal("subject selection must use the full box and one physical transform", center, extent)
	}
}

func TestLayerDirectLightZeroNormalOffAndStrictRadius(t *testing.T) {
	packet := layerDirectTestPacket()
	face := packet.Model.Geometry.Faces[0]
	light := layerDirectTestLight(33.25, 48, 13)
	for _, enabled := range []bool{false, true} {
		lights := []meshscene.RetainedLight{light}
		if enabled {
			lights[0].Flags[0] = 0 // model-light switch off, even if ground remains on
			lights[0].Flags[1] = 1
		}
		data, err := layerAppendDirectGeometry(nil, packet, lights, 2, enabled)
		if err != nil {
			t.Fatal(err)
		}
		for at := 48; at < len(data); at += layerDirectVertexStride {
			if binary.LittleEndian.Uint32(data[at:]) != 0 {
				t.Fatal("off path did not retain zero lighting")
			}
		}
	}
	face.Normal = [3]float32{}
	if layerDirectFaceLight(packet, face, []meshscene.RetainedLight{light}, [8]uint32{}, 1, 2) != 0 {
		t.Fatal("fragment with no source normal received light")
	}
	face.Normal = [3]float32{0, 0, -1}
	if layerDirectFaceLight(packet, face, []meshscene.RetainedLight{light}, [8]uint32{}, 1, 2) != 0 {
		t.Fatal("back-facing face received light")
	}
	face.Normal = [3]float32{0, 0, 1}
	light.PositionRadius[2] = 18
	if layerDirectFaceLight(packet, face, []meshscene.RetainedLight{light}, [8]uint32{}, 1, 2) != 0 {
		t.Fatal("radius equality received light")
	}
}

func TestLayerDirectLightEightSourcesQuantizationAndFallbackBound(t *testing.T) {
	packet := layerDirectTestPacket()
	center, extent := layerDirectSubjectBound(packet, 2)
	lights := make([]meshscene.RetainedLight, 10)
	for i := range lights {
		lights[i] = layerDirectTestLight(center[0], center[1], 0)
	}
	lights[8].ColorGroundGain = [4]float32{.25, .5, 2}
	indices, count := layerSelectSmoke(lights, center, extent)
	if indices != [8]uint32{8, 1, 2, 3, 4, 5, 6, 7} || count != 8 {
		t.Fatal("stable eight-source budget changed", indices, count)
	}
	if code := layerDirectLightCode([3]float32{-1, 1, 3}); code != 64<<7|127<<14 {
		t.Fatal("BL4 base-128 clamp/round changed", code)
	}
	if code := layerDirectLightCode([3]float32{.007, .009, 2}); code != 1<<7|127<<14 {
		t.Fatal("BL4 channel quantization changed", code)
	}
	packet.Model.Geometry.Width, packet.Model.Geometry.Height = 0, 0
	center, extent = layerDirectSubjectBound(packet, 2)
	if center != [2]float32{36.5, 45.5} || extent != 10 {
		t.Fatal("missing authored box must use whole corner extent", center, extent)
	}
}

func TestLayerDirectGeometryRejectsUnalignedFacesWithoutSuppressionBypass(t *testing.T) {
	for _, damage := range []func(*meshscene.EffectGeometry){
		func(p *meshscene.EffectGeometry) { p.Vertices[1].PositionUV[0] += .25 },
		func(p *meshscene.EffectGeometry) { p.Vertices = p.Vertices[:6] },
		func(p *meshscene.EffectGeometry) { p.Vertices = append(p.Vertices, p.Vertices[:3]...) },
		func(p *meshscene.EffectGeometry) { p.Model.Geometry = nil },
	} {
		packet := layerDirectTestPacket()
		damage(&packet)
		data, err := layerAppendDirectGeometry(make([]byte, 64), packet, nil, 2, true)
		if err == nil || len(data) != 64 {
			t.Fatal("unaligned source accepted or earlier geometry discarded", len(data), err)
		}
	}
	packet := layerDirectTestPacket()
	packet.Suppressed, packet.Model.Geometry = "requires subject composition", nil
	data, err := layerAppendDirectGeometry(nil, packet, nil, 2, true)
	if err != nil || len(data) != 0 {
		t.Fatal("suppressed geometry was admitted", err)
	}
}

func TestLayerDirectLightingCoreABIAndNativeResources(t *testing.T) {
	source := layerDirectShaderSource(32)
	ast, err := naga.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.LowerWithCapabilities(ast, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, typ := range module.Types {
		if typ.Name == "LayerVertex" {
			found = true
			s, ok := typ.Inner.(ir.StructType)
			if !ok || s.Span != layerDirectVertexStride || len(s.Members) != 4 {
				t.Fatal("direct vertex light-code ABI changed", typ.Inner)
			}
			for i, member := range s.Members {
				if member.Offset != uint32(i*16) {
					t.Fatal("direct corner lane moved", member)
				}
			}
		}
	}
	if !found {
		t.Fatal("direct vertex ABI absent")
	}
	code, _, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(code, "vertex layer_triangle_vertexOutput layer_triangle_vertex(")
	if start < 0 {
		t.Fatal("native direct vertex entry absent")
	}
	signature := code[start:]
	signature = signature[:strings.Index(signature, ") {")]
	if strings.Contains(signature, "_buffer_sizes") {
		t.Fatal("native direct vertex reads an unbound runtime-array size", signature)
	}
	if !strings.Contains(signature, "layerU [[buffer(0)]]") || !strings.Contains(signature, "layerVertices [[buffer(2)]]") || strings.Contains(signature, "[[buffer(5)]]") {
		t.Fatal("native direct vertex resources do not match the shared layout", signature)
	}
	entry := code[start:]
	entry = entry[:strings.Index(entry, "\n}")]
	if !strings.Contains(entry, "< 32 ? layerVertices.inner[") || !strings.Contains(entry, ".params = metal::float4(0.0, 0.0, 0.0,") {
		t.Fatal("native direct entry lost its fixed-tier load or light-code output", entry)
	}
	for _, line := range strings.Split(entry, "\n") {
		if strings.Contains(line, ".position =") && strings.Contains(line, "?") {
			t.Fatal("conditional corner load reached position arithmetic", line)
		}
	}
	if !strings.Contains(code, "layer_geometry_fragment(") || !strings.Contains(code, "127u") || !strings.Contains(code, "63.5") {
		t.Fatal("native direct colour entry lost BL4 numeric decode")
	}
	start = strings.Index(code, "fragment layer_geometry_fragmentOutput layer_geometry_fragment(")
	if start < 0 {
		t.Fatal("native direct colour entry absent")
	}
	fragment := code[start:]
	fragment = fragment[:strings.Index(fragment, "\n}")]
	if !strings.Contains(fragment, "layerAtlas [[texture(0)]]") || strings.Contains(fragment, "[[buffer(") || strings.Contains(fragment, "[[texture(1)]]") || !strings.Contains(fragment, "metal::min(") {
		t.Fatal("native direct colour resources or BL4 clamp changed", fragment)
	}
	if !strings.Contains(fragment, ".params.w < 0.5") {
		t.Fatal("native zero-light bypass absent", fragment)
	}
}
