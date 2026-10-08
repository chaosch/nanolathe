package meshscene

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

type spriteArt struct {
	cell, width, height, xoff, yoff int
	hold                            int32
	hue                             [3]float32
	emission                        [3]float32
	identity                        *formats.GAFFrame
}
type spriteSequence struct {
	frames []spriteArt
	extent float32
	cycle  uint64
}
type spriteSequenceName struct{ bank, entry string }
type spriteCell struct {
	region atlasRegion
	rgba   []byte
}

type spriteBankCensus struct {
	Path                                       string `json:"path"`
	Entries, Frames, UniqueCells, UniquePixels int
	MaxWidth, MaxHeight                        int
	RawPixels, CroppedPixels                   int64
	FullyTransparentFrames                     int
	MaxCroppedWidth, MaxCroppedHeight          int
}

// NewBattleSprites resolves immutable art before the measured loop. The bank
// set follows content's SimArt requests; feature bodies/shadows and all event
// frames are prepared from authored names [05 R-FEAT-01 §10][06 R-WFX-01 §1].
// Rest animation samples the definition-shared authored cycle [03 §4.4].
// When featureNames are provided, only those placed feature roots plus every
// battle-catalog unit corpse and their authored successor closure are loaded.
// All catalog units cover future factory products without predicting gameplay.
// No arguments preserves the unrestricted catalog diagnostic; an empty string
// argument requests corpse-only preparation for a map with no placed features.
func NewBattleSprites(fs *vfs.FS, pal *palette.Tables, cat *content.Catalog, featureNames ...string) (*BattleSprites, error) {
	if fs == nil || pal == nil || cat == nil {
		return nil, fmt.Errorf("nanolathe: prototype sprite preparation failed: logical path battle sprites, providers searched [arguments], expected VFS, palette and catalog")
	}
	b := &BattleSprites{effects: drawlist.AllEffects(), previousFeatures: make(map[uint64][4]float32), sequences: make(map[string]*spriteSequence), sequenceLookups: make(map[spriteSequenceName]*spriteSequence), missing: make(map[string]string), counts: make(map[string]int), previousEffects: make(map[uint64][4]float32), previousProjectiles: make(map[uint64][4]float32), previousBeams: make(map[uint64][4]float32)}
	for i, c := range pal.Base {
		b.colors[i] = [4]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255, 1}
	}
	b.logical = pal.Logical
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
	features := b.spriteFeatureScope(cat, featureNames)
	for _, f := range features {
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
					requests[bank][name] = f.Animating != 0
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
		census := spriteBankCensus{Path: path}
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
		census.Entries = len(entries)
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
				census.MaxWidth, census.MaxHeight = max(census.MaxWidth, w), max(census.MaxHeight, h)
				rgba := spriteFrameRGBA(f, pal)
				b.alternateChildrenPrepared += spriteAlternateChildren(f)
				hue, emission := spriteEmission(rgba)
				seq.extent = max(seq.extent, float32(max(f.Width, f.Height)))
				seq.cycle += uint64(max(meta.Frames[i].Value, 1))
				b.rawSpritePixels += int64(w) * int64(h)
				census.RawPixels += int64(w) * int64(h)
				var minX, minY int
				var empty bool
				rgba, w, h, minX, minY, empty = cropSpriteMargins(rgba, w, h)
				census.MaxCroppedWidth, census.MaxCroppedHeight = max(census.MaxCroppedWidth, w), max(census.MaxCroppedHeight, h)
				b.croppedSpritePixels += int64(w) * int64(h)
				census.CroppedPixels += int64(w) * int64(h)
				if empty {
					b.fullyTransparentFrames++
					census.FullyTransparentFrames++
				}
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
					census.UniqueCells++
					census.UniquePixels += w * h
				}
				// Top-left after cropping is anchor-oldOffset+min: subtract min
				// from the offset so every retained texel keeps its authored point.
				// The dispatch identity keeps the original authored canvas metadata.
				seq.frames[i] = spriteArt{cell: cell, width: w, height: h, xoff: int(f.XOffset) - minX, yoff: int(f.YOffset) - minY, hold: max(int32(meta.Frames[i].Value), 1), hue: hue, emission: emission, identity: &formats.GAFFrame{Width: f.Width, Height: f.Height, XOffset: f.XOffset, YOffset: f.YOffset}}
				a := seq.frames[i]
				b.artReach = max(b.artReach, float32(max(a.xoff, -a.xoff, a.width-a.xoff, a.xoff-a.width, a.yoff, -a.yoff, a.height-a.yoff, a.yoff-a.height)))
				b.framesLoaded++
				census.Frames++
			}
			b.sequences[key] = seq
		}
		b.bankCensus = append(b.bankCensus, census)
	}
	if err := b.packSprites(cells); err != nil {
		return nil, err
	}
	return b, nil
}

// cropSpriteMargins preserves every nonzero-alpha texel verbatim, with one
// source-transparent pixel of margin wherever the canvas provides it. This
// keeps the linear sampler's edge coverage and transparent atlas gutters;
// position/art is preserved, not bit-exact filtered output under centre UVs.
// A wholly transparent frame retains a transparent 1x1 raster, its slot and
// hold: it must not disappear from animation timing or dispatch counts.
func cropSpriteMargins(rgba []byte, width, height int) (pixels []byte, w, h, minX, minY int, empty bool) {
	minX, minY = width, height
	maxX, maxY := -1, -1
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if rgba[(y*width+x)*4+3] == 0 {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < 0 {
		return []byte{0, 0, 0, 0}, 1, 1, 0, 0, true
	}
	minX, minY = max(minX-1, 0), max(minY-1, 0)
	maxX, maxY = min(maxX+1, width-1), min(maxY+1, height-1)
	w, h = maxX-minX+1, maxY-minY+1
	if w == width && h == height {
		return rgba, w, h, 0, 0, false
	}
	pixels = make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		source := ((minY+y)*width + minX) * 4
		copy(pixels[y*w*4:(y+1)*w*4], rgba[source:source+w*4])
	}
	return pixels, w, h, minX, minY, false
}

func (b *BattleSprites) spriteFeatureScope(cat *content.Catalog, names []string) []*content.FeatureDef {
	selected := make(map[*content.FeatureDef]bool)
	var missing []string
	var visit func(*content.FeatureDef)
	var named func(string)
	named = func(name string) {
		if key := content.CanonicalKey(name); key != "" {
			if f := cat.Features[key]; f != nil {
				visit(f)
			} else {
				missing = append(missing, key)
			}
		}
	}
	visit = func(f *content.FeatureDef) {
		if f == nil || selected[f] {
			return
		}
		selected[f] = true
		for _, next := range []struct {
			f    *content.FeatureDef
			name string
		}{{f.FeatureDeadDef, f.FeatureDead}, {f.FeatureBurntDef, f.FeatureBurnt}, {f.FeatureReclamateDef, f.FeatureReclamate}} {
			if next.f != nil {
				visit(next.f)
			} else {
				named(next.name)
			}
		}
	}
	if len(names) == 0 {
		for _, f := range cat.Features {
			visit(f)
		}
	} else {
		b.featureScopeFiltered = true
		for _, name := range names {
			named(name)
		}
		// Every unit in the battle catalog can be a factory product or a
		// subsequent resurrection; no live allocator reads are needed.
		for _, u := range cat.Units {
			if u != nil {
				named(u.Corpse)
			}
		}
	}
	out := make([]*content.FeatureDef, 0, len(selected))
	for f := range selected {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CanonicalKey < out[j].CanonicalKey })
	for _, f := range out {
		b.preparedFeatureNames = append(b.preparedFeatureNames, f.CanonicalKey)
	}
	sort.Strings(missing)
	for _, name := range missing {
		if len(b.unresolvedFeatureNames) == 0 || b.unresolvedFeatureNames[len(b.unresolvedFeatureNames)-1] != name {
			b.unresolvedFeatureNames = append(b.unresolvedFeatureNames, name)
		}
	}
	return out
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

// Art is complete before worker lookups begin, so missing resolutions can also
// be retained. Raw names avoid repeating normalization and string concatenation
// for every visible feature; callers still own missing-art diagnostics.
func (b *BattleSprites) sequence(bank, entry string) *spriteSequence {
	name := spriteSequenceName{bank, entry}
	if seq, ok := b.sequenceLookups[name]; ok {
		return seq
	}
	seq := b.sequences[spriteSequenceKey(bank, entry)]
	if b.sequenceLookups == nil {
		b.sequenceLookups = make(map[spriteSequenceName]*spriteSequence)
	}
	b.sequenceLookups[name] = seq
	return seq
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
		largest := append([]spriteBankCensus(nil), b.bankCensus...)
		sort.SliceStable(largest, func(i, j int) bool { return largest[i].UniquePixels > largest[j].UniquePixels })
		var descriptions []string
		for _, bank := range largest[:min(len(largest), 3)] {
			descriptions = append(descriptions, fmt.Sprintf("%s=%d pixels", bank.Path, bank.UniquePixels))
		}
		return fmt.Errorf("nanolathe: prototype sprite atlas too large: logical path sprite atlas, providers searched [prepared animation banks], expected at most 8192x8192, prepared %d cells from %d feature definitions; largest banks [%s]", len(cells), len(b.preparedFeatureNames), strings.Join(descriptions, ", "))
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
