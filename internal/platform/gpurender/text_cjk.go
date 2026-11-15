package gpurender

import "github.com/nanolathe-gg/nanolathe/internal/film"

func measureCJKTierWidth(face *film.AtlasFace, tokens []struct {
	rn      rune
	forceNL bool
}) float32 {
	var w float32
	for _, tk := range tokens {
		if tk.forceNL {
			// 遇到强制换行，后面就不算宽度了（或者算到换行处，看你需求）
			break
		}
		if glyph, ok := face.Glyphs[string(tk.rn)]; ok {
			w += float32(glyph.Advance)
		} else {
			w += 8 // 找不到字默认宽度
		}
	}
	return w
}
