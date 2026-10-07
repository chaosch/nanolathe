package meshscene

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

type atlasRegion struct{ x, y, w, h int }

func (r atlasRegion) uv(corner [2]float32, width, height int) [2]float32 {
	return [2]float32{(float32(r.x) + 0.5 + corner[0]*float32(r.w-1)) / float32(width), (float32(r.y) + 0.5 + corner[1]*float32(r.h-1)) / float32(height)}
}

type atlasResult struct {
	texture           Texture
	byName            map[string]atlasRegion
	white             atlasRegion
	uvWidth, uvHeight int
	metadata          map[string]any
}
type textureRef struct {
	source                *formats.GAFSource
	path, entry, provider string
	frameCount            int
}
type atlasCell struct {
	key    string
	names  []string
	rgba   []byte
	region atlasRegion
}
type textureCensus struct {
	Name         string `json:"name"`
	Bank         string `json:"bank"`
	Provider     string `json:"provider"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SourceFrames int    `json:"source_frames"`
	Cell         int    `json:"deduplicated_cell"`
}

func canonicalTexture(s string) string { return strings.ToLower(s) }

// The prototype retains one immutable RGBA atlas, with a one-texel replicated
// border around every cell. Pixel-identical first frames share storage even
// when several 3DO faces or unit types use them (DESIGN_GPU_RENDERER §22).
func buildAtlas(fs *vfs.FS, models []*model.Model, pal *palette.Tables, scale int) (*atlasResult, error) {
	wanted := map[string]bool{}
	for _, m := range models {
		for _, p := range m.Pieces {
			for pri, face := range p.Primitives {
				if p.Selection && pri == 0 {
					continue
				}
				if face.IsColored == 0 && face.TextureName != "" {
					wanted[canonicalTexture(face.TextureName)] = true
				}
			}
		}
	}
	primary, logos := map[string]textureRef{}, map[string]textureRef{}
	seen := map[string]bool{}
	for _, info := range fs.Entries() {
		logical := strings.ToLower(info.Path)
		if !strings.HasPrefix(logical, "textures/") || !strings.HasSuffix(logical, ".gaf") || seen[logical] {
			continue
		}
		seen[logical] = true
		source, err := formats.LoadGAFSourceFile(fs, info.Path, 64<<20, formats.DefaultGAFLimits())
		if err != nil {
			return nil, fmt.Errorf("nanolathe: prototype texture bank failed: logical path %s, providers searched [%s], expected readable GAF: %w", info.Path, info.Source.ProviderID(), err)
		}
		target := primary
		if logical == "textures/logos.gaf" {
			target = logos
		}
		for _, entry := range source.Metadata().Entries {
			key := canonicalTexture(entry.Name)
			if !wanted[key] || len(entry.Frames) == 0 {
				continue
			}
			target[key] = textureRef{source: source, path: info.Path, entry: entry.Name, provider: info.Source.ProviderID(), frameCount: len(entry.Frames)}
		}
	}
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)
	cells := []atlasCell{{key: "white", rgba: []byte{255, 255, 255, 255}, region: atlasRegion{w: 1, h: 1}}}
	byDigest := map[[32]byte]int{}
	textureRows := []textureCensus{}
	missing := []string{}
	for _, name := range names {
		ref, ok := logos[name]
		if !ok {
			ref, ok = primary[name]
		}
		if !ok {
			missing = append(missing, name)
			continue
		}
		frame, err := ref.source.Frame(ref.entry, 0, 16<<20)
		if err != nil {
			return nil, err
		}
		w, h := int(frame.Width), int(frame.Height)
		if w == 0 || h == 0 {
			missing = append(missing, name)
			continue
		}
		rgba := make([]byte, w*h*4)
		for i, index := range frame.Pixels {
			if i >= w*h {
				break
			}
			c := pal.Base[index]
			copy(rgba[4*i:4*i+3], c[:3])
			if i >= len(frame.Transparent) || !frame.Transparent[i] {
				rgba[4*i+3] = 255
			}
		}
		digestBytes := make([]byte, 8+len(rgba))
		binary.LittleEndian.PutUint32(digestBytes, uint32(w))
		binary.LittleEndian.PutUint32(digestBytes[4:], uint32(h))
		copy(digestBytes[8:], rgba)
		hash := sha256.Sum256(digestBytes)
		cell, found := byDigest[hash]
		if !found {
			cell = len(cells)
			byDigest[hash] = cell
			cells = append(cells, atlasCell{key: name, rgba: rgba, region: atlasRegion{w: w, h: h}})
		}
		cells[cell].names = append(cells[cell].names, name)
		textureRows = append(textureRows, textureCensus{Name: name, Bank: ref.path, Provider: ref.provider, Width: w, Height: h, SourceFrames: ref.frameCount, Cell: cell})
	}
	order := make([]int, len(cells))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := cells[order[i]], cells[order[j]]
		if a.region.h != b.region.h {
			return a.region.h > b.region.h
		}
		return a.key < b.key
	})
	width := 512
	for _, c := range cells {
		for width < c.region.w+2 {
			width *= 2
		}
	}
	height := 0
	for {
		x, y, rowH := 0, 0, 0
		for _, ci := range order {
			r := &cells[ci].region
			if x+r.w+2 > width {
				x = 0
				y += rowH
				rowH = 0
			}
			r.x, r.y = x+1, y+1
			x += r.w + 2
			rowH = max(rowH, r.h+2)
		}
		height = y + rowH
		if height <= width || width >= 4096 {
			break
		}
		width *= 2
	}
	hPower := 1
	for hPower < height {
		hPower *= 2
	}
	height = hPower
	if width > 8192 || height > 8192 {
		return nil, fmt.Errorf("nanolathe: prototype atlas too large: logical path texture atlas, providers searched [GAF first frames], expected at most 8192x8192")
	}
	texture := Texture{Width: width, Height: height, RGBA: make([]byte, width*height*4)}
	result := &atlasResult{byName: map[string]atlasRegion{}, uvWidth: width, uvHeight: height}
	for ci, c := range cells {
		r := c.region
		for y := -1; y <= r.h; y++ {
			for x := -1; x <= r.w; x++ {
				sx, sy := min(max(x, 0), r.w-1), min(max(y, 0), r.h-1)
				copy(texture.RGBA[((r.y+y)*width+r.x+x)*4:], c.rgba[(sy*r.w+sx)*4:(sy*r.w+sx+1)*4])
			}
		}
		if ci == 0 {
			result.white = r
		}
		for _, name := range c.names {
			result.byName[name] = r
		}
	}
	if scale == 2 {
		larger := Texture{Width: width * 2, Height: height * 2, RGBA: make([]byte, width*height*16)}
		for y := 0; y < larger.Height; y++ {
			for x := 0; x < larger.Width; x++ {
				from := ((y/2)*width + x/2) * 4
				to := (y*larger.Width + x) * 4
				copy(larger.RGBA[to:to+4], texture.RGBA[from:from+4])
			}
		}
		texture = larger

	}
	result.texture = texture
	result.metadata = map[string]any{"width": texture.Width, "height": texture.Height, "rgba_bytes": len(texture.RGBA), "referenced_texture_names": len(names), "resolved_texture_names": len(textureRows), "unique_rgba_cells": len(cells) - 1, "missing_textures": missing, "textures": textureRows, "dimension_multiplier": scale, "policy": "deduplicated first GAF frames; logos precede default textures; one replicated border; 2x is nearest-neighbour enlargement, no authored extra detail"}
	return result, nil
}
