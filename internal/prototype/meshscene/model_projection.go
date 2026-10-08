package meshscene

import (
	"fmt"
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

const (
	ModelProjectionKnown uint32 = 1 << iota
	ModelProjectionDirectAll
)

const (
	ModelProjectionPieceDirect uint32 = 1 << iota
	ModelProjectionPieceUnknown
)

// ModelProjection is three float4 groups (48 bytes). Origin holds world XYZ
// and ModelProjection flags. Anchors holds native XY and doubled XY in the
// production record space. Control holds model record step, doubled-geometry
// gate, physical pixels per record pixel and reserved. Geometry quantization
// precedes the post-record scale; output scale must never enter the floor.
type ModelProjection struct{ Origin, Anchors, Control [4]float32 }

// ModelProjectionFrame records follow original LiveFrame.Instances order;
// PieceFlags follow the original pose buffer, never the mesh-packed instances.
// View is record camera X/Z and post-record physical translation X/Y.
type ModelProjectionFrame struct {
	Records    []ModelProjection
	PieceFlags []uint32
	View       [4]float32
}

// ModelProjectionView converts only presentation transforms. modelScale is the
// client's modelScale, while World.Step is the world recording scale (§14.2).
// Native Metal uses the Enhanced lane; Original's separate image-blit scale
// is deliberately not admitted by the client exporter.
func ModelProjectionView(world drawlist.WorldSpace, cam [2]int32, modelScale float32, doubled bool, outputScale float32) (control, view [4]float32, err error) {
	record := float32(camera.ZoomOf(world.Step)) / float32(camera.ZoomUnit)
	factor := record
	if world.Factor > 0 {
		factor = world.Factor
	} else if world.Zoom > 0 {
		factor = float32(world.Zoom) / float32(camera.ZoomUnit)
	}
	for _, v := range []float32{modelScale, record, factor, outputScale} {
		if v <= 0 || math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return control, view, modelProjectionError("finite positive recording and output scales")
		}
	}
	if modelScale != 1 && modelScale != 2 {
		return control, view, modelProjectionError("production model record step 1 or 2")
	}
	control = [4]float32{modelScale, 0, factor / record * outputScale, 0}
	if doubled {
		control[1] = 1
	}
	view = [4]float32{float32(cam[0]), float32(cam[1]), world.OffsetX * outputScale, world.OffsetY * outputScale}
	return
}

func NewModelProjection(origin [3]numeric.Fixed, native, doubled [2]int32, flags uint32, control [4]float32) ModelProjection {
	return ModelProjection{
		Origin:  [4]float32{float32(origin[0]) / 65536, float32(origin[1]) / 65536, float32(origin[2]) / 65536, float32(flags)},
		Anchors: [4]float32{float32(native[0]), float32(native[1]), float32(doubled[0]), float32(doubled[1])},
		Control: control,
	}
}

// ApplyModelProjections copies a source-only callback into retained instance
// and pose order, reusing dst's storage. resolve receives the subject's
// zeroed model-piece flags to fill. Call after Frame and before another
// Frame. Unsupported families have unknown records and keep existing world
// projection.
func (r *RetainedBattle) ApplyModelProjections(dst *ModelProjectionFrame, f *LiveFrame, view [4]float32, resolve func(kind uint8, id uint64, pieces []uint32) (ModelProjection, error)) error {
	dst.View = view
	dst.Records, dst.PieceFlags = dst.Records[:0], dst.PieceFlags[:0]
	if f == nil {
		return nil
	}
	dst.Records = resizeComposition(dst.Records, len(f.Instances))
	clear(dst.Records)
	dst.PieceFlags = resizeComposition(dst.PieceFlags, len(f.Current))
	clear(dst.PieceFlags)
	if resolve == nil {
		return nil
	}
	var err error
	r.VisitSubjects(f, func(i int, kind uint8, id uint64, offset, count int) {
		if err != nil {
			return
		}
		if offset < 0 || offset+count > len(dst.PieceFlags) {
			err = modelProjectionError("flags aligned to subject pose span")
			return
		}
		dst.Records[i], err = resolve(kind, id, dst.PieceFlags[offset:offset+count])
	})
	return err
}

func modelProjectionError(expected string) error {
	return fmt.Errorf("nanolathe: retained projection failed: logical path model projection metadata, providers searched [production client retained subject identities], expected %s", expected)
}
