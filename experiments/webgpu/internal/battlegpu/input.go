package battlegpu

import (
	"github.com/gogpu/gpucontext"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"sync"
)

// Input bridge preserves ordered edges and physical holds. Coordinates enter
// GoGPU in DIP and leave this bridge in drawable pixels, as the production
// Metal input adapter expects (DESIGN_METAL_RENDERER §6).
type eventQueue struct {
	mu       sync.Mutex
	events   []meshscene.NativeEvent
	mods     uint32
	scale    float32
	overflow uint64
}

func (q *eventQueue) add(e meshscene.NativeEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.events) >= 8192 {
		q.overflow++
		return
	}
	q.events = append(q.events, e)
}
func (q *eventQueue) pointer(kind uint32, button uint32, x, y float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := meshscene.NativeEvent{Kind: kind, Button: button, Key: 1, Modifiers: q.mods, X: float32(x) * q.scale, Y: float32(y) * q.scale}
	if kind == 1 {
		e.Key = 0
	}
	if len(q.events) >= 8192 {
		q.overflow++
		return
	}
	q.events = append(q.events, e)
}
func (q *eventQueue) drain(dst []meshscene.NativeEvent) []meshscene.NativeEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	dst = append(dst[:0], q.events...)
	q.events = q.events[:0]
	return dst
}
func (q *eventQueue) key(kind uint32, k gpucontext.Key, m gpucontext.Modifiers) {
	q.mu.Lock()
	q.mods = uint32(m) & 15
	mods := q.mods
	q.mu.Unlock()
	code, char, ok := nativeKey(k)
	if ok {
		q.add(meshscene.NativeEvent{Kind: kind, Button: code, Key: char, Modifiers: mods})
	} else {
		q.add(meshscene.NativeEvent{Kind: 6, Modifiers: mods})
	}
}

// These are public host key identities, not retail executable constants. The
// reverse table corresponds to cmd/nanolathe/metal_input_darwin.go.
func nativeKey(k gpucontext.Key) (code, char uint32, ok bool) {
	letters := [26]uint32{0, 11, 8, 2, 14, 3, 5, 4, 34, 38, 40, 37, 46, 45, 31, 35, 12, 15, 1, 17, 32, 9, 13, 7, 16, 6}
	if k >= gpucontext.KeyA && k <= gpucontext.KeyZ {
		return letters[k-gpucontext.KeyA], uint32('a') + uint32(k-gpucontext.KeyA), true
	}
	digits := [10]uint32{29, 18, 19, 20, 21, 23, 22, 26, 28, 25}
	if k >= gpucontext.Key0 && k <= gpucontext.Key9 {
		return digits[k-gpucontext.Key0], uint32('0') + uint32(k-gpucontext.Key0), true
	}
	function := [12]uint32{122, 120, 99, 118, 96, 97, 98, 100, 101, 109, 103, 111}
	if k >= gpucontext.KeyF1 && k <= gpucontext.KeyF12 {
		return function[k-gpucontext.KeyF1], 0, true
	}
	switch k {
	case gpucontext.KeyLeft:
		return 123, 0, true
	case gpucontext.KeyRight:
		return 124, 0, true
	case gpucontext.KeyDown:
		return 125, 0, true
	case gpucontext.KeyUp:
		return 126, 0, true
	case gpucontext.KeyHome:
		return 115, 0, true
	case gpucontext.KeyEnd:
		return 119, 0, true
	case gpucontext.KeyPageUp:
		return 116, 0, true
	case gpucontext.KeyPageDown:
		return 121, 0, true
	case gpucontext.KeyInsert:
		return 114, 0xf727, true
	case gpucontext.KeyDelete:
		return 117, 0, true
	case gpucontext.KeyPause:
		return 127, 0xf730, true
	case gpucontext.KeyEscape:
		return 53, 27, true
	case gpucontext.KeyEnter:
		return 36, 13, true
	case gpucontext.KeyTab:
		return 48, 9, true
	case gpucontext.KeySpace:
		return 49, 32, true
	case gpucontext.KeyBackspace:
		return 51, 8, true
	case gpucontext.KeyEqual:
		return 24, '=', true
	case gpucontext.KeyMinus:
		return 27, '-', true
	case gpucontext.KeyComma:
		return 43, ',', true
	case gpucontext.KeyPeriod:
		return 47, '.', true
	case gpucontext.KeyGrave:
		return 50, '`', true
	case gpucontext.KeyNumpadAdd:
		return 69, '+', true
	case gpucontext.KeyNumpadSubtract:
		return 78, '-', true
	}
	return 0, 0, false
}
