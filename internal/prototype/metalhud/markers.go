package metalhud

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

type markerKey struct {
	atlas        *drawlist.MarkerAtlas
	rect         drawlist.Rect
	size         int32
	ink, outline byte
	selected     bool
}

func (f *Foreground) Markers(batch drawlist.Markers) {
	for _, m := range batch.Marks {
		if m.Size <= 0 || m.Alpha == 0 {
			continue
		}
		x, y := int(m.X-m.Size/2), int(m.Y-m.Size/2)
		c := m.Clip
		if m.IconAtlas != nil {
			k := markerKey{m.IconAtlas, m.IconRect, m.Size, m.Index, m.Outline, m.Selected}
			r := f.marker(k)
			f.textured(r, x, y, m.HasClip, int(c.X), int(c.Y), int(c.W), int(c.H), [4]float32{1, 1, 1, float32(m.Alpha) / 255})
			continue
		}
		x0, y0, x1, y1 := max(x, 0), max(y, 0), min(x+int(m.Size), f.w), min(y+int(m.Size), f.h)
		if m.HasClip {
			x0 = max(x0, int(c.X))
			y0 = max(y0, int(c.Y))
			x1 = min(x1, int(c.X+c.W))
			y1 = min(y1, int(c.Y+c.H))
		}
		if x0 >= x1 || y0 >= y1 {
			continue
		}
		ink := f.color(m.Index)
		ink[3] = float32(m.Alpha) / 255
		f.solid(x0, y0, x1, y1, ink, false)
		if m.Selected {
			ink = f.color(m.Outline)
			ink[3] = float32(m.Alpha) / 255
			// Preserve all four edge writes, including repeated corner blends,
			// exactly as the production strategic marker executor (§16.11).
			f.solid(x0, y0, x1, y0+1, ink, false)
			f.solid(x0, y1-1, x1, y1, ink, false)
			f.solid(x0, y0, x0+1, y1, ink, false)
			f.solid(x1-1, y0, x1, y1, ink, false)
		}
	}
}

func (f *Foreground) marker(k markerKey) region {
	if r, ok := f.markers[k]; ok {
		return r
	}
	a, q := k.atlas, k.rect
	if a.Width <= 0 || a.Height <= 0 || len(a.Pixels) != a.Width*a.Height*4 || q.X < 0 || q.Y < 0 || q.W <= 0 || q.H <= 0 || int64(q.X)+int64(q.W) > int64(a.Width) || int64(q.Y)+int64(q.H) > int64(a.Height) {
		f.fail("invalid strategic marker atlas or source rectangle")
		return region{}
	}
	r := f.allocate(int(k.size), int(k.size))
	ink, outline := f.color(k.ink), f.color(k.outline)
	f.write(r, func(x, y int) [4]byte {
		// The existing marker shader interpolates independent coverage masks,
		// clamped to this symbol. Bake its samples at logical pixel centers so
		// the Metal atlas still uses nearest filtering (GPU design §18.5).
		px := float32(q.X) + (float32(x)+.5)*float32(q.W)/float32(k.size) - .5
		py := float32(q.Y) + (float32(y)+.5)*float32(q.H)/float32(k.size) - .5
		bx, by := int(math.Floor(float64(px))), int(math.Floor(float64(py)))
		fx, fy := px-float32(bx), py-float32(by)
		at := func(xx, yy, channel int) float32 {
			xx = max(int(q.X), min(xx, int(q.X+q.W)-1))
			yy = max(int(q.Y), min(yy, int(q.Y+q.H)-1))
			return float32(a.Pixels[(yy*a.Width+xx)*4+channel]) / 255
		}
		var mask [4]float32
		for c := 0; c < 4; c++ {
			top := at(bx, by, c)*(1-fx) + at(bx+1, by, c)*fx
			bottom := at(bx, by+1, c)*(1-fx) + at(bx+1, by+1, c)*fx
			mask[c] = top*(1-fy) + bottom*fy
		}
		halo := float32(0)
		if k.selected {
			halo = mask[2]
		}
		alpha := mask[0] + mask[1] + halo + mask[3]
		if alpha <= 0 {
			return [4]byte{}
		}
		var p [4]byte
		for c := 0; c < 3; c++ {
			p[c] = byte(max(0, min(255, int((ink[c]*mask[0]+mask[1]+outline[c]*halo)/alpha*255+.5))))
		}
		p[3] = byte(max(0, min(255, int(alpha*255+.5))))
		return p
	})
	if f.err == nil {
		f.markers[k] = r
	}
	return r
}
