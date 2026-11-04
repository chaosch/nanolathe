package film

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"image"
	"image/png"
	"sync"
	"sync/atomic"
)

type atlasGlyph struct {
	X, Y, W, H int
	Left, Top  int
	Advance    float64
	// Scale is how many face Units one atlas pixel covers; zero means one.
	// tools/film-font-cjk writes it for glyphs it rasterizes below the atlas
	// resolution the Latin faces use.
	Scale  float64
	Levels []*image.Gray
}

// Units is how many face Units one atlas texel of the Glyph covers.
func (g *atlasGlyph) Units() float64 {
	if g.Scale == 0 {
		return 1
	}
	return g.Scale
}

type atlasFace struct {
	Cap     float64
	Glyphs  map[string]*atlasGlyph
	Kerning map[string]float64
}

// loadAtlasFace decodes one atlas pair into coverage mipmaps. name is the
// asset stem: "display" and "body" for the Latin faces, "display-cjk" and
// "body-cjk" for the optional Chinese additions. The bytes come from readAsset,
// which the host supplies: embedded in the executable, fetched in the browser.
func loadAtlasFace(name string) *atlasFace {
	metadata, err := readAsset(name + ".json")
	if err != nil {
		panic(err)
	}
	face := new(atlasFace)
	if err := json.Unmarshal(metadata, face); err != nil {
		panic(err)
	}
	data, err := readAsset(name + ".png")
	if err != nil {
		panic(err)
	}
	atlas, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	for _, g := range face.Glyphs {
		if g.W == 0 || g.H == 0 {
			continue
		}
		// A transparent border prevents neighbouring glyphs from bleeding
		// into the filtered edge. Mips preserve coverage in small captions.
		base := image.NewGray(image.Rect(0, 0, g.W+4, g.H+4))
		ab := atlas.Bounds()
		for ty := 0; ty < g.H; ty++ {
			for tx := 0; tx < g.W; tx++ {
				sx, sy := g.X+tx, g.Y+ty
				if sx < ab.Min.X || sx >= ab.Max.X || sy < ab.Min.Y || sy >= ab.Max.Y {
					continue
				}
				r, gg, bl, a := atlas.At(sx, sy).RGBA()
				if a == 0 {
					continue // 透明保持 0
				}
				// 纯黑描边（Python OUTLINE_COLOR=(0,0,0)）标记为 1，与透明(0)区分
				if r < 8*257 && gg < 8*257 && bl < 8*257 {
					base.Pix[(ty+2)*base.Stride+(tx+2)] = 1
					continue
				}
				// 正常字形：NTSC 灰度
				gray := uint8((int(r>>8)*299 + int(gg>>8)*587 + int(bl>>8)*114) / 1000)
				if gray == 0 {
					gray = 1 // 避免和透明撞
				}
				base.Pix[(ty+2)*base.Stride+(tx+2)] = gray
			}
		}
		g.Levels = append(g.Levels, base)
		for base.Bounds().Dx() > 8 && base.Bounds().Dy() > 8 {
			next := image.NewGray(image.Rect(0, 0, (base.Bounds().Dx()+1)/2, (base.Bounds().Dy()+1)/2))
			for y := 0; y < next.Bounds().Dy(); y++ {
				for x := 0; x < next.Bounds().Dx(); x++ {
					sum := 0
					for dy := 0; dy < 2; dy++ {
						for dx := 0; dx < 2; dx++ {
							sum += int(base.GrayAt(x*2+dx, y*2+dy).Y)
						}
					}
					next.Pix[y*next.Stride+x] = uint8((sum + 2) / 4)
				}
			}
			g.Levels = append(g.Levels, next)
			base = next
		}
	}
	return face
}

// filmFaces is the base pair every language loads: Latin and Cyrillic only.
var filmFaces = sync.OnceValue(func() map[string]*atlasFace {
	faces := make(map[string]*atlasFace, 2)
	for _, name := range []string{"display", "body"} {
		faces[name] = loadAtlasFace(name)
	}
	return faces
})
var otherFilms = sync.OnceValue(func() map[string]*atlasFace {
	faces := make(map[string]*atlasFace, 2)
	for _, name := range []string{"large", "medium", "normal", "small", "title", "tiny"} {
		faces[name] = loadAtlasFace(name)
	}
	return faces
})

// cjkEnabled gates the optional Chinese face. It is off until a language that
// needs it is selected, so a battle or a menu in English or Ukrainian never
// decodes the CJK atlases or pays for their mipmaps.
var cjkEnabled atomic.Bool

// cjkReady reports whether the Chinese atlases can be read. An embedded build
// has them the moment they are asked for; a browser build waits for its fetch.
var cjkReady atomic.Bool

// A failed optional download leaves the base faces usable. Another explicit
// language selection clears the failure and asks the host to retry.
var cjkFailure atomic.Pointer[error]

// startCJKFetch asks the host to deliver the Chinese atlases. The executable
// has nothing to do; the browser build replaces it with a fetch request.
var startCJKFetch = func() {}

// EnableCJK asks for the Chinese additions to both faces. An embedded build
// merges them on the next lookup; a browser build starts the fetch and merges
// them once the bytes arrive. Calling it more than once is free.
func EnableCJK() {
	if cjkEnabled.CompareAndSwap(false, true) {
		cjkFailure.Store(nil)
		startCJKFetch()
	}
}

// CJKReady reports whether the Chinese glyphs can be drawn yet. A host that has
// to fetch them is not ready the moment it is asked.
func CJKReady() bool { return cjkEnabled.Load() && cjkReady.Load() }

// CJKError reports an optional download failure, cleared by the next request.
func CJKError() error {
	if err := cjkFailure.Load(); err != nil {
		return *err
	}
	return nil
}

// cjkFaces is the base pair with the Chinese additions merged in, built once
// on first use. The CJK atlas carries every Glyph it draws, so a key already
// present in the base face keeps the base entry.
var cjkFaces = sync.OnceValue(func() map[string]*atlasFace {
	faces := make(map[string]*atlasFace)
	all := filmFaces()
	for _, name := range []string{"display", "body", "large", "medium", "normal", "small", "title", "tiny"} {
		var cjk *atlasFace
		if i18n.Language() != "english" {
			cjk = loadAtlasFace(i18n.Language() + "-" + name + "-cjk")
		} else {
			if name == "display" || name == "body" {
				cjk = all[name]
			} else {
				cjk = all["body"]
			}
		}
		base := all[name]
		switch {
		case cjk == nil && base == nil:
			fmt.Printf("WARN: face %q has neither cjk nor base atlas, skipped\n", name)
			continue
		case cjk == nil:
			// 只有基础面：直接用
			faces[name] = base
		case base == nil:
			// 只有 CJK 面：你的 chinese-*-cjk 已含完整字形集，直接当完整面用
			faces[name] = cjk
		default:
			merged := &atlasFace{
				Cap:     base.Cap,
				Glyphs:  make(map[string]*atlasGlyph, len(base.Glyphs)+len(cjk.Glyphs)),
				Kerning: base.Kerning,
			}
			for k, g := range base.Glyphs {
				merged.Glyphs[k] = g
			}
			for k, g := range cjk.Glyphs {
				if _, ok := merged.Glyphs[k]; !ok {
					merged.Glyphs[k] = g
				}
			}
			faces[name] = merged
		}
	}
	return faces
})

func TextFace(name string) *atlasFace {
	key := "display"
	if name == "body" {
		key = "body"
	}
	if CJKReady() {
		return cjkFaces()[key]
	}
	return filmFaces()[key]
}

// Unsupported characters retain a space's advance, preserving the existing
// script contract instead of collapsing the rest of the line.
func (f *atlasFace) Glyph(r rune) (*atlasGlyph, rune) {
	if g, ok := f.Glyphs[string(r)]; ok {
		return g, r
	}
	return f.Glyphs[" "], ' '
}

func init() {

}
