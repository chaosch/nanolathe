//go:build darwin

package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/metalhud"
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
	labels  drawlist.List
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
	if cl == nil || adapter == nil {
		return metalEffectFrame{}, fmt.Errorf("nanolathe: native stock-effects recording requires client and atlas adapter")
	}
	return r.adapt(r.capture(cl), adapter)
}

// capture is the half of record that reads the client: the stock-effect
// producers and the unit labels, recorded into this recorder's storage.
func (r *metalEffectRecorder) capture(cl *client.Client) client.RetainedEffects {
	start := time.Now()
	recorded := cl.RecordRetainedEffectsInto(&r.storage)
	// Unit labels open the last layer: production draws them after the
	// airborne pass and before strip 9 [03 §1]. Empty unless the client
	// leaves them to the host (SetExternalSelection).
	cl.RecordRetainedLabels(&r.labels)
	r.recordMS = float64(time.Since(start)) / 1e6
	return recorded
}

// adapt is the half of record that never touches the client: it converts the
// captured lists, which this recorder's storage owns, into effect layers. It
// may therefore run beside the host's foreground recording.
func (r *metalEffectRecorder) adapt(recorded client.RetainedEffects, adapter *metalhud.Foreground) (metalEffectFrame, error) {
	var out metalEffectFrame
	start := time.Now()
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
	groups[4] = slices.Insert(groups[4], 0, metalhud.EffectBatch{List: &r.labels, Labels: true})
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
