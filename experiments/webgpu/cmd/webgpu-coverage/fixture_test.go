package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"testing"
)

// Independent authored triangle-centre evaluation. No centre touches an edge,
// so this contract does not depend on the hardware top-left tie convention.
func TestAuthoredCoverageHasNoSampleEdgeTies(t *testing.T) {
	scene, f, want := coverageScene(), coverageCases()[0].frame(), atlasBlocks()
	for i, mesh := range scene.Meshes {
		var got [4][4]byte
		for sample := range 4 {
			p := [2]float32{float32(sample%2) + .5, float32(sample/2) + .5}
			for triangle := 0; triangle < len(mesh.Indices); triangle += 3 {
				var corners [3][2]float32
				for j := range corners {
					vertex := mesh.Vertices[mesh.Indices[triangle+j]]
					// The authored direct projection uses record step1, doubled
					// geometry, post-record scale1 and viewport translation4.
					x := .5*float32(math.Floor(float64(2*vertex.Position[0]))) + 4
					y := .5*float32(math.Floor(float64(2*vertex.Position[2]-vertex.Position[1]))) + 4
					corners[j] = [2]float32{(x - f.Composition.Slots[i].Screen[0]) * 2, (y - f.Composition.Slots[i].Screen[1]) * 2}
				}
				positive, negative := false, false
				for j, a := range corners {
					b := corners[(j+1)%3]
					cross := (b[0]-a[0])*(p[1]-a[1]) - (b[1]-a[1])*(p[0]-a[0])
					if cross == 0 {
						t.Fatalf("mesh %d triangle %d sample %d touches an authored edge", i, triangle/3, sample)
					}
					positive = positive || cross > 0
					negative = negative || cross < 0
				}
				if positive && negative {
					continue
				}
				if got[sample][3] != 0 {
					t.Fatalf("mesh %d sample %d has overlapping authored interiors", i, sample)
				}
				color := mesh.Vertices[mesh.Indices[triangle]].Color
				got[sample] = [4]byte{byte(color[0] * 255), byte(color[1] * 255), byte(color[2] * 255), 255}
			}
		}
		if got != want[i] {
			t.Fatalf("mesh %d centre colours=%v; want %v", i, got, want[i])
		}
	}
}

func TestFixtureProjectionAndFinalOpacity(t *testing.T) {
	for _, c := range coverageCases() {
		f := c.frame()
		for i, p := range f.ModelProjections.Records {
			if p.Origin[3] != 3 || p.Control != [4]float32{1, 1, 1, 0} || f.ModelProjections.View != [4]float32{0, 0, 4, 4} {
				t.Fatal("fixture no longer selects the exact doubled direct projection")
			}
			if f.Instances[i].Visual.State[1] != c.Opacity || f.Composition.Slots[i].Params[0] != c.Opacity {
				t.Fatal("fixture did not supply subject opacity at instance and final boundary")
			}
			if c.Grouped {
				source := f.Composition.InstanceSlots[i]
				if source != uint32(i+4) || f.Composition.Slots[source-1].Params[0] != 1 ||
					f.Composition.Groups[i].FinalSlot != uint32(i+1) || f.Composition.Paint[i].Slot != uint32(i+1) {
					t.Fatal("source and final selectors or opacity are not independent")
				}
			}
		}
		colors := c.worldColors()
		if c.Supersample {
			if colors[0] != [4]float32{c.Opacity / 4, 0, 0, c.Opacity / 4} ||
				colors[1] != [4]float32{c.Opacity / 2, 0, c.Opacity / 2, c.Opacity} {
				t.Fatal("quarter coverage, red/blue mean or once-only opacity changed")
			}
		} else if colors[0] != [4]float32{c.Opacity, 0, 0, c.Opacity} || colors[2] != [4]float32{} {
			t.Fatal("supersample-off must read the top-left, not any covered texel")
		}
	}
}

func authoredPixels(c coverageCase) ([]slotRecord, []byte, []byte) {
	slots := make([]slotRecord, len(c.frame().Composition.Slots))
	for i := range slots {
		slots[i] = slotRecord{Atlas: [4]float32{float32(i * 2), 0, 2, 2}}
	}
	atlas := make([]byte, 256*2)
	for i, block := range atlasBlocks() {
		for j, color := range block {
			copy(atlas[(j/2)*256+(i*2+j%2)*4:], color[:])
		}
	}
	world := make([]byte, 256*fixtureHeight)
	// All fixture expectations are dyadic half-float values. Encoding these
	// independently catches decoder mistakes without a float-to-half helper.
	half := map[float32]uint16{0: 0, .125: 0x3000, .25: 0x3400, .5: 0x3800, 1: 0x3c00}
	for i, color := range c.worldColors() {
		for j, value := range color {
			binary.LittleEndian.PutUint16(world[2*256+(2+i*2)*8+j*2:], half[value])
		}
	}
	return slots, atlas, world
}

func TestPixelComparatorRejectsStaleEmptyAndWrongResolve(t *testing.T) {
	for _, c := range coverageCases() {
		slots, atlas, world := authoredPixels(c)
		result, err := comparePixels(c, slots, atlas, 16, 2, 256, world, 256)
		if err != nil || !result.Passed {
			t.Fatalf("authored expected %s: %+v %v", c.Name, result, err)
		}
		for _, mutation := range []func([]byte, []byte){
			func(a, _ []byte) { clear(a) },
			func(_, w []byte) { clear(w) },
			func(_, w []byte) { binary.LittleEndian.PutUint16(w[2*256+2*8:], 0x3c00) },
			func(_, w []byte) { binary.LittleEndian.PutUint16(w[0:], 0x7c00) },
			func(_, w []byte) { binary.LittleEndian.PutUint16(w[0:], 0x7e00) },
		} {
			a, w := bytes.Clone(atlas), bytes.Clone(world)
			mutation(a, w)
			if bytes.Equal(a, atlas) && bytes.Equal(w, world) {
				continue // opaque-off red already equals 1
			}
			if result, err := comparePixels(c, slots, a, 16, 2, 256, w, 256); err == nil || result.Passed {
				t.Fatalf("%s accepted a missing, incorrect or nonfinite diagnostic", c.Name)
			}
		}
	}
}

func TestNonfiniteSampleStillHasSerializableFailureEvidence(t *testing.T) {
	c := coverageCases()[0]
	slots, atlas, world := authoredPixels(c)
	binary.LittleEndian.PutUint16(world[2*256+2*8:], 0x7e00)
	result, err := comparePixels(c, slots, atlas, 16, 2, 256, world, 256)
	if err == nil || result.Passed || result.Samples[0].Actual[0] != nil || result.Samples[0].ActualHalf[0] != 0x7e00 {
		t.Fatal("nonfinite sample lost the failing status or exact source bits")
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatal("nonfinite output prevented the failure report from being saved", err)
	}
}

func TestPackingRejectsOneTexelAndWrongOpacity(t *testing.T) {
	c := coverageCases()[0]
	f := c.frame()
	data := make([]byte, len(f.Composition.Slots)*48)
	for i, slot := range f.Composition.Slots {
		for j, vector := range [][4]float32{slot.Screen, {float32(i * 2), 0, 2, 2}, slot.Params} {
			for k, value := range vector {
				binary.LittleEndian.PutUint32(data[i*48+j*16+k*4:], math.Float32bits(value))
			}
		}
	}
	if _, err := readSlots(data, f); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		offset int
		value  float32
	}{{24, 1}, {16, 1}, {32, .5}} {
		bad := bytes.Clone(data)
		binary.LittleEndian.PutUint32(bad[change.offset:], math.Float32bits(change.value))
		if _, err := readSlots(bad, f); err == nil {
			t.Fatal("packing accepted wrong doubled extent, parity or opacity")
		}
	}
}

func TestDeferredFrameworkErrorRecordsCannotBeIgnored(t *testing.T) {
	var output bytes.Buffer
	records := &errorRecords{}
	logger := slog.New(errorHandler{base: slog.NewTextHandler(&output, nil), errors: records})
	logger.Warn("ordinary host warning")
	logger.With("fixture", "quarter").ErrorContext(context.Background(), "finish shared frame encoder failed", "err", "authored failure")
	logger.Error("submit failed", "err", "authored failure")
	messages := records.snapshot()
	if len(messages) != 2 || !strings.Contains(messages[0], "finish shared frame encoder failed") ||
		!strings.Contains(messages[0], "fixture=quarter") || !strings.Contains(messages[1], "submit failed") {
		t.Fatalf("deferred framework errors were not retained: %v", messages)
	}
}
