package meshscene

import (
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
}

func RetainBattle(opts BattleOptions, fs *vfs.FS, sess *session.Session) (*Scene, *RetainedBattle, error) {
	pal, err := palette.Load(fs)
	if err != nil {
		return nil, nil, err
	}
	opts.Playable = false
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
	scene.Metadata["terrain_policy"] = "whole map, native TNT pixels up to 8192 per axis; larger maps nearest downsampled"
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
	pub := r.source.buildPublication(previous, current, camera, old)
	r.previous = pub
	return pub.frame
}
func (r *RetainedBattle) Report() map[string]any {
	report := map[string]any{"sprites": r.source.spriteReport()}
	if r.previous != nil {
		report["final_census"] = r.previous.census
	}
	return report
}
