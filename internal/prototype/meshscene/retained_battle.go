package meshscene

import (
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// RetainedBattle translates the production host's joined frame pair. It owns
// no simulation loop, input, HUD, session lifecycle or content provider.
type RetainedBattle struct {
	source   *battleSource
	previous *battlePublication
	spare    *battlePublication
	// The last build's pair, camera and model culling slack, for Covers and
	// Recull. A camera-only change keeps the build's instances.
	pair    [2]*frame.Frame
	camera  [3]float32
	slack   float32
	noReuse bool
	// generation numbers builds for LiveFrame.Generation.
	generation uint64
	// Timings splits the last Frame build in milliseconds: maps, units,
	// features, projectiles, sprites, debris, fog, census, materials (diagnostic).
	Timings              [9]float64
	UnitPose, UnitVisual float64
	SpriteSplits         [6]float64
}

// sectionTiming is a diagnostic split timer for publication builds.
type sectionTiming struct {
	mark         time.Time
	ms           [9]float64
	pose, visual time.Duration
}

func (t *sectionTiming) begin() { t.mark = time.Now(); t.ms = [9]float64{}; t.pose, t.visual = 0, 0 }
func (t *sectionTiming) split(i int) {
	now := time.Now()
	t.ms[i] = float64(now.Sub(t.mark)) / 1e6
	t.mark = now
}

func RetainBattle(opts BattleOptions, fs *vfs.FS, sess *session.Session) (*Scene, *RetainedBattle, error) {
	pal, err := palette.Load(fs)
	if err != nil {
		return nil, nil, err
	}
	// The production renderers resolve every indexed source through the
	// client's gamma-adjusted display palette; the terrain tiles already do.
	if opts.DisplayPalette != nil {
		pal.Base = *opts.DisplayPalette
	}
	opts.Playable = false
	opts.Seed = sess.RNGSimSeed
	opts.ViewPlayer = int(sess.LocalOwner)
	if opts.QuadMultiplier == 0 {
		opts.QuadMultiplier = 1
	}
	if opts.TextureScale == 0 {
		opts.TextureScale = 1
	}
	cx, cz := playOpeningCamera(sess)
	scene, source, err := retainBattle(opts, fs, sess.Catalog, pal, sess, cx, cz, true)
	if err != nil {
		return nil, nil, err
	}
	scene.Playable = true
	scene.Live = nil
	scene.OverlayAtlas = Texture{Width: 1, Height: 1, RGBA: []byte{255, 255, 255, 255}}
	scene.Name = "Metal world with production battle HUD and controls"
	scene.Metadata["scene_kind"] = "ordinary production battle host with retained Metal world"
	scene.Metadata["hud_policy"] = "production foreground draw list; production mouse and keyboard service"
	scene.Metadata["gameplay"] = sess.Gameplay
	scene.Metadata["crt_seed"] = sess.RNGCrtSeed
	scene.Metadata["configured_unit_limit"] = sess.Units.UnitLimit()
	scene.Metadata["camera_policy"] = "production camera and pointer service; fixed drawable at two physical pixels per logical pixel"
	scene.Metadata["pose_source"] = "production client's pinned completed frame pair; no independent simulation or presentation clock"
	scene.Metadata["terrain_policy"] = "full-resolution native TNT unique-tile atlas and lookup; optional production detail tiles; reduced whole-map fallback retained for minimap"
	return scene, &RetainedBattle{source: source}, nil
}
func (r *RetainedBattle) Frame(previous, current *frame.Frame, camera [3]float32) LiveFrame {
	if current == nil {
		return LiveFrame{Camera: camera}
	}
	old := r.previous
	// Reusing culling results at the same tick or skipping a publication must
	// not substitute a different pair for the production host's pinned pair.
	if old != nil && (previous == nil || old.frame.Tick != previous.Tick) {
		copyOld := *old
		copyOld.byID = nil
		old = &copyOld
	}
	// The production host replaces its LiveFrame with each result, so the
	// publication before r.previous has no reader left: recycle its storage.
	recycle := r.spare
	r.spare = r.previous
	// A camera at rest culls models exactly. A moving one widens the region by
	// a twelfth of the view width, so the frames until the next tick usually
	// need only Recull rather than another build.
	slack := float32(0)
	if !r.noReuse && r.previous != nil && r.camera != camera && camera[2] > 0 {
		slack = float32(r.source.viewport[0]) / (12 * camera[2])
	}
	r.source.cullSlack = slack
	pub := r.source.buildPublication(previous, current, camera, old, recycle)
	r.source.applyStandaloneMaterials(pub, current)
	r.source.cullSlack, r.source.materialArena = 0, nil
	r.pair, r.camera, r.slack = [2]*frame.Frame{previous, current}, camera, slack
	r.source.timing.split(8)
	r.Timings = r.source.timing.ms
	r.UnitPose, r.UnitVisual = float64(r.source.timing.pose)/1e6, float64(r.source.timing.visual)/1e6
	if b, ok := r.source.sprites.(*BattleSprites); ok {
		r.SpriteSplits = b.Splits
	}
	r.generation++
	pub.frame.Generation = r.generation
	r.previous = pub
	return pub.frame
}

// SetCameraReuse selects whether camera-only changes may keep a build (the
// default). Off, every camera change needs a build with exact culling.
func (r *RetainedBattle) SetCameraReuse(on bool) {
	if r != nil {
		r.noReuse = !on
	}
}

// Covers reports whether the last build's model culling region contains the
// region camera needs, so a camera-only change can keep its instances.
func (r *RetainedBattle) Covers(camera [3]float32) bool {
	if r == nil || r.noReuse || r.previous == nil || camera[2] <= 0 || r.camera[2] <= 0 {
		return false
	}
	// Recorded sprites carry the art variant chosen for their camera scale.
	if DetailScale(camera[2]) != DetailScale(r.camera[2]) {
		return false
	}
	// Both regions add the same margin, which cancels.
	for axis := range 2 {
		view := float32(r.source.viewport[axis])
		need := view / (2 * camera[2])
		have := view/(2*r.camera[2]) + r.slack
		if abs32(camera[axis]-r.camera[axis])+need > have {
			return false
		}
	}
	return true
}

// Recull reselects the last build's sprites, lights and distortions for a
// camera that Covers accepts, as a build at that camera would. Their counts
// are capped after culling, so they cannot use the widened model region.
// Instances, poses and subject identities are unchanged.
func (r *RetainedBattle) Recull(live *LiveFrame, camera [3]float32) {
	if r == nil || r.previous == nil || live == nil {
		return
	}
	pub, s := r.previous, r.source
	if s.sprites != nil {
		pub.frame.Sprites, pub.frame.Lights = s.sprites.Reselect(pub.frame.Sprites[:0], pub.frame.Lights[:0], r.pair[0], r.pair[1], camera, s.viewport)
		pub.frame.Distortions = s.sprites.Distortions(pub.frame.Distortions[:0], r.pair[0], r.pair[1], camera, s.viewport)
	}
	pub.frame.Camera = camera
	pub.census.SubmittedSprites, pub.census.SubmittedDistortions = len(pub.frame.Sprites), len(pub.frame.Distortions)
	live.Sprites, live.Lights, live.Distortions, live.Camera = pub.frame.Sprites, pub.frame.Lights, pub.frame.Distortions, camera
}

// VisitSubjects calls visit with each instance's index, subject identity and
// pose span, in instance order. f must be the last Frame result.
func (r *RetainedBattle) VisitSubjects(f *LiveFrame, visit func(i int, kind uint8, id uint64, offset, count int)) {
	if r == nil || r.previous == nil || f == nil || len(r.previous.keys) != len(f.Instances) {
		return
	}
	for i, key := range r.previous.keys {
		instance := f.Instances[i]
		// A mesh without pieces has no pose span and no subject.
		if count := len(r.source.models[instance.Mesh].Pieces); count > 0 {
			visit(i, key.kind, key.id, instance.PoseOffset, count)
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
func (r *RetainedBattle) Report() map[string]any {
	report := map[string]any{"sprites": r.source.spriteReport(), "material_frames_missing": r.source.materialFramesMissing}
	if r.previous != nil {
		report["final_census"] = r.previous.census
	}
	return report
}

func (r *RetainedBattle) SetDitheredFog(enabled bool) {
	r.source.scene.DitheredFog = enabled
}

// SetSourceEffects forwards the four currently supported source treatments:
// blast rings, vegetation shimmer, wreck glow and wreck shimmer. Other Enhanced
// switches still need their own native implementation.
func (r *RetainedBattle) SetSourceEffects(e drawlist.Effects) {
	r.source.effects = e
	if r.source.sprites != nil {
		r.source.sprites.SetSourceEffects(e)
	}
}
