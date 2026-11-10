package client

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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

func PrintDebug(args ...interface{}) {
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
func PrintDebugInternal(args ...interface{}) {
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
var dumpedlock sync.RWMutex

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
type FaceTier struct {
	Name string
	Px   int
}

var faceTiers = []FaceTier{
	{"tiny", 12},
	{"small", 14},
	{"normal", 16}, //{"normal", 20},
	//{"medium", 20},
	//{"large", 20},
	//{"title", 20},
}

func PickTier(targetH int) FaceTier {
	if targetH <= 0 {
		return faceTiers[0]
	}
	for _, t := range faceTiers {
		if t.Px >= targetH {
			return t
		}
	}
	return faceTiers[len(faceTiers)-1]
}

func (c *Client) DrawRetailFilmCJK(text string, x, baselineY, maxWidth int, controlHeight int, palColor byte, attribs uint32) (int, bool) {
	key := TranslateCacheKey{
		text,
		x,
		baselineY,
		maxWidth,
		controlHeight,
		palColor,
		attribs,
	}
	real_text := text
	if printed == nil {
		printed = make(map[string]int)
	}
	if len(real_text) == 0 {
		return 0, false
	}

	// 翻译兜底
	tr := i18n.T(real_text)
	if tr == real_text {
		if _, exists := printed[real_text]; !exists {
			//c.PrintDebugInternal("translate text not found!", real_text)
			return 0, false
		}
	}

	safeMaxWidth := maxWidth
	if safeMaxWidth < 0 {
		safeMaxWidth = 9999
	}

	// === 1. 解析控制符 ===
	type token struct {
		rn      rune
		forceNL bool
	}

	var tokens []token
	var explicitSize = 0
	var gridRows, gridCols = 0, 0

	runes := []rune(tr)
	i := 0
	for i < len(runes) {
		rn := runes[i]
		if rn == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			// 解析 \nxx,xx 或单纯的 \n
			if next == 'n' || next == 'N' {
				i += 2
				start := i
				for i < len(runes) && (runes[i] >= '0' && runes[i] <= '9' || runes[i] == ',') {
					i++
				}
				if i > start {
					parts := string(runes[start:i])
					var r, c int
					if _, err := fmt.Sscanf(parts, "%d,%d", &r, &c); err == nil {
						gridRows, gridCols = r, c
					}
				}
				// ★ 核心修复：遇到 \n，必须往 tokens 里插入一个换行标记！
				tokens = append(tokens, token{forceNL: true})
				continue
			}
			// 解析 \sNN
			if next == 's' || next == 'S' {
				i += 2
				start := i
				for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
					i++
				}
				if i > start {
					fmt.Sscanf(string(runes[start:i]), "%d", &explicitSize)
				}
				continue
			}
		}

		tokens = append(tokens, token{rn: rn})
		i++
	}

	// === 2. 决定字号 ===
	const paddingY = 0
	availHeight := controlHeight - paddingY*2
	if availHeight < 1 {
		availHeight = 1
	}

	var defaultTier FaceTier
	if explicitSize > 0 {
		defaultTier = PickTier(explicitSize)
	} else if gridRows > 1 && gridCols > 0 {
		cellHeight := availHeight / gridRows
		defaultTier = PickTier(cellHeight)
	} else {
		defaultTier = PickTier(availHeight)
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
	currentLineY := baselineY + paddingY
	curTier := defaultTier
	curSize := curTier.Px
	maxLineWidth := 0.0

	// 判断是否多行
	isMultiLine := len(lines) > 1
	sprite := drawlist.Sprite{}
	for _, line := range lines {
		lineTier := curTier
		lineSize := curSize

		// 量行宽（统一使用 g.W）
		var lineW float64
		for _, tk := range line {
			rn := tk.rn
			face := film.TextFaceInGame(lineTier.Name)
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

		for _, tk := range line {
			rn := tk.rn
			face := film.TextFaceInGame(lineTier.Name)
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

			// tiny 动态上色（深拷贝 Frame 防污染）
			if lineTier.Name == "tiny" && palColor > 0 {
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

			// ★ 垂直定位：单行居中，多行往下递推
			var drawY int32
			if !isMultiLine {
				drawY = int32(baselineY + (controlHeight-int(frame.Height))/2)
			} else {
				drawY = int32(currentLineY + (lineSize-int(frame.Height))/2)
			}

			sprite = drawlist.Sprite{
				Frame:   frame,
				X:       drawX,
				Y:       drawY,
				Kind:    drawlist.BlitKeyed,
				Key:     0,
				HasClip: false,
			}
			c.List().RecordSprite(sprite)

			cursorX += float64(g.W)

			entry := TranslateCache[key]
			entry.Sprites = append(entry.Sprites, sprite)
			TranslateCache[key] = entry
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
	if !i18n.I18nLanguageNeedCJK() {
		return 0, false
	}
	key := TranslateCacheKey{
		text,
		x,
		baselineY,
		maxWidth,
		controlHeight,
		palColor,
		attribs,
	}
	if cache, ok := TranslateCache[key]; ok {
		sprites := cache.Sprites
		for _, sprite := range sprites {
			c.List().RecordSprite(sprite)
		}
		return 0, true
	} else {
		return c.DrawRetailFilmCJK(text, x, baselineY, maxWidth, controlHeight, palColor, attribs)
	}
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

type TranslateValue struct {
	Key     TranslateCacheKey
	GAEntry formats.GAFEntry
	Sprites []drawlist.Sprite
}

type TranslateCacheKey struct {
	text          string
	x             int
	baselineY     int
	maxWidth      int
	controlHeight int
	palColor      byte
	attribs       uint32
}

var TranslateCache map[TranslateCacheKey]TranslateValue

func init() {
	TranslateCache = make(map[TranslateCacheKey]TranslateValue)
}

var dumped map[string]uint32

func DumpGAFFrame(f *formats.GAFFrame, Name string) {
	defer dumpedlock.Unlock()
	dumpedlock.Lock()
	if dumped == nil {
		dumped = make(map[string]uint32)
	}
	var pcindex int
	pcindex = 2                                // 根据你的调用层级决定，如果层级不对，改成 2 或者 4
	pc, _, line, ok := runtime.Caller(pcindex) // ★ 接住 file 和 line
	if ok {
		fn := runtime.FuncForPC(pc)
		if fn != nil {
			// 如果只想看文件名，可以用 filepath.Base(file) 取最后一部分
			// 比如：prompt := fmt.Sprintf("debug message: %s (%s:%d):%v\n", fn.Name(), filepath.Base(file), line, args)
			prompt := fmt.Sprintf("%s——%d", fn.Name(), line)
			prompt = strings.Replace(prompt, "*", "", -1)
			//if _, exists := printed[prompt]; !exist
			dumped[prompt]++
			err := dumpGAFFramePNG(filepath.Join("E:/IdeaProjects/frames/", prompt)+"_"+Name+".png", f)
			if err != nil {
				fmt.Println(err)
			} else {
				PrintDebug(filepath.Join("E:/IdeaProjects/frames/", prompt) + "_" + Name + ".png")
			}
			//}
		}
	}

}

func dumpGAFFramePNG(path string, f *formats.GAFFrame) error {
	if f == nil {
		return os.ErrInvalid
	}
	w := int(f.Width)
	h := int(f.Height)
	if w <= 0 || h <= 0 {
		return os.ErrInvalid
	}

	pix := f.PlainPixels
	trans := f.PlainTransparent
	if len(pix) == 0 {
		pix = f.Pixels
		trans = f.Transparent
	}
	want := w * h
	if len(pix) != want {
		return os.ErrInvalid
	}

	rect := image.Rect(0, 0, w, h)
	img := image.NewPaletted(rect, fixedPal)
	copy(img.Pix, pix)

	// 透明像素统一设置为 ColorKey 索引
	for i := range trans {
		if i >= len(img.Pix) {
			break
		}
		if trans[i] {
			img.Pix[i] = f.ColorKey
		}
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, img)
}

var fixedPal color.Palette

func init() {
	raw := []color.RGBA{
		{0, 0, 0, 255},
		{128, 0, 0, 255},
		{0, 128, 0, 255},
		{128, 128, 0, 255},
		{0, 0, 128, 255},
		{128, 0, 128, 255},
		{0, 128, 128, 255},
		{192, 192, 192, 255},
		{79, 101, 125, 255},
		{131, 153, 177, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{255, 235, 243, 255},
		{235, 199, 211, 255},
		{215, 163, 179, 255},
		{195, 135, 151, 255},
		{175, 111, 127, 255},
		{155, 91, 99, 255},
		{139, 71, 79, 255},
		{123, 59, 71, 255},
		{111, 51, 59, 255},
		{99, 43, 51, 255},
		{87, 35, 43, 255},
		{75, 27, 39, 255},
		{59, 23, 31, 255},
		{47, 15, 23, 255},
		{35, 11, 15, 255},
		{23, 7, 11, 255},
		{115, 255, 223, 255},
		{87, 231, 191, 255},
		{67, 207, 159, 255},
		{47, 183, 131, 255},
		{31, 159, 103, 255},
		{19, 139, 79, 255},
		{15, 119, 63, 255},
		{11, 107, 55, 255},
		{7, 95, 47, 255},
		{7, 83, 43, 255},
		{0, 71, 39, 255},
		{0, 63, 35, 255},
		{0, 51, 27, 255},
		{0, 39, 23, 255},
		{0, 27, 15, 255},
		{0, 19, 11, 255},
		{227, 239, 255, 255},
		{199, 223, 231, 255},
		{175, 207, 203, 255},
		{147, 183, 167, 255},
		{127, 159, 131, 255},
		{107, 135, 103, 255},
		{95, 111, 83, 255},
		{95, 99, 71, 255},
		{91, 87, 59, 255},
		{83, 67, 51, 255},
		{71, 59, 43, 255},
		{59, 51, 35, 255},
		{47, 43, 27, 255},
		{35, 31, 19, 255},
		{23, 19, 15, 255},
		{11, 11, 7, 255},
		{251, 251, 215, 255},
		{223, 223, 183, 255},
		{195, 195, 155, 255},
		{171, 171, 131, 255},
		{147, 147, 111, 255},
		{119, 119, 87, 255},
		{99, 99, 67, 255},
		{83, 83, 51, 255},
		{67, 67, 35, 255},
		{51, 51, 23, 255},
		{35, 35, 15, 255},
		{27, 27, 7, 255},
		{23, 23, 7, 255},
		{19, 19, 0, 255},
		{15, 15, 0, 255},
		{11, 11, 0, 255},
		{251, 251, 251, 255},
		{235, 235, 235, 255},
		{219, 219, 219, 255},
		{203, 203, 203, 255},
		{187, 187, 187, 255},
		{171, 171, 171, 255},
		{155, 155, 155, 255},
		{139, 139, 139, 255},
		{123, 123, 123, 255},
		{107, 107, 107, 255},
		{91, 91, 91, 255},
		{75, 75, 75, 255},
		{59, 59, 59, 255},
		{43, 43, 43, 255},
		{31, 31, 31, 255},
		{15, 15, 15, 255},
		{235, 243, 255, 255},
		{203, 227, 255, 255},
		{175, 207, 255, 255},
		{151, 179, 255, 255},
		{123, 151, 255, 255},
		{103, 127, 255, 255},
		{83, 107, 239, 255},
		{63, 91, 227, 255},
		{51, 75, 215, 255},
		{35, 59, 203, 255},
		{23, 47, 175, 255},
		{15, 39, 151, 255},
		{7, 31, 123, 255},
		{7, 23, 99, 255},
		{0, 15, 71, 255},
		{0, 11, 47, 255},
		{227, 247, 255, 255},
		{191, 219, 231, 255},
		{159, 191, 207, 255},
		{131, 167, 183, 255},
		{107, 143, 163, 255},
		{83, 119, 139, 255},
		{63, 95, 115, 255},
		{47, 75, 95, 255},
		{39, 63, 87, 255},
		{35, 55, 79, 255},
		{31, 47, 71, 255},
		{27, 39, 63, 255},
		{23, 31, 55, 255},
		{19, 27, 47, 255},
		{15, 19, 39, 255},
		{11, 15, 31, 255},
		{215, 239, 255, 255},
		{187, 227, 239, 255},
		{155, 203, 223, 255},
		{131, 183, 207, 255},
		{107, 163, 195, 255},
		{83, 143, 179, 255},
		{63, 123, 163, 255},
		{47, 107, 151, 255},
		{35, 91, 135, 255},
		{27, 75, 119, 255},
		{19, 63, 103, 255},
		{11, 51, 87, 255},
		{7, 39, 71, 255},
		{0, 27, 55, 255},
		{0, 19, 39, 255},
		{0, 11, 27, 255},
		{255, 231, 255, 255},
		{231, 199, 235, 255},
		{211, 171, 215, 255},
		{187, 147, 195, 255},
		{167, 123, 179, 255},
		{143, 99, 159, 255},
		{119, 75, 143, 255},
		{99, 59, 127, 255},
		{79, 43, 111, 255},
		{67, 31, 99, 255},
		{55, 23, 87, 255},
		{43, 15, 71, 255},
		{31, 7, 59, 255},
		{19, 0, 43, 255},
		{11, 0, 31, 255},
		{7, 0, 19, 255},
		{215, 255, 167, 255},
		{171, 231, 127, 255},
		{131, 211, 91, 255},
		{103, 191, 63, 255},
		{75, 171, 43, 255},
		{67, 151, 43, 255},
		{55, 135, 39, 255},
		{47, 119, 27, 255},
		{43, 103, 19, 255},
		{35, 91, 15, 255},
		{31, 79, 11, 255},
		{27, 67, 7, 255},
		{23, 51, 0, 255},
		{15, 39, 0, 255},
		{11, 27, 0, 255},
		{7, 15, 0, 255},
		{255, 227, 159, 255},
		{227, 199, 115, 255},
		{203, 175, 83, 255},
		{179, 151, 63, 255},
		{155, 131, 47, 255},
		{131, 111, 35, 255},
		{107, 91, 23, 255},
		{83, 71, 15, 255},
		{75, 59, 11, 255},
		{67, 51, 7, 255},
		{59, 43, 7, 255},
		{55, 35, 0, 255},
		{47, 27, 0, 255},
		{39, 19, 0, 255},
		{31, 15, 0, 255},
		{27, 11, 0, 255},
		{255, 255, 163, 255},
		{251, 243, 131, 255},
		{247, 227, 103, 255},
		{243, 211, 79, 255},
		{239, 187, 51, 255},
		{239, 167, 27, 255},
		{235, 143, 19, 255},
		{231, 123, 15, 255},
		{223, 79, 7, 255},
		{215, 35, 0, 255},
		{191, 31, 0, 255},
		{167, 27, 0, 255},
		{147, 23, 0, 255},
		{123, 19, 0, 255},
		{99, 19, 0, 255},
		{79, 15, 0, 255},
		{255, 255, 0, 255},
		{255, 191, 0, 255},
		{255, 131, 0, 255},
		{255, 71, 0, 255},
		{211, 43, 0, 255},
		{171, 23, 0, 255},
		{127, 7, 0, 255},
		{87, 0, 0, 255},
		{223, 203, 255, 255},
		{187, 159, 223, 255},
		{155, 119, 191, 255},
		{127, 87, 159, 255},
		{103, 59, 127, 255},
		{75, 35, 95, 255},
		{51, 19, 63, 255},
		{27, 7, 31, 255},
		{211, 219, 255, 255},
		{135, 159, 247, 255},
		{67, 111, 239, 255},
		{23, 71, 231, 255},
		{11, 43, 187, 255},
		{7, 23, 143, 255},
		{0, 7, 99, 255},
		{0, 0, 55, 255},
		{123, 255, 119, 255},
		{83, 223, 79, 255},
		{51, 191, 43, 255},
		{27, 159, 19, 255},
		{27, 127, 11, 255},
		{23, 95, 7, 255},
		{19, 63, 0, 255},
		{11, 31, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{0, 0, 0, 255},
		{193, 204, 217, 255},
		{160, 160, 164, 255},
		{128, 128, 128, 255},
		{255, 0, 0, 255},
		{0, 255, 0, 255},
		{255, 255, 0, 255},
		{0, 0, 255, 255},
		{255, 0, 255, 255},
		{0, 255, 255, 255},
		{255, 255, 255, 255},
	}
	fixedPal = make(color.Palette, len(raw))
	for i := range raw {
		fixedPal[i] = raw[i]
	}
}

// 傳入：包的物理路徑，和要讀的 GAF 名字

func NeedCjkTranslate() bool {
	return needCjkTranslate(i18n.Language())
}
func needCjkTranslate(language string) bool {
	return i18n.LanguageNeedCJK(language)
}

func ClearCjkTranslateCache() {
	TranslateCache = make(map[TranslateCacheKey]TranslateValue)

}
