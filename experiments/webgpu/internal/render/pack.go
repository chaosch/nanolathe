package render

import (
	"encoding/binary"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"math"
)

const vertexStride = 48
const poseStride = 64
const instanceStride = 16
const uniformSize = 256

func putFloat(b []byte, at int, v float32) {
	binary.LittleEndian.PutUint32(b[at:], math.Float32bits(v))
}
func packVertices(vertices []meshscene.Vertex) []byte {
	b := make([]byte, len(vertices)*vertexStride)
	for i, v := range vertices {
		at := i * vertexStride
		for j, x := range v.Position {
			putFloat(b, at+j*4, x)
		}
		putFloat(b, at+12, 1)
		for j, x := range v.UV {
			putFloat(b, at+16+j*4, x)
		}
		binary.LittleEndian.PutUint32(b[at+24:], v.Piece)
		binary.LittleEndian.PutUint32(b[at+28:], v.Material)
		for j, x := range v.Color {
			putFloat(b, at+32+j*4, x)
		}
		putFloat(b, at+44, 1)
	}
	return b
}
func packPoses(b []byte, a, c []meshscene.Mat4) {
	for i := 0; i < len(a)+len(c); i++ {
		p := a[0]
		if i < len(a) {
			p = a[i]
		} else {
			p = c[i-len(a)]
		}
		for j, x := range p {
			putFloat(b, i*poseStride+j*4, x)
		}
	}
}
func packInstances(b []byte, instances []meshscene.Instance) {
	for i, v := range instances {
		at := i * instanceStride
		binary.LittleEndian.PutUint32(b[at:], uint32(v.PoseOffset))
		binary.LittleEndian.PutUint32(b[at+4:], uint32(v.Mesh))
		binary.LittleEndian.PutUint32(b[at+8:], uint32(v.Subject))
		binary.LittleEndian.PutUint32(b[at+12:], 0)
	}
}
