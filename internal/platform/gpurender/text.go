package gpurender

import (
	"fmt"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"math"
)

// The FNT text family for the modern executor (docs/DESIGN_GPU_RENDERER.md §2.3,
// C-G4). Text is keyed, not destination-reading: a glyph's set bits write the
// run's single colour index and its clear bits leave the destination untouched,
// exactly as internal/client drawTextClipped writes `frame[index] = color` over the set
// bits [03 §7.1]. The layout — the truncate-to-width, the descender baseline, the
// offset-0 absent-glyph skip, the newline/NUL terminator, the per-glyph advance
// and first-character table bias — is reproduced from drawTextClipped/MeasureText/
// TruncateToWidth so the covered pixel set matches the byte writer [fmt fnt][07 §7].
//
// One glyph strip per *formats.FNT packs every present glyph horizontally into
// the scene atlas, the set bit flagged in green (the same opacity convention the
// GAF frame regions use); the run colour rides the vertex red channel and the
// scene shader's glyph op keys on the green flag. The strip is packed on first
// use and cached by pointer identity for the font's lifetime, so a text run
// merges into the same device draw as the sprites and fills around it (§11.2).

// fntAtlas is one font's packed glyph strip and its per-code layout. entry is the
// strip's scene atlas placement; xOffset[code] is a present glyph's left edge in
// scene atlas coordinates (-1 when the code is absent) and width[code] is its
// advance, both indexed by the character code.
type fntAtlas struct {
	entry   sceneEntry
	height  int
	xOffset [256]int32
	width   [256]int32
}

// baselineDescender returns the signed FNT baseline adjustment. It mirrors the
// software rasterizer so the modern layout matches the byte writer [03 R-FONT-01 §1].
func baselineDescender(fnt *formats.FNT) int {
	if fnt == nil {
		return 0
	}
	return int(fnt.Baseline)
}

// resolveGlyph applies the offset-0 absent-glyph rule, returning the character
// code and whether the code is present. LoadFNT has already applied the
// first-character table bias when it stored Glyphs [fmt fnt].
func resolveGlyph(fnt *formats.FNT, code int) (int, bool) {
	if fnt == nil || code < 0 || code > 255 {
		return 0, false
	}
	if fnt.Glyphs[code] == nil {
		return 0, false
	}
	return code, true
}

// measureText sums the glyph advance of one line up to the first newline or NUL,
// skipping absent glyphs, mirroring internal/client MeasureText [02 §7][03 §7.1].
func measureText(fnt *formats.FNT, text string) int {
	if fnt == nil || len(text) == 0 {
		return 0
	}
	width := 0
	for i := 0; i < len(text); i++ {
		b := text[i]
		if b == 0x0A || b == 0x00 {
			break
		}
		code, ok := resolveGlyph(fnt, int(b))
		if !ok {
			continue
		}
		width += int(fnt.Glyphs[code].Width)
	}
	return width
}

// truncateToWidth removes trailing bytes until the advance fits maxWidth,
// through the retail bounded copy, mirroring internal/client TruncateToWidth
// [07 §7][03 R-FONT-01 §3][GAP T22]. maxWidth <= 0 returns the input unchanged.
func truncateToWidth(fnt *formats.FNT, text string, maxWidth int) string {
	if maxWidth <= 0 || fnt == nil || len(text) == 0 {
		return text
	}
	// The 300-byte stack buffer is filled by a bounded copy of at most 299
	// bytes, so a longer string is cut to 299 before the width loop begins
	// [03 R-FONT-01 §3]. This mirrors internal/client.TextBufferBytes; the
	// modern executor does not import the software renderer, and text_test.go
	// cross-checks the two truncators against each other.
	const textBufferBytes = 299
	src := text
	if len(src) > textBufferBytes {
		src = src[:textBufferBytes]
	}
	if measureText(fnt, src) <= maxWidth {
		return src
	}
	for len(src) > 0 && measureText(fnt, src) > maxWidth {
		src = src[:len(src)-1]
	}
	return src
}

// buildFNTAtlas packs every present glyph of fnt into a horizontal strip in the
// scene atlas with the set-bit flag in green (255) and alpha opaque so
// premultiplied sampling recovers the flag; clear bits stay zero
// (docs/DESIGN_GPU_RENDERER.md §2.3, §11.2, C-G4). The per-code left edge — in
// scene atlas coordinates — and width are recorded for the draw walk. A font with
// no present glyph yields nil.
func (r *Renderer) buildFNTAtlas(fnt *formats.FNT) *fntAtlas {
	if fnt == nil || fnt.Height == 0 {
		return nil
	}
	h := int(fnt.Height)
	a := &fntAtlas{height: h}
	total := 0
	for i := 0; i < 256; i++ {
		g := fnt.Glyphs[i]
		if g == nil {
			a.xOffset[i] = -1
			continue
		}
		a.xOffset[i] = int32(total)
		a.width[i] = int32(g.Width)
		total += int(g.Width)
	}
	if total <= 0 {
		return nil
	}
	a.entry = r.scene.allocate(total, h)
	if !a.entry.ok {
		return nil
	}
	buf := r.scene.scratch(total * h * 4)
	for i := range buf {
		buf[i] = 0
	}
	for i := 0; i < 256; i++ {
		g := fnt.Glyphs[i]
		if g == nil {
			continue
		}
		xoff := int(a.xOffset[i])
		gw := int(g.Width)
		for gy := 0; gy < h; gy++ {
			for gx := 0; gx < gw; gx++ {
				if !g.On(gx, gy) {
					continue
				}
				p := (gy*total + xoff + gx) * 4
				buf[p+1] = 255 // green: set-bit flag
				buf[p+3] = 255 // alpha opaque so premultiplied green survives
			}
		}
	}
	r.scene.upload(a.entry, buf)
	// Rebase the per-code left edges onto the packed page, so a glyph quad
	// samples the strip in scene atlas coordinates.
	for i := 0; i < 256; i++ {
		if a.xOffset[i] >= 0 {
			a.xOffset[i] += a.entry.x
		}
	}
	return a
}

// fntAtlasFor returns the cached glyph strip for fnt, packing it on first use and
// caching it (including a nil for a font with no present glyph) by pointer
// identity (docs/DESIGN_GPU_RENDERER.md §2.3).
func (r *Renderer) fntAtlasFor(fnt *formats.FNT) *fntAtlas {
	if fnt == nil {
		return nil
	}
	if a, ok := r.scene.fonts[fnt]; ok {
		return a
	}
	a := r.buildFNTAtlas(fnt)
	if r.scene.fonts == nil {
		r.scene.fonts = make(map[*formats.FNT]*fntAtlas)
	}
	r.scene.fonts[fnt] = a
	return a
}

// glyphClipBounds returns the pixel boundary used by the text executor. A
// Glyphs record owns this boundary so deferred replay cannot borrow a later UI
// surface's clip.
func glyphClipBounds(g drawlist.Glyphs, width, height int) (x0, y0, x1, y1 int) {
	x0, y0, x1, y1 = 0, 0, width, height
	if !g.HasClip {
		return
	}
	x0, y0 = max(int(g.Clip.X), 0), max(int(g.Clip.Y), 0)
	x1, y1 = min(int(g.Clip.X+g.Clip.W), width), min(int(g.Clip.Y+g.Clip.H), height)
	return
}

// Glyphs replays one FNT text run, reproducing drawTextClipped's layout exactly: the
// truncate-to-width before clipping, the descender baseline, the offset-0 skip,
// the per-glyph advance and the newline/NUL terminator (docs/DESIGN_GPU_RENDERER.md
// §2.3)[02 §7][03 §7.1][07 §7]. Each glyph is a keyed quad from the font atlas to
// its screen rectangle, clipped to the framebuffer; the run colour rides the
// vertex red channel and the glyph shader writes it where the atlas marks a set
// bit, leaving the destination untouched elsewhere (C-G4).
func (r *Renderer) Glyphs(g drawlist.Glyphs) {
	r.GlyphsDispatch(g)
}
func (r *Renderer) CJKGlyphs(g drawlist.Glyphs) {
	if g.CJKFace == nil {
		return
	}
	face := g.CJKFace.(*film.AtlasFace)
	if face.GpuTex == nil {
		return
	}

	CJKPaddingTopRatio := 0.40
	CJKPaddingBottomRatio := 0.0
	CJKPaddingLeftRatio := 0.10
	CJKPaddingRightRatio := 0.10
	CJKLineSpacingRatio := 0.15

	if r.sched.worldOn && !r.chromeRegion {
		fx := float64(r.sched.txx(float32(g.X)))
		fy := float64(r.sched.txy(float32(g.Y)))
		g.X = int32(math.Round(fx))
		g.Y = int32(math.Round(fy))
		r.sched.worldOn = false
		defer func() { r.sched.worldOn = true }()
	}

	type token struct {
		rn      rune
		forceNL bool
	}
	var tokens []token
	var explicitSize = 0
	//var gridRows, gridCols = 0, 0
	runes := []rune(g.Text)
	i := 0
	for i < len(runes) {
		rn := runes[i]
		if rn == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if next == 'n' || next == 'N' {
				i += 2
				start := i
				for i < len(runes) && (runes[i] >= '0' && runes[i] <= '9' || runes[i] == ',') {
					i++
				}
				if i > start {
					parts := string(runes[start:i])
					var r, c int
					if _, err := fmt.Sscanf(parts, "%d,%d", &r, &c); err == nil {
						//gridRows, gridCols = r, c
					}
				}
				tokens = append(tokens, token{forceNL: true})
				continue
			}
			if next == 's' || next == 'S' {
				i += 2
				start := i
				for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
					i++
				}
				if i > start {
					fmt.Sscanf(string(runes[start:i]), "%d", &explicitSize)
				}
				continue
			}
		}
		tokens = append(tokens, token{rn: rn})
		i++
	}

	g.X += g.ScreenOffsetX
	g.Y += g.ScreenOffsetY

	surfaceW, surfaceH := r.w, r.h
	if r.chromeRegion {
		surfaceW, surfaceH = r.chromeW, r.chromeH
	}
	if g.Y < 0 || g.Y > int32(surfaceH) || g.X < 0 || g.X > int32(surfaceW) {
		return
	}

	baseH := float64(g.Font.Height)
	// 【修改点 1】：去掉硬编码的 +30，恢复正常的上下 padding 逻辑
	padTop := int(baseH * CJKPaddingTopRatio)
	padBottom := int(baseH * CJKPaddingBottomRatio)
	padLeft := int(baseH * CJKPaddingLeftRatio)
	padRight := int(baseH * CJKPaddingRightRatio)
	lineSpacing := int(baseH * CJKLineSpacingRatio)

	availH := int(baseH) - padTop - padBottom
	var tier client.FaceTier

	tier = client.PickTier(availH)

	face = film.TextFaceInGame(tier.Name)
	if face == nil || face.GpuTex == nil {
		return
	}
	fmt.Println(g.Text, g.Font.Height, tier, padTop, padBottom, padLeft, padRight, availH)

	// 获取切片的真实全高（因为我们在 Python 里已经裁剪了留白，这个高度就是实际最高点到最低点）
	var cellH int
	if ag, ok := face.Glyphs["A"]; ok {
		cellH = int(ag.H)
	} else {
		cellH = availH
	}

	clipX, clipY := 0, 0
	clipW, clipH := surfaceW, surfaceH
	if g.HasClip {
		clipX = int(g.Clip.X) + int(g.ScreenOffsetX)
		clipY = int(g.Clip.Y) + int(g.ScreenOffsetY)
		clipW = int(g.Clip.W)
		clipH = int(g.Clip.H)
	}

	var lines [][]token
	var currentLine []token
	maxW := 0
	if g.HasClip {
		maxW = clipW - padLeft - padRight
	} else if int(g.MaxWidth) > 0 {
		maxW = int(g.MaxWidth) - padLeft - padRight
	}

	currentLineW := 0
	for _, tk := range tokens {
		if tk.forceNL {
			lines = append(lines, currentLine)
			currentLine = nil
			currentLineW = 0
			continue
		}

		charW := 8
		if glyph, ok := face.Glyphs[string(tk.rn)]; ok {
			charW = int(glyph.Advance)
		}

		if maxW > 0 && currentLineW+charW > maxW {
			lines = append(lines, currentLine)
			currentLine = nil
			currentLineW = 0
		}
		currentLine = append(currentLine, tk)
		currentLineW += charW
	}
	if len(currentLine) > 0 {
		lines = append(lines, currentLine)
	}

	startX := int(g.X) + padLeft

	// 【修改点 2】：计算纯几何居中的起始 Y 坐标
	// 因为图集已经没有多余留白，我们直接让图块在 availH 里完美居中
	// 这里加了 -1 的视觉补偿，因为像素字体通常视觉重心偏下，你可以根据实际效果微调这个值（比如 -1、-2 或 0）
	drawTopY := int(g.Y) + padTop + (availH-cellH)/2 - 3
	currentDrawY := drawTopY

	col := float32(g.Color)
	lineHeight := cellH + lineSpacing

	for _, line := range lines {
		curX := startX
		for _, tk := range line {
			glyph, ok := face.Glyphs[string(tk.rn)]
			if !ok {
				curX += 8
				continue
			}

			if glyph.W > 0 {
				var imgs [4]*ebiten.Image
				imgs[0] = face.GpuTex

				// 【修改点 3】：点对点直接渲染，抛弃不稳定的 top 计算
				// 因为所有切片的高度一致，直接用算好的居中起点
				drawX := curX
				drawY := currentDrawY

				if g.HasClip {
					if drawX+int(glyph.W) < clipX ||
						drawX > clipX+clipW ||
						drawY+int(glyph.H) < clipY ||
						drawY > clipY+clipH {
						curX += int(glyph.Advance)
						continue
					}
				}

				u0 := float32(glyph.X)
				v0 := float32(glyph.Y)
				u1 := u0 + float32(glyph.W)
				v1 := v0 + float32(glyph.H)
				if r.sched.begin(schedOpaque, drawX, drawY, drawX+int(glyph.W), drawY+int(glyph.H), imgs) {
					if tier.Name == "tiny" {
						r.sched.quad(schedOpaque,
							float32(drawX), float32(drawY),
							float32(drawX+int(glyph.W)), float32(drawY+int(glyph.H)),
							u0, v0,
							u1, v1,
							[4]float32{col, 0, 0, 0}, [4]float32{0, 0, 0, sceneOpGlyph})
					} else {
						whiteTint := [4]float32{0, 0, 0, 0}
						r.sched.quad(schedOpaque,
							float32(drawX), float32(drawY),
							float32(drawX+int(glyph.W)), float32(drawY+int(glyph.H)),
							u0, v0,
							u1, v1,
							whiteTint, [4]float32{0, 0, 0, sceneOpGlyph})
					}
				}
			}
			curX += int(glyph.Advance)
		}
		// 换行：下一行直接往下挪整个图块高度
		currentDrawY += lineHeight
	}
}
func (r *Renderer) GlyphsOriginal(g drawlist.Glyphs) {
	if r == nil || r.surfaces[0] == nil || r.scene2D == nil {
		return
	}
	fnt := g.Font
	if fnt == nil || fnt.Height == 0 || len(g.Text) == 0 {
		return
	}
	if r.sched.worldOn && !r.chromeRegion {
		g.X = int32(math.Floor(float64(r.sched.txx(float32(g.X))) + .5))
		g.Y = int32(math.Floor(float64(r.sched.txy(float32(g.Y))) + .5))
		if g.HasClip {
			x0, y0, x1, y1 := r.sched.txRect(int(g.Clip.X), int(g.Clip.Y), int(g.Clip.X+g.Clip.W), int(g.Clip.Y+g.Clip.H))
			g.Clip = drawlist.Rect{X: int32(x0), Y: int32(y0), W: int32(x1 - x0), H: int32(y1 - y0)}
		}
		r.sched.worldOn = false
		defer func() { r.sched.worldOn = true }()
	}
	g.X += g.ScreenOffsetX
	g.Y += g.ScreenOffsetY
	text := g.Text
	// Truncate-to-width happens before clipping, exactly as drawTextClipped does [07 §7].
	if int(g.MaxWidth) > 0 {
		text = truncateToWidth(fnt, text, int(g.MaxWidth))
		if len(text) == 0 {
			return
		}
	}
	// Whole-string admission uses the baseline origin, not the adjusted glyph
	// top, and tests one-past text edges against inclusive clip bounds
	// [03 R-FONT-01 §3].
	surfaceW, surfaceH := r.w, r.h
	if r.chromeRegion {
		surfaceW, surfaceH = r.chromeW, r.chromeH
	}
	clipX0, clipY0, clipX1, clipY1 := glyphClipBounds(g, surfaceW, surfaceH)
	if int(g.X) < clipX0 || int(g.Y) < clipY0 || int(g.X)+measureText(fnt, text) >= clipX1 || int(g.Y)+int(fnt.Height) >= clipY1 {
		return
	}
	atlas := r.fntAtlasFor(fnt)
	if atlas == nil || !atlas.entry.ok {
		return
	}
	// Once admitted, the signed baseline may overrun the private surface.
	// Only framebuffer bounds remain as host storage protection.
	clipX0, clipY0, clipX1, clipY1 = 0, 0, surfaceW, surfaceH
	top := int(g.Y) - baselineDescender(fnt)
	curX := int(g.X)
	gh := atlas.height
	// The scheduler bounds cover the baseline-adjusted pixels.
	bx0, by0 := max(curX, clipX0), max(top, clipY0)
	bx1 := min(curX+measureText(fnt, text), clipX1)
	by1 := min(top+gh, clipY1)
	if !r.sched.begin(schedOpaque, bx0, by0, bx1, by1, r.sceneImages(atlas.entry)) {
		return
	}
	col := float32(g.Color)
	for i := 0; i < len(text); i++ {
		b := text[i]
		if b == 0x0A || b == 0x00 { // newline / NUL terminates the advance [02 §7].
			break
		}
		code, ok := resolveGlyph(fnt, int(b))
		if !ok { // offset 0 means absent glyph, skipped [02 §7].
			continue
		}
		gw := int(atlas.width[code])
		if gw > 0 {
			xoff := int(atlas.xOffset[code])
			yoff := int(atlas.entry.y)
			// The glyph is a rectangle, so drawTextClipped's per-pixel framebuffer clip is
			// the rectangular intersection of [curX,curX+gw)×[top,top+gh) with the
			// framebuffer; the source sub-rect shifts to match.
			c0, c1 := max(clipX0-curX, 0), min(gw, clipX1-curX)
			r0, r1 := max(clipY0-top, 0), min(gh, clipY1-top)
			if c0 < c1 && r0 < r1 {
				r.sched.quad(schedOpaque,
					float32(curX+c0), float32(top+r0), float32(curX+c1), float32(top+r1),
					float32(xoff+c0), float32(yoff+r0), float32(xoff+c1), float32(yoff+r1),
					[4]float32{col, 0, 0, 0}, [4]float32{0, 0, 0, sceneOpGlyph})
			}
		}
		curX += gw // advance; space advances via its glyph width [03 §7.1].
	}

}

func (r *Renderer) GlyphsDispatch(g drawlist.Glyphs) {
	if r == nil || r.surfaces[0] == nil || r.scene2D == nil {
		return
	}
	// ★ 外层分发：如果带了中文字库，走 CJK 渲染器
	if i18n.I18nLanguageNeedCJK() && i18n.HasCJK(g.Text) {
		r.CJKGlyphs(g)
		return
	} else {
		r.GlyphsOriginal(g)
		return
	}

}
