package metalhud

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

func (f *Foreground) Sprite(s drawlist.Sprite) {
	white := [4]float32{1, 1, 1, 1}
	x, y := int(s.X), int(s.Y)
	clip := s.Clip
	if s.PCX != nil {
		f.textured(f.image(imageKey{pcx: s.PCX}), x, y, s.HasClip, int(clip.X), int(clip.Y), int(clip.W), int(clip.H), white)
		return
	}
	g := s.Frame
	if g == nil {
		return
	}
	k := imageKey{gaf: g}
	switch s.Kind {
	case drawlist.BlitKeyed:
		if s.Anchored {
			x -= int(g.XOffset)
			y -= int(g.YOffset)
		}
	case drawlist.BlitTinted:
		if f.pal == nil {
			return
		}
		x -= int(g.XOffset)
		y -= int(g.YOffset)
		white[3] = .5
	case drawlist.BlitFeatureNormal, drawlist.BlitFeatureShadow:
		// These commands already carry top-left placement and ignore private
		// clips, exactly as the production feature sprite executor does.
		s.HasClip = false
		if s.Trans {
			if f.pal == nil {
				return
			}
			white[3] = .5
		}
	case drawlist.BlitScaled:
		if s.Src.W <= 0 || s.Src.H <= 0 || s.Dst.W <= 0 || s.Dst.H <= 0 {
			return
		}
		k.sx = int(s.Src.X)
		k.sy = int(s.Src.Y)
		k.sw = int(s.Src.W)
		k.sh = int(s.Src.H)
		k.dw = int(s.Dst.W)
		k.dh = int(s.Dst.H)
		x, y = int(s.Dst.X), int(s.Dst.Y)
	case drawlist.BlitLit:
		if s.Pal == nil {
			return
		}
		if g.Compressed == 0 {
			// Raw font sources choose destination LHT rows; the mode byte is
			// their key, and unsafe source rows are suppressed [03 R-FONT-01 §6].
			for yy := 0; yy < int(g.Height); yy++ {
				for xx := 0; xx < int(g.Width); xx++ {
					i := yy*int(g.Width) + xx
					if i >= len(g.Pixels) || g.Pixels[i] == s.LightRow || g.Pixels[i] >= 32 {
						continue
					}
					px, py := x+xx, y+yy
					if s.HasClip && !clip.Contains(int32(px), int32(py)) {
						continue
					}
					f.litPoint(px, py, g.Pixels[i])
				}
			}
			return
		}
		k.lit = true
		k.pal = s.Pal
		k.row = s.LightRow
	default:
		f.fail("unsupported sprite kind %d", s.Kind)
		return
	}
	f.textured(f.image(k), x, y, s.HasClip, int(clip.X), int(clip.Y), int(clip.W), int(clip.H), white)
}

func (f *Foreground) Cursor(c drawlist.Cursor) {
	if c.Frame == nil {
		return
	}
	r, x, y := f.image(imageKey{gaf: c.Frame}), int(c.HotX), int(c.HotY)
	start := len(f.quads)
	f.textured(r, x, y, false, 0, 0, 0, 0, [4]float32{1, 1, 1, 1})
	// A host may move an unpinned cursor to a later pointer sample after this
	// list was recorded. Only a whole quad can move: a clipped one would carry
	// its clipped edge to the new position.
	if !c.Pinned && len(f.quads) == start+1 && x >= 0 && y >= 0 && x+r.w <= f.cw && y+r.h <= f.ch {
		f.cursorQuads = append(f.cursorQuads, start)
	}
}

// CursorQuads returns the indices of the last Prepare's movable cursor quads.
func (f *Foreground) CursorQuads() []int { return f.cursorQuads }

func (f *Foreground) Surface(s drawlist.Surface) {
	w, h, sw, sh := int(s.Dst.W), int(s.Dst.H), int(s.SrcW), int(s.SrcH)
	if w <= 0 || h <= 0 || sw <= 0 || sh <= 0 || len(s.Pixels) == 0 {
		return
	}
	// Distinct occurrences retain distinct regions: a later write to the same
	// identity must not replace texels used by an earlier quad in this list.
	n := f.surfaceUses[s.Identity]
	f.surfaceUses[s.Identity] = n + 1
	k := surfaceKey{s.Identity, n, w, h, sw, sh}
	e := f.surfaces[k]
	if e == nil {
		e = &surfaceEntry{region: f.allocate(w, h)}
		if f.err != nil {
			return
		}
		f.surfaces[k] = e
	}
	if s.Identity == 0 || s.Revision == 0 || !e.initialized || e.revision != s.Revision {
		f.write(e.region, func(x, y int) [4]byte {
			// Size-over-size nearest mapping, derived before destination clipping
			// [03 §3.6]. Byte zero remains an opaque palette entry.
			i := (y*sh/h)*sw + x*sw/w
			if i >= len(s.Pixels) {
				return [4]byte{}
			}
			r, g, b, a := f.pal.RGBA(s.Pixels[i])
			return [4]byte{r, g, b, a}
		})
		e.revision = s.Revision
		e.initialized = true
	}
	c := s.Clip
	f.textured(e.region, int(s.Dst.X), int(s.Dst.Y), s.HasClip, int(c.X), int(c.Y), int(c.W), int(c.H), [4]float32{1, 1, 1, 1})
}

func (f *Foreground) Glyphs(g drawlist.Glyphs) {
	font := g.Font
	if font == nil || font.Height == 0 || len(g.Text) == 0 {
		return
	}
	// Only the anchor follows world zoom. Glyph metrics, baseline and screen
	// offsets retain native size (GPU design §16.3).
	if f.world {
		g.X, g.Y = f.project(g.X, g.Y)
		if g.HasClip {
			c := g.Clip
			x0 := int32(math.Floor(float64(c.X)*float64(f.scale) + float64(f.ox)))
			y0 := int32(math.Floor(float64(c.Y)*float64(f.scale) + float64(f.oy)))
			x1 := int32(math.Ceil(float64(c.X+c.W)*float64(f.scale) + float64(f.ox)))
			y1 := int32(math.Ceil(float64(c.Y+c.H)*float64(f.scale) + float64(f.oy)))
			g.Clip = drawlist.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
		}
	}
	wasWorld, cw, ch := f.world, f.cw, f.ch
	f.world = false
	f.cw = f.w
	f.ch = f.h
	defer func() { f.world = wasWorld; f.cw = cw; f.ch = ch }()
	g.X += g.ScreenOffsetX
	g.Y += g.ScreenOffsetY
	text := client.TruncateToWidth(font, g.Text, int(g.MaxWidth))
	x, y := int(g.X), int(g.Y)
	x0, y0, x1, y1 := 0, 0, f.w, f.h
	if g.HasClip {
		x0 = max(int(g.Clip.X), 0)
		y0 = max(int(g.Clip.Y), 0)
		x1 = min(int(g.Clip.X+g.Clip.W), f.w)
		y1 = min(int(g.Clip.Y+g.Clip.H), f.h)
	}
	// Admission is a whole-string baseline test with strict one-past edges.
	// After admission only framebuffer safety clips the descender [03 R-FONT-01 §3].
	if x < x0 || y < y0 || x+client.MeasureText(font, text) >= x1 || y+int(font.Height) >= y1 {
		return
	}
	y -= int(font.Baseline)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == 0 || c == '\n' {
			break
		}
		glyph := font.Glyphs[c]
		if glyph == nil {
			continue
		}
		f.textured(f.glyph(fontKey{font, c}), x, y, false, 0, 0, 0, 0, f.color(g.Color))
		x += int(glyph.Width)
	}
}
