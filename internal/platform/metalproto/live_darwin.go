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
type nativeAssets struct {
	Materials, Frames, Heights               uintptr
	MaterialCount, FrameCount, Width, Height uint32
	Rect                                     [4]float32
}
type liveUpload struct {
	Previous, Current, Offsets, Spans, Sprites, Lights, Visuals, Distortions, Fog, Selectors uintptr
	Camera, FogRect                                                                          [4]float32
	Alpha                                                                                    float32
	PoseCount, InstanceCount, SpriteCount, LightCount, DistortionCount, FogWidth, FogHeight  uint32
	Seconds                                                                                  float64
}
type livePacking struct {
	spans           []meshSpan
	cursor, offsets []uint32
	visuals         []meshscene.ModelVisual
	selectors       []uint32
}

func (p *livePacking) prepare(scene *meshscene.Scene, f meshscene.LiveFrame, seconds float64, visualPasses bool) (liveUpload, liveRow, error) {
	var u liveUpload
	var r liveRow
	if !visualPasses {
		f.Distortions = nil
		f.Fog = meshscene.FogFrame{}
	}
	if unsafe.Sizeof(u) != 152 || unsafe.Sizeof(meshscene.Distortion{}) != 64 || unsafe.Sizeof(meshscene.Sprite{}) != 96 || unsafe.Sizeof(meshscene.Light{}) != 32 || unsafe.Sizeof(nativeInput{}) != 16 {
		return u, r, fmt.Errorf("metalproto: live GPU ABI mismatch")
	}
	if len(f.Previous) != len(f.Current) || len(f.Current) > math.MaxInt32 || len(f.Instances) > math.MaxInt32 || len(f.Sprites) > math.MaxInt32 || f.Alpha < 0 || f.Alpha > 1 || math.IsNaN(float64(f.Alpha)) {
		return u, r, fmt.Errorf("metalproto: invalid live frame topology or alpha")
	}
	for i := range p.cursor {
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
	for i := range p.cursor {
		p.spans[i].Start = start
		p.cursor[i] = start
		start += p.spans[i].Count
	}
	if cap(p.offsets) < len(f.Instances) {
		p.offsets = make([]uint32, len(f.Instances))
		p.visuals = make([]meshscene.ModelVisual, len(f.Instances))
	} else {
		p.offsets = p.offsets[:len(f.Instances)]
		p.visuals = p.visuals[:len(f.Instances)]
	}
	for _, instance := range f.Instances {
		p.offsets[p.cursor[instance.Mesh]] = uint32(instance.PoseOffset)
		p.visuals[p.cursor[instance.Mesh]] = instance.Visual
		p.cursor[instance.Mesh]++
	}
	lights := f.Lights
	if len(lights) > 32 {
		lights = lights[:32]
	}
	if len(f.Distortions) > 192 || f.Fog.Width < 0 || f.Fog.Height < 0 || uint64(f.Fog.Width)*uint64(f.Fog.Height)*4 != uint64(len(f.Fog.RGBA)) {
		return u, r, fmt.Errorf("metalproto: invalid visual payload")
	}
	u = liveUpload{Visuals: pointer(p.visuals), Distortions: pointer(f.Distortions), DistortionCount: uint32(len(f.Distortions)), Fog: pointer(f.Fog.RGBA), FogWidth: uint32(f.Fog.Width), FogHeight: uint32(f.Fog.Height), FogRect: f.Fog.Rect, Previous: pointer(f.Previous), Current: pointer(f.Current), Offsets: pointer(p.offsets), Spans: pointer(p.spans), Sprites: pointer(f.Sprites), Lights: pointer(lights), Camera: [4]float32{f.Camera[0], f.Camera[1], f.Camera[2], 0}, Alpha: f.Alpha, PoseCount: uint32(len(f.Current)), InstanceCount: uint32(len(f.Instances)), SpriteCount: uint32(len(f.Sprites)), LightCount: uint32(len(lights)), Seconds: seconds}
	r.Seconds = seconds
	r.Alpha = f.Alpha
	r.Tick = f.Tick
	r.Instances = len(f.Instances)
	r.Poses = len(f.Current)
	r.Sprites = len(f.Sprites)
	r.Lights = len(lights)
	r.DroppedLights = len(f.Lights) - len(lights)
	r.Distortions = len(f.Distortions)
	r.FogBytes = len(f.Fog.RGBA)
	r.UploadBytes = uint64(len(f.Current))*128 + uint64(len(f.Instances))*68 + uint64(len(f.Distortions))*64 + uint64(r.FogBytes) + uint64(len(f.Sprites))*96 + uint64(len(lights))*32
	for i, span := range p.spans[:len(p.cursor)] {
		if span.Count == 0 || len(scene.Meshes[i].Indices) == 0 {
			continue
		}
		r.ModelDraws++
		r.ModelVertices += uint64(len(scene.Meshes[i].Vertices)) * uint64(span.Count)
		r.ModelIndices += uint64(len(scene.Meshes[i].Indices)) * uint64(span.Count)
	}
	if visualPasses {
		p.selectors = p.selectors[:0]
		for pass := 1; pass <= 2; pass++ {
			for mesh, span := range p.spans[:len(p.cursor)] {
				out := meshSpan{Start: uint32(len(p.selectors))}
				for j := uint32(0); j < span.Count; j++ {
					v := p.visuals[span.Start+j]
					eligible := v.State[3] > 0
					if pass == 2 {
						eligible = v.State[2] > 0 && v.Outline[3] > 0
					}
					if eligible {
						p.selectors = append(p.selectors, j)
						out.Count++
					}
				}
				p.spans[pass*len(p.cursor)+mesh] = out
				if pass == 1 {
					r.ShadowInstances += int(out.Count)
					r.ShadowIndices += uint64(len(scene.Meshes[mesh].Indices)) * uint64(out.Count)
					if out.Count > 0 && len(scene.Meshes[mesh].Indices) > 0 {
						r.ShadowDraws++
					}
				} else {
					r.Constructing += int(out.Count)
					r.OutlineIndices += uint64(len(scene.Meshes[mesh].EdgeIndices)) * uint64(out.Count)
					if out.Count > 0 && len(scene.Meshes[mesh].EdgeIndices) > 0 {
						r.OutlineDraws++
					}
				}
			}
		}
		u.Selectors = pointer(p.selectors)
		r.UploadBytes += uint64(len(p.selectors)) * 4
	}
	if r.Sprites > 0 {
		r.SpriteDraws = 1
	}
	return u, r, nil
}
