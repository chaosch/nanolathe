//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Concatenate after projection.metal and the base/group shaders.
//
//go:embed native/outline_rows.metal
var OutlineRowsShaderSource string

// OutlineRowsRing names the immutable contiguous vertices compileMesh reserves
// before material admission, in reverse-piece/forward-primitive order.
type OutlineRowsRing struct{ Start, Count, Piece, CornerOffset uint32 }
type NativeOutlineRowsUpload struct {
	Rings               unsafe.Pointer
	RingCount, Reserved uint32
}

// Keep the wrapper alive until nm_mesh_outline_rows returns; Native borrows Rings.
type OutlineRowsMeshUpload struct {
	Native NativeOutlineRowsUpload
	Rings  []OutlineRowsRing
}

func PrepareOutlineRows(mesh *meshscene.Mesh) (*OutlineRowsMeshUpload, error) {
	if mesh == nil || unsafe.Sizeof(OutlineRowsRing{}) != 16 || unsafe.Sizeof(NativeOutlineRowsUpload{}) != 16 || len(mesh.EdgeIndices)%2 != 0 {
		return nil, fmt.Errorf("metalproto: outline ring ABI or edge span mismatch")
	}
	out := &OutlineRowsMeshUpload{}
	for at := 0; at < len(mesh.EdgeIndices); {
		start := mesh.EdgeIndices[at]
		n := uint32(0)
		for {
			if at+1 >= len(mesh.EdgeIndices) || int(start+n) >= len(mesh.Vertices) || mesh.EdgeIndices[at] != start+n {
				return nil, fmt.Errorf("metalproto: outline ring is not a contiguous authored cycle")
			}
			end := mesh.EdgeIndices[at+1]
			at += 2
			n++
			if end == start {
				break
			}
			if end != start+n {
				return nil, fmt.Errorf("metalproto: outline ring edge does not follow its authored corner")
			}
		}
		if n < 2 {
			return nil, fmt.Errorf("metalproto: outline ring has fewer than two corners")
		}
		piece := mesh.Vertices[start].Piece
		for _, v := range mesh.Vertices[start : start+n] {
			if v.Piece != piece {
				return nil, fmt.Errorf("metalproto: outline ring crosses model pieces")
			}
		}
		var compact uint32
		if len(out.Rings) != 0 {
			prev := out.Rings[len(out.Rings)-1]
			compact = prev.CornerOffset + prev.Count
		}
		if uint64(compact)+uint64(n) > math.MaxUint32 {
			return nil, fmt.Errorf("metalproto: outline compact corners exceed native range")
		}
		out.Rings = append(out.Rings, OutlineRowsRing{Start: start, Count: n, Piece: piece, CornerOffset: compact})
	}
	if len(out.Rings) > math.MaxInt32 {
		return nil, fmt.Errorf("metalproto: outline ring count exceeds native range")
	}
	out.Native.RingCount = uint32(len(out.Rings))
	if len(out.Rings) > 0 {
		out.Native.Rings = unsafe.Pointer(&out.Rings[0])
	}
	return out, nil
}
