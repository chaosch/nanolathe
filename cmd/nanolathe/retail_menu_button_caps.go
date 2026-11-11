package main

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
)

// 1. 替代 UIBlitLit 的迴圈
// 替代原本帶 UIBlitLit 的 for 迴圈
// 替代原本帶 UIBlitLit 的 for 迴圈
func interceptUIBlitLit(
	c *client.Client,
	font *formats.GAFEntry,
	text string,
	x, y, maxWidth int, // <--- 加在這裡
	pal *palette.Tables,
	shade int,
) {
	if c == nil || font == nil || len(text) == 0 {
		return
	}
	baselineHeight := retailGAFBaselineHeight(font)

	// 1. 攔截中文
	if _, drawn := c.EmitGlyphsIntercept(text, x, y, maxWidth, baselineHeight, 0, 2); drawn {
		return
	}

	// 2. 英文走原廠
	remaining := maxWidth
	for i := 0; i < len(text); i++ {
		code := text[i]
		if code == 0 {
			return
		}
		frame := retailGAFGlyph(font, code)
		if frame == nil {
			continue
		}
		advance := int(frame.Width)
		if remaining >= 0 && remaining < advance {
			return
		}
		if code != 0x20 {
			c.UIBlitLit(frame, x-int(frame.XOffset), y-(int(frame.YOffset)-baselineHeight), pal, shade)
		}
		x += advance
		if remaining >= 0 {
			remaining -= advance
			if remaining < 0 {
				return
			}
		}
	}
}

// 2. 替代 UIBlitClipped 的迴圈
// 替代原本帶 UIBlitClipped 的 for 迴圈
func interceptUIBlitClipped(
	c *client.Client,
	font *formats.GAFEntry,
	text string,
	x, y, maxWidth int, // <--- 加在這裡
	clipX, clipY, clipW, clipH int,
) {
	if c == nil || font == nil || len(text) == 0 {
		return
	}
	baselineHeight := retailGAFBaselineHeight(font)

	// 1. 攔截中文
	if _, drawn := c.EmitGlyphsIntercept(text, x, y, maxWidth, baselineHeight, 0, 2); drawn {
		return
	}

	// 2. 英文走原廠
	remaining := maxWidth
	for i := 0; i < len(text); i++ {
		code := text[i]
		if code == 0 {
			return
		}
		frame := retailGAFGlyph(font, code)
		if frame == nil {
			continue
		}
		advance := int(frame.Width)
		if remaining >= 0 && remaining < advance {
			return
		}
		if code != 0x20 {
			c.UIBlitClipped(frame, x-int(frame.XOffset), y-(int(frame.YOffset)-baselineHeight), clipX, clipY, clipW, clipH)
		}
		x += advance
		if remaining >= 0 {
			remaining -= advance
			if remaining < 0 {
				return
			}
		}
	}
}

// interceptUIBlitMixed 接管包含 UIBlitLit 和 UIBlit 兩種混合情況的迴圈。
func interceptUIBlitMixed(
	c *client.Client,
	font *formats.GAFEntry,
	text string,
	x, y, maxWidth int,
	pal *palette.Tables,
	shade int,
) {
	if c == nil || font == nil || len(text) == 0 {
		return
	}

	baselineHeight := retailGAFBaselineHeight(font)

	// 1. 整串中文攔截
	if _, drawn := c.EmitGlyphsIntercept(text, x, y, maxWidth, baselineHeight, 0, 2); drawn {
		return
	}

	// 2. 英文走原廠（完全照搬你剛才發的迴圈邏輯）
	interceptUIBlitMixed(c, font, text, x, y, maxWidth, pal, shade)
	//remaining := maxWidth
	//for i := 0; i < len(text); i++ {
	//	code := text[i]
	//	if code == 0 {
	//		return
	//	}
	//	frame := retailGAFGlyph(font, code)
	//	if frame == nil {
	//		continue
	//	}
	//	advance := int(frame.Width)
	//	if remaining >= 0 && remaining < advance {
	//		return
	//	}
	//
	//	if code != 0x20 {
	//		if pal != nil {
	//			c.UIBlitLit(frame, x-int(frame.XOffset), y-(int(frame.YOffset)-baselineHeight), pal, shade)
	//		} else {
	//			c.UIBlit(frame, x-int(frame.XOffset), y-(int(frame.YOffset)-baselineHeight))
	//		}
	//	}
	//	x += advance
	//	if remaining >= 0 {
	//		remaining -= advance
	//		if remaining < 0 {
	//			return
	//		}
	//	}
	//}
}
