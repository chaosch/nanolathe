package metalhud

import (
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// The terrain command supplies the production inverse painted-map mapping;
// it is metadata here, never a second terrain draw. Ground shader masks are the
// same BuildWaterMask upload used by the native surface (§26, §29).
func (s *effectSink) Terrain(t drawlist.Terrain) { s.terrain = t }

var _ drawlist.TrailSink = (*effectSink)(nil)
var _ drawlist.SurfaceWakeSink = (*effectSink)(nil)
var _ drawlist.ScorchSink = (*effectSink)(nil)

func (s *effectSink) ground(m meshscene.EffectGround) {
	scale, ox, oy := float32(1), float32(0), float32(0)
	if s.world {
		scale, ox, oy = s.scale, s.ox, s.oy
	}
	m.CentreAxis[0] = m.CentreAxis[0]*scale + ox
	m.CentreAxis[1] = m.CentreAxis[1]*scale + oy
	m.CentreAxis[2] *= scale
	m.CentreAxis[3] *= scale
	m.CrossKind[0] *= scale
	m.CrossKind[1] *= scale
	ts := float32(s.terrain.Scale.Float()) * scale
	if ts <= 0 {
		s.fail("ground marks require valid terrain scale")
		return
	}
	m.Mapping = [4]float32{float32(s.terrain.OriginX) - ox/ts, float32(s.terrain.OriginY) - oy/ts, 1 / ts, 0}
	m.Clip = [4]float32{0, 0, float32(s.w), float32(s.h)}
	first := len(s.layer.Ground)
	s.layer.Ground = append(s.layer.Ground, m)
	if n := len(s.layer.Ops); n > 0 {
		last := &s.layer.Ops[n-1]
		if last.Kind == meshscene.EffectOpGround && last.First+last.Count == first {
			last.Count++
			return
		}
	}
	s.layer.Ops = append(s.layer.Ops, meshscene.EffectOp{Kind: meshscene.EffectOpGround, First: first, Count: 1})
}

func (s *effectSink) Trails(batch drawlist.Trails) {
	for _, m := range batch.Marks {
		if m.Strength == 0 || m.AxisX == 0 && m.AxisY == 0 || m.CrossX == 0 && m.CrossY == 0 {
			continue
		}
		shape := float32(0)
		if m.Shape == drawlist.TrailTrack {
			shape = 1
		}
		s.ground(meshscene.EffectGround{CentreAxis: [4]float32{float32(m.X), float32(m.Y), float32(m.AxisX) / 256, float32(m.AxisY) / 256}, CrossKind: [4]float32{float32(m.CrossX) / 256, float32(m.CrossY) / 256, 0, 0}, Values: [4]float32{0, float32(m.Strength) / 255, 0, shape}})
	}
}
func (s *effectSink) ScorchMarks(batch drawlist.ScorchMarks) {
	for i, m := range batch.Marks {
		if i >= drawlist.ScorchMarkLimit {
			break
		}
		if !(m.Radius > 0) || !(m.Age >= 0 && (m.Landing || m.Age < drawlist.ScorchLifeTicks)) {
			continue
		}
		variant := float32(m.Variant % 64)
		if m.Landing {
			variant += 64
		}
		s.ground(meshscene.EffectGround{CentreAxis: [4]float32{m.X, m.Y, m.Radius, 0}, CrossKind: [4]float32{0, m.Radius * .7, 1, 0}, Values: [4]float32{m.Age, 0, variant, 0}})
	}
}
func (s *effectSink) SurfaceWakes(batch drawlist.SurfaceWakes) {
	if !s.terrain.Water.Enabled {
		return
	}
	for _, m := range batch.Marks {
		if m.Alpha <= 0 || !m.Dust && !m.Foam {
			continue
		}
		kind := min(max(m.Age, float32(0)), 1)
		if m.Dust {
			kind += 2
		}
		alpha := m.Alpha
		if m.Foam {
			kind = 4 + min(max(m.Age, float32(0)), 1)
			alpha *= .6
		}
		s.ground(meshscene.EffectGround{CentreAxis: [4]float32{m.X, m.Y, m.AxisX, m.AxisY}, CrossKind: [4]float32{m.CrossX, m.CrossY, 2, 0}, Values: [4]float32{kind, alpha, 0, 0}})
	}
}
