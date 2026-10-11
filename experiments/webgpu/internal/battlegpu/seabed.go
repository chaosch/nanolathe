package battlegpu

import (
	"encoding/binary"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

// The producer owns admission. This list copies only original source identities,
// preserving the published shadow/body sequence (GPU design §26.1, §26.3).
// The completed physical-viewport bed is presentation storage, never frame state.
type layerSeabed struct {
	target      layerTexture
	layout      *wgpu.PipelineLayout // owned by Layers' ordinary terrain layout
	raw, sprite *wgpu.RenderPipeline
	rawGroup    *wgpu.BindGroup
	sources     []uint32
	remaining   [2][]layerDraw
	active      bool
	encoded     bool
}

func layerSeabedSources(out []uint32, f meshscene.LiveFrame, remap []uint32) []uint32 {
	out = out[:0]
	// §26.3: an inactive surface retains ordinary feature replay. No further
	// geometry, height or medium inference is made from a tagged source.
	if !f.Composed || f.Water.Mask[3] <= .5 {
		return out
	}
	for _, paint := range f.Composition.FeatureSprites {
		if paint.Submerged && uint64(paint.Source) < uint64(len(remap)) && remap[paint.Source] != 0 {
			out = append(out, paint.Source)
		}
	}
	return out
}

func (l *Layers) layerTerrainEntries(bed, ground *wgpu.TextureView) []wgpu.BindGroupEntry {
	if bed == nil {
		bed = l.terrainImage.v
	}
	if ground == nil {
		ground = l.terrainImage.v
	}
	return []wgpu.BindGroupEntry{{Binding: 0, Buffer: l.uniform, Size: layerUniformSize}, {Binding: 1, TextureView: l.tileAtlas.v}, {Binding: 2, TextureView: l.lookup.v}, {Binding: 3, TextureView: l.tileDetail.v}, {Binding: 4, TextureView: l.terrainImage.v}, {Binding: 5, TextureView: ground}, {Binding: 6, TextureView: l.waterMask.v}, {Binding: 7, TextureView: bed}}
}

func (l *Layers) layerBindTerrain() error {
	return l.layerGroup(&l.terrainGroup, l.terrainLayout, l.layerTerrainEntries(l.seabed.target.v, l.groundLightView))
}

func (l *Layers) layerSeabedPipeline(shader *wgpu.ShaderModule, layout *wgpu.PipelineLayout, vertex, fragment string, blend int) (*wgpu.RenderPipeline, error) {
	pipeline, err := l.layerCreatePipeline(shader, layout, vertex, fragment, blend, 1, gputypes.TextureFormatRGBA16Float)
	if err == nil {
		l.owned = append(l.owned, pipeline.Release)
	}
	return pipeline, err
}

func (l *Layers) layerPrepareSeabed(f meshscene.LiveFrame, width, height int) error {
	bed := &l.seabed
	bed.encoded = false
	bed.sources = layerSeabedSources(bed.sources, f, l.spriteRemap)
	bed.active = len(bed.sources) != 0
	if !bed.active {
		return nil
	}
	if width > 8192 || height > 8192 {
		return layerError("seabed viewport exceeds core 8192 dimension limit")
	}
	if bed.raw == nil {
		shader, err := l.layerModule(layerTerrainShader)
		if err != nil {
			return err
		}
		bed.raw, err = l.layerSeabedPipeline(shader, bed.layout, "layer_terrain_vertex", "layer_seabed_fragment", -1)
		if err != nil {
			return err
		}
	}
	// Sprite upload and the ordinary/feature/bed family update run in Prepare
	// before this target is encoded. A lazy bed uses the same allocated tier.
	if bed.rawGroup == nil {
		// Binding 7 must never alias this pass's bed attachment, even though
		// the raw entry does not read it. The safe fallback shares group-0 ABI.
		if err := l.layerGroup(&bed.rawGroup, l.terrainLayout, l.layerTerrainEntries(nil, nil)); err != nil {
			return err
		}
	}
	if bed.target.v != nil && bed.target.w == width && bed.target.h == height {
		return nil
	}
	target := layerTexture{w: width, h: height}
	var err error
	target.t, err = l.d.CreateTexture(&wgpu.TextureDescriptor{Label: "feature-seabed", Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: gputypes.TextureFormatRGBA16Float, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc})
	if err != nil {
		return err
	}
	target.v, err = l.d.CreateTextureView(target.t, nil)
	if err != nil {
		target.close()
		return err
	}
	// Replace borrowed bindings before releasing the old view. The host has
	// already completed the preceding frame; no submitted work retains it.
	if err = l.layerGroup(&l.terrainGroup, l.terrainLayout, l.layerTerrainEntries(target.v, l.groundLightView)); err != nil {
		target.close()
		return err
	}
	bed.target.close()
	bed.target = target
	return nil
}

// layerSeabedRemaining keeps bulk DrawSprites compatible: only sources already
// committed to the bed disappear, while unpromoted features retain their band.
func layerSeabedRemaining(remap []uint32, bands [2][2]uint32, drawn []bool) [2][]layerDraw {
	skip := make([]bool, bands[0][1]+bands[1][1])
	for source, packed := range remap {
		if packed != 0 && drawn[source] {
			skip[packed-1] = true
		}
	}
	var runs [2][]layerDraw
	for phase, band := range bands {
		for at, end := band[0], band[0]+band[1]; at < end; {
			if skip[at] {
				at++
				continue
			}
			first := at
			for at < end && !skip[at] {
				at++
			}
			runs[phase] = append(runs[phase], layerDraw{first: first, count: at - first})
		}
	}
	return runs
}

func (l *Layers) layerTakeSeabedSource(source uint32) (uint32, bool) {
	if uint64(source) >= uint64(len(l.spriteRemap)) || !l.featureSources[source] || l.featureDrawn[source] || l.spriteRemap[source] == 0 {
		return 0, false
	}
	l.featureDrawn[source] = true
	return l.spriteRemap[source] - 1, true
}

// EncodeSeabed renders a distinct source attachment before DrawTerrain samples
// it. Call after Prepare, before the ordinary world pass; depth is borrowed and
// the world pass clears it afterward. No active source means no pass or upload.
func (l *Layers) EncodeSeabed(enc *wgpu.CommandEncoder, depth *wgpu.TextureView) error {
	if l.closed {
		return layerError("closed")
	}
	bed := &l.seabed
	if !bed.active || bed.encoded {
		return nil
	}
	if enc == nil || depth == nil {
		return layerError("seabed encoder and depth attachment required")
	}
	// Flags.w is the completed-bed lane; every raw shader read bypasses it.
	// Preparing alone leaves the existing direct terrain path unchanged.
	binary.LittleEndian.PutUint32(l.uniformBytes[92:], 1)
	if err := l.q.WriteBuffer(l.uniform, 92, l.uniformBytes[92:96]); err != nil {
		return err
	}
	l.counts.UploadBytes += 4
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{Label: "feature-seabed", ColorAttachments: []wgpu.RenderPassColorAttachment{{View: bed.target.v, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{A: 1}}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	if err != nil {
		return err
	}
	l.state.selectResources(pass, bed.raw, bed.rawGroup)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1})
	l.counts.Draws++
	for _, source := range bed.sources {
		packed, ok := l.layerTakeSeabedSource(source)
		if !ok {
			continue
		}
		l.state.selectResources(pass, bed.sprite, l.spriteGroup)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1, FirstInstance: packed})
		l.counts.Draws++
		l.counts.Sprites++
		l.counts.SeabedSprites++
	}
	if err := pass.End(); err != nil {
		return err
	}
	bed.encoded = true
	if !l.featurePaintOrder {
		bed.remaining = layerSeabedRemaining(l.spriteRemap, l.spriteBands, l.featureDrawn)
	}
	return nil
}

// DebugSeabedResource borrows the current-frame completed RGBA16F bed. No
// encoded bed returns nil, so inactive frames cannot expose stale source pixels.
func (l *Layers) DebugSeabedResource() (*wgpu.Texture, int, int) {
	if l == nil || l.closed || !l.seabed.encoded {
		return nil, 0, 0
	}
	return l.seabed.target.t, l.seabed.target.w, l.seabed.target.h
}
