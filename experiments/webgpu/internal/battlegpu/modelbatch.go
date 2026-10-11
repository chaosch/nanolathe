package battlegpu

import (
	"slices"

	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

type modelBatchWork struct {
	independent      bool
	ordered          []byte
	slots, instances []bool
}

// SetSourceBatching selects the diagnostic A/B path before Prepare. Both paths
// retain immutable resident geometry, identical UVs and original world paint
// order. The default batches independent source rectangles by mesh. Shadows
// only batch adjacent compatible draws, preserving shared-slot chronology.
func (m *Models) SetSourceBatching(enabled bool) {
	m.disableModelBatching = !enabled
}

func modelBatchFlags(flags []bool, count int) []bool {
	flags = slices.Grow(flags[:0], count)[:count]
	clear(flags)
	return flags
}

// Run boundaries never cross a mesh or packed-record discontinuity. The
// outline lane additionally cannot cross an ordinary body record. This keeps
// carrier shadow members in exactly their original primitive order.
func modelDrawRun(draws []modelDraw, first int, outlines, enabled bool) int {
	end := first + 1
	if !enabled || outlines && !draws[first].outline {
		return end
	}
	for end < len(draws) && draws[end].mesh == draws[first].mesh &&
		draws[end].instance == draws[end-1].instance+1 && (!outlines || draws[end].outline) {
		end++
	}
	return end
}

func modelSlotRun(slots []uint32, first int, enabled bool) int {
	end := first + 1
	for enabled && end < len(slots) && slots[end] == slots[end-1]+1 {
		end++
	}
	return end
}

func (m *Models) modelBatchSources(f meshscene.LiveFrame) {
	w := &m.batchWork
	w.independent = false
	if m.disableModelBatching || len(m.draws) == 0 {
		return
	}
	// Ordinary producer packets have unique source selectors. Keep currently
	// accepted aliased standalone composed inputs on the original draw path:
	// reordering shared coverage or outline erasure would change their result.
	if f.Composed {
		w.slots = modelBatchFlags(w.slots, len(f.Composition.Slots))
		for _, draw := range m.draws {
			slot := f.Composition.InstanceSlots[draw.instance] - 1
			if w.slots[slot] {
				return
			}
			w.slots[slot] = true
		}
	}
	slices.SortStableFunc(m.draws, func(a, b modelDraw) int {
		if a.mesh < b.mesh {
			return -1
		}
		if a.mesh > b.mesh {
			return 1
		}
		if a.outline != b.outline {
			if a.outline {
				return -1
			}
			return 1
		}
		return 0
	})
	n := len(f.Instances) * modelInstanceStride
	w.ordered = slices.Grow(w.ordered[:0], n)[:n]
	w.instances = modelBatchFlags(w.instances, len(f.Instances))
	for i := range m.draws {
		draw := &m.draws[i]
		original := int(draw.instance)
		copy(w.ordered[i*modelInstanceStride:], m.instanceBytes[original*modelInstanceStride:(original+1)*modelInstanceStride])
		w.instances[original] = true
		draw.instance = uint32(i)
	}
	at := len(m.draws) * modelInstanceStride
	for original, drawn := range w.instances {
		if !drawn {
			copy(w.ordered[at:], m.instanceBytes[original*modelInstanceStride:(original+1)*modelInstanceStride])
			at += modelInstanceStride
		}
	}
	// Prepare has already cloned shadow/projected records from the original
	// prefix. Their indices and suffix bytes must remain unchanged. Pose and
	// group identity fields also remain original; only main draw addresses move.
	copy(m.instanceBytes[:n], w.ordered)
	w.independent = true
}
