//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// The host keeps one helper alongside its retained battle. Preparation reads
// the pinned production frame and camera on the presentation owner [I6].
type metalModelComposer struct {
	orderer  *client.RetainedModelOrderer
	composer *meshscene.ModelComposer
	order    []meshscene.ModelOrder
}

func newMetalModelComposer(scene *meshscene.Scene) *metalModelComposer {
	return &metalModelComposer{orderer: client.NewRetainedModelOrderer(), composer: meshscene.NewModelComposer(scene)}
}

func (m *metalModelComposer) Prepare(cl *client.Client, retained *meshscene.RetainedBattle, live *meshscene.LiveFrame, current *frame.Frame, width, height int) (meshscene.ModelComposition, error) {
	m.order = m.order[:0]
	for _, e := range m.orderer.Prepare(cl, current) {
		m.order = append(m.order, meshscene.ModelOrder{Kind: e.Kind, ID: e.ID, GroupKind: e.GroupKind, GroupID: e.GroupID, Rank: e.Rank, Phase: e.Phase, Row: e.Row, FeatureIndex: e.FeatureIndex, Sprite: e.Sprite, FeatureBody: e.FeatureBody, FeatureShadow: e.FeatureShadow, Submerged: e.Submerged})
	}
	return m.composer.Prepare(retained, live, current, width, height, m.order)
}
