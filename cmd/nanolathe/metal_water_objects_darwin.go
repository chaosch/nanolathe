//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// waterObjects borrows the pinned frame and current retained identity spans,
// on the presentation owner. It neither records meshes nor steps time.
func (m *metalSubjectRules) waterObjects(cl *client.Client, world *meshscene.RetainedBattle, live *meshscene.LiveFrame, current *frame.Frame) []meshscene.WaterObject {
	if cl == nil || world == nil || live == nil || current == nil {
		return nil
	}
	m.water = resizeMetal(m.water, len(live.Instances))
	clear(m.water)
	world.VisitSubjects(live, func(i int, kind uint8, id uint64, _, _ int) {
		var r client.RetainedWaterObject
		switch kind {
		case 1:
			if v, ok := m.units[id]; ok {
				r = cl.RetainedUnitWaterObject(*v, current.Tick)
			}
		case 2:
			if id > 0 && id <= uint64(len(current.Features)) {
				r = cl.RetainedFeatureWaterObject(current.Features[id-1])
			}
		case 3:
			if v, ok := m.projectiles[id]; ok {
				r = cl.RetainedProjectileWaterObject(*v)
			}
		}
		p := &m.water[i].Params
		p[1] = r.SeaY
		if r.Reflect {
			p[0] = 1
		}
		if r.Underwater {
			p[2] = 1
		}
	})
	return m.water
}

func resizeMetal[T any](v []T, n int) []T {
	if cap(v) < n {
		return make([]T, n, n+n/4)
	}
	return v[:n]
}
