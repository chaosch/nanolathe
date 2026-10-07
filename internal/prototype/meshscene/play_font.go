package meshscene

import "github.com/nanolathe-gg/nanolathe/internal/film"

type playGlyph struct {
	uv                       [4]float32
	left, top, w, h, advance float32
}
type playFont struct {
	cap    float32
	glyphs [128]playGlyph
}

// ASCII glyphs are copied once from the existing OFL body face. The atlas
// owns its RGBA bytes; shared FilmTypeface coverage levels stay read-only.
func newPlayFont() (Texture, playFont) {
	face := film.FilmTypeface("body")
	const width = 2048
	type packed struct {
		r    int
		x, y int
	}
	var packedGlyphs []packed
	x, y, row := 4, 0, 0
	for r := 32; r < 127; r++ {
		g, ok := face.Glyphs[rune(r)]
		if !ok || len(g.Levels) == 0 {
			continue
		}
		b := g.Levels[0].Bounds()
		w, h := b.Dx(), b.Dy()
		if x+w+2 > width {
			x = 0
			y += row + 2
			row = 0
		}
		packedGlyphs = append(packedGlyphs, packed{r, x, y})
		x += w + 2
		row = max(row, h)
	}
	height := 1
	for height < y+row+2 {
		height *= 2
	}
	atlas := Texture{Width: width, Height: height, RGBA: make([]byte, width*height*4)}
	for yy := 0; yy < 2; yy++ {
		for xx := 0; xx < 2; xx++ {
			i := (yy*width + xx) * 4
			atlas.RGBA[i], atlas.RGBA[i+1], atlas.RGBA[i+2], atlas.RGBA[i+3] = 255, 255, 255, 255
		}
	}
	font := playFont{cap: float32(face.Cap)}
	for _, q := range packedGlyphs {
		g := face.Glyphs[rune(q.r)]
		img := g.Levels[0]
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		for yy := 0; yy < h; yy++ {
			for xx := 0; xx < w; xx++ {
				i := ((q.y+yy)*width + q.x + xx) * 4
				atlas.RGBA[i], atlas.RGBA[i+1], atlas.RGBA[i+2] = 255, 255, 255
				atlas.RGBA[i+3] = img.GrayAt(xx+b.Min.X, yy+b.Min.Y).Y
			}
		}
		font.glyphs[q.r] = playGlyph{uv: [4]float32{float32(q.x) / width, float32(q.y) / float32(height), float32(q.x+w) / width, float32(q.y+h) / float32(height)}, left: float32(g.Left - 2), top: float32(g.Top - 2), w: float32(w), h: float32(h), advance: float32(g.Advance)}
	}
	return atlas, font
}
func (p *PlayController) text(out *[]OverlayQuad, x, y, size float32, s string, color [4]float32, maxWidth float32) {
	k := size / p.font.cap
	pen := x
	for _, r := range s {
		if r < 32 || r >= 127 {
			r = '?'
		}
		g := p.font.glyphs[r]
		if maxWidth > 0 && pen+g.advance*k > x+maxWidth {
			break
		}
		if g.w > 0 && r != ' ' {
			*out = append(*out, OverlayQuad{Rect: [4]float32{pen + g.left*k, y + size + g.top*k, g.w * k, g.h * k}, UV: g.uv, Color: color})
		}
		pen += g.advance * k
	}
}
func (p *PlayController) fill(out *[]OverlayQuad, r playRect, c [4]float32) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	uv := [4]float32{0.5 / float32(p.atlas.Width), 0.5 / float32(p.atlas.Height), 1.5 / float32(p.atlas.Width), 1.5 / float32(p.atlas.Height)}
	*out = append(*out, OverlayQuad{Rect: [4]float32{r.x, r.y, r.w, r.h}, UV: uv, Color: c})
}
func (p *PlayController) outline(out *[]OverlayQuad, r playRect, t float32, c [4]float32) {
	p.fill(out, playRect{r.x, r.y, r.w, t}, c)
	p.fill(out, playRect{r.x, r.y + r.h - t, r.w, t}, c)
	p.fill(out, playRect{r.x, r.y, t, r.h}, c)
	p.fill(out, playRect{r.x + r.w - t, r.y, t, r.h}, c)
}
