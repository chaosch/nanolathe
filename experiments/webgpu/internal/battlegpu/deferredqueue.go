package battlegpu

import "github.com/gogpu/wgpu"

// rendererQueue is the part of the device queue the renderer's components
// write through. *wgpu.Queue satisfies it; Renderer substitutes a
// deferredQueue so preparation never waits inside the queue.
type rendererQueue interface {
	WriteBuffer(*wgpu.Buffer, uint64, []byte) error
	WriteTexture(*wgpu.ImageCopyTexture, []byte, *wgpu.ImageDataLayout, *wgpu.Extent3D) error
}

type deferredWrite struct {
	buffer  *wgpu.Buffer
	offset  uint64
	texture *wgpu.ImageCopyTexture
	dst     wgpu.ImageCopyTexture
	layout  wgpu.ImageDataLayout
	size    wgpu.Extent3D
	hasLay  bool
	hasSize bool
	at, n   int
}

// deferredQueue records writes while the renderer prepares a frame and
// replays them in order once the borrowed source has been handed back.
//
// A queue write begins the frame's pending-write encoder and stages its
// bytes; on Metal that is a run of Objective-C calls that also absorbs GPU
// backpressure. Made during preparation it delayed the source worker, which
// may start the next frame only after preparation has read every borrowed
// slice. Recording copies each write's bytes (a source slice may be borrowed)
// into one reused arena; the queue copies them again at replay, synchronously,
// so the arena is free for the next frame as soon as flush returns.
type deferredQueue struct {
	q         *wgpu.Queue
	deferring bool
	writes    []deferredWrite
	arena     []byte
	// Bytes is the last flushed frame's recorded volume (diagnostic).
	Bytes int
}

func (d *deferredQueue) WriteBuffer(b *wgpu.Buffer, offset uint64, data []byte) error {
	if !d.deferring {
		return d.q.WriteBuffer(b, offset, data)
	}
	at := len(d.arena)
	d.arena = append(d.arena, data...)
	d.writes = append(d.writes, deferredWrite{buffer: b, offset: offset, at: at, n: len(data)})
	return nil
}

func (d *deferredQueue) WriteTexture(dst *wgpu.ImageCopyTexture, data []byte, layout *wgpu.ImageDataLayout, size *wgpu.Extent3D) error {
	if !d.deferring {
		return d.q.WriteTexture(dst, data, layout, size)
	}
	at := len(d.arena)
	d.arena = append(d.arena, data...)
	w := deferredWrite{at: at, n: len(data)}
	if dst != nil {
		w.dst = *dst
		w.texture = &w.dst
	}
	if layout != nil {
		w.layout, w.hasLay = *layout, true
	}
	if size != nil {
		w.size, w.hasSize = *size, true
	}
	d.writes = append(d.writes, w)
	return nil
}

// begin starts recording a frame's writes.
func (d *deferredQueue) begin() {
	d.deferring = true
	d.writes = d.writes[:0]
	d.arena = d.arena[:0]
}

// flush replays the recorded writes in order and returns the first error.
func (d *deferredQueue) flush() error {
	d.deferring = false
	d.Bytes = len(d.arena)
	var first error
	for i := range d.writes {
		w := &d.writes[i]
		data := d.arena[w.at : w.at+w.n]
		var err error
		if w.buffer != nil {
			err = d.q.WriteBuffer(w.buffer, w.offset, data)
		} else {
			var layout *wgpu.ImageDataLayout
			if w.hasLay {
				layout = &w.layout
			}
			var size *wgpu.Extent3D
			if w.hasSize {
				size = &w.size
			}
			err = d.q.WriteTexture(w.texture, data, layout, size)
		}
		if err != nil && first == nil {
			first = err
		}
	}
	clear(d.writes)
	d.writes = d.writes[:0]
	return first
}
