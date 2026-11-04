package client

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"runtime"
	"sync"
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
				fmt.Printf(prompt)
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
	pcindex = 2
	pc, _, _, ok := runtime.Caller(pcindex)
	if ok {
		fn := runtime.FuncForPC(pc)
		prompt := fmt.Sprintf("debug message: %s:%v\n", fn.Name(), args)
		if fn != nil {
			if _, exists := printed[prompt]; !exists {
				fmt.Printf(prompt)
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
	em   int
}

var faceTiers = []faceTier{
	{"small", 20},
	{"normal", 30},
	{"medium", 40},
	{"large", 50},
	{"title", 60},
}

func pickTier(targetH int) faceTier {
	if targetH <= 0 {
		return faceTiers[0]
	}
	for _, t := range faceTiers {
		if t.em >= targetH {
			return t
		}
	}
	return faceTiers[len(faceTiers)-1]
}

func (c *Client) DrawRetailFilmCJK(text string, x, baselineY, maxWidth int, controlHeight int, palColor byte, attribs uint32) (int, bool) {
	if printed == nil {
		printed = make(map[string]int)
	}
	if len(text) == 0 {
		c.PrintDebug("text is empty")
		return 0, false
	}
	tr := ""
	if tr = i18n.T(text); tr == text {
		if _, exists := printed[text]; !exists {
			c.PrintDebugInternal("translate text not found!", text)
		}
		return 0, false
	}
	if c == nil {
		fmt.Printf("drawRetailFilmCJKCylindrical returned:%s\n", "CJK not ready!")
		return 0, false
	}

	safeMaxWidth := maxWidth
	if safeMaxWidth < 0 {
		safeMaxWidth = 9999
	}

	// === 1. 解析控制符 ===
	type token struct {
		rn      rune
		size    int
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
			if next == 's' || next == 'S' {
				i++
				start := i + 1
				end := start
				for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
					end++
				}
				if end > start {
					size := 0
					for k := start; k < end; k++ {
						size = size*10 + int(runes[k]-'0')
					}
					if size >= 6 && size <= 32 {
						tokens = append(tokens, token{size: size})
					}
					i = end - 1
				}
				continue
			}
		}
		tokens = append(tokens, token{rn: rn})
	}

	// === 2. 默认字号 ===
	const paddingY = 1
	availHeight := controlHeight - paddingY*2
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
	curSize := curTier.em
	maxLineWidth := 0.0

	for _, line := range lines {
		lineTier, lineSize := curTier, curSize
		for _, tk := range line {
			if tk.size > 0 {
				lineSize = tk.size
				lineTier = pickTier(lineSize)
			}
		}

		var lineW float64
		c.PrintDebug("face", lineTier)

		for _, tk := range line {
			rn := tk.rn
			face := film.TextFaceInGame(lineTier.name)
			if face != nil {
				if g := face.Glyphs[string(rn)]; g != nil && g.Advance > 0 {
					lineW += g.Advance
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

		cursorX := float64(x)
		switch {
		case attribs&4 != 0:
			cursorX = float64(x+safeMaxWidth) - lineW - 3
		case attribs&2 != 0:
			cursorX = float64(x) + (float64(safeMaxWidth)-lineW)/2.0
		default:
			cursorX += 3
		}

		lineEndSize := lineSize
		renderSize := curSize
		renderTier := curTier

		for _, tk := range line {
			if tk.size > 0 {
				renderSize = tk.size
				renderTier = pickTier(renderSize)
				curSize = renderSize
				curTier = renderTier
				lineEndSize = renderSize
				continue
			}
			rn := tk.rn
			renderTier.name = "tiny"

			face := film.TextFaceInGame(renderTier.name)
			if face == nil {
				if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
					cursorX += float64(renderSize)
				} else {
					cursorX += float64(renderSize / 2)
				}
				continue
			}

			g := face.Glyphs[string(rn)]
			if g == nil {
				if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
					cursorX += float64(renderSize)
				} else {
					cursorX += float64(renderSize / 2)
				}
				continue
			}

			frame := g.Frame()
			//fmt.Printf("Drawing char: frame.Width=%d, frame.Height=%d\n", frame.Width, frame.Height)
			if frame == nil || frame.Width == 0 || frame.Height == 0 {
				cursorX += float64(g.W)
				continue
			}
			if renderTier.name == "tiny" {
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
			// ★ 修正 1：水平定位，使用字形的实际像素宽 W 进行对齐
			// 如果你想保留原生的宽间距但字形居中，可以改为：
			// drawX := int32(cursorX + (g.Advance - float64(g.W))/2.0 + float64(g.Left))
			drawX := int32(cursorX + float64(g.Left))

			// ★ 修正 2：垂直定位，使用 (renderSize - frame.Height)/2 进行垂直居中
			// 不再使用产生偏移的 g.Top
			drawY := int32(currentLineY + (renderSize-int(frame.Height))/2)

			sprite := drawlist.Sprite{
				Frame:   frame,
				X:       drawX,
				Y:       drawY,
				Kind:    drawlist.BlitKeyed,
				Key:     0,
				HasClip: false, // ★ 修正 3：临时关闭裁剪，确保能看清全貌。确认没问题后可改回 true
				// HasClip: true,
				// Clip: drawlist.Rect{
				// 	X: int32(x), Y: int32(baselineY - 20),
				// 	W: int32(safeMaxWidth), H: int32(controlHeight + 40),
				// },
			}
			c.List().RecordSprite(sprite)

			// ★ 修正 4：光标推进改用 g.W，彻底解决字间距过大的问题
			// 如果你觉得现在字距太紧，可以改为：cursorX += float64(g.Advance)
			cursorX += float64(g.W)
		}

		lineStep := int(float64(lineEndSize) * 1.25)
		if lineStep < lineEndSize+2 {
			lineStep = lineEndSize + 2
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
