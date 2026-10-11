package battlegpu

import (
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"testing"
)

func TestBattlePaintPreservesFeatureShadowAndBodyBetweenSubjects(t *testing.T) {
	f := meshscene.LiveFrame{Composition: meshscene.ModelComposition{
		Paint:          []meshscene.ModelPaint{{Slot: 2, Rank: 3, Phase: 1}, {Slot: 1, Rank: 1, Phase: 1}, {Slot: 3, Rank: 0, Phase: 3}},
		FeatureSprites: []meshscene.FeatureSpritePaint{{Source: 0, Rank: 2, Phase: 1, Shadow: true}, {Source: 4, Rank: 2, Phase: 1}},
	}}
	got := battlePaintOrder(nil, f)
	if len(got) != 5 || got[0].slot != 1 || !got[1].feature || got[1].source != 0 || !got[2].feature || got[2].source != 4 || got[3].slot != 2 || got[4].phase != 3 {
		t.Fatalf("paint walk: %+v", got)
	}
}
