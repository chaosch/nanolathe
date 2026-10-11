package battlegpu

import (
	"strconv"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// The pinned Metal render HAL does not bind Naga's runtime-array size argument.
// Fixed declarations use the bound allocation tier, retaining every declaration
// so native buffer/texture indices still match the shared group (C-G3, C-G7).
func layerABIOverlaySource(capacities [5]int) string {
	source := layerOverlayShader
	for i, name := range [...]string{"LayerQuad", "LayerVertex", "LayerGround", "LayerSmoke", "LayerLight"} {
		source = strings.Replace(source, "array<"+name+",1>", "array<"+name+","+strconv.Itoa(max(capacities[i], 1))+">", 1)
	}
	return source
}

func layerABISpriteSource(capacity int) string {
	return strings.Replace(layerSpriteShader, "array<LayerSprite,1>", "array<LayerSprite,"+strconv.Itoa(max(capacity, 1))+">", 1)
}

type layerABIKey struct {
	capacities [5]int
	seabed     bool
}

// Growth resources never enter Layers.owned. Prepare and Close follow the host
// completion boundary, allowing replacement to drop the prior family at once.
// Release callbacks keep the module alive until its pipelines have been freed.
type layerABIBundle struct {
	key       layerABIKey
	pipelines [7]*wgpu.RenderPipeline
	releases  []func()
}

func (b *layerABIBundle) close() {
	for i := len(b.releases) - 1; i >= 0; i-- {
		b.releases[i]()
	}
	*b = layerABIBundle{}
}

// Creation is transactional: a partial candidate is freed on failure, while
// the current family remains intact. Publish before releasing the old owner.
func layerABIReplace(current *layerABIBundle, key layerABIKey, build func(*layerABIBundle) error, publish func(*layerABIBundle)) error {
	if current.key == key && len(current.releases) != 0 {
		return nil
	}
	next := layerABIBundle{key: key}
	if err := build(&next); err != nil {
		next.close()
		return err
	}
	previous := *current
	*current = next
	publish(current)
	previous.close()
	return nil
}

type layerABIFamilies struct {
	sprite, annotation, foreground, effects       layerABIBundle
	spriteLayout, annotationLayout, overlayLayout *wgpu.PipelineLayout
}

func (a *layerABIFamilies) close() {
	a.effects.close()
	a.foreground.close()
	a.annotation.close()
	a.sprite.close()
}

func (l *Layers) layerABIModule(b *layerABIBundle, source string) (*wgpu.ShaderModule, error) {
	s, err := l.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: source})
	if err == nil {
		b.releases = append(b.releases, s.Release)
	}
	return s, err
}

func (l *Layers) layerABIPipeline(b *layerABIBundle, slot int, shader *wgpu.ShaderModule, layout *wgpu.PipelineLayout, vertex, fragment string, blend, depth int, format gputypes.TextureFormat) error {
	p, err := l.layerCreatePipeline(shader, layout, vertex, fragment, blend, depth, format)
	if err == nil {
		b.pipelines[slot] = p
		b.releases = append(b.releases, p.Release)
	}
	return err
}

func (l *Layers) layerABISpriteKey() layerABIKey {
	return layerABIKey{capacities: [5]int{max(l.sprites.size/96, 1)}, seabed: l.seabed.active || l.seabed.sprite != nil}
}

func (l *Layers) layerUpdateSpritePipelines() error {
	key := l.layerABISpriteKey()
	return layerABIReplace(&l.layerABI.sprite, key, func(b *layerABIBundle) error {
		shader, err := l.layerABIModule(b, layerABISpriteSource(key.capacities[0]))
		if err != nil {
			return err
		}
		if err = l.layerABIPipeline(b, 0, shader, l.layerABI.spriteLayout, "layer_sprite_vertex", "layer_sprite_fragment", 0, 2, l.format); err != nil {
			return err
		}
		// A feature's production paint entry owns coverage; scene depth must
		// not replace its order (GPU design C-G3, C-G5; Metal design §4).
		if err = l.layerABIPipeline(b, 1, shader, l.layerABI.spriteLayout, "layer_sprite_vertex", "layer_sprite_fragment", 0, 1, l.format); err != nil {
			return err
		}
		if key.seabed {
			return l.layerABIPipeline(b, 2, shader, l.layerABI.spriteLayout, "layer_sprite_vertex", "layer_sprite_fragment", 0, 1, gputypes.TextureFormatRGBA16Float)
		}
		return nil
	}, func(b *layerABIBundle) {
		l.sprite, l.feature, l.seabed.sprite = b.pipelines[0], b.pipelines[1], b.pipelines[2]
	})
}

func (l *Layers) layerABIOverlayCapacities(quads *layerBuffer) [5]int {
	return [5]int{max(quads.size/64, 1), max(l.effectVertices.size/layerDirectVertexStride, 1), max(l.effectGround.size/80, 1), max(l.effectSmoke.size/layerSmokeStride, 1), max(l.lights.size/64, 1)}
}

func (l *Layers) layerUpdateOverlayPipelines() error {
	annotation := layerABIKey{capacities: [5]int{max(l.annotations.size/64, 1), 1, 1, 1, 1}}
	if err := layerABIReplace(&l.layerABI.annotation, annotation, func(b *layerABIBundle) error {
		shader, err := l.layerABIModule(b, layerABIOverlaySource(annotation.capacities))
		if err != nil {
			return err
		}
		return l.layerABIPipeline(b, 0, shader, l.layerABI.annotationLayout, "layer_annotation_vertex", "layer_annotation_fragment", 0, 1, l.format)
	}, func(b *layerABIBundle) { l.annotation = b.pipelines[0] }); err != nil {
		return err
	}
	foreground := layerABIKey{capacities: l.layerABIOverlayCapacities(&l.overlayQuads)}
	if err := layerABIReplace(&l.layerABI.foreground, foreground, func(b *layerABIBundle) error {
		shader, err := l.layerABIModule(b, layerABIOverlaySource(foreground.capacities))
		if err != nil {
			return err
		}
		for i, fragment := range [...]string{"layer_quad_fragment", "layer_overlay_multiply_fragment"} {
			if err = l.layerABIPipeline(b, i, shader, l.layerABI.overlayLayout, "layer_quad_vertex", fragment, i, 0, l.outputFormat); err != nil {
				return err
			}
		}
		return nil
	}, func(b *layerABIBundle) { l.overlay = [2]*wgpu.RenderPipeline{b.pipelines[0], b.pipelines[1]} }); err != nil {
		return err
	}
	effects := layerABIKey{capacities: l.layerABIOverlayCapacities(&l.effectQuads)}
	return layerABIReplace(&l.layerABI.effects, effects, func(b *layerABIBundle) error {
		shader, err := l.layerABIModule(b, layerABIOverlaySource(effects.capacities))
		if err != nil {
			return err
		}
		for i, fragment := range [...]string{"layer_quad_fragment", "layer_multiply_fragment", "layer_quad_fragment"} {
			if err = l.layerABIPipeline(b, i, shader, l.layerABI.overlayLayout, "layer_quad_vertex", fragment, i, 1, l.format); err != nil {
				return err
			}
		}
		for i, entries := range [...][2]string{{"layer_triangle_vertex", "layer_geometry_fragment"}, {"layer_ground_vertex", "layer_ground_fragment"}, {"layer_ground_vertex", "layer_trail_fragment"}, {"layer_smoke_vertex", "layer_smoke_fragment"}} {
			blend := 0
			if i == 2 {
				blend = 1
			}
			if err = l.layerABIPipeline(b, i+3, shader, l.layerABI.overlayLayout, entries[0], entries[1], blend, 1, l.format); err != nil {
				return err
			}
		}
		return nil
	}, func(b *layerABIBundle) {
		l.fx = [3]*wgpu.RenderPipeline{b.pipelines[0], b.pipelines[1], b.pipelines[2]}
		l.geometry, l.ground, l.trail, l.smoke = b.pipelines[3], b.pipelines[4], b.pipelines[5], b.pipelines[6]
	})
}
