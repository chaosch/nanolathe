package meshscene

import (
	"fmt"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/headless"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/orders"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

type poseTemplate struct {
	handle    pool.Handle
	initial   [3]numeric.Fixed
	poses     [][]Mat4
	positions [][3]float32
}

func samplePoses(scene *Scene, opts LoadOptions, fs *vfs.FS, cat *content.Catalog, roster []string, models []*model.Model, cols, rows int, pitch, cx, cz float32, mapName string) error {
	cfg := session.SkirmishConfig{MapName: mapName, NumPlayers: 2, UnitLimit: 100}
	cfg.ApplyDefaults()
	cfg.Mapping = 0
	cfg.LineOfSight = 0
	cfg.CommanderDeath = int(session.CommanderDeathContinues)
	cfg.Players[0].Controller = session.SkirmishControllerHuman
	cfg.Players[1].Controller = session.SkirmishControllerComputer
	composed, err := headless.ComposeFreshBattle(headless.FreshBattleRequest{Gameplay: gameplay.Modern, Kind: headless.ScenarioSkirmish, Map: mapName, LocalOwner: -1, Skirmish: cfg, SimulationSeed: 12345, CRTSeed: 6789, FS: fs, Catalog: cat})
	if err != nil {
		return err
	}
	sess := composed.Session
	templates := make([]poseTemplate, len(roster))
	for i, name := range roster {
		def, _ := cat.Unit(name)
		x, z := numeric.FixedFromInt(int64(cx)-256+int64(i%4)*128), numeric.FixedFromInt(int64(cz)-192+int64(i/4)*128)
		y := sess.World.HeightAt(x, z)
		handle, err := sess.Units.Create(def, 0, x, y, z)
		if err != nil {
			return fmt.Errorf("nanolathe: prototype pose unit creation failed: logical path %s, providers searched [unit pool], expected live retail template: %w", name, err)
		}
		unit := sess.Units.Unit(handle)
		if unit == nil {
			return fmt.Errorf("nanolathe: prototype pose template unavailable: logical path %s, providers searched [unit pool], expected created unit", name)
		}
		if sess.Movement != nil {
			sess.Movement.EnsureUnit(unit)
		}
		queue, ok := unit.Orders.(*orders.Queue)
		if !ok || queue == nil {
			return fmt.Errorf("nanolathe: prototype template order queue unavailable: logical path %s, providers searched [unit record], expected ordinary movement queue", name)
		}
		order := orders.Lookup("Move_Ground")
		goalX, goalZ := x+numeric.FixedFromInt(400), z+numeric.FixedFromInt(120)
		queue.Push(order, orders.NewNodeForOrder(order, 0, goalX, sess.World.HeightAt(goalX, goalZ), goalZ, sess.Clock.GlobalTick, handle, false))
		templates[i] = poseTemplate{handle: handle, initial: [3]numeric.Fixed{x, y, z}}
	}
	var drained []frame.EventView
	firstTick, lastTick := uint32(0), uint32(0)
	// Only completed, published frames cross this boundary. We never inspect a
	// mutable COB state to make a rendered pose (DESIGN_GPU_RENDERER §13.5; I6).
	for sample := 0; sample < opts.Samples; sample++ {
		sess.Step(sess.Clock.ScaledAnchor + 1)
		committed := sess.Snapshot.Current()
		if committed == nil {
			return fmt.Errorf("nanolathe: prototype pose publication missing: logical path committed frame, providers searched [session snapshot], expected completed 30 Hz sample")
		}
		if sample == 0 {
			firstTick = committed.Tick
		}
		lastTick = committed.Tick
		for i := range templates {
			var view *frame.UnitView
			for j := range committed.Units {
				if committed.Units[j].Slot == templates[i].handle {
					view = &committed.Units[j]
					break
				}
			}
			if view == nil {
				return fmt.Errorf("nanolathe: prototype pose template left publication: logical path %s, providers searched [committed units], expected surviving template", roster[i])
			}
			templates[i].poses = append(templates[i].poses, committedMatrices(models[i], *view))
			templates[i].positions = append(templates[i].positions, [3]float32{float32(view.X-templates[i].initial[0]) / 65536, float32(view.Y-templates[i].initial[1]) / 65536, float32(view.Z-templates[i].initial[2]) / 65536})
		}
		drained = sess.Snapshot.DrainCommittedEvents(drained)
	}
	poseCount := 0
	for instance := 0; instance < opts.Units; instance++ {
		mesh := instance % len(models)
		scene.Instances = append(scene.Instances, Instance{Mesh: mesh, PoseOffset: poseCount})
		poseCount += len(models[mesh].Pieces)
	}
	scene.Frames = make([]PoseFrame, opts.Samples)
	for sample := range scene.Frames {
		scene.Frames[sample].Transforms = make([]Mat4, poseCount)
		for instance, record := range scene.Instances {
			// Keep natural template motion, but stagger the replay phase. The slot's
			// placement is a renderer workload choice, not an authoritative position.
			phase := (sample + (instance/len(models))*7) % opts.Samples
			template := &templates[record.Mesh]
			x := cx + (float32(instance%cols)-float32(cols-1)/2)*pitch
			z := cz + (float32(instance/cols)-float32(rows-1)/2)*pitch
			position := template.positions[phase]
			for piece, matrix := range template.poses[phase] {
				if matrix[15] != 0 {
					matrix[12] += x + position[0]
					matrix[13] += position[1]
					matrix[14] += z + position[2]
				}
				scene.Frames[sample].Transforms[record.PoseOffset+piece] = matrix
			}
		}
	}
	scene.Metadata["pose_source"] = "ordinary skirmish committed frame; ordinary unit allocator and movement orders; published script-piece link; model.Compose hierarchy"
	scene.Metadata["template_units"] = len(templates)
	scene.Metadata["template_roster"] = roster
	scene.Metadata["template_session_mode"] = "modern"
	scene.Metadata["template_tick_range"] = [2]uint32{firstTick, lastTick}
	scene.Metadata["template_seed_pair"] = [2]uint32{12345, 6789}
	scene.Metadata["template_initial_positions"] = func() [][3]float32 {
		p := make([][3]float32, len(templates))
		for i, t := range templates {
			for axis := range p[i] {
				p[i][axis] = float32(t.initial[axis]) / 65536
			}
		}
		return p
	}()
	scene.Metadata["pose_replay_loop"] = "cyclic; phase offsets between replicas; sampled motion can jump at wrap"
	scene.Metadata["hidden_piece_policy"] = "all-zero matrix (including w); inherited parent visibility; backend must suppress triangles with hidden matrices"
	scene.Metadata["matrix_policy"] = "column-major affine basis sampled through canonical model.Compose; final model Z mirrored to world Z; GPU interpolation is approximate presentation"
	return nil
}

// The canonical composer performs the authored half-turn and Z/X/Y hierarchy
// arithmetic [03 §2.4]. The prototype converts its affine basis to binary32;
// it does not claim retail's per-vertex fixed-point rounding at GPU precision.
// Mirror only the final output Z, as Transform.WorldOffset documents.
func committedMatrices(m *model.Model, view frame.UnitView) []Mat4 {
	states := make([]model.PieceState, len(m.Pieces))
	for _, piece := range view.Pieces {
		index := piece.Index
		if piece.Name != "" {
			index = -1
			for i, p := range m.Pieces {
				if p.Name == piece.Name {
					index = i
					break
				}
			}
		}
		if index < 0 || index >= len(states) {
			continue
		}
		states[index] = model.PieceState{RotX: piece.RotX, RotY: piece.RotY, RotZ: piece.RotZ, Trans: [3]numeric.Fixed{piece.Tx, piece.Ty, piece.Tz}, Hidden: piece.Hidden}
	}
	model.FoldRootAngles(states, m.Root, view.Heading, view.Pitch, view.Bank)
	result := make([]Mat4, len(m.Pieces))
	const basisSize = 1024
	for piece := range m.Pieces {
		hidden := false
		for parent := piece; parent >= 0 && parent < len(states); parent = m.Pieces[parent].Parent {
			if states[parent].Hidden {
				hidden = true
				break
			}
		}
		if hidden {
			continue
		}
		transform := model.Compose(m, states, piece)
		origin := transform.Apply([3]numeric.Fixed{})
		matrix := Mat4{15: 1}
		for axis := 0; axis < 3; axis++ {
			basis := [3]numeric.Fixed{}
			basis[axis] = numeric.FixedFromInt(basisSize)
			tip := transform.Apply(basis)
			for row := 0; row < 3; row++ {
				matrix[axis*4+row] = float32(tip[row]-origin[row]) / (65536 * basisSize)
			}
			matrix[axis*4+2] = -matrix[axis*4+2]
		}
		matrix[12], matrix[13], matrix[14] = float32(origin[0])/65536, float32(origin[1])/65536, -float32(origin[2])/65536
		result[piece] = matrix
	}
	return result
}
