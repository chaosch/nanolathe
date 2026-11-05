package client

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"regexp"
	"runtime"
	"sync"
	"unicode/utf8"
)

const COLOR_EDGE = 79

type HAlign uint32

const (
	AlignLeft   HAlign = 0
	AlignCenter HAlign = 2
	AlignRight  HAlign = 4
)

var printed map[string]int

func (c *Client) PrintDebug(args ...interface{}) {
	defer printlock.Unlock()
	printlock.Lock()
	if printed == nil {
		printed = make(map[string]int)
	}
	var pcindex int
	pcindex = 1
	pc, _, _, ok := runtime.Caller(pcindex)
	if ok {
		fn := runtime.FuncForPC(pc)
		prompt := fmt.Sprintf("debug message: %s:%v\n", fn.Name(), args)
		if fn != nil {
			if _, exists := printed[prompt]; !exists {
				fmt.Printf("%s", prompt)
				printed[prompt]++
			}
		}
	}
}
func (c *Client) PrintDebugInternal(args ...interface{}) {
	defer printlock.Unlock()
	printlock.Lock()
	if printed == nil {
		printed = make(map[string]int)
	}
	var pcindex int
	pcindex = 3                                // 根据你的调用层级决定，如果层级不对，改成 2 或者 4
	pc, _, line, ok := runtime.Caller(pcindex) // ★ 接住 file 和 line
	if ok {
		fn := runtime.FuncForPC(pc)
		if fn != nil {
			// 如果只想看文件名，可以用 filepath.Base(file) 取最后一部分
			// 比如：prompt := fmt.Sprintf("debug message: %s (%s:%d):%v\n", fn.Name(), filepath.Base(file), line, args)
			prompt := fmt.Sprintf("debug message: %s:%d:%v\n", fn.Name(), line, args)
			if _, exists := printed[prompt]; !exists {
				fmt.Printf("%s", prompt)
				printed[prompt]++
			}
		}
	}
}

var printlock sync.RWMutex

func PrintStackTrace() {
	defer printlock.Unlock()
	printlock.Lock()
	if printed == nil {
		printed = make(map[string]int)
	}
	pcs := make([]uintptr, 10)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := frames.Next()
		fmt.Printf("%s\n  -> %s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
	}
}

// ============================================================
// 字体档位（与 Python NEW_FACE 一致）
// ============================================================
type faceTier struct {
	name string
	px   int
}

var faceTiers = []faceTier{
	{"tiny", 12},
	{"small", 14},
	{"normal", 16},
	//{"normal", 20},
	//{"medium", 20},
	//{"large", 20},
	//{"title", 20},
}

func pickTier(targetH int) faceTier {
	if targetH <= 0 {
		return faceTiers[0]
	}
	for _, t := range faceTiers {
		if t.px >= targetH {
			return t
		}
	}
	return faceTiers[len(faceTiers)-1]
}

func (c *Client) DrawRetailFilmCJK(text string, x, baselineY, maxWidth int, controlHeight1 int, palColor byte, attribs uint32) (int, bool) {
	real_text := text
	if printed == nil {
		printed = make(map[string]int)
	}
	if len(real_text) == 0 {
		//c.PrintDebug("real_text is empty")
		return 0, false
	}

	// 翻译兜底：查不到只报错，不中断渲染（继续画英文）
	tr := i18n.T(real_text)
	if tr == real_text {
		if _, exists := printed[real_text]; !exists {
			//if !IsDigitsAndSymbols(real_text) {
			c.PrintDebugInternal("translate text not found!", real_text)
			//}
			return 0, false
		}
	}
	//if c == nil {
	//	fmt.Printf("drawRetailFilmCJKCylindrical returned:%s\n", "CJK not ready!")
	//	return 0, false
	//}

	safeMaxWidth := maxWidth
	if safeMaxWidth < 0 {
		safeMaxWidth = 9999
	}

	// === 1. 解析控制符（只保留 \n 换行） ===
	type token struct {
		rn      rune
		forceNL bool
	}

	var tokens []token
	runes := []rune(tr)
	for i := 0; i < len(runes); i++ {
		rn := runes[i]
		if rn == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if next == 'n' || next == 'N' {
				tokens = append(tokens, token{forceNL: true})
				i++
				continue
			}
		}
		tokens = append(tokens, token{rn: rn})
	}

	// === 2. 默认字号 ===
	const paddingY = 0
	availHeight := controlHeight1 - paddingY*2
	if availHeight < 1 {
		availHeight = 1
	}
	defaultTier := pickTier(availHeight)

	// === 3. 换行排版 ===
	var lines [][]token
	var currentLine []token
	for _, tk := range tokens {
		if tk.forceNL {
			lines = append(lines, currentLine)
			currentLine = nil
			continue
		}
		currentLine = append(currentLine, tk)
	}
	if len(currentLine) > 0 {
		lines = append(lines, currentLine)
	}

	// === 4. 渲染 ===
	currentLineY := baselineY + paddingY
	curTier := defaultTier
	curSize := curTier.px
	maxLineWidth := 0.0

	for _, line := range lines {
		lineTier := curTier
		lineSize := curSize

		// 量行宽（统一使用 g.W，确保与实际渲染推进一致）
		var lineW float64
		for _, tk := range line {
			rn := tk.rn
			face := film.TextFaceInGame(lineTier.name)
			if face != nil {
				if g := face.Glyphs[string(rn)]; g != nil && g.W > 0 {
					lineW += float64(g.W)
					continue
				}
			}
			if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
				lineW += float64(lineSize)
			} else {
				lineW += float64(lineSize / 2)
			}
		}
		if lineW > maxLineWidth {
			maxLineWidth = lineW
		}

		// 对齐起点计算
		cursorX := float64(x)
		switch {
		case attribs&4 != 0: // 右对齐
			cursorX = float64(x+safeMaxWidth) - lineW - 3
		case attribs&2 != 0: // 居中对齐
			cursorX = float64(x) + (float64(safeMaxWidth)-lineW)/2.0
		default: // 左对齐
			cursorX += 3
		}
		//c.PrintDebugInternal(real_text, tr, maxWidth, controlHeight1, lineTier.px)
		for _, tk := range line {
			rn := tk.rn
			face := film.TextFaceInGame(lineTier.name)
			if face == nil {
				if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
					cursorX += float64(lineSize)
				} else {
					cursorX += float64(lineSize / 2)
				}
				continue
			}

			g := face.Glyphs[string(rn)]
			if g == nil {
				if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
					cursorX += float64(lineSize)
				} else {
					cursorX += float64(lineSize / 2)
				}
				continue
			}

			frame := g.Frame()
			if frame == nil || frame.Width == 0 || frame.Height == 0 {
				cursorX += float64(g.W)
				continue
			}

			// ★ tiny 动态上色（深拷贝 Frame 防污染）
			if lineTier.name == "tiny" && palColor > 0 {
				newPixels := make([]byte, len(frame.Pixels))
				for i := 0; i < len(frame.Pixels); i++ {
					if !frame.Transparent[i] {
						newPixels[i] = palColor
					}
				}
				frame = &formats.GAFFrame{
					Width:       frame.Width,
					Height:      frame.Height,
					Pixels:      newPixels,
					Transparent: frame.Transparent,
				}
			}

			// 水平定位
			drawX := int32(cursorX + float64(g.Left))

			// 垂直定位：tiny 直接顶行顶，其他字号垂直居中
			//drawY := int32(currentLineY)
			//if lineTier.name != "tiny" {
			//	drawY = int32(currentLineY + (lineSize-int(frame.Height))/2)
			//}
			drawY := int32(baselineY + (controlHeight1-int(frame.Height))/2)

			sprite := drawlist.Sprite{
				Frame:   frame,
				X:       drawX,
				Y:       drawY,
				Kind:    drawlist.BlitKeyed,
				Key:     0,
				HasClip: false,
			}
			c.List().RecordSprite(sprite)

			// 推光标统一用 g.W
			cursorX += float64(g.W)
		}

		// 行距推进
		lineStep := int(float64(lineSize) * 1.25)
		if lineStep < lineSize+2 {
			lineStep = lineSize + 2
		}
		currentLineY += lineStep
	}

	return int(maxLineWidth + 0.5), true
}
func (c *Client) DrawRetailFilmCJKCylindrical(text string, x, baselineY, maxWidth int, controlHeight int, palColor byte, attribs uint32) (int, bool) {
	if palColor == 0 {
		palColor = 255
	}
	return c.DrawRetailFilmCJK(text, x, baselineY, maxWidth, controlHeight, palColor, attribs)
}

func isAllASCII(s string) bool {
	// 如果字节长度等于字符数量，说明全都是单字节字符
	return len(s) == utf8.RuneCountInString(s)
}

// IsDigitsAndSymbols 判断字符串是否只包含数字和符号（不含字母、中文、空格）
func IsDigitsAndSymbols(s string) bool {
	var onlyDigitsAndSymbols = regexp.MustCompile(`^[0-9[:punct:]]+$`)
	return onlyDigitsAndSymbols.MatchString(s)
}
