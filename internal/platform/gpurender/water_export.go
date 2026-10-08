package gpurender

import "slices"

// UploadRGBA exports an owned copy of the existing projected mask for a
// retained executor. Construction and channel meanings remain those of
// BuildWaterMask (DESIGN_GPU_RENDERER §26.1, §26.3).
func (m *PreparedWaterMask) UploadRGBA() (rgba []byte, width, height, step int) {
	if m == nil {
		return nil, 0, 0, 0
	}
	return slices.Clone(m.pixels), m.w, m.h, m.step
}

// UploadBlocks exports the mask's block index, one flag per block of size
// painted-map pixels that holds water, as visibleWater reads it (§26.1).
func (m *PreparedWaterMask) UploadBlocks() (blocks []bool, width, height, size int) {
	if m == nil {
		return nil, 0, 0, 0
	}
	return slices.Clone(m.blocks), m.blockW, m.blockH, waterBlockSize
}
