//go:build darwin

package main

import (
	"fmt"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/metalhud"
)

// metalWaterSources reads the already recorded stock layers and already packed
// effect art. It does not record effects again or advance their private RNG.
// Run before another Prepare can replace the adapter's borrowed atlas identity.
// dst is reused storage from the previous draw; an empty result stays nil.
func metalWaterSources(dst []meshscene.WaterSource, cl *client.Client, adapter *metalhud.Foreground, layers []meshscene.EffectLayer) ([]meshscene.WaterSource, meshscene.WaterSourceCounts, error) {
	if cl == nil || adapter == nil {
		return nil, meshscene.WaterSourceCounts{}, fmt.Errorf("nanolathe: native water sources require client and prepared effects atlas")
	}
	if !cl.RetainedWaterSurface().Enabled {
		return nil, meshscene.WaterSourceCounts{}, nil
	}
	out, counts, err := meshscene.AppendWaterSources(dst, layers, cl.DisplayPalette(), adapter.GlowAtlas().Frame)
	if len(out) == 0 {
		out = nil
	}
	return out, counts, err
}
