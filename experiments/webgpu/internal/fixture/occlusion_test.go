package fixture

import "testing"

func TestCoplanarOverlapIsColorAmbiguous(t *testing.T) {
	w, _ := New(64, 200, 1280, 960, "animated")
	r := Raster(w, w.Sample(17), .25)
	if !r.Sample(76, 128).DepthAmbiguous {
		t.Fatal("coplanar inter-model color tie not detected")
	}
}
