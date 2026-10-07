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
	units    map[uint64]*frame.UnitView
	features map[uint64]*frame.FeatureView
}

func newMetalShadowComposer(retained *meshscene.RetainedBattle) *metalShadowComposer {
	return &metalShadowComposer{composer: meshscene.NewShadowComposer(retained), units: make(map[uint64]*frame.UnitView), features: make(map[uint64]*frame.FeatureView)}
}
func (m *metalShadowComposer) Prepare(cl *client.Client, retained *meshscene.RetainedBattle, live *meshscene.LiveFrame, current *frame.Frame, width, height int, order []meshscene.ModelOrder) (meshscene.ShadowComposition, error) {
	clear(m.units)
	clear(m.features)
	if current != nil {
		for i := range current.Units {
			u := &current.Units[i]
			m.units[u.InstanceID] = u
		}
		for i := range current.Features {
			m.features[uint64(i)+1] = &current.Features[i]
		}
	}
	return m.composer.Prepare(retained, live, current, width, height, order, func(kind uint8, id uint64) meshscene.ShadowPolicy {
		var p client.RetainedShadowPolicy
		if kind == 1 {
			if u := m.units[id]; u != nil {
				p = cl.RetainedUnitShadow(*u, current.Tick)
			}
		}
		if kind == 2 {
			if f := m.features[id]; f != nil {
				p = cl.RetainedFeatureShadow(*f)
			}
		}
		return meshscene.ShadowPolicy{Rules: metalModelRules(p.Rules), Clearance: p.Clearance, Softness: p.Softness, ClipKey: p.ClipKey, Supersample: p.Supersample, Tint: p.Tint}
	})
}
