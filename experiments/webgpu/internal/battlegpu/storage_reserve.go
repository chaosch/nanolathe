package battlegpu

import "os"

// This diagnostic moves fixed-array pipeline compilation to construction.
// Floors reserve buffers only; normal growth remains available and textures
// and immutable geometry stay shared. Every floor is below core binding limits.
func storageReserveEnabled() bool { return os.Getenv("NANOLATHE_WEBGPU_RESERVE_STORAGE") == "1" }

// Endpoints hold two pose records for each interpolated pose. Selected material
// records are the largest family; lights have the producer's complete 64 budget.
var modelStorageReserveBytes = [9]int{4 << 20, 2 << 20, 2 << 20, 8 << 20, 4096, 512 << 10, 1 << 20, 512 << 10, 512 << 10}

const glowStorageReserveBytes = 512 << 10

func (l *Layers) storageFloor(dst *layerBuffer) int {
	switch dst {
	case &l.sprites, &l.annotations:
		return 256 << 10
	case &l.overlayQuads:
		return 64 << 10
	case &l.effectQuads, &l.effectGround:
		return 128 << 10
	case &l.effectVertices:
		return 1 << 20
	case &l.effectSmoke:
		return 512 << 10
	case &l.lights:
		return 4096
	}
	return 0
}
