package meshscene

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
)

type meshCensus struct {
	Unit                  string `json:"unit"`
	Model                 string `json:"model"`
	Pieces                int    `json:"pieces"`
	SourceFaces           int    `json:"source_faces"`
	SourceQuads           int    `json:"source_quads"`
	SourceTriangles       int    `json:"source_triangles"`
	SelectionFacesSkipped int    `json:"selection_faces_skipped"`
	LinesSkipped          int    `json:"lines_skipped"`
	TexturedFaces         int    `json:"textured_faces"`
	MissingTextureFaces   int    `json:"missing_texture_faces"`
	ResultQuads           int    `json:"result_quads"`
	ResultTriangles       int    `json:"result_triangles"`
	ResultVertices        int    `json:"result_vertices"`
	AuthoredEdges         int    `json:"authored_edges"`
}

func compileMesh(m *model.Model, atlas *atlasResult, pal *palette.Tables, multiplier int, census *meshCensus) Mesh {
	mesh := Mesh{Name: m.Name, Pieces: len(m.Pieces)}
	census.Model, census.Pieces = m.Name, len(m.Pieces)
	// Equal subject heights keep the later face, including the opened Solar rim.
	// Match the production reverse piece walk [03 R-REN-03A §3].
	for pi := len(m.Pieces) - 1; pi >= 0; pi-- {
		piece := m.Pieces[pi]
		shadeNormals := retainedShadeNormals(piece)
		for pri, primitive := range piece.Primitives {
			if piece.Selection && pri == 0 {
				census.SelectionFacesSkipped++
				continue
			}
			n := len(primitive.VertexIndices)
			valid := true
			for _, index := range primitive.VertexIndices {
				if int(index) >= len(piece.Vertices) {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			// Retain the authored rings once, including two-corner primitives.
			// The native line pass approximates retail row extrema [03 R-COMP-01 §3].
			if n >= 2 {
				base := uint32(len(mesh.Vertices))
				for _, index := range primitive.VertexIndices {
					p := piece.Vertices[index]
					mesh.Vertices = append(mesh.Vertices, Vertex{Position: [3]float32{float32(p[0]) / 65536, float32(p[1]) / 65536, float32(p[2]) / 65536}, Piece: uint32(pi)})
				}
				for vi := 0; vi < n; vi++ {
					mesh.EdgeIndices = append(mesh.EdgeIndices, base+uint32(vi), base+uint32((vi+1)%n))
				}
			}
			if n < 3 {
				census.LinesSkipped++
				continue
			}
			if primitive.IsColored&1 == 0 && (n != 4 || primitive.TextureName == "") {
				// The production quad mapper rejects clear/non-quad textures [03 R-REN-03A §5].
				continue
			}
			census.SourceFaces++
			census.SourceTriangles += n - 2
			if n == 4 {
				census.SourceQuads++
				census.ResultQuads += multiplier
			}
			region := atlas.white
			color := [3]float32{1, 1, 1}
			// Match modelPrimitiveDispatch: only bit zero selects flat art;
			// even nonzero authored residue still selects textures [R-REN-03A §5].
			textured := primitive.IsColored&1 == 0 && primitive.TextureName != ""
			if textured {
				census.TexturedFaces++
				if r, ok := atlas.byName[canonicalTexture(primitive.TextureName)]; ok {
					region = r
				} else {
					census.MissingTextureFaces++
					c := pal.Base[0xd1]
					color = [3]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255}
				}
			} else {
				c := pal.Base[byte(primitive.ColorIndex)]
				color = [3]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255}
			}
			materialID := uint32(0)
			if atlas.useMaterials {
				material := Material{FirstFrame: atlas.whiteFrame, FrameCount: 1}
				if id := atlas.materialByName[canonicalTexture(primitive.TextureName)]; textured && id != 0 {
					material = atlas.materials[id]
				}
				material.Reserved = uint32(len(mesh.MaterialSlots))
				materialID = uint32(len(atlas.materials))
				atlas.materials = append(atlas.materials, material)
				mesh.MaterialSlots = append(mesh.MaterialSlots, ModelMaterialSlot{Piece: pi, Primitive: pri, Material: materialID})
			}
			vertices := make([]Vertex, n)
			uvCorners := [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
			for vi, index := range primitive.VertexIndices {
				p := piece.Vertices[index]
				vertices[vi] = Vertex{Position: [3]float32{float32(p[0]) / 65536, float32(p[1]) / 65536, float32(p[2]) / 65536}, Color: color, Piece: uint32(pi), ShadeNormal: shadeNormals[index]}
				uv := [2]float32{0.5, 0.5}
				if textured && n == 4 {
					uv = uvCorners[vi]
				}
				if atlas.useMaterials {
					vertices[vi].Material = materialID
					vertices[vi].UV = uv
				} else {
					vertices[vi].UV = region.uv(uv, atlas.uvWidth, atlas.uvHeight)
				}
			}
			normal := faceNormal(vertices[0].Position, vertices[1].Position, vertices[2].Position)
			for i := range vertices {
				vertices[i].Normal = normal
			}
			if n == 4 {
				// A strip creates actual smaller spatial quads. It changes tessellation,
				// not the authored outline or the first/last texture corners.
				for strip := 0; strip < multiplier; strip++ {
					a, b := float32(strip)/float32(multiplier), float32(strip+1)/float32(multiplier)
					q := [4]Vertex{lerpVertex(vertices[0], vertices[1], a), lerpVertex(vertices[0], vertices[1], b), lerpVertex(vertices[3], vertices[2], b), lerpVertex(vertices[3], vertices[2], a)}
					base := uint32(len(mesh.Vertices))
					mesh.Vertices = append(mesh.Vertices, q[:]...)
					mesh.Indices = append(mesh.Indices, base, base+1, base+2, base, base+2, base+3)
				}
			} else {
				for j := 1; j < n-1; j++ {
					for strip := 0; strip < multiplier; strip++ {
						a, b := float32(strip)/float32(multiplier), float32(strip+1)/float32(multiplier)
						tri := [3]Vertex{vertices[0], lerpVertex(vertices[j], vertices[j+1], a), lerpVertex(vertices[j], vertices[j+1], b)}
						base := uint32(len(mesh.Vertices))
						mesh.Vertices = append(mesh.Vertices, tri[:]...)
						mesh.Indices = append(mesh.Indices, base, base+1, base+2)
					}
				}
			}
		}
	}
	mesh.SourceFaces = census.SourceFaces
	census.ResultTriangles = len(mesh.Indices) / 3
	census.ResultVertices = len(mesh.Vertices)
	census.AuthoredEdges = len(mesh.EdgeIndices) / 2
	return mesh
}

func lerpVertex(a, b Vertex, t float32) Vertex {
	out := a
	for i := range out.Position {
		out.Position[i] = a.Position[i] + (b.Position[i]-a.Position[i])*t
	}
	for i := range out.ShadeNormal {
		out.ShadeNormal[i] = a.ShadeNormal[i] + (b.ShadeNormal[i]-a.ShadeNormal[i])*t
	}
	for i := range out.UV {
		out.UV[i] = a.UV[i] + (b.UV[i]-a.UV[i])*t
	}
	return out
}

// Conventional outward normals for this prototype's Lambert lighting. Asset
// inspection confirms the ARMSTUMP top faces point up and ARMPEEP underside
// points down with this cross product. Retail SHD uses the opposite convention;
// its wrapped shading row is not a Lambert intensity (DESIGN_GPU_RENDERER §22).
func faceNormal(a, b, c [3]float32) [3]float32 {
	u := [3]float32{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float32{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	n := [3]float32{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
	length := float32(math.Sqrt(float64(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])))
	if length == 0 {
		return [3]float32{0, 1, 0}
	}
	for i := range n {
		n[i] /= length
	}
	return n
}

// Match the production model lane: average normalized authored face normals at
// each shared corner without renormalizing the average [03 §2.4.1]. The GPU
// rotates this retained lane with the piece and evaluates the existing SHD row.
func retainedShadeNormals(piece model.Piece) [][3]float32 {
	sums := make([][3]float64, len(piece.Vertices))
	counts := make([]int, len(piece.Vertices))
	for pi, primitive := range piece.Primitives {
		if piece.Selection && pi == 0 || len(primitive.VertexIndices) < 3 {
			continue
		}
		a, b, c := primitive.VertexIndices[0], primitive.VertexIndices[1], primitive.VertexIndices[2]
		if int(a) >= len(sums) || int(b) >= len(sums) || int(c) >= len(sums) {
			continue
		}
		var u, v [3]float64
		for j := range u {
			u[j] = float64(piece.Vertices[b][j]) - float64(piece.Vertices[a][j])
			v[j] = float64(piece.Vertices[b][j]) - float64(piece.Vertices[c][j])
		}
		n := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
		length := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
		if length == 0 {
			n = [3]float64{0, 1, 0}
		} else {
			for j := range n {
				n[j] /= length
			}
		}
		for _, index := range primitive.VertexIndices {
			if int(index) >= len(sums) {
				continue
			}
			for j := range n {
				sums[index][j] += n[j]
			}
			counts[index]++
		}
	}
	out := make([][3]float32, len(sums))
	for i := range out {
		if counts[i] != 0 {
			for j := range out[i] {
				out[i][j] = float32(sums[i][j] / float64(counts[i]))
			}
		}
	}
	return out
}
