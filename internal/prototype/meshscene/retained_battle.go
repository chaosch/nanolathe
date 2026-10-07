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
	pub := r.source.buildPublication(previous, current, camera, old, recycle)
	r.source.applyStandaloneMaterials(pub, current)
	r.source.timing.split(8)
	r.Timings = r.source.timing.ms
	r.UnitPose, r.UnitVisual = float64(r.source.timing.pose)/1e6, float64(r.source.timing.visual)/1e6
	if b, ok := r.source.sprites.(*BattleSprites); ok {
		r.SpriteSplits = b.Splits
	}
	r.previous = pub
	return pub.frame
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
