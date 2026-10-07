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
}

func compileMesh(m *model.Model, atlas *atlasResult, pal *palette.Tables, multiplier int, census *meshCensus) Mesh {
	mesh := Mesh{Name: m.Name, Pieces: len(m.Pieces)}
	census.Model, census.Pieces = m.Name, len(m.Pieces)
	for pi, piece := range m.Pieces {
		for pri, primitive := range piece.Primitives {
			if piece.Selection && pri == 0 {
				census.SelectionFacesSkipped++
				continue
			}
			n := len(primitive.VertexIndices)
			if n < 3 {
				census.LinesSkipped++
				continue
			}
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
					color = [3]float32{1, 0, 1}
				}
			} else {
				c := pal.Base[byte(primitive.ColorIndex)]
				color = [3]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255}
			}
			vertices := make([]Vertex, n)
			uvCorners := [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
			for vi, index := range primitive.VertexIndices {
				p := piece.Vertices[index]
				vertices[vi] = Vertex{Position: [3]float32{float32(p[0]) / 65536, float32(p[1]) / 65536, float32(p[2]) / 65536}, Color: color, Piece: uint32(pi)}
				uv := [2]float32{0.5, 0.5}
				if textured && n == 4 {
					uv = uvCorners[vi]
				}
				vertices[vi].UV = region.uv(uv, atlas.uvWidth, atlas.uvHeight)
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
	return mesh
}

func lerpVertex(a, b Vertex, t float32) Vertex {
	out := a
	for i := range out.Position {
		out.Position[i] = a.Position[i] + (b.Position[i]-a.Position[i])*t
	}
	for i := range out.UV {
		out.UV[i] = a.UV[i] + (b.UV[i]-a.UV[i])*t
	}
	return out
}

// Match render.faceNormal's authored winding and degenerate fallback; the
// transform mirrors final model Z separately [03 §2.4.1].
func faceNormal(a, b, c [3]float32) [3]float32 {
	u := [3]float32{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float32{b[0] - c[0], b[1] - c[1], b[2] - c[2]}
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
