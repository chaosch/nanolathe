package battlegpu

import (
	"reflect"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestModelCompactPackingPreservesSelectorsAndIsolation(t *testing.T) {
	input := make([]meshscene.ModelSlot, 8)
	for i := range input {
		height := float32(1)
		if i%2 == 0 {
			height = 1000
		}
		input[i] = meshscene.ModelSlot{Screen: [4]float32{float32(i), float32(-i), 512, height}, Params: [4]float32{float32(i), .5, 2, 3}}
	}
	before := append([]meshscene.ModelSlot(nil), input...)
	compact := modelPackingWork{compact: true}
	var legacy modelPackingWork
	slots, w, h, err := compact.pack(nil, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, ow, oh, err := legacy.pack(nil, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("modified borrowed slots")
	}
	if w != ow || modelAtlasExtent(h, 0)*2 != modelAtlasExtent(oh, 0) {
		t.Fatalf("expected half resident height: compact=%dx%d original=%dx%d", w, h, ow, oh)
	}
	for i, s := range slots {
		if s.Screen != input[i].Screen || s.Params != input[i].Params {
			t.Fatalf("logical selector %d moved", i)
		}
		a := s.Atlas
		for _, v := range a {
			if int(v)%2 != 0 {
				t.Fatalf("slot %d has odd sample block %v", i, a)
			}
		}
		if a[0] < 0 || a[1] < 0 || a[0]+a[2] > float32(w) || a[1]+a[3] > float32(h) {
			t.Fatalf("slot %d out of bounds", i)
		}
		for j := 0; j < i; j++ {
			b := slots[j].Atlas
			if a[0] < b[0]+b[2] && b[0] < a[0]+a[2] && a[1] < b[1]+b[3] && b[1] < a[1]+a[3] {
				t.Fatalf("slots %d/%d overlap", i, j)
			}
		}
	}
	again, _, _, err := compact.pack(nil, input, nil)
	if err != nil || !reflect.DeepEqual(slots, again) {
		t.Fatalf("packing is not deterministic: %v", err)
	}
	if n := testing.AllocsPerRun(20, func() {
		var e error
		slots, _, _, e = compact.pack(slots, input, nil)
		if e != nil {
			panic(e)
		}
	}); n != 0 {
		t.Fatalf("warm packing allocated %g objects", n)
	}
}
