package drawlist

import "github.com/nanolathe-gg/nanolathe/internal/film"

// 在 drawlist 包里
//
//	type GlyphAtlas interface {
//		// 定义 GPU Sink 查询字形所需的方法，例如获取纹理和位置
//		GetGlyph(r rune) (texture interface{}, advance int32, w, h, left, top int32)
//		GetLineHeight() int32
//	}
//
// 辅助函数：根据图集估算文本宽度
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
