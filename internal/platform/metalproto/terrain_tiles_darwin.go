//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// TerrainTilesShaderSource precedes terrain_fragment. The bridge includes
// terrain_tiles.h/.inc and uploads immutable scene sources before rendering.
// Bind selects native/detail and nearest/fractional filtering; the host keeps
// the existing standalone Terrain texture path when no tile source is supplied.
//
//go:embed native/terrain_tiles.metal
var TerrainTilesShaderSource string

type nativeTerrainTilesUpload struct {
	Base, Detail, Lookup                                                  uintptr
	BaseWidth, BaseHeight, DetailWidth, DetailHeight, MapWidth, MapHeight uint32
}

// packTerrainTiles borrows scene-owned bytes only through synchronous native
// Prepare, which copies them to retained textures. There is no per-frame walk.
func packTerrainTiles(t *meshscene.TerrainTiles) (nativeTerrainTilesUpload, error) {
	var out nativeTerrainTilesUpload
	if unsafe.Sizeof(out) != 48 {
		return out, fmt.Errorf("metalproto: terrain tiles ABI mismatch")
	}
	if t == nil {
		return out, nil
	}
	if t.Width <= 0 || t.Height <= 0 || t.Width > 4096 || t.Height > 4096 || len(t.Lookup) != t.Width*t.Height || t.Atlas.Width <= 0 || t.Atlas.Height <= 0 || t.Atlas.Width > 8192 || t.Atlas.Height > 8192 || t.Atlas.Width%32 != 0 || t.Atlas.Height%32 != 0 || len(t.Atlas.RGBA) != t.Atlas.Width*t.Atlas.Height*4 {
		return out, fmt.Errorf("metalproto: invalid terrain tile dimensions or bytes")
	}
	cells := uint32((t.Atlas.Width / 32) * (t.Atlas.Height / 32))
	for _, cell := range t.Lookup {
		if cell >= cells {
			return out, fmt.Errorf("metalproto: terrain lookup exceeds atlas cells")
		}
	}
	if len(t.Detail.RGBA) != 0 {
		if t.Detail.Width != t.Atlas.Width*2 || t.Detail.Height != t.Atlas.Height*2 || len(t.Detail.RGBA) != t.Detail.Width*t.Detail.Height*4 {
			return out, fmt.Errorf("metalproto: invalid detail terrain tile dimensions or bytes")
		}
		out.Detail, out.DetailWidth, out.DetailHeight = pointer(t.Detail.RGBA), uint32(t.Detail.Width), uint32(t.Detail.Height)
	} else if t.Detail.Width != 0 || t.Detail.Height != 0 {
		return out, fmt.Errorf("metalproto: missing detail terrain tile pixels")
	}
	out.Base, out.Lookup = pointer(t.Atlas.RGBA), pointer(t.Lookup)
	out.BaseWidth, out.BaseHeight, out.MapWidth, out.MapHeight = uint32(t.Atlas.Width), uint32(t.Atlas.Height), uint32(t.Width), uint32(t.Height)
	return out, nil
}
