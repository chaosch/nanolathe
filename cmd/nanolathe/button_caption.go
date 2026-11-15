package main

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/gui"
	"github.com/nanolathe-gg/nanolathe/internal/ui"
)

// Button captions use a single-line pen. The stage-count bit moves the pen;
// pointer capture and the current down word do not [03 R-FONT-01 §6].
func (g *gameShell) drawRetailButtonCaption(c *client.Client, p *ui.Panel, index int, gad gui.Gadget, r gui.Rect, text string, selected *formats.FNT, measure func(string) int, metric int) {
	//c.PrintDebug(text)
	fmt.Println("drawRetailButtonCaption", fmt.Sprintf("gad.quickkey%c", gad.QuickKey))

	fmt.Println(fmt.Sprintf("drawRetailButtonCaption【原始文本 Hex】:%x", text))

	fmt.Println("drawRetailButtonCaption【原始文本 string】: ", text)
	client.PrintDebug(text)

	x, y, build, centred := retailButtonCaptionPen(gad, r, measure(text), metric)
	color, shade := g.retailTextPen(p, index, gad)
	draw := func(run string, px int, color byte) {
		fmt.Println(fmt.Sprintf("draw【原始文本 Hex】: %x", run))

		fmt.Println("draw【原始文本 string】: ", run)
		client.PrintDebug(run)
		g.drawRetailStringSelected(c, run, px, y, int(r.W), color, shade, selected)
	}
	key := captionQuickKeyIndex(text, gad.QuickKey)
	fmt.Println(key, fmt.Sprintf("%c", key))
	if key < 0 || (!build && (!centred || gad.GrayedOut&1 != 0)) {
		draw(text, x, color)
		return
	}
	// Each run retains the same pen family: the GAF path ignores the installed
	// foreground, while its FNT fallback uses it [03 R-FONT-01 §6].
	prefix, letter, suffix := text[:key], text[key:key+1], text[key+1:]
	draw(prefix, x, color)
	// TODO(question): settle admission of a keyed later build caption;
	// retail measures the first stored caption in that branch. Retain selected
	// prefix spacing until its exceptional writer paths are known [07 R-WGT-01 §3].
	x += measure(prefix)
	keyColor := color
	if build {
		keyColor = g.guiColor(10)
	}
	draw(letter, x, keyColor)
	if centred {
		underline := byte(2)
		if gad.Stages != 0 {
			underline = 0
		}
		c.UIFillRect(x, y+metric-1, measure(letter), 1, g.guiColor(underline))
	}
	draw(suffix, x+measure(letter), color)
}

// Caption bytes and the key use the ordinary single-player ASCII case fold;
// the first terminated-string match wins [07 R-WGT-01 §3][07 R-WGT-02 §2].
func captionQuickKeyIndexASC(text string, key byte) int {
	if key == 0 {
		return -1
	}
	// 如果快捷键本身就不是 ASCII 字符，直接返回 -1
	fold := func(b byte) byte {
		if b >= 'A' && b <= 'Z' {
			return b + ('a' - 'A')
		}
		return b
	}
	for i := 0; i < len(text) && text[i] != 0; i++ {
		if text[i] >= 0x80 {
			continue
		}
		if fold(text[i]) == fold(key) {
			return i
		}
	}
	return -1
}

// Caption bytes and the key use the ordinary single-player ASCII case fold;
// the first terminated-string match wins [07 R-WGT-01 §3][07 R-WGT-02 §2].
func captionQuickKeyIndex(text string, key byte) int {
	fmt.Println("captionQuickKeyIndex text:", text, "key:", key)
	if key == 0 || key >= 0x80 {
		return -1
	}
	fold := func(b byte) byte {
		if b >= 'A' && b <= 'Z' {
			return b + ('a' - 'A')
		}
		return b
	}

	// ★ 核心神改：直接遍历 rune，让中文字符天然被跳过！
	// 这样引擎就能在 "选择e地图" 中间精准找到 'e'，且绝不会切碎汉字！
	for i, r := range text {
		if r == 0 {
			break
		}
		// 只对 ASCII 字符进行匹配
		if r < 0x80 && fold(byte(r)) == fold(key) {
			return i // 注意：这里的 i 是字节索引！Go 的 range 会完美返回真实的字节偏移量
		}
	}
	return -1
}
