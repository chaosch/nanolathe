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
	Marks             unsafe.Pointer
	Count, Reserved   uint32
	Ages              [4]float32
	Frames            unsafe.Pointer
	FrameCount, Spare uint32
}

// groundMark and groundFrame are NMGroundMark and NMGroundFrame: a recording's
// marks share one mapping and clip, which CrossKind[3] indexes.
type groundMark struct{ CentreAxis, CrossKind, Values [4]float32 }
type groundFrame struct{ Mapping, Clip [4]float32 }
type groundEffectsPacking struct {
	marks  []groundMark
	frames []groundFrame
}

// Prepare uses the same layer traversal as effectsPacking's frame-global ground
// markers. Coordinates/mapping are scaled once into physical framebuffer pixels.
// The descriptor borrows packing storage through the synchronous native copy.
func (p *groundEffectsPacking) Prepare(layers []meshscene.EffectLayer, scale, alpha float32) (nativeGroundUpload, error) {
	var out nativeGroundUpload
	if unsafe.Sizeof(out) != 48 || unsafe.Sizeof(groundMark{}) != 48 || unsafe.Sizeof(groundFrame{}) != 32 {
		return out, fmt.Errorf("metalproto: ground-effects ABI mismatch")
	}
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return out, fmt.Errorf("metalproto: invalid ground-effects scale")
	}
	p.marks, p.frames = p.marks[:0], p.frames[:0]
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
			// A scorch's age is its whole ticks plus the frame's tick
			// fraction (§29). The shader adds the fraction back, the same
			// float32 sum, so the marks stay constant through a tick; an age
			// that is not that sum (an arrival mark) keeps its value.
			if m.CrossKind[2] == 1 && m.Values[3] == 0 {
				whole := float32(math.Round(float64(m.Values[0] - alpha)))
				if whole+alpha == m.Values[0] {
					m.Values[0], m.Values[3] = whole, 1
				}
			}
			frame := groundFrame{m.Mapping, m.Clip}
			if n := len(p.frames); n == 0 || p.frames[n-1] != frame {
				p.frames = append(p.frames, frame)
			}
			m.CrossKind[3] = float32(len(p.frames) - 1)
			p.marks = append(p.marks, groundMark{m.CentreAxis, m.CrossKind, m.Values})
		}
	}
	if len(p.marks) > math.MaxInt32 || len(p.frames) > 1<<24 {
		return out, fmt.Errorf("metalproto: ground-effects count exceeds native range")
	}
	out.Marks, out.Count = pointer(p.marks), uint32(len(p.marks))
	out.Frames, out.FrameCount = pointer(p.frames), uint32(len(p.frames))
	out.Ages = [4]float32{drawlist.ScorchFadeStartTicks, drawlist.ScorchLifeTicks, alpha, 0}
	return out, nil
}
