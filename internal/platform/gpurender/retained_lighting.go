package gpurender

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
)

// RetainedLight is a 64-byte CPU/GPU transfer value (GPU design §23, §31).
// PositionRadius is record X, unsheared record Y, absolute scaled height,
// radius. ColorGroundGain is displayed emission RGB and the terrain-only
// multiplier, including its family share, age/fade and player's strength.
// GroundAgeFadeKind carries source ground height, age, fade and family number:
// explosion, nano, fire, projectile, wreck, spark. Flags are model receiver on,
// ground receiver on, age present and fade present, each zero or one.
// These are RECORD pixels, not native world xyz or supersampled raster pixels.
type RetainedLight struct {
	PositionRadius, ColorGroundGain, GroundAgeFadeKind [4]float32
	Flags                                              [4]uint32
}

const RetainedLightLimit = battleLightLimit

// Family identifiers match the production gather, including its spark split.
const (
	RetainedLightExplosion  = uint32(lightExplosion)
	RetainedLightNano       = uint32(lightNano)
	RetainedLightFire       = uint32(lightFire)
	RetainedLightProjectile = uint32(lightProjectile)
	RetainedLightWreck      = uint32(lightWreck)
	RetainedLightSpark      = uint32(lightSpark)
)

// RetainedLightingSettings takes the same inputs as the production executor.
// Families are the content's weapons, nano and ground percentages (§19.4).
// Width/Height supply the fallback clip for a list without a recorded world
// region. Pass actual settings; zero percentages deliberately disable a lane.
// Glow's global switch/strength owns bloom, not this local-light gather.
type RetainedLightingSettings struct {
	Effects        drawlist.Effects
	GroundStrength int
	Families       [3]int
	Width, Height  int
}

// RetainedLighting borrows the exact production gather without constructing an
// Ebitengine executor. Its private zero-value Renderer is CPU scratch only:
// New/Execute, shader creation, image allocation and submission are never called.
// The emission cache grows only when previously unseen immutable art appears;
// light selection and output use fixed, reused storage. Use one per host reader.
type RetainedLighting struct {
	cpu     Renderer
	scratch [RetainedLightLimit]battleLight
	output  [RetainedLightLimit]RetainedLight
	sources drawlist.List
	wrecks  []drawlist.ModelGeometry
}

// RetainedWreckSource is the wreck gather's input, drawlist.WreckSource; this
// adapter adds no cooling curve, visibility decision or lifetime of its own
// (§28, §31).
type RetainedWreckSource = drawlist.WreckSource

// BeginSources assembles a source-only production record from retained batches.
// It is an alternative to Gather(fullList), never a second presentation record.
// Pass the original WorldSpace from the batch; all batches must share its
// recording coordinates and extent. Append their inputs in production order.
func (l *RetainedLighting) BeginSources(world drawlist.WorldSpace) {
	l.sources.Reset()
	l.wrecks = l.wrecks[:0]
	world.Begin = true
	l.sources.RecordWorld(world)
}

// AppendSources borrows original emitter art, emissive lines, nano fills and
// model metadata from one retained recording. Do not pass expanded sprite
// leaves as art: whole composite emitters consume exactly one source slot.
// Sprites/Flashes/Halos from an effects adapter are glow/receiver inputs, not
// additional local-light sources. This does not clone or replay any geometry.
func (l *RetainedLighting) AppendSources(art []drawlist.Sprite, lines []drawlist.Line, nano []drawlist.Fill, models []drawlist.Model) {
	for _, source := range art {
		l.sources.RecordLightSource(source)
	}
	for _, line := range lines {
		l.sources.RecordLine(line)
	}
	for _, fill := range nano {
		l.sources.RecordFill(fill)
	}
	for _, model := range models {
		l.sources.RecordModel(model)
	}
}

// AppendWrecks supplies light-only retained model descriptors without generating
// model faces. The shallow proxy reproduces modelWorldBounds exactly; production
// prepareWreckLighting still owns admission, radius, energy, clipping and budget.
// Include a model through AppendSources OR here, never both.
func (l *RetainedLighting) AppendWrecks(sources []RetainedWreckSource) {
	start := len(l.wrecks)
	for _, source := range sources {
		l.wrecks = append(l.wrecks, drawlist.ModelGeometry{
			Width: int32(source.Bounds.Dx()), Height: int32(source.Bounds.Dy()),
			AnchorX: int32(source.Bounds.Min.X), AnchorY: int32(source.Bounds.Min.Y),
			WorldHeight: source.WorldHeight, WreckHeatScale: source.Scale, WreckEmission: source.Emission,
		})
	}
	for i, source := range sources {
		l.sources.RecordModel(drawlist.Model{Geometry: &l.wrecks[start+i], ShadowOnly: source.ShadowOnly})
	}
}

// GatherSources performs the one ordinary gather on the assembled metadata.
// Source arrays/model headers are borrowed until this returns; result storage
// has Gather's lifetime. No simulation/presentation cursor or private CRT runs.
func (l *RetainedLighting) GatherSources() []RetainedLight { return l.Gather(&l.sources) }

func NewRetainedLighting(palette [256][4]byte, settings RetainedLightingSettings) *RetainedLighting {
	l := &RetainedLighting{}
	l.cpu.lighting.lights = l.scratch[:0]
	l.cpu.lighting.colors = make(map[*formats.GAFFrame][3]float32)
	l.cpu.displayPalette = palette
	l.SetSettings(settings)
	return l
}

// SetPalette invalidates only intrinsic emission measurements, exactly as the
// production display-palette update does, with no GPU table upload.
func (l *RetainedLighting) SetPalette(palette [256][4]byte) {
	if l.cpu.displayPalette != palette {
		l.cpu.displayPalette = palette
		clear(l.cpu.lighting.colors)
	}
}

func (l *RetainedLighting) SetSettings(s RetainedLightingSettings) {
	l.cpu.SetEffects(s.Effects)
	l.cpu.SetGroundLightStrength(s.GroundStrength)
	l.cpu.SetGlowFamilies(s.Families[0], s.Families[1], s.Families[2])
	l.cpu.w, l.cpu.h = s.Width, s.Height
}

// ResetSources releases cached art references when its battle/content ends.
func (l *RetainedLighting) ResetSources() { clear(l.cpu.lighting.colors) }

// Gather accepts a pinned, visibility-admitted production record. It does not
// record, advance presentation or retain list-owned data. Returned storage is
// borrowed until the next Gather; a native upload must consume it synchronously.
// Family caps/reserves, stable eviction, nano clustering, palette measurement,
// source clipping and toggles all remain prepareBattleLighting's single policy.
func (l *RetainedLighting) Gather(list *drawlist.List) []RetainedLight {
	if list == nil {
		return l.output[:0]
	}
	l.cpu.prepareBattleLighting(list)
	state := &l.cpu.lighting
	for i, source := range state.lights {
		out := RetainedLight{
			PositionRadius:    [4]float32{source.position[0], source.position[1], source.position[2], source.radius},
			ColorGroundGain:   [4]float32{source.color[0], source.color[1], source.color[2], 0},
			GroundAgeFadeKind: [4]float32{source.ground, source.age, source.fade, float32(source.kind)},
		}
		if !state.modelDisabled {
			out.Flags[0] = 1
		}
		if !state.groundDisabled {
			out.Flags[1] = 1
			out.ColorGroundGain[3] = groundScale(&source) * (1 + state.groundStrengthOffset)
		}
		if source.ageKnown {
			out.Flags[2] = 1
		}
		if source.fadeKnown {
			out.Flags[3] = 1
		}
		l.output[i] = out
	}
	return l.output[:len(state.lights)]
}
