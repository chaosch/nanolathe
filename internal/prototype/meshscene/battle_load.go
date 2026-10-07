package meshscene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/headless"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/visibility"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// LoadBattle retains art before its live worker starts. The ordinary three-army
// fixture owns all gameplay; only completed frame copies become render inputs
// (docs/SIM_BENCHMARK.md; DESIGN_GPU_RENDERER §13.5; I6).
func LoadBattle(opts BattleOptions) (_ *Scene, resultErr error) {
	if opts.Root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		opts.Root = filepath.Join(home, "TotalAnnihilation")
	}
	if opts.Map == "" {
		opts.Map = headless.SimBenchDefaultMap
	}
	if opts.ArmySize == 0 {
		opts.ArmySize = headless.SimBenchDefaultArmySize
	}
	if opts.Width == 0 {
		opts.Width = 1920
	}
	if opts.Height == 0 {
		opts.Height = 1080
	}
	if opts.Zoom == 0 {
		opts.Zoom = 0.75
	}
	if opts.QuadMultiplier == 0 {
		opts.QuadMultiplier = 1
	}
	if opts.TextureScale == 0 {
		opts.TextureScale = 1
	}
	if opts.ViewPlayer < -1 || opts.ViewPlayer > 9 || opts.ArmySize < 250 || opts.ArmySize > 1000 || opts.Preticks < 0 || opts.Preticks > 36000 || opts.Width < 1 || opts.Height < 1 || opts.Zoom < 0.1 || opts.Zoom > 4 || (opts.QuadMultiplier != 1 && opts.QuadMultiplier != 2 && opts.QuadMultiplier != 5) || (opts.TextureScale != 1 && opts.TextureScale != 2) {
		return nil, fmt.Errorf("nanolathe: live prototype options invalid: logical path battle, providers searched [options], expected view -1..9, army 250..1000, preticks 0..36000, positive viewport, zoom 0.1..4, geometry 1/2/5 and textures 1/2")
	}
	fs := vfs.New()
	defer func() {
		if resultErr != nil {
			fs.Close()
		}
	}()
	if err := fs.MountGameDirectory(opts.Root); err != nil {
		return nil, err
	}
	cat, err := content.Compile(fs)
	if err != nil {
		return nil, err
	}
	pal, err := palette.Load(fs)
	if err != nil {
		return nil, err
	}
	mapName := battleModelMapName(opts.Map)
	composed, fixture, err := headless.ComposeSimBenchBattle(headless.SimBenchOptions{Map: mapName, ArmySize: opts.ArmySize, UnitLimit: max(headless.SimBenchDefaultUnitLimit, opts.ArmySize+1), Gameplay: gameplay.Modern, Seed: opts.Seed, Difficulty: 1}, fs, cat)
	if err != nil {
		return nil, err
	}
	sess := composed.Session
	// The camera policy selects an existing session view; the default spectator
	// uses the production field host's ordinary +nowisee command [I6].
	if opts.ViewPlayer < 0 {
		if err := sess.EnqueueHumanCommand(session.HumanCommand{Kind: session.HumanVisibility, Visibility: session.HumanVisibilityCommand{ClearMask: visibility.ModeHistoryEnabled | visibility.ModeCurrentEnabled}}); err != nil {
			return nil, err
		}
	} else {
		if err := sess.EnqueueHumanCommand(session.HumanCommand{Kind: session.HumanView, View: session.HumanViewCommand{Player: uint8(opts.ViewPlayer)}}); err != nil {
			return nil, err
		}
	}
	warmStart := time.Now()
	var drained []frame.EventView
	for sess.Clock.GlobalTick < uint32(opts.Preticks) || sess.Snapshot.Current() == nil {
		if err := battleStep(sess); err != nil {
			return nil, err
		}
		drained = sess.Snapshot.DrainCommittedEvents(drained)
	}
	warmDuration := time.Since(warmStart)
	if opts.ViewPlayer >= 0 && sess.Snapshot.Current().ViewingPlayer != uint8(opts.ViewPlayer) {
		return nil, fmt.Errorf("nanolathe: live prototype view unavailable: logical path player %d, providers searched [session player records], expected present active participant", opts.ViewPlayer)
	}
	cx, cz := float32(fixture.CentreX), float32(fixture.CentreZ)
	originY := float32(0) // Live geometry retains published world height; screen Y uses z-y/2.

	// Preload the complete mounted objects3d namespace, including otherwise
	// unused projectile and corpse art. A future birth never performs GPU upload.
	paths := []string{}
	seen := map[string]bool{}
	for _, entry := range fs.Entries() {
		path := strings.ToLower(strings.ReplaceAll(entry.Path, `\`, "/"))
		if strings.HasPrefix(path, "objects3d/") && strings.HasSuffix(path, ".3do") && !seen[path] {
			seen[path] = true
			paths = append(paths, entry.Path)
		}
	}
	sort.Slice(paths, func(i, j int) bool { return strings.ToLower(paths[i]) < strings.ToLower(paths[j]) })
	models := []*model.Model{}
	failures := []map[string]any{}
	modelIndices := map[string]int{}
	for _, path := range paths {
		loaded, loadErr := model.Load(fs, path)
		if loadErr != nil {
			failures = append(failures, map[string]any{"logical_path": path, "error": loadErr.Error()})
			continue
		}
		modelIndices[battleModelKey(path)] = len(models)
		models = append(models, loaded)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("nanolathe: live prototype models missing: logical path objects3d, providers searched [mounted content], expected retained 3DO meshes")
	}
	atlas, err := buildAtlas(fs, models, pal, opts.TextureScale)
	if err != nil {
		return nil, err
	}
	atlas.useMaterials = true
	scene := &Scene{Name: "live ordinary three-army battle", Atlas: atlas.texture, Materials: atlas.materials, TextureFrames: atlas.frames, Camera: [3]float32{cx, cz, opts.Zoom}, Metadata: map[string]any{}}
	census := make([]meshCensus, len(models))
	vertices, indices, edges := 0, 0, 0
	for i, m := range models {
		mesh := compileMesh(m, atlas, pal, opts.QuadMultiplier, &census[i])
		scene.Meshes = append(scene.Meshes, mesh)
		vertices += len(mesh.Vertices)
		indices += len(mesh.Indices)
		edges += len(mesh.EdgeIndices)
	}
	tntPath := "maps/" + mapName + ".tnt"
	terrain, err := formats.LoadTNTFile(fs, tntPath)
	if err != nil {
		return nil, err
	}
	scene.Terrain, scene.TerrainRect = bakeTerrain(terrain, pal, cx, cz, 4096, 4096)
	scene.HeightField = battleHeightField(sess.World)
	source := newBattleSource(scene, sess, fs, models, modelIndices, originY, [2]int{opts.Width, opts.Height})
	source.spectator, source.palette = opts.ViewPlayer < 0, pal
	if err := source.prepareFog(); err != nil {
		return nil, err
	}
	if err := source.prepareSprites(pal, cat); err != nil {
		return nil, err
	}
	if err := source.publishInitial(); err != nil {
		return nil, err
	}
	scene.Live = source
	initial := source.latest.Load()
	scene.Instances = initial.frame.Instances
	scene.Frames = []PoseFrame{{Transforms: initial.frame.Previous}, {Transforms: initial.frame.Current}}
	info, _ := fs.Stat(tntPath)
	scene.Metadata["scene_kind"] = "live ordinary authoritative simbench session; no replicated or replayed units"
	scene.Metadata["fixture"] = fixture
	scene.Metadata["gameplay"] = "modern"
	scene.Metadata["view_player"] = opts.ViewPlayer
	scene.Metadata["spectator"] = source.spectator
	scene.Metadata["fog"] = map[string]any{"policy": "committed two-channel fog cache, authored Gray/Black masks at 8-world-pixel samples; current gray multiplies RGB by 0.5 rather than destination palette remapping; no simulation-mask smoothing", "initial_dimensions": [2]int{initial.frame.Fog.Width, initial.frame.Fog.Height}, "initial_rgba_bytes": len(initial.frame.Fog.RGBA), "publication_policy": "reuse immutable RGBA when fog source/version are unchanged; replacement publishes a detached full-cache image"}
	scene.Metadata["height_field"] = map[string]any{"dimensions": [2]int{scene.HeightField.Width, scene.HeightField.Height}, "bytes": len(scene.HeightField.Values) * 4, "sample_centre_rect": scene.HeightField.Rect, "policy": "immutable map-load height samples, quantized integer receiver query; GPU interpolation is display-only"}
	scene.Metadata["simulation_seed"] = opts.Seed
	scene.Metadata["crt_seed"] = opts.Seed
	scene.Metadata["requested_preticks"] = opts.Preticks
	scene.Metadata["actual_preticks"] = sess.Clock.GlobalTick
	scene.Metadata["pretick_elapsed_seconds"] = warmDuration.Seconds()
	scene.Metadata["initial_census"] = initial.census
	scene.Metadata["configured_unit_limit"] = max(headless.SimBenchDefaultUnitLimit, opts.ArmySize+1)
	scene.Metadata["resolved_unit_limit"] = sess.Units.UnitLimit()
	scene.Metadata["unique_models"] = len(models)
	scene.Metadata["model_load_failures"] = failures
	scene.Metadata["models"] = census
	scene.Metadata["retained_vertices"] = vertices
	scene.Metadata["retained_triangles"] = indices / 3
	scene.Metadata["retained_authored_edges"] = edges / 2
	scene.Metadata["retained_mesh_bytes"] = vertices*64 + indices*4 + edges*4
	scene.Metadata["geometry_multiplier"] = opts.QuadMultiplier
	scene.Metadata["texture_multiplier"] = opts.TextureScale
	scene.Metadata["atlas"] = atlas.metadata
	scene.Metadata["terrain"] = map[string]any{"logical_path": tntPath, "provider": info.Source.ProviderID(), "crop_rect": scene.TerrainRect, "width": scene.Terrain.Width, "height": scene.Terrain.Height, "rgba_bytes": len(scene.Terrain.RGBA), "fixed_world_y_offset": originY, "policy": "flat native TNT crop, immutable height field supplies native terrain depth and shadow receivers"}
	scene.Metadata["pose_source"] = "worker reads pinned completed frame pairs; InstanceID matching snaps births, suppresses deaths and prevents slot-reuse blending"
	scene.Metadata["presentation_buffer_ticks"] = 1
	scene.Metadata["presentation_buffer_seconds"] = battlePeriod.Seconds()
	scene.Metadata["viewport"] = [2]int{opts.Width, opts.Height}
	scene.Metadata["camera_policy"] = "native pixels per world unit; pan clamped to crop, zoom 0.1..4; views wider than crop expose limited terrain; renderer input only"
	scene.Metadata["render_limitations"] = []string{"conventional depth and Lambert shading; matrix interpolation can shrink rotating pieces", "ordinary perspective applies published LOS and cloak admission; fog uses 8-pixel authored mask samples and scalar gray modulation rather than destination palette lookup", "unknown or out-of-range logo colour uses frame zero/clamping rather than retail texture omission", "construction height cutoff and authored ring lines approximate the five-stage height-key reveal and raster row extremes", "cargo uses committed world poses and shared native depth; carrier staged height-key composition and one final parent ALP commit omitted", "cloak is straight opacity 0.5 rather than source-major destination-palette ALP", "unit shadows use authored gates and immutable ground receiver; native projection/depth differs from retail silhouette clipping and structure cache", "flat terrain crop with height occlusion, no GUI, audio or ordinary animated model texture cursors", "projectile secondary-model replacement and paired triangle fragments omitted", "wreck shimmer uses neutral-pose model bounds rather than changing heading/pose bounds", "feature visibility reuses the current stock tall-feature admission approximation"}
	scene.Metadata["sprites"] = source.spriteReport()
	return scene, nil
}

func battleModelKey(name string) string {
	name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	name = strings.TrimPrefix(name, "objects3d/")
	return strings.TrimSuffix(name, ".3do")
}
func battleModelMapName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimPrefix(name, "maps/")
	name = strings.TrimSuffix(name, ".ota")
	return strings.TrimSuffix(name, ".tnt")
}

// Dispatches do not count as ticks. Loading and pending-battle setup must finish
// before the first committed frame can cross the presentation boundary.
func battleStep(sess *session.Session) error {
	before := sess.Clock.GlobalTick
	for pump := 0; pump < 16; pump++ {
		sess.Step(sess.Clock.ScaledAnchor + 1)
		if sess.Clock.GlobalTick > before && sess.Snapshot.Current() != nil {
			return nil
		}
	}
	return fmt.Errorf("nanolathe: live prototype session stopped advancing: logical path committed frame, providers searched [session dispatcher], expected next ordinary tick after %d (state %d)", before, sess.State)
}
