package battlegpu

import (
	"fmt"

	"github.com/gogpu/wgpu"
)

// Copies from a buffer into a texture need 256-byte rows on every backend.
// Region starts use 512 bytes, the strictest placement any backend asks for.
const (
	uploadRowAlign    = 256
	uploadRegionAlign = 512
	uploadMinCapacity = 1 << 20
)

type textureUpload struct {
	texture    *wgpu.Texture
	x, y, w, h uint32
	offset     uint64
	rowBytes   uint32
}

// textureUploads stages rewrites of textures that earlier submissions may
// still sample. Queue.WriteTexture cannot do this on the pinned wgpu: it
// writes a host-visible texture at once, and Apple GPUs keep every
// single-sampled texture in shared memory, so a rewrite could change texels
// a frame in flight is still reading. Buffer writes are always staged and
// ordered with GPU work, so the dirty rectangles are packed into one reused
// CPU arena, uploaded with Queue.WriteBuffer into a renderer-owned buffer, and
// copied into their textures by commands recorded on the frame's encoder
// before any pass that samples them.
//
// Regions stay pending until an encoder records them: a frame dropped after
// preparation (no drawable) leaves its rectangles for the next encoder, in
// order, so a texture whose version the frame already consumed is never left
// stale. A texture created in the same frame, which no submission has used,
// is written directly by its creator and never staged.
type textureUploads struct {
	d        *wgpu.Device
	q        modelBufferWriter
	buffer   *wgpu.Buffer
	capacity uint64
	arena    []byte
	regions  []textureUpload
	copies   []wgpu.BufferTextureCopy
	// Diagnostic: the last recorded upload's bytes and regions.
	Bytes, Regions int
}

func newTextureUploads(d *wgpu.Device, q modelBufferWriter) *textureUploads {
	return &textureUploads{d: d, q: q}
}

func uploadAlign(n, a int) int { return (n + a - 1) / a * a }

// stage copies a w x h RGBA8 rectangle at (x, y) of a tightly packed source
// image of srcWidth texels per row into the arena. The source may be borrowed;
// it is not read after stage returns.
func (u *textureUploads) stage(t *wgpu.Texture, src []byte, srcWidth, x, y, w, h int) error {
	if t == nil || w <= 0 || h <= 0 || x < 0 || y < 0 || x+w > srcWidth || len(src) < ((y+h-1)*srcWidth+x+w)*4 {
		return fmt.Errorf("nanolathe: WebGPU texture upload failed: logical path presentation/uploads, providers searched [retained scene], expected a rectangle inside its source image")
	}
	// A newer rectangle covering an older pending one supersedes it.
	kept := u.regions[:0]
	for _, r := range u.regions {
		if r.texture == t && int(r.x) >= x && int(r.y) >= y && int(r.x+r.w) <= x+w && int(r.y+r.h) <= y+h {
			continue
		}
		kept = append(kept, r)
	}
	clear(u.regions[len(kept):])
	u.regions = kept
	row := uploadAlign(w*4, uploadRowAlign)
	at := uploadAlign(len(u.arena), uploadRegionAlign)
	end := at + row*h
	if end > cap(u.arena) {
		grown := make([]byte, len(u.arena), max(end, 2*cap(u.arena)))
		copy(grown, u.arena)
		u.arena = grown
	}
	u.arena = u.arena[:end]
	for i := 0; i < h; i++ {
		from := ((y+i)*srcWidth + x) * 4
		copy(u.arena[at+i*row:at+i*row+w*4], src[from:from+w*4])
	}
	u.regions = append(u.regions, textureUpload{texture: t, x: uint32(x), y: uint32(y), w: uint32(w), h: uint32(h), offset: uint64(at), rowBytes: uint32(row)})
	return nil
}

// forget drops pending rectangles of a texture about to be released, so no
// copy is ever recorded into a released texture.
func (u *textureUploads) forget(t *wgpu.Texture) {
	if u == nil || t == nil {
		return
	}
	kept := u.regions[:0]
	for _, r := range u.regions {
		if r.texture != t {
			kept = append(kept, r)
		}
	}
	clear(u.regions[len(kept):])
	u.regions = kept
}

// record uploads the arena and records every pending copy on enc. It must run
// on the frame's encoder before the first pass that samples a staged texture.
func (u *textureUploads) record(enc *wgpu.CommandEncoder) error {
	if u == nil || len(u.regions) == 0 {
		if u != nil {
			u.arena = u.arena[:0]
			u.Bytes, u.Regions = 0, 0
		}
		return nil
	}
	if enc == nil {
		return fmt.Errorf("nanolathe: WebGPU texture upload failed: logical path presentation/uploads, providers searched [frame encoder], expected an encoder for staged copies")
	}
	need := uint64(len(u.arena))
	if u.buffer == nil || u.capacity < need {
		capacity := uint64(uploadMinCapacity)
		for capacity < need {
			capacity *= 2
		}
		b, err := u.d.CreateBuffer(&wgpu.BufferDescriptor{Label: "battle-texture-uploads", Size: capacity, Usage: wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		// Earlier copies hold their own reference until their submission
		// completes, so the old buffer may be released at once.
		if u.buffer != nil {
			u.buffer.Release()
		}
		u.buffer, u.capacity = b, capacity
	}
	// Writes above the staging belt's chunk size would each allocate a
	// one-off staging buffer; chunks of that size reuse completed ones.
	if _, err := writeModelUpload(u.q, u.buffer, u.arena, true); err != nil {
		return err
	}
	// Consecutive rectangles of one texture share a copy command; staging
	// order is kept, so overlapping rectangles land oldest first.
	for at := 0; at < len(u.regions); {
		t := u.regions[at].texture
		u.copies = u.copies[:0]
		for ; at < len(u.regions) && u.regions[at].texture == t; at++ {
			r := u.regions[at]
			u.copies = append(u.copies, wgpu.BufferTextureCopy{
				BufferLayout: wgpu.ImageDataLayout{Offset: r.offset, BytesPerRow: r.rowBytes, RowsPerImage: r.h},
				TextureBase:  wgpu.ImageCopyTexture{Texture: t, Origin: wgpu.Origin3D{X: r.x, Y: r.y}},
				Size:         wgpu.Extent3D{Width: r.w, Height: r.h, DepthOrArrayLayers: 1},
			})
		}
		enc.CopyBufferToTexture(u.buffer, t, u.copies)
	}
	clear(u.copies)
	u.Bytes, u.Regions = len(u.arena), len(u.regions)
	clear(u.regions)
	u.regions = u.regions[:0]
	u.arena = u.arena[:0]
	return nil
}

func (u *textureUploads) close() {
	if u == nil {
		return
	}
	if u.buffer != nil {
		u.buffer.Release()
		u.buffer = nil
	}
	clear(u.regions)
	u.regions, u.arena, u.copies = nil, nil, nil
}
