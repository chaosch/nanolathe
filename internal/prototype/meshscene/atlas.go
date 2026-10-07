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
	palette           Texture
	byName            map[string]atlasRegion
	white             atlasRegion
	uvWidth, uvHeight int
	metadata          map[string]any
	materials         []Material
	frames            [][4]float32
	materialByName    map[string]uint32
	useMaterials      bool
	byFrame           map[*formats.GAFFrame]atlasRegion
	framesByName      map[string][]*formats.GAFFrame
	whiteFrame        uint32
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
// border around every cell. Pixel-identical frames share storage even
// when several 3DO faces or unit types use them (DESIGN_GPU_RENDERER §22).
func buildAtlas(fs *vfs.FS, models []*model.Model, pal *palette.Tables, scale int, materials *ModelMaterialSource) (*atlasResult, error) {
	wanted := map[string]bool{}
	for _, m := range models {
		for _, p := range m.Pieces {
			for pri, face := range p.Primitives {
				if p.Selection && pri == 0 {
					continue
				}
				if face.IsColored&1 == 0 && face.TextureName != "" {
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
	cells := []atlasCell{{key: "white", rgba: []byte{0, 255, 0, 255}, region: atlasRegion{w: 1, h: 1}}}
	byDigest := map[[32]byte]int{}
	textureRows := []textureCensus{}
	frameCells := map[string][]int{}
	framePointers := map[*formats.GAFFrame]int{}
	framesByName := map[string][]*formats.GAFFrame{}
	teamNames := map[string]bool{}
	missing := []string{}
	for _, name := range names {
		ref, ok := primary[name]
		if !ok {
			ref, ok = logos[name]
		}
		if !ok {
			missing = append(missing, name)
			continue
		}
		count := ref.frameCount
		if strings.EqualFold(ref.path, "textures/logos.gaf") && ref.frameCount == 10 {
			teamNames[name] = true
		}
		for fi := 0; fi < count; fi++ {
			frame, err := ref.source.Frame(ref.entry, fi, 16<<20)
			if err != nil {
				return nil, err
			}
			w, h := int(frame.Width), int(frame.Height)
			if w == 0 || h == 0 {
				return nil, fmt.Errorf("nanolathe: prototype texture frame empty: logical path %s/%s, providers searched [%s], expected nonempty frame %d", ref.path, ref.entry, ref.provider, fi)
			}
			rgba := modelFrameRGBA(frame, pal)
			digestBytes := make([]byte, 8+len(rgba))
			binary.LittleEndian.PutUint32(digestBytes, uint32(w))
			binary.LittleEndian.PutUint32(digestBytes[4:], uint32(h))
			copy(digestBytes[8:], rgba)
			hash := sha256.Sum256(digestBytes)
			cell, found := byDigest[hash]
			if !found {
				cell = len(cells)
				byDigest[hash] = cell
				cells = append(cells, atlasCell{key: fmt.Sprintf("%s/%d", name, fi), rgba: rgba, region: atlasRegion{w: w, h: h}})
			}
			frameCells[name] = append(frameCells[name], cell)
			framePointers[frame] = cell
			framesByName[name] = append(framesByName[name], frame)
			if fi == 0 {
				cells[cell].names = append(cells[cell].names, name)
				textureRows = append(textureRows, textureCensus{Name: name, Bank: ref.path, Provider: ref.provider, Width: w, Height: h, SourceFrames: ref.frameCount, Cell: cell})
			}
		}
	}
	if materials != nil {
		for fi, frame := range materials.Frames {
			if frame == nil || frame.Width == 0 || frame.Height == 0 {
				continue
			}
			if _, exists := framePointers[frame]; exists {
				continue
			}
			w, h := int(frame.Width), int(frame.Height)
			if len(frame.Pixels) < w*h {
				return nil, fmt.Errorf("nanolathe: retained material pixels unavailable: logical path model material frame %d, providers searched [production material source], expected complete indexed raster", fi)
			}
			rgba := modelFrameRGBA(frame, pal)
			digest := make([]byte, 8+len(rgba))
			binary.LittleEndian.PutUint32(digest, uint32(w))
			binary.LittleEndian.PutUint32(digest[4:], uint32(h))
			copy(digest[8:], rgba)
			hash := sha256.Sum256(digest)
			cell, found := byDigest[hash]
			if !found {
				cell = len(cells)
				byDigest[hash] = cell
				cells = append(cells, atlasCell{key: fmt.Sprintf("prepared/%d", fi), rgba: rgba, region: atlasRegion{w: w, h: h}})
			}
			framePointers[frame] = cell
		}
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
		return nil, fmt.Errorf("nanolathe: prototype atlas too large: logical path texture atlas, providers searched [GAF model and prepared material frames], expected at most 8192x8192")
	}
	texture := Texture{Width: width, Height: height, RGBA: make([]byte, width*height*4)}
	result := &atlasResult{byName: map[string]atlasRegion{}, uvWidth: width, uvHeight: height, materialByName: map[string]uint32{}, materials: []Material{{}}, byFrame: map[*formats.GAFFrame]atlasRegion{}, framesByName: framesByName}
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
	for frame, cell := range framePointers {
		result.byFrame[frame] = cells[cell].region
	}
	// Flat primitives use the same per-primitive material slot as textures.
	a, b := result.white.uv([2]float32{}, width, height), result.white.uv([2]float32{1, 1}, width, height)
	result.whiteFrame = uint32(len(result.frames))
	result.frames = append(result.frames, [4]float32{a[0], a[1], b[0], b[1]})
	for _, name := range names {
		bindings := frameCells[name]
		if len(bindings) == 0 {
			continue
		}
		kind := uint32(0)
		if teamNames[name] {
			kind = 1
		}
		result.materialByName[name] = uint32(len(result.materials))
		result.materials = append(result.materials, Material{FirstFrame: uint32(len(result.frames)), FrameCount: uint32(len(bindings)), Kind: kind})
		for _, ci := range bindings {
			r := cells[ci].region
			a, b := r.uv([2]float32{}, width, height), r.uv([2]float32{1, 1}, width, height)
			result.frames = append(result.frames, [4]float32{a[0], a[1], b[0], b[1]})
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
	result.palette = Texture{Width: 256, Height: 2, RGBA: make([]byte, 256*2*4)}
	for i := 0; i < 256; i++ {
		a, b := pal.Base[i], pal.Base[pal.Blue[i]]
		copy(result.palette.RGBA[i*4:], a[:])
		copy(result.palette.RGBA[(256+i)*4:], b[:])
		result.palette.RGBA[i*4+3] = 255
		result.palette.RGBA[(256+i)*4+3] = 255
	}
	result.metadata = map[string]any{"width": texture.Width, "height": texture.Height, "rgba_bytes": len(texture.RGBA), "referenced_texture_names": len(names), "resolved_texture_names": len(textureRows), "unique_rgba_cells": len(cells) - 1, "missing_textures": missing, "textures": textureRows, "dimension_multiplier": scale, "policy": "deduplicated model GAF and prepared skin frames; primary textures precede fallback logos; exactly ten fallback logo frames select team; one replicated border; 2x is nearest-neighbour enlargement, no authored extra detail"}
	return result, nil
}
