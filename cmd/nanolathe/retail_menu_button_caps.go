package main

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
)

func retailGAFGlyphDispatch(font *formats.GAFEntry, r rune) *formats.GAFFrame {
	if i18n.I18nLanguageNeedCJK() && i18n.HasCJK(string(r)) {
		return retailGAFGlyphCJK(font, r)
	}
	return retailGAFGlyph(font, byte(r))
}

func retailGAFGlyphCJK(font *formats.GAFEntry, r rune) *formats.GAFFrame {
	const (
		padLeft   = 0
		padRight  = 0
		padTop    = 0
		padBottom = 0
	)

	availHeight := retailGAFTextHeight(font) - padTop - padBottom
	tier := client.PickTier(availHeight)
	face := film.TextFaceInGame(tier.Name)
	if face == nil {
		return nil
	}
	g := face.Glyphs[string(r)]
	if g == nil {
		return nil
	}
	f := g.Frame()
	f.Width = uint16(g.Advance)
	f.YOffset = int16(tier.Px)
	return f
}
