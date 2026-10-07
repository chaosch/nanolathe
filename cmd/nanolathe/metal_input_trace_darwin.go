//go:build darwin

package main

import (
	"encoding/json"
	"os"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/ui"
)

// metalInputTrace is a diagnostic (NANOLATHE_METAL_INPUT_TRACE=<file>): one
// JSON line per host step that applied scroll or pinch events, with the
// camera before and after the step and the battle's camera-input gates.
type metalInputTrace struct {
	out  *os.File
	step uint64
}

type metalInputTraceRow struct {
	Step                       uint64
	Scrolls, Precise, Momentum int
	Pinches                    int
	FocusEvents                []uint32 // native focus changes in this batch, 1 gained
	Clicks                     []uint32 // native click counts of button downs
	PanX, PanY                 float64
	MouseX, MouseY             int32
	CamX, CamZ                 [2]int32
	Zoom                       [2]int32
	Focused, Talk, Modal       bool
	DragScroll, Megamap        bool
	UnitInfo, OverMinimap      bool
	OverViewport, Captured     bool
}

func (t *metalInputTrace) before(b *battleSession, cl *client.Client, events []meshscene.NativeEvent) *metalInputTraceRow {
	t.step++
	if t.out == nil || b == nil || b.cam == nil {
		return nil
	}
	row := metalInputTraceRow{Step: t.step}
	for _, e := range events {
		switch {
		case e.Kind == 1 && e.Key&4 != 0:
			row.Scrolls++
			if e.Key&1 != 0 {
				row.Precise++
			}
			if e.Key&2 != 0 {
				row.Momentum++
			}
		case e.Kind == 9:
			row.Pinches++
		case e.Kind == 8:
			row.FocusEvents = append(row.FocusEvents, e.Key)
		case e.Kind == 2:
			row.Clicks = append(row.Clicks, e.Key)
		}
	}
	if row.Scrolls == 0 && row.Pinches == 0 && len(row.FocusEvents) == 0 && len(row.Clicks) == 0 {
		return nil
	}
	mouse := cl.Input().Mouse
	row.PanX, row.PanY = mouse.PanX, mouse.PanY
	row.MouseX, row.MouseY = int32(mouse.X), int32(mouse.Y)
	row.CamX[0], row.CamZ[0], row.Zoom[0] = b.cam.X, b.cam.Z, int32(b.cam.EffectiveZoom())
	row.Focused = cl.IsFocused()
	row.Talk = b.isTalkGUIActive()
	row.Modal = b.battleState().Modal() != ui.BattleModalClosed
	row.DragScroll = b.dragScrollActive
	row.Megamap = b.megamapShown()
	row.UnitInfo = unitInfoOpen()
	row.OverMinimap = b.isOverMinimap(row.MouseX, row.MouseY)
	row.OverViewport = b.overBattleViewport(row.MouseX, row.MouseY)
	row.Captured = cl.PointerCaptured()
	return &row
}

func (t *metalInputTrace) after(row *metalInputTraceRow, b *battleSession, cl *client.Client) {
	if row == nil || b == nil || b.cam == nil {
		return
	}
	row.CamX[1], row.CamZ[1], row.Zoom[1] = b.cam.X, b.cam.Z, int32(b.cam.EffectiveZoom())
	data, _ := json.Marshal(row)
	t.out.Write(append(data, '\n'))
}
