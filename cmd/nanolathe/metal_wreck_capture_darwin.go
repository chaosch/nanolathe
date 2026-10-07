//go:build darwin

package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/session"
)

// A detached presentation fixture compares equal displayed poses of the actual
// authored Flash corpse. It neither kills a unit nor advances the session.
func newMetalWreckCapture(scene *meshscene.Scene, world *meshscene.RetainedBattle, sess *session.Session, name string, width, height int) (meshscene.LiveSource, error) {
	if name != "armflash-wreck" && name != "armflash-wreck-sink" {
		return nil, fmt.Errorf("nanolathe: unsupported wreck capture %s", name)
	}
	opening := sess.Snapshot.Current()
	var unit frame.UnitView
	for _, u := range opening.Units {
		if u.Owner == sess.LocalOwner && strings.EqualFold(u.DefName, "armcom") {
			unit = u
			break
		}
	}
	def, ok := sess.Catalog.Unit("armflash")
	if !ok || unit.InstanceID == 0 {
		return nil, fmt.Errorf("nanolathe: wreck capture requires the local ARM commander")
	}
	corpse := sess.Catalog.Features[content.CanonicalKey(def.Corpse)]
	if corpse == nil || corpse.Object == "" {
		return nil, fmt.Errorf("nanolathe: wreck capture requires the authored Flash corpse model")
	}
	feature := frame.FeatureView{InstanceID: 1, Owner: unit.Owner, OwnerKnown: true, X: unit.X, Y: unit.Y, Z: unit.Z, Heading: unit.Heading, DefName: corpse.CanonicalKey, Model: corpse.Object, FootX: int8(corpse.FootprintX), FootZ: int8(corpse.FootprintZ)}
	fixed := &frame.Frame{Tick: opening.Tick, ViewingPlayer: unit.Owner, Features: []frame.FeatureView{feature}, Players: opening.Players, Visibility: opening.Visibility}
	camera := [3]float32{float32(unit.X) / 65536, float32(unit.Z)/65536 - float32(unit.Y)/131072, 6}
	live := world.Frame(fixed, fixed, camera)
	if len(live.Instances) != 1 {
		return nil, fmt.Errorf("nanolathe: wreck capture requires one retained corpse")
	}
	instance := live.Instances[0]
	mesh := &scene.Meshes[instance.Mesh]
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := -minX, -minY
	for _, v := range mesh.Vertices {
		m, p := live.Current[instance.PoseOffset+int(v.Piece)], v.Position
		if m[15] == 0 {
			continue
		}
		x := m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12]
		y := m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13]
		z := m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14]
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, z-y/2), max(maxY, z-y/2)
	}
	camera = [3]float32{(minX + maxX) / 2, (minY + maxY) / 2, min(float32(6), float32(width)*.8/max(maxX-minX, 1), float32(height)*.8/max(maxY-minY, 1))}
	if name == "armflash-wreck-sink" {
		// Symmetric half-pixel endpoints give exactly the reference displayed pose.
		// This is a diagnostic translation, not a claim about the sinking rate.
		previous, current := *fixed, *fixed
		a, b := feature, feature
		a.Y += 32768
		b.Y -= 32768
		previous.Features = []frame.FeatureView{a}
		current.Features = []frame.FeatureView{b}
		// Do not reuse the earlier reference publication as the supplied prior pose.
		previous.Tick = 1
		current.Tick = 2
		live = world.Frame(&previous, &current, camera)
		live.Alpha = .5
	} else {
		live = world.Frame(fixed, fixed, camera)
		live.Alpha = 1
	}
	live.Fog = meshscene.FogFrame{}
	live.Sprites = nil
	live.Lights = nil
	live.Distortions = nil
	live.Overlay = nil
	for i := range live.Instances {
		live.Instances[i].Visual.Emission = [4]float32{}
	}
	fixture := map[string]any{"model": name, "model_resource": corpse.Object, "source": "compiled ARM Flash corpse definition; detached presentation-only translation", "camera": camera, "viewport": [2]int{width, height}, "alpha": live.Alpha, "effects_disabled": true}
	scene.Metadata["model_capture"] = fixture
	scene.Metadata["scene_kind"] = "fixed authored wreck interpolation diagnostic"
	scene.Name = "Native wreck capture: " + name
	scene.Camera = camera
	scene.Instances = live.Instances
	scene.Frames = []meshscene.PoseFrame{{Transforms: live.Previous}, {Transforms: live.Current}}
	return &metalModelCapture{frame: live, report: fixture}, nil
}
