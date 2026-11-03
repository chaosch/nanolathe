package client

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

// UITextWidthClipped records the usual FNT run with the private GUI surface
// boundary that constrains pixels after the retail width truncation.
func (c *Client) UITextWidthClipped(fnt *formats.FNT, text string, x, y, maxWidth int, color byte, clipX, clipY, clipW, clipH int) {
	if c == nil || c.fnt == nil || fnt == nil || clipW <= 0 || clipH <= 0 {
		return
	}

	// controlHeight 用 fnt.Height（行高），attribs 按调用方传；
	// UITextWidth 没有 attribs，默认 0（左对齐）。
	height := int(fnt.Height)
	if _, drawn := c.DrawRetailFilmCJK(text, x, y, maxWidth, height, color, 0); drawn {
		return
	}
	// 翻译缺失或字体未就绪时落回 GAF，保持原行为。
	c.emitGlyphs(drawlist.Glyphs{
		Font: fnt, Text: text, X: int32(x), Y: int32(y), Color: color, MaxWidth: int32(maxWidth),
		HasClip: true, Clip: drawlist.Rect{X: int32(clipX), Y: int32(clipY), W: int32(clipW), H: int32(clipH)},
	})
}
