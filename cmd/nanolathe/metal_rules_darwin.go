//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/render"
)

// metalSubjectRules keeps each build's identity lookups and its rule and
// water results between builds, so a build allocates nothing. Results are
// valid until the next build's Prepare; native upload copies them first.
type metalSubjectRules struct {
	units       map[uint64]*frame.UnitView
	projectiles map[uint64]*frame.ProjectileView
	rules       []meshscene.ModelRules
	water       []meshscene.WaterObject
}

// index maps the pinned frame's unit and projectile identities.
func (m *metalSubjectRules) index(current *frame.Frame) {
	if m.units == nil {
		m.units = make(map[uint64]*frame.UnitView, len(current.Units))
		m.projectiles = make(map[uint64]*frame.ProjectileView, len(current.Projectiles))
	}
	clear(m.units)
	clear(m.projectiles)
	for i := range current.Units {
		v := &current.Units[i]
		m.units[v.InstanceID] = v
	}
	for i := range current.Projectiles {
		v := &current.Projectiles[i]
		m.projectiles[v.PresentationID] = v
	}
}

// modelRules borrows only the host's pinned current publication.
// Unit/feature policy stays in the production client; projectiles retain their
// direct keyless model path [03 §5.4][03 R-COMP-02 §6].
func (m *metalSubjectRules) modelRules(cl *client.Client, world *meshscene.RetainedBattle, live *meshscene.LiveFrame, current *frame.Frame) []meshscene.ModelRules {
	if world == nil || live == nil || current == nil {
		return nil
	}
	m.rules = world.ApplyModelRules(m.rules, live, func(kind uint8, id uint64) meshscene.ModelRules {
		resolved := client.RetainedModelRules{KeyBias: render.NanoframeHeightBias}
		switch kind {
		case 1: // Retained battle unit publication identity.
			if unit := m.units[id]; unit != nil {
				resolved = cl.RetainedUnitRules(*unit, current.Tick)
			}
		case 2: // Retained battle feature publication identity.
			if id > 0 && id <= uint64(len(current.Features)) {
				resolved = cl.RetainedFeatureRules(current.Features[id-1])
			}
		}
		return metalModelRules(resolved)
	})
	return m.rules
}

func metalModelRules(r client.RetainedModelRules) meshscene.ModelRules {
	out := meshscene.ModelRules{
		Below:   [4]float32{r.Below[0], r.Below[1], r.Below[2], render.NanoframeKeep},
		Band:    [4]float32{r.Band[0], r.Band[1], r.Band[2], render.NanoframeKeep},
		Above:   [4]float32{r.Above[0], r.Above[1], r.Above[2], render.NanoframeKeep},
		Clip:    [4]float32{float32(r.Waterline), float32(r.WaterlineKey), 0, float32(r.KeyBias)},
		Shadow:  [4]float32{0, float32(r.GroundY) / 65536, float32(r.OriginY) / 65536, float32(r.OutlineIndex)},
		Outline: r.Outline,
	}
	if r.KeyPlane {
		out.Reveal[3] = 1
		if r.Digger {
			out.Clip[2] = 1
		}
	}
	if r.Reveal != nil {
		out.Reveal[0], out.Reveal[1], out.Reveal[2] = float32(r.Reveal.Line), float32(r.Reveal.Floor), 1
		out.Below[3], out.Band[3], out.Above[3] = float32(r.Reveal.Below), float32(r.Reveal.Band), float32(r.Reveal.Above)
	}
	if r.CastsShadow {
		out.Shadow[0] = 2
		if r.Structure && !r.Digger {
			out.Shadow[0] = 1
		}
	}
	return out
}
