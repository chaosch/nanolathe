package main

import (
	"reflect"
	"testing"
)

func TestCoverageGrowthPreservesSubjectsAndAlignsEndpoints(t *testing.T) {
	cases := coverageCasesWithPoseGrowth()
	if len(cases) != 19 {
		t.Fatal(len(cases))
	}
	before, grown, after := cases[16].frame(), cases[17].frame(), cases[18].frame()
	if len(grown.Current) != 40000 || len(grown.Previous) != 40000 || len(grown.ModelProjections.PieceFlags) != 40000 {
		t.Fatal("growth records are not aligned")
	}
	if !reflect.DeepEqual(before.Current, grown.Current[:len(before.Current)]) || !reflect.DeepEqual(before.Instances, grown.Instances) || !reflect.DeepEqual(before.Composition, grown.Composition) || !reflect.DeepEqual(before, after) {
		t.Fatal("growth changed visible subjects or did not return to the original packet")
	}
}
