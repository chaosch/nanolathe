//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/metalhud"
)

// metalSelection places the production selected-unit quads in their units'
// paint slots, so each body covers its own quad as presentUnit draws it
// [03 R-WATER-01 §1] rule 5. A grouped unit (cargo, factory occupant) uses
// its group's slot. Quads whose unit has no slot this frame are counted and
// not drawn: the composer culled the unit, so its quad is off screen too.
type metalSelection struct {
	slots       map[uint64]uint32
	groups      map[uint64]uint64
	quads       []meshscene.OverlayQuad
	annotations []meshscene.Annotation
	unplaced    uint64
}

func (m *metalSelection) Prepare(cl *client.Client, hud *metalhud.Foreground, order []meshscene.ModelOrder, c meshscene.ModelComposition, scale float32) []meshscene.Annotation {
	m.annotations = m.annotations[:0]
	ws, selections := cl.RetainedSelections()
	if len(selections) == 0 {
		return nil
	}
	if m.slots == nil {
		m.slots, m.groups = make(map[uint64]uint32), make(map[uint64]uint64)
	}
	clear(m.slots)
	clear(m.groups)
	for _, p := range c.Paint {
		if p.Kind == client.RetainedModelUnit {
			m.slots[p.ID] = p.Slot
		}
	}
	for _, o := range order {
		if o.Kind == client.RetainedModelUnit && o.GroupKind == client.RetainedModelUnit && o.GroupID != 0 {
			m.groups[o.ID] = o.GroupID
		}
	}
	for _, s := range selections {
		owner := s.InstanceID
		if group, ok := m.groups[owner]; ok {
			owner = group
		}
		slot := m.slots[owner]
		if slot == 0 {
			m.unplaced++
			continue
		}
		m.quads = hud.WorldLines(ws, s.Lines[:], m.quads[:0])
		for _, q := range m.quads {
			m.annotations = append(m.annotations, meshscene.Annotation{Slot: slot, Rect: [4]float32{q.Rect[0] * scale, q.Rect[1] * scale, q.Rect[2] * scale, q.Rect[3] * scale}, Color: q.Color})
		}
	}
	return m.annotations
}
