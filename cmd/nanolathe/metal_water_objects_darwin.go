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
	units := make(map[uint64]frame.UnitView, len(current.Units))
	features := make(map[uint64]frame.FeatureView, len(current.Features))
	projectiles := make(map[uint64]frame.ProjectileView, len(current.Projectiles))
	for _, v := range current.Units {
		units[v.InstanceID] = v
	}
	for i, v := range current.Features {
		features[uint64(i)+1] = v
	}
	for _, v := range current.Projectiles {
		projectiles[v.PresentationID] = v
	}
	resolved := world.ApplyModelRules(live, func(kind uint8, id uint64) meshscene.ModelRules {
		var r client.RetainedWaterObject
		switch kind {
		case 1:
			if v, ok := units[id]; ok {
				r = cl.RetainedUnitWaterObject(v, current.Tick)
			}
		case 2:
			if v, ok := features[id]; ok {
				r = cl.RetainedFeatureWaterObject(v)
			}
		case 3:
			if v, ok := projectiles[id]; ok {
				r = cl.RetainedProjectileWaterObject(v)
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
