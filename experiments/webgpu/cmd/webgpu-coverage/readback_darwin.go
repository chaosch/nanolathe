//go:build darwin

package main

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type textureRead struct {
	d       *wgpu.Device
	b       *wgpu.Buffer
	pending *wgpu.MapPending
	width   int
	height  int
	row     int
	size    uint64
}

func newTextureRead(d *wgpu.Device, width, height, bytes int) (*textureRead, error) {
	r := &textureRead{d: d, width: width, height: height, row: (width*bytes + 255) &^ 255}
	r.size = uint64(r.row * height)
	var err error
	r.b, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "authored-coverage-readback", Size: r.size,
		Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Copy only after the texture's final use in this encoder. The next Prepare
// and render happen in a separate encoder after verified completion. This
// avoids the pinned CopySrc-to-render same-encoder tracking failure.
func (r *textureRead) encode(enc *wgpu.CommandEncoder, source *wgpu.Texture, old gputypes.TextureUsage) {
	enc.TransitionTextures([]wgpu.TextureBarrier{{Texture: source,
		Range: wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1},
		Usage: wgpu.TextureUsageTransition{OldUsage: old, NewUsage: wgpu.TextureUsageCopySrc}}})
	enc.CopyTextureToBuffer(source, r.b, []wgpu.BufferTextureCopy{{
		BufferLayout: wgpu.ImageDataLayout{BytesPerRow: uint32(r.row), RowsPerImage: uint32(r.height)},
		TextureBase:  wgpu.ImageCopyTexture{Texture: source, Aspect: gputypes.TextureAspectAll},
		Size:         wgpu.Extent3D{Width: uint32(r.width), Height: uint32(r.height), DepthOrArrayLayers: 1}}})
}

func (r *textureRead) poll() ([]byte, bool, error) {
	if r.pending == nil {
		var err error
		r.pending, err = r.b.MapAsync(wgpu.MapModeRead, 0, r.size)
		if err != nil {
			return nil, true, err
		}
	}
	r.d.Poll(wgpu.PollPoll)
	ready, err := r.pending.Status()
	if !ready || err != nil {
		return nil, ready, err
	}
	view, err := r.b.MappedRange(0, r.size)
	if err != nil {
		return nil, true, err
	}
	bytes := append([]byte(nil), view.Bytes()...)
	view.Release()
	if err = r.b.Unmap(); err != nil {
		return nil, true, fmt.Errorf("unmap coverage diagnostic: %w", err)
	}
	r.pending.Release()
	r.pending = nil
	return bytes, true, nil
}

func (r *textureRead) close() {
	if r == nil {
		return
	}
	if r.pending != nil {
		r.pending.Release()
	}
	if r.b != nil {
		r.b.Release()
	}
	*r = textureRead{}
}

type fixtureTarget struct {
	color, depth         *wgpu.Texture
	colorView, depthView *wgpu.TextureView
}

func newFixtureTarget(d *wgpu.Device) (t *fixtureTarget, err error) {
	t = &fixtureTarget{}
	defer func() {
		if err != nil {
			t.close()
			t = nil
		}
	}()
	t.color, err = d.CreateTexture(&wgpu.TextureDescriptor{Label: "coverage-HDR-output",
		Size:          wgpu.Extent3D{Width: fixtureWidth, Height: fixtureHeight, DepthOrArrayLayers: 1},
		MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D,
		Format: gputypes.TextureFormatRGBA16Float, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc})
	if err != nil {
		return
	}
	t.colorView, err = d.CreateTextureView(t.color, nil)
	if err != nil {
		return
	}
	t.depth, err = d.CreateTexture(&wgpu.TextureDescriptor{Label: "coverage-output-depth",
		Size:          wgpu.Extent3D{Width: fixtureWidth, Height: fixtureHeight, DepthOrArrayLayers: 1},
		MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D,
		Format: gputypes.TextureFormatDepth32Float, Usage: wgpu.TextureUsageRenderAttachment})
	if err != nil {
		return
	}
	t.depthView, err = d.CreateTextureView(t.depth, nil)
	return
}

func (t *fixtureTarget) close() {
	if t == nil {
		return
	}
	if t.depthView != nil {
		t.depthView.Release()
	}
	if t.colorView != nil {
		t.colorView.Release()
	}
	if t.depth != nil {
		t.depth.Release()
	}
	if t.color != nil {
		t.color.Release()
	}
	*t = fixtureTarget{}
}
