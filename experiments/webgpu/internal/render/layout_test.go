package render

import (
	"encoding/binary"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"math"
	"testing"
)

func TestWGSLStorageOffsets(t *testing.T) {
	v := meshscene.Vertex{Position: [3]float32{2, 3, 5}, UV: [2]float32{.125, .875}, Piece: 7, Material: 3, Color: [3]float32{.25, .5, .75}}
	b := packVertices([]meshscene.Vertex{v})
	if len(b) != 48 {
		t.Fatal(len(b))
	}
	for at, want := range map[int]float32{0: 2, 4: 3, 8: 5, 12: 1, 16: .125, 20: .875, 32: .25, 36: .5, 40: .75, 44: 1} {
		if got := math.Float32frombits(binary.LittleEndian.Uint32(b[at:])); got != want {
			t.Fatalf("offset %d got %g want %g", at, got, want)
		}
	}
	if binary.LittleEndian.Uint32(b[24:]) != 7 || binary.LittleEndian.Uint32(b[28:]) != 3 {
		t.Fatal("integer storage lanes moved")
	}
}
func TestEndpointArraysAndInstanceStride(t *testing.T) {
	var a, b meshscene.Mat4
	for i := range a {
		a[i] = float32(i + 1)
		b[i] = float32(i + 21)
	}
	out := make([]byte, 128)
	packPoses(out, []meshscene.Mat4{a}, []meshscene.Mat4{b})
	if math.Float32frombits(binary.LittleEndian.Uint32(out[64:])) != 21 || math.Float32frombits(binary.LittleEndian.Uint32(out[124:])) != 36 {
		t.Fatal("endpoint arrays interleaved")
	}
	ib := make([]byte, 16)
	packInstances(ib, []meshscene.Instance{{PoseOffset: 12, Mesh: 2, Subject: 5000}})
	if binary.LittleEndian.Uint32(ib) != 12 || binary.LittleEndian.Uint32(ib[4:]) != 2 || binary.LittleEndian.Uint32(ib[8:]) != 5000 || binary.LittleEndian.Uint32(ib[12:]) != 0 {
		t.Fatal("instance stride moved")
	}
}
