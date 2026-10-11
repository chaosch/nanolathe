package battlegpu

import "github.com/gogpu/wgpu"

// Untimed selected descriptor payloads exclude driver alignment, immutable model
// texture/palette owners, old model buffer tiers, pipeline/staging caches and
// borrowed host resources. This inventory is not an actual GPU heap count.
func (r *Renderer) resourceDescriptors() map[string]any {
	type descriptor struct {
		Label, Format string
		Width, Height int
		Bytes         uint64
	}
	textures, buffers := []descriptor{}, []descriptor{}
	var texBytes, bufBytes uint64
	tex := func(label string, t *wgpu.Texture, w, h int) {
		if t == nil {
			return
		}
		n := uint64(w) * uint64(h) * uint64(t.Format().BlockCopySize())
		textures = append(textures, descriptor{label, t.Format().String(), w, h, n})
		texBytes += n
	}
	buf := func(label string, b *wgpu.Buffer) {
		if b == nil {
			return
		}
		buffers = append(buffers, descriptor{Label: label, Bytes: b.Size()})
		bufBytes += b.Size()
	}
	if r != nil {
		tex("world", r.world, r.width, r.height)
		tex("world-depth", r.depth, r.width, r.height)
		if m := r.models; m != nil {
			a := m.atlas
			tex("model-source", a.color, a.width, a.height)
			tex("model-key-coverage", a.metadata, a.width, a.height)
			tex("model-final", a.merged, a.width, a.height)
			tex("model-height-depth", a.depth, a.width, a.height)
			tex("model-legacy-shadow-dummy", a.shadow, 1, 1)
			for _, v := range []struct {
				name string
				a    modelShadowAtlas
			}{{"shadow", m.shadowWork.atlas}, {"shadow-dummy", m.shadowWork.dummy}} {
				tex(v.name+"-body", v.a.body, v.a.width, v.a.height)
				tex(v.name+"-depth", v.a.depth, v.a.width, v.a.height)
				tex(v.name+"-mask", v.a.mask, v.a.width, v.a.height)
			}
			for _, v := range []struct {
				name string
				b    *wgpu.Buffer
			}{
				{"model-vertices", m.vertices}, {"model-indices", m.indices}, {"model-materials", m.materialData}, {"model-frames", m.frameData}, {"model-centers", m.centers}, {"model-uniform", m.uniform},
				{"model-endpoints", m.endpoints.buffer}, {"model-poses", m.poses.buffer}, {"model-instances", m.instances.buffer}, {"model-selected", m.selected.buffer}, {"model-lights", m.lights.buffer}, {"model-slots", m.slots.buffer}, {"model-shadow-records", m.shadowRecords.buffer}, {"model-groups", m.groups.buffer}, {"model-members", m.members.buffer},
				{"shadow-vertices", m.shadowWork.vertices}, {"shadow-indices", m.shadowWork.indices}, {"shadow-uniform", m.shadowWork.uniform},
			} {
				buf(v.name, v.b)
			}
		}
		if l := r.layers; l != nil {
			for _, v := range []struct {
				name string
				t    layerTexture
			}{
				{"painted-terrain", l.terrainImage}, {"terrain-tiles", l.tileAtlas}, {"terrain-detail", l.tileDetail}, {"terrain-lookup", l.lookup}, {"sprite-atlas", l.spriteAtlas}, {"sprite-detail", l.spriteDetail}, {"fog-atlas", l.fogAtlas}, {"water-mask", l.waterMask}, {"overlay-atlas", l.overlayAtlas}, {"effect-atlas", l.effectAtlas}, {"fog-grid", l.fogGrid}, {"seabed", l.seabed.target},
			} {
				tex(v.name, v.t.t, v.t.w, v.t.h)
			}
			buf("layer-uniform", l.uniform)
			for _, v := range []struct {
				name string
				b    layerBuffer
			}{
				{"layer-sprites", l.sprites}, {"layer-overlay", l.overlayQuads}, {"layer-effect-quads", l.effectQuads}, {"layer-direct-vertices", l.effectVertices}, {"layer-ground", l.effectGround}, {"layer-smoke", l.effectSmoke}, {"layer-lights", l.lights}, {"layer-annotations", l.annotations},
			} {
				buf(v.name, v.b.b)
			}
		}
		if g := r.glow; g != nil {
			for _, v := range []struct {
				name string
				t    glowTexture
			}{{"glow-emission", g.emission}, {"glow-quarter", g.quarter}, {"glow-across", g.across}, {"glow-octaves", g.octaves}, {"glow-source-atlas", g.atlas}} {
				tex(v.name, v.t.t, v.t.w, v.t.h)
			}
			buf("glow-uniform", g.uniform)
			buf("glow-vertices", g.vertexBuffer)
		}
		if g := r.groundLight; g != nil {
			tex("ground-light", g.texture, g.textureWidth, g.textureHeight)
			buf("ground-light-sources", g.sources)
			buf("ground-light-discs", g.discs)
			buf("ground-light-uniform", g.uniform)
		}
		if f := r.reflections; f != nil {
			tex("reflection-color", f.color.t, f.width, f.height)
			tex("reflection-soft", f.soft.t, f.width, f.height)
			tex("reflection-tile-flags", f.flags.t, (f.width+63)/64, (f.height+63)/64)
			buf("reflection-vertices", f.vertices)
			buf("reflection-indices", f.indices)
			buf("reflection-control", f.control)
		}
		if l := r.lenses; l != nil {
			tex("lens-snapshot", l.snapshot, l.textureWidth, l.textureHeight)
		}
	}
	return map[string]any{
		"scope":    "selected current descriptor payloads, not actual GPU heap; excludes model immutable texture/palette owners, older model buffer tiers, distortion/lens buffers, driver alignment, pipeline/staging caches and borrowed host resources",
		"textures": textures, "current_buffers": buffers, "texture_descriptor_bytes": texBytes, "current_buffer_descriptor_bytes": bufBytes,
	}
}
