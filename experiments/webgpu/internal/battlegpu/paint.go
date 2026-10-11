package battlegpu

import (
	"cmp"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"slices"
)

type battlePaint struct {
	slot, source, phase, rank uint32
	feature                   bool
}

// Restore the producer's stable phase/rank walk after isolated model raster.
// Equal-rank feature shadow/body references retain their publication order,
// as in the existing native composition packer; no new world ordering is made.
func battlePaintOrder(dst []battlePaint, f meshscene.LiveFrame) []battlePaint {
	dst = dst[:0]
	for _, p := range f.Composition.Paint {
		if f.StockEffects != nil && p.Kind == 3 {
			continue
		}
		dst = append(dst, battlePaint{slot: p.Slot, phase: p.Phase, rank: p.Rank})
	}
	for _, p := range f.Composition.FeatureSprites {
		dst = append(dst, battlePaint{source: p.Source, phase: p.Phase, rank: p.Rank, feature: true})
	}
	slices.SortStableFunc(dst, func(a, b battlePaint) int {
		if a.phase != b.phase {
			return cmp.Compare(a.phase, b.phase)
		}
		return cmp.Compare(a.rank, b.rank)
	})
	return dst
}
