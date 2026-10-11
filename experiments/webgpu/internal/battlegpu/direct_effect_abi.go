package battlegpu

const layerDirectVertexStride = 64

// The pinned render HAL leaves runtime-array size arguments unbound. Declare
// this entry's exact uploaded buffer tier instead, as the model resolve does;
// guarded vertex loads then depend only on owned storage (GPU design C-G7).
func layerDirectShaderSource(capacity int) string {
	return layerABIOverlaySource([5]int{1, capacity, 1, 1, 1})
}
