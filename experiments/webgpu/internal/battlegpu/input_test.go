package battlegpu

import (
	"github.com/gogpu/gpucontext"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"testing"
)

func TestNativeBridgeIdentity(t *testing.T) {
	for _, c := range []struct {
		k          gpucontext.Key
		code, char uint32
	}{{gpucontext.KeyA, 0, 'a'}, {gpucontext.KeyZ, 6, 'z'}, {gpucontext.Key1, 18, '1'}, {gpucontext.Key0, 29, '0'}, {gpucontext.KeyF1, 122, 0}, {gpucontext.KeyF12, 111, 0}, {gpucontext.KeyUp, 126, 0}, {gpucontext.KeyPause, 127, 0xf730}} {
		code, char, ok := nativeKey(c.k)
		if !ok || code != c.code || char != c.char {
			t.Fatalf("host key %v got %d %d %v", c.k, code, char, ok)
		}
	}
}
func TestBridgeKeepsInputEdgeOrderAndDIPConversion(t *testing.T) {
	q := eventQueue{scale: 2}
	q.key(4, gpucontext.KeyA, gpucontext.ModShift)
	q.pointer(2, 1, 10, 20)
	q.pointer(3, 1, 10, 20)
	q.key(5, gpucontext.KeyA, 0)
	out := q.drain(nil)
	if len(out) != 4 || out[0].Kind != 4 || out[1].Kind != 2 || out[2].Kind != 3 || out[3].Kind != 5 {
		t.Fatalf("ordered edges: %v", out)
	}
	if out[1].X != 20 || out[1].Y != 40 || out[1].Button != 1 || out[1].Modifiers != 1 || out[3].Modifiers != 0 {
		t.Fatalf("native coordinates/modifiers: %v", out)
	}
	out = q.drain(out)
	if len(out) != 0 {
		t.Fatal("events replayed")
	}
	q.events = make([]meshscene.NativeEvent, 8192)
	q.add(meshscene.NativeEvent{Kind: 1})
	if q.overflow != 1 || len(q.events) != 8192 {
		t.Fatal("queue bound")
	}
}
