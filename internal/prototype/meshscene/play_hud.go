package meshscene

import (
	"fmt"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/hud"
	"github.com/nanolathe-gg/nanolathe/internal/input"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

var (
	playInk    = [4]float32{0.91, 0.95, 0.97, 1}
	playMuted  = [4]float32{0.52, 0.65, 0.72, 1}
	playAccent = [4]float32{0.22, 0.94, 0.72, 1}
	playRed    = [4]float32{1, 0.34, 0.27, 1}
	playPanel  = [4]float32{0.025, 0.055, 0.074, 0.96}
)

// Overlay reads only committed facts, with detached local interface sections.
// It uses the prototype's compact layout rather than claiming retail chrome.
func (p *PlayController) Overlay(f *frame.Frame, camera [3]float32, paused bool) []OverlayQuad {
	cur := p.compose(f)
	if cur == nil {
		return nil
	}
	var out []OverlayQuad
	s := p.scale
	w, h := float32(p.viewport[0]), float32(p.viewport[1])
	// World markings precede chrome, which hides their portion under the rail.
	for _, u := range cur.Units {
		if cur.Selection.Contains(u.Slot) {
			p.unitMark(&out, u, camera, playAccent, true)
		}
	}
	if p.pointerKnown && !p.overHUD(p.pointer.X, p.pointer.Y) {
		if _, u, ok := p.pick(cur, p.pointer.X, p.pointer.Y, camera); ok {
			color := playRed
			if u.Owner == p.sess.LocalOwner {
				color = playInk
			}
			p.unitMark(&out, u, camera, color, false)
		}
	}
	if p.drag {
		c := p.pickCamera(camera)
		band := client.WorldSelectionBand(&c, p.dragStart[0], p.dragStart[1], p.dragStart[2], p.dragEnd[0], p.dragEnd[1], p.dragEnd[2])
		a, b := band.Record.MinX, band.Record.MinY
		r := playRect{(float32(a)+float32(c.X)-camera[0])*camera[2] + w/2, (float32(b)+float32(c.Z)-camera[1])*camera[2] + h/2, float32(band.Record.MaxX-a) * camera[2], float32(band.Record.MaxY-b) * camera[2]}
		p.fill(&out, r, [4]float32{0.1, 0.9, 0.68, 0.08})
		p.outline(&out, r, max(1, s), playAccent)
	}
	if p.product != "" && p.pointerKnown && !p.overHUD(p.pointer.X, p.pointer.Y) {
		p.preview(cur, p.pointer.X, p.pointer.Y, camera)
		if def, ok := p.cat.Unit(p.product); ok && def != nil {
			x, y := p.project(numeric.FixedFromInt(int64(p.siteX*16)), numeric.FixedFromInt(int64(p.site.SiteHeight)), numeric.FixedFromInt(int64(p.siteZ*16)), camera)
			r := playRect{x, y, float32(def.FootprintX*16) * camera[2], float32(def.FootprintZ*16) * camera[2]}
			color := playAccent
			if p.siteError != nil {
				color = playRed
			}
			p.fill(&out, r, [4]float32{color[0], color[1], color[2], 0.18})
			p.outline(&out, r, 2*s, color)
			p.text(&out, r.x, r.y-24*s, 14*s, def.Name, color, 300*s)
		}
	}
	p.fill(&out, p.panel(), playPanel)
	p.fill(&out, playRect{0, 0, w, 56 * s}, [4]float32{0.018, 0.032, 0.048, 0.98})
	p.fill(&out, playRect{0, h - 36*s, w, 36 * s}, [4]float32{0.018, 0.032, 0.048, 0.96})
	p.fill(&out, playRect{299 * s, 56 * s, s, h - 92*s}, [4]float32{0.14, 0.27, 0.31, 1})
	p.text(&out, 16*s, 17*s, 20*s, "NANOLATHE", playInk, 260*s)
	p.resources(&out, cur)
	p.minimapOverlay(&out, cur, camera)
	p.selectionPanel(&out, cur)
	status := fmt.Sprintf("%02d:%02d", cur.Tick/1800, (cur.Tick/30)%60)
	if paused {
		status += "  PAUSED"
	}
	p.text(&out, w-190*s, 17*s, 16*s, status, playAccent, 180*s)
	p.text(&out, 320*s, h-25*s, 12*s, "LMB select / drag  |  Shift add / queue  |  RMB context  |  Arrows pan  |  Wheel zoom  |  C commander  |  Space pause", playMuted, w-336*s)
	p.text(&out, 16*s, h-25*s, 12*s, "Destroy the enemy commander", playMuted, 280*s)
	if p.message != "" && cur.Tick <= p.messageUntil {
		p.fill(&out, playRect{320 * s, 65 * s, min(w-336*s, 850*s), 34 * s}, [4]float32{0.02, 0.04, 0.05, 0.9})
		p.text(&out, 332*s, 75*s, 13*s, p.message, playInk, w-360*s)
	}
	if p.product != "" || p.latch != input.LatchNormal {
		label := strings.ToUpper(p.latch.String()) + " - CLICK TARGET"
		if p.product != "" {
			def, _ := p.cat.Unit(p.product)
			label = "BUILD " + strings.ToUpper(def.Name)
			if p.siteError != nil {
				label += "  /  " + p.siteError.Error()
			}
		}
		p.fill(&out, playRect{320 * s, h - 78*s, w - 336*s, 34 * s}, [4]float32{0.01, 0.07, 0.065, 0.95})
		color := playAccent
		if p.product != "" && p.siteError != nil {
			color = playRed
		}
		p.text(&out, 332*s, h-68*s, 14*s, label, color, w-360*s)
	}
	if cur.Result.Ended {
		p.resultOverlay(&out, cur)
	}
	if p.pointerKnown {
		color := playInk
		if p.product != "" || p.latch != input.LatchNormal {
			color = playAccent
		}
		x, y := p.pointer.X, p.pointer.Y
		p.fill(&out, playRect{x - 7*s, y, 14 * s, s}, color)
		p.fill(&out, playRect{x, y - 7*s, s, 14 * s}, color)
	}
	return out
}
func (p *PlayController) resources(out *[]OverlayQuad, f *frame.Frame) {
	s := p.scale
	for _, e := range f.Economy {
		if e.Player != p.sess.LocalOwner {
			continue
		}
		p.resourceBar(out, playRect{330 * s, 12 * s, 230 * s, 31 * s}, "METAL", e.Metal, e.MetalCapacity, e.MetalProduced-e.MetalConsumed, [4]float32{0.28, 0.72, 0.78, 1})
		p.resourceBar(out, playRect{590 * s, 12 * s, 230 * s, 31 * s}, "ENERGY", e.Energy, e.EnergyCapacity, e.EnergyProduced-e.EnergyConsumed, [4]float32{0.98, 0.74, 0.22, 1})
		break
	}
}
func (p *PlayController) resourceBar(out *[]OverlayQuad, r playRect, name string, value, capacity, rate float32, c [4]float32) {
	s := p.scale
	p.text(out, r.x, r.y, 12*s, fmt.Sprintf("%s  %.0f / %.0f  %+.1f", name, value, capacity, rate), playInk, r.w+20*s)
	p.fill(out, playRect{r.x, r.y + 23*s, r.w, 4 * s}, [4]float32{0.12, 0.2, 0.25, 1})
	ratio := float32(0)
	if capacity > 0 {
		ratio = max(0, min(1, value/capacity))
	}
	p.fill(out, playRect{r.x, r.y + 23*s, r.w * ratio, 4 * s}, c)
}
func (p *PlayController) minimapOverlay(out *[]OverlayQuad, f *frame.Frame, cam [3]float32) {
	s := p.scale
	r := p.minimap()
	p.text(out, 16*s, 61*s, 10*s, "TACTICAL MAP   /   CLICK TO RECENTER", playMuted, 268*s)
	p.fill(out, playRect{16 * s, 76 * s, 268 * s, 268 * s}, [4]float32{0, 0, 0, 1})
	*out = append(*out, OverlayQuad{Rect: [4]float32{r.x, r.y, r.w, r.h}, UV: [4]float32{0, 0, 1, 1}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{1}})
	if fog := f.Fog; fog.Valid && fog.W > 0 && fog.H > 0 {
		x0, z0 := float32(fog.OriginX*32+16), float32(fog.OriginZ*32+16)
		fw, fh := float32(fog.W*32), float32(fog.H*32)
		*out = append(*out, OverlayQuad{Rect: [4]float32{r.x, r.y, r.w, r.h}, UV: [4]float32{-x0 / fw, -z0 / fh, (p.mapW - x0) / fw, (p.mapH - z0) / fh}, Color: [4]float32{1, 1, 1, 1}, Params: [4]float32{2}})
	}
	for _, u := range f.Units {
		if !client.SnapshotVisible(f, u, f.ViewingPlayer) {
			continue
		}
		x := (float32(u.X) / 65536) / p.mapW
		y := (float32(u.Z)/65536 - float32(u.Y)/131072) / p.mapH
		if x < 0 || x > 1 || y < 0 || y > 1 {
			continue
		}
		c := playRed
		if u.Owner == p.sess.LocalOwner {
			c = playAccent
		} else if int(u.Owner) < len(f.Players) && f.Players[p.sess.LocalOwner].Allies[u.Owner] {
			c = [4]float32{0.34, 0.7, 1, 1}
		}
		size := 3 * s
		if f.Selection.Contains(u.Slot) {
			size = 5 * s
			c = playInk
		}
		p.fill(out, playRect{r.x + x*r.w - size/2, r.y + y*r.h - size/2, size, size}, c)
	}
	// The viewport indicator is clipped to the painted minimap.
	halfX, halfY := float32(p.viewport[0])/max(cam[2], 0.01)/2, float32(p.viewport[1])/max(cam[2], 0.01)/2
	left, top := max(0, (cam[0]-halfX)/p.mapW), max(0, (cam[1]-halfY)/p.mapH)
	right, bottom := min(1, (cam[0]+halfX)/p.mapW), min(1, (cam[1]+halfY)/p.mapH)
	if right > left && bottom > top {
		p.outline(out, playRect{r.x + left*r.w, r.y + top*r.h, (right - left) * r.w, (bottom - top) * r.h}, s, [4]float32{1, 1, 1, 0.85})
	}
	p.outline(out, r, s, [4]float32{0.3, 0.49, 0.52, 1})
}
func (p *PlayController) selectionPanel(out *[]OverlayQuad, f *frame.Frame) {
	s := p.scale
	label := "SELECT YOUR UNITS"
	if len(f.Selection.Handles) > 1 {
		label = fmt.Sprintf("%d UNITS SELECTED", len(f.Selection.Handles))
	}
	if len(f.Selection.Handles) == 1 {
		if u, ok := playUnit(f, f.Selection.Primary); ok {
			if d, ok := p.cat.Unit(u.DefName); ok && d != nil {
				label = d.Name
			} else {
				label = u.DefName
			}
			p.text(out, 16*s, 383*s, 12*s, fmt.Sprintf("Health %d / %d", u.Health, u.MaxHealth), playMuted, 268*s)
		}
	}
	p.text(out, 16*s, 357*s, 18*s, label, playInk, 268*s)
	if len(f.Selection.Handles) == 0 {
		p.text(out, 16*s, 385*s, 12*s, "Click your commander to begin", playMuted, 268*s)
	}
	keys := []uint32{'M', 'A', 'P', 'R', 'S'}
	names := []string{"MOVE", "ATTACK", "PATROL", "RECLAIM", "STOP"}
	enabled := []bool{f.CommandPage.CanMove, f.CommandPage.CanAttack, f.CommandPage.CanPatrol, f.CommandPage.CanReclaim, f.CommandPage.CanStop}
	for i, key := range keys {
		r := playRect{float32(16+i%2*138) * s, float32(414+i/2*38) * s, 130 * s, 32 * s}
		c := playMuted
		bg := [4]float32{0.07, 0.13, 0.17, 1}
		if enabled[i] {
			c = playInk
			bg = [4]float32{0.09, 0.2, 0.24, 1}
		}
		if p.latch != input.LatchNormal && strings.EqualFold(p.latch.String(), names[i]) {
			c = playAccent
			bg = [4]float32{0.035, 0.29, 0.25, 1}
		}
		p.fill(out, r, bg)
		p.text(out, r.x+10*s, r.y+9*s, 12*s, fmt.Sprintf("%c  %s", key, names[i]), c, r.w-16*s)
	}
	buildHint := "BUILD   /   SHIFT QUEUES"
	if u, ok := playUnit(f, f.CommandPage.Builder); ok && u.IsFactory {
		buildHint = "BUILD   /   SHIFT ADDS 5   /   RMB -1"
	}
	p.text(out, 16*s, 538*s, 11*s, buildHint, playMuted, 268*s)
	products := p.products(f)
	count := (len(products) + 5) / 6
	for i := 0; i < 6; i++ {
		n := p.page*6 + i
		if n >= len(products) {
			break
		}
		def, ok := p.cat.Unit(products[n])
		if !ok || def == nil {
			continue
		}
		r := playRect{16 * s, float32(562+i*42) * s, 268 * s, 38 * s}
		bg := [4]float32{0.055, 0.13, 0.16, 1}
		if p.product == def.CanonicalKey {
			bg = [4]float32{0.025, 0.28, 0.22, 1}
		}
		p.fill(out, r, bg)
		queue := ""
		for _, q := range f.OrderQueues {
			if q.Unit == f.CommandPage.Builder {
				queue = hud.QueueCountLabel([]frame.OrderQueueView{q}, products[n])
				break
			}
		}
		p.text(out, r.x+8*s, r.y+5*s, 12*s, def.Name, playInk, 214*s)
		p.text(out, r.x+224*s, r.y+5*s, 12*s, queue, playAccent, 40*s)
		costs := fmt.Sprintf("M %.0f   E %.0f", def.BuildCostMetal, def.BuildCostEnergy)
		for _, b := range f.Builds {
			if b.Builder == f.CommandPage.Builder && content.CanonicalKey(b.ProductKey) == def.CanonicalKey {
				costs += fmt.Sprintf("   %.0f%%", 100*(1-b.Remaining))
				break
			}
		}
		p.text(out, r.x+8*s, r.y+22*s, 10*s, costs, playMuted, 252*s)
	}
	if count == 0 {
		p.text(out, 16*s, 570*s, 12*s, "Select a builder or factory", playMuted, 268*s)
	}
	if count > 1 {
		p.fill(out, playRect{16 * s, 826 * s, 130 * s, 28 * s}, [4]float32{0.09, 0.2, 0.24, 1})
		p.fill(out, playRect{154 * s, 826 * s, 130 * s, 28 * s}, [4]float32{0.09, 0.2, 0.24, 1})
		p.text(out, 26*s, 835*s, 11*s, "< PREV  (,)", playInk, 116*s)
		p.text(out, 164*s, 835*s, 11*s, fmt.Sprintf("NEXT (.)  %d/%d", p.page+1, count), playInk, 116*s)
	}
}
func (p *PlayController) unitMark(out *[]OverlayQuad, u frame.UnitView, cam [3]float32, color [4]float32, health bool) {
	m := p.HullModel(u.Model)
	if m == nil || m.Root < 0 || m.Root >= len(m.Pieces) {
		return
	}
	// Draw the authored root bounds with the same orientation as the shared
	// hover picker [03 R-WATER-01 §1][07 R-REV-01 §1-§3].
	piece := m.Pieces[m.Root]
	if len(piece.Vertices) < 3 {
		return
	}
	var a, b [3]numeric.Fixed
	for _, v := range piece.Vertices {
		for axis := 0; axis < 3; axis++ {
			q := v[axis] + piece.Translate[axis]
			a[axis] = min(a[axis], q)
			b[axis] = max(b[axis], q)
		}
	}
	points := [4][3]numeric.Fixed{{a[0], a[1], a[2]}, {b[0], a[1], a[2]}, {b[0], a[1], b[2]}, {a[0], a[1], b[2]}}
	model.RotatePoints(points[:], u.Heading, u.Pitch, u.Bank)
	var xy [4][2]float32
	for i, q := range points {
		xy[i][0], xy[i][1] = p.project(u.X+q[0], u.Y+q[1], u.Z-q[2], cam)
	}
	minX, minY, maxX, maxY := xy[0][0], xy[0][1], xy[0][0], xy[0][1]
	for _, q := range xy {
		minX = min(minX, q[0])
		minY = min(minY, q[1])
		maxX = max(maxX, q[0])
		maxY = max(maxY, q[1])
	}
	if maxX < 300*p.scale || maxY < 56*p.scale || minX > float32(p.viewport[0]) || minY > float32(p.viewport[1])-36*p.scale {
		return
	}
	for i := range xy {
		p.line(out, xy[i], xy[(i+1)%4], max(1, p.scale), color)
	}
	if health && u.MaxHealth > 0 {
		x, y := p.project(u.X, u.Y, u.Z, cam)
		ratio := max(float32(0), min(1, float32(u.Health)/float32(u.MaxHealth)))
		width := 32 * p.scale
		p.fill(out, playRect{x - width/2, y - 28*p.scale, width, 4 * p.scale}, [4]float32{0.04, 0.05, 0.06, 0.9})
		p.fill(out, playRect{x - width/2, y - 28*p.scale, width * ratio, 4 * p.scale}, color)
	}
}
func (p *PlayController) line(out *[]OverlayQuad, a, b [2]float32, t float32, c [4]float32) {
	dx, dy := b[0]-a[0], b[1]-a[1]
	abs := func(v float32) float32 { return max(v, -v) }
	n := max(1, int(max(abs(dx), abs(dy))/max(2, t)))
	for i := 0; i <= n; i++ {
		f := float32(i) / float32(n)
		p.fill(out, playRect{a[0] + dx*f - t/2, a[1] + dy*f - t/2, max(2, t), max(2, t)}, c)
	}
}
func (p *PlayController) resultOverlay(out *[]OverlayQuad, f *frame.Frame) {
	s := p.scale
	w, h := float32(p.viewport[0]), float32(p.viewport[1])
	label := "DEFEAT"
	if f.Result.Draw {
		label = "DRAW"
	} else {
		for _, winner := range f.Result.Winners {
			if winner == int(p.sess.LocalOwner) {
				label = "VICTORY"
			}
		}
	}
	r := playRect{max(320*s, (w-550*s)/2), (h - 170*s) / 2, 550 * s, 170 * s}
	p.fill(out, r, [4]float32{0.02, 0.045, 0.055, 0.97})
	p.outline(out, r, 2*s, playAccent)
	p.text(out, r.x+24*s, r.y+22*s, 28*s, label, playAccent, r.w-48*s)
	p.text(out, r.x+24*s, r.y+76*s, 14*s, f.Result.Reason, playInk, r.w-48*s)
	p.text(out, r.x+24*s, r.y+126*s, 12*s, "Match complete. Close the window to exit.", playMuted, r.w-48*s)
}
