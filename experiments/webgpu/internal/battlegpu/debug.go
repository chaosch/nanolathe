package battlegpu

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"strings"
)

type modelDebugCapture struct {
	paths []string
	reads []*battleCapture
	png   []bool
}

// Diagnostic copies are encoded only after the measured window. Resources are
// borrowed through submission; the next callback waits for actual completion.
func captureModelDebug(d *wgpu.Device, e *wgpu.CommandEncoder, renderer *Renderer, path string, out *outputWriter) (c *modelDebugCapture, err error) {
	c = &modelDebugCapture{}
	m, world, worldW, worldH := renderer.models, renderer.world, renderer.width, renderer.height
	color, _, poses, w, h, n := m.DebugResources()
	for i, t := range []*wgpu.Texture{color, nil, world, m.atlas.color, m.atlas.metadata} {
		if t == nil {
			continue
		}
		readW, readH := w, h
		if i == 2 {
			readW, readH = worldW, worldH
		}
		bytes, format, suffix, png := 4, wgpu.TextureFormatRGBA8Unorm, []string{".color.png", ".shadow.png", ".world.png", ".source.png", ".keys.png"}[i], true
		if i == 4 {
			bytes, format, suffix, png = 2, modelKeyFormat, ".keys.rg8", false
		}
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
	shadowBody, shadowDepth, shadowMask, shadowW, shadowH := m.DebugShadowResources()
	bed, bedW, bedH := renderer.layers.DebugSeabedResource()
	for _, field := range []struct {
		t           *wgpu.Texture
		w, h, bytes int
		format      gputypes.TextureFormat
		suffix      string
		png         bool
	}{
		{bed, bedW, bedH, 8, gputypes.TextureFormatRGBA16Float, ".seabed.rgba16f", false},
		{shadowBody, shadowW, shadowH, 4, gputypes.TextureFormatRGBA8Unorm, ".shadow-body.png", true},
		{shadowDepth, shadowW, shadowH, 2, modelAtlasDepthFormat, ".shadow-depth.u16", false},
		{shadowMask, shadowW, shadowH, 1, gputypes.TextureFormatR8Unorm, ".shadow-mask.r8", false},
		{renderer.lenses.snapshot, renderer.lenses.textureWidth, renderer.lenses.textureHeight, 8, gputypes.TextureFormatRGBA16Float, ".lens-snapshot.rgba16f", false},
		{renderer.reflections.color.t, renderer.reflections.width, renderer.reflections.height, 4, wgpu.TextureFormatRGBA8Unorm, ".reflection-color.png", true},
		{renderer.reflections.soft.t, renderer.reflections.width, renderer.reflections.height, 4, wgpu.TextureFormatRGBA8Unorm, ".reflection-soft.png", true},
		{renderer.reflections.flags.t, (renderer.reflections.width + 63) / 64, (renderer.reflections.height + 63) / 64, 4, gputypes.TextureFormatR32Uint, ".reflection-flags.r32u", false},
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
		aspect := gputypes.TextureAspectAll
		if field.format == modelAtlasDepthFormat {
			aspect = gputypes.TextureAspectDepthOnly
		}
		e.TransitionTextures([]wgpu.TextureBarrier{{Texture: field.t, Range: wgpu.TextureRange{Aspect: aspect, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: wgpu.TextureUsageTextureBinding, NewUsage: wgpu.TextureUsageCopySrc}}})
		e.CopyTextureToBuffer(field.t, read.b, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{BytesPerRow: uint32(read.row), RowsPerImage: uint32(read.h)}, TextureBase: wgpu.ImageCopyTexture{Texture: field.t, Aspect: aspect}, Size: wgpu.Extent3D{Width: uint32(read.w), Height: uint32(read.h), DepthOrArrayLayers: 1}}})
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
	if err = out.write(path+".instances.bin", instances); err != nil {
		return
	}
	err = out.write(path+".slots.bin", slots)
	return
}
func (c *modelDebugCapture) finish(out *outputWriter) error {
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
			var raw, encoded []byte
			if raw, encoded, err = r.encodeMetadata(data); err == nil {
				if err = out.write(c.paths[i]+".rgba8", raw); err == nil {
					err = out.write(c.paths[i], encoded)
				}
			}
		} else if c.png[i] {
			var encoded []byte
			if encoded, err = r.encodePNG(data); err == nil {
				err = out.write(c.paths[i], encoded)
			}
		} else {
			err = out.write(c.paths[i], data)
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
