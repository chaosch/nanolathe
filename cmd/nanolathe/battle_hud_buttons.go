package main

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"github.com/nanolathe-gg/nanolathe/vfs"
	"strings"
	"sync"
)

func RenderCommandButtons(fs *vfs.FS) *formats.GAF {
	if !client.NeedCjkTranslate() {
		return nil
	}
	//buttonnGAF是所有按钮GAF
	butttonGAF, _ := LoadGAFCommandButtons(fs)
	if butttonGAF == nil {
		return nil
	}
	//翻译渲染所有按钮GAF
	TranslateCommands(butttonGAF, commandButtonNames[:])
	return butttonGAF
	//for i, _ := range gafs.Entries {
	//	cmd := ""
	//	if commandButtonName(gafs.Entries[i].Name) != "" {
	//		cmd = commandButtonName(gafs.Entries[i].Name)
	//	}
	//	if cmd != "" {
	//		if cached, ok := renderedButtonCache.Load(gafs.Entries[i].Name); ok {
	//			cachedEntry := cached.(*formats.GAFEntry)
	//			// 命中緩存，直接覆蓋指針
	//			gafs.Entries[i].Frames = cachedEntry.Frames
	//			gafs.Entries[i].FrameCount = cachedEntry.FrameCount
	//		} else {
	//			//第三方阵营，只能用其中一个来其中一个来匹配。
	//			fallbackName := commandButtonName(gafs.Entries[i].Name)
	//			fallbackName = "ARM" + fallbackName
	//			if fallbackCached, ex := renderedButtonCache.Load(fallbackName); ex {
	//				fallbackCachedEntry := fallbackCached.(*formats.GAFEntry)
	//				// 命中緩存，直接覆蓋指針
	//				gafs.Entries[i].Frames = fallbackCachedEntry.Frames
	//				gafs.Entries[i].FrameCount = fallbackCachedEntry.FrameCount
	//			} else {
	//				continue
	//			}
	//		}
	//	}
	//}
}
func LoadGAFCommandButtons(fs *vfs.FS) (*formats.GAF, error) {
	data, err := fs.ReadFile("anims/commongui.gaf")
	if err != nil {
		return nil, err
	}
	if gaf, err := formats.LoadGAFNoneText(data, commandButtonNames[:]); err != nil {
		return nil, err
	} else {
		return gaf, nil
	}
}

func TranslateCommands(gaf *formats.GAF, keys []string) {
	for i, _ := range gaf.Entries {
		cmd := gaf.Entries[i].Name
		realCmd := ""
		needTranslate := false
		for _, k := range keys {
			if strings.Contains(cmd, k) {
				realCmd = i18n.T(k)
				needTranslate = true
				break
			}
		}
		if needTranslate {
			RenderCjkFontToGAF(&gaf.Entries[i], realCmd)
		}
	}
}

// 傳入 Entry 的指針，直接在內部修改像素

var renderedButtonCache sync.Map

func RenderCjkFontToGAF(entry *formats.GAFEntry, text string) {
	// 定義常量
	if cached, ok := renderedButtonCache.Load(entry.Name); ok {
		cachedEntry := cached.(*formats.GAFEntry)
		entry.Frames = cachedEntry.Frames
		entry.FrameCount = cachedEntry.FrameCount
		entry.Unknown1 = cachedEntry.Unknown1
		entry.Unknown2 = cachedEntry.Unknown2
		return
	}
	const (
		PaddingY               = 2
		PressedOffsetY         = 2 // 按下时文字下移像素
		PaddingLeft            = 25
		PaddingLeftNormal      = 4
		PaddingRight           = 0
		UnderlineColor         = 38
		UnderlineColorDisabled = 248
		UnderlineOffsetY       = 1
	)

	isStateButton := false
	var textParts []string
	cmd := commandButtonName(entry.Name)
	if cmd == "CLOAK" || cmd == "FIREORD" || cmd == "MOVEORD" || cmd == "ONOFF" {
		isStateButton = true
		textParts = strings.Split(text, "|")
	}

	for i := range entry.Frames {
		frame := entry.Frames[i].Frame
		if frame == nil || len(frame.Pixels) == 0 {
			continue
		}

		width := int(frame.Width)
		height := int(frame.Height)

		// =====================================================================
		// 【破局之法】：彻底重构第二子帧
		// =====================================================================
		targetFrame := frame
		if len(frame.Subframes) > 1 && frame.Subframes[1] != nil {
			sub := frame.Subframes[1]
			if sub.Width != frame.Width || sub.Height != frame.Height {
				// 如果子帧尺寸和主帧不一样，强行建一个一样大的空画布
				targetFrame = &formats.GAFFrame{
					Width:       frame.Width,
					Height:      frame.Height,
					XOffset:     frame.XOffset,
					YOffset:     frame.YOffset,
					Pixels:      make([]byte, width*height),
					Transparent: make([]bool, width*height),
				}
				for p := 0; p < len(targetFrame.Transparent); p++ {
					targetFrame.Transparent[p] = true
				}
				// 替换回原对象的切片里
				frame.Subframes[1] = targetFrame
			} else {
				targetFrame = sub
				targetFrame.XOffset = frame.XOffset
				targetFrame.YOffset = frame.YOffset

				// 尺寸一致，直接清空原本的英文
				for p := 0; p < len(targetFrame.Pixels); p++ {
					targetFrame.Pixels[p] = 0
					if p < len(targetFrame.Transparent) {
						targetFrame.Transparent[p] = true
					}
				}
			}
		}
		// =====================================================================

		currentText := text
		if isStateButton {
			if i < len(textParts) {
				currentText = textParts[i]
			} else {
				currentText = textParts[len(textParts)-1]
			}
		}

		availHeight := height - PaddingY*2
		if availHeight < 1 {
			availHeight = 1
		}

		curTier := client.PickTier(availHeight)
		face := film.TextFaceInGame(curTier.Name)
		if face == nil {
			continue
		}
		lineSize := curTier.Px

		var palColor uint8 = 15
		if i == 1 {
			palColor = 14
		} else if i == 2 {
			palColor = 8
		}

		var lineW float64
		for _, rn := range currentText {
			g := face.Glyphs[string(rn)]
			if g != nil && g.W > 0 {
				lineW += float64(g.W)
				continue
			}
			if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
				lineW += float64(lineSize)
			} else {
				lineW += float64(lineSize) / 2
			}
		}

		currentPaddingLeft := PaddingLeftNormal
		if isStateButton {
			currentPaddingLeft = PaddingLeft
		}

		availWidth := float64(width) - float64(currentPaddingLeft+PaddingRight)
		cursorX := float64(currentPaddingLeft)

		if !isStateButton {
			cursorX += (availWidth - lineW) / 2.0
		}

		type drawInfo struct {
			runes            rune
			drawX            int
			drawY            int
			width            int
			height           int
			finalPixels      []byte
			finalTransparent []bool
		}
		var drawList []drawInfo

		for _, rn := range currentText {
			g := face.Glyphs[string(rn)]
			if g == nil || g.Frame() == nil {
				if (rn >= 0x4E00 && rn <= 0x9FFF) || (rn >= 0x3400 && rn <= 0x4DBF) {
					cursorX += float64(lineSize)
				} else {
					cursorX += float64(lineSize) / 2
				}
				continue
			}

			glyphFrame := g.Frame()
			if glyphFrame.Width == 0 || glyphFrame.Height == 0 || len(glyphFrame.Pixels) == 0 {
				cursorX += float64(g.W)
				continue
			}

			var finalPixels []byte
			var finalTransparent []bool

			if curTier.Name == "tiny" && palColor > 0 {
				newPixels := make([]byte, len(glyphFrame.Pixels))
				for pIdx := 0; pIdx < len(glyphFrame.Pixels); pIdx++ {
					if pIdx < len(glyphFrame.Transparent) && !glyphFrame.Transparent[pIdx] {
						newPixels[pIdx] = palColor
					}
				}
				finalPixels = newPixels
				finalTransparent = glyphFrame.Transparent
			} else {
				finalPixels = glyphFrame.Pixels
				finalTransparent = glyphFrame.Transparent
			}

			drawX := int(cursorX + float64(g.Left))
			drawY := PaddingY + (availHeight-int(glyphFrame.Height))/2

			drawList = append(drawList, drawInfo{
				runes: rn, drawX: drawX, drawY: drawY,
				width: int(glyphFrame.Width), height: int(glyphFrame.Height),
				finalPixels: finalPixels, finalTransparent: finalTransparent,
			})

			cursorX += float64(g.W)
		}

		underlineIdx := -1
		for idx := len(drawList) - 1; idx >= 0; idx-- {
			if drawList[idx].runes < 128 {
				underlineIdx = idx
				break
			}
		}

		for idx, item := range drawList {
			actualDrawY := item.drawY

			if i == 1 && !isStateButton {
				actualDrawY += PressedOffsetY
			}

			if idx == underlineIdx {
				actualDrawY -= UnderlineOffsetY
			}

			for gy := 0; gy < item.height; gy++ {
				for gx := 0; gx < item.width; gx++ {
					px := item.drawX + gx
					py := actualDrawY + gy

					// 现在按主帧宽高算，targetFrame 保证等大，绝对不越界
					if px >= 0 && px < width && py >= 0 && py < height {
						idxPix := py*width + px
						gIdx := gy*item.width + gx

						if gIdx >= 0 && gIdx < len(item.finalPixels) {
							transparent := false
							if gIdx < len(item.finalTransparent) {
								transparent = item.finalTransparent[gIdx]
							}

							if !transparent {
								pixel := item.finalPixels[gIdx]

								if i == 2 && !isStateButton {
									if curTier.Name == "tiny" {
										targetFrame.Pixels[idxPix] = 8
										if idxPix < len(targetFrame.Transparent) {
											targetFrame.Transparent[idxPix] = false
										}
									} else {
										if pixel >= 10 && pixel <= 31 {
											targetFrame.Pixels[idxPix] = 8
										} else {
											targetFrame.Pixels[idxPix] = pixel
										}
										if idxPix < len(targetFrame.Transparent) {
											targetFrame.Transparent[idxPix] = false
										}
									}
								} else {
									targetFrame.Pixels[idxPix] = pixel
									if idxPix < len(targetFrame.Transparent) {
										targetFrame.Transparent[idxPix] = false
									}
								}
							}
						}
					}
				}
			}

			if idx == underlineIdx {
				underlineY := actualDrawY + item.height
				if underlineY >= height {
					underlineY = height - 1
				}

				ulColor := uint8(UnderlineColor)
				if i == 2 && !isStateButton || isStateButton && i == 4 && cmd == "ONOFF" {
					ulColor = UnderlineColorDisabled
				}

				for ux := 0; ux < item.width; ux++ {
					ulx := item.drawX + ux
					if ulx >= 0 && ulx < width && underlineY >= 0 && underlineY < height {
						idxPix := underlineY*width + ulx
						// 这里不需要再判断越界了，因为 targetFrame 已经强制撑成主帧大小
						targetFrame.Pixels[idxPix] = ulColor
						if idxPix < len(targetFrame.Transparent) {
							targetFrame.Transparent[idxPix] = false
						}
					}
				}
			}
		}
	}

	cachedCopy := &formats.GAFEntry{
		Name:       entry.Name,
		FrameCount: entry.FrameCount,
		Unknown1:   entry.Unknown1,
		Unknown2:   entry.Unknown2,
		Frames:     entry.Frames,
	}
	renderedButtonCache.Store(entry.Name, cachedCopy)
}
