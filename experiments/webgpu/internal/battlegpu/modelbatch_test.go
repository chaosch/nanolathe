package battlegpu

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

func TestModelBatchPreservesRecordsShadowClonesAndPainter(t *testing.T) {
	f := meshscene.LiveFrame{Composed: true, Instances: make([]meshscene.Instance, 6), Composition: meshscene.ModelComposition{InstanceSlots: []uint32{1, 2, 3, 0, 4, 5}, Slots: make([]meshscene.ModelSlot, 5)}}
	m := &Models{draws: []modelDraw{{mesh: 2, instance: 0}, {mesh: 1, instance: 1}, {mesh: 2, instance: 2, outline: true}, {mesh: 2, instance: 4}, {mesh: 1, instance: 5, outline: true}}, instanceBytes: make([]byte, 8*modelInstanceStride), paint: []modelPaint{{3, 1}, {1, 2}, {5, 1}}, shadowDraws: []modelDraw{{mesh: 2, instance: 6}, {mesh: 1, instance: 7}}}
	for i := range 8 {
		for j := range modelInstanceStride {
			m.instanceBytes[i*modelInstanceStride+j] = byte(i*31 + j)
		}
	}
	original := bytes.Clone(m.instanceBytes)
	paint, shadows := append([]modelPaint(nil), m.paint...), append([]modelDraw(nil), m.shadowDraws...)
	m.modelBatchSources(f)
	if !m.batchWork.independent {
		t.Fatal("unique source selectors not batched")
	}
	for i, old := range []int{5, 1, 2, 0, 4, 3, 6, 7} {
		if !bytes.Equal(m.instanceBytes[i*modelInstanceStride:(i+1)*modelInstanceStride], original[old*modelInstanceStride:(old+1)*modelInstanceStride]) {
			t.Fatalf("record %d lost original %d fields", i, old)
		}
	}
	if !reflect.DeepEqual(m.paint, paint) || !reflect.DeepEqual(m.shadowDraws, shadows) {
		t.Fatal("source batching changed painter or shadow sequence")
	}
	if modelDrawRun(m.draws, 0, false, true) != 2 || modelDrawRun(m.draws, 2, false, true) != 5 {
		t.Fatal("repeated meshes do not form full body runs")
	}
}

func TestModelBatchAliasAndDiagnosticRetainChronology(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		f := meshscene.LiveFrame{Composed: true, Instances: make([]meshscene.Instance, 3), Composition: meshscene.ModelComposition{InstanceSlots: []uint32{1, 2, 1}, Slots: make([]meshscene.ModelSlot, 2)}}
		m := &Models{draws: []modelDraw{{mesh: 2, instance: 0, outline: true}, {mesh: 1, instance: 1}, {mesh: 2, instance: 2}}, instanceBytes: make([]byte, 3*modelInstanceStride), disableModelBatching: disabled}
		before := append([]modelDraw(nil), m.draws...)
		m.modelBatchSources(f)
		if m.batchWork.independent || !reflect.DeepEqual(before, m.draws) {
			t.Fatalf("alias/disabled source changed order: disabled=%v", disabled)
		}
	}
}

func TestModelShadowAndCopyRunsPreserveOrderBarriers(t *testing.T) {
	draws := []modelDraw{{mesh: 1, instance: 8, outline: true}, {mesh: 1, instance: 9, outline: true}, {mesh: 2, instance: 10, outline: true}, {mesh: 1, instance: 11}, {mesh: 1, instance: 12, outline: true}, {mesh: 1, instance: 14, outline: true}}
	if modelDrawRun(draws, 0, false, true) != 2 || modelDrawRun(draws, 2, false, true) != 3 || modelDrawRun(draws, 3, false, true) != 5 || modelDrawRun(draws, 3, true, true) != 4 || modelDrawRun(draws, 4, true, true) != 5 || modelDrawRun(draws, 0, false, false) != 1 {
		t.Fatal("shadow run crossed mesh, record, outline or diagnostic barrier")
	}
	slots := []uint32{2, 3, 6, 7, 7, 8}
	if modelSlotRun(slots, 0, true) != 2 || modelSlotRun(slots, 2, true) != 4 || modelSlotRun(slots, 4, true) != 6 || modelSlotRun(slots, 0, false) != 1 {
		t.Fatal("copy run crossed selector discontinuity")
	}
}
