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
