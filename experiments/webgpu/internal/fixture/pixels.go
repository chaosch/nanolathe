package fixture

import "math"

type projected struct{ x, y, depth, u, v float64 }
type face struct {
	corners [3]projected
	id      uint32
}
type RasterOracle struct {
	faces   []face
	texture []byte
}
type PixelSample struct {
	X, Y     int
	ID       uint32
	RGBA     [4]byte
	Depth    float64
	Interior bool
}

// Raster preprojects authored geometry. Barycentric sampling is independent of GPU packing.
func Raster(w *Workload, s Snapshot, alpha float32) *RasterOracle {
	r := &RasterOracle{texture: w.Scene.Atlas.RGBA}
	for at, inst := range s.Instances {
		mesh := w.Scene.Meshes[inst.Mesh]
		vertices := make([]projected, len(mesh.Vertices))
		for i, v := range mesh.Vertices {
			m := Blend(s.Previous[at*3+int(v.Piece)], s.Current[at*3+int(v.Piece)], alpha)
			p := Point(m, v.Position)
			vertices[i] = projected{(float64(p[0])-float64(w.Scene.Camera[0]))*float64(w.Scene.Camera[2]) + float64(w.Width)/2, (float64(p[2])-float64(w.Scene.Camera[1])-float64(p[1])*.5)*float64(w.Scene.Camera[2]) + float64(w.Height)/2, .5 - (float64(p[2])-float64(w.Scene.Camera[1])+float64(p[1])*2)*.00001, float64(v.UV[0]), float64(v.UV[1])}
		}
		for i := 0; i < len(mesh.Indices); i += 3 {
			r.faces = append(r.faces, face{[3]projected{vertices[mesh.Indices[i]], vertices[mesh.Indices[i+1]], vertices[mesh.Indices[i+2]]}, uint32(inst.Subject)})
		}
	}
	return r
}
func (r *RasterOracle) Sample(x, y int) PixelSample {
	sample := PixelSample{X: x, Y: y, RGBA: [4]byte{0, 0, 0, 255}, Depth: 1, Interior: true}
	px, py := float64(x)+.5, float64(y)+.5
	for _, f := range r.faces {
		a, b, c := f.corners[0], f.corners[1], f.corners[2]
		den := (b.y-c.y)*(a.x-c.x) + (c.x-b.x)*(a.y-c.y)
		if math.Abs(den) < 1e-12 {
			continue
		}
		u := ((b.y-c.y)*(px-c.x) + (c.x-b.x)*(py-c.y)) / den
		v := ((c.y-a.y)*(px-c.x) + (a.x-c.x)*(py-c.y)) / den
		q := 1 - u - v
		if min(u, v, q) < 0 {
			continue
		}
		depth := u*a.depth + v*b.depth + q*c.depth
		if depth >= sample.Depth {
			continue
		}
		tx := max(0, min(7, int((u*a.u+v*b.u+q*c.u)*8)))
		ty := max(0, min(7, int((u*a.v+v*b.v+q*c.v)*8)))
		copy(sample.RGBA[:], r.texture[(ty*8+tx)*4:])
		sample.ID = f.id
		sample.Depth = depth
		sample.Interior = min(u, v, q) > .015
	}
	return sample
}
