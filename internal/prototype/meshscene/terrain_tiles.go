package meshscene

import (
	"fmt"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

// TerrainTiles retains one cell per referenced TNT tile, in first map-use
// order. Lookup is a row-major R32Uint image of atlas cell indices. The atlas
// stores native 32x32 tiles; Detail, when present, stores 64x64 detail tiles
// (or the production nearest-doubled fallback for a missing detail tile).
// Palette expansion is load-time only. Every texel is opaque, as in the terrain
// palette expansion; material transparency rules do not apply to painted TNT.
// Rect is the complete authored raster, including non-playable map borders.
// Sampling clamps within the selected tile, not into its atlas neighbor
// (DESIGN_GPU_RENDERER §14.2, §16.3, §16.7).
type TerrainTiles struct {
	Atlas, Detail Texture
	Width, Height int // tile-map dimensions, not world or atlas pixels
	Lookup        []uint32
	TileCount     int
	Rect          [4]float32
}

// BuildTerrainTiles preserves the original TNT pixels without baking a map-sized
// photograph or downsampling. Callers may replace it with BuildBattleTerrainTiles
// using the production display palette and optional detail art before upload.
func BuildTerrainTiles(t *formats.TNT, display [256][4]byte) (*TerrainTiles, error) {
	if t == nil || len(t.TileGraphics)%1024 != 0 {
		return nil, fmt.Errorf("nanolathe: retained terrain tiles invalid: logical path maps, providers searched [TNT], expected complete 32x32 tile pixels")
	}
	return buildTerrainTiles(int(t.TileMapWidth), int(t.TileMapHeight), t.TileIndices, len(t.TileGraphics)/1024, func(id int) []byte { return t.TileGraphics[id*1024 : (id+1)*1024] }, nil, display)
}

// BuildBattleTerrainTiles accepts Client.BattleTerrainSources without recording
// a frame or advancing presentation state. Both sources and display are borrowed
// only for this load-time call; the result owns all native upload bytes.
func BuildBattleTerrainTiles(source drawlist.Terrain, display [256][4]byte) (*TerrainTiles, error) {
	t := source.Terrain
	if t == nil || t.CellW%2 != 0 || t.CellH%2 != 0 {
		return nil, fmt.Errorf("nanolathe: retained terrain tiles invalid: logical path maps, providers searched [battle terrain], expected complete tile grid")
	}
	return buildTerrainTiles(int(t.CellW/2), int(t.CellH/2), t.TileIndices, len(t.TileSet), func(id int) []byte { return t.TileSet[id][:] }, source.Detail, display)
}

func buildTerrainTiles(w, h int, indices []uint16, count int, tile func(int) []byte, detail [][drawlist.DetailTilePixels]byte, display [256][4]byte) (*TerrainTiles, error) {
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 || len(indices) != w*h || count <= 0 {
		return nil, fmt.Errorf("nanolathe: retained terrain tiles invalid: logical path maps, providers searched [tile grid], expected validated TNT dimensions and tile set")
	}
	out := &TerrainTiles{Width: w, Height: h, Lookup: make([]uint32, len(indices)), Rect: [4]float32{0, 0, float32(w * 32), float32(h * 32)}}
	remap := make([]int, count)
	for i := range remap {
		remap[i] = -1
	}
	used := make([]int, 0, min(count, len(indices)))
	for i, id := range indices {
		if int(id) >= count {
			return nil, fmt.Errorf("nanolathe: retained terrain tile missing: logical path maps, providers searched [tile set], expected tile %d", id)
		}
		cell := remap[id]
		if cell < 0 {
			cell = len(used)
			remap[id] = cell
			used = append(used, int(id))
		}
		out.Lookup[i] = uint32(cell)
	}
	out.TileCount = len(used)
	cols := 1
	for cols*cols < len(used) {
		cols++
	}
	rows := (len(used) + cols - 1) / cols
	out.Atlas = Texture{Width: cols * 32, Height: rows * 32, RGBA: make([]byte, cols*rows*32*32*4)}
	if len(detail) > 0 {
		out.Detail = Texture{Width: cols * 64, Height: rows * 64, RGBA: make([]byte, cols*rows*64*64*4)}
	}
	for cell, id := range used {
		native := tile(id)
		if len(native) != 1024 {
			return nil, fmt.Errorf("nanolathe: retained terrain tile invalid: logical path maps, providers searched [tile set], expected 1024 pixels")
		}
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				at := (((cell/cols)*32+y)*out.Atlas.Width + (cell%cols)*32 + x) * 4
				color := display[native[y*32+x]]
				copy(out.Atlas.RGBA[at:at+3], color[:3])
				out.Atlas.RGBA[at+3] = 255
			}
		}
		if len(detail) == 0 {
			continue
		}
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				index := native[(y/2)*32+x/2]
				if id < len(detail) {
					index = detail[id][y*64+x]
				}
				at := (((cell/cols)*64+y)*out.Detail.Width + (cell%cols)*64 + x) * 4
				color := display[index]
				copy(out.Detail.RGBA[at:at+3], color[:3])
				out.Detail.RGBA[at+3] = 255
			}
		}
	}
	return out, nil
}
