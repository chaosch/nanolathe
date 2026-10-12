package meshscene

import "testing"

func TestViewportChangeInvalidatesCameraCoverageUntilRebuild(t *testing.T) {
	r := &RetainedBattle{source: &battleSource{viewport: [2]int{640, 480}}, previous: &battlePublication{}, camera: [3]float32{0, 0, 1}}
	camera := [3]float32{0, 0, 1}
	if !r.Covers(camera) {
		t.Fatal("same camera lost coverage")
	}
	r.SetViewport(640, 480)
	if !r.Covers(camera) {
		t.Fatal("unchanged extent invalidated coverage")
	}
	r.SetViewport(-1, 480)
	if !r.Covers(camera) {
		t.Fatal("invalid extent changed coverage")
	}
	r.SetViewport(800, 600)
	if r.Covers(camera) {
		t.Fatal("old publication claimed coverage of grown viewport")
	}
	live := LiveFrame{Camera: [3]float32{123, 456, 1}}
	r.Recull(&live, camera)
	if live.Camera != [3]float32{123, 456, 1} {
		t.Fatal("recull used stale viewport publication")
	}
	r.SetViewport(320, 240)
	if r.Covers(camera) {
		t.Fatal("unrebuilt changed extent claimed coverage")
	}
}
