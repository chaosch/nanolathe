package client

import "github.com/nanolathe-gg/nanolathe/internal/drawlist"

// RetainedWaterSurface borrows the ordinary presentation observer's water
// metadata, including its paused/interpolated phase and integrated current.
// Call with the presentation pinned, on the same owner as recording; this
// retains the existing metadata helper's observation contract (§26.1, §30).
func (c *Client) RetainedWaterSurface() drawlist.WaterSurface {
	return c.waterSurfaceMetadata()
}
