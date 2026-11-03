package film

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/nanolathe-gg/nanolathe/formats"
)

// ============================================================
// 图集元数据（与 Python 烘焙脚本输出 JSON 一一对应）
// ============================================================

type ingameGlyphMeta struct {
	X       int     `json:"x"`
	Y       int     `json:"y"`
	W       int     `json:"w"`
	H       int     `json:"h"`
	Left    int     `json:"left"`
	Top     int     `json:"top"`
	Advance float64 `json:"advance"`
	Scale   float64 `json:"scale"`
}

type ingameAtlasJSON struct {
	Cap    int                        `json:"cap"`
	Glyphs map[string]ingameGlyphMeta `json:"glyphs"`
}

// InGameAtlasFace 表示一个已加载的字体面
type InGameAtlasFace struct {
	Name   string
	Cap    int
	atlas  image.Image
	glyphs map[rune]ingameGlyphMeta
}

var (
	ingameFacesMu  sync.RWMutex
	ingameAtlasMap = map[string]*InGameAtlasFace{}

	ingameAssetsDir = filepath.Join("internal", "film", "assets")
)

// InGameFaceNames 与 Python ALL_FACE 的 key 一致
var InGameFaceNames = []string{"display", "body", "small", "normal", "medium", "large", "title"}

// SetInGameAssetsDir 允许外部覆盖图集目录
func SetInGameAssetsDir(dir string) { ingameAssetsDir = dir }

// LoadInGameAtlases 加载全部字体面（幂等，可多次调用）
func LoadInGameAtlases() error {
	ingameFacesMu.Lock()
	defer ingameFacesMu.Unlock()
	for _, name := range InGameFaceNames {
		if _, ok := ingameAtlasMap[name]; ok {
			continue
		}
		f, err := loadInGameFace(name)
		if err != nil {
			return fmt.Errorf("load ingame face %s: %w", name, err)
		}
		ingameAtlasMap[name] = f
	}
	return nil
}

// InGameAtlasesReady 返回是否所有字体面都已加载
func InGameAtlasesReady() bool {
	ingameFacesMu.RLock()
	defer ingameFacesMu.RUnlock()
	for _, name := range InGameFaceNames {
		if _, ok := ingameAtlasMap[name]; !ok {
			return false
		}
	}
	return true
}

// InGameFace 按名称取字体面，未加载返回 nil
func InGameFace(name string) *InGameAtlasFace {
	ingameFacesMu.RLock()
	defer ingameFacesMu.RUnlock()
	return ingameAtlasMap[name]
}

// loadInGameFace 读取 {name}-cjk.json 与 {name}-cjk.png
func loadInGameFace(name string) (*InGameAtlasFace, error) {
	jsonPath := filepath.Join(ingameAssetsDir, name+"-cjk.json")
	pngPath := filepath.Join(ingameAssetsDir, name+"-cjk.png")

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, err
	}
	var meta ingameAtlasJSON
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse %s: %w", jsonPath, err)
	}

	pf, err := os.Open(pngPath)
	if err != nil {
		return nil, err
	}
	defer pf.Close()
	img, err := png.Decode(pf)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", pngPath, err)
	}

	glyphs := make(map[rune]ingameGlyphMeta, len(meta.Glyphs))
	for s, gm := range meta.Glyphs {
		rs := []rune(s)
		if len(rs) != 1 {
			continue
		}
		glyphs[rs[0]] = gm
	}

	return &InGameAtlasFace{
		Name:   name,
		Cap:    meta.Cap,
		atlas:  img,
		glyphs: glyphs,
	}, nil
}

// ============================================================
// 调色板反查（与 Python 端 PALETTE + DARKEN=0.85 完全一致）
// ============================================================

var ingamePalette = [16][3]uint8{
	{213, 213, 182}, {194, 189, 160}, {165, 165, 131}, {145, 145, 111},
	{124, 124, 94}, {101, 101, 73}, {84, 84, 56}, {70, 70, 43},
	{56, 56, 29}, {43, 43, 19}, {29, 29, 12}, {22, 22, 5},
	{19, 19, 5}, {16, 16, 0}, {12, 12, 0}, {9, 9, 0},
}

const ingamePalBase = 64 // COLOR_BASE_BRIGHT

func ingameRGBToPalIdx(r, g, b uint8) byte {
	best, bestDist := 0, 1<<30
	for i, c := range ingamePalette {
		dr := int(r) - int(c[0])
		dg := int(g) - int(c[1])
		db := int(b) - int(c[2])
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			bestDist, best = d, i
		}
	}
	return byte(ingamePalBase + best)
}

// ============================================================
// 对外查询
// ============================================================

// InGameGlyphInfo 返回字形的度量信息
type InGameGlyphInfo struct {
	W       int
	H       int
	Left    int
	Top     int
	Advance float64
	Scale   float64
}

// GlyphInfo 查询单个字符的度量信息；不存在返回 nil
func (f *InGameAtlasFace) GlyphInfo(rn rune) *InGameGlyphInfo {
	if f == nil {
		return nil
	}
	gm, ok := f.glyphs[rn]
	if !ok {
		return nil
	}
	return &InGameGlyphInfo{
		W:       gm.W,
		H:       gm.H,
		Left:    gm.Left,
		Top:     gm.Top,
		Advance: gm.Advance,
		Scale:   gm.Scale,
	}
}

// Frame 从图集切出该字符的帧，Pixels 里存调色板索引 64~79。
// 不做任何缓存，每次调用都新分配。
func (f *InGameAtlasFace) Frame(rn rune) *formats.GAFFrame {
	if f == nil {
		return nil
	}
	gm, ok := f.glyphs[rn]
	if !ok || gm.W <= 0 || gm.H <= 0 {
		return nil
	}

	w, h := gm.W, gm.H
	pixels := make([]byte, w*h)
	transparent := make([]bool, w*h)
	b := f.atlas.Bounds()

	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			sx, sy := gm.X+tx, gm.Y+ty
			idx := ty*w + tx

			if sx < b.Min.X || sx >= b.Max.X || sy < b.Min.Y || sy >= b.Max.Y {
				transparent[idx] = true
				continue
			}
			r, g, bl, a := f.atlas.At(sx, sy).RGBA()
			if a == 0 {
				transparent[idx] = true
				continue
			}
			r8, g8, b8 := byte(r>>8), byte(g>>8), byte(bl>>8)
			if r8 < 8 && g8 < 8 && b8 < 8 {
				pixels[idx] = 79 // 纯黑描边
				continue
			}
			pixels[idx] = ingameRGBToPalIdx(r8, g8, b8)
		}
	}

	return &formats.GAFFrame{
		Width:       uint16(w),
		Height:      uint16(h),
		Pixels:      pixels,
		Transparent: transparent,
	}
}

// Frame 从 Levels[0] 切出 GAFFrame，Pixels 存调色板索引 64~79。
// Levels[0] 带 2px padding，字形在 (2,2) 开始。
// 约定：灰度 0 = 透明；灰度 1 = 纯黑描边；其余 = 字形亮度。
func (g *atlasGlyph) Frame() *formats.GAFFrame {
	if g == nil || g.W <= 0 || g.H <= 0 || len(g.Levels) == 0 {
		return nil
	}
	src := g.Levels[0]
	if src == nil {
		return nil
	}

	const pad = 2
	w, h := g.W, g.H
	pixels := make([]byte, w*h)
	transparent := make([]bool, w*h)
	b := src.Bounds()

	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			sx := pad + tx
			sy := pad + ty
			idx := ty*w + tx

			if sx < b.Min.X || sx >= b.Max.X || sy < b.Min.Y || sy >= b.Max.Y {
				transparent[idx] = true
				continue
			}
			gray := src.GrayAt(sx, sy).Y
			if gray == 0 {
				transparent[idx] = true
				continue
			}
			if gray == 1 {
				pixels[idx] = 79 // 描边
				continue
			}
			ratio := math.Pow(float64(gray)/255.0, 0.8)
			palIdx := 64 + int((1.0-ratio)*15)
			if palIdx < 64 {
				palIdx = 64
			}
			if palIdx > 78 {
				palIdx = 78
			}
			pixels[idx] = byte(palIdx)
		}
	}

	return &formats.GAFFrame{
		Width:       uint16(w),
		Height:      uint16(h),
		Pixels:      pixels,
		Transparent: transparent,
	}
}

func TextFaceInGame(name string) *atlasFace {
	key := name
	if CJKReady() {
		return cjkFaces()[key]
	}
	return filmFaces()[key]
}
