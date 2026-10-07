//go:build darwin

package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// The Metal host's +fps panel keeps the production panel's layout and spike
// graph (internal/platform/ebitenapp drawFPSOverlay) but plots this
// renderer's phases. Next and Pack prepare the following frame on the worker
// while the render thread encodes and presents, so the lanes overlap and must
// not be added. It is host-only presentation and never touches the session.
const (
	metalFPSWidth    = 350
	metalFPSHeight   = 284
	metalFPSPlotL    = 94
	metalFPSPlotR    = metalFPSWidth - 7
	metalFPSLaneTop  = 62
	metalFPSLaneH    = 34
	metalFPSHistory  = 30 * time.Second
	metalFPSLive     = 500 * time.Millisecond
	metalFPSRepaint  = 50 * time.Millisecond
	metalFPSImageKey = 0x6d6574616c667073 // host-owned atlas identity
)

type metalFPSSample struct {
	at       time.Time
	interval float64 // ms between presents; zero when this frame was not shown
	late     bool
	lanes    [5]float64 // Next, Pack, Encode, GPU, Wait
}

type metalFPS struct {
	target    float64 // cap interval, ms
	history   []metalFPSSample
	next      int
	count     int
	presented float64
	pixels    []byte
	revision  uint64
	painted   time.Time
	scratch   []float64
}

var metalFPSLanes = [...]struct {
	name  string
	color [3]byte
}{
	{"Frame", [3]byte{72, 170, 245}},
	{"Next", [3]byte{55, 185, 105}},
	{"Pack", [3]byte{235, 192, 69}},
	{"Encode", [3]byte{242, 134, 177}},
	{"GPU", [3]byte{185, 130, 230}},
	{"Wait", [3]byte{70, 207, 205}},
}

func (m *metalFPS) observe(now time.Time, frames []meshscene.FrameTiming) {
	if m.history == nil {
		m.history = make([]metalFPSSample, 8192)
	}
	for _, f := range frames {
		s := metalFPSSample{at: now, lanes: [5]float64{f.Next, f.Pack, f.Encode, f.GPU, f.Wait}}
		if f.Presented > 0 {
			if m.presented > 0 && f.Presented > m.presented {
				s.interval = (f.Presented - m.presented) * 1000
				// Late as production counts it: past the planned spacing by
				// half a refresh (the cap and the panel are both 120 Hz here).
				s.late = s.interval > m.target*1.5
			}
			m.presented = f.Presented
		}
		m.history[m.next] = s
		m.next = (m.next + 1) % len(m.history)
		m.count = min(m.count+1, len(m.history))
	}
}

// paint redraws the bitmap at most every metalFPSRepaint and returns it with
// a revision that changes only when the pixels do.
func (m *metalFPS) paint(now time.Time, font *formats.FNT) ([]byte, uint64) {
	if m.pixels != nil && now.Sub(m.painted) < metalFPSRepaint {
		return m.pixels, m.revision
	}
	m.painted = now
	m.revision++
	if m.pixels == nil {
		m.pixels = make([]byte, 4*metalFPSWidth*metalFPSHeight)
	}
	px := m.pixels
	for i := 0; i < len(px); i += 4 {
		px[i], px[i+1], px[i+2], px[i+3] = 13, 20, 28, 255
	}
	const columns = metalFPSPlotR - metalFPSPlotL
	var peaks [6][columns]float64
	var lateColumn [columns]bool
	var peak [6]float64
	late, frames := 0, 0
	live := [6][]float64{}
	for i := 0; i < m.count; i++ {
		s := m.history[(m.next-1-i+len(m.history))%len(m.history)]
		age := now.Sub(s.at)
		if age > metalFPSHistory {
			break
		}
		if age < 0 {
			continue
		}
		frames++
		column := max(0, columns-1-int(age.Nanoseconds()*columns/metalFPSHistory.Nanoseconds()))
		values := [6]float64{s.interval, s.lanes[0], s.lanes[1], s.lanes[2], s.lanes[3], s.lanes[4]}
		for lane, v := range values {
			peaks[lane][column] = max(peaks[lane][column], v)
			peak[lane] = max(peak[lane], v)
			if age <= metalFPSLive && v > 0 {
				live[lane] = append(live[lane], v)
			}
		}
		if s.late {
			late++
			lateColumn[column] = true
		}
	}
	var median [6]float64
	for lane, values := range live {
		if len(values) > 0 {
			m.scratch = append(m.scratch[:0], values...)
			slices.Sort(m.scratch)
			median[lane] = m.scratch[len(m.scratch)/2]
		}
	}
	scale := 2 * m.target
	for lane, l := range metalFPSLanes {
		top := metalFPSLaneTop + lane*metalFPSLaneH
		bottom := top + metalFPSLaneH - 5
		y := metalFPSGraphY(m.target, scale, top, bottom)
		for x := metalFPSPlotL; x < metalFPSPlotR; x++ {
			metalFPSPixel(px, x, y, [3]byte{100, 92, 55})
		}
		for c := range columns {
			v := peaks[lane][c]
			if v <= 0 {
				continue
			}
			color := l.color
			if lane == 0 && lateColumn[c] {
				color = [3]byte{245, 133, 58}
			}
			for row := metalFPSGraphY(v, scale, top, bottom); row <= bottom; row++ {
				metalFPSPixel(px, metalFPSPlotL+c, row, color)
			}
		}
		metalFPSText(px, font, fmt.Sprintf("%s %.1f", l.name, median[lane]), 7, top)
		metalFPSText(px, font, fmt.Sprintf("peak %.1f", peak[lane]), 7, top+12)
	}
	fps := "FPS --"
	if median[0] > 0 {
		fps = fmt.Sprintf("FPS %.0f", 1000/median[0])
	}
	metalFPSText(px, font, fps+" (500ms med)   cap "+fmt.Sprintf("%.1f ms", m.target), 7, 2)
	metalFPSText(px, font, fmt.Sprintf("Frame %.1f ms   peak %.1f (+%.1f)", median[0], peak[0], max(0, peak[0]-m.target)), 7, 15)
	metalFPSText(px, font, fmt.Sprintf("GPU %.1f ms   room %.1f ms   Metal, prepared ahead", median[4], m.target-median[4]), 7, 28)
	metalFPSText(px, font, fmt.Sprintf("30s: %d/%d late (missed refresh)", late, frames), 7, 41)
	metalFPSText(px, font, "-30s", metalFPSPlotL, 266)
	metalFPSText(px, font, "-15s", (metalFPSPlotL+metalFPSPlotR)/2-12, 266)
	metalFPSText(px, font, "now", metalFPSPlotR-18, 266)
	return px, m.revision
}

func metalFPSGraphY(v, scale float64, top, bottom int) int {
	if scale <= 0 || v >= scale {
		return top
	}
	return bottom - int(v*float64(bottom-top)/scale)
}

func metalFPSPixel(px []byte, x, y int, c [3]byte) {
	if x < 0 || y < 0 || x >= metalFPSWidth || y >= metalFPSHeight {
		return
	}
	i := 4 * (y*metalFPSWidth + x)
	px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], 255
}

// metalFPSText sets the font's glyph bits from a top-left origin, advancing
// by glyph width as the production FNT writer does.
func metalFPSText(px []byte, font *formats.FNT, text string, x, y int) {
	if font == nil {
		return
	}
	for i := 0; i < len(text); i++ {
		g := font.Glyphs[text[i]]
		if g == nil {
			continue
		}
		for gy := range int(g.Height) {
			for gx := range int(g.Width) {
				if g.On(gx, gy) {
					metalFPSPixel(px, x+gx, y+gy, [3]byte{235, 235, 235})
				}
			}
		}
		x += int(g.Width)
	}
}
