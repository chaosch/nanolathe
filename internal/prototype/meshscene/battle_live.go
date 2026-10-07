package meshscene

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

const battlePeriod = time.Second / 30

type battleSource struct {
	materialAtlas                                     *atlasResult
	materialSource                                    *ModelMaterialSource
	materialFramesMissing                             uint64
	effects                                           drawlist.Effects
	scene                                             *Scene
	sess                                              *session.Session // accessed only during preparation or by worker
	fs                                                *vfs.FS
	models                                            []*model.Model
	modelIndices                                      map[string]int
	originY                                           float32
	viewport                                          [2]int
	sprites                                           battleSpriteAdapter
	play                                              *PlayController
	pendingInputs                                     []PlayInput
	inputOverflow                                     uint64
	ended                                             bool
	spectator                                         bool
	palette                                           *palette.Tables
	fogGray, fogBlack                                 [4]*formats.GAFEntry
	poseScratch                                       battlePoseScratch
	timing                                            sectionTiming
	materialCache                                     map[modelMaterialCacheKey][]NativeModelMaterial
	lookup                                            publicationLookup
	latest                                            atomic.Pointer[battlePublication]
	mu                                                sync.Mutex // short camera/lifecycle exchange; never held while stepping
	camera                                            [3]float32
	paused, started, closed                           bool
	nextSeconds                                       float64
	hasSeconds                                        bool
	lastRenderTick                                    uint32
	renderCalls, heldRenderCalls, skippedPublications uint64
	workerErr                                         error
	stop, done                                        chan struct{}
	closeOnce                                         sync.Once
	finalReport                                       map[string]any
	// Everything below is worker-owned until Close joins it.
	totalTicks         uint64
	tickRecords        []battleTickRecord
	missingModels      map[string]int
	startTime, endTime time.Time
	pausedSeconds      float64
	missedDeadlines    int
}

type battleTickRecord struct {
	Tick              uint32       `json:"tick"`
	ElapsedSeconds    float64      `json:"elapsed_seconds"`
	SimulationMS      float64      `json:"simulation_ms"`
	PosePreparationMS float64      `json:"pose_preparation_ms"`
	TickWorkMS        float64      `json:"tick_work_ms"`
	StartLatenessMS   float64      `json:"start_lateness_ms"`
	MissedDeadlines   int          `json:"missed_30hz_deadlines"`
	Camera            [3]float32   `json:"camera"`
	Census            battleCensus `json:"census"`
}

func newBattleSource(scene *Scene, sess *session.Session, fs *vfs.FS, models []*model.Model, indices map[string]int, originY float32, viewport [2]int) *battleSource {
	return &battleSource{effects: drawlist.AllEffects(), scene: scene, sess: sess, fs: fs, models: models, modelIndices: indices, originY: originY, viewport: viewport, camera: scene.Camera, stop: make(chan struct{}), done: make(chan struct{}), missingModels: map[string]int{}}
}
func (s *battleSource) noteMissing(name string) { s.missingModels[name]++ }
func (s *battleSource) publishInitial() error {
	current, previous := s.sess.Snapshot.PinLatest()
	if current == nil {
		return fmt.Errorf("nanolathe: live prototype publication unavailable: logical path committed frame, providers searched [session snapshot], expected opening battle frame")
	}
	pub := s.buildPublication(previous, current, s.camera, nil, nil)
	if s.play != nil {
		pub.frame.Overlay = s.play.Overlay(current, s.camera, false)
	}
	s.sess.Snapshot.Unpin(current)
	s.sess.Snapshot.Unpin(previous)
	pub.blendStart = time.Now().Add(battlePeriod)
	s.latest.Store(pub)
	return nil
}

// Next neither steps nor inspects the session. It exchanges host camera input
// and takes an immutable publication, so a slow sim tick cannot block display.
func (s *battleSource) Next(seconds float64, input Input) (LiveFrame, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return LiveFrame{}, fmt.Errorf("nanolathe: live prototype source closed: logical path presentation frame, providers searched [live source], expected running worker")
	}
	if !s.started {
		// CLI resolution/framing edits are complete before the first Next call.
		s.camera = s.scene.Camera
		if initial := s.latest.Load(); initial != nil {
			copyPublication := *initial
			copyPublication.blendStart = time.Now().Add(battlePeriod)
			copyPublication.previous = nil
			s.latest.Store(&copyPublication)
		}
		s.started = true
		go s.run()
	}
	dt := float64(0)
	if s.hasSeconds && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
		dt = max(0, min(seconds-s.nextSeconds, 0.1))
	}
	s.nextSeconds = seconds
	s.hasSeconds = true
	speed := float32(600*dt) / max(s.camera[2], 0.1)
	s.camera[0] += min(max(input.PanX, -1), 1) * speed
	s.camera[1] += min(max(input.PanZ, -1), 1) * speed
	if input.Zoom != 0 && !math.IsNaN(float64(input.Zoom)) && !math.IsInf(float64(input.Zoom), 0) {
		s.camera[2] *= float32(math.Pow(1.12, float64(input.Zoom)))
	}
	s.camera[2] = min(max(s.camera[2], 0.1), 4)
	s.clampCamera()
	if s.play != nil {
		for _, event := range input.Events {
			sample := PlayInput{Event: event, Camera: s.camera}
			n := len(s.pendingInputs)
			if n > 0 && event.Kind == 1 && event.WheelX == 0 && event.WheelY == 0 && s.pendingInputs[n-1].Event.Kind == 1 && s.pendingInputs[n-1].Event.WheelX == 0 && s.pendingInputs[n-1].Event.WheelY == 0 {
				s.pendingInputs[n-1] = sample
			} else if n < 8192 {
				s.pendingInputs = append(s.pendingInputs, sample)
			} else {
				s.inputOverflow++
			}
		}
	}
	if input.PauseToggle {
		s.paused = !s.paused
	}
	camera, paused, workerErr := s.camera, s.paused || s.ended, s.workerErr
	pub := s.latest.Load()
	now := time.Now()
	if pub != nil && !paused && now.Before(pub.blendStart) && pub.previous != nil {
		pub = pub.previous
	}
	s.renderCalls++
	if pub != nil {
		if s.lastRenderTick == pub.frame.Tick {
			s.heldRenderCalls++
		} else {
			if s.lastRenderTick != 0 && pub.frame.Tick > s.lastRenderTick+1 {
				s.skippedPublications += uint64(pub.frame.Tick - s.lastRenderTick - 1)
			}
			s.lastRenderTick = pub.frame.Tick
		}
	}
	s.mu.Unlock()
	if pub == nil {
		return LiveFrame{}, fmt.Errorf("nanolathe: live prototype opening frame missing: logical path presentation frame, providers searched [worker publication], expected immutable opening pose")
	}
	result := pub.frame
	result.Camera = camera
	result.Alpha = float32(min(max(float64(now.Sub(pub.blendStart))/float64(battlePeriod), 0), 1))
	if paused {
		// Pause intentionally snaps to the latest committed pose, rather than
		// leaving an unfinished interpolation running while gameplay is stopped.
		result.Alpha = 1
	}
	return result, workerErr
}
func (s *battleSource) clampCamera() {
	r := s.scene.TerrainRect
	leading, trailing := [2]float32{}, [2]float32{}
	if s.play != nil {
		scale := max(float32(1), min(float32(s.viewport[0])/1440, float32(s.viewport[1])/900))
		leading = [2]float32{300 * scale, 56 * scale}
		trailing = [2]float32{0, 36 * scale}
	}
	for axis := 0; axis < 2; axis++ {
		lo, hi := r[axis], r[axis+2]
		half := float32(s.viewport[axis]) / (2 * s.camera[2])
		a, b := lo+half-leading[axis]/s.camera[2], hi-half+trailing[axis]/s.camera[2]
		if a > b {
			s.camera[axis] = (a + b) / 2
		} else {
			s.camera[axis] = min(max(s.camera[axis], a), b)
		}
	}
}
func (s *battleSource) inputSnapshot() ([3]float32, bool, []PlayInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.pendingInputs
	s.pendingInputs = nil
	return s.camera, s.paused, events
}
func (s *battleSource) processInput(camera [3]float32, events []PlayInput) [3]float32 {
	if s.play == nil {
		return camera
	}
	// Session access and controller state are confined to this worker.
	current, previous := s.sess.Snapshot.PinLatest()
	moved := s.play.Process(events, current, camera)
	s.sess.Snapshot.Unpin(current)
	s.sess.Snapshot.Unpin(previous)
	s.mu.Lock()
	// Preserve a newer native pan/zoom while the worker was processing commands.
	if s.camera == camera {
		s.camera = moved
		s.clampCamera()
	}
	camera = s.camera
	s.mu.Unlock()
	return camera
}

// A deadline consumes one ordinary tick. If processing misses intervals, the
// host skips those scheduling opportunities rather than inventing catch-up
// ticks; the report gives actual cadence and every missed opportunity. This
// policy affects wall time only, not what a tick computes [I6].
func (s *battleSource) run() {
	defer close(s.done)
	s.startTime = time.Now()
	var pausedAt time.Time
	defer func() {
		s.endTime = time.Now()
		if !pausedAt.IsZero() {
			s.pausedSeconds += s.endTime.Sub(pausedAt).Seconds()
		}
	}()
	deadline := s.startTime.Add(battlePeriod)
	timer := time.NewTimer(battlePeriod)
	defer timer.Stop()
	var drained []frame.EventView
	for {
		wait := max(time.Until(deadline), 0)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-s.stop:
			return
		case <-timer.C:
		}
		camera, paused, events := s.inputSnapshot()
		camera = s.processInput(camera, events)
		ended := s.sess.State == session.StatePostBattle
		if f := s.sess.Snapshot.Current(); f != nil && f.Result.Ended {
			ended = true
		}
		if paused || ended {
			if pausedAt.IsZero() {
				pausedAt = time.Now()
			}
			if s.play != nil {
				current, previous := s.sess.Snapshot.PinLatest()
				if current != nil {
					pub := s.buildPublication(previous, current, camera, s.latest.Load(), nil)
					pub.frame.Overlay = s.play.Overlay(current, camera, paused)
					pub.blendStart = time.Now().Add(-battlePeriod)
					s.latest.Store(pub)
				}
				s.sess.Snapshot.Unpin(current)
				s.sess.Snapshot.Unpin(previous)
			}
			s.mu.Lock()
			s.ended = ended
			s.mu.Unlock()
			deadline = time.Now().Add(battlePeriod)
			continue
		}
		start := time.Now()
		if !pausedAt.IsZero() {
			s.pausedSeconds += start.Sub(pausedAt).Seconds()
			pausedAt = time.Time{}
		}
		late := max(start.Sub(deadline), 0)
		if err := battleStep(s.sess); err != nil {
			s.mu.Lock()
			s.workerErr = err
			s.mu.Unlock()
			return
		}
		simDone := time.Now()
		current, previous := s.sess.Snapshot.PinLatest()
		if current == nil {
			s.mu.Lock()
			s.workerErr = fmt.Errorf("nanolathe: live prototype worker publication missing: logical path committed frame, providers searched [snapshot], expected completed tick")
			s.mu.Unlock()
			return
		}
		old := s.latest.Load()
		pub := s.buildPublication(previous, current, camera, old, nil)
		if s.play != nil {
			pub.frame.Overlay = s.play.Overlay(current, camera, false)
		}
		s.sess.Snapshot.Unpin(current)
		s.sess.Snapshot.Unpin(previous)
		drained = s.sess.Snapshot.DrainCommittedEvents(drained)
		finish := time.Now()
		// One fixed tick of presentation buffering lets preparation complete
		// before this pair begins blending, without extrapolating world state.
		pub.blendStart = deadline.Add(battlePeriod)
		if old != nil {
			prior := *old
			prior.previous = nil // Never retain a chain of the entire battle.
			pub.previous = &prior
		}
		deadline = deadline.Add(battlePeriod)
		missed := 0
		if !finish.Before(deadline) {
			missed = int(finish.Sub(deadline)/battlePeriod) + 1
			deadline = deadline.Add(time.Duration(missed) * battlePeriod)
			s.missedDeadlines += missed
		}
		record := battleTickRecord{Tick: pub.frame.Tick, ElapsedSeconds: finish.Sub(s.startTime).Seconds(), SimulationMS: float64(simDone.Sub(start)) / 1e6, PosePreparationMS: float64(finish.Sub(simDone)) / 1e6, TickWorkMS: float64(finish.Sub(start)) / 1e6, StartLatenessMS: float64(late) / 1e6, MissedDeadlines: missed, Camera: camera, Census: pub.census}
		if s.play != nil && len(s.tickRecords) >= 8192 {
			s.tickRecords[s.totalTicks%8192] = record
		} else {
			s.tickRecords = append(s.tickRecords, record)
		}
		s.totalTicks++
		// Publishing fully prepared, detached slices requires no snapshot pins on
		// the renderer. Old arrays are never overwritten while any Next owns them.
		s.latest.Store(pub)
	}
}

// Close is safe both as the native host's finalizer and a deferred cleanup.
// It joins before reading worker diagnostics or closing content providers.
func (s *battleSource) Close() map[string]any {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		started := s.started
		close(s.stop)
		s.mu.Unlock()
		if started {
			<-s.done
		} else {
			close(s.done)
		}
		if s.play != nil && s.totalTicks > 8192 {
			at := int(s.totalTicks % 8192)
			ordered := append([]battleTickRecord(nil), s.tickRecords[at:]...)
			s.tickRecords = append(ordered, s.tickRecords[:at]...)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		elapsed := float64(0)
		if !s.startTime.IsZero() {
			elapsed = s.endTime.Sub(s.startTime).Seconds()
		}
		active := max(elapsed-s.pausedSeconds, 0)
		tickRange := [2]uint32{}
		if len(s.tickRecords) > 0 {
			tickRange = [2]uint32{s.tickRecords[0].Tick, s.tickRecords[len(s.tickRecords)-1].Tick}
		}
		missing := make([]map[string]any, 0, len(s.missingModels))
		names := make([]string, 0, len(s.missingModels))
		for name := range s.missingModels {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			missing = append(missing, map[string]any{"model": name, "observed_missing_instances": s.missingModels[name]})
		}
		rate := float64(0)
		if active > 0 {
			rate = float64(s.totalTicks) / active
		}
		s.finalReport = map[string]any{"tick_records": s.tickRecords, "tick_range": tickRange, "ticks": s.totalTicks, "retained_tick_records": len(s.tickRecords), "elapsed_seconds": elapsed, "paused_seconds": s.pausedSeconds, "active_seconds": active, "actual_ticks_per_active_second": rate, "target_ticks_per_second": 30, "missed_30hz_deadlines": s.missedDeadlines, "render_calls": s.renderCalls, "render_calls_holding_same_tick": s.heldRenderCalls, "worker_publications_skipped_by_renderer": s.skippedPublications, "missing_models": missing, "sprites": s.spriteReport(), "scheduler_policy": "one ordinary tick per available 30 Hz deadline; missed opportunities skipped; no display-driven stepping", "publication_storage": "immutable detached slices; Go owns lifetime, no retained sampled replay", "final_session_state": s.sess.State}
		s.finalReport["presentation_buffer_ticks"] = 1
		s.finalReport["presentation_buffer_seconds"] = battlePeriod.Seconds()
		s.finalReport["presentation_policy"] = "fixed one-tick buffering; only current and immediately prior publication retained; pause snaps to latest committed pose; no extrapolation"
		if pub := s.latest.Load(); pub != nil {
			s.finalReport["final_census"] = pub.census
		}
		if s.workerErr != nil {
			s.finalReport["worker_error"] = s.workerErr.Error()
		}
		if s.play != nil {
			s.finalReport["play"] = s.play.Report()
			s.finalReport["input_queue_overflow"] = s.inputOverflow
		}
		closePlayControllers(s.sess)
		if err := s.fs.Close(); err != nil {
			s.finalReport["content_close_error"] = err.Error()
		}
	})
	return s.finalReport
}
