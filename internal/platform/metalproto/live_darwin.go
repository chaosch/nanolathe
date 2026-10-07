//go:build darwin

package metalproto

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

type nativeInput struct {
	PanX, PanZ, Zoom float32
	PauseToggle      uint32
}
type meshSpan struct{ Start, Count uint32 }

// Same scalar/pointer layout as NMLiveUpload. Slices are copied synchronously;
// the native ring never retains a pointer into Go memory after the call.
type liveUpload struct {
	Previous, Current, Offsets, Spans, Sprites, Lights          uintptr
	Camera                                                      [4]float32
	Alpha                                                       float32
	PoseCount, InstanceCount, SpriteCount, LightCount, Reserved uint32
	Seconds                                                     float64
}
type livePacking struct {
	spans           []meshSpan
	cursor, offsets []uint32
}

func (p *livePacking) prepare(scene *meshscene.Scene, f meshscene.LiveFrame, seconds float64) (liveUpload, liveRow, error) {
	var u liveUpload
	var r liveRow
	if unsafe.Sizeof(u) != 96 || unsafe.Sizeof(meshscene.Sprite{}) != 96 || unsafe.Sizeof(meshscene.Light{}) != 32 || unsafe.Sizeof(nativeInput{}) != 16 {
		return u, r, fmt.Errorf("metalproto: live GPU ABI mismatch")
	}
	if len(f.Previous) != len(f.Current) || len(f.Current) > math.MaxInt32 || len(f.Instances) > math.MaxInt32 || len(f.Sprites) > math.MaxInt32 || f.Alpha < 0 || f.Alpha > 1 || math.IsNaN(float64(f.Alpha)) {
		return u, r, fmt.Errorf("metalproto: invalid live frame topology or alpha")
	}
	for i := range p.spans {
		p.spans[i] = meshSpan{}
		p.cursor[i] = 0
	}
	for _, instance := range f.Instances {
		if instance.Mesh < 0 || instance.Mesh >= len(scene.Meshes) || instance.PoseOffset < 0 || instance.PoseOffset > len(f.Current) || scene.Meshes[instance.Mesh].Pieces > len(f.Current)-instance.PoseOffset {
			return u, r, fmt.Errorf("metalproto: live instance has invalid mesh or pose span")
		}
		p.spans[instance.Mesh].Count++
	}
	var start uint32
	for i := range p.spans {
		p.spans[i].Start = start
		p.cursor[i] = start
		start += p.spans[i].Count
	}
	if cap(p.offsets) < len(f.Instances) {
		p.offsets = make([]uint32, len(f.Instances))
	} else {
		p.offsets = p.offsets[:len(f.Instances)]
	}
	for _, instance := range f.Instances {
		p.offsets[p.cursor[instance.Mesh]] = uint32(instance.PoseOffset)
		p.cursor[instance.Mesh]++
	}
	lights := f.Lights
	if len(lights) > 32 {
		lights = lights[:32]
	}
	u = liveUpload{Previous: pointer(f.Previous), Current: pointer(f.Current), Offsets: pointer(p.offsets), Spans: pointer(p.spans), Sprites: pointer(f.Sprites), Lights: pointer(lights), Camera: [4]float32{f.Camera[0], f.Camera[1], f.Camera[2], 0}, Alpha: f.Alpha, PoseCount: uint32(len(f.Current)), InstanceCount: uint32(len(f.Instances)), SpriteCount: uint32(len(f.Sprites)), LightCount: uint32(len(lights)), Seconds: seconds}
	r.Seconds = seconds
	r.Alpha = f.Alpha
	r.Tick = f.Tick
	r.Instances = len(f.Instances)
	r.Poses = len(f.Current)
	r.Sprites = len(f.Sprites)
	r.Lights = len(lights)
	r.DroppedLights = len(f.Lights) - len(lights)
	r.UploadBytes = uint64(len(f.Current))*128 + uint64(len(f.Instances))*4 + uint64(len(f.Sprites))*96 + uint64(len(lights))*32
	for i, span := range p.spans {
		if span.Count == 0 || len(scene.Meshes[i].Indices) == 0 {
			continue
		}
		r.ModelDraws++
		r.ModelVertices += uint64(len(scene.Meshes[i].Vertices)) * uint64(span.Count)
		r.ModelIndices += uint64(len(scene.Meshes[i].Indices)) * uint64(span.Count)
	}
	if r.Sprites > 0 {
		r.SpriteDraws = 1
	}
	return u, r, nil
}
