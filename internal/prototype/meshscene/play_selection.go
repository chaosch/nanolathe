package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/units"
)

// Native rendering has a centred float camera. Rebase its pointer into the
// ordinary 1x hull-picking space, retaining that picker's strict polygon,
// visibility and size-score decisions [07 R-REV-01][07 R-SEL-02B2].
func (p *PlayController) pick(f *frame.Frame, x, y float32, cam [3]float32) (pool.Handle, frame.UnitView, bool) {
	c := p.pickCamera(cam)
	zoom := max(cam[2], 0.01)
	sx := int32((x-float32(p.viewport[0])/2)/zoom + cam[0] - float32(c.X))
	sy := int32((y-float32(p.viewport[1])/2)/zoom + cam[1] - float32(c.Z))
	return client.PickSnapshotUnit(f, sx, sy, &c, f.ViewingPlayer, p)
}
func (p *PlayController) pickCamera(cam [3]float32) camera.Camera {
	return camera.Camera{X: int32(cam[0]), Z: int32(cam[1])}
}
func (p *PlayController) project(x, y, z numeric.Fixed, cam [3]float32) (float32, float32) {
	return (float32(x)/65536-cam[0])*cam[2] + float32(p.viewport[0])/2, (float32(z)/65536-float32(y)/131072-cam[1])*cam[2] + float32(p.viewport[1])/2
}
func (p *PlayController) cursorWorld(x, y float32, cam [3]float32) [3]numeric.Fixed {
	zoom := max(cam[2], 0.01)
	px := int32(cam[0] + (x-float32(p.viewport[0])/2)/zoom)
	pz := int32(cam[1] + (y-float32(p.viewport[1])/2)/zoom)
	wx, wy, wz, _ := p.sess.CursorToWorld(px, pz)
	return [3]numeric.Fixed{wx, wy, wz}
}
func playDragPoint(v [3]numeric.Fixed) [3]numeric.Fixed {
	return [3]numeric.Fixed{numeric.FixedFromInt(int64(int16(v[0] >> 16))), numeric.FixedFromInt(int64(v[1] >> 16)), numeric.FixedFromInt(int64(int16(v[2] >> 16)))}
}

// Readiness is the shell's committed form of the production selection gate:
// eligible status, finished build, single-player grace, airbase carrier
// [07 §9][08 R-TRIG-01 §3]. Live unit pools are never consulted.
func (p *PlayController) ready(f *frame.Frame, v frame.UnitView) bool {
	if v.Slot == 0 || v.Flags&units.ClassifierEligibleStatus == 0 || v.BuildRemaining != 0 {
		return false
	}
	if v.Carrier == 0 {
		return true
	}
	carrier, ok := playUnit(f, v.Carrier)
	if !ok {
		return false
	}
	def, ok := p.cat.Unit(carrier.DefName)
	return ok && def != nil && def.IsAirBase
}
func (p *PlayController) selectable(f *frame.Frame, v frame.UnitView) bool {
	return v.Owner == p.sess.LocalOwner && p.ready(f, v)
}
func (p *PlayController) selectHandles(f *frame.Frame, handles []pool.Handle, toggle bool) {
	refs := make([]pool.UnitRef, 0, len(handles))
	for _, h := range handles {
		if v, ok := playUnit(f, h); ok && p.selectable(f, v) {
			refs = append(refs, pool.UnitRef{Handle: v.Slot, Serial: v.AllocationSerial})
		}
	}
	if toggle {
		p.local.ToggleSelection(refs)
	} else {
		p.local.ReplaceSelection(refs)
	}
	p.selectionEvents++
	p.product = ""
	p.latch = 1
	p.notify("Selection updated")
}
func (p *PlayController) finishSelection(f *frame.Frame, event NativeEvent, cam [3]float32) {
	p.drag = false
	dx, dy := event.X-p.pressX, event.Y-p.pressY
	if dx*dx+dy*dy < 36*p.scale*p.scale {
		h, v, ok := p.pick(f, event.X, event.Y, cam)
		if ok && p.selectable(f, v) {
			p.selectHandles(f, []pool.Handle{h}, p.pressShift)
		} else if !p.pressShift {
			p.selectHandles(f, nil, false)
		}
		return
	}
	p.dragEnd = playDragPoint(p.cursorWorld(event.X, event.Y, cam))
	c := p.pickCamera(cam)
	band := client.WorldSelectionBand(&c, p.dragStart[0], p.dragStart[1], p.dragStart[2], p.dragEnd[0], p.dragEnd[1], p.dragEnd[2])
	handles := client.SnapshotUnitHandlesInBand(f, &c, band, f.ViewingPlayer)
	p.selectHandles(f, handles, p.pressShift)
}
