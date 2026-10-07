package meshscene

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// Load prepares immutable retail art and a bounded committed-pose replay. The
// dense instance grid is presentation-only: its count is not a live battle
// census. No retained renderer state is supplied to the session [I6].
func Load(opts LoadOptions) (*Scene, error) {
	if opts.Root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		opts.Root = filepath.Join(home, "TotalAnnihilation")
	}
	if opts.Map == "" {
		opts.Map = "Greenhaven"
	}
	if opts.Units == 0 {
		opts.Units = 360
	}
	if opts.QuadMultiplier == 0 {
		opts.QuadMultiplier = 1
	}
	if opts.TextureScale == 0 {
		opts.TextureScale = 1
	}
	if opts.Samples == 0 {
		opts.Samples = 120
	}
	if opts.Units < 1 || opts.Units > 1500 || opts.Samples < 2 || opts.Samples > 240 || (opts.QuadMultiplier != 1 && opts.QuadMultiplier != 2 && opts.QuadMultiplier != 5) || (opts.TextureScale != 1 && opts.TextureScale != 2) {
		return nil, fmt.Errorf("nanolathe: prototype options invalid: logical path scene, providers searched [options], expected 1..1500 instances, 2..240 samples, geometry 1/2/5 and texture 1/2")
	}
	fs := vfs.New()
	defer fs.Close()
	if err := fs.MountGameDirectory(opts.Root); err != nil {
		return nil, err
	}
	pal, err := palette.Load(fs)
	if err != nil {
		return nil, err
	}
	cat, err := content.Compile(fs)
	if err != nil {
		return nil, err
	}
	mapName := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(opts.Map, `\`, "/"), "maps/"), ".ota"), ".tnt")
	tntPath := "maps/" + mapName + ".tnt"
	terrain, err := formats.LoadTNTFile(fs, tntPath)
	if err != nil {
		return nil, err
	}

	// These are authored unit names, not manufactured geometry. The two sides
	// cover walkers, vehicles and aircraft with different piece hierarchies.
	roster := []string{"armpeep", "corfink", "armpw", "corak", "armrock", "corstorm", "armstump", "corraid", "armham", "corthud", "armfav", "corfav"}
	models := make([]*model.Model, len(roster))
	for i, name := range roster {
		def, ok := cat.Unit(name)
		if !ok || def == nil {
			return nil, fmt.Errorf("nanolathe: prototype unit missing: logical path %s, providers searched [catalog], expected retail roster definition", name)
		}
		models[i], err = model.Load(fs, "objects3d/"+def.ObjectName+".3do")
		if err != nil {
			return nil, err
		}
	}
	art, err := buildAtlas(fs, models, pal, opts.TextureScale, opts.MaterialSource)
	if err != nil {
		return nil, err
	}
	scene := &Scene{Name: "retail committed-pose replay", Atlas: art.texture, ModelPalette: art.palette, Metadata: map[string]any{}}
	census := make([]meshCensus, len(models))
	for i, m := range models {
		scene.Meshes = append(scene.Meshes, compileMesh(m, art, pal, opts.QuadMultiplier, &census[i]))
		census[i].Unit = roster[i]
	}
	rows := int(math.Ceil(math.Sqrt(float64(opts.Units) / (16.0 / 9.0))))
	cols := (opts.Units + rows - 1) / rows
	const pitch = float32(42)
	extentW, extentH := float32(cols)*pitch, float32(rows)*pitch
	cx, cz := float32(terrain.Width*16)/2, float32(terrain.Height*16)/2
	scene.Camera = [3]float32{cx, cz, min(1440/extentW, 780/extentH)}
	scene.Terrain, scene.TerrainRect = bakeTerrain(terrain, pal, cx, cz, int(extentW)+256, int(extentH)+256)
	if err := samplePoses(scene, opts, fs, cat, roster, models, cols, rows, pitch, cx, cz, mapName); err != nil {
		return nil, err
	}
	meshVertices, meshIndices, meshEdges, drawnTriangles, pieceMatrices := 0, 0, 0, 0, 0
	for _, m := range scene.Meshes {
		meshVertices += len(m.Vertices)
		meshIndices += len(m.Indices)
		meshEdges += len(m.EdgeIndices)
	}
	instancesPerMesh := make([]int, len(models))
	for _, instance := range scene.Instances {
		instancesPerMesh[instance.Mesh]++
		drawnTriangles += len(scene.Meshes[instance.Mesh].Indices) / 3
		pieceMatrices += scene.Meshes[instance.Mesh].Pieces
	}
	info, _ := fs.Stat(tntPath)
	scene.Metadata["scene_kind"] = "presentation instancing of ordinary-session committed COB pose templates"
	scene.Metadata["rendered_instances"] = len(scene.Instances)
	scene.Metadata["instances_per_mesh"] = instancesPerMesh
	scene.Metadata["models"] = census
	scene.Metadata["geometry_multiplier"] = opts.QuadMultiplier
	scene.Metadata["geometry_policy"] = "spatial strip subdivision of authored quads; edge subdivision of triangle fans; unchanged artistic silhouette"
	scene.Metadata["retained_vertices"] = meshVertices
	scene.Metadata["retained_triangles"] = meshIndices / 3
	scene.Metadata["triangles_per_frame"] = drawnTriangles
	scene.Metadata["retained_mesh_bytes"] = meshVertices*64 + (meshIndices+meshEdges)*4
	scene.Metadata["matrices_per_sample"] = pieceMatrices
	scene.Metadata["pose_sample_bytes"] = pieceMatrices * 64 * len(scene.Frames)
	scene.Metadata["pose_samples"] = len(scene.Frames)
	scene.Metadata["pose_sample_hz"] = 30
	scene.Metadata["atlas"] = art.metadata
	scene.Metadata["terrain"] = map[string]any{"logical_path": tntPath, "provider": info.Source.ProviderID(), "source_width": terrain.Width * 16, "source_height": terrain.Height * 16, "crop_rect": scene.TerrainRect, "texture_width": scene.Terrain.Width, "texture_height": scene.Terrain.Height, "rgba_bytes": len(scene.Terrain.RGBA), "policy": "native TNT tile pixels baked to flat RGBA backdrop; no elevation, features, fog or shadows"}
	scene.Metadata["camera_fit_viewport"] = [2]int{1600, 900}
	scene.Metadata["render_limitations"] = []string{"orthographic prototype shading, not retail raster parity", "one frozen first GAF frame per texture; team logos use owner zero", "no particles, projectile meshes, construction effects, cargo, shadows or fog", "dense instances repeat sampled template trajectories at different grid positions"}
	return scene, nil
}

func bakeTerrain(t *formats.TNT, pal *palette.Tables, cx, cz float32, width, height int) (Texture, [4]float32) {
	fullW, fullH := int(t.TileMapWidth)*32, int(t.TileMapHeight)*32
	width, height = min(max(width, 1600), fullW), min(max(height, 900), fullH)
	x0, z0 := min(max(int(cx)-width/2, 0), fullW-width), min(max(int(cz)-height/2, 0), fullH-height)
	texture := Texture{Width: width, Height: height, RGBA: make([]byte, width*height*4)}
	for z := 0; z < height; z++ {
		for x := 0; x < width; x++ {
			sx, sz := x+x0, z+z0
			tile := int(t.TileIndices[(sz/32)*int(t.TileMapWidth)+sx/32])
			index := t.TileGraphics[tile*1024+(sz%32)*32+sx%32]
			rgba := pal.Base[index]
			offset := (z*width + x) * 4
			copy(texture.RGBA[offset:offset+3], rgba[:3])
			texture.RGBA[offset+3] = 255
		}
	}
	return texture, [4]float32{float32(x0), float32(z0), float32(x0 + width), float32(z0 + height)}
}
