// Package metalhud adapts the production foreground draw list to the throwaway
// native renderer. It owns presentation resources only (GPU design §2, I6).
package metalhud

import (
	"fmt"
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

const atlasSize = 4096

// Foreground is worker-owned. Prepare borrows the list synchronously; returned
// quads, effect layers and atlas bytes remain valid until the next Prepare or
// PrepareEffectLayers. Consumers must finish packing/copying before that call;
// native command buffers read their own ring storage. Palette tables and loaded
// art must remain immutable for this object's lifetime.
type Foreground struct {
	pal           *palette.Tables
	w, h          int
	atlas         meshscene.Texture
	version       uint64
	dirty         [4]int
	first         bool
	x, y, shelf   int
	quads         []meshscene.OverlayQuad
	effectLayers  []meshscene.EffectLayer
	err           error
	world         bool
	scale, ox, oy float32
	cw, ch        int
	images        map[imageKey]region
	fonts         map[fontKey]region
	markers       map[markerKey]region
	surfaces      map[surfaceKey]*surfaceEntry
	surfaceUses   map[uint64]int
	effectArt     map[effectArtKey]region
}

var _ drawlist.Sink = (*Foreground)(nil)
var _ drawlist.WorldSink = (*Foreground)(nil)
var _ drawlist.MarkerSink = (*Foreground)(nil)

// New allocates a retained 4096-square RGBA atlas. Pixel zero is opaque white,
// shared by solid primitives. Palette index zero is otherwise ordinary ink.
func New(pal *palette.Tables, width, height int) *Foreground {
	f := &Foreground{pal: pal, w: width, h: height, cw: width, ch: height,
		atlas:   meshscene.Texture{Width: atlasSize, Height: atlasSize, RGBA: make([]byte, atlasSize*atlasSize*4)},
		version: 1, first: true, dirty: [4]int{0, 0, atlasSize, atlasSize}, x: 1, shelf: 1, images: make(map[imageKey]region), fonts: make(map[fontKey]region), markers: make(map[markerKey]region),
		surfaces: make(map[surfaceKey]*surfaceEntry), surfaceUses: make(map[uint64]int)}
	copy(f.atlas.RGBA[:4], []byte{255, 255, 255, 255})
	return f
}

// Prepare preserves List.Replay's paint order, including destination reads.
// Version changes only when retained texture bytes change. An error rejects the
// entire foreground rather than quietly omitting unsupported art.
func (f *Foreground) Prepare(list *drawlist.List) ([]meshscene.OverlayQuad, meshscene.Texture, uint64, error) {
	f.beginPrepare()
	if list != nil {
		list.Replay(f)
	}
	if f.err != nil {
		return nil, f.atlas, f.version, f.err
	}
	return f.quads, f.atlas, f.version, nil
}

func (f *Foreground) beginPrepare() {
	for i := range f.effectLayers {
		resetEffectLayer(&f.effectLayers[i])
	}
	f.quads = f.quads[:0]
	f.dirty = [4]int{}
	if f.first {
		f.dirty = [4]int{0, 0, atlasSize, atlasSize}
		f.first = false
	}
	f.err = nil
	f.world = false
	f.scale = 1
	f.ox = 0
	f.oy = 0
	f.cw = f.w
	f.ch = f.h
	clear(f.surfaceUses)
	if f.w <= 0 || f.h <= 0 {
		f.fail("invalid logical surface %dx%d", f.w, f.h)
	}
}

// DirtyRect returns the bounding rectangle of atlas texels changed by the most
// recent Prepare, in x/y/width/height pixels. The first Prepare returns the whole
// atlas; unchanged content returns zero extent. A native consumer can stage just
// these rows from the retained full atlas and update its existing texture before
// drawing this frame. DirtyRect and bytes are borrowed until the next Prepare.
func (f *Foreground) DirtyRect() [4]int { return f.dirty }

func (f *Foreground) dirtyPixel(x, y int) {
	if f.dirty[2] == 0 || f.dirty[3] == 0 {
		f.dirty = [4]int{x, y, 1, 1}
		return
	}
	x0, y0 := min(f.dirty[0], x), min(f.dirty[1], y)
	x1, y1 := max(f.dirty[0]+f.dirty[2], x+1), max(f.dirty[1]+f.dirty[3], y+1)
	f.dirty = [4]int{x0, y0, x1 - x0, y1 - y0}
}

func (f *Foreground) fail(format string, args ...any) {
	if f.err == nil {
		f.err = fmt.Errorf("nanolathe: native foreground: "+format, args...)
	}
}

func (f *Foreground) color(index byte) [4]float32 {
	r, g, b, _ := f.pal.RGBA(index)
	return [4]float32{float32(r) / 255, float32(g) / 255, float32(b) / 255, 1}
}

// emit mirrors the executor's world transform and minimum one-pixel primitive
// span (GPU design §16.3). UVs retain the original mapping through clipping.
func (f *Foreground) emit(x0, y0, x1, y1, u0, v0, u1, v1 float32, color [4]float32, multiply bool) {
	if f.err != nil || x0 >= x1 || y0 >= y1 {
		return
	}
	if f.world {
		x0 = x0*f.scale + f.ox
		x1 = x1*f.scale + f.ox
		y0 = y0*f.scale + f.oy
		y1 = y1*f.scale + f.oy
		x1 = max(x1, x0+1)
		y1 = max(y1, y0+1)
	}
	ax0, ay0, ax1, ay1 := max(x0, 0), max(y0, 0), min(x1, float32(f.w)), min(y1, float32(f.h))
	if ax0 >= ax1 || ay0 >= ay1 {
		return
	}
	du, dv := (u1-u0)/(x1-x0), (v1-v0)/(y1-y0)
	q := meshscene.OverlayQuad{Rect: [4]float32{ax0, ay0, ax1 - ax0, ay1 - ay0},
		UV: [4]float32{u0 + (ax0-x0)*du, v0 + (ay0-y0)*dv, u0 + (ax1-x0)*du, v0 + (ay1-y0)*dv}, Color: color}
	if multiply {
		q.Params[1] = 1
	}
	f.quads = append(f.quads, q)
}

func (f *Foreground) solid(x0, y0, x1, y1 int, color [4]float32, multiply bool) {
	x0 = max(x0, 0)
	y0 = max(y0, 0)
	x1 = min(x1, f.cw)
	y1 = min(y1, f.ch)
	// Constant texel-center UV avoids sampling adjacent art at thin spans.
	u := float32(.5 / atlasSize)
	f.emit(float32(x0), float32(y0), float32(x1), float32(y1), u, u, u, u, color, multiply)
}

func (f *Foreground) World(w drawlist.WorldSpace) {
	if !w.Begin {
		f.world = false
		f.cw = f.w
		f.ch = f.h
		return
	}
	f.world = true
	f.cw = max(f.w, int(w.RecordW))
	f.ch = max(f.h, int(w.RecordH))
	step := float32(camera.ZoomOf(w.Step)) / float32(camera.ZoomUnit)
	f.scale = 1
	if step > 0 {
		if w.Factor > 0 {
			f.scale = w.Factor / step
		} else if w.Zoom > 0 {
			f.scale = float32(w.Zoom) / float32(camera.ZoomUnit) / step
		}
	}
	f.ox = w.OffsetX
	f.oy = w.OffsetY
}

func (f *Foreground) project(x, y int32) (int32, int32) {
	if !f.world {
		return x, y
	}
	return int32(math.Floor(float64(float32(x)*f.scale+f.ox) + .5)), int32(math.Floor(float64(float32(y)*f.scale+f.oy) + .5))
}

func (f *Foreground) Clear()  { f.solid(0, 0, f.cw, f.ch, f.color(0), false) }
func (f *Foreground) Expand() {}
func (f *Foreground) Terrain(drawlist.Terrain) {
	f.fail("unexpected terrain command in foreground list")
}
func (f *Foreground) Model(drawlist.Model) { f.fail("unsupported model command in foreground list") }
func (f *Foreground) Fog(drawlist.Fog)     { f.fail("unexpected fog command in foreground list") }
