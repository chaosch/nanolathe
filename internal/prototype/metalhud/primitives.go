package metalhud

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

// The factors are the existing Enhanced executor's destination-table arithmetic
// [03 §4.3.4], including its factor-above-two clamp (GPU design §13.3).
func lightScale(row int) float32 { return min(float32(2), 1+float32(max(0, min(row, 31)))/30) }
func shadeScale(level int) float32 {
	if level >= 0 {
		return lightScale(level)
	}
	return min(float32(2), .06875*float32(max(-32, level)+32))
}

func (f *Foreground) Fill(c drawlist.Fill) {
	x, y, w, h := int(c.Rect.X), int(c.Rect.Y), int(c.Rect.W), int(c.Rect.H)
	col := f.color(c.Index)
	switch c.Style {
	case drawlist.FillSolid, drawlist.FillSolidInclusive:
		f.solid(x, y, x+w, y+h, col, false)
	case drawlist.FillOutline:
		f.frame(x, y, x+w-1, y+h-1, 0, 0, f.cw-1, f.ch-1, col)
	case drawlist.FillFrameInclusive:
		r := c.Clip
		f.frame(x, y, x+w-1, y+h-1, int(r.X), int(r.Y), int(r.X+r.W)-1, int(r.Y+r.H)-1, col)
	case drawlist.FillLitRect, drawlist.FillShadeRect:
		if f.pal == nil {
			return
		}
		k := lightScale(int(c.Level))
		if c.Style == drawlist.FillShadeRect {
			k = shadeScale(int(c.Level))
		}
		f.solid(x, y, x+w, y+h, [4]float32{k, k, k, 1}, true)
	default:
		f.fail("unsupported fill style %d", c.Style)
	}
}

func (f *Foreground) runX(x0, x1, y int, c [4]float32) {
	if x0 > x1 {
		return
	}
	if f.world && f.scale < 1 && x0 < x1 {
		f.solid(x0, y, x1, y+1, c, false)
		f.solid(x1, y, x1+1, y+1, c, false)
		return
	}
	f.solid(x0, y, x1+1, y+1, c, false)
}
func (f *Foreground) runY(x, y0, y1 int, c [4]float32) {
	if y0 > y1 {
		return
	}
	if f.world && f.scale < 1 && y0 < y1 {
		f.solid(x, y0, x+1, y1, c, false)
		f.solid(x, y1, x+1, y1+1, c, false)
		return
	}
	f.solid(x, y0, x+1, y1+1, c, false)
}
func (f *Foreground) frame(x0, y0, x1, y1, cx0, cy0, cx1, cy1 int, c [4]float32) {
	if x0 > x1 || y0 > y1 || cx0 > cx1 || cy0 > cy1 {
		return
	}
	l, t, r, b := max(x0, max(cx0, 0)), max(y0, max(cy0, 0)), min(x1, min(cx1, f.cw-1)), min(y1, min(cy1, f.ch-1))
	if l > r || t > b {
		return
	}
	// Clip each original edge independently; clipping a rectangle first would
	// invent an edge at the viewport boundary [R-SEL-02A].
	if y0 >= t && y0 <= b {
		f.runX(l, r, y0, c)
	}
	if y1 != y0 && y1 >= t && y1 <= b {
		f.runX(l, r, y1, c)
	}
	if x0 >= l && x0 <= r {
		f.runY(x0, t, b, c)
	}
	if x1 != x0 && x1 >= l && x1 <= r {
		f.runY(x1, t, b, c)
	}
}

func (f *Foreground) Line(l drawlist.Line) {
	if min(l.X0, l.X1) >= int32(f.cw) || min(l.Y0, l.Y1) >= int32(f.ch) || max(l.X0, l.X1) < 0 || max(l.Y0, l.Y1) < 0 {
		return
	}
	x, y, x1, y1 := l.X0, l.Y0, l.X1, l.Y1
	dx, dy := x1-x, y1-y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	sx, sy := int32(1), int32(1)
	if x > x1 {
		sx = -1
	}
	if y > y1 {
		sy = -1
	}
	err := dx - dy
	open := false
	var row, lo, hi int32
	flush := func() {
		if open {
			f.runX(int(lo), int(hi), int(row), f.color(l.Index))
			open = false
		}
	}
	// Use the same strict Bresenham comparisons as the production executor,
	// reducing only consecutive same-row pixels to one run [03 §5.4].
	for {
		if x >= 0 && y >= 0 && x < int32(f.cw) && y < int32(f.ch) {
			if open && y == row {
				lo = min(lo, x)
				hi = max(hi, x)
			} else {
				flush()
				row, lo, hi, open = y, x, x, true
			}
		}
		if x == x1 && y == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
	flush()
}

func (f *Foreground) litPoint(x, y int, row byte) {
	if f.pal == nil || x < 0 || y < 0 || x >= f.cw || y >= f.ch {
		return
	}
	if f.world {
		// Nearest-selected lit points touch each destination only once, rather
		// than repeatedly brightening collapsed record pixels (§16.3).
		sx := int(math.Ceil(float64(x)*float64(f.scale) + float64(f.ox) - .5))
		sy := int(math.Ceil(float64(y)*float64(f.scale) + float64(f.oy) - .5))
		if x != int(math.Floor((float64(sx)+.5-float64(f.ox))/float64(f.scale))) || y != int(math.Floor((float64(sy)+.5-float64(f.oy))/float64(f.scale))) {
			return
		}
		world, cw, ch := f.world, f.cw, f.ch
		f.world = false
		f.cw = f.w
		f.ch = f.h
		defer func() { f.world = world; f.cw = cw; f.ch = ch }()
		x, y = sx, sy
	}
	k := lightScale(int(row))
	f.solid(x, y, x+1, y+1, [4]float32{k, k, k, 1}, true)
}
func (f *Foreground) Points(p drawlist.Points) {
	for _, pt := range p.Points {
		x, y := int(pt.X), int(pt.Y)
		if p.Kind == drawlist.PointLit {
			f.litPoint(x, y, pt.Index)
		} else if p.Kind == drawlist.PointPlain {
			f.solid(x, y, x+1, y+1, f.color(pt.Index), false)
		} else {
			f.fail("unsupported point kind %d", p.Kind)
			return
		}
	}
}
func (f *Foreground) Flash(c drawlist.Flash) {
	c.Expand(func(x, y int32, row byte) { f.litPoint(int(x), int(y), row) })
}
func (f *Foreground) Halo(c drawlist.Halo) {
	c.Expand(func(x, y int32, row byte) { f.litPoint(int(x), int(y), row) })
}

// These world-only families cannot be silently dropped if a caller sends a
// full scene instead of the intended production foreground recording.
func (f *Foreground) Trails(drawlist.Trails) { f.fail("unexpected trail command in foreground list") }
func (f *Foreground) Lens(drawlist.Lens)     { f.fail("unsupported lens command in foreground list") }
func (f *Foreground) SurfaceWakes(drawlist.SurfaceWakes) {
	f.fail("unexpected wake command in foreground list")
}
func (f *Foreground) ScorchMarks(drawlist.ScorchMarks) {
	f.fail("unexpected scorch command in foreground list")
}
