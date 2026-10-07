//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Concatenate after the base shaders: this source uses their Vertex, Uniforms,
// ModelSlot, ModelRules and project helper, plus the shared water field.
//
//go:embed native/water_objects.metal
var waterObjectsShaderSource string

type nativeWaterObjectsUpload struct {
	Objects, Seabed          uintptr
	ObjectCount, SeabedCount uint32
	Controls                 [4]float32
}
type waterObjectsPacking struct {
	objects []meshscene.WaterObject
	seabed  []uint32
	sources []meshscene.WaterSource
}

// Prepare copies per-instance admission and already-native seabed sprite
// indices (zero based). The host removes promoted sprites from ordinary paint
// and draws them before the water surface. RecordScale/EffectiveScale are the
// production logical recording and effective camera scales in world pixels;
// outputScale converts those to physical pixels exactly once.
func (p *waterObjectsPacking) Prepare(objects []meshscene.WaterObject, seabed []uint32, recordScale, effectiveScale, outputScale float32) (nativeWaterObjectsUpload, error) {
	var out nativeWaterObjectsUpload
	if unsafe.Sizeof(out) != 40 || unsafe.Sizeof(meshscene.WaterObject{}) != 16 {
		return out, fmt.Errorf("metalproto: water-objects ABI mismatch")
	}
	for _, v := range []float32{recordScale, effectiveScale, outputScale} {
		if v <= 0 || math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return out, fmt.Errorf("metalproto: invalid water-object scale")
		}
	}
	if len(objects) > math.MaxInt32 || len(seabed) > math.MaxInt32 {
		return out, fmt.Errorf("metalproto: water-object count exceeds native range")
	}
	p.objects = append(p.objects[:0], objects...)
	p.seabed = append(p.seabed[:0], seabed...)
	for _, o := range p.objects {
		for _, v := range o.Params {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return out, fmt.Errorf("metalproto: invalid water-object parameter")
			}
		}
	}
	out.Objects, out.Seabed = pointer(p.objects), pointer(p.seabed)
	out.ObjectCount, out.SeabedCount = uint32(len(objects)), uint32(len(seabed))
	out.Controls = [4]float32{recordScale * outputScale, effectiveScale * outputScale, outputScale, 0}
	return out, nil
}

// nativeWaterSourcesUpload is separate so the 40-byte object descriptor stays
// stable. Each source is a four-corner quad; native cap accounting counts these
// corners, not the six fan-triangle vertices submitted to the GPU.
type nativeWaterSourcesUpload struct {
	Sources               uintptr
	VertexCount, Reserved uint32
}

func (p *waterObjectsPacking) PrepareSources(sources []meshscene.WaterSource, scale float32) (nativeWaterSourcesUpload, error) {
	var out nativeWaterSourcesUpload
	if unsafe.Sizeof(out) != 16 || unsafe.Sizeof(meshscene.WaterSourceVertex{}) != 64 || unsafe.Sizeof(meshscene.WaterSource{}) != 256 {
		return out, fmt.Errorf("metalproto: water-source ABI mismatch")
	}
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) || len(sources) > math.MaxInt32/4 {
		return out, fmt.Errorf("metalproto: invalid water-source scale or count")
	}
	p.sources = append(p.sources[:0], sources...)
	for i := range p.sources {
		for j := range p.sources[i].Vertices {
			v := &p.sources[i].Vertices[j]
			for _, group := range [][4]float32{v.PositionHeight, v.UV, v.Color, v.Bounds} {
				for _, value := range group {
					if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
						return out, fmt.Errorf("metalproto: nonfinite water-source vertex")
					}
				}
			}
			if v.PositionHeight[3] != 1 && v.PositionHeight[3] != 2 {
				return out, fmt.Errorf("metalproto: invalid water-source kind")
			}
			if v.PositionHeight[3] != p.sources[i].Vertices[0].PositionHeight[3] {
				return out, fmt.Errorf("metalproto: inconsistent water-source quad kind")
			}
			v.PositionHeight[0] *= scale
			v.PositionHeight[1] *= scale
		}
	}
	out.Sources, out.VertexCount = pointer(p.sources), uint32(len(p.sources)*4)
	return out, nil
}
