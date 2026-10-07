package metalhud

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
)

type region struct{ x, y, w, h int }
type imageKey struct {
	gaf                    *formats.GAFFrame
	pcx                    *formats.PCX
	pal                    *palette.Tables
	lit                    bool
	row                    byte
	sx, sy, sw, sh, dw, dh int
}
type fontKey struct {
	font *formats.FNT
	code byte
}
type surfaceKey struct {
	identity              uint64
	ordinal, w, h, sw, sh int
}
type surfaceEntry struct {
	region      region
	revision    uint64
	initialized bool
}

func (f *Foreground) allocate(w, h int) region {
	if f.err != nil || w <= 0 || h <= 0 {
		return region{}
	}
	// Duplicate one edge texel around each source, matching the production
	// scene atlas: fractional world sampling must never reach adjacent art.
	pw, ph := w+2, h+2
	if pw > atlasSize || ph > atlasSize {
		f.fail("art %dx%d exceeds %dx%d atlas", w, h, atlasSize, atlasSize)
		return region{}
	}
	if f.x+pw > atlasSize {
		f.x = 0
		f.y += f.shelf
		f.shelf = 0
	}
	if f.y+ph > atlasSize {
		f.fail("%dx%d atlas exhausted packing %dx%d art", atlasSize, atlasSize, w, h)
		return region{}
	}
	r := region{f.x + 1, f.y + 1, w, h}
	f.x += pw
	f.shelf = max(f.shelf, ph)
	return r
}

// write compares retained bytes so dynamic surfaces with unchanged pixels do
// not force another full atlas upload. It increments once per changed region.
func (f *Foreground) write(r region, pixel func(x, y int) [4]byte) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	changed := false
	for y := -1; y <= r.h; y++ {
		for x := -1; x <= r.w; x++ {
			p := pixel(max(0, min(x, r.w-1)), max(0, min(y, r.h-1)))
			off := ((r.y+y)*atlasSize + r.x + x) * 4
			for c := 0; c < 4; c++ {
				if f.atlas.RGBA[off+c] != p[c] {
					changed = true
					f.atlas.RGBA[off+c] = p[c]
				}
			}
		}
	}
	if changed {
		f.version++
	}
}

func (f *Foreground) image(k imageKey) region {
	if r, ok := f.images[k]; ok {
		return r
	}
	w, h := 0, 0
	if k.gaf != nil {
		w, h = int(k.gaf.Width), int(k.gaf.Height)
	} else if k.pcx != nil {
		w, h = int(k.pcx.Width), int(k.pcx.Height)
	}
	if k.dw > 0 && k.dh > 0 {
		w, h = k.dw, k.dh
	}
	r := f.allocate(w, h)
	f.write(r, func(x, y int) [4]byte {
		sx, sy := x, y
		if k.dw > 0 && k.dh > 0 {
			// Scaled GAF is span-over-span, unlike Surface's size-over-size
			// mapping [07 R-HUD-03 §11]. Bake those integer samples once.
			sx, sy = k.sx, k.sy
			if w > 1 {
				sx += x * (k.sw - 1) / (w - 1)
			}
			if h > 1 {
				sy += y * (k.sh - 1) / (h - 1)
			}
		}
		var idx byte
		if k.gaf != nil {
			g := k.gaf
			fw, fh := int(g.Width), int(g.Height)
			if sx < 0 || sy < 0 || sx >= fw || sy >= fh {
				return [4]byte{}
			}
			i := sy*fw + sx
			if i >= len(g.Pixels) || (i < len(g.Transparent) && g.Transparent[i]) {
				return [4]byte{}
			}
			idx = g.Pixels[i]
		} else if k.pcx != nil {
			i := sy*int(k.pcx.Width) + sx
			if i >= 0 && i < len(k.pcx.Pixels) {
				idx = k.pcx.Pixels[i]
			}
		}
		if k.lit {
			idx = k.pal.LightLookup(int(k.row), idx)
		}
		r, g, b, a := f.pal.RGBA(idx)
		return [4]byte{r, g, b, a}
	})
	if f.err == nil {
		f.images[k] = r
	}
	return r
}

func (f *Foreground) glyph(k fontKey) region {
	if r, ok := f.fonts[k]; ok {
		return r
	}
	g := k.font.Glyphs[k.code]
	if g == nil {
		return region{}
	}
	r := f.allocate(int(g.Width), int(k.font.Height))
	f.write(r, func(x, y int) [4]byte {
		if g.On(x, y) {
			return [4]byte{255, 255, 255, 255}
		}
		return [4]byte{}
	})
	if f.err == nil {
		f.fonts[k] = r
	}
	return r
}

func (f *Foreground) textured(r region, x, y int, has bool, clipX, clipY, clipW, clipH int, color [4]float32) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	if !has {
		clipX, clipY, clipW, clipH = 0, 0, f.cw, f.ch
	}
	x0, y0 := max(x, max(clipX, 0)), max(y, max(clipY, 0))
	x1, y1 := min(x+r.w, min(clipX+clipW, f.cw)), min(y+r.h, min(clipY+clipH, f.ch))
	const a = float32(atlasSize)
	f.emit(float32(x0), float32(y0), float32(x1), float32(y1),
		float32(r.x+x0-x)/a, float32(r.y+y0-y)/a, float32(r.x+x1-x)/a, float32(r.y+y1-y)/a, color, false)
}
