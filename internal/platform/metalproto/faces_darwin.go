//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Concatenate before the base model shaders. The face module declares its own
// ABI types and exposes nm_face_lanes/nm_face_pixel for the model shader.
//
//go:embed native/faces.metal
var facesShaderSource string

// NativeMeshFacesUpload borrows immutable mesh storage until nm_mesh_faces
// returns. Mesh upload is cold; no posed vertices cross the host boundary.
// Its C counterpart NMFaceUpload is 40 bytes on the supported 64-bit host.
type NativeMeshFacesUpload struct {
	Faces, Corners, Indices                      unsafe.Pointer
	FaceCount, CornerCount, IndexCount, Reserved uint32
}

func PrepareMeshFaces(mesh *meshscene.Mesh) (NativeMeshFacesUpload, error) {
	var out NativeMeshFacesUpload
	if mesh == nil || unsafe.Sizeof(out) != 40 || unsafe.Sizeof(meshscene.MeshFace{}) != 32 || unsafe.Sizeof(meshscene.Vertex{}) != 64 {
		return out, fmt.Errorf("metalproto: authored face ABI mismatch")
	}
	if len(mesh.Faces) > math.MaxInt32 || len(mesh.FaceCorners) > math.MaxInt32 || len(mesh.FaceIndices) > math.MaxInt32 {
		return out, fmt.Errorf("metalproto: authored face count exceeds native range")
	}
	for i, f := range mesh.Faces {
		if f.CornerCount < 3 || uint64(f.CornerStart)+uint64(f.CornerCount) > uint64(len(mesh.FaceCorners)) || uint64(f.IndexStart)+uint64(f.IndexCount) > uint64(len(mesh.FaceIndices)) || uint64(f.IndexCount) != 3*uint64(f.CornerCount-2) || f.Flags & ^meshscene.MeshFaceQuad != 0 || (f.Flags&meshscene.MeshFaceQuad != 0) != (f.CornerCount == 4) {
			return out, fmt.Errorf("metalproto: invalid authored face %d", i)
		}
		for _, v := range mesh.FaceCorners[f.CornerStart : f.CornerStart+f.CornerCount] {
			if v.Piece != f.Piece || v.Material != f.Material {
				return out, fmt.Errorf("metalproto: inconsistent authored face %d corner", i)
			}
		}
	}
	for _, i := range mesh.FaceIndices {
		if uint64(i) >= uint64(len(mesh.FaceCorners)) {
			return out, fmt.Errorf("metalproto: authored face index outside corner span")
		}
	}
	out.Faces, out.Corners, out.Indices = pointer(mesh.Faces), pointer(mesh.FaceCorners), pointer(mesh.FaceIndices)
	out.FaceCount, out.CornerCount, out.IndexCount = uint32(len(mesh.Faces)), uint32(len(mesh.FaceCorners)), uint32(len(mesh.FaceIndices))
	return out, nil
}
