//go:build darwin

package metalproto

import (
	"cmp"
	"slices"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

type orderedSprite struct {
	index int
	depth float32
}

// Keep short features in source order, before every model [03 R-RAST-01 §6].
// Remaining billboards retain the prototype scene-depth approximation, but
// sort at the position actually rendered, including the shader's depth bias.
// Scratch belongs to the synchronous native upload; publications stay immutable.
func (p *livePacking) orderSprites(source []meshscene.Sprite, alpha float32, camera [3]float32) int {
	p.sprites = p.sprites[:0]
	p.spriteOrder = p.spriteOrder[:0]
	for i, sprite := range source {
		if sprite.Current[3] == 1 {
			p.sprites = append(p.sprites, sprite)
			continue
		}
		y := sprite.Previous[1] + (sprite.Current[1]-sprite.Previous[1])*alpha
		z := sprite.Previous[2] + (sprite.Current[2]-sprite.Previous[2])*alpha
		depth := min(max(float32(0.5)-((z-camera[1])+2*y)*float32(0.00001), float32(0.001)), float32(0.999))
		depth = min(max(depth+sprite.Params[3], float32(0.0001)), float32(0.9998))
		p.spriteOrder = append(p.spriteOrder, orderedSprite{index: i, depth: depth})
	}
	ground := len(p.sprites)
	slices.SortStableFunc(p.spriteOrder, func(a, b orderedSprite) int { return cmp.Compare(b.depth, a.depth) })
	for _, entry := range p.spriteOrder {
		p.sprites = append(p.sprites, source[entry.index])
	}
	return ground
}

// Features retain production slots; fallback effect art stays in a separate
// tail until the stock producer's native consumer replaces it.
func (p *livePacking) orderCompositionSprites(f meshscene.LiveFrame) int {
	p.sprites = p.sprites[:0]
	p.spriteOrder = p.spriteOrder[:0]
	p.spritePacked = slices.Grow(p.spritePacked[:0], len(f.Sprites))[:len(f.Sprites)]
	for i, sp := range f.Sprites {
		if i < len(f.Composition.SpriteSources) {
			kind := f.Composition.SpriteSources[i].Kind
			if kind == meshscene.SpriteSourceFeatureBody || kind == meshscene.SpriteSourceFeatureShadow {
				p.spritePacked[i] = uint32(len(p.sprites))
				p.sprites = append(p.sprites, sp)
				continue
			}
		}
		if f.StockEffects != nil && i < len(f.Composition.SpriteSources) && f.Composition.SpriteSources[i].Kind == meshscene.SpriteSourceFallbackEffect {
			continue
		}
		y := sp.Previous[1] + (sp.Current[1]-sp.Previous[1])*f.Alpha
		z := sp.Previous[2] + (sp.Current[2]-sp.Previous[2])*f.Alpha
		depth := min(max(float32(.5)-((z-f.Camera[1])+2*y)*float32(.00001), float32(.001)), float32(.999))
		p.spriteOrder = append(p.spriteOrder, orderedSprite{i, depth + sp.Params[3]})
	}
	featureCount := len(p.sprites)
	slices.SortStableFunc(p.spriteOrder, func(a, b orderedSprite) int { return cmp.Compare(b.depth, a.depth) })
	for _, entry := range p.spriteOrder {
		p.spritePacked[entry.index] = uint32(len(p.sprites))
		p.sprites = append(p.sprites, f.Sprites[entry.index])
	}
	return featureCount
}
