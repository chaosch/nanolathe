package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/gui"
	"github.com/nanolathe-gg/nanolathe/internal/platform/screenkit"
	"github.com/nanolathe-gg/nanolathe/internal/ui"
)

func unitViewerUIFixture() *toolsScreen {
	s := &toolsScreen{open: true, viewer: true, action: "Idle", weapon: 1, spinning: true,
		shell: &gameShell{frontend: ui.NewFrontend(modeMenuSingle)}}
	for i := range 40 {
		d := &content.UnitDef{Name: fmt.Sprintf("Unit %02d", i), UnitName: fmt.Sprintf("UNIT%d", i),
			Description: strings.Repeat("A long authored description wraps in the data column. ", 15)}
		s.entries = append(s.entries, unitViewerEntry{Key: d.UnitName, Def: d})
	}
	s.buildPanel()
	s.filter()
	return s
}

func TestUnitViewerSearchUsesSettingsMetricsBeforeGraphicsUpload(t *testing.T) {
	s := unitViewerUIFixture()
	if s.fonts.Display != nil || s.art != nil {
		t.Fatal("building native controls uploaded graphics before Draw")
	}
	p, index := s.panel, s.panel.Index("SEARCH")
	capacity := int(p.Window.Gadgets[index].Rect.W) - 4 - 2*unitViewerTextInset
	lengths := make([]int, 0, 2)
	for _, ch := range []rune{'W', 'i'} {
		p.SetText("SEARCH", "")
		p.FocusEditor(index)
		s.updateInput(screenkit.Input{}, []rune(strings.Repeat(string(ch), 127)), false, 0)
		text := p.TextAt(index)
		if unitViewerTextWidth(text) > capacity || unitViewerTextWidth(text+string(ch)) <= capacity {
			t.Fatalf("editor did not stop at its painted width: %q, capacity %d", text, capacity)
		}
		lengths = append(lengths, len(text))
		s.updateInput(screenkit.Input{Keys: []ebiten.Key{ebiten.KeyHome}}, nil, false, 0)
		if p.EditorCaret() != 0 {
			t.Fatal("new presentation stole native Home/caret behavior")
		}
	}
	if lengths[0] >= lengths[1] {
		t.Fatal("native editor uses a byte count instead of proportional settings text")
	}
}

func TestUnitViewerSettingsLayoutMapsClicksAtEveryScale(t *testing.T) {
	for _, size := range [][2]float64{{1440, 900}, {960, 600}, {800, 600}, {900, 1200}} {
		s := unitViewerUIFixture()
		s.layout(size[0], size[1])
		p := s.panel
		list := p.Window.Gadgets[p.Index("UNITS")]
		point := s.deviceRect(screenkit.Rect{X: float64(list.Rect.X + 10), Y: float64(list.Rect.Y+2) + 2.5*float64(list.ItemHeight)})
		s.updateInput(screenkit.Input{X: point.X, Y: point.Y, Pressed: true, Down: true}, nil, false, 0)
		s.updateInput(screenkit.Input{X: point.X, Y: point.Y, Released: true}, nil, false, 0)
		if s.selected != s.filtered[2].Def {
			t.Fatalf("%vx%v: native row hit differs from painted row", size[0], size[1])
		}
		for _, g := range p.Window.Gadgets[1:] {
			r := s.deviceRect(unitViewerRect(g.Rect))
			if r.X < 0 || r.Y < 0 || r.X+r.W > size[0]+0.001 || r.Y+r.H > size[1]+0.001 {
				t.Fatalf("%vx%v: control %s escapes the canvas: %+v", size[0], size[1], g.Name, r)
			}
		}
	}
}

func TestUnitViewerNativeThumbAndEndArrowsRetainSelection(t *testing.T) {
	s := unitViewerUIFixture()
	selected := s.selected
	p := s.panel
	for _, names := range [][2]string{{"UNITS", "UNITSCROLL"}, {"INFO", "INFOSCROLL"}} {
		list, bar := p.Index(names[0]), p.Index(names[1])
		g := p.Window.Gadgets[bar]
		if !p.ActiveAt(bar) {
			t.Fatalf("%s overflow did not enable its native scrollbar", names[0])
		}
		size, travel := p.SliderMetricsAt(bar, unitViewerTextMetric)
		x, y := float64(g.Rect.X+g.Rect.W/2), float64(g.Rect.Y+2)+float64(size)/2
		s.serviceWidgets(x, y, true, true, false, 0)
		s.serviceWidgets(x, y+float64(travel), true, false, false, 0)
		s.serviceWidgets(x, y+float64(travel), false, false, true, 0)
		if p.ListAt(list).Top() != p.ListMaxTopAt(list) || s.selected != selected {
			t.Fatalf("%s native thumb did not reach the end independently of selection", names[0])
		}
		arrow := -1
		for i, a := range p.Window.Gadgets {
			if a.Kind == gui.KindButton && a.Assoc == g.Assoc && a.Attribs&0x1000 != 0 {
				arrow = i
				break
			}
		}
		if arrow < 0 {
			t.Fatalf("%s has no associated up arrow", names[0])
		}
		r := p.Window.Gadgets[arrow].Rect
		x, y = float64(r.X+r.W/2), float64(r.Y+r.H/2)
		knob := p.SliderKnobAt(bar)
		s.serviceWidgets(x, y, true, true, false, 0)
		s.serviceWidgets(x, y, false, false, true, 0)
		if p.SliderKnobAt(bar) >= knob || s.selected != selected {
			t.Fatalf("%s arrow did not step its native bar independently of selection", names[0])
		}
	}
}

func TestUnitViewerSettingsButtonsRespectUnavailableActions(t *testing.T) {
	s := unitViewerUIFixture()
	click := func(name string) {
		r := s.panel.Window.Gadgets[s.panel.Index(name)].Rect
		x, y := float64(r.X+r.W/2), float64(r.Y+r.H/2)
		s.serviceWidgets(x, y, true, true, false, 0)
		s.serviceWidgets(x, y, false, false, true, 0)
	}
	click("MOVE")
	if s.action != "Idle" {
		t.Fatal("unavailable animation button accepted a click")
	}
	// This is an authored API fixture; no model or VM is created by input.
	s.selected.Script = &cob.Program{Scripts: map[string]int{"StartMoving": 0}}
	s.selected.BMCode = 1
	s.refreshControls()
	click("MOVE")
	if s.action != "Move" || !s.spinning {
		t.Fatal("available animation button did not change action independently of rotation")
	}
	click("PAUSE")
	if !s.animationPaused || !s.spinning {
		t.Fatal("animation pause changed the independent rotation control")
	}
}

// The last unit, scrolled fully into view, sits in the bottom painted row; a
// click there must select it. The native service hit-tests whole rows below a
// two-pixel inset, so the painted rows and the scroll limit must agree with it.
func TestUnitViewerBottomRowAtFullScrollIsClickable(t *testing.T) {
	s := unitViewerUIFixture()
	p := s.panel
	list := p.Index("UNITS")
	g := p.Window.Gadgets[list]
	rows := unitViewerListRows(g)
	if p.ListMaxTopAt(list) != len(s.filtered)-rows {
		t.Fatalf("scroll limit %d leaves a different row count than the %d hit rows", p.ListMaxTopAt(list), rows)
	}
	p.ScrollTextListAt(list, float32(len(s.filtered)))
	top := p.ListAt(list).Top()
	x, y := float64(g.Rect.X+10), float64(g.Rect.Y+2)+(float64(rows)-0.5)*float64(g.ItemHeight)
	s.updateInput(screenkit.Input{X: x, Y: y, Pressed: true, Down: true}, nil, false, 0)
	s.updateInput(screenkit.Input{X: x, Y: y, Released: true}, nil, false, 0)
	if top+rows-1 != len(s.filtered)-1 || s.selected != s.filtered[len(s.filtered)-1].Def {
		t.Fatalf("bottom row at full scroll (top %d) did not select the last unit", top)
	}
}
