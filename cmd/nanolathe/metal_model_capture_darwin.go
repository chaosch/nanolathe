//go:build darwin

package main

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// metalModelCapture freezes a presentation fixture; it owns no clock, input or
// session lifecycle. The activated Solar is the ownership review scene of
// DESIGN_GPU_RENDERER §22.1, using the same COB-derived pose as --shot-model.
type metalModelCapture struct {
	frame  meshscene.LiveFrame
	report map[string]any
}

func newMetalModelCapture(scene *meshscene.Scene, world *meshscene.RetainedBattle, sess *session.Session, modelName string, width, height int, fs vfs.FSOps) (meshscene.LiveSource, error) {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if scene == nil || world == nil || sess == nil || sess.Catalog == nil || sess.Snapshot == nil || width <= 0 || height <= 0 || (name != "armsolar" && name != "armcom" && name != "armcom-ground") {
		return nil, fmt.Errorf("nanolathe: Metal model capture requires retained assets, an opening session, positive dimensions and armsolar, armcom or armcom-ground")
	}
	opening := sess.Snapshot.Current()
	if opening == nil || opening.Tick != 0 {
		return nil, fmt.Errorf("nanolathe: Metal model capture requires a committed opening frame")
	}
	var commander frame.UnitView
	found := false
	for _, unit := range opening.Units {
		if unit.Owner == sess.LocalOwner && strings.EqualFold(unit.DefName, "armcom") {
			commander, found = unit, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("nanolathe: Metal model capture requires the local opening ARM commander")
	}
	unit := commander
	unit.Pieces = slices.Clone(commander.Pieces)
	poseSource := "opening committed commander PieceView lanes"
	var groundFeatures []frame.FeatureView
	if name == "armcom-ground" {
		// Presentation fixture only: overlap the opening pose with the nearest
		// actual short sprite feature to inspect [03 R-RAST-01 §6] ordering.
		nearest := math.MaxFloat64
		for _, feature := range opening.Features {
			if feature.Height >= 10 || feature.Filename == "" || feature.RuntimeLive {
				continue
			}
			dx, dz := float64(feature.X-unit.X), float64(feature.Z-unit.Z)
			if distance := dx*dx + dz*dz; distance < nearest {
				nearest = distance
				groundFeatures = []frame.FeatureView{feature}
			}
		}
		if len(groundFeatures) == 0 {
			return nil, fmt.Errorf("nanolathe: ground overlap capture requires an opening short sprite feature")
		}
		unit.X, unit.Y, unit.Z = groundFeatures[0].X, groundFeatures[0].Y, groundFeatures[0].Z
		poseSource += "; presentation-only relocation onto nearest actual short feature"
	}
	if name == "armsolar" {
		def, ok := sess.Catalog.Unit(name)
		if !ok || def == nil {
			return nil, fmt.Errorf("nanolathe: Metal model capture requires the compiled ARMSOLAR definition")
		}
		poses, err := actualActivatedModelPose(fs, def)
		if err != nil {
			return nil, err
		}
		// Reuse the opening owner's placement and colour without allocating a
		// simulation unit. Only this detached presentation record changes.
		unit = frame.UnitView{
			InstanceID: commander.InstanceID, AllocationSerial: commander.AllocationSerial,
			Slot: commander.Slot, Owner: commander.Owner, X: commander.X, Y: commander.Y, Z: commander.Z,
			DefName: def.CanonicalKey, Model: def.ObjectName, Pieces: poses,
			BMCode: def.BMCode != 0, ZBuffer: def.ZBuffer, NoShadow: def.NoShadow,
			CanHover: def.CanHover, Floater: def.Floater, Digger: def.Digger,
			OwnerColor: commander.OwnerColor, OwnerColorKnown: commander.OwnerColorKnown,
			Activated: true,
		}
		poseSource = "production unit-bound COB Create and Activate via actualActivatedModelPose"
	}
	// Construct a minimal owned frame rather than retaining borrowed slices
	// from the session publication. Local ownership admits the model normally.
	fixed := &frame.Frame{Tick: opening.Tick, ViewingPlayer: unit.Owner, Units: []frame.UnitView{unit}, Features: groundFeatures, Players: opening.Players, Visibility: opening.Visibility}
	camera := [3]float32{float32(unit.X) / 65536, float32(unit.Z)/65536 - float32(unit.Y)/131072, 6}
	live := world.Frame(fixed, fixed, camera)
	if len(live.Instances) != 1 {
		return nil, fmt.Errorf("nanolathe: Metal model capture did not resolve exactly one retained %s model", name)
	}
	// Fit and centre from the real posed mesh, including opened panels. The
	// native live projection is x,z-y/2; no manufactured pose or bounds enter.
	instance := live.Instances[0]
	if instance.Mesh < 0 || instance.Mesh >= len(scene.Meshes) {
		return nil, fmt.Errorf("nanolathe: Metal model capture returned an invalid mesh")
	}
	mesh := &scene.Meshes[instance.Mesh]
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := -minX, -minY
	visible := false
	for _, vertex := range mesh.Vertices {
		at := instance.PoseOffset + int(vertex.Piece)
		if at < 0 || at >= len(live.Current) || live.Current[at][15] == 0 {
			continue
		}
		m, p := live.Current[at], vertex.Position
		x := m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12]
		y := m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13]
		z := m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14]
		projectedY := z - y/2
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, projectedY), max(maxY, projectedY)
		visible = true
	}
	if !visible {
		return nil, fmt.Errorf("nanolathe: Metal model capture has no visible %s geometry", name)
	}
	camera = [3]float32{(minX + maxX) / 2, (minY + maxY) / 2, min(float32(6), float32(width)*0.8/max(maxX-minX, 1), float32(height)*0.8/max(maxY-minY, 1))}
	live = world.Frame(fixed, fixed, camera)
	live.Alpha = 1
	// The fixture inspects model ownership and base colour, without unrelated
	// fog, light or GUI layers; only the requested overlap feature remains.
	live.Fog = meshscene.FogFrame{}
	live.Lights, live.Distortions, live.Overlay = nil, nil, nil
	if len(groundFeatures) == 0 {
		live.Sprites = nil
	}
	fixture := map[string]any{"model": name, "model_resource": unit.Model, "pose_source": poseSource, "source_tick": opening.Tick, "camera": camera, "viewport": [2]int{width, height}, "fixed_frame_pair": true, "pieces": len(unit.Pieces), "ground_features": len(groundFeatures)}
	if scene.Metadata == nil {
		scene.Metadata = make(map[string]any)
	}
	scene.Metadata["model_capture"] = fixture
	scene.Metadata["scene_kind"] = "fixed model visual capture from production pose sources"
	scene.Name = "Native model capture: " + name
	scene.Camera, scene.Instances = camera, live.Instances
	scene.Frames = []meshscene.PoseFrame{{Transforms: live.Previous}, {Transforms: live.Current}}
	return &metalModelCapture{frame: live, report: fixture}, nil
}

func (c *metalModelCapture) Next(_ float64, _ meshscene.Input) (meshscene.LiveFrame, error) {
	return c.frame, nil
}

func (c *metalModelCapture) Close() map[string]any { return c.report }
