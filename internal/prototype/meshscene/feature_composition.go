package meshscene

import "fmt"

// SpriteSourceKind classifies the old art adapter independently of the native
// 96-byte Sprite. Zero is unclassified caller-supplied art, NOT fallback art.
// Native packing may remove only FallbackEffect once production effects are
// installed; feature bodies/shadows retain their production painter positions.
type SpriteSourceKind uint8

const (
	SpriteSourceUnknown SpriteSourceKind = iota
	SpriteSourceFeatureBody
	SpriteSourceFeatureShadow
	SpriteSourceFallbackEffect
)

// SpriteSource is aligned to original LiveFrame.Sprites indices. FeatureID is
// the exact committed uint64 identity: no float conversion or bit payload is
// involved. FeatureIndex names this publication's committed Features element
// because its InstanceID may be zero; it is never a temporal matching key.
// Transparent selects destination ALP for static feature cursors;
// runtime body and shadow cursors are keyed opaque [03 R-RAST-01 §6].
type SpriteSource struct {
	Kind         SpriteSourceKind
	FeatureID    uint64
	FeatureIndex uint32
	Transparent  bool
}

// FeatureSpritePaint is a painter reference to ORIGINAL live.Sprites storage.
// Source is zero-based; it must be remapped after native filtering/reordering.
// Shadow precedes body at equal Phase/Rank. Submerged requests the production
// seabed promotion before water; it does not change the ordinary body rank.
type FeatureSpritePaint struct {
	Source, Rank, Phase uint32
	Row                 int32
	ID                  uint64
	FeatureIndex        uint32
	Shadow, Transparent bool
	Submerged           bool
}

type featureSpriteSources struct{ body, shadow int } // original index + 1

// SpriteSources borrows exact emission metadata until the adapter's next
// Append. The retained production host calls it immediately after Frame, on
// its presentation owner; this accessor is not an asynchronous LiveSource API.
func (b *BattleSprites) SpriteSources() []SpriteSource { return b.spriteSources }

func (r *RetainedBattle) SpriteSources(live *LiveFrame) ([]SpriteSource, error) {
	if r == nil || r.source == nil || r.previous == nil || live == nil || r.previous.frame.Tick != live.Tick || len(r.previous.frame.Sprites) != len(live.Sprites) || len(live.Sprites) > 0 && &r.previous.frame.Sprites[0] != &live.Sprites[0] {
		return nil, featureCompositionError("matching retained publication and original sprite storage")
	}
	if len(live.Sprites) == 0 {
		return nil, nil
	}
	b, ok := r.source.sprites.(*BattleSprites)
	if !ok || b.spriteSourceTick != live.Tick || len(b.spriteSources) != len(live.Sprites) {
		return nil, featureCompositionError("sprite metadata aligned to the retained publication")
	}
	return b.SpriteSources(), nil
}

func (c *ModelComposer) prepareFeatureSprites(order []ModelOrder) error {
	clear(c.featureSources)
	for i, source := range c.composition.SpriteSources {
		at := c.featureSources[source.FeatureIndex]
		switch source.Kind {
		case SpriteSourceFeatureBody:
			if at.body != 0 {
				return featureCompositionError("unique committed feature body ordinal")
			}
			at.body = i + 1
		case SpriteSourceFeatureShadow:
			if at.shadow != 0 {
				return featureCompositionError("unique committed feature shadow ordinal")
			}
			at.shadow = i + 1
		default:
			continue
		}
		c.featureSources[source.FeatureIndex] = at
	}
	for _, entry := range order {
		if !entry.Sprite || entry.Kind != 2 {
			continue
		}
		at := c.featureSources[entry.FeatureIndex]
		// Production records a feature's shadow then body, never sorting the
		// pair by billboard depth [03 R-RAST-01 §6][C-G3]. Missing, fully
		// transparent or culled frames simply have no source reference.
		if entry.FeatureShadow && at.shadow != 0 {
			c.appendFeatureSprite(at.shadow-1, entry, true)
		}
		if entry.FeatureBody && at.body != 0 {
			c.appendFeatureSprite(at.body-1, entry, false)
		}
	}
	return nil
}

func (c *ModelComposer) appendFeatureSprite(source int, entry ModelOrder, shadow bool) {
	c.composition.FeatureSprites = append(c.composition.FeatureSprites, FeatureSpritePaint{
		Source: uint32(source), Rank: entry.Rank, Phase: entry.Phase, Row: entry.Row,
		ID: entry.ID, FeatureIndex: entry.FeatureIndex, Shadow: shadow, Transparent: c.composition.SpriteSources[source].Transparent, Submerged: entry.Submerged,
	})
}

func featureCompositionError(expected string) error {
	return fmt.Errorf("nanolathe: feature sprite composition failed: logical path retained feature sprites, providers searched [production order prepared art metadata], expected %s", expected)
}
