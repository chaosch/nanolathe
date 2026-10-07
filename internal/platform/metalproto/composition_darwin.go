//go:build darwin

package metalproto

import (
	"cmp"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"slices"
)

// Slots isolate byte-key ownership; the separate paint walk restores the
// production subject order after retained geometry has been rasterized.
func (p *livePacking) prepareComposition(f meshscene.LiveFrame, u *liveUpload) error {
	p.compositionDraws = p.compositionDraws[:0]
	p.compositionPaint = p.compositionPaint[:0]
	p.seabed = p.seabed[:0]
	if !f.Composed {
		return nil
	}
	c := f.Composition
	if len(c.InstanceSlots) != len(f.Instances) || len(c.InstanceRanks) != len(f.Instances) {
		return fmt.Errorf("metalproto: composition selectors differ from instance count")
	}
	p.compositionSelectors = slices.Grow(p.compositionSelectors[:0], len(f.Instances))[:len(f.Instances)]
	clear(p.compositionSelectors)
	for i, slot := range c.InstanceSlots {
		if slot == 0 {
			continue
		}
		if int(slot) > len(c.Slots) {
			return fmt.Errorf("metalproto: composition slot outside atlas")
		}
		at := p.sourcePacked[i]
		p.compositionSelectors[at] = slot
		p.compositionDraws = append(p.compositionDraws, cloakDraw{uint32(f.Instances[i].Mesh), at, slot, c.InstanceRanks[i]})
	}
	slices.SortStableFunc(p.compositionDraws, func(a, b cloakDraw) int { return cmp.Compare(a.Phase, b.Phase) })
	for _, paint := range c.Paint {
		if f.StockEffects != nil && paint.Kind == 3 {
			continue
		}
		p.compositionPaint = append(p.compositionPaint, [4]uint32{paint.Slot, paint.Phase, paint.Rank, 0})
	}
	if f.StockEffects != nil {
		for i, slot := range c.ProjectileSlots {
			p.compositionPaint = append(p.compositionPaint, [4]uint32{slot, 2, uint32(i), 2})
		}
	}
	for _, paint := range c.FeatureSprites {
		if int(paint.Source) >= len(p.spritePacked) {
			return fmt.Errorf("metalproto: feature sprite outside publication")
		}
		if paint.Submerged {
			p.seabed = append(p.seabed, p.spritePacked[paint.Source])
			continue
		}
		p.compositionPaint = append(p.compositionPaint, [4]uint32{p.spritePacked[paint.Source] + 1, paint.Phase, paint.Rank, 1})
	}
	slices.SortStableFunc(p.compositionPaint, func(a, b [4]uint32) int {
		if a[1] != b[1] {
			return cmp.Compare(a[1], b[1])
		}
		return cmp.Compare(a[2], b[2])
	})
	// Annotations are grouped by slot; each slot's range is drawn in its paint
	// band before the body commit.
	p.annotations = p.annotations[:0]
	p.annotationRanges = slices.Grow(p.annotationRanges[:0], len(c.Slots))[:len(c.Slots)]
	clear(p.annotationRanges)
	if len(f.Annotations) > 0 {
		p.annotationOrder = append(p.annotationOrder[:0], f.Annotations...)
		slices.SortStableFunc(p.annotationOrder, func(a, b meshscene.Annotation) int { return cmp.Compare(a.Slot, b.Slot) })
		for _, a := range p.annotationOrder {
			if a.Slot == 0 || int(a.Slot) > len(c.Slots) {
				continue
			}
			r := &p.annotationRanges[a.Slot-1]
			if r[1] == 0 {
				r[0] = uint32(len(p.annotations))
			}
			r[1]++
			p.annotations = append(p.annotations, [8]float32{a.Rect[0], a.Rect[1], a.Rect[2], a.Rect[3], a.Color[0], a.Color[1], a.Color[2], a.Color[3]})
		}
		u.Annotations = pointer(p.annotations)
		u.AnnotationRanges = pointer(p.annotationRanges)
		u.AnnotationCount = uint32(len(p.annotations))
		u.AnnotationRangeCount = uint32(len(p.annotationRanges))
	}
	u.CompositionSlots = pointer(c.Slots)
	u.CompositionSelectors = pointer(p.compositionSelectors)
	u.CompositionDraws = pointer(p.compositionDraws)
	u.CompositionPaint = pointer(p.compositionPaint)
	u.CompositionSlotCount = uint32(len(c.Slots))
	u.CompositionDrawCount = uint32(len(p.compositionDraws))
	u.CompositionPaintCount = uint32(len(p.compositionPaint))
	u.CompositionWidth = uint32(c.AtlasWidth)
	u.CompositionHeight = uint32(c.AtlasHeight)
	u.Composed = 1
	return nil
}
