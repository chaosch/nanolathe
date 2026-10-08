package meshscene

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/render"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

type battleEntityKey struct {
	kind uint8
	id   uint64
}
type battlePoseSpan struct{ mesh, offset, count, instance int }
type battlePublication struct {
	frame                 LiveFrame
	byID                  map[battleEntityKey]battlePoseSpan
	keys                  []battleEntityKey     // each frame.Instances entry's subject, in instance order
	materials             []NativeModelMaterial // backs the instances' uncached material walks
	census                battleCensus
	fogSource, fogVersion uint64
	blendStart            time.Time
	previous              *battlePublication // bounded presentation fallback; its own previous is nil
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
	SubmittedDebris      int     `json:"submitted_debris_models"`
	RejectedUnits        int     `json:"admission_rejected_unit_models"`
	RejectedFeatures     int     `json:"visibility_rejected_feature_models"`
	RejectedProjectiles  int     `json:"visibility_rejected_projectile_models"`
	SubmittedShadows     int     `json:"submitted_model_shadows"`
	SubmittedOutlines    int     `json:"submitted_nanoframe_outlines"`
	SubmittedHotWrecks   int     `json:"submitted_hot_wreck_models"`
	SubmittedDistortions int     `json:"submitted_distortions"`
	SubmittedSprites     int     `json:"submitted_sprites"`
	SubmittedModels      int     `json:"submitted_model_instances"`
	SubmittedTriangles   int     `json:"submitted_model_triangles"`
	SubmittedMatrices    int     `json:"submitted_piece_matrices"`
	CulledModels         int     `json:"culled_models"`
	PosesReused          int     `json:"unit_poses_reused"`
	MissingModels        int     `json:"missing_model_instances"`
	Births               int     `json:"unit_births"`
	Deaths               int     `json:"unit_deaths"`
	UnitsPerOwner        [10]int `json:"units_per_owner"`
	ArmyUnits            int     `json:"army_units_owners_1_2_3"`
}

// recycle, when non-nil, is a publication no reader still holds; its storage
// is reused. It must not be old, which this build reads.
func (s *battleSource) buildPublication(previous, current *frame.Frame, camera [3]float32, old, recycle *battlePublication) *battlePublication {
	matrixCapacity := len(current.Units)*12 + len(current.Projectiles)*2
	if old != nil {
		matrixCapacity = max(matrixCapacity, len(old.frame.Current)+128)
	}
	var pub *battlePublication
	// The recycled publication's fog texture is free unless old still shares it.
	var fogStore []byte
	if recycle != nil && recycle != old {
		if fog := recycle.frame.Fog.RGBA; len(fog) > 0 && (old == nil || len(old.frame.Fog.RGBA) == 0 || &fog[0] != &old.frame.Fog.RGBA[0]) {
			fogStore = fog
		}
		byID := recycle.byID
		clear(byID)
		*recycle = battlePublication{frame: LiveFrame{Camera: camera, Tick: current.Tick, Current: recycle.frame.Current[:0], Previous: recycle.frame.Previous[:0], Instances: recycle.frame.Instances[:0],
			Sprites: recycle.frame.Sprites[:0], Lights: recycle.frame.Lights[:0], Distortions: recycle.frame.Distortions[:0]}, byID: byID,
			keys: recycle.keys[:0], materials: recycle.materials[:0]}
		pub = recycle
	} else {
		pub = &battlePublication{frame: LiveFrame{Camera: camera, Tick: current.Tick, Current: make([]Mat4, 0, matrixCapacity), Previous: make([]Mat4, 0, matrixCapacity), Instances: make([]Instance, 0, len(current.Units)+len(current.Projectiles))}, byID: make(map[battleEntityKey]battlePoseSpan, len(current.Units)+len(current.Projectiles))}
	}
	s.materialArena = &pub.materials
	s.timing.begin()
	census := battleCensus{Tick: current.Tick, Units: len(current.Units), Projectiles: len(current.Projectiles), Effects: len(current.Effects), Features: len(current.Features), Strips: len(current.Strips), Fragments: len(current.Fragments), Debris: len(current.Debris)}
	previousUnits, previousFeatures, previousProjectiles := s.lookup.previous()
	if previous != nil {
		for i := range previous.Units {
			u := &previous.Units[i]
			previousUnits[u.InstanceID] = u
		}
		for i := range previous.Features {
			f := &previous.Features[i]
			if f.InstanceID != 0 {
				previousFeatures[f.InstanceID] = f
			}
		}
		for i := range previous.Projectiles {
			p := &previous.Projectiles[i]
			previousProjectiles[p.PresentationID] = p
		}
	}
	unitSlots := s.lookup.slots()
	for i := range current.Units {
		unitSlots[current.Units[i].Slot] = &current.Units[i]
	}
	s.timing.split(0)
	currentUnits := s.lookup.current()
	for ui, u := range current.Units {
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
		if !battleModelUnitVisible(current, u, unitSlots, s.spectator) {
			census.RejectedUnits++
			continue
		}
		mesh, ok := s.modelIndices[s.modelKeys.key(u.Model)]
		if !ok {
			census.MissingModels++
			s.noteMissing(u.Model)
			continue
		}
		presenter, _ := battleUnitPresenter(u, unitSlots)
		priorPresenter := previousUnits[presenter.InstanceID]
		if !battleInView(presenter.X, presenter.Y, presenter.Z, camera, s.viewport, s.cullSlack) && (priorPresenter == nil || !battleInView(priorPresenter.X, priorPresenter.Y, priorPresenter.Z, camera, s.viewport, s.cullSlack)) {
			census.CulledModels++
			continue
		}
		if prior != nil && s.modelKeys.key(prior.Model) != s.modelKeys.key(u.Model) {
			prior = nil
		}
		key := battleEntityKey{kind: 1, id: u.InstanceID}
		t0 := time.Now()
		reused := s.appendUnit(pub, key, mesh, u, prior, old)
		t1 := time.Now()
		var oldInstance *Instance
		if reused >= 0 {
			oldInstance = &old.frame.Instances[reused]
			census.PosesReused++
		}
		s.applyUnitVisual(pub, &current.Units[ui], current, oldInstance)
		t2 := time.Now()
		s.applyUnitSubject(pub, u, unitSlots)
		s.timing.pose += t1.Sub(t0)
		s.timing.visual += t2.Sub(t1)
		census.SubmittedUnits++
	}
	for id := range previousUnits {
		if !currentUnits[id] {
			census.Deaths++
		}
	}
	s.timing.split(1)
	for fi, f := range current.Features {
		if f.IsBurning {
			census.BurningFeatures++
		}
		if f.Model == "" || f.Filename != "" {
			continue
		}
		if !battleFeatureVisible(current, f, s.spectator) {
			census.RejectedFeatures++
			continue
		}
		mesh, ok := s.modelIndices[s.modelKeys.key(f.Model)]
		if !ok {
			census.MissingModels++
			s.noteMissing(f.Model)
			continue
		}
		prior := previousFeatures[f.InstanceID]
		if prior != nil && s.modelKeys.key(prior.Model) != s.modelKeys.key(f.Model) {
			prior = nil
		}
		if !battleInView(f.X, f.Y, f.Z, camera, s.viewport, s.cullSlack) && (prior == nil || !battleInView(prior.X, prior.Y, prior.Z, camera, s.viewport, s.cullSlack)) {
			census.CulledModels++
			continue
		}
		u := frame.UnitView{X: f.X, Y: f.Y, Z: f.Z, Heading: f.Heading, Pitch: f.Pitch, Bank: f.Bank}
		var prevUnit *frame.UnitView
		if prior != nil {
			prevUnit = &frame.UnitView{X: prior.X, Y: prior.Y, Z: prior.Z, Heading: prior.Heading, Pitch: prior.Pitch, Bank: prior.Bank}
		}
		// The ordinal identifies only this publication's composition span.
		// Production currently publishes zero feature InstanceID, so that field
		// cannot distinguish models here or match them across ticks. Only real
		// nonzero identities above authorize prior-pose interpolation [I6].
		// Never reuse old byID ordinal spans: insertions/removals can put an
		// unrelated feature at the same index, even with the same model.
		// TODO(question): publish stable feature identities if temporal wreck
		// interpolation is wanted; zero-ID features snap to committed poses.
		s.appendUnit(pub, battleEntityKey{kind: 2, id: uint64(fi) + 1}, mesh, u, prevUnit, nil)
		s.applyFeatureVisual(pub, &current.Features[fi], current)
		census.SubmittedFeatures++
	}
	s.timing.split(2)
	for _, p := range current.Projectiles {
		// The ordinary dispatcher skips burst schedulers and recognizes only
		// these three model families; an authored Model cannot override the
		// projectile's render family [03 §5.4][06 R-WFX-01 §4].
		if p.BurstRemaining != 0 || p.Model == "" || (p.RenderType != render.RenderTypeBaseSpriteModel && p.RenderType != render.RenderTypeBaseModelDistinct && p.RenderType != render.RenderTypeRecordOrientation) {
			continue
		}
		if !battlePointVisible(current, p.X, p.Y, p.Z, s.spectator) {
			census.RejectedProjectiles++
			continue
		}
		mesh, ok := s.modelIndices[s.modelKeys.key(p.Model)]
		if !ok {
			census.MissingModels++
			s.noteMissing(p.Model)
			continue
		}
		prior := previousProjectiles[p.PresentationID]
		if prior != nil && s.modelKeys.key(prior.Model) != s.modelKeys.key(p.Model) {
			prior = nil
		}
		if !battleInView(p.X, p.Y, p.Z, camera, s.viewport, s.cullSlack) && (prior == nil || !battleInView(prior.X, prior.Y, prior.Z, camera, s.viewport, s.cullSlack)) {
			census.CulledModels++
			continue
		}
		key := battleEntityKey{kind: 3, id: p.PresentationID}
		span := s.beginInstance(pub, key, mesh)
		pub.frame.Instances[len(pub.frame.Instances)-1].Phase = PhaseEffects
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
	s.timing.split(3)
	if s.sprites != nil {
		s.sprites.SetWalkSlack(s.cullSlack)
		pub.frame.Sprites, pub.frame.Lights = s.sprites.Append(pub.frame.Sprites, pub.frame.Lights, previous, current, camera, s.viewport)
		pub.frame.Distortions = s.sprites.Distortions(pub.frame.Distortions, previous, current, camera, s.viewport)
	}
	s.timing.split(4)
	census.SubmittedDebris = s.appendDetachedDebris(pub, current, camera, s.spectator)
	s.timing.split(5)
	pub.frame.Fog = s.appendFog(current, old, fogStore)
	s.timing.split(6)
	pub.fogSource, pub.fogVersion = current.Fog.Source, current.Fog.Version
	census.SubmittedDistortions = len(pub.frame.Distortions)
	census.SubmittedModels = len(pub.frame.Instances)
	census.SubmittedSprites = len(pub.frame.Sprites)
	census.SubmittedMatrices = len(pub.frame.Current)
	for _, instance := range pub.frame.Instances {
		census.SubmittedTriangles += len(s.scene.Meshes[instance.Mesh].Indices) / 3
		if instance.Visual.State[3] > 0 {
			census.SubmittedShadows++
		}
		if instance.Visual.Outline[3] > 0 {
			census.SubmittedOutlines++
		}
		if instance.Visual.Emission[3] > 0 {
			census.SubmittedHotWrecks++
		}
	}
	pub.census = census
	s.timing.split(7)
	return pub
}

func (s *battleSource) beginInstance(pub *battlePublication, key battleEntityKey, mesh int) battlePoseSpan {
	offset, count := len(pub.frame.Current), len(s.models[mesh].Pieces)
	pub.frame.Current = slices.Grow(pub.frame.Current, count)[:offset+count]
	pub.frame.Previous = slices.Grow(pub.frame.Previous, count)[:offset+count]
	pub.frame.Instances = append(pub.frame.Instances, Instance{Mesh: mesh, PoseOffset: offset, Visual: ModelVisual{State: [4]float32{0, 1, 0, 0}}})
	pub.keys = append(pub.keys, key)
	span := battlePoseSpan{mesh: mesh, offset: offset, count: count, instance: len(pub.frame.Instances) - 1}
	pub.byID[key] = span
	return span
}

// appendUnit returns the index of old's instance whose pose it copied, or -1.
// A pose is a function of the model and battlePoseInputsEqual's fields, so a
// unit whose inputs equal the previous tick's keeps the previous tick's pose,
// which old already holds as its current endpoint.
func (s *battleSource) appendUnit(pub *battlePublication, key battleEntityKey, mesh int, current frame.UnitView, previous *frame.UnitView, old *battlePublication) int {
	span := s.beginInstance(pub, key, mesh)
	if old != nil && previous != nil {
		if prior, ok := old.byID[key]; ok && prior.mesh == span.mesh && prior.count == span.count && battlePoseInputsEqual(previous, &current) {
			pose := old.frame.Current[prior.offset : prior.offset+prior.count]
			copy(pub.frame.Current[span.offset:span.offset+span.count], pose)
			copy(pub.frame.Previous[span.offset:span.offset+span.count], pose)
			return prior.instance
		}
	}
	s.poseScratch.unitInto(pub.frame.Current[span.offset:span.offset+span.count], s.models[mesh], current)
	s.copyPriorOrSnap(pub, span, key, old, func(dst []Mat4) {
		if previous != nil {
			s.poseScratch.unitInto(dst, s.models[mesh], *previous)
		} else {
			copy(dst, pub.frame.Current[span.offset:span.offset+span.count])
		}
	})
	return -1
}

// battlePoseInputsEqual compares every field unitInto reads.
func battlePoseInputsEqual(a, b *frame.UnitView) bool {
	return a.X == b.X && a.Y == b.Y && a.Z == b.Z && a.Heading == b.Heading && a.Pitch == b.Pitch && a.Bank == b.Bank &&
		a.BMCode == b.BMCode && (a.BuildRemaining <= 0) == (b.BuildRemaining <= 0) && slices.Equal(a.Pieces, b.Pieces)
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

// battleCullMargin is the world-unit margin around the view within which a
// model's origin admits it, so a body that overhangs the edge still draws.
const battleCullMargin = 256

// battleInView admits a model origin within the view plus the margin and the
// caller's slack, both in world units.
func battleInView(x, y, z numeric.Fixed, camera [3]float32, viewport [2]int, slack float32) bool {
	// Live projection follows camera.WorldToScreen's half-height term [03 §2.5].
	dx := float32(x)/65536 - camera[0]
	dy := float32(z)/65536 - float32(y)/131072 - camera[1]
	halfW := float32(viewport[0])/(2*camera[2]) + battleCullMargin + slack
	halfH := float32(viewport[1])/(2*camera[2]) + battleCullMargin + slack
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
		s.states[index] = model.PieceState{RotX: p.RotX, RotY: p.RotY, RotZ: p.RotZ, Trans: [3]numeric.Fixed{p.Tx, p.Ty, p.Tz}, Hidden: p.Hidden, DontShade: p.DontShade, DontCache: p.DontCache}
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
		// Carry the origin with the pose endpoint selected by copyPriorOrSnap.
		// Otherwise sinking changes integer face keys even at an equal display
		// pose. These homogeneous-row lanes never participate in XYZ products.
		dst[piece][7], dst[piece][11] = float32(u.Y)/65536, 1
		// The unused homogeneous row carries presentation-only base-shade
		// admission; XYZ transforms never read it [03 R-REN-03A §4].
		if u.BMCode || (s.states[piece].DontCache && u.BuildRemaining <= 0) {
			dst[piece][3] = 1
		} else if s.states[piece].DontShade {
			dst[piece][3] = 2 // production identity SHD row 15
		}
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
		// Standalone effect pieces bypass SHD [03 R-COMP-02 §6].
		dst[piece][3] = 1
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
	// Composition already applied the chain to the zero vector.
	origin := transform.Position()
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
	SetSourceEffects(drawlist.Effects)
	SetWalkSlack(float32)
	Append([]Sprite, []Light, *frame.Frame, *frame.Frame, [3]float32, [2]int) ([]Sprite, []Light)
	// Reselect is Append at another camera for the last Append's frames.
	Reselect([]Sprite, []Light, *frame.Frame, *frame.Frame, [3]float32, [2]int) ([]Sprite, []Light)
	Distortions([]Distortion, *frame.Frame, *frame.Frame, [3]float32, [2]int) []Distortion
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
	sprites.SetSpectator(s.spectator)
	sprites.SetFeatureModelBounds(s.featureModelBounds())
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

// publicationLookup keeps the per-build identity maps; each build clears them.
// A battleSource builds on one goroutine, so one set suffices.
type publicationLookup struct {
	units       map[uint64]*frame.UnitView
	features    map[uint64]*frame.FeatureView
	projectiles map[uint64]*frame.ProjectileView
	slotUnits   map[pool.Handle]*frame.UnitView
	present     map[uint64]bool
}

func (l *publicationLookup) previous() (map[uint64]*frame.UnitView, map[uint64]*frame.FeatureView, map[uint64]*frame.ProjectileView) {
	if l.units == nil {
		l.units, l.features, l.projectiles = map[uint64]*frame.UnitView{}, map[uint64]*frame.FeatureView{}, map[uint64]*frame.ProjectileView{}
	}
	clear(l.units)
	clear(l.features)
	clear(l.projectiles)
	return l.units, l.features, l.projectiles
}

func (l *publicationLookup) slots() map[pool.Handle]*frame.UnitView {
	if l.slotUnits == nil {
		l.slotUnits = map[pool.Handle]*frame.UnitView{}
	}
	clear(l.slotUnits)
	return l.slotUnits
}

func (l *publicationLookup) current() map[uint64]bool {
	if l.present == nil {
		l.present = map[uint64]bool{}
	}
	clear(l.present)
	return l.present
}
