package battlegpu

import (
	"bytes"
	"testing"

	"github.com/gogpu/wgpu"
)

// Each staged rectangle starts on a 512-byte boundary with 256-byte rows,
// holds exactly its source texels, and leaves row padding untouched by
// neighbouring source columns.
func TestTextureUploadPacksAlignedRows(t *testing.T) {
	const width, height = 70, 9
	src := make([]byte, width*height*4)
	for i := range src {
		src[i] = byte(i*7 + 1)
	}
	tex := new(wgpu.Texture)
	u := newTextureUploads(nil, nil)
	if err := u.stage(tex, src, width, 3, 2, 65, 5); err != nil {
		t.Fatal(err)
	}
	if err := u.stage(tex, src, width, 0, 8, 1, 1); err != nil {
		t.Fatal(err)
	}
	if len(u.regions) != 2 {
		t.Fatalf("regions %d", len(u.regions))
	}
	for _, r := range u.regions {
		if r.offset%uploadRegionAlign != 0 || r.rowBytes%uploadRowAlign != 0 || r.rowBytes < r.w*4 {
			t.Fatalf("misaligned region %+v", r)
		}
		for row := uint32(0); row < r.h; row++ {
			from := (int(r.y+row)*width + int(r.x)) * 4
			at := int(r.offset) + int(row*r.rowBytes)
			if !bytes.Equal(u.arena[at:at+int(r.w)*4], src[from:from+int(r.w)*4]) {
				t.Fatalf("row %d of %+v changed", row, r)
			}
		}
	}
	if len(u.arena)%uploadRowAlign != 0 {
		t.Fatal("arena length breaks the buffer write size rule")
	}
}

// A covering rewrite supersedes older pending rectangles of the same texture
// only; a released texture loses every pending rectangle.
func TestTextureUploadSupersedesAndForgets(t *testing.T) {
	src := make([]byte, 16*16*4)
	a, b := new(wgpu.Texture), new(wgpu.Texture)
	u := newTextureUploads(nil, nil)
	for _, s := range []struct {
		t          *wgpu.Texture
		x, y, w, h int
	}{{a, 2, 2, 4, 4}, {b, 2, 2, 4, 4}, {a, 8, 8, 8, 8}, {a, 0, 0, 8, 8}} {
		if err := u.stage(s.t, src, 16, s.x, s.y, s.w, s.h); err != nil {
			t.Fatal(err)
		}
	}
	if len(u.regions) != 3 || u.regions[0].texture != b || u.regions[1].x != 8 || u.regions[2].w != 8 {
		t.Fatalf("supersede kept the wrong regions: %+v", u.regions)
	}
	u.forget(a)
	if len(u.regions) != 1 || u.regions[0].texture != b {
		t.Fatalf("forget kept a released texture: %+v", u.regions)
	}
	if err := u.stage(a, src, 16, 10, 0, 7, 1); err == nil {
		t.Fatal("rectangle outside its source accepted")
	}
}

// A replaced resource outlives the submission that flushes its pending
// queue write: it is released only once the queue's index has advanced past
// its replacement, and immediately where no index is tracked (browser).
func TestTextureUploadsRetireAfterSubmit(t *testing.T) {
	var index uint64 = 7
	u := newTextureUploads(nil, nil)
	u.submitted = func() uint64 { return index }
	released := 0
	u.retire(func() { released++ })
	u.collect()
	if released != 0 {
		t.Fatal("released before any later submission")
	}
	index++
	u.retire(func() { released += 10 })
	u.collect()
	if released != 1 {
		t.Fatalf("released %d, want only the retiree a submission has passed", released)
	}
	index++
	u.close()
	if released != 11 {
		t.Fatal("close leaked a retiree")
	}
	untracked := newTextureUploads(nil, nil)
	untracked.retire(func() { released = -1 })
	if released != -1 {
		t.Fatal("an untracked queue must release at once")
	}
}
