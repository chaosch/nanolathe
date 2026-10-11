package fixture

import (
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"math"
	"testing"
)

func TestCountsAndAnimationEnvelope(t *testing.T) {
	for _, count := range []int{64, 1000, 3000, 5000} {
		for _, detail := range []int{200, 500} {
			w, e := New(count, detail, 1920, 1080, "animated")
			if e != nil {
				t.Fatal(e)
			}
			if len(w.Scene.Instances) != count {
				t.Fatal("instance census")
			}
			for _, m := range w.Scene.Meshes {
				if m.Pieces != 3 || len(m.Indices)/3 != detail {
					t.Fatalf("pieces/triangles %d/%d", m.Pieces, len(m.Indices)/3)
				}
			}
			for _, tick := range []uint64{0, 17, 83, 239} {
				s := w.Sample(tick)
				for at, inst := range s.Instances {
					m := w.Scene.Meshes[inst.Mesh]
					minX, maxX, minY, maxY := float32(1e6), float32(-1e6), float32(1e6), float32(-1e6)
					for _, v := range m.Vertices {
						p := Point(s.Previous[at*3+int(v.Piece)], v.Position)
						x := (p[0]-8)*w.Scene.Camera[2] + 960
						y := (p[2]-w.Scene.Camera[1]-p[1]*.5)*w.Scene.Camera[2] + 540
						minX = min(minX, x)
						maxX = max(maxX, x)
						minY = min(minY, y)
						maxY = max(maxY, y)
					}
					if minX < 0 || maxX > 1920 || minY < 0 || maxY > 1080 {
						t.Fatalf("instance %d clips: %v", inst.Subject, []float32{minX, maxX, minY, maxY})
					}
					if maxX-minX < w.Cell*w.Scene.Camera[2]*.5 || maxY-minY < w.Cell*w.Scene.Camera[2]*.5 {
						t.Fatal("model spans less than half its cell")
					}
				}
			}
		}
	}
}
func TestEndpointMetadataAndRigidOracle(t *testing.T) {
	a := Reflect(rigid(0, 0, 1, 2, 3, 1))
	b := Reflect(rigid(math.Pi/2, 0, 3, 4, 5, 1))
	a[3] = 2
	b[3] = 1
	a[7] = 2
	b[7] = 4
	a[11] = 1
	b[11] = 1
	for _, alpha := range []float32{0, .25, .5, .75, 1} {
		m := Blend(a, b, alpha)
		p := Point(m, [3]float32{1, 0, 0})
		theta := float64(alpha) * math.Pi / 2
		want := [3]float64{1 + 2*float64(alpha) + math.Cos(theta), 2 + 2*float64(alpha), 3 + 2*float64(alpha) + math.Sin(theta)}
		for i := range p {
			if math.Abs(float64(p[i])-want[i]) > 1e-6 {
				t.Fatalf("alpha %v point %v want %v", alpha, p, want)
			}
		}
		if m[3] != b[3] || m[7] != 2+2*alpha || m[11] != 1 || m[15] != 1 {
			t.Fatal("metadata leaked into rotation")
		}
	}
	b[15] = 0
	for _, alpha := range []float32{0, .5, 1} {
		if Blend(a, b, alpha) != (meshscene.Mat4{}) {
			t.Fatal("either-hidden gate")
		}
	}
}
func TestShortestPathEqualAndNearBases(t *testing.T) {
	a := Reflect(rigid(170*math.Pi/180, 0, 0, 0, 0, 1))
	b := Reflect(rigid(-170*math.Pi/180, 0, 0, 0, 0, 1))
	m := Blend(a, b, .5)
	if math.Abs(float64(m[0])+1) > 1e-6 {
		t.Fatal("long rotation path")
	}
	b = a
	b[12] = 1
	if m = Blend(a, b, .25); m[0] != a[0] || m[1] != a[1] || m[10] != a[10] || m[12] != .25 {
		t.Fatal("equal basis must stay exact")
	}
	b = Reflect(rigid(170*math.Pi/180+1e-5, 0, 0, 0, 0, 1))
	m = Blend(a, b, .5)
	l := m[0]*m[0] + m[1]*m[1] + m[2]*m[2]
	if math.Abs(float64(l)-1) > 1e-6 {
		t.Fatal("near basis shrank")
	}
}
func TestHashes(t *testing.T) {
	w, e := New(3000, 200, 1920, 1080, "animated")
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]string{"mesh0": "69865a3cd0c8dd7e09773a7cb5820bd90797151371ed91cd01b24d8869ce4048", "mesh1": "0566a1727236d053913e37ebec6483ed9080698ea549fb6b5ab293c3d65215f8", "mesh2": "05c721b26ea1915e4b71c1c33fca651dbcbaa986e2675d7ecdea8b095491f40e", "texture": "2b15382253a36cc1c0270fe23421003664363e9f845f36a0a48151ba0a648faa", "configuration": "81046ee739735c67958104aafb7e9aef0b61a526374b43789088129ede9ff581", "animation_endpoints_0_1": "e39a8ec1e0d68d3b3950382f0b74719fea62248a8f9cea7dc630bcc8a2ef0208"}
	for k, v := range want {
		if w.Hashes[k] != v {
			t.Fatalf("%s hash changed: %s", k, w.Hashes[k])
		}
	}
	again, _ := New(3000, 200, 1920, 1080, "animated")
	for k, v := range w.Hashes {
		if again.Hashes[k] != v {
			t.Fatal("unstable hash")
		}
	}
}
func TestInteriorColorUVAndDepthSamples(t *testing.T) {
	w, _ := New(64, 200, 1920, 1080, "static")
	r := Raster(w, w.Sample(0), 0)
	for _, want := range []PixelSample{{100, 100, 1, [4]byte{255, 32, 16, 255}, .5000270356669128, true, false}, {116, 105, 1, [4]byte{255, 224, 32, 255}, .5000266190002461, true, false}, {80, 120, 1, [4]byte{255, 32, 16, 255}, .5000287716666211, true, false}, {115, 145, 1, [4]byte{255, 224, 32, 255}, .5000266883332879, true, false}, {280, 115, 0, [4]byte{0, 0, 0, 255}, 1, true, false}, {460, 100, 3, [4]byte{255, 224, 32, 255}, .5000270356669129, true, false}} {
		got := r.Sample(want.X, want.Y)
		if got.ID != want.ID || got.RGBA != want.RGBA || !got.Interior || math.Abs(got.Depth-want.Depth) > 1e-9 {
			t.Fatalf("pixel got %+v want %+v", got, want)
		}
	}
	// The atlas quadrants are authored explicitly and never color-space converted by this oracle.
	for _, test := range []struct {
		x, y int
		want [4]byte
	}{{2, 2, [4]byte{255, 32, 16, 255}}, {6, 2, [4]byte{16, 255, 64, 255}}, {2, 6, [4]byte{32, 64, 255, 255}}, {6, 6, [4]byte{255, 224, 32, 255}}} {
		var got [4]byte
		copy(got[:], w.Scene.Atlas.RGBA[(test.y*8+test.x)*4:])
		if got != test.want {
			t.Fatal("UV quadrant")
		}
	}
}
