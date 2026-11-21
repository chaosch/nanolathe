package main

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"strings"
)

func (g *gameShell) installCJKBriefing(width, height int32) {
	text := g.briefing.text

	// 1. 獲取你的字體（假設你用的是 film.TextFaceInGame）
	// 這裡可以根據高度選字體
	curTier := client.PickTier(int(height) / 10) // 舉例，具體看簡報框高度
	face := film.TextFaceInGame(curTier.Name)
	if face == nil {
		return
	}

	// 2. 這裡寫一個簡單的中文斷行邏輯
	var lines []string
	var currentLine []rune
	var currentWidth int
	maxWidth := int(width) - 10 // 留點邊距

	for _, rn := range text {
		if rn == '\n' {
			lines = append(lines, string(currentLine))
			currentLine = nil
			currentWidth = 0
			continue
		}
		g, ok := face.Glyphs[string(rn)]
		w := 12 // 預設寬度
		if ok {
			w = int(g.W)
		}

		// 超寬就換行（暫時忽略標點避頭尾規則，先保證能排出來）
		if currentWidth+w > maxWidth && len(currentLine) > 0 {
			lines = append(lines, string(currentLine))
			currentLine = nil
			currentWidth = 0
		}
		currentLine = append(currentLine, rn)
		currentWidth += w
	}
	if len(currentLine) > 0 {
		lines = append(lines, string(currentLine))
	}

	// 3. 覆蓋原廠的 lines 和分頁數據
	g.briefing.lines = nil
	for _, line := range lines {
		// 這裡需要填入原廠的 briefingTextLine 結構
		g.briefing.lines = append(g.briefing.lines, briefingTextLine{
			Text: line,
			// X, Y 等欄位可以不用填，因為你的渲染函數會自己算位置
		})
	}

	// 4. 重新計算頁數（假設一頁顯示 10 行）
	const linesPerPage = 10
	g.briefing.pageCount = (len(lines) + linesPerPage - 1) / linesPerPage
	g.briefing.page = 0
}
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF) ||
			(r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE6F) ||
			(r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)) {
			w += 12
		} else {
			w += 6
		}
	}
	return w
}
func briefingSplitBlinkRunsIncludeCJK(text string) string {
	if i18n.I18nLanguageNeedCJK() {
		return briefingSplitBlinkRunsForCJK(text)
	} else {
		return briefingSplitBlinkRuns(text)
	}
}
func briefingSplitBlinkRunsForCJK1(text string) string {
	src := []byte(text)
	out := make([]byte, 0, len(src)+16)
	var blinkStack [][]byte

	fmt.Printf("[SplitBlinkCJK] START | input=%q\n", text)

	for i := 0; i < len(src); i++ {
		r := src[i]
		open := len(blinkStack) > 0
		fmt.Printf("[SplitBlinkCJK] i=%d, char=%q, open=%t, stackLen=%d\n", i, string(r), open, len(blinkStack))

		if r == '&' {
			if !open {
				if i+1 >= len(src) {
					out = append(out, r)
					continue
				}
				j := i + 1
				var n int
				switch {
				case src[j]&0xE0 == 0xC0:
					n = 2
				case src[j]&0xF0 == 0xE0:
					n = 3
				case src[j]&0xF8 == 0xF0:
					n = 4
				default:
					n = 1
				}
				if j+n > len(src) {
					out = append(out, r)
					continue
				}
				letter := make([]byte, n)
				copy(letter, src[j:j+n])
				blinkStack = append(blinkStack, letter)
				out = append(out, r)
				out = append(out, letter...)
				i += n
				fmt.Printf("[SplitBlinkCJK] >>> OPEN TAG, letter=%q, stack=%#v\n", string(letter), blinkStack)
			} else {
				blinkStack = blinkStack[:len(blinkStack)-1]
				out = append(out, r)
				fmt.Printf("[SplitBlinkCJK] >>> CLOSE TAG, stack=%#v\n", blinkStack)
			}
			continue
		}

		// 优先处理 CRLF \r\n
		if r == '\r' && i+1 < len(src) && src[i+1] == '\n' && open {
			currentLetter := blinkStack[len(blinkStack)-1]
			fmt.Printf("[SplitBlinkCJK] >>> CRLF INSIDE BLINK, split, current letter=%q\n", string(currentLetter))
			out = append(out, '&')
			out = append(out, '\r', '\n')
			out = append(out, '&')
			out = append(out, currentLetter...)
			i++ // 吃掉 \n
			continue
		}
		if r == '\n' && open {
			currentLetter := blinkStack[len(blinkStack)-1]
			fmt.Printf("[SplitBlinkCJK] >>> LF INSIDE BLINK, split, current letter=%q\n", string(currentLetter))
			out = append(out, '&')
			out = append(out, '\r', '\n')
			out = append(out, '&')
			out = append(out, currentLetter...)
			continue
		}

		out = append(out, r)
	}

	fmt.Printf("[SplitBlinkCJK] END | output=%q\n", string(out))
	return string(out)
}
func briefingSplitBlinkRunsForCJK(text string) string {
	// 将输入按 Rune（Unicode 字符）拆分，确保一个汉字作为一个整体处理
	src := []rune(text)
	out := make([]rune, 0, len(src)+16)
	var blinkStack [][]rune

	fmt.Printf("[SplitBlinkCJK] START | input=%q\n", text)

	for i := 0; i < len(src); i++ {
		r := src[i]
		open := len(blinkStack) > 0
		fmt.Printf("[SplitBlinkCJK] i=%d, char=%q, open=%t, stackLen=%d\n", i, string(r), open, len(blinkStack))

		if r == '&' {
			if !open {
				// 开启标签
				if i+1 >= len(src) {
					out = append(out, r)
					continue
				}
				// 获取 & 后面的那个字符
				letter := []rune{src[i+1]}
				blinkStack = append(blinkStack, letter)
				out = append(out, r)
				out = append(out, letter...)
				i++ // 吞掉下一个字符
				fmt.Printf("[SplitBlinkCJK] >>> OPEN TAG, letter=%q, stack=%#v\n", string(letter), blinkStack)
			} else {
				// 关闭标签
				blinkStack = blinkStack[:len(blinkStack)-1]
				out = append(out, r)
				fmt.Printf("[SplitBlinkCJK] >>> CLOSE TAG, stack=%#v\n", blinkStack)
			}
			continue
		}

		// 处理 CRLF (\r\n) 在闪烁标签内部的情况
		if r == '\r' && i+1 < len(src) && src[i+1] == '\n' && open {
			currentLetter := blinkStack[len(blinkStack)-1]
			fmt.Printf("[SplitBlinkCJK] >>> CRLF INSIDE BLINK, split, current letter=%q\n", string(currentLetter))
			out = append(out, '&')
			out = append(out, '\r', '\n')
			out = append(out, '&')
			out = append(out, currentLetter...)
			i++ // 吃掉 \n
			continue
		}

		// 处理 LF (\n) 在闪烁标签内部的情况
		if r == '\n' && open {
			currentLetter := blinkStack[len(blinkStack)-1]
			fmt.Printf("[SplitBlinkCJK] >>> LF INSIDE BLINK, split, current letter=%q\n", string(currentLetter))
			// 如果是单独的 \n，补齐为 \r\n 或保留原样
			out = append(out, '&')
			out = append(out, '\r', '\n')
			out = append(out, '&')
			out = append(out, currentLetter...)
			continue
		}

		out = append(out, r)
	}

	fmt.Printf("[SplitBlinkCJK] END | output=%q\n", string(out))
	return string(out)
}
func retailWordWrapIncludeCJK(text string, width int, measure func(string) int) string {
	if i18n.I18nLanguageNeedCJK() {
		return retailWordWrapForCJK(text, width, displayWidth)
	} else {
		return retailWordWrap(text, width, measure)
	}
}
func retailWordWrapForCJK(text string, width int, measure func(string) int) string {
	if width <= 0 {
		return text
	}

	var out strings.Builder
	currentLineWidth := 0
	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// 1. 原有换行符透传
		if r == '\n' || r == '\r' {
			out.WriteRune(r)
			currentLineWidth = 0
			continue
		}

		// 2. 颜色控制符透传
		if r == '&' && i+1 < len(runes) {
			out.WriteRune(r)
			i++
			out.WriteRune(runes[i])
			continue
		}

		// 3. 计算字符真实宽度
		charW := measure(string(r)) // 先用原版逻辑（针对英文）
		if r > 127 {
			// ★ 核心修改：如果是中文（多字节），强制用一个合适的中文字宽。
			// 如果你渲染出来的中文字号比较大，这里可以改成 14 或 16。
			// 这是保证黑框不溢出的关键。
			charW = 12
		}

		// 4. 超宽换行
		if currentLineWidth+charW > width && currentLineWidth > 0 {
			// 标点避头尾
			if strings.ContainsRune("。，！？；：、）》”", r) {
				out.WriteRune(r)
				currentLineWidth += charW
				continue
			}
			out.WriteString("\r\n")
			currentLineWidth = 0
		}

		out.WriteRune(r)
		currentLineWidth += charW
	}

	return out.String()
}
func briefingLayLineIncludeCJK(raw string, measure func(string) int, open *bool, budget int) briefingTextLine {
	if i18n.I18nLanguageNeedCJK() {
		return briefingLayLineForCJK(raw, measureCJk, open, budget)
	} else {
		return briefingLayLine(raw, measure, open, budget)
	}
}

func measureCJk(text string) int {
	face := film.TextFaceInGame("tiny")
	if len(text) == 0 {
		return 0
	}
	width := 0
	for _, r := range text {
		b := r
		if b == 0x0A || b == 0x00 { // 0x0A terminates advance; NUL also terminates [02 §7]
			break
		}
		g := face.Glyphs[string(b)]
		if g == nil { // offset 0 means absent glyph [02 §7][03 §7.1]
			continue
		}
		width += int(g.Advance)
	}
	return width
}
func briefingLayLineForCJK(raw string, measure func(string) int, open *bool, budget int) briefingTextLine {
	var out []byte
	var runs []briefingTextRun

	runeLen := func(s string, i int) int {
		if i >= len(s) {
			return 0
		}
		b := s[i]
		switch {
		case b&0x80 == 0x00:
			return 1
		case b&0xE0 == 0xC0:
			return 2
		case b&0xF0 == 0xE0:
			return 3
		case b&0xF8 == 0xF0:
			return 4
		default:
			return 1
		}
	}

	for i := 0; i < len(raw); {
		// 1. 跳过所有换行符，不参与 measure 测量
		if raw[i] == '\r' || raw[i] == '\n' {
			i++
			continue
		}

		// 2. 解析标签
		if raw[i] == '&' {
			if *open {
				letter := byte(0)
				nl := 0
				if i+1 < len(raw) {
					nl = runeLen(raw, i+1)
					if raw[i+1] < 0x80 {
						letter = raw[i+1]
					}
				}
				if len(runs) < budget {
					start := i + 1 + nl
					if start > len(raw) {
						start = len(raw)
					}
					end := start
					for end < len(raw) && raw[end] != '&' && end-start < 127 {
						end += runeLen(raw, end)
					}
					if end > len(raw) {
						end = len(raw)
					}

					// ★ 获取当前纯文本的绝对宽度作为 X 坐标
					runs = append(runs, briefingTextRun{
						Text:  raw[start:end],
						X:     measure(string(out)),
						Entry: briefingRunColorEntry(letter),
					})
				}
				*open = false
				i += 1 + nl
			} else {
				*open = true
				i++
			}
			if i >= len(raw) {
				break
			}
			continue
		}

		// 3. 只收集纯文字用于计算宽度
		n := runeLen(raw, i)
		if n == 0 {
			break
		}
		out = append(out, raw[i:i+n]...)
		i += n
	}

	return briefingTextLine{Text: string(out), Runs: runs}
}
