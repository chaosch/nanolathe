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

// 描边颜色统一变量，方便修改
const COLOR_EDGE = 79 // 描边黑/暗色（调色板索引）
type HAlign uint32

var cjkCache map[glyphCacheKey]*cachedGlyph

type cachedGlyph struct {
	frame *formats.GAFFrame
}

const (
	AlignLeft   HAlign = 0 // 000
	AlignCenter HAlign = 2 // 010  <-- 必须强制等于 2
	AlignRight  HAlign = 4 // 100  <-- 必须强制等于 4
)

type glyphCacheKey struct {
	rn   rune
	size int // 这里存入的是包含留白和描边的最终像素宽度 finalW
	face string
}

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

var printlock sync.RWMutex // ★ 新增全局读写锁

func PrintStackTrace() {
	// 获取调用栈，1 是跳过 printStackTrace 自己，2 是跳过直接调用者，取更上层
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
func (c *Client) DrawRetailFilmCJK(text string, x, baselineY, maxWidth int, controlHeight int, palColor byte, attribs uint32) (int, bool) {
	if cjkCache == nil {
		cjkCache = make(map[glyphCacheKey]*cachedGlyph)
	}
	if printed == nil {
		printed = make(map[string]int)
	}
	if len(text) == 0 {
		c.PrintDebug("text is empty")
		return 0, false
	}
	if i18n.Language() != "chinese" {
		c.PrintDebug("not chinese interface!")
		return 0, false
	}
	tr := ""
	if tr = i18n.T(text); tr == text {
		if _, exists := printed[text]; !exists {
			c.PrintDebugInternal("translate text not found!", text)
		}
		return 0, false
	}
	if c == nil || !film.CJKReady() {
		fmt.Printf("drawRetailFilmCJKCylindrical returned:%s\n", "CJK not ready!")
		return 0, false
	}
	// === 0. 解析字体前缀 ===
	faceName := "display"
	if len(tr) >= 2 && tr[0] == '\\' {
		if tr[1] == 'd' || tr[1] == 'D' {
			faceName = "display"
			tr = tr[2:]
		} else if tr[1] == 'b' || tr[1] == 'B' {
			faceName = "body"
			tr = tr[2:]
		}
	}
	safeMaxWidth := maxWidth
	if safeMaxWidth < 0 {
		safeMaxWidth = 9999
	}
	face := film.TextFace(faceName)
	if face == nil || face.Cap <= 0 {
		c.PrintDebug("input error!")
		return 0, false
	}

	// 调色板范围设定 (64是亮黄，72是暗黄，79是描边)
	const (
		COLOR_BASE_BRIGHT = 64
		COLOR_BASE_DARK   = 72
		COLOR_EDGE        = 79
		LINE_STEP_PERCENT = 1.25 // ★ 新增：行距常量 (字高的 125%)
	)

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

	// === 2. 计算全局默认字号 ===
	const paddingY = 1
	availHeight := controlHeight - paddingY*2
	defaultSize := int(float64(availHeight) / LINE_STEP_PERCENT)
	if defaultSize < 10 {
		defaultSize = 10
	}

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
	// 起始 Y 坐标
	currentLineY := baselineY + paddingY
	curSize := defaultSize

	maxLineWidth := 0.0

	for _, line := range lines {
		var lineW float64
		// 为了正确计算居中，先遍历一次获取本行最终的 curSize（如果行内有 \s 控制符）
		lineSize := curSize
		for _, tk := range line {
			if tk.size > 0 {
				lineSize = tk.size
			}
			sz := lineSize
			if tk.rn < 128 {
				lineW += float64(sz/2 + 1)
			} else {
				lineW += float64(sz + 1)
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

		// 记录本行最后使用的字号，用于决定下一行的行距
		lineEndSize := lineSize
		renderSize := curSize

		for _, tk := range line {
			if tk.size > 0 {
				renderSize = tk.size
				curSize = tk.size // 更新全局当前字号，供后续行初始化使用
				lineEndSize = tk.size
				continue
			}
			rn := tk.rn
			size := renderSize

			glyph, _ := face.Glyph(rn)
			if glyph == nil || len(glyph.Levels) == 0 {
				if rn < 128 {
					cursorX += float64(size/2 + 1)
				} else {
					cursorX += float64(size + 1)
				}
				continue
			}
			srcMask := glyph.Levels[0]
			b := srcMask.Bounds()
			if b.Empty() {
				if rn < 128 {
					cursorX += float64(size/2 + 1)
				} else {
					cursorX += float64(size + 1)
				}
				continue
			}

			isASCII := rn < 128
			cacheKey := glyphCacheKey{rn: rn, size: size, face: faceName}
			cached, exists := cjkCache[cacheKey]

			if !exists {
				origW := float64(b.Dx())
				origH := float64(b.Dy())

				scale := float64(size) / 110.0
				w := int(origW * scale)
				h := int(origH * scale)

				if isASCII {
					w = size / 2
					if h > size {
						h = size
					}
				}

				if !isASCII {
					if w < 1 {
						w = 1
					}
					if h < 1 {
						h = 1
					}

					paddedW := w + 2
					paddedH := h + 2

					pixels := make([]byte, paddedW*paddedH)
					transparent := make([]bool, paddedW*paddedH)
					for j := range transparent {
						transparent[j] = true
					}

					shape := make([]byte, paddedW*paddedH)
					for ty := 0; ty < h; ty++ {
						for tx := 0; tx < w; tx++ {
							x0 := b.Min.X + int(float64(tx)*origW/float64(w))
							y0 := b.Min.Y + int(float64(ty)*origH/float64(h))
							if x0 >= b.Max.X {
								x0 = b.Max.X - 1
							}
							if y0 >= b.Max.Y {
								y0 = b.Max.Y - 1
							}
							shape[(ty+1)*paddedW+(tx+1)] = srcMask.GrayAt(x0, y0).Y
						}
					}

					for ty := 0; ty < paddedH; ty++ {
						for tx := 0; tx < paddedW; tx++ {
							idx := ty*paddedW + tx
							grayVal := shape[idx]

							if grayVal > 0 {
								transparent[idx] = false
								ratio := float64(grayVal) / 255.0
								colorRange := float64(COLOR_BASE_DARK - COLOR_BASE_BRIGHT)
								palIdx := float64(COLOR_BASE_BRIGHT) + (1.0-ratio)*colorRange
								pixels[idx] = byte(palIdx)
							} else {
								isOutline := false
								for oy := -1; oy <= 1; oy++ {
									for ox := -1; ox <= 1; ox++ {
										nx, ny := tx+ox, ty+oy
										if nx >= 0 && nx < paddedW && ny >= 0 && ny < paddedH {
											if shape[ny*paddedW+nx] > 0 {
												isOutline = true
											}
										}
									}
								}
								if isOutline {
									pixels[idx] = COLOR_EDGE
									transparent[idx] = false
								}
							}
						}
					}

					cached = &cachedGlyph{
						frame: &formats.GAFFrame{
							Width: uint16(paddedW), Height: uint16(paddedH),
							Pixels: pixels, Transparent: transparent,
						},
					}
				} else {
					// ASCII：优先用 developerFont，然后 GAF，最后 messageFNT
					var frame *formats.GAFFrame

					//// 1. 首选 developerFont（完整、规范）
					//if c.developerFont != nil && int(rn) >= int(c.developerFont.FirstCode) {
					//	idx := int(rn) - int(c.developerFont.FirstCode)
					//	if idx >= 0 && idx < len(c.developerFont.Glyphs) {
					//		if g := c.developerFont.Glyphs[idx]; g != nil && g.Width > 0 && g.Height > 0 {
					//			fw, fh := int(g.Width), int(g.Height)
					//			pixels := make([]byte, fw*fh)
					//			transparent := make([]bool, fw*fh)
					//			ink := c.GUIColor(15) // 白色，或者 250
					//			stride := (fw + 7) / 8
					//			for y := 0; y < fh; y++ {
					//				for x := 0; x < fw; x++ {
					//					i := y*fw + x
					//					bi := y*stride + x/8
					//					if bi < len(g.Bits) && g.Bits[bi]&(1<<(7-uint(x%8))) != 0 {
					//						pixels[i] = ink
					//					} else {
					//						transparent[i] = true
					//					}
					//				}
					//			}
					//			frame = &formats.GAFFrame{
					//				Width: uint16(fw), Height: uint16(fh),
					//				Pixels: pixels, Transparent: transparent,
					//			}
					//		}
					//	}
					//}

					// 2. 其次 GAF
					if frame == nil && c.messageGAF != nil && len(c.messageGAF.Frames) > 0 && int(rn) < len(c.messageGAF.Frames) {
						if f := c.messageGAF.Frames[rn].Frame; f != nil {
							frame = f
						}
					}

					// 3. 再其次 messageFNT
					if frame == nil && c.messageFNT != nil {
						idx := int(rn) - int(c.messageFNT.FirstCode)
						if idx >= 0 && idx < len(c.messageFNT.Glyphs) {
							if g := c.messageFNT.Glyphs[idx]; g != nil && g.Width > 0 && g.Height > 0 {
								fw, fh := int(g.Width), int(g.Height)
								pixels := make([]byte, fw*fh)
								transparent := make([]bool, fw*fh)
								ink := c.GUIColor(15)
								stride := (fw + 7) / 8
								for y := 0; y < fh; y++ {
									for x := 0; x < fw; x++ {
										i := y*fw + x
										bi := y*stride + x/8
										if bi < len(g.Bits) && g.Bits[bi]&(1<<(7-uint(x%8))) != 0 {
											pixels[i] = ink
										} else {
											transparent[i] = true
										}
									}
								}
								frame = &formats.GAFFrame{
									Width: uint16(fw), Height: uint16(fh),
									Pixels: pixels, Transparent: transparent,
								}
							}
						}
					}

					if frame != nil {
						cached = &cachedGlyph{frame: frame}
					}
				}
				if cached.frame != nil {
					cjkCache[cacheKey] = cached
				}
			}

			if cached == nil || cached.frame == nil {
				if rn < 128 {
					cursorX += float64(size/2 + 1)

				} else {
					cursorX += float64(size + 1)
				}
				continue
			}

			startY := currentLineY + (size-int(cached.frame.Height))/2
			if !isASCII {
				startY += 1
			} else {
				startY += 2
			}

			sprite := drawlist.Sprite{
				Frame: cached.frame, X: int32(cursorX - 1), Y: int32(startY - 1),
				Kind: drawlist.BlitKeyed, Key: 0, HasClip: true,
				Clip: drawlist.Rect{X: int32(x), Y: int32(baselineY - 20), W: int32(safeMaxWidth), H: int32(controlHeight + 40)},
			}
			c.List().RecordSprite(sprite)

			if rn < 128 {
				cursorX += float64(cached.frame.Width)
			} else {
				cursorX += float64(size + 1)
			}
		}

		// ★ 核心：渲染完这一行后，直接按本行最后生效的字号计算行距，推挤下一行
		lineStep := int(float64(lineEndSize) * LINE_STEP_PERCENT)
		if lineStep < lineEndSize+2 {
			lineStep = lineEndSize + 2
		}
		currentLineY += lineStep
	}
	return int(maxLineWidth + 0.5), true
}

// DrawRetailFilmCJKCylindrical keeps the original signature.
func (c *Client) DrawRetailFilmCJKCylindrical(text string, x, baselineY, maxWidth int, controlHeight int, palColor byte, attribs uint32) (int, bool) {
	return c.DrawRetailFilmCJK(text, x, baselineY, maxWidth, controlHeight, palColor, attribs)
}
