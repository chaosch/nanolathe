//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"math"
	"unsafe"
)

//go:embed native/ground_effects.metal
var groundEffectsShaderSource string

type nativeGroundUpload struct {
	Marks           uintptr
	Count, Reserved uint32
	Ages            [4]float32
}
type groundEffectsPacking struct{ marks []meshscene.EffectGround }

// Prepare uses the same layer traversal as effectsPacking's frame-global ground
// markers. Coordinates/mapping are scaled once into physical framebuffer pixels.
// The descriptor borrows packing storage through the synchronous native copy.
func (p *groundEffectsPacking) Prepare(layers []meshscene.EffectLayer, scale float32) (nativeGroundUpload, error) {
	var out nativeGroundUpload
	if unsafe.Sizeof(out) != 32 || unsafe.Sizeof(meshscene.EffectGround{}) != 80 {
		return out, fmt.Errorf("metalproto: ground-effects ABI mismatch")
	}
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return out, fmt.Errorf("metalproto: invalid ground-effects scale")
	}
	p.marks = p.marks[:0]
	for _, layer := range layers {
		for _, m := range layer.Ground {
			for i := range m.CentreAxis {
				m.CentreAxis[i] *= scale
			}
			m.CrossKind[0] *= scale
			m.CrossKind[1] *= scale
			m.Mapping[2] /= scale
			for i := range m.Clip {
				m.Clip[i] *= scale
			}
			p.marks = append(p.marks, m)
		}
	}
	if len(p.marks) > math.MaxInt32 {
		return out, fmt.Errorf("metalproto: ground-effects count exceeds native range")
	}
	out.Marks, out.Count = pointer(p.marks), uint32(len(p.marks))
	out.Ages = [4]float32{drawlist.ScorchFadeStartTicks, drawlist.ScorchLifeTicks, 0, 0}
	return out, nil
}
