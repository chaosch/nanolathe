//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// applyMetalWaterObjects borrows the pinned frame and current retained identity
// spans, on the presentation owner. It neither records meshes nor steps time.
func applyMetalWaterObjects(cl *client.Client, world *meshscene.RetainedBattle, live *meshscene.LiveFrame, current *frame.Frame) []meshscene.WaterObject {
	if cl == nil || world == nil || live == nil || current == nil {
		return nil
	}
	units := make(map[uint64]*frame.UnitView, len(current.Units))
	projectiles := make(map[uint64]*frame.ProjectileView, len(current.Projectiles))
	for i := range current.Units {
		v := &current.Units[i]
		units[v.InstanceID] = v
	}
	for i := range current.Projectiles {
		v := &current.Projectiles[i]
		projectiles[v.PresentationID] = v
	}
	resolved := world.ApplyModelRules(live, func(kind uint8, id uint64) meshscene.ModelRules {
		var r client.RetainedWaterObject
		switch kind {
		case 1:
			if v, ok := units[id]; ok {
				r = cl.RetainedUnitWaterObject(*v, current.Tick)
			}
		case 2:
			if id > 0 && id <= uint64(len(current.Features)) {
				r = cl.RetainedFeatureWaterObject(current.Features[id-1])
			}
		case 3:
			if v, ok := projectiles[id]; ok {
				r = cl.RetainedProjectileWaterObject(*v)
			}
		}
		var p [4]float32
		p[1] = r.SeaY
		if r.Reflect {
			p[0] = 1
		}
		if r.Underwater {
			p[2] = 1
		}
		return meshscene.ModelRules{Clip: p}
	})
	out := make([]meshscene.WaterObject, len(resolved))
	for i, r := range resolved {
		out[i].Params = r.Clip
	}
	return out
}
