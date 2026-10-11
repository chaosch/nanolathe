package battlegpu

import (
	"strconv"
	"strings"
)

const layerDirectVertexStride = 64

// The pinned render HAL leaves runtime-array size arguments unbound. Declare
// this entry's exact uploaded buffer tier instead, as the model resolve does;
// guarded vertex loads then depend only on owned storage (GPU design C-G7).
func layerDirectShaderSource(capacity int) string {
	return strings.Replace(layerOverlayShader, "array<LayerVertex,1>", "array<LayerVertex,"+strconv.Itoa(max(capacity, 1))+">", 1)
}

func (l *Layers) layerUpdateDirectPipeline() error {
	capacity := max(l.effectVertices.size/layerDirectVertexStride, 1)
	if capacity == l.directCapacity {
		return nil
	}
	shader, err := l.layerModule(layerDirectShaderSource(capacity))
	if err != nil {
		return err
	}
	pipeline, err := l.layerPipeline(shader, l.directLayout, "layer_triangle_vertex", "layer_geometry_fragment", 0, 1)
	if err != nil {
		return err
	}
	// Buffer tiers only grow. Prior tier modules/pipelines remain completion-safe
	// in Layers' bounded owned list and are released by Close.
	l.geometry, l.directCapacity = pipeline, capacity
	return nil
}
