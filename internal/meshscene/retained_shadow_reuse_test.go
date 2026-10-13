package meshscene

import "testing"

// A reused shadow reads the body's atlas image, so the slot must hold the
// whole body: a viewport-clipped slot lacks the offscreen part its displaced
// shadow still shows, and a depth slab has no atlas image unless the renderer
// rasterizes every slot.
func TestShadowBodyReuseNeedsWholeAtlasBody(t *testing.T) {
	const width, height = 200, 100
	slot := func(x, y, w, h, slab float32) ModelSlot {
		return ModelSlot{Screen: [4]float32{x, y, w, h}, Params: [4]float32{1, 1, slab}}
	}
	live := &LiveFrame{}
	live.Composition.Slots = []ModelSlot{
		slot(10, 10, 20, 20, 0),  // 1: inside
		slot(-1, 10, 20, 20, 0),  // 2: clipped left
		slot(10, -1, 20, 20, 0),  // 3: clipped top
		slot(180, 10, 21, 20, 0), // 4: clipped right (x1 = width+1)
		slot(10, 80, 20, 21, 0),  // 5: clipped bottom (y1 = height+1)
		slot(10, 10, 20, 20, 1),  // 6: depth slab
		slot(0, 0, 200, 100, 0),  // 7: touches the edges without clipping
	}
	for _, tc := range []struct {
		mode ShadowBodyReuse
		want []bool
	}{
		{ShadowBodiesRaster, []bool{false, false, false, false, false, false, false}},
		{ShadowBodiesAtlasSlots, []bool{true, false, false, false, false, false, true}},
		{ShadowBodiesAllSlots, []bool{true, false, false, false, false, true, true}},
	} {
		c := &ShadowComposer{Reuse: tc.mode}
		for i, want := range tc.want {
			if got := c.reusesBody(live, uint32(i+1), width, height); got != want {
				t.Fatalf("mode %d slot %d reuse=%v want %v", tc.mode, i+1, got, want)
			}
		}
		if c.reusesBody(live, 0, width, height) || c.reusesBody(live, 8, width, height) {
			t.Fatalf("mode %d reused an absent slot", tc.mode)
		}
	}
}
