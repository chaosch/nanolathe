package main

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"github.com/nanolathe-gg/nanolathe/vfs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func RenderCommandButtons(fs *vfs.FS, gafs *formats.GAF) {
	if !client.NeedCjkTranslate() {
		return
	}
	//buttonnGAF是所有按钮GAF
	butttonGAF, _ := LoadGAFFromArchive(fs, "totala1.hpi", "anims/commongui.GAF")
	//翻译渲染所有按钮GAF
	TranslateCommands(butttonGAF, commandButtonNames[:])
	for i, _ := range gafs.Entries {
		cmd := ""
		if commandButtonName(gafs.Entries[i].Name) != "" {
			cmd = commandButtonName(gafs.Entries[i].Name)
		}
		if cmd != "" {
			if cached, ok := renderedButtonCache.Load(gafs.Entries[i].Name); ok {
				cachedEntry := cached.(*formats.GAFEntry)
				// 命中緩存，直接覆蓋指針
				gafs.Entries[i].Frames = cachedEntry.Frames
				gafs.Entries[i].FrameCount = cachedEntry.FrameCount
			} else {
				//第三方阵营，只能用其中一个来其中一个来匹配。
				fallbackName := commandButtonName(gafs.Entries[i].Name)
				fallbackName = "ARM" + fallbackName
				if fallbackCached, ex := renderedButtonCache.Load(fallbackName); ex {
					fallbackCachedEntry := fallbackCached.(*formats.GAFEntry)
					// 命中緩存，直接覆蓋指針
					gafs.Entries[i].Frames = fallbackCachedEntry.Frames
					gafs.Entries[i].FrameCount = fallbackCachedEntry.FrameCount
				} else {
					continue
				}
			}
		}
	}
}

func LoadGAFFromArchive(fs *vfs.FS, archiveName string, gafName string) (*formats.GAF, error) {
	// 1. 透過 VFS 拿到這個 GAF 所有的來源層
	sources := fs.Sources(gafName)
	if len(sources) == 0 {
		return nil, fmt.Errorf("找不到文件: %s", gafName)
	}
	// 2. 尋找目標包的絕對路徑
	var targetArchivePath string
	for _, src := range sources {
		// 用 filepath.Base 取出包名，和傳入的 archiveName 做忽略大小寫的比較
		if strings.EqualFold(filepath.Base(src.Source.SourcePath), archiveName) {
			targetArchivePath = src.Source.SourcePath
			break
		}
	}

	if targetArchivePath == "" {
		return nil, fmt.Errorf("找不到包: %s", archiveName)
	}

	// 3. 建立一個乾淨的臨時 VFS，用反查出來的絕對路徑直接掛載
	tempFS := vfs.New()
	_, err := tempFS.MountArchive(targetArchivePath, 0)
	if err != nil {
		return nil, fmt.Errorf("掛載 %s 失敗: %w", targetArchivePath, err)
	}
	defer tempFS.Close()

	// 4. 從這個乾淨的 VFS 直接讀取
	file, err := tempFS.OpenMount(0, gafName)
	if err != nil {
		return nil, fmt.Errorf("包裡找不到 %s: %w", gafName, err)
	}
	defer file.Close()

	// 5. 輸出數據
	data, err := vfs.WholeBytes(file)
	if err != nil {
		return nil, err
	}
	client.PrintDebug(len(data))
	os.WriteFile("e:/raw_commongui.gaf", data, 0644)
	return formats.LoadGAFIsolated(data, commandButtonNames[:])
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

func RenderCjkFontToGAFBAK(entry *formats.GAFEntry, text string) {
	// 定義常量（你可以根據實際情況調整數值）
	if cached, ok := renderedButtonCache.Load(entry.Name); ok {
		cachedEntry := cached.(*formats.GAFEntry)
		entry.Frames = cachedEntry.Frames
		entry.FrameCount = cachedEntry.FrameCount
		entry.Unknown1 = cachedEntry.Unknown1
		entry.Unknown2 = cachedEntry.Unknown2
		return
	}
	const (
		PaddingY       = 2 // 上下留白像素
		PressedOffsetY = 1 // 按下（第1幀）文字下沉的像素
	)

	for i := range entry.Frames {
		frame := entry.Frames[i].Frame
		if frame == nil || len(frame.Pixels) == 0 {
			continue
		}

		width := int(frame.Width)
		height := int(frame.Height)

		// 1. 計算扣除留白後的有效高度
		availHeight := height - PaddingY*2
		if availHeight < 1 {
			availHeight = 1
		}

		// 2. 根據有效高度決定字體大小
		curTier := client.PickTier(availHeight)
		face := film.TextFaceInGame(curTier.Name)
		if face == nil {
			continue
		}
		lineSize := curTier.Px

		// 3. 決定顏色（根據幀狀態：0=正常, 1=按下, 2=禁用）
		var palColor uint8 = 15 // 正常
		if i == 1 {
			palColor = 14 // 按下
		} else if i == 2 {
			palColor = 8 // 禁用
		}

		// 4. 計算行寬
		var lineW float64
		for _, rn := range text {
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

		// 5. 水平居中對齊起點
		cursorX := (float64(width) - lineW) / 2.0

		// 6. 逐字渲染與貼圖
		for _, rn := range text {
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

			// tiny 動態上色，small/normal 直接渲染
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

			// 7. 定位（加入留白與下沉邏輯）
			drawX := int(cursorX + float64(g.Left))

			// 垂直居中：起點為 PaddingY，加上有效高度和字體高度的居中偏移
			drawY := PaddingY + (availHeight-int(glyphFrame.Height))/2

			// 按下狀態文字下沉
			if i == 1 {
				drawY += PressedOffsetY
			}

			// 8. 像素 Blit（原地寫入 frame.Pixels）
			for gy := 0; gy < int(glyphFrame.Height); gy++ {
				for gx := 0; gx < int(glyphFrame.Width); gx++ {
					px := drawX + gx
					py := drawY + gy

					if px >= 0 && px < width && py >= 0 && py < height {
						idx := py*width + px
						gIdx := gy*int(glyphFrame.Width) + gx

						if gIdx >= 0 && gIdx < len(finalPixels) && gIdx < len(finalTransparent) {
							if !finalTransparent[gIdx] {
								frame.Pixels[idx] = finalPixels[gIdx]
							}
						}
					}
				}
			}
			cursorX += float64(g.W)
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
		PaddingY               = 2   // 上下留白像素
		PressedOffsetY         = 1   // 按下（第1幀）文字下沉的像素
		PaddingLeft            = 25  // 左側留白（避開邊框）
		PaddingLeftNormal      = 4   // 普通按鈕左留白
		PaddingRight           = 0   // 右側留白（避開原廠指示燈）
		UnderlineColor         = 38  // 下劃線顏色（正常綠色）
		UnderlineColorDisabled = 248 // 下劃線顏色（禁用狀態灰色）
		UnderlineOffsetY       = 1   // 被劃線字母上移的像素
	)

	// 1. 判斷是否為狀態按鈕
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

		// 決定當前幀要畫的文字
		currentText := text
		if isStateButton {
			if i < len(textParts) {
				currentText = textParts[i]
			} else {
				currentText = textParts[len(textParts)-1]
			}
		}

		width := int(frame.Width)
		height := int(frame.Height)

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

		// 決定顏色
		var palColor uint8 = 15 // 正常
		if i == 1 {
			palColor = 14 // 按下
		} else if i == 2 {
			palColor = 8 // 禁用
		}

		// 計算行寬
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

		// 根據按鈕類型選擇左留白
		currentPaddingLeft := PaddingLeftNormal
		if isStateButton {
			currentPaddingLeft = PaddingLeft
		}

		availWidth := float64(width) - float64(currentPaddingLeft+PaddingRight)
		cursorX := float64(currentPaddingLeft)

		// 普通按鈕居中，狀態按鈕左對齊
		if !isStateButton {
			cursorX += (availWidth - lineW) / 2.0
		}

		// 暫存所有需要繪製的字母信息
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

		// 第一步：計算位置並收集信息
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

			// 需求 5：保留非 tiny 字體的立體感
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

			// 狀態切換按鈕按下時不位移
			if i == 1 && !isStateButton {
				drawY += PressedOffsetY
			}

			drawList = append(drawList, drawInfo{
				runes: rn, drawX: drawX, drawY: drawY,
				width: int(glyphFrame.Width), height: int(glyphFrame.Height),
				finalPixels: finalPixels, finalTransparent: finalTransparent,
			})

			cursorX += float64(g.W)
		}

		// 第二步：找到需要畫下劃線的字母（最後一個英文字母）
		underlineIdx := -1
		for idx := len(drawList) - 1; idx >= 0; idx-- {
			if drawList[idx].runes < 128 {
				underlineIdx = idx
				break
			}
		}

		// 第三步：執行繪製
		for idx, item := range drawList {
			actualDrawY := item.drawY
			// 需要劃線的字母整體上移
			if idx == underlineIdx {
				actualDrawY -= UnderlineOffsetY
			}

			// 畫字母
			for gy := 0; gy < item.height; gy++ {
				for gx := 0; gx < item.width; gx++ {
					px := item.drawX + gx
					py := actualDrawY + gy

					if px >= 0 && px < width && py >= 0 && py < height {
						idxPix := py*width + px
						gIdx := gy*item.width + gx

						if gIdx >= 0 && gIdx < len(item.finalPixels) && gIdx < len(item.finalTransparent) {
							if !item.finalTransparent[gIdx] {
								pixel := item.finalPixels[gIdx]

								// 需求 1 & 2：狀態按鈕第3幀不變灰，普通按鈕第3幀變灰
								if i == 2 && !isStateButton {
									if curTier.Name == "tiny" {
										frame.Pixels[idxPix] = 8
									} else {
										if pixel >= 10 && pixel <= 31 {
											frame.Pixels[idxPix] = 8
										} else {
											frame.Pixels[idxPix] = pixel
										}
									}
								} else {
									frame.Pixels[idxPix] = pixel
								}
							}
						}
					}
				}
			}

			// 畫下劃線
			if idx == underlineIdx {
				underlineY := actualDrawY + item.height
				if underlineY >= height {
					underlineY = height - 1
				}

				// 【核心改動】：普通按鈕第三幀用灰色下劃線
				ulColor := uint8(UnderlineColor)
				if i == 2 && !isStateButton || isStateButton && i == 4 && cmd == "ONOFF" {
					ulColor = UnderlineColorDisabled
				}

				for ux := 0; ux < item.width; ux++ {
					ulx := item.drawX + ux
					if ulx >= 0 && ulx < width && underlineY >= 0 && underlineY < height {
						frame.Pixels[underlineY*width+ulx] = ulColor
					}
				}
			}
		}
	}

	// 存入緩存
	cachedCopy := &formats.GAFEntry{
		Name:       entry.Name,
		FrameCount: entry.FrameCount,
		Unknown1:   entry.Unknown1,
		Unknown2:   entry.Unknown2,
		Frames:     entry.Frames,
	}
	renderedButtonCache.Store(entry.Name, cachedCopy)
}
