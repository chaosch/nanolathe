package main

import (
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
func briefingSplitBlinkRunsForCJK(text string) string {
	src := []byte(text)
	out := make([]byte, 0, len(src)+16)
	open := false
	var letter []byte

	for i := 0; i < len(src) && src[i] != 0xFF; i++ {
		// 1. 记录标签字母（如 R）
		if src[i] == '&' {
			if i+1 < len(src) {
				j := i + 1
				n := 1
				if src[j]&0xE0 == 0xC0 {
					n = 2
				} else if src[j]&0xF0 == 0xE0 {
					n = 3
				} else if src[j]&0xF8 == 0xF0 {
					n = 4
				}
				if j+n <= len(src) {
					letter = append(letter[:0], src[j:j+n]...)
				} else {
					letter = append(letter[:0], src[j:]...)
				}
			}
			open = !open
			out = append(out, src[i])
			continue
		}

		// 2. 遇到换行符且在标签内部
		if src[i] == '\n' && open {
			// ★ 绝不覆盖字节，直接安全替换
			if len(out) > 0 && out[len(out)-1] == '\r' {
				out = out[:len(out)-1] // 砍掉 \r
			}
			// 补全闭合、换行、重启标签
			out = append(out, '&', '\r', '\n', '&')
			out = append(out, letter...)
			continue
		}

		out = append(out, src[i])
	}
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

		// 1. 已有的换行符直接透传
		if r == '\n' || r == '\r' {
			out.WriteRune(r)
			currentLineWidth = 0
			continue
		}

		// 2. 原样输出控制符 &R，且不占排版宽度
		if r == '&' && i+1 < len(runes) {
			out.WriteRune(r)
			i++
			out.WriteRune(runes[i])
			continue
		}

		// 3. 普通字符宽度计算
		charW := 6
		if r > 127 {
			charW = 12
		}

		// 4. 超宽则换行（带避头标点保护）
		if currentLineWidth+charW > width && currentLineWidth > 0 {
			// 标点不放在行首
			if !strings.ContainsRune("，。！？；：、）》”", r) {
				out.WriteString("\r\n")
				currentLineWidth = 0
			}
		}

		out.WriteRune(r)
		currentLineWidth += charW
	}

	return out.String()
}
func briefingLayLineIncludeCJK(raw string, measure func(string) int, open *bool, budget int) briefingTextLine {
	if i18n.I18nLanguageNeedCJK() {
		return briefingLayLineForCJK(raw, measure, open, budget)
	} else {
		return briefingLayLine(raw, measure, open, budget)
	}
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
