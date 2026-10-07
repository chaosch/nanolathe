//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"math"
	"unsafe"
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
