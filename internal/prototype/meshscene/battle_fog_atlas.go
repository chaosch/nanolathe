package meshscene

import (
	"fmt"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
)

// These dimensions match the production fog compositor's ordered 2x2 cell
// neighbourhood (DESIGN_GPU_RENDERER §11.2, C-G7). Each tile is relative to the
// cell origin, so a frame may paint beyond its own 32-pixel cell [03 §3.3].
const (
	battleFogTile     = 64
	battleFogColumns  = 14
	battleFogVariants = 4
	battleFogSlots    = battleFogColumns * battleFogVariants
)

// Missing and degenerate frames leave the destination unchanged. Gray-family
// compression is checked before composition, matching its consumer's raw-frame
// gate [03 R-COMP-01 §2]. Black frames retain their plain keyed-copy behavior.
func battleFogFrame(entry *formats.GAFEntry, index int, gray bool) *formats.GAFFrame {
	if entry == nil || index < 0 || index >= len(entry.Frames) {
		return nil
	}
	f := entry.Frames[index].Frame
	if f == nil || gray && f.Compressed != 0 {
		return nil
	}
	return f
}

func battleFogFramePresent(entry *formats.GAFEntry, index int, gray bool) bool {
	f := battleFogFrame(entry, index, gray)
	return f != nil && f.Width != 0 && f.Height != 0
}

func battleFogAtlas(gray, black [4]*formats.GAFEntry, pal *palette.Tables) (Texture, error) {
	w, h := battleFogColumns*battleFogTile, 2*battleFogVariants*battleFogTile+1
	atlas := Texture{Width: w, Height: h, RGBA: make([]byte, w*h*4)}
	families := [2][4]*formats.GAFEntry{gray, black}
	for family := range families {
		for variant, entry := range families[family] {
			for index := 0; index < battleFogColumns; index++ {
				f := battleFogFrame(entry, index, family == 0)
				if f == nil {
					continue
				}
				// Production uses an ordered leaf path for these cases. This
				// prototype has only the native atlas path and must reject them.
				if len(f.Subframes) != 0 || f.SubframeCount != 0 {
					return Texture{}, fmt.Errorf("nanolathe: unsupported Metal prototype fog composite: logical path anims/fog.gaf, providers searched [], expected leaf frame in entry %s frame %d", entry.Name, index)
				}
				if f.Width == 0 || f.Height == 0 {
					continue
				}
				ox, oy := -int(f.XOffset), -int(f.YOffset)
				fw, fh := int(f.Width), int(f.Height)
				if ox < 0 || oy < 0 || ox+fw > battleFogTile || oy+fh > battleFogTile {
					return Texture{}, fmt.Errorf("nanolathe: unsupported Metal prototype fog extent: logical path anims/fog.gaf, providers searched [], expected frame within a %d-pixel cell neighbourhood; entry %s frame %d is %dx%d at (%d,%d)", battleFogTile, entry.Name, index, fw, fh, ox, oy)
				}
				baseX := index*battleFogTile + ox
				baseY := (family*battleFogVariants+variant)*battleFogTile + oy
				for y := 0; y < fh; y++ {
					for x := 0; x < fw; x++ {
						src := y*fw + x
						if src >= len(f.Pixels) {
							continue
						}
						at := ((baseY+y)*w + baseX + x) * 4
						// Fog black copies expand the authored physical index directly;
						// gray uses only coverage [03 §4.3][R-RR16-A §8].
						atlas.RGBA[at], atlas.RGBA[at+1], atlas.RGBA[at+2], _ = pal.RGBA(f.Pixels[src])
						if src >= len(f.Transparent) || !f.Transparent[src] {
							atlas.RGBA[at+3] = 255
						}
					}
				}
			}
		}
	}
	// The final row supplies all palette entries, including the solid/checker
	// dark colour at render.FogDarkPaletteIndex. It is outside every mask tile.
	for index := 0; index < 256; index++ {
		at := ((h-1)*w + index) * 4
		atlas.RGBA[at], atlas.RGBA[at+1], atlas.RGBA[at+2], atlas.RGBA[at+3] = pal.RGBA(byte(index))
	}
	return atlas, nil
}
