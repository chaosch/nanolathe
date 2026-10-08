//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// The host owns this helper beside metalModelComposer. Call after model
// composition with the same pinned frame and metalModelComposer.order [I6].
type metalShadowComposer struct {
	composer *meshscene.ShadowComposer
	// units indexes frame's units by identity until the committed frame or
	// its tick changes (published storage may be recycled).
	units map[uint64]*frame.UnitView
	frame *frame.Frame
	tick  uint32
}

func newMetalShadowComposer(retained *meshscene.RetainedBattle) *metalShadowComposer {
	return &metalShadowComposer{composer: meshscene.NewShadowComposer(retained), units: make(map[uint64]*frame.UnitView)}
}
func (m *metalShadowComposer) Prepare(cl *client.Client, retained *meshscene.RetainedBattle, live *meshscene.LiveFrame, current *frame.Frame, width, height int, order []meshscene.ModelOrder) (meshscene.ShadowComposition, error) {
	if m.frame != current || current != nil && m.tick != current.Tick {
		clear(m.units)
		if current != nil {
			for i := range current.Units {
				u := &current.Units[i]
				m.units[u.InstanceID] = u
			}
		}
		m.frame = current
		if current != nil {
			m.tick = current.Tick
		}
	}
	return m.composer.Prepare(retained, live, current, width, height, order, func(kind uint8, id uint64) meshscene.ShadowPolicy {
		var p client.RetainedShadowPolicy
		if kind == 1 {
			if u := m.units[id]; u != nil {
				p = cl.RetainedUnitShadow(*u, current.Tick)
			}
		}
		// A feature subject's identity is its index plus one.
		if kind == 2 && id >= 1 && id <= uint64(len(current.Features)) {
			p = cl.RetainedFeatureShadow(current.Features[id-1])
		}
		return meshscene.ShadowPolicy{Rules: metalModelRules(p.Rules), Clearance: p.Clearance, Softness: p.Softness, ClipKey: p.ClipKey, Supersample: p.Supersample, Tint: p.Tint}
	})
}
