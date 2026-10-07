//go:build darwin

package main

import (
	"fmt"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/metalhud"
)

// metalEffectFrame borrows the effects adapter's atlas/quads until its next
// Prepare. Geometry and art remain owned by the source recording, independent
// of the client's next HUD recording. Native drawing must honor each layer's
// Ops and report Models it cannot yet submit, rather than silently dropping
// direct fragments/debris [DESIGN_GPU_RENDERER C-G3, C-G5].
type metalEffectFrame struct {
	BeforeFeatures, BeforeGround, AfterGround, AfterAir, AfterLabels meshscene.EffectLayer
	Atlas                                                            meshscene.Texture
	Version                                                          uint64
	Dirty                                                            [4]int
	ProductionEffects                                                client.EffectDrawStats
	ProductionStrips                                                 client.StripDrawStats
	LightSources                                                     client.RetainedLightSources
}

// metalEffectRecorder keeps one presenter's effect copies and adapter inputs
// between draws. Each record overwrites the previous draw's batches, which the
// native submission has already copied [I6].
type metalEffectRecorder struct {
	storage client.RetainedEffectStorage
	groups  [5][]metalhud.EffectBatch
	orders  [][]meshscene.EffectProjectileModel
	// recordMS and adaptMS split one record call (diagnostic).
	recordMS, adaptMS float64
}

// record must run once per presented pinned frame, after the client's
// BeginPresentationFrame and before recording its foreground. Give it a
// dedicated adapter, so HUD Prepare cannot replace borrowed effect quads or
// lose the aggregated effects-atlas upload rectangle.
func (r *metalEffectRecorder) record(cl *client.Client, adapter *metalhud.Foreground) (metalEffectFrame, error) {
	var out metalEffectFrame
	if cl == nil || adapter == nil {
		return out, fmt.Errorf("nanolathe: native stock-effects recording requires client and atlas adapter")
	}
	start := time.Now()
	recorded := cl.RecordRetainedEffectsInto(&r.storage)
	r.recordMS = float64(time.Since(start)) / 1e6
	start = time.Now()
	groups := r.groups[:]
	order := 0
	for i, batches := range [][]client.RetainedEffectBatch{recorded.BeforeFeatures, recorded.BeforeGround, recorded.AfterGround, recorded.AfterAir, recorded.AfterLabels} {
		groups[i] = groups[i][:0]
		for j := range batches {
			if order == len(r.orders) {
				r.orders = append(r.orders, nil)
			}
			converted := r.orders[order][:0]
			for _, e := range batches[j].ProjectileOrder {
				converted = append(converted, meshscene.EffectProjectileModel{ID: e.ID, Member: e.Member})
			}
			r.orders[order] = converted
			order++
			groups[i] = append(groups[i], metalhud.EffectBatch{List: &batches[j].List, ProjectileModels: batches[j].ProjectileModels, ProjectileOrder: converted})
		}
	}
	groups[0] = append(groups[0], metalhud.EffectBatch{List: &recorded.LightSources.Short, SourceOnly: true})
	groups[1] = append(groups[1], metalhud.EffectBatch{List: &recorded.LightSources.Ground, SourceOnly: true})
	layers, atlas, version, err := adapter.PrepareEffectLayers(groups)
	r.adaptMS = float64(time.Since(start)) / 1e6
	out.Atlas, out.Version, out.Dirty = atlas, version, adapter.DirtyRect()
	out.ProductionEffects, out.ProductionStrips = recorded.Effects, recorded.Strips
	out.LightSources = recorded.LightSources
	if len(layers) == len(groups) {
		out.BeforeFeatures, out.BeforeGround, out.AfterGround, out.AfterAir, out.AfterLabels = layers[0], layers[1], layers[2], layers[3], layers[4]
	}
	return out, err
}
