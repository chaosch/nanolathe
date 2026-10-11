//go:build darwin

package battlegpu

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"os"
	"strings"
)

type modelDebugCapture struct {
	paths []string
	reads []*battleCapture
	png   []bool
}

// Diagnostic copies are encoded only after the measured window. Resources are
// borrowed through submission; the next callback waits for actual completion.
func captureModelDebug(d *wgpu.Device, e *wgpu.CommandEncoder, renderer *Renderer, path string) (c *modelDebugCapture, err error) {
	c = &modelDebugCapture{}
	m, world, worldW, worldH := renderer.models, renderer.world, renderer.width, renderer.height
	color, shadow, poses, w, h, n := m.DebugResources()
	for i, t := range []*wgpu.Texture{color, shadow, world, m.atlas.color, m.atlas.metadata} {
		readW, readH := w, h
		if i == 2 {
			readW, readH = worldW, worldH
		}
		bytes, format, suffix, png := 4, wgpu.TextureFormatRGBA8Unorm, []string{".color.png", ".shadow.png", ".world.png", ".source.png", ".keys.png"}[i], true
		if i == 2 {
			format = renderer.format
			if format == gputypes.TextureFormatRGBA16Float {
				bytes, suffix, png = 8, ".world.rgba16f", false
			}
		}
		read := &battleCapture{d: d, w: readW, h: readH, row: (readW*bytes + 255) &^ 255, format: format}
		read.size = uint64(read.row * readH)
		read.b, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "diagnostic-atlas-readback", Size: read.size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
		c.reads = append(c.reads, read)
		c.paths = append(c.paths, path+suffix)
		c.png = append(c.png, png)
		if err != nil {
			return
		}
		e.TransitionTextures([]wgpu.TextureBarrier{{Texture: t, Range: wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageTextureBinding, NewUsage: wgpu.TextureUsageCopySrc}}})
		e.CopyTextureToBuffer(t, read.b, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{BytesPerRow: uint32(read.row), RowsPerImage: uint32(readH)}, TextureBase: wgpu.ImageCopyTexture{Texture: t, Aspect: gputypes.TextureAspectAll}, Size: wgpu.Extent3D{Width: uint32(readW), Height: uint32(readH), DepthOrArrayLayers: 1}}})
	}
	// Field copies prove native pixel output independently of submission counts.
	for _, field := range []struct {
		t           *wgpu.Texture
		w, h, bytes int
		format      gputypes.TextureFormat
		suffix      string
		png         bool
	}{
		{renderer.groundLight.texture, renderer.groundLight.textureWidth, renderer.groundLight.textureHeight, 4, wgpu.TextureFormatRGBA8Unorm, ".ground.png", true},
		{renderer.glow.emission.t, renderer.glow.emission.w, renderer.glow.emission.h, 8, gputypes.TextureFormatRGBA16Float, ".glow.rgba16f", false},
		{renderer.glow.octaves.t, renderer.glow.octaves.w, renderer.glow.octaves.h, 8, gputypes.TextureFormatRGBA16Float, ".glow-octaves.rgba16f", false},
	} {
		if field.t == nil {
			continue
		}
		read := &battleCapture{d: d, w: field.w, h: field.h, row: (field.w*field.bytes + 255) &^ 255, format: field.format}
		read.size = uint64(read.row * read.h)
		read.b, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "diagnostic-light-readback", Size: read.size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
		c.reads = append(c.reads, read)
		c.paths = append(c.paths, path+field.suffix)
		c.png = append(c.png, field.png)
		if err != nil {
			return
		}
		e.TransitionTextures([]wgpu.TextureBarrier{{Texture: field.t, Range: wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageTextureBinding, NewUsage: wgpu.TextureUsageCopySrc}}})
		e.CopyTextureToBuffer(field.t, read.b, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{BytesPerRow: uint32(read.row), RowsPerImage: uint32(read.h)}, TextureBase: wgpu.ImageCopyTexture{Texture: field.t, Aspect: gputypes.TextureAspectAll}, Size: wgpu.Extent3D{Width: uint32(read.w), Height: uint32(read.h), DepthOrArrayLayers: 1}}})
	}
	if n > 0 {
		read := &battleCapture{d: d, size: uint64(n * 80)}
		read.b, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "diagnostic-poses-readback", Size: read.size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
		c.reads = append(c.reads, read)
		c.paths = append(c.paths, path+".poses.bin")
		c.png = append(c.png, false)
		if err != nil {
			return
		}
		e.CopyBufferToBuffer(poses, 0, read.b, 0, read.size)
	}
	instances, slots := m.DebugPacking()
	if err = os.WriteFile(path+".instances.bin", instances, 0644); err != nil {
		return
	}
	err = os.WriteFile(path+".slots.bin", slots, 0644)
	return
}
func (c *modelDebugCapture) finish() error {
	for i, r := range c.reads {
		data, ready, err := r.poll()
		if err != nil {
			return err
		}
		if !ready {
			return modelError("completed diagnostic map")
		}
		if strings.HasSuffix(c.paths[i], ".keys.png") {
			// Metadata is unassociated data: zero alpha must not erase its key.
			err = r.saveMetadata(c.paths[i], data)
		} else if c.png[i] {
			err = r.save(c.paths[i], data)
		} else {
			err = os.WriteFile(c.paths[i], data, 0644)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (c *modelDebugCapture) close() {
	if c != nil {
		for _, r := range c.reads {
			r.close()
		}
	}
}
