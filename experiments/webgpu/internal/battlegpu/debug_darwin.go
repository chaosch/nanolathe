//go:build darwin

package battlegpu

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"os"
)

type modelDebugCapture struct {
	paths []string
	reads []*battleCapture
	png   []bool
}

// Diagnostic copies are encoded only after the measured window. Resources are
// borrowed through submission; the next callback waits for actual completion.
func captureModelDebug(d *wgpu.Device, e *wgpu.CommandEncoder, m *Models, path string, world *wgpu.Texture, worldW, worldH int) (c *modelDebugCapture, err error) {
	c = &modelDebugCapture{}
	color, shadow, poses, w, h, n := m.DebugResources()
	for i, t := range []*wgpu.Texture{color, shadow, world} {
		readW, readH := w, h
		if i == 2 {
			readW, readH = worldW, worldH
		}
		read := &battleCapture{d: d, w: readW, h: readH, row: (readW*4 + 255) &^ 255, format: wgpu.TextureFormatRGBA8Unorm}
		if i == 2 {
			read.format = m.format
		}
		read.size = uint64(read.row * readH)
		read.b, err = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "diagnostic-atlas-readback", Size: read.size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
		c.reads = append(c.reads, read)
		c.paths = append(c.paths, path+[]string{".color.png", ".shadow.png", ".world.png"}[i])
		c.png = append(c.png, true)
		if err != nil {
			return
		}
		e.TransitionTextures([]wgpu.TextureBarrier{{Texture: t, Range: wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageTextureBinding, NewUsage: wgpu.TextureUsageCopySrc}}})
		e.CopyTextureToBuffer(t, read.b, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{BytesPerRow: uint32(read.row), RowsPerImage: uint32(readH)}, TextureBase: wgpu.ImageCopyTexture{Texture: t, Aspect: gputypes.TextureAspectAll}, Size: wgpu.Extent3D{Width: uint32(readW), Height: uint32(readH), DepthOrArrayLayers: 1}}})
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
		if c.png[i] {
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
