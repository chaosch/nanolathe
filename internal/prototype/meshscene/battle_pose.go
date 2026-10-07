package meshscene

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/render"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

type battleEntityKey struct {
	kind uint8
	id   uint64
}
type battlePoseSpan struct{ mesh, offset, count int }
type battlePublication struct {
	frame      LiveFrame
	byID       map[battleEntityKey]battlePoseSpan
	census     battleCensus
	blendStart time.Time
}
type battlePoseScratch struct {
	states     []model.PieceState
	transforms []model.Transform
	compose    model.ComposeScratch
}

type battleCensus struct {
	Tick                 uint32  `json:"tick"`
	Units                int     `json:"units"`
	UnitsMoving          int     `json:"units_moving_by_committed_position"`
	Nanoframes           int     `json:"nanoframes"`
	Cargo                int     `json:"attached_units"`
	Cloaked              int     `json:"cloaked_units"`
	Projectiles          int     `json:"projectiles"`
	Effects              int     `json:"effects"`
	Features             int     `json:"features"`
	BurningFeatures      int     `json:"burning_features"`
	Strips               int     `json:"strip_objects"`
	Fragments            int     `json:"fragments"`
	Debris               int     `json:"detached_mesh_debris"`
	SubmittedUnits       int     `json:"submitted_unit_models"`
	SubmittedFeatures    int     `json:"submitted_feature_models"`
	SubmittedProjectiles int     `json:"submitted_projectile_models"`
	SubmittedSprites     int     `json:"submitted_sprites"`
	SubmittedModels      int     `json:"submitted_model_instances"`
	SubmittedTriangles   int     `json:"submitted_model_triangles"`
	SubmittedMatrices    int     `json:"submitted_piece_matrices"`
	CulledModels         int     `json:"culled_models"`
	MissingModels        int     `json:"missing_model_instances"`
	Births               int     `json:"unit_births"`
	Deaths               int     `json:"unit_deaths"`
	UnitsPerOwner        [10]int `json:"units_per_owner"`
	ArmyUnits            int     `json:"army_units_owners_1_2_3"`
}

func (s *battleSource) buildPublication(previous, current *frame.Frame, camera [3]float32, old *battlePublication) *battlePublication {
	matrixCapacity := len(current.Units)*12 + len(current.Projectiles)*2
	if old != nil {
		matrixCapacity = max(matrixCapacity, len(old.frame.Current)+128)
	}
	pub := &battlePublication{frame: LiveFrame{Camera: camera, Tick: current.Tick, Current: make([]Mat4, 0, matrixCapacity), Previous: make([]Mat4, 0, matrixCapacity), Instances: make([]Instance, 0, len(current.Units)+len(current.Projectiles))}, byID: make(map[battleEntityKey]battlePoseSpan, len(current.Units)+len(current.Projectiles))}
	census := battleCensus{Tick: current.Tick, Units: len(current.Units), Projectiles: len(current.Projectiles), Effects: len(current.Effects), Features: len(current.Features), Strips: len(current.Strips), Fragments: len(current.Fragments), Debris: len(current.Debris)}
	previousUnits := make(map[uint64]*frame.UnitView, len(current.Units))
	previousFeatures := map[uint64]*frame.FeatureView{}
	previousProjectiles := map[uint64]*frame.ProjectileView{}
	if previous != nil {
		for i := range previous.Units {
			u := &previous.Units[i]
			previousUnits[u.InstanceID] = u
		}
		for i := range previous.Features {
			f := &previous.Features[i]
			previousFeatures[f.InstanceID] = f
		}
		for i := range previous.Projectiles {
			p := &previous.Projectiles[i]
			previousProjectiles[p.PresentationID] = p
		}
	}
	currentUnits := make(map[uint64]bool, len(current.Units))
	for _, u := range current.Units {
		currentUnits[u.InstanceID] = true
		if int(u.Owner) < len(census.UnitsPerOwner) {
			census.UnitsPerOwner[u.Owner]++
		}
		if u.Owner >= 1 && u.Owner <= 3 {
			census.ArmyUnits++
		}
		prior := previousUnits[u.InstanceID]
		if prior == nil {
			census.Births++
		} else if prior.X != u.X || prior.Y != u.Y || prior.Z != u.Z {
			census.UnitsMoving++
		}
		if u.BuildRemaining > 0 {
			census.Nanoframes++
		}
		if u.Carrier != 0 {
			census.Cargo++
		}
		if u.Cloaked {
			census.Cloaked++
		}
		mesh, ok := s.modelIndices[battleModelKey(u.Model)]
		if !ok {
			census.MissingModels++
			s.noteMissing(u.Model)
			continue
		}
		if !battleInView(u.X, u.Y, u.Z, camera, s.viewport) && (prior == nil || !battleInView(prior.X, prior.Y, prior.Z, camera, s.viewport)) {
			census.CulledModels++
			continue
		}
		if prior != nil && battleModelKey(prior.Model) != battleModelKey(u.Model) {
			prior = nil
		}
		key := battleEntityKey{kind: 1, id: u.InstanceID}
		s.appendUnit(pub, key, mesh, u, prior, old)
		census.SubmittedUnits++
	}
	for id := range previousUnits {
		if !currentUnits[id] {
			census.Deaths++
		}
	}
	for _, f := range current.Features {
		if f.IsBurning {
			census.BurningFeatures++
		}
		if f.Model == "" || f.Filename != "" {
			continue
		}
		mesh, ok := s.modelIndices[battleModelKey(f.Model)]
		if !ok {
			census.MissingModels++
			s.noteMissing(f.Model)
			continue
		}
		prior := previousFeatures[f.InstanceID]
		if prior != nil && battleModelKey(prior.Model) != battleModelKey(f.Model) {
			prior = nil
		}
		if !battleInView(f.X, f.Y, f.Z, camera, s.viewport) && (prior == nil || !battleInView(prior.X, prior.Y, prior.Z, camera, s.viewport)) {
			census.CulledModels++
			continue
		}
		u := frame.UnitView{X: f.X, Y: f.Y, Z: f.Z, Heading: f.Heading, Pitch: f.Pitch, Bank: f.Bank}
		var prevUnit *frame.UnitView
		if prior != nil {
			prevUnit = &frame.UnitView{X: prior.X, Y: prior.Y, Z: prior.Z, Heading: prior.Heading, Pitch: prior.Pitch, Bank: prior.Bank}
		}
		s.appendUnit(pub, battleEntityKey{kind: 2, id: f.InstanceID}, mesh, u, prevUnit, old)
		census.SubmittedFeatures++
	}
	for _, p := range current.Projectiles {
		// The ordinary dispatcher skips burst schedulers and recognizes only
		// these three model families; an authored Model cannot override the
		// projectile's render family [03 §5.4][06 R-WFX-01 §4].
		if p.BurstRemaining != 0 || p.Model == "" || (p.RenderType != render.RenderTypeBaseSpriteModel && p.RenderType != render.RenderTypeBaseModelDistinct && p.RenderType != render.RenderTypeRecordOrientation) {
			continue
		}
		mesh, ok := s.modelIndices[battleModelKey(p.Model)]
		if !ok {
			census.MissingModels++
			s.noteMissing(p.Model)
			continue
		}
		prior := previousProjectiles[p.PresentationID]
		if prior != nil && battleModelKey(prior.Model) != battleModelKey(p.Model) {
			prior = nil
		}
		if !battleInView(p.X, p.Y, p.Z, camera, s.viewport) && (prior == nil || !battleInView(prior.X, prior.Y, prior.Z, camera, s.viewport)) {
			census.CulledModels++
			continue
		}
		key := battleEntityKey{kind: 3, id: p.PresentationID}
		span := s.beginInstance(pub, key, mesh)
		s.poseScratch.projectileInto(pub.frame.Current[span.offset:span.offset+span.count], s.models[mesh], p, current.Tick)
		s.copyPriorOrSnap(pub, span, key, old, func(dst []Mat4) {
			if prior != nil {
				s.poseScratch.projectileInto(dst, s.models[mesh], *prior, previous.Tick)
			} else {
				copy(dst, pub.frame.Current[span.offset:span.offset+span.count])
			}
		})
		census.SubmittedProjectiles++
	}
	if s.sprites != nil {
		pub.frame.Sprites, pub.frame.Lights = s.sprites.Append(nil, nil, previous, current, camera, s.viewport)
	}
	census.SubmittedModels = len(pub.frame.Instances)
	census.SubmittedSprites = len(pub.frame.Sprites)
	census.SubmittedMatrices = len(pub.frame.Current)
	for _, instance := range pub.frame.Instances {
		census.SubmittedTriangles += len(s.scene.Meshes[instance.Mesh].Indices) / 3
	}
	pub.census = census
	return pub
}

func (s *battleSource) beginInstance(pub *battlePublication, key battleEntityKey, mesh int) battlePoseSpan {
	offset, count := len(pub.frame.Current), len(s.models[mesh].Pieces)
	pub.frame.Current = slices.Grow(pub.frame.Current, count)[:offset+count]
	pub.frame.Previous = slices.Grow(pub.frame.Previous, count)[:offset+count]
	pub.frame.Instances = append(pub.frame.Instances, Instance{Mesh: mesh, PoseOffset: offset})
	span := battlePoseSpan{mesh: mesh, offset: offset, count: count}
	pub.byID[key] = span
	return span
}
func (s *battleSource) appendUnit(pub *battlePublication, key battleEntityKey, mesh int, current frame.UnitView, previous *frame.UnitView, old *battlePublication) {
	span := s.beginInstance(pub, key, mesh)
	s.poseScratch.unitInto(pub.frame.Current[span.offset:span.offset+span.count], s.models[mesh], current)
	s.copyPriorOrSnap(pub, span, key, old, func(dst []Mat4) {
		if previous != nil {
			s.poseScratch.unitInto(dst, s.models[mesh], *previous)
		} else {
			copy(dst, pub.frame.Current[span.offset:span.offset+span.count])
		}
	})
}
func (s *battleSource) copyPriorOrSnap(pub *battlePublication, span battlePoseSpan, key battleEntityKey, old *battlePublication, derive func([]Mat4)) {
	dst := pub.frame.Previous[span.offset : span.offset+span.count]
	if old != nil {
		if prior, ok := old.byID[key]; ok && prior.mesh == span.mesh && prior.count == span.count {
			copy(dst, old.frame.Current[prior.offset:prior.offset+prior.count])
			return
		}
	}
	derive(dst)
}
func battleInView(x, y, z numeric.Fixed, camera [3]float32, viewport [2]int) bool {
	// Live projection follows camera.WorldToScreen's half-height term [03 §2.5].
	dx := float32(x)/65536 - camera[0]
	dy := float32(z)/65536 - float32(y)/131072 - camera[1]
	halfW := float32(viewport[0])/(2*camera[2]) + 256
	halfH := float32(viewport[1])/(2*camera[2]) + 256
	return dx >= -halfW && dx <= halfW && dy >= -halfH && dy <= halfH
}

func (s *battlePoseScratch) reserve(count int) {
	if cap(s.states) < count {
		s.states = make([]model.PieceState, count)
	} else {
		s.states = s.states[:count]
		clear(s.states)
	}
	if cap(s.transforms) < count {
		s.transforms = make([]model.Transform, count)
	} else {
		s.transforms = s.transforms[:count]
	}
	s.compose.BeginModel(count)
}

// This is committedMatrices with reused state/composition scratch. It retains
// the canonical authored hierarchy, script link and final model-Z reflection
// [03 §2.4]; no per-vertex geometry is rebuilt on this path.
func (s *battlePoseScratch) unitInto(dst []Mat4, m *model.Model, u frame.UnitView) {
	s.reserve(len(m.Pieces))
	for _, p := range u.Pieces {
		index := p.Index
		if p.Name != "" {
			index = -1
			for i, piece := range m.Pieces {
				if strings.EqualFold(piece.Name, p.Name) {
					index = i
					break
				}
			}
		}
		if index < 0 || index >= len(s.states) {
			continue
		}
		s.states[index] = model.PieceState{RotX: p.RotX, RotY: p.RotY, RotZ: p.RotZ, Trans: [3]numeric.Fixed{p.Tx, p.Ty, p.Tz}, Hidden: p.Hidden}
	}
	model.FoldRootAngles(s.states, m.Root, u.Heading, u.Pitch, u.Bank)
	for piece := range m.Pieces {
		hidden := false
		for parent := piece; parent >= 0 && parent < len(s.states); parent = m.Pieces[parent].Parent {
			if s.states[parent].Hidden {
				hidden = true
				break
			}
		}
		if hidden {
			dst[piece] = Mat4{}
			continue
		}
		s.transforms[piece] = model.ComposeInto(m, s.states, piece, s.transforms[piece], &s.compose)
		dst[piece] = battleAffine(s.transforms[piece], u.X, u.Y, u.Z)
	}
}

// Projectile effect entries transform a single raw piece and omit authored
// parent translation, exactly as buildProjectileStandalonePiece documents
// [03 §5.2]. Reuse the production angle-fold helpers, not its vertex expansion.
func (s *battlePoseScratch) projectileInto(dst []Mat4, m *model.Model, p frame.ProjectileView, tick uint32) {
	clear(dst)
	if m.Root < 0 || m.Root >= len(m.Pieces) {
		return
	}
	pitch := p.Pitch
	if p.Meteor {
		pitch = p.MeteorPitch
	}
	fill := func(piece int, roll uint16, propeller bool) {
		singlePiece := m.Pieces[piece]
		singlePiece.Parent = -1
		singlePiece.Children = nil
		singlePiece.Translate = [3]numeric.Fixed{}
		one := model.Model{Name: m.Name, Root: 0, Pieces: []model.Piece{singlePiece}}
		s.reserve(1)
		s.states[0].RotZ = roll
		if p.RenderType != render.RenderTypeRecordOrientation {
			render.FoldProjectileAngles(s.states, 0, p.Yaw, pitch)
		} else {
			s.states[0].RotY = p.Yaw
			s.states[0].RotX = pitch
		}
		if propeller {
			render.FoldPublishedPropellerSpin(s.states, 0, p, tick)
		}
		s.transforms[0] = model.ComposeInto(&one, s.states, 0, s.transforms[0], &s.compose)
		dst[piece] = battleAffine(s.transforms[0], p.X, p.Y, p.Z)
	}
	fill(m.Root, p.Roll, false)
	if p.RenderType == render.RenderTypeBaseSpriteModel && tick < p.ExpiryTick && len(m.Pieces[m.Root].Children) > 0 {
		child := m.Pieces[m.Root].Children[0]
		if child >= 0 && child < len(m.Pieces) {
			roll := p.Roll
			if p.Propeller {
				roll = 0
			}
			fill(child, roll, p.Propeller)
		}
	}
}
func battleAffine(transform model.Transform, x, y, z numeric.Fixed) Mat4 {
	origin := transform.Apply([3]numeric.Fixed{})
	matrix := Mat4{15: 1}
	const basisSize = 1024
	for axis := 0; axis < 3; axis++ {
		basis := [3]numeric.Fixed{}
		basis[axis] = numeric.FixedFromInt(basisSize)
		tip := transform.Apply(basis)
		for row := 0; row < 3; row++ {
			matrix[axis*4+row] = float32(tip[row]-origin[row]) / (65536 * basisSize)
		}
		matrix[axis*4+2] = -matrix[axis*4+2]
	}
	matrix[12] = float32(origin[0]+x) / 65536
	matrix[13] = float32(origin[1]+y) / 65536
	matrix[14] = float32(z-origin[2]) / 65536
	return matrix
}

type battleSpriteAdapter interface {
	Append([]Sprite, []Light, *frame.Frame, *frame.Frame, [3]float32, [2]int) ([]Sprite, []Light)
	Report() map[string]any
}

// Sprite construction is linked by the parallel art adapter; there are no
// filesystem reads or sprite-cursor writes on the render thread.
func (s *battleSource) prepareSprites(pal *palette.Tables, cat *content.Catalog) error {
	current, previous := s.sess.Snapshot.PinLatest()
	if current == nil {
		s.sess.Snapshot.Unpin(previous)
		return fmt.Errorf("nanolathe: live prototype sprite preparation unavailable: logical path committed frame, providers searched [session snapshot], expected placed feature definitions")
	}
	featureNames := make([]string, 0, max(len(current.Features), 1))
	for _, feature := range current.Features {
		featureNames = append(featureNames, feature.DefName)
	}
	s.sess.Snapshot.Unpin(current)
	s.sess.Snapshot.Unpin(previous)
	if len(featureNames) == 0 {
		featureNames = append(featureNames, "") // Explicit scope: unit corpse chains only.
	}
	sprites, err := NewBattleSprites(s.fs, pal, cat, featureNames...)
	if err != nil {
		return err
	}
	sprites.SetTerrain(s.sess.World)
	s.sprites = sprites
	s.scene.SpriteAtlas = sprites.Texture()
	return nil
}
func (s *battleSource) spriteReport() map[string]any {
	if s.sprites != nil {
		return s.sprites.Report()
	}
	return map[string]any{"status": "sprite adapter pending integration"}
}
