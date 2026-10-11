//go:build darwin

package battlegpu

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
	"image/png"
	"os"
)

// Final capture is an extra untimed draw into a copyable texture. Reading an
// acquired surface or callback cadence cannot establish a scanout timestamp.
type battleCapture struct {
	d         *wgpu.Device
	t         *wgpu.Texture
	view      *wgpu.TextureView
	b         *wgpu.Buffer
	pending   *wgpu.MapPending
	w, h, row int
	size      uint64
	format    gputypes.TextureFormat
}

func newBattleCapture(d *wgpu.Device, f gputypes.TextureFormat, w, h int) (c *battleCapture, err error) {
	c = &battleCapture{d: d, w: w, h: h, row: (w*4 + 255) &^ 255, format: f}
	c.size = uint64(c.row * h)
	c.t, err = d.CreateTexture(&wgpu.TextureDescriptor{Label: "untimed-final-capture", Size: wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: f, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc})
	if err != nil {
		return
	}
	c.view, err = d.CreateTextureView(c.t, nil)
	if err != nil {
		return
	}
	c.b, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "untimed-capture-readback", Size: c.size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	return
}
func (c *battleCapture) encode(e *wgpu.CommandEncoder) {
	e.TransitionTextures([]wgpu.TextureBarrier{{Texture: c.t, Range: wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageRenderAttachment, NewUsage: wgpu.TextureUsageCopySrc}}})
	e.CopyTextureToBuffer(c.t, c.b, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{BytesPerRow: uint32(c.row), RowsPerImage: uint32(c.h)}, TextureBase: wgpu.ImageCopyTexture{Texture: c.t, Aspect: gputypes.TextureAspectAll}, Size: wgpu.Extent3D{Width: uint32(c.w), Height: uint32(c.h), DepthOrArrayLayers: 1}}})
}
func (c *battleCapture) poll() ([]byte, bool, error) {
	if e := c.d.WaitIdle(); e != nil {
		return nil, true, e
	}
	if c.pending == nil {
		p, e := c.b.MapAsync(wgpu.MapModeRead, 0, c.size)
		if e != nil {
			return nil, true, e
		}
		c.pending = p
	}
	c.d.Poll(wgpu.PollPoll)
	ready, e := c.pending.Status()
	if !ready || e != nil {
		return nil, ready, e
	}
	v, e := c.b.MappedRange(0, c.size)
	if e != nil {
		return nil, true, e
	}
	data := append([]byte(nil), v.Bytes()...)
	v.Release()
	c.b.Unmap()
	c.pending.Release()
	c.pending = nil
	return data, true, nil
}
func (c *battleCapture) save(path string, data []byte) error {
	im := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	for y := range c.h {
		copy(im.Pix[y*im.Stride:y*im.Stride+c.w*4], data[y*c.row:y*c.row+c.w*4])
	}
	if c.format == wgpu.TextureFormatBGRA8Unorm || c.format == wgpu.TextureFormatBGRA8UnormSrgb {
		for i := 0; i < len(im.Pix); i += 4 {
			im.Pix[i], im.Pix[i+2] = im.Pix[i+2], im.Pix[i]
		}
	}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	e = png.Encode(f, im)
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func (c *battleCapture) close() {
	if c == nil {
		return
	}
	if c.pending != nil {
		c.pending.Release()
	}
	if c.b != nil {
		c.b.Release()
	}
	if c.view != nil {
		c.view.Release()
	}
	if c.t != nil {
		c.t.Release()
	}
}
