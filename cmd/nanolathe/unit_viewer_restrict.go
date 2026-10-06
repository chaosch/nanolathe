package main

// The unit restriction editor (docs/DESIGN_MODS_MUTATORS.md §15.9). The unit
// viewer edits a content.Restrictions draft beside its own unrestricted
// preview catalog and stays presentation only: nothing here creates a battle,
// compiles a battle catalog or reaches the simulation. Opened from the
// Nanolathe screen's card it edits that screen's draft, and Back hands the
// draft back; opened with Ctrl+U at the main menu it shows Apply, with the
// number of changed names, once the draft differs from the running content's
// saved set (proposal R-P8).

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gui"
	"github.com/nanolathe-gg/nanolathe/internal/platform/screenkit"
)

// unitViewerNoLimit is the stepper's value above RestrictionMaxCount, where
// retail's slider reads No Limit [08 R-SKIR-01 §10].
const unitViewerNoLimit = content.RestrictionMaxCount + 1

// unitViewerRestrictRoute is how the editor was opened, which decides what
// leaving it does with the draft (proposal R-P8).
type unitViewerRestrictRoute uint8

const (
	// unitViewerRestrictMenu is Ctrl+U at the main menu: Apply writes the
	// running content's layer and Back discards.
	unitViewerRestrictMenu unitViewerRestrictRoute = iota
	// unitViewerRestrictCard is the Nanolathe screen's card: Back hands the
	// draft to the screen, whose own Apply and Back write or discard it.
	unitViewerRestrictCard
)

// unitViewerRestrictName is what the catalog says about one unit name. A
// restriction names a unit name and covers every record so named (§15.3,
// proposal R-P1), so the editor keeps these per name, not per record.
type unitViewerRestrictName struct {
	// lock is the catalog's refusal of a count of 0 for the name:
	// RestrictionNoRestrict, which takes no count at all, or
	// RestrictionRemovesCommander, which still takes a cap (§15.3); zero
	// when every count applies.
	lock content.RestrictionReason
	// invalid marks a name with no stored spelling, which
	// content.Restrictions refuses (an empty name, or one carrying NUL, ','
	// or '='); no stock unit has one.
	invalid bool
	records int // retained records carrying the name
}

// unitViewerRestrict is the editor's state: the draft, what Apply compares it
// with, and the running content's names.
type unitViewerRestrict struct {
	route unitViewerRestrictRoute
	// names is a lookup by restriction key, never ranged; keys lists them in
	// ascending order for the bulk edits.
	names map[string]unitViewerRestrictName
	keys  []string
	draft content.Restrictions
	// saved is the running content's saved set, which the menu route's
	// Apply count compares the draft with.
	saved content.Restrictions
	only  bool // Restricted only narrows the library with the search
	host  *gameShell
	done  func(content.Restrictions)
	wheel float64 // fractional wheel travel over the count
}

// unitViewerRestrictKey is the restriction key of a record: its unit name's
// canonical key, as content.CheckRestrictions matches records (§15.3).
func unitViewerRestrictKey(def *content.UnitDef) string {
	if def == nil {
		return ""
	}
	return content.CanonicalKey(def.UnitName)
}

// unitViewerRestrictNames reads every name of the library from the immutable
// catalog: its records, and why the catalog would refuse to remove it —
// asked of content.CheckRestrictions itself, so the editor locks exactly what
// battle entry would refuse.
func unitViewerRestrictNames(cat *content.Catalog, entries []unitViewerEntry) (map[string]unitViewerRestrictName, []string) {
	names := map[string]unitViewerRestrictName{}
	var keys []string
	var removeAll content.Restrictions
	for _, e := range entries {
		if e.Def == nil {
			continue
		}
		key := unitViewerRestrictKey(e.Def)
		n, seen := names[key]
		if !seen {
			keys = append(keys, key)
			n.invalid = removeAll.Set(key, 0) != nil
		}
		n.records++
		names[key] = n
	}
	if cat != nil && !removeAll.IsZero() {
		_, issues := cat.CheckRestrictions(removeAll)
		for _, issue := range issues {
			if n, ok := names[issue.Unit]; ok && issue.Reason != content.RestrictionUnknownUnit {
				n.lock = issue.Reason
				names[issue.Unit] = n
			}
		}
	}
	slices.Sort(keys)
	return names, keys
}

// value is the stepper's reading of key: its count, or unitViewerNoLimit.
func (e *unitViewerRestrict) value(key string) int {
	if count, ok := e.draft.Count(key); ok {
		return int(count)
	}
	return unitViewerNoLimit
}

// floor is the lowest value key may take: 0, or 1 for a side's commander,
// which can be capped but not removed; -1 when the name takes no count.
func (e *unitViewerRestrict) floor(key string) int {
	n, ok := e.names[key]
	switch {
	case !ok || n.invalid || n.lock == content.RestrictionNoRestrict:
		return -1
	case n.lock == content.RestrictionRemovesCommander:
		return 1
	}
	return 0
}

// setValue stores v for key, unitViewerNoLimit clearing its entry, and
// reports whether the name takes v.
func (e *unitViewerRestrict) setValue(key string, v int) bool {
	low := e.floor(key)
	if low < 0 || v < low || v > unitViewerNoLimit {
		return false
	}
	if v == unitViewerNoLimit {
		e.draft.Clear(key)
		return true
	}
	return e.draft.Set(key, uint8(v)) == nil
}

// toggle is the On/Off switch: Off is 0 and On is No limit, so a capped unit
// switches Off and an Off unit comes back with No limit (§15.9).
func (e *unitViewerRestrict) toggle(key string) bool {
	if e.value(key) == 0 {
		return e.setValue(key, unitViewerNoLimit)
	}
	return e.setValue(key, 0)
}

// step moves key's count over retail's slider range, 0..100 with No limit
// above: down from No limit gives 100 and up from 100 gives No limit
// [08 R-SKIR-01 §10]. Up from No limit starts a cap at 1, so a cap of five
// is five presses rather than a count down from 100 (§15.9); the plus button
// therefore cycles. A coarse step goes to the next multiple of ten, a viewer
// convenience for the long range.
func (e *unitViewerRestrict) step(key string, dir int, coarse bool) bool {
	low := e.floor(key)
	if low < 0 || dir == 0 {
		return false
	}
	v := e.value(key)
	next := v + dir
	switch {
	case dir > 0 && v == unitViewerNoLimit && coarse:
		next = 10
	case dir > 0 && v == unitViewerNoLimit:
		next = 1
	case !coarse:
	case dir < 0 && v == unitViewerNoLimit:
		next = content.RestrictionMaxCount
	case dir < 0:
		next = (v - 1) / 10 * 10
	case v >= content.RestrictionMaxCount:
		next = unitViewerNoLimit
	default:
		next = (v/10 + 1) * 10
	}
	next = max(low, min(unitViewerNoLimit, next))
	if next == v {
		return false
	}
	return e.setValue(key, next)
}

// changed counts the names whose entry differs between the draft and the
// saved set: Apply's number.
func (e *unitViewerRestrict) changed() int {
	a, b := e.draft.Entries(), e.saved.Entries()
	n, i, j := 0, 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || (i < len(a) && a[i].Unit < b[j].Unit):
			n, i = n+1, i+1
		case i == len(a) || b[j].Unit < a[i].Unit:
			n, j = n+1, j+1
		default:
			if a[i].Count != b[j].Count {
				n++
			}
			i, j = i+1, j+1
		}
	}
	return n
}

// tally counts the draft's entries the running content takes, removed (0)
// and capped (1..100), and those it leaves out (§15.3).
func (e *unitViewerRestrict) tally() (removed, capped, leftOut int) {
	for _, r := range e.draft.Entries() {
		n, ok := e.names[r.Unit]
		switch {
		case !ok || n.invalid || n.lock == content.RestrictionNoRestrict,
			r.Count == 0 && n.lock == content.RestrictionRemovesCommander:
			leftOut++
		case r.Count == 0:
			removed++
		default:
			capped++
		}
	}
	return removed, capped, leftOut
}

// restrictionTallyText is the count of a set in the words the editor's
// footer, the Nanolathe screen's card and the loading screen share
// (§8.3, §15.9): "3 removed, 2 capped", or "No restrictions".
func restrictionTallyText(removed, capped int) string {
	var parts []string
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", removed))
	}
	if capped > 0 {
		parts = append(parts, fmt.Sprintf("%d capped", capped))
	}
	if len(parts) == 0 {
		return "No restrictions"
	}
	return strings.Join(parts, ", ")
}

// footer is the library's footer: the names removed and capped, and the
// entries the running content leaves out.
func (e *unitViewerRestrict) footer() string {
	if e.names == nil {
		return ""
	}
	removed, capped, leftOut := e.tally()
	text := restrictionTallyText(removed, capped)
	if leftOut > 0 {
		if removed+capped == 0 {
			text = ""
		} else {
			text += ", "
		}
		text += fmt.Sprintf("%d left out", leftOut)
	}
	return text
}

// keep narrows filtered library entries to the names with an entry.
func (e *unitViewerRestrict) keep(filtered []unitViewerEntry) []unitViewerEntry {
	return slices.DeleteFunc(filtered, func(entry unitViewerEntry) bool {
		_, restricted := e.draft.Count(unitViewerRestrictKey(entry.Def))
		return !restricted
	})
}

// unitViewerRestrictState is how a library row shows its name's state.
type unitViewerRestrictState struct {
	text    string // "" for No limit
	lock    bool   // a padlock: the name cannot be restricted or removed
	removed bool   // the row dims
}

// rowState is a name's state for every record carrying it (§15.9): nothing
// for No limit, the count for a cap, Removed for 0, and a padlock with its
// reason for a name that cannot be restricted or removed; a commander keeps
// its padlock beside a cap.
func (e *unitViewerRestrict) rowState(key string) unitViewerRestrictState {
	n, known := e.names[key]
	count, restricted := e.draft.Count(key)
	switch {
	case !known:
		return unitViewerRestrictState{}
	case n.invalid:
		return unitViewerRestrictState{text: "Fixed", lock: true}
	case n.lock == content.RestrictionNoRestrict:
		return unitViewerRestrictState{text: "Norestrict", lock: true}
	case n.lock == content.RestrictionRemovesCommander && restricted && count > 0:
		return unitViewerRestrictState{text: fmt.Sprintf("Max %d", count), lock: true}
	case n.lock == content.RestrictionRemovesCommander:
		return unitViewerRestrictState{text: "Commander", lock: true}
	case !restricted:
		return unitViewerRestrictState{}
	case count == 0:
		return unitViewerRestrictState{text: "Removed", removed: true}
	}
	return unitViewerRestrictState{text: fmt.Sprintf("Max %d", count)}
}

// note is the selected unit's line under the block's heading: why its
// controls are limited, that its entry covers every record of a duplicated
// name, or how to step faster. warn draws it in amber with a padlock.
func (e *unitViewerRestrict) note(def *content.UnitDef) (text string, warn bool) {
	if def == nil {
		return "Select a unit to restrict it.", false
	}
	key := unitViewerRestrictKey(def)
	n, known := e.names[key]
	count, restricted := e.draft.Count(key)
	switch {
	case !known:
		return "Reading this content's units...", false
	case n.invalid:
		return "This unit's name cannot be stored.", true
	case n.lock == content.RestrictionNoRestrict && restricted:
		return "Norestrict: its entry is left out.", true
	case n.lock == content.RestrictionNoRestrict:
		return "Norestrict: it cannot be restricted.", true
	case n.lock == content.RestrictionRemovesCommander && restricted && count == 0:
		return "Commander: its 0 is left out.", true
	case n.lock == content.RestrictionRemovesCommander:
		return "Commander: capped, never removed.", true
	case n.records > 1:
		return fmt.Sprintf("Covers all %d records named %s.", n.records, strings.ToUpper(def.UnitName)), false
	}
	return "Per player. Shift steps by ten.", false
}

// stateLabel is the block heading's right-hand state for key.
func (e *unitViewerRestrict) stateLabel(key string) (string, color.RGBA) {
	switch v, low := e.value(key), e.floor(key); {
	case key == "" || e.names == nil:
		return "", nlDim
	case low < 0:
		return "Locked", nlAmber
	case v == 0:
		return "Removed", unitViewerRemovedInk
	case v == unitViewerNoLimit:
		return "No limit", nlGreenText
	}
	return "Capped", nlCream
}

// valueText is the count's readout.
func (e *unitViewerRestrict) valueText(key string) string {
	if key == "" || e.floor(key) < 0 {
		return "-"
	}
	if v := e.value(key); v != unitViewerNoLimit {
		return fmt.Sprint(v)
	}
	return "No limit"
}

var unitViewerRemovedInk = color.RGBA{226, 150, 128, 255}

// ---------------------------------------------------------------------------
// Layout. The block sits under the data column's tabs, its rows level with
// the stage's action and control rows; the toggle sits on the library's
// count line and Apply beside Back.

const unitViewerRestrictTop = 594

var (
	unitViewerRestrictWell = screenkit.Rect{X: 1002, Y: unitViewerActionRowY, W: 126, H: 36}
	// unitViewerRestrictStepper is the wheel's target: both step buttons
	// and the readout between them.
	unitViewerRestrictStepper = screenkit.Rect{X: 962, Y: unitViewerActionRowY, W: 206, H: 36}
	unitViewerRestrictOnlyR   = gui.Rect{X: 128, Y: 186, W: 138, H: 20}
)

// addRestrictControls adds the editor's native controls to the viewer
// panel, so they share its focus, keys and hit testing.
func addRestrictControls(add func(kind gui.Kind, name, text string, x, y, width, height int32) int, button func(name, text string, x, y, width int32)) {
	button("RSWITCH", "", 862, unitViewerActionRowY, 92)
	button("RDOWN", "", 962, unitViewerActionRowY, 36)
	button("RUP", "", 1132, unitViewerActionRowY, 36)
	button("RRESET", "Reset", 862, unitViewerControlRowY, 306)
	r := unitViewerRestrictOnlyR
	add(gui.KindButton, "RESTRONLY", "", r.X, r.Y, r.W, r.H)
	button("APPLY", "Apply", 902, 26, 132)
}

// refreshRestrictControls greys what the selected unit cannot take and
// shows Apply on the menu route once the draft differs.
func (s *toolsScreen) refreshRestrictControls() {
	p := s.panel
	e := &s.restrict
	grey := func(name string, off bool) {
		if i := p.Index(name); i >= 0 {
			p.Window.Gadgets[i].GrayedOut = 0
			if off {
				p.Window.Gadgets[i].GrayedOut = 1
			}
		}
	}
	key := unitViewerRestrictKey(s.selected)
	low, v := e.floor(key), e.value(key)
	grey("RSWITCH", low < 0 || (v != 0 && low > 0))
	grey("RDOWN", low < 0 || v <= low)
	grey("RUP", low < 0)
	grey("RRESET", e.draft.IsZero())
	if i := p.Index("APPLY"); i >= 0 {
		n := 0
		if e.route == unitViewerRestrictMenu {
			n = e.changed()
		}
		p.SetActiveAt(i, n > 0)
		if n > 0 {
			p.SetText("APPLY", fmt.Sprintf("Apply  %d", n))
		}
	}
}

// activateRestrict runs one of the editor's controls; false for another
// control.
func (s *toolsScreen) activateRestrict(name string) bool {
	e := &s.restrict
	key := unitViewerRestrictKey(s.selected)
	switch name {
	case "RSWITCH":
		e.toggle(key)
	case "RDOWN":
		e.step(key, -1, s.shiftHeld)
	case "RUP":
		e.step(key, 1, s.shiftHeld)
	case "RRESET":
		// Reset empties the set, every unit back to No limit. Retail's
		// Reset instead gives every row 100 [08 R-SKIR-01 §10], a board that
		// reads as caps everywhere; the editor does not copy it (§15.9).
		e.draft = content.Restrictions{}
		s.refilterRestricted()
	case "RESTRONLY":
		e.only = !e.only
		s.filter()
	case "APPLY":
		s.applyRestrictions()
	default:
		return false
	}
	return true
}

// refilterRestricted applies Restricted only again after a bulk edit. A
// single unit's edit keeps the list as it is, so the selection never jumps
// away from the unit being edited.
func (s *toolsScreen) refilterRestricted() {
	if s.restrict.only {
		s.filter()
	}
}

// wheelRestrict steps the selected unit's count by whole wheel notches.
func (s *toolsScreen) wheelRestrict(dy float64) {
	e := &s.restrict
	e.wheel += dy
	for math.Abs(e.wheel) >= 1 {
		dir := int(math.Copysign(1, e.wheel))
		e.wheel -= float64(dir)
		e.step(unitViewerRestrictKey(s.selected), dir, s.shiftHeld)
	}
}

// openRestrictionEditor opens the viewer as the Nanolathe screen's
// restriction editor over that screen: it edits draft, and Back hands the
// result to done (proposal R-P8).
func (s *toolsScreen) openRestrictionEditor(g *gameShell, draft content.Restrictions, done func(content.Restrictions)) {
	s.show(g)
	s.restrict.route, s.restrict.draft, s.restrict.saved, s.restrict.done = unitViewerRestrictCard, draft, draft, done
	s.openViewer()
}

// leaveRestrictions is the editor's half of Back: the card route hands the
// draft to the screen; the menu route discards it.
func (s *toolsScreen) leaveRestrictions() {
	e := &s.restrict
	if e.route == unitViewerRestrictCard && e.done != nil {
		done := e.done
		e.done = nil
		done(e.draft)
	}
}

// applyRestrictions is the menu route's Apply: the draft becomes the running
// content's restriction setting, written to its layer (§15.9).
func (s *toolsScreen) applyRestrictions() {
	e := &s.restrict
	g := e.host
	if e.route != unitViewerRestrictMenu || g == nil {
		return
	}
	g.selectRestrictions(e.draft)
	g.saveSettings()
	e.saved = e.draft
	if p := g.activePanel(); p != nil && g.frontend != nil && g.frontend.Mode == modeMenuMain {
		g.refreshMainMenuModStatus(p)
	}
}

// ---------------------------------------------------------------------------
// Drawing.

// drawRestrictBlock is the selected unit's restriction under the data
// column: its state, the note, and the readout between the stepper's
// buttons. The switch, buttons and toggle are native controls drawn with the
// rest of the panel.
func (s *toolsScreen) drawRestrictBlock(dst *ebiten.Image) {
	e := &s.restrict
	x, w, top := 862.0, 306.0, float64(unitViewerRestrictTop)
	screenkit.Fill(dst, s.deviceRect(screenkit.Rect{X: x, Y: top, W: w, H: 1}), color.RGBA{123, 119, 86, 100})
	s.label(dst, "Restriction", screenkit.Rect{X: x, Y: top + 7, W: 150, H: 20}, screenkit.Style{Size: 13, Tracking: 0.12, Top: nlKicker, Upper: true}, true)
	key := unitViewerRestrictKey(s.selected)
	state, ink := e.stateLabel(key)
	s.label(dst, state, screenkit.Rect{X: x + 150, Y: top + 9, W: w - 150, H: 18}, screenkit.Style{Size: 12, Tracking: 0.1, Top: ink, Upper: true, Align: 2}, true)
	note, warn := e.note(s.selected)
	nx, noteInk := x, nlDim
	if warn {
		s.drawPadlock(dst, screenkit.Rect{X: x, Y: top + 28, W: 10, H: 12}, nlAmber)
		nx, noteInk = x+16, nlAmber
	}
	s.label(dst, note, screenkit.Rect{X: nx, Y: top + 27, W: x + w - nx, H: 17}, screenkit.Style{Size: 12, Top: noteInk}, false)
	well := unitViewerRestrictWell
	screenkit.Fill(dst, s.deviceRect(well), color.RGBA{5, 10, 6, 230})
	screenkit.Outline(dst, s.deviceRect(well), max(1, s.scale), color.RGBA{75, 82, 65, 255})
	value := e.valueText(key)
	st := screenkit.Style{Size: 16, Tracking: 0.06, Top: nlCream, Upper: true, Align: 1}
	if e.floor(key) < 0 {
		st.Top = nlDim
	}
	s.label(dst, value, screenkit.Rect{X: well.X + 4, Y: well.Y + (well.H-st.Size)/2 - 1, W: well.W - 8, H: st.Size + 5}, st, true)
}

// drawRestrictSwitch is the On/Off switch, the Nanolathe screen's throw
// switch at the viewer's size: a lit green ON or a dark red OFF, the knob
// covering the side not chosen.
func (s *toolsScreen) drawRestrictSwitch(dst *ebiten.Image, index int, g gui.Gadget, r screenkit.Rect) {
	key := unitViewerRestrictKey(s.selected)
	on := s.restrict.value(key) != 0 || s.restrict.floor(key) < 0
	track := s.deviceRect(r)
	screenkit.Fill(dst, track, color.RGBA{6, 8, 6, 240})
	half := r.W / 2
	left, right := screenkit.Rect{X: r.X, Y: r.Y, W: half, H: r.H}, screenkit.Rect{X: r.X + half, Y: r.Y, W: half, H: r.H}
	knob := left
	if on {
		screenkit.VGradient(dst, s.deviceRect(right), color.RGBA{90, 240, 110, 255}, color.RGBA{16, 120, 36, 255})
	} else {
		screenkit.VGradient(dst, s.deviceRect(left), color.RGBA{96, 30, 22, 255}, color.RGBA{40, 12, 8, 255})
		knob = right
	}
	ls := screenkit.Style{Size: 12, Tracking: 0.14, Top: color.RGBA{255, 170, 150, 255}, Align: 1}
	s.label(dst, "OFF", screenkit.Rect{X: left.X, Y: r.Y + (r.H-12)/2 - 1, W: half, H: 16}, ls, true)
	ls.Top = color.RGBA{236, 255, 238, 255}
	s.label(dst, "ON", screenkit.Rect{X: right.X, Y: r.Y + (r.H-12)/2 - 1, W: half, H: 16}, ls, true)
	screenkit.VGradient(dst, s.deviceRect(knob), color.RGBA{122, 122, 114, 255}, color.RGBA{56, 56, 50, 255})
	screenkit.Bevel(dst, s.deviceRect(knob), max(1, 1.5*s.scale), color.RGBA{214, 214, 206, 255}, color.RGBA{24, 24, 22, 255}, false)
	screenkit.Outline(dst, track, max(1, s.scale), color.RGBA{0, 0, 0, 255})
	if g.GrayedOut != 0 {
		screenkit.Fill(dst, track, color.RGBA{8, 12, 8, 150})
	} else if s.panel.Hovered() == index {
		screenkit.Fill(dst, track, color.RGBA{255, 255, 255, 10})
	}
	if s.panel.Focused() == index {
		screenkit.Outline(dst, track.Inset(-2*s.scale), max(1, s.scale), nlKicker)
	}
}

// drawRestrictOnly is the Restricted only toggle on the library's count
// line: a check box and its caption.
func (s *toolsScreen) drawRestrictOnly(dst *ebiten.Image, index int, r screenkit.Rect) {
	box := screenkit.Rect{X: r.X + 2, Y: r.Y + 3, W: 13, H: 13}
	edge := color.RGBA{86, 102, 74, 255}
	if s.panel.Hovered() == index {
		edge = nlGreenText
	}
	screenkit.Fill(dst, s.deviceRect(box), color.RGBA{5, 10, 6, 230})
	if s.restrict.only {
		screenkit.Fill(dst, s.deviceRect(box.Inset(2.5)), color.RGBA{61, 200, 84, 255})
	}
	screenkit.Outline(dst, s.deviceRect(box), max(1, s.scale*0.8), edge)
	ink := nlDim
	if s.restrict.only {
		ink = nlGreenText
	}
	s.label(dst, "Restricted only", screenkit.Rect{X: box.X + box.W + 6, Y: r.Y + 2, W: r.X + r.W - box.X - box.W - 6, H: 15}, screenkit.Style{Size: 11, Top: ink}, false)
	if s.panel.Focused() == index {
		screenkit.Outline(dst, s.deviceRect(r), max(1, s.scale), nlKicker)
	}
}

// drawStepGlyph draws the stepper's minus or plus over its button.
func (s *toolsScreen) drawStepGlyph(dst *ebiten.Image, r screenkit.Rect, plus, disabled bool) {
	c := nlGreenText
	if disabled {
		c = color.RGBA{70, 84, 64, 255}
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	screenkit.Fill(dst, s.deviceRect(screenkit.Rect{X: cx - 7, Y: cy - 1.25, W: 14, H: 2.5}), c)
	if plus {
		screenkit.Fill(dst, s.deviceRect(screenkit.Rect{X: cx - 1.25, Y: cy - 7, W: 2.5, H: 14}), c)
	}
}

// drawPadlock draws a small closed padlock filling r (logical units).
func (s *toolsScreen) drawPadlock(dst *ebiten.Image, r screenkit.Rect, c color.RGBA) {
	d := s.deviceRect(r)
	body := screenkit.Rect{X: d.X, Y: d.Y + d.H*0.45, W: d.W, H: d.H * 0.55}
	screenkit.Ring(dst, d.X+d.W/2, body.Y, d.W*0.32, max(1, d.W*0.14), c)
	screenkit.Fill(dst, body, c)
	screenkit.Fill(dst, screenkit.Rect{X: d.X + d.W/2 - d.W*0.08, Y: body.Y + body.H*0.3, W: d.W * 0.16, H: body.H * 0.4}, color.RGBA{30, 24, 8, 255})
}

// drawRestrictRowState draws a library row's state right-aligned at right on
// its ID line and returns the width it took.
func (s *toolsScreen) drawRestrictRowState(dst *ebiten.Image, st unitViewerRestrictState, right, y float64) float64 {
	if st.text == "" && !st.lock {
		return 0
	}
	style := screenkit.Style{Size: 9.5, Tracking: 0.08, Upper: true}
	if st.lock {
		// A padlock and its reason share the line with the ID.
		style.Size, style.Tracking = 9, 0.02
	}
	tw := unitViewerMeasure(st.text, style, true)
	switch {
	case st.lock:
		style.Top = nlAmber
		w := tw + 12
		s.drawPadlock(dst, screenkit.Rect{X: right - w, Y: y + 1, W: 8, H: 10}, nlAmber)
		s.label(dst, st.text, screenkit.Rect{X: right - tw, Y: y + 1, W: tw + 1, H: 13}, style, true)
		return w
	case st.removed:
		style.Top = unitViewerRemovedInk
		s.label(dst, st.text, screenkit.Rect{X: right - tw, Y: y + 1, W: tw + 1, H: 13}, style, true)
		return tw
	}
	chip := screenkit.Rect{X: right - tw - 10, Y: y - 1, W: tw + 10, H: 15}
	screenkit.Fill(dst, s.deviceRect(chip), color.RGBA{24, 36, 26, 230})
	screenkit.Outline(dst, s.deviceRect(chip), max(1, s.scale*0.75), color.RGBA{120, 130, 92, 255})
	style.Top, style.Align = nlCream, 1
	s.label(dst, st.text, screenkit.Rect{X: chip.X, Y: chip.Y + 2, W: chip.W, H: 13}, style, true)
	return chip.W
}
