package meshscene

import (
	"fmt"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/hud"
	"github.com/nanolathe-gg/nanolathe/internal/input"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// Process preserves native edge order and each edge's captured camera. Space
// and held camera-pan keys are handled by the native host before this boundary.
func (p *PlayController) Process(events []PlayInput, f *frame.Frame, camera [3]float32) [3]float32 {
	cur := p.compose(f)
	if cur == nil {
		return camera
	}
	for _, sample := range events {
		e, cam := sample.Event, sample.Camera
		if cam[2] <= 0 {
			cam = camera
		}
		p.events++
		if e.Kind == 1 || e.Kind == 2 || e.Kind == 3 {
			p.pointer, p.pointerCamera, p.pointerKnown = e, cam, true
			if p.drag {
				p.dragEnd = playDragPoint(p.cursorWorld(e.X, e.Y, cam))
			}
		}
		p.local.SetShiftHeld(e.Modifiers&1 != 0)
		if e.Kind == 4 {
			if e.Key == 27 {
				p.cancel()
				continue
			}
			if e.Key == 'C' {
				camera = p.commanderCamera(cur, camera)
				continue
			}
			if e.Key == ',' || e.Key == '.' {
				p.changePage(cur, e.Key == '.')
				continue
			}
			if !cur.Result.Ended {
				p.key(cur, e.Key)
			}
			continue
		}
		if e.Kind == 2 && e.Button == 1 {
			if p.product != "" || p.latch != input.LatchNormal {
				p.cancel()
				continue
			}
			if !cur.Result.Ended {
				if p.overHUD(e.X, e.Y) {
					p.panelClick(cur, e)
				} else {
					p.issueOrder(cur, e, cam, input.LatchNormal)
				}
			}
			continue
		}
		if e.Kind == 2 && e.Button == 0 {
			if r := p.minimap(); r.contains(e.X, e.Y) {
				camera[0] = (e.X - r.x) / r.w * p.mapW
				camera[1] = (e.Y - r.y) / r.h * p.mapH
				p.minimapClicks++
				continue
			}
			if p.overHUD(e.X, e.Y) {
				if !cur.Result.Ended {
					p.panelClick(cur, e)
				}
				continue
			}
			if cur.Result.Ended {
				continue
			}
			switch {
			case p.product != "":
				p.place(cur, e, cam)
			case p.latch != input.LatchNormal:
				p.issueOrder(cur, e, cam, p.latch)
				if e.Modifiers&1 == 0 {
					p.latch = input.LatchNormal
				}
			default:
				p.drag = true
				p.pressX, p.pressY = e.X, e.Y
				p.pressShift = e.Modifiers&1 != 0
				p.dragStart = playDragPoint(p.cursorWorld(e.X, e.Y, cam))
				p.dragEnd = p.dragStart
			}
		}
		if e.Kind == 3 && e.Button == 0 && p.drag {
			p.finishSelection(cur, e, cam)
		}
		// Local changes are visible to later events in the same native batch.
		cur = p.compose(f)
	}
	if p.product != "" && p.pointerKnown && !p.overHUD(p.pointer.X, p.pointer.Y) {
		p.preview(cur, p.pointer.X, p.pointer.Y, p.pointerCamera)
	}
	return camera
}
func (p *PlayController) cancel() {
	p.product = ""
	p.latch = input.LatchNormal
	p.drag = false
	p.siteError = nil
	p.notify("Cancelled")
}
func (p *PlayController) key(f *frame.Frame, key uint32) {
	switch key {
	case 'M':
		p.arm(f, input.LatchMove)
	case 'A':
		p.arm(f, input.LatchAttack)
	case 'P':
		p.arm(f, input.LatchPatrol)
	case 'R':
		p.arm(f, input.LatchReclaim)
	case 'S':
		if len(f.Selection.Handles) == 0 || !f.CommandPage.CanStop {
			return
		}
		if p.enqueue(session.HumanCommand{Kind: session.HumanStop, Stop: session.HumanStopCommand{Handles: p.selected(f)}}, "Stop") {
			p.orders++
		}
		p.product = ""
		p.latch = input.LatchNormal
	}
}
func (p *PlayController) arm(f *frame.Frame, l input.Latch) {
	page := f.CommandPage
	allowed := false
	switch l {
	case input.LatchMove:
		allowed = page.CanMove
	case input.LatchAttack:
		allowed = page.CanAttack
	case input.LatchPatrol:
		allowed = page.CanPatrol
	case input.LatchReclaim:
		allowed = page.CanReclaim
	}
	if !allowed {
		p.notify("Selection cannot " + strings.ToLower(l.String()))
		return
	}
	p.product = ""
	p.drag = false
	p.latch = l
	p.notify(l.String() + ": click a target; Shift queues; Esc cancels")
}
func (p *PlayController) commanderCamera(f *frame.Frame, cam [3]float32) [3]float32 {
	for _, u := range f.Units {
		if u.Owner != p.sess.LocalOwner {
			continue
		}
		def, ok := p.cat.Unit(u.DefName)
		if ok && def != nil && def.Commander {
			cam[0] = float32(u.X) / 65536
			cam[1] = float32(u.Z)/65536 - float32(u.Y)/131072
			return cam
		}
	}
	p.notify("No living commander")
	return cam
}
func (p *PlayController) issueOrder(f *frame.Frame, e NativeEvent, cam [3]float32, l input.Latch) {
	if len(f.Selection.Handles) == 0 {
		p.notify("Select your units first")
		return
	}
	pos3 := p.cursorWorld(e.X, e.Y, cam)
	pos := orders.ResolvePos{X: pos3[0], Y: pos3[1], Z: pos3[2], InterfaceType: orders.InterfaceTypeRightClick}
	// Mapping memory, not the display coverage channel, controls feature words
	// [04 R-ORD-02 §1]. Unit picking still has priority for the target handle.
	m := f.Visibility
	m.CoverageBytes = false
	if client.PointVisible(m, pos.X, pos.Y, pos.Z, 0, f.ViewingPlayer) {
		cx, cz := world.WorldToCell(pos.X), world.WorldToCell(pos.Z)
		for _, v := range f.Features {
			if cx < v.CX || cz < v.CZ || cx >= v.CX+max(1, int32(v.FootX)) || cz >= v.CZ+max(1, int32(v.FootZ)) {
				continue
			}
			pos.HasFeature = v.Reclaimable
			key := content.CanonicalKey(v.DefName)
			if i := strings.IndexByte(key, '_'); i >= 0 {
				key = key[:i]
			}
			_, pos.IsWreck = p.cat.Unit(key)
			pos.FeatureResurrectable = pos.IsWreck && v.Reclaimable
			break
		}
	}
	target, _, _ := p.pick(f, e.X, e.Y, cam)
	c := session.HumanCommand{Kind: session.HumanOrder, Order: session.HumanOrderCommand{Handles: p.selected(f), Code: hud.LatchToCode(l), Target: target, Position: pos, Queued: e.Modifiers&1 != 0}}
	label := l.String() + " order"
	if l == input.LatchNormal {
		label = "Context order"
	}
	if p.enqueue(c, label) {
		p.orders++
	}
}
func (p *PlayController) preview(f *frame.Frame, x, y float32, cam [3]float32) {
	def, ok := p.cat.Unit(p.product)
	if !ok || def == nil {
		p.siteError = fmt.Errorf("Product unavailable")
		return
	}
	point := p.cursorWorld(x, y, cam)
	footX, footZ := def.FootprintX, def.FootprintZ
	p.siteX, p.siteZ = world.PlacementAnchor(point[0], point[2], footX, footZ)
	p.site, p.siteError = p.sess.PreviewPlacementForCursor(p.siteX, p.siteZ, def, footX, footZ, f.CommandPage.Builder)
}
func (p *PlayController) place(f *frame.Frame, e NativeEvent, cam [3]float32) {
	p.preview(f, e.X, e.Y, cam)
	if p.siteError != nil {
		p.notify("Cannot build: " + p.siteError.Error())
		return
	}
	def, ok := p.cat.Unit(p.product)
	if !ok || def == nil || f.CommandPage.Builder == 0 {
		p.notify("Select a builder")
		return
	}
	wx, wz := world.PlacementCenter(p.siteX, p.siteZ, def.FootprintX, def.FootprintZ)
	c := session.HumanCommand{Kind: session.HumanMobileBuild, MobileBuild: session.HumanMobileBuildCommand{Builder: f.CommandPage.Builder, Product: p.product, WX: wx, WY: numeric.Fixed(int64(p.site.SiteHeight) << 16), WZ: wz, Queued: e.Modifiers&1 != 0}}
	if p.enqueue(c, "Build "+def.Name) {
		p.builds++
		if e.Modifiers&1 == 0 {
			p.product = ""
		}
	}
}
func (p *PlayController) changePage(f *frame.Frame, next bool) {
	count := (len(p.products(f)) + 5) / 6
	if count < 1 {
		p.page = 0
		return
	}
	if next {
		p.page = (p.page + 1) % count
	} else {
		p.page = (p.page + count - 1) % count
	}
}
func (p *PlayController) panelClick(f *frame.Frame, e NativeEvent) {
	s := p.scale
	keys := []uint32{'M', 'A', 'P', 'R', 'S'}
	for i, key := range keys {
		if e.Button == 0 && (playRect{float32(16+(i%2)*138) * s, float32(414+(i/2)*38) * s, 130 * s, 32 * s}).contains(e.X, e.Y) {
			p.key(f, key)
			return
		}
	}
	if e.Button == 0 && (playRect{16 * s, 826 * s, 130 * s, 28 * s}).contains(e.X, e.Y) {
		p.changePage(f, false)
		return
	}
	if e.Button == 0 && (playRect{154 * s, 826 * s, 130 * s, 28 * s}).contains(e.X, e.Y) {
		p.changePage(f, true)
		return
	}
	products := p.products(f)
	for i := 0; i < 6; i++ {
		n := p.page*6 + i
		if n >= len(products) {
			break
		}
		if !(playRect{16 * s, float32(562+i*42) * s, 268 * s, 38 * s}).contains(e.X, e.Y) {
			continue
		}
		def, ok := p.cat.Unit(products[n])
		if !ok || def == nil {
			continue
		}
		p.latch = input.LatchNormal
		p.drag = false
		if hud.ProductArmsPlacement(def) {
			if e.Button != 0 {
				return
			}
			p.product = def.CanonicalKey
			p.notify("Place " + def.Name + "; Shift queues; right-click cancels")
		} else {
			count := 1
			if e.Modifiers&1 != 0 {
				count = 5
			}
			if e.Button == 1 {
				count = -count
			}
			if p.enqueue(session.HumanCommand{Kind: session.HumanFactoryBuild, FactoryBuild: session.HumanFactoryBuildCommand{Builder: f.CommandPage.Builder, Product: def.CanonicalKey, Count: count}}, fmt.Sprintf("Queue %s x%d", def.Name, count)) {
				p.builds++
			}
		}
		return
	}
}
