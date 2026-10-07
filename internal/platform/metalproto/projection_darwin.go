//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Concatenate before faces/base shaders; this module has no other shader-type
// dependency. Bind records/piece flags/view at buffers 27/28/29 in VS and CS.
//
//go:embed native/projection.metal
var projectionShaderSource string

// NativeProjectionUpload is 40 bytes on the supported 64-bit host. Native
// copies its operands after acquiring a ring slot, before Prepare is repeated.
type NativeProjectionUpload struct {
	Records, PieceFlags     uintptr
	RecordCount, PieceCount uint32
	View                    [4]float32
}

type ProjectionPacking struct {
	records []meshscene.ModelProjection
	flags   []uint32
	seen    []bool
}

// Prepare remaps original instance indices through sourcePacked. Pose indices
// stay unchanged: mesh packing rearranges instances, not the pose endpoints.
func (p *ProjectionPacking) Prepare(f meshscene.ModelProjectionFrame, sourcePacked []uint32) (NativeProjectionUpload, error) {
	var out NativeProjectionUpload
	if unsafe.Sizeof(out) != 40 || unsafe.Sizeof(meshscene.ModelProjection{}) != 48 || len(f.Records) != len(sourcePacked) || len(f.Records) > math.MaxInt32 || len(f.PieceFlags) > math.MaxInt32 {
		return out, projectionUploadError("matching native layout and instance mapping")
	}
	finite := func(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
	for _, v := range f.View {
		if !finite(v) {
			return out, projectionUploadError("finite view")
		}
	}
	p.records = append(p.records[:0], f.Records...)
	if cap(p.seen) < len(sourcePacked) {
		p.seen = make([]bool, len(sourcePacked))
	} else {
		p.seen = p.seen[:len(sourcePacked)]
		clear(p.seen)
	}
	for i, at := range sourcePacked {
		if uint64(at) >= uint64(len(p.records)) || p.seen[at] {
			return out, projectionUploadError("an instance permutation")
		}
		p.seen[at] = true
		v := f.Records[i]
		for _, group := range [][4]float32{v.Origin, v.Anchors, v.Control} {
			for _, lane := range group {
				if !finite(lane) {
					return out, projectionUploadError("finite metadata")
				}
			}
		}
		if v.Origin[3] < 0 || v.Origin[3] > 3 || v.Origin[3] != float32(uint32(v.Origin[3])) {
			return out, projectionUploadError("valid staging flags")
		}
		if uint32(v.Origin[3])&meshscene.ModelProjectionKnown != 0 && ((v.Control[0] != 1 && v.Control[0] != 2) || (v.Control[1] != 0 && v.Control[1] != 1) || v.Control[2] <= 0) {
			return out, projectionUploadError("production record step and geometry gate")
		}
		p.records[at] = v
	}
	p.flags = append(p.flags[:0], f.PieceFlags...)
	for _, flag := range p.flags {
		if flag & ^uint32(meshscene.ModelProjectionPieceDirect|meshscene.ModelProjectionPieceUnknown) != 0 {
			return out, projectionUploadError("valid piece projection flags")
		}
	}
	out = NativeProjectionUpload{Records: pointer(p.records), PieceFlags: pointer(p.flags), RecordCount: uint32(len(p.records)), PieceCount: uint32(len(p.flags)), View: f.View}
	return out, nil
}

func projectionUploadError(expected string) error {
	return fmt.Errorf("nanolathe: retained projection upload failed: logical path model projection metadata, providers searched [production client retained instance packing], expected %s", expected)
}
