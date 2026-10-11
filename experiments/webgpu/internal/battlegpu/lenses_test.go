package battlegpu

import (
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestLensCopyBoundsMatchNativeScaledFloorCeilAndClamp(t *testing.T) {
	for _, test := range []struct {
		read          [4]float32
		scale         float32
		width, height int
		want          [4]uint32
	}{
		{[4]float32{-1.5, 2.25, 5, 3.25}, 2, 13, 9, [4]uint32{0, 4, 7, 5}},
		{[4]float32{2.75, 1.75, .25000003, .25000003}, 1, 4, 3, [4]uint32{2, 1, 1, 1}},
		{[4]float32{5, 4, 9, 7}, .5, 10, 8, [4]uint32{2, 2, 5, 4}},
		{[4]float32{-4, 1, 2, 1}, 1, 5, 5, [4]uint32{}},
		{[4]float32{5, 1, 2, 1}, 1, 5, 5, [4]uint32{}},
		{[4]float32{1, 5, 2, 1}, 1, 5, 5, [4]uint32{}},
	} {
		got, err := lensReadRect(test.read, test.scale, test.width, test.height)
		if err != nil || got != test.want {
			t.Fatal("native copy bounds changed", test, got, err)
		}
	}
	// Native replay adds its already-scaled float32 lanes before ceil. A
	// widened addition here would copy an extra unrequested column/row.
	if float64(float32(2.75))+float64(float32(.25000003)) <= 3 {
		t.Fatal("authored rounding fixture no longer distinguishes precision")
	}
	for _, read := range [][4]float32{{0, 0, -1, 1}, {float32(math.Inf(1)), 0, 1, 1}} {
		if _, err := lensReadRect(read, 1, 5, 5); err == nil {
			t.Fatal("invalid bounds accepted", read)
		}
	}
	world, snapshot := &wgpu.Texture{}, &wgpu.Texture{}
	copy := lensCopyRegion(world, snapshot, [4]uint32{2, 3, 4, 5})
	if copy.Source.Texture != world || copy.Destination.Texture != snapshot || copy.Source.Origin != (wgpu.Origin3D{X: 2, Y: 3}) || copy.Destination.Origin != copy.Source.Origin || copy.Size != (wgpu.Extent3D{Width: 4, Height: 5, DepthOrArrayLayers: 1}) {
		t.Fatal("snapshot alias or source-coordinate translation", copy)
	}
	for _, previous := range []gputypes.TextureUsage{0, wgpu.TextureUsageTextureBinding} {
		before, after := lensCopyTransitions(world, snapshot, previous)
		if before[0].Texture != world || before[0].Usage.OldUsage != wgpu.TextureUsageRenderAttachment || before[0].Usage.NewUsage != wgpu.TextureUsageCopySrc || before[1].Texture != snapshot || before[1].Usage.OldUsage != previous || before[1].Usage.NewUsage != wgpu.TextureUsageCopyDst || after[0].Usage.OldUsage != wgpu.TextureUsageCopySrc || after[0].Usage.NewUsage != wgpu.TextureUsageRenderAttachment || after[1].Usage.OldUsage != wgpu.TextureUsageCopyDst || after[1].Usage.NewUsage != wgpu.TextureUsageTextureBinding {
			t.Fatal("copy/draw barrier state changed", before, after)
		}
		for _, barrier := range [4]wgpu.TextureBarrier{before[0], before[1], after[0], after[1]} {
			if barrier.Range.MipLevelCount != 1 || barrier.Range.ArrayLayerCount != 1 || barrier.Range.Aspect != gputypes.TextureAspectAll {
				t.Fatal("copy barrier omitted its single color subresource", barrier)
			}
		}
	}
}

func TestLensPackingKeepsOriginalIndicesSamplesKeysAndSingleScale(t *testing.T) {
	a := meshscene.EffectLensSample{Rect: [4]float32{1.25, 2.5, .75, 1.5}, Source: [4]float32{3.5, 4.25, .75, 1.5}}
	b := meshscene.EffectLensSample{Rect: [4]float32{6, 7, 1, 2}, Source: [4]float32{8, 9, 1, 2}}
	stock := &meshscene.StockFrame{Scale: 1.25, Layers: []meshscene.EffectLayer{
		{Lenses: []meshscene.EffectLens{{ReadRect: [4]float32{40, 40, 1, 1}, Samples: []meshscene.EffectLensSample{{Rect: [4]float32{40, 40, 1, 1}, Source: [4]float32{40, 40, 1, 1}}}}, {ReadRect: [4]float32{3, 4, 6, 7}, KeyRGB: [3]uint8{0, 128, 255}, Samples: []meshscene.EffectLensSample{a, b}}}},
		{},
		{Lenses: []meshscene.EffectLens{{ReadRect: [4]float32{8, 9, 1, 2}, KeyRGB: [3]uint8{17, 31, 63}, Samples: []meshscene.EffectLensSample{b}}}},
	}}
	before := slices.Clone(stock.Layers[0].Lenses[1].Samples)
	r := &Lenses{}
	if err := r.pack(stock, 32, 32); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		layer, index uint32
		first, count uint32
	}{{0, 0, 0, 0}, {0, 1, 0, 2}, {2, 0, 2, 1}} {
		span, ok := r.span(test.layer, test.index)
		if !ok || span.first != test.first || span.count != test.count {
			t.Fatal("original lens index moved", test, span, ok)
		}
	}
	if _, ok := r.span(1, 0); ok {
		t.Fatal("empty layer borrowed its neighbor's lens")
	}
	if len(r.data) != 3*48 {
		t.Fatal("native sample stride changed", len(r.data))
	}
	for i, sample := range []meshscene.EffectLensSample{a, b, b} {
		key := [4]float32{0, 128, 255, 0}
		if i == 2 {
			key = [4]float32{17, 31, 63, 0}
		}
		for lane, vector := range [3][4]float32{sample.Rect, sample.Source, key} {
			for j, v := range vector {
				if lane < 2 {
					v *= 1.25
				}
				if binary.LittleEndian.Uint32(r.data[i*48+lane*16+j*4:]) != math.Float32bits(v) {
					t.Fatal("scaled packing or key bytes changed", i, lane, j)
				}
			}
		}
	}
	if !slices.Equal(before, stock.Layers[0].Lenses[1].Samples) {
		t.Fatal("borrowed retained sample mutated")
	}
	stock.Layers[0].Lenses[1].Samples[0].Source[0] = float32(math.NaN())
	if err := r.pack(stock, 32, 32); err == nil {
		t.Fatal("nonfinite source accepted")
	}
}

func TestLensAbsentAndEmptyFramesAllocateNoSamplesOrSnapshot(t *testing.T) {
	r := &Lenses{counts: LensCounters{Draws: 2, Copies: 2, Samples: 3, UploadBytes: 160}, ready: true}
	if err := r.Prepare(meshscene.LiveFrame{}, 19, 11); err != nil {
		t.Fatal(err)
	}
	r.Draw(nil, 0, 0)
	if r.ready || r.Counters() != (LensCounters{}) || r.samples != nil || r.viewport != nil || r.snapshot != nil || r.data != nil {
		t.Fatal("absent frame retained work", r)
	}
	stock := &meshscene.StockFrame{Scale: 1, Layers: []meshscene.EffectLayer{{Lenses: []meshscene.EffectLens{{ReadRect: [4]float32{1, 1, 1, 1}}, {ReadRect: [4]float32{40, 1, 1, 1}, Samples: []meshscene.EffectLensSample{{}}}}}}}
	if err := r.Prepare(meshscene.LiveFrame{StockEffects: stock}, 19, 11); err != nil {
		t.Fatal(err)
	}
	for _, index := range []uint32{0, 1} {
		if err := r.Snapshot(nil, nil, 0, index); err != nil {
			t.Fatal("empty lens asked for GPU inputs", err)
		}
		r.Draw(nil, 0, index)
	}
	if r.Counters() != (LensCounters{}) || r.samples != nil || r.snapshot != nil {
		t.Fatal("empty lens encoded work")
	}
	if err := r.Snapshot(nil, nil, 1, 0); err == nil {
		t.Fatal("original layer bound unchecked")
	}
	if err := r.Prepare(meshscene.LiveFrame{}, 8193, 11); err == nil {
		t.Fatal("oversized extent admitted")
	}
	r.Close()
	r.Close()
	if r.Prepare(meshscene.LiveFrame{}, 19, 11) == nil || r.Snapshot(nil, nil, 0, 0) == nil {
		t.Fatal("closed component accepted work")
	}
}

func TestLensSnapshotMustBeConsumedAtItsOwnBarrier(t *testing.T) {
	r := &Lenses{prepared: true, ready: true, readyLayer: 2, readyIndex: 3}
	if r.Snapshot(nil, nil, 2, 4) == nil {
		t.Fatal("another lens can overwrite a pending snapshot")
	}
	r.Draw(nil, 2, 3)
	r.Draw(nil, 2, 4)
	if !r.ready || r.Counters() != (LensCounters{}) {
		t.Fatal("non-draw consumed the snapshot")
	}
	limit := gputypes.DefaultLimits().MaxBufferSize
	for _, test := range [][3]uint64{{0, 48, 48}, {0, 49, 96}, {96, 96, 96}, {96, 97, 192}, {0, limit, limit}} {
		if got := lensCapacity(test[0], test[1]); got != test[2] {
			t.Fatal("sample capacity grew beyond need/core limit", test, got)
		}
	}
}

func TestLensCoreShaderNativeSlotsNearestKeyAndHDRPreservation(t *testing.T) {
	if _, err := naga.Compile(lensShader); err != nil {
		t.Fatal(err)
	}
	ast, err := naga.Parse(lensShader)
	if err != nil {
		t.Fatal(err)
	}
	module, err := naga.LowerWithCapabilities(ast, lensShader, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, typ := range module.Types {
		if typ.Name == "LensSample" {
			s, ok := typ.Inner.(ir.StructType)
			if !ok || s.Span != 48 || len(s.Members) != 3 {
				t.Fatal("sample vertex ABI changed", typ.Inner)
			}
			for i, member := range s.Members {
				if member.Offset != uint32(i*16) {
					t.Fatal("sample lane moved", member)
				}
			}
			found = true
		}
		if a, ok := typ.Inner.(ir.ArrayType); ok && a.Size.Constant == nil {
			t.Fatal("sample input needs unbound runtime metadata")
		}
	}
	if !found {
		t.Fatal("missing sample structure")
	}
	code, info, err := msl.Compile(module, msl.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if info.RequiresSizesBuffer {
		t.Fatal("stock lens requires optional metadata")
	}
	for _, binding := range []string{"lensViewport [[buffer(0)]]", "lensSnapshot [[texture(0)]]", "[[attribute(0)]]", "[[attribute(1)]]", "[[attribute(2)]]"} {
		if !strings.Contains(code, binding) {
			t.Fatal("native binding or instance attribute moved", binding)
		}
	}
	for _, line := range strings.Split(code, "\n") {
		if (strings.Contains(line, "pixel =") || strings.Contains(line, ".source =") || strings.Contains(line, ".position =")) && strings.Contains(line, "?") {
			t.Fatal("guarded corner multiplication reached pixel/source", line)
		}
	}
	for _, expression := range []string{
		"floor(input.source)", "textureLoad(lensSnapshot,texel,0)", "floor(color.rgb*255.0+0.5)",
		"key.x==input.key.x && key.y==input.key.y && key.z==input.key.z", "return color;",
	} {
		if !strings.Contains(lensShader, expression) {
			t.Fatal("native nearest/key/HDR operation changed", expression)
		}
	}
	if strings.Contains(lensShader, "textureSample") || strings.Contains(lensShader, "all(") {
		t.Fatal("lens acquired filtering or unsupported vector comparison")
	}
	layout, vertices := lensLayoutEntries(), lensVertexLayout()
	if len(layout) != 2 || layout[0].Buffer.MinBindingSize != 16 || layout[1].Binding != 1 || len(vertices) != 1 || vertices[0].ArrayStride != 48 || vertices[0].StepMode != gputypes.VertexStepModeInstance {
		t.Fatal("core lens layout changed", layout, vertices)
	}
	for i, attr := range vertices[0].Attributes {
		if attr.Offset != uint64(i*16) || attr.ShaderLocation != uint32(i) || attr.Format != gputypes.VertexFormatFloat32x4 {
			t.Fatal("vertex lane moved", attr)
		}
	}
	// Authored colors straddle the exact native RGB-byte comparison. A match
	// discards regardless of alpha; HDR is neither keyed nor clamped to one.
	key := [3]float32{0, 128, 255}
	for _, test := range []struct {
		color [4]float32
		match bool
	}{
		{[4]float32{0, .5, 1, .25}, true},
		{[4]float32{0, .5 - 1.0/512, 1, .25}, false},
		{[4]float32{1.5, 2, 4.75, .375}, false},
	} {
		match := true
		for i, want := range key {
			if float32(math.Floor(float64(test.color[i]*255+.5))) != want {
				match = false
			}
		}
		if match != test.match {
			t.Fatal("RGB byte key boundary changed", test)
		}
	}
}
