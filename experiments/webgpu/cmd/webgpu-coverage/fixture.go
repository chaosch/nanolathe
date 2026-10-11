package main

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

const fixtureWidth, fixtureHeight = 8, 8

type coverageCase struct {
	Name        string
	Grouped     bool
	Supersample bool
	Opacity     float32
}

func coverageCases() []coverageCase {
	var cases []coverageCase
	for _, grouped := range []bool{false, true} {
		for _, supersample := range []bool{false, true} {
			for _, opacity := range []float32{1, .5} {
				cases = append(cases, coverageCase{
					Name:    fmt.Sprintf("group-%t-supersample-%t-opacity-%.1f", grouped, supersample, opacity),
					Grouped: grouped, Supersample: supersample, Opacity: opacity,
				})
			}
		}
	}
	return cases
}

// This is authored geometry, not a retail asset or a span-raster parity claim.
// Projection is the exported doubled direct lane: screen=.5*floor(2*x,2*z-y)
// +(4,4). A one-pixel slot then maps it to two atlas texels [GPU design §17].
func coverageScene() *meshscene.Scene {
	vertex := func(x, z float32, color [3]float32) meshscene.Vertex {
		return meshscene.Vertex{Position: [3]float32{x, 0, z}, Normal: [3]float32{0, 1, 0},
			ShadeNormal: [3]float32{0, 1, 0}, UV: [2]float32{.5, .5}, Color: color}
	}
	red, blue := [3]float32{1, 0, 0}, [3]float32{0, 0, 1}
	// Source triangle (0,0),(2,0),(0,1): only TL (.5,.5) is strictly inside.
	// Its diagonal is x/2+y=1, away from all four 2x2 sample centres.
	quarter := meshscene.Mesh{Name: "authored-quarter-top-left", Pieces: 1,
		Vertices: []meshscene.Vertex{vertex(-2, -2, red), vertex(-1, -2, red), vertex(-2, -1.5, red)},
		Indices:  []uint32{0, 1, 2}}
	mean := meshscene.Mesh{Name: "authored-red-blue-columns", Pieces: 1}
	quad := func(x0, x1, z0, z1 float32, color [3]float32) {
		base := uint32(len(mean.Vertices))
		mean.Vertices = append(mean.Vertices, vertex(x0, z0, color), vertex(x1, z0, color),
			vertex(x0, z1, color), vertex(x1, z1, color))
		for _, index := range []uint32{0, 1, 2, 2, 1, 3} {
			mean.Indices = append(mean.Indices, base+index)
		}
	}
	// Each column occupies 1x2 source texels; its triangle diagonal misses both
	// sample centres. The internal colour boundary is the integer texel edge.
	quad(0, .5, -2, -1, red)
	quad(.5, 1, -2, -1, blue)
	// Mirrored source triangle (2,2),(0,2),(2,1): only BR is strictly inside.
	other := meshscene.Mesh{Name: "authored-quarter-bottom-right", Pieces: 1,
		Vertices: []meshscene.Vertex{vertex(3, -1, blue), vertex(2, -1, blue), vertex(3, -1.5, blue)},
		Indices:  []uint32{0, 1, 2}}
	return &meshscene.Scene{Name: "authored-model-coverage", Meshes: []meshscene.Mesh{quarter, mean, other},
		// The shader explicitly treats green>0.5 as unindexed white. Vertex RGB
		// supplies the authored colour; no palette identity is guessed.
		Atlas: meshscene.Texture{Width: 1, Height: 1, RGBA: []byte{0, 255, 0, 255}}}
}

func (c coverageCase) frame() meshscene.LiveFrame {
	// Matrices carry production metadata in the otherwise unused fourth row:
	// [3]=1 means unshaded; [15]=1 means visible. XYZ is an identity transform.
	pose := meshscene.Mat4{1, 0, 0, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	f := meshscene.LiveFrame{Generation: 1, Camera: [3]float32{0, 0, 1},
		Previous: []meshscene.Mat4{pose}, Current: []meshscene.Mat4{pose}, Composed: true}
	if c.Supersample {
		f.LightControls = 8
	}
	f.ModelProjections = meshscene.ModelProjectionFrame{View: [4]float32{0, 0, 4, 4}, PieceFlags: []uint32{0}}
	for i := range 3 {
		f.Instances = append(f.Instances, meshscene.Instance{Mesh: i, PoseOffset: 0,
			Visual: meshscene.ModelVisual{State: [4]float32{0, c.Opacity, 0, 0}}})
		f.ModelProjections.Records = append(f.ModelProjections.Records, meshscene.ModelProjection{
			Origin:  [4]float32{0, 0, 0, float32(meshscene.ModelProjectionKnown | meshscene.ModelProjectionDirectAll)},
			Control: [4]float32{1, 1, 1, 0}})
		f.Composition.Slots = append(f.Composition.Slots, meshscene.ModelSlot{
			Screen: [4]float32{float32(2 + i*2), 2, 1, 1}, Params: [4]float32{c.Opacity, float32(meshscene.ModelPhaseGround)}})
		f.Composition.InstanceSlots = append(f.Composition.InstanceSlots, uint32(i+1))
		f.Composition.Paint = append(f.Composition.Paint, meshscene.ModelPaint{
			Slot: uint32(i + 1), Phase: meshscene.ModelPhaseGround, Rank: uint32(i), Kind: 1, ID: uint64(i + 1)})
	}
	if c.Grouped {
		for i := range 3 {
			source := f.Composition.Slots[i]
			source.Params[0] = 1 // only the final slot applies subject opacity
			f.Composition.Slots = append(f.Composition.Slots, source)
			f.Composition.InstanceSlots[i] = uint32(i + 4)
			f.Composition.MemberGroup = append(f.Composition.MemberGroup, uint32(i+1))
			f.Composition.Groups = append(f.Composition.Groups, meshscene.ModelGroup{
				ParentInstance: uint32(i), ParentSlot: uint32(i + 4), FinalSlot: uint32(i + 1),
				Members: []meshscene.ModelGroupMember{{Instance: uint32(i), Slot: uint32(i + 4)}}})
		}
	}
	return f
}

// Expected premultiplied atlas blocks, TL,TR,BL,BR. The independent fixture
// contract checks raster output before assigning any world-resolve evidence.
func atlasBlocks() [3][4][4]byte {
	red, blue := [4]byte{255, 0, 0, 255}, [4]byte{0, 0, 255, 255}
	return [3][4][4]byte{{red, {}, {}, {}}, {red, blue, red, blue}, {{}, {}, {}, blue}}
}

func (c coverageCase) worldColors() [3][4]float32 {
	colors := [3][4]float32{{1, 0, 0, 1}, {1, 0, 0, 1}, {}}
	if c.Supersample {
		colors = [3][4]float32{{.25, 0, 0, .25}, {.5, 0, .5, 1}, {0, 0, .25, .25}}
	}
	for i := range colors {
		for j := range colors[i] {
			colors[i][j] *= c.Opacity
		}
	}
	return colors
}

type slotRecord struct {
	Screen [4]float32 `json:"screen"`
	Atlas  [4]float32 `json:"atlas"`
	Params [4]float32 `json:"params"`
}

func readSlots(data []byte, f meshscene.LiveFrame) ([]slotRecord, error) {
	if len(data) != len(f.Composition.Slots)*48 {
		return nil, fmt.Errorf("slot packing has %d bytes, want %d", len(data), len(f.Composition.Slots)*48)
	}
	slots := make([]slotRecord, len(f.Composition.Slots))
	for i, wanted := range f.Composition.Slots {
		vectors := []*[4]float32{&slots[i].Screen, &slots[i].Atlas, &slots[i].Params}
		for j, vector := range vectors {
			for k := range *vector {
				vector[k] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*48+j*16+k*4:]))
			}
		}
		s := slots[i]
		if s.Screen != wanted.Screen || s.Params != wanted.Params || s.Atlas[2] != 2 || s.Atlas[3] != 2 ||
			!integerEven(s.Atlas[0]) || !integerEven(s.Atlas[1]) {
			return nil, fmt.Errorf("slot %d must preserve screen/opacity and allocate an even-origin 2x2 body block: %+v", i+1, s)
		}
	}
	return slots, nil
}

func integerEven(v float32) bool {
	return v >= 0 && !math.IsInf(float64(v), 0) && float32(int(v)) == v && int(v)%2 == 0
}

type sampleComparison struct {
	Name       string      `json:"name"`
	Pixel      [2]int      `json:"pixel"`
	Actual     [4]*float32 `json:"actual"` // null for a nonfinite channel; exact bits remain below
	ActualHalf [4]uint16   `json:"actual_half_bits"`
	Expected   [4]float32  `json:"expected"`
}

type caseComparison struct {
	Case            coverageCase       `json:"case"`
	AtlasKind       string             `json:"atlas_kind"`
	AtlasSize       [2]int             `json:"atlas_size"`
	AtlasRowBytes   int                `json:"atlas_row_bytes"`
	WorldRowBytes   int                `json:"world_row_bytes"`
	Slots           []slotRecord       `json:"slots"`
	Samples         []sampleComparison `json:"samples"`
	AtlasMismatches int                `json:"atlas_mismatches"`
	WorldMismatches int                `json:"world_mismatches"`
	Passed          bool               `json:"passed"`
}

func comparePixels(c coverageCase, slots []slotRecord, atlas []byte, aw, ah, ar int, world []byte, wr int) (caseComparison, error) {
	r := caseComparison{Case: c, AtlasKind: "source", AtlasSize: [2]int{aw, ah}, AtlasRowBytes: ar, WorldRowBytes: wr, Slots: slots}
	if c.Grouped {
		r.AtlasKind = "merged-final"
	}
	if aw < 1 || ah < 1 || ar < aw*4 || len(atlas) != ar*ah || wr < fixtureWidth*8 || len(world) != wr*fixtureHeight || len(slots) < 3 {
		return r, fmt.Errorf("complete atlas/HDR readback and placement required")
	}
	blocks := atlasBlocks()
	wantAtlas := make([]byte, ar*ah)
	for i := range 3 {
		x, y := int(slots[i].Atlas[0]), int(slots[i].Atlas[1])
		if x < 0 || y < 0 || x+2 > aw || y+2 > ah {
			return r, fmt.Errorf("slot %d block inside actual atlas dimensions", i+1)
		}
		for j, color := range blocks[i] {
			copy(wantAtlas[(y+j/2)*ar+(x+j%2)*4:], color[:])
		}
	}
	for y := range ah {
		for x := range aw * 4 {
			if atlas[y*ar+x] != wantAtlas[y*ar+x] {
				r.AtlasMismatches++
			}
		}
	}
	colors := c.worldColors()
	for y := range fixtureHeight {
		for x := range fixtureWidth {
			want := [4]float32{}
			if y == 2 && x >= 2 && x <= 6 && x%2 == 0 {
				want = colors[(x-2)/2]
			}
			var actual [4]float32
			var actualHalf [4]uint16
			for channel := range 4 {
				half := binary.LittleEndian.Uint16(world[y*wr+x*8+channel*2:])
				actualHalf[channel] = half
				actual[channel] = halfFloat(half)
				// Every expected value is exactly representable in both RGBA8
				// source and half-float output. No tolerance hides lost coverage.
				if actual[channel] != want[channel] {
					r.WorldMismatches++
				}
			}
			if y == 2 && x >= 2 && x <= 6 && x%2 == 0 {
				var finite [4]*float32
				for i, value := range actual {
					if !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) {
						finite[i] = &value
					}
				}
				r.Samples = append(r.Samples, sampleComparison{Name: []string{"quarter-top-left", "red-blue-mean", "quarter-bottom-right"}[(x-2)/2],
					Pixel: [2]int{x, y}, Actual: finite, ActualHalf: actualHalf, Expected: want})
			}
		}
	}
	r.Passed = r.AtlasMismatches == 0 && r.WorldMismatches == 0
	if !r.Passed {
		return r, fmt.Errorf("%s: atlas byte mismatches=%d; HDR channel mismatches=%d", c.Name, r.AtlasMismatches, r.WorldMismatches)
	}
	return r, nil
}

func halfFloat(h uint16) float32 {
	sign := float64(1)
	if h&0x8000 != 0 {
		sign = -1
	}
	exponent, fraction := int((h>>10)&31), int(h&1023)
	if exponent == 31 {
		if fraction == 0 {
			return float32(math.Inf(int(sign)))
		}
		return float32(math.NaN())
	}
	if exponent == 0 {
		return float32(sign * math.Ldexp(float64(fraction), -24))
	}
	return float32(sign * math.Ldexp(float64(1024+fraction), exponent-25))
}
