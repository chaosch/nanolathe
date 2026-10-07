//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/platform/gpurender"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// Preparation runs while composition exclusively owns the terrain, before the
// asynchronous playable worker starts. The production mask builder stays sole
// owner of shoreline classification (GPU design §26.1).
func prepareMetalScene(scene *meshscene.Scene, terrain *world.Terrain) {
	if scene == nil || terrain == nil {
		return
	}
	pixels, w, h, step := gpurender.BuildWaterMask(terrain).UploadRGBA()
	scene.WaterMask = meshscene.Texture{Width: w, Height: h, RGBA: pixels}
	scene.WaterMaskStep = step
	scene.WaterLava = terrain.LavaWorld
	scene.WaterDamaging = terrain.WaterDoesDamage != 0 && terrain.WaterDamage != 0
}

func prepareMetalFrame(cl *client.Client, world *meshscene.RetainedBattle, scene *meshscene.Scene, live *meshscene.LiveFrame, current *frame.Frame, width, height int) {
	if live.ModelRules == nil {
		live.ModelRules = applyMetalModelRules(cl, world, live, current)
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
}
