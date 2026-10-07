package meshscene

// MeshFace is a 32-byte immutable authored body primitive. Faces and material
// selectors share their ordinal when materials are enabled. Corners preserve
// authored order; Indices names an unsplit fan in Mesh.FaceIndices. Reflection
// admission reserves max(CornerCount, IndexCount) before appending a face, then
// consumes CornerCount for mapped quads or IndexCount for other polygons
// (DESIGN_GPU_RENDERER §26.6). Stress tessellation never enters this accounting.
type MeshFace struct {
	CornerStart, CornerCount, Piece, Material uint32
	IndexStart, IndexCount, Primitive, Flags  uint32
}

const MeshFaceQuad uint32 = 1

func (m *Mesh) appendAuthoredFace(corners []Vertex, piece, primitive int, material uint32) {
	f := MeshFace{CornerStart: uint32(len(m.FaceCorners)), CornerCount: uint32(len(corners)), Piece: uint32(piece), Material: material, IndexStart: uint32(len(m.FaceIndices)), Primitive: uint32(primitive)}
	if len(corners) == 4 {
		f.Flags = MeshFaceQuad
	}
	m.FaceCorners = append(m.FaceCorners, corners...)
	for i := 1; i+1 < len(corners); i++ {
		m.FaceIndices = append(m.FaceIndices, f.CornerStart, f.CornerStart+uint32(i), f.CornerStart+uint32(i+1))
	}
	f.IndexCount = uint32(len(m.FaceIndices)) - f.IndexStart
	m.Faces = append(m.Faces, f)
}
