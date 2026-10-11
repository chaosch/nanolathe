// Package fixture constructs authored presentation workloads without sessions or assets.
package fixture

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

const Version = "authored-v1"
const Seed = uint32(7)

type Snapshot struct {
	Generation        uint64
	Previous, Current []meshscene.Mat4
	Instances         []meshscene.Instance
}
type Workload struct {
	Scene                                       meshscene.Scene
	Count, Detail, Width, Height, Columns, Rows int
	Mode                                        string
	Cell, Scale                                 float32
	Hashes                                      map[string]string
}

func New(count, detail, width, height int, mode string) (*Workload, error) {
	if count != 64 && count != 1000 && count != 3000 && count != 5000 {
		return nil, fmt.Errorf("unsupported count %d", count)
	}
	if detail != 200 && detail != 500 {
		return nil, fmt.Errorf("unsupported detail %d", detail)
	}
	if width <= 64 || height <= 64 {
		return nil, fmt.Errorf("invalid physical extent")
	}
	if mode != "animated" && mode != "static" && mode != "camera" {
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
	w := &Workload{Count: count, Detail: detail, Width: width, Height: height, Mode: mode}
	w.Columns = int(math.Ceil(math.Sqrt(float64(count) * float64(width) / float64(height))))
	w.Rows = (count + w.Columns - 1) / w.Columns
	zoom := float32(width) / 16
	margin := float32(32) / zoom
	worldH := float32(height) / zoom
	w.Cell = min((16-2*margin)/float32(w.Columns), (worldH-2*margin)/float32(w.Rows))
	w.Scale = w.Cell * 0.44
	w.Scene = meshscene.Scene{Name: Version, Camera: [3]float32{8, worldH / 2, zoom}}
	for mesh := 0; mesh < 3; mesh++ {
		m := makeMesh(mesh, detail)
		for i := range m.Vertices {
			for j := range m.Vertices[i].Position {
				m.Vertices[i].Position[j] *= w.Scale
			}
		}
		w.Scene.Meshes = append(w.Scene.Meshes, m)
	}
	// A small asymmetric authored atlas. Opaque RGBA bytes, nearest diagnostic sampling.
	w.Scene.Atlas = meshscene.Texture{Width: 8, Height: 8, RGBA: make([]byte, 8*8*4)}
	colors := [][4]byte{{255, 32, 16, 255}, {16, 255, 64, 255}, {32, 64, 255, 255}, {255, 224, 32, 255}}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := colors[(y/4)*2+x/4]
			if x == 0 && y == 0 {
				c = [4]byte{255, 0, 255, 255}
			}
			copy(w.Scene.Atlas.RGBA[(y*8+x)*4:], c[:])
		}
	}
	// Group instance records by mesh; ID/position remain their original grid identities.
	for mesh := 0; mesh < 3; mesh++ {
		for id := mesh; id < count; id += 3 {
			w.Scene.Instances = append(w.Scene.Instances, meshscene.Instance{Mesh: mesh, PoseOffset: len(w.Scene.Instances) * 3, Subject: uint64(id + 1)})
		}
	}
	w.Hashes = map[string]string{}
	for i, m := range w.Scene.Meshes {
		w.Hashes[fmt.Sprintf("mesh%d", i)] = meshHash(m)
	}
	w.Hashes["texture"] = digest(w.Scene.Atlas.RGBA)
	cfg, _ := json.Marshal([]any{Version, Seed, count, detail, width, height, mode, w.Columns, w.Rows, w.Cell, w.Scale, w.Scene.Camera, "yaw + body translation + turret/barrel articulation; nominal tick/30"})
	w.Hashes["configuration"] = digest(cfg)
	snap := w.Sample(0)
	b := []byte{}
	for _, poses := range [][]meshscene.Mat4{snap.Previous, snap.Current} {
		for _, p := range poses {
			for _, v := range p {
				b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
			}
		}
	}
	w.Hashes["animation_endpoints_0_1"] = digest(b)
	return w, nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func meshHash(m meshscene.Mesh) string {
	b := []byte{}
	for _, v := range m.Vertices {
		for _, x := range []float32{v.Position[0], v.Position[1], v.Position[2], v.Normal[0], v.Normal[1], v.Normal[2], v.Color[0], v.Color[1], v.Color[2], v.ShadeNormal[0], v.ShadeNormal[1], v.ShadeNormal[2]} {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(x))
		}
		for _, x := range v.UV {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(x))
		}
		b = binary.LittleEndian.AppendUint32(b, v.Piece)
		b = binary.LittleEndian.AppendUint32(b, v.Material)
	}
	for _, i := range m.Indices {
		b = binary.LittleEndian.AppendUint32(b, i)
	}
	return digest(b)
}

func makeMesh(kind, detail int) meshscene.Mesh {
	m := meshscene.Mesh{Name: fmt.Sprintf("authored-bevel-%d", kind), Pieces: 3}
	// Top grids supply real noncoincident detail. Bevel/side/bottom surfaces remain topology.
	top := [][2]int{{8, 5}, {5, 5}, {3, 5}}
	if detail == 500 {
		top = [][2]int{{23, 5}, {10, 7}, {9, 5}}
	}
	for piece := 0; piece < 3; piece++ {
		radius := float32(.6)
		height := float32(.4)
		if piece == 1 {
			radius = .23
			height = .24
		}
		if piece == 2 {
			radius = .16
			height = .14
		}
		stretch := float32(1) + float32(kind)*.035
		outer := radius
		inner := radius
		if piece == 0 {
			inner -= .05
		}
		nx, ny := top[piece][0], top[piece][1]
		for y := 0; y < ny; y++ {
			for x := 0; x < nx; x++ {
				x0, x1 := -inner+2*inner*float32(x)/float32(nx), -inner+2*inner*float32(x+1)/float32(nx)
				z0, z1 := -inner+2*inner*float32(y)/float32(ny), -inner+2*inner*float32(y+1)/float32(ny)
				quad(&m, piece, kind, [4][3]float32{{x0 * stretch, height, z0}, {x1 * stretch, height, z0}, {x1 * stretch, height, z1}, {x0 * stretch, height, z1}})
			}
		}
		ring := [][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
		for side := 0; side < 4; side++ {
			a, b := ring[side], ring[(side+1)%4]
			topY := height
			if piece == 0 {
				topY = height - .05
				quad(&m, piece, kind, [4][3]float32{{a[0] * inner * stretch, height, a[1] * inner}, {b[0] * inner * stretch, height, b[1] * inner}, {b[0] * outer * stretch, topY, b[1] * outer}, {a[0] * outer * stretch, topY, a[1] * outer}})
			}
			quad(&m, piece, kind, [4][3]float32{{a[0] * outer * stretch, topY, a[1] * outer}, {b[0] * outer * stretch, topY, b[1] * outer}, {b[0] * outer * stretch, 0, b[1] * outer}, {a[0] * outer * stretch, 0, a[1] * outer}})
		}
		quad(&m, piece, kind, [4][3]float32{{-outer * stretch, 0, -outer}, {outer * stretch, 0, -outer}, {outer * stretch, 0, outer}, {-outer * stretch, 0, outer}})
		if piece == 0 {
			// Two independently authored non-quad triangles form a shallow nose ridge.
			triangle(&m, piece, kind, [3][3]float32{{-.22, .1, -.6}, {.22, .1, -.6}, {0, .24, -.65}})
			triangle(&m, piece, kind, [3][3]float32{{-.22, .1, -.6}, {0, .24, -.65}, {0, 0, -.65}})
		}
	}
	return m
}
func triangle(m *meshscene.Mesh, piece, kind int, p [3][3]float32) {
	uv := [3][2]float32{{.08, .08}, {.92, .08}, {.08, .92}}
	if kind == 1 {
		for i := range uv {
			uv[i][0] = 1 - uv[i][0]
		}
	}
	for i := 0; i < 3; i++ {
		m.Indices = append(m.Indices, uint32(len(m.Vertices)))
		m.Vertices = append(m.Vertices, meshscene.Vertex{Position: p[i], Normal: [3]float32{0, 1, 0}, UV: uv[i], Color: [3]float32{1, 1, 1}, Piece: uint32(piece), Material: uint32(kind + 1)})
	}
}
func quad(m *meshscene.Mesh, piece, kind int, p [4][3]float32) {
	base := uint32(len(m.Vertices))
	uv := [4][2]float32{{.08, .08}, {.92, .08}, {.92, .92}, {.08, .92}}
	if kind == 1 {
		for i := range uv {
			uv[i][0] = 1 - uv[i][0]
		}
	}
	for i := 0; i < 4; i++ {
		m.Vertices = append(m.Vertices, meshscene.Vertex{Position: p[i], Normal: [3]float32{0, 1, 0}, UV: uv[i], Color: [3]float32{1, 1, 1}, Piece: uint32(piece), Material: uint32(kind + 1)})
	}
	m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
}
func (w *Workload) Sample(tick uint64) Snapshot {
	if w.Mode != "animated" {
		tick = 0
	}
	s := Snapshot{Generation: tick + 1, Instances: w.Scene.Instances, Previous: w.endpoint(tick), Current: w.endpoint(tick + 1)}
	if w.Mode != "animated" {
		s.Current = append([]meshscene.Mat4(nil), s.Previous...)
	}
	return s
}
func (w *Workload) endpoint(tick uint64) []meshscene.Mat4 {
	out := make([]meshscene.Mat4, w.Count*3)
	worldH := float32(w.Height) * 16 / float32(w.Width)
	for at, inst := range w.Scene.Instances {
		id := int(inst.Subject) - 1
		gridID := id
		if w.Count == 64 && id == 1 {
			gridID = 0
		}
		phase := float64(tick)/30 + float64((uint32(id)*37+Seed)%101)/17
		yaw := float64(id%13) * .35
		dx, dz := float32(0), float32(0)
		turret, barrel := 0.0, 0.0
		if w.Mode == "animated" {
			yaw += phase * .7
			dx = float32(math.Sin(phase)) * .015 * w.Cell
			dz = float32(math.Cos(phase)) * .015 * w.Cell
			turret = math.Sin(phase*.9) * .6
			barrel = math.Cos(phase*1.1) * .35
		}
		x := (float32(gridID%w.Columns)-float32(w.Columns-1)/2)*w.Cell + 8 + dx
		z := (float32(gridID/w.Columns)-float32(w.Rows-1)/2)*w.Cell + worldH/2 + w.Scale*.16 + dz
		if w.Count == 64 && id == 1 {
			x += w.Scale * .25
			z += w.Scale * .25
		}
		body := rigid(yaw, 0, 0, 0, 0, 1)
		out[at*3] = body
		out[at*3+1] = compose(body, rigid(turret, 0, 0, .38*w.Scale, 0, 1))
		out[at*3+2] = compose(body, rigid(turret, barrel, .25*w.Scale, .66*w.Scale, 0, 1))
		// compose applies proper local rotations; each final matrix has one world-Z reflection.
		for piece := 0; piece < 3; piece++ {
			out[at*3+piece] = Reflect(out[at*3+piece])
			m := &out[at*3+piece]
			m[12] += x
			m[14] = z - m[14]
			m[3] = 1
			m[7] = 0
			m[11] = 1
			m[15] = 1
		}
	}
	return out
}
func rigid(yaw, pitch float64, x, y, z, scale float32) meshscene.Mat4 {
	cy, sy := float32(math.Cos(yaw)), float32(math.Sin(yaw))
	cp, sp := float32(math.Cos(pitch)), float32(math.Sin(pitch))
	// Authored input rotations are proper. Reflection applies to the final world row only.
	return meshscene.Mat4{cy * scale, 0, -sy * scale, 0, sy * sp * scale, cp * scale, cy * sp * scale, 0, sy * cp * scale, -sp * scale, cy * cp * scale, 0, x, y, z, 1}
}
func compose(a, b meshscene.Mat4) meshscene.Mat4 {
	var m meshscene.Mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			for k := 0; k < 4; k++ {
				m[col*4+row] += a[k*4+row] * b[col*4+k]
			}
		}
	}
	return m
}

// EndpointForTest reflects the final world row exactly once, outside rotation construction.
func Reflect(m meshscene.Mat4) meshscene.Mat4 { m[2] = -m[2]; m[6] = -m[6]; m[10] = -m[10]; return m }
