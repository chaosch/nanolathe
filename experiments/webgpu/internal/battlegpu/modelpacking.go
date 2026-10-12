package battlegpu

import (
	"math"
	"slices"

	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

type modelPackingWork struct {
	compact bool
	order   []int
	seen    []bool
}

// SetSourcePacking selects compact body-slot placement before Prepare. Logical
// slot IDs, world paint order, group membership and exported shadow slots are
// unchanged. Compact placement remains opt-in: moving hardware raster rectangles can
// change interpolated edge rounding. False retains input-order placement.
func (m *Models) SetSourcePacking(compact bool) { m.packingWork.compact = compact }

// modelPackSlots allocates independent body sources and final subjects at 2x,
// including Metal's direct-slab slots. Even extents/origins keep each native
// pixel's four samples inside one subject. Shadows retain exported coordinates.
func modelPackSlots(input []meshscene.ModelSlot) ([]meshscene.ModelSlot, int, int, error) {
	var work modelPackingWork
	return work.pack(nil, input, nil)
}

// Direct slots take no atlas space: each keeps a zero-origin doubled extent,
// so its staged projection and face admission keep the atlas path's scale.
func (p *modelPackingWork) pack(dst, input []meshscene.ModelSlot, direct []bool) ([]meshscene.ModelSlot, int, int, error) {
	out := append(dst[:0], input...)
	width := 2048
	for _, s := range out {
		if !modelFinite(s.Screen[0]+s.Screen[1]+s.Screen[2]+s.Screen[3]) || s.Screen[2] <= 0 || s.Screen[3] <= 0 || s.Screen[2] > 4096 || s.Screen[3] > 4096 {
			return nil, 0, 0, modelError("finite positive body rectangles within doubled core atlas dimensions")
		}
		for width < 2*int(math.Ceil(float64(s.Screen[2]))) {
			width *= 2
		}
	}
	p.order = slices.Grow(p.order[:0], len(out))[:len(out)]
	for i := range p.order {
		p.order[i] = i
	}
	if p.compact {
		// Stable logical selectors remain in out; only allocation traversal moves.
		// Ceiled heights are the actual raster extents, with original index ties.
		slices.SortFunc(p.order, func(a, b int) int {
			ah, bh := math.Ceil(float64(out[a].Screen[3])), math.Ceil(float64(out[b].Screen[3]))
			if ah > bh {
				return -1
			}
			if ah < bh {
				return 1
			}
			return a - b
		})
	}
	for {
		x, y, row := 0, 0, 0
		for _, i := range p.order {
			w, h := 2*int(math.Ceil(float64(out[i].Screen[2]))), 2*int(math.Ceil(float64(out[i].Screen[3])))
			if i < len(direct) && direct[i] {
				out[i].Atlas = [4]float32{0, 0, float32(w), float32(h)}
				continue
			}
			if x+w > width {
				x, y, row = 0, y+row, 0
			}
			out[i].Atlas = [4]float32{float32(x), float32(y), float32(w), float32(h)}
			x += w
			row = max(row, h)
		}
		height := max(64, (y+row+63)&^63)
		if height <= 8192 {
			return out, width, height, nil
		}
		if width >= 8192 {
			return nil, 0, 0, modelError("all isolated doubled body slots fitting an 8192-square atlas")
		}
		width *= 2
	}
}
