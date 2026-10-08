//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/platform/gpurender"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// Preparation runs while composition exclusively owns the terrain, before the
// asynchronous playable worker starts. The production mask builder stays sole
// owner of shoreline classification (GPU design §26.1).
func prepareMetalScene(scene *meshscene.Scene, terrain *world.Terrain) {
	if scene == nil || terrain == nil {
		return
	}
	mask := gpurender.BuildWaterMask(terrain)
	pixels, w, h, step := mask.UploadRGBA()
	scene.WaterMask = meshscene.Texture{Width: w, Height: h, RGBA: pixels}
	scene.WaterMaskStep = step
	scene.WaterBlocks, scene.WaterBlocksW, scene.WaterBlocksH, scene.WaterBlockSize = mask.UploadBlocks()
	scene.WaterLava = terrain.LavaWorld
	scene.WaterDamaging = terrain.WaterDoesDamage != 0 && terrain.WaterDamage != 0
}

func prepareMetalFrame(cl *client.Client, world *meshscene.RetainedBattle, scene *meshscene.Scene, live *meshscene.LiveFrame, current *frame.Frame, width, height int, subjects *metalSubjectRules) {
	live.LightControls &= 1
	if cl.Effects().Finish {
		live.LightControls |= 2
	}
	if cl.Effects().Glint {
		live.LightControls |= 4
	}
	if cl.Effects().Supersample {
		live.LightControls |= 8
	}
	// Rules and water verdicts belong to a build; a Recull keeps both.
	if live.ModelRules == nil && current != nil {
		subjects.index(current)
		live.ModelRules = subjects.modelRules(cl, world, live, current)
		live.WaterObjects = subjects.waterObjects(cl, world, live, current)
	}
	z := live.Camera[2]
	if z <= 0 {
		return
	}
	cx, cy := live.Camera[0], live.Camera[1]
	dx, dy := float32(width)/(2*z), float32(height)/(2*z)
	half := float32(.5) / z
	sample := [4]float32{cx - dx + half, cy - dy + half, cx + dx - half, cy + dy - half}
	live.Water = meshscene.NewWaterUniform(cl.RetainedWaterSurface(), cl.Effects(), scene.WaterLava, scene.WaterDamaging, scene.WaterMask.Width, scene.WaterMask.Height, scene.WaterMaskStep, scene.TerrainRect, sample)
	// Production runs the surface, seabed treatment and reflections only when
	// water lies within one block of the view (GPU design §26.1).
	if metalWaterVisible(scene, cx-dx, cy-dy, cx+dx, cy+dy) {
		live.Water.Controls[3] = 1
	} else {
		live.Water.Mask[3] = 0
	}
}

// metalWaterVisible is production's visibleWater over the view rectangle in
// painted-map pixels: one block of slack on each side, because the damp
// shoreline band lies on dry ground beside the water.
func metalWaterVisible(scene *meshscene.Scene, x0, y0, x1, y1 float32) bool {
	size, bw, bh := scene.WaterBlockSize, scene.WaterBlocksW, scene.WaterBlocksH
	if size <= 0 || bw <= 0 || bh <= 0 || len(scene.WaterBlocks) < bw*bh {
		return false
	}
	block := func(v float32) int { return numeric.FloorDiv(int(v), size) }
	bx0, by0 := max(block(x0)-1, 0), max(block(y0)-1, 0)
	bx1, by1 := min(block(x1)+1, bw-1), min(block(y1)+1, bh-1)
	for y := by0; y <= by1; y++ {
		for x := bx0; x <= bx1; x++ {
			if scene.WaterBlocks[y*bw+x] {
				return true
			}
		}
	}
	return false
}
