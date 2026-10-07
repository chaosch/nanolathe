package meshscene

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/effects"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

type spriteArt struct {
	cell, width, height, xoff, yoff int
	hold                            int32
	hue                             [3]float32
	identity                        *formats.GAFFrame
}
type spriteSequence struct{ frames []spriteArt }
type spriteCell struct {
	region atlasRegion
	rgba   []byte
}

// NewBattleSprites resolves immutable art before the measured loop. The bank
// set follows content's SimArt requests; feature bodies/shadows and all event
// frames are prepared from authored names [05 R-FEAT-01 §10][06 R-WFX-01 §1].
// Animated rest features deliberately retain their first frame in this
// experiment; the production rest cursor is presentation-owned [03 §4.4].
func NewBattleSprites(fs *vfs.FS, pal *palette.Tables, cat *content.Catalog) (*BattleSprites, error) {
	if fs == nil || pal == nil || cat == nil {
		return nil, fmt.Errorf("nanolathe: prototype sprite preparation failed: logical path battle sprites, providers searched [arguments], expected VFS, palette and catalog")
	}
	b := &BattleSprites{sequences: make(map[string]*spriteSequence), missing: make(map[string]string), counts: make(map[string]int), previousEffects: make(map[uint64][4]float32), previousProjectiles: make(map[uint64][4]float32), previousBeams: make(map[uint64][4]float32)}
	for i, c := range pal.Base {
		b.colors[i] = [4]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255, 1}
	}
	b.logical = pal.Logical
	for i := range b.flashSides {
		b.flashSides[i] = effects.FlashTableSides(i)
	}
	// nil means all entries/all frames of a default or weapon effect bank.
	requests := map[string]map[string]bool{content.DefaultEffectBank: nil}
	for _, w := range cat.Weapons {
		if w == nil {
			continue
		}
		for _, bank := range []string{w.ExplosionGaf, w.WaterExplosionGaf, w.LavaExplosionGaf} {
			if bank = content.CanonicalKey(bank); bank != "" {
				requests[bank] = nil
			}
		}
	}
	for _, f := range cat.Features {
		if f == nil || f.Filename == "" {
			continue
		}
		bank := content.CanonicalKey(f.Filename)
		if req, exists := requests[bank]; exists && req == nil {
			continue
		}
		if requests[bank] == nil {
			requests[bank] = make(map[string]bool)
		}
		for _, name := range []string{f.SeqName, f.SeqNameShad} {
			if name = content.CanonicalKey(name); name != "" {
				if _, exists := requests[bank][name]; !exists {
					requests[bank][name] = false
				}
			}
		}
		for _, name := range []string{f.SeqNameBurn, f.SeqNameBurnShad, f.SeqNameDie, f.SeqNameDieShad, f.SeqNameReclamate, f.SeqNameReclamateShad} {
			if name = content.CanonicalKey(name); name != "" {
				requests[bank][name] = true
			}
		}
	}
	cells := []spriteCell{{region: atlasRegion{w: 1, h: 1}, rgba: []byte{255, 255, 255, 255}}}
	digests := make(map[[32]byte]int)
	banks := make([]string, 0, len(requests))
	for bank := range requests {
		banks = append(banks, bank)
	}
	sort.Strings(banks)
	for _, bank := range banks {
		path := content.EffectBankPath(bank)
		source, err := formats.LoadGAFSourceFile(fs, path, content.EffectBankMaxBytes, content.EffectBankGAFLimits())
		if err != nil {
			b.missing[path] = err.Error()
			continue
		}
		b.banksLoaded++
		entries := make([]string, 0)
		if requests[bank] == nil {
			for _, e := range source.Metadata().Entries {
				entries = append(entries, content.CanonicalKey(e.Name))
			}
		} else {
			for entry := range requests[bank] {
				entries = append(entries, entry)
			}
		}
		sort.Strings(entries)
		for _, name := range entries {
			key := spriteSequenceKey(bank, name)
			if _, exists := b.sequences[key]; exists {
				continue // GAF Find preserves first matching authored entry.
			}
			meta, ok := source.Metadata().Find(name)
			if !ok || len(meta.Frames) == 0 {
				b.missing[path+"#"+name] = "entry missing or empty"
				continue
			}
			count := len(meta.Frames)
			if requests[bank] != nil && !requests[bank][name] {
				count = 1
			}
			seq := &spriteSequence{frames: make([]spriteArt, count)}
			for i := 0; i < count; i++ {
				f, err := source.Frame(name, i, 32<<20)
				if err != nil || f == nil || f.Width == 0 || f.Height == 0 {
					reason := "empty frame"
					if err != nil {
						reason = err.Error()
					}
					b.missing[fmt.Sprintf("%s#%s[%d]", path, name, i)] = reason
					continue
				}
				w, h := int(f.Width), int(f.Height)
				rgba := make([]byte, w*h*4)
				var hue [3]float32
				var weight float32
				for pixel := 0; pixel < w*h; pixel++ {
					if pixel >= len(f.Pixels) || pixel >= len(f.Transparent) || f.Transparent[pixel] {
						continue
					}
					c := pal.Base[f.Pixels[pixel]]
					copy(rgba[pixel*4:pixel*4+3], c[:3])
					rgba[pixel*4+3] = 255
					brightness := float32(max(c[0], max(c[1], c[2]))) / 255
					weight += brightness
					for channel := 0; channel < 3; channel++ {
						hue[channel] += float32(c[channel]) / 255 * brightness
					}
				}
				if weight > 0 {
					for channel := range hue {
						hue[channel] /= weight
					}
				}
				b.alternateChildrenOmitted += spriteAlternateChildren(f)
				// PlainPixels already contains the ordinary composite graph. ALP
				// children need destination-index access absent from this RGBA host.
				// TODO(question): port destination-dependent GAF ALP children to the
				// native composite; until then decoded plain children alone draw [fmt gaf].
				header := make([]byte, 8)
				binary.LittleEndian.PutUint32(header, uint32(w))
				binary.LittleEndian.PutUint32(header[4:], uint32(h))
				digest := sha256.New()
				digest.Write(header)
				digest.Write(rgba)
				var hash [32]byte
				copy(hash[:], digest.Sum(nil))
				cell, exists := digests[hash]
				if !exists {
					cell = len(cells)
					digests[hash] = cell
					cells = append(cells, spriteCell{region: atlasRegion{w: w, h: h}, rgba: rgba})
				}
				seq.frames[i] = spriteArt{cell: cell, width: w, height: h, xoff: int(f.XOffset), yoff: int(f.YOffset), hold: max(int32(meta.Frames[i].Value), 1), hue: hue, identity: &formats.GAFFrame{Width: f.Width, Height: f.Height, XOffset: f.XOffset, YOffset: f.YOffset}}
				b.framesLoaded++
			}
			b.sequences[key] = seq
		}
	}
	if err := b.packSprites(cells); err != nil {
		return nil, err
	}
	return b, nil
}

func spriteAlternateChildren(f *formats.GAFFrame) int {
	n := 0
	for _, child := range f.Subframes {
		if child.AlternateBlitter != 0 {
			n++
		}
		n += spriteAlternateChildren(child)
	}
	return n
}

func spriteSequenceKey(bank, entry string) string {
	return content.CanonicalKey(bank) + "|" + content.CanonicalKey(entry)
}

func (b *BattleSprites) packSprites(cells []spriteCell) error {
	order := make([]int, len(cells))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return cells[order[i]].region.h > cells[order[j]].region.h })
	width, height := 2048, 0
	for _, cell := range cells {
		for width < cell.region.w+4 {
			width *= 2
		}
	}
	for {
		x, y, rowHeight := 0, 0, 0
		for _, i := range order {
			r := &cells[i].region
			if x+r.w+4 > width {
				x, y, rowHeight = 0, y+rowHeight, 0
			}
			r.x, r.y = x+2, y+2
			x += r.w + 4
			rowHeight = max(rowHeight, r.h+4)
		}
		height = y + rowHeight
		if height <= width || width >= 8192 {
			break
		}
		width *= 2
	}
	if width > 8192 || height > 8192 {
		return fmt.Errorf("nanolathe: prototype sprite atlas too large: logical path sprite atlas, providers searched [prepared animation banks], expected at most 8192x8192")
	}
	b.texture = Texture{Width: width, Height: height, RGBA: make([]byte, width*height*4)}
	b.regions = make([]atlasRegion, len(cells))
	for i, cell := range cells {
		r := cell.region
		b.regions[i] = r
		// Two replicated gutters; no premultiplication before the shader.
		for y := -2; y < r.h+2; y++ {
			for x := -2; x < r.w+2; x++ {
				sx, sy := min(max(x, 0), r.w-1), min(max(y, 0), r.h-1)
				copy(b.texture.RGBA[((r.y+y)*width+r.x+x)*4:], cell.rgba[(sy*r.w+sx)*4:(sy*r.w+sx+1)*4])
			}
		}
	}
	return nil
}
