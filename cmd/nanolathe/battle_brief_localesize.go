package main

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"strings"
)

func briefingSplitBlinkRunsIncludeCJK(text string) string {
	if i18n.I18nLanguageNeedCJK() {
		return briefingSplitBlinkRunsForCJK(text)
	} else {
		return briefingSplitBlinkRuns(text)
	}
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
		return retailWordWrapForCJK(text, width, measureCJk)
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
