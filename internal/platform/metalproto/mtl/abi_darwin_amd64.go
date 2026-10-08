//go:build darwin && amd64

package mtl

import (
	"math"
	"unsafe"
)

var floatTrampolineABI0 uintptr

// Call is one C call under the System V AMD64 ABI: integer and pointer
// arguments in X (rdi, rsi, rdx, rcx, r8, r9), floating-point arguments in
// D (xmm0..xmm7), and Stack for memory-class arguments and integers beyond
// the registers, in argument order. Aggregates larger than 16 bytes pass by
// value on the stack. R and F return rax, rdx and xmm0, xmm1.
type Call struct {
	Fn    uintptr
	X     [6]uintptr
	D     [8]uint64
	Stack [12]uintptr
	R     [2]uintptr
	F     [2]uint64
}

var msgSendStret uintptr

func loadArch() { msgSendStret = symbol(libobjc, "objc_msgSend_stret") }

func bits(v float64) uintptr { return uintptr(math.Float64bits(v)) }

// SendRect calls a method returning NSRect. Thirty-two bytes come back
// through a hidden result pointer: objc_msgSend_stret(&rect, self, sel).
func SendRect(id ID, sel SEL) [4]float64 {
	var a struct {
		c    Call
		rect [4]float64
	}
	a.c.Fn = msgSendStret
	a.c.X[0], a.c.X[1], a.c.X[2] = uintptr(unsafe.Pointer(&a.rect)), uintptr(id), uintptr(sel)
	a.c.Do()
	return a.rect
}

// NewWindow is [[NSWindow alloc] initWithContentRect:styleMask:backing:defer:];
// the rectangle travels by value on the stack.
func NewWindow(x, y, w, h float64, style, backing uintptr, deferred bool) ID {
	window := ID(send0(GetClass("NSWindow"), selAlloc))
	c := SendCall(window, Sel("initWithContentRect:styleMask:backing:defer:"))
	c.X[2], c.X[3], c.X[4] = style, backing, boolArg(deferred)
	c.Stack[0], c.Stack[1], c.Stack[2], c.Stack[3] = bits(x), bits(y), bits(w), bits(h)
	c.Do()
	return ID(c.R[0])
}

func setViewport(e ID, v *Viewport) {
	c := SendCall(e, selSetViewport)
	for i, f := range [6]float64{v.X, v.Y, v.W, v.H, v.Near, v.Far} {
		c.Stack[i] = bits(f)
	}
	c.Do()
}

func replaceRegion(t ID, r *Region, bytes unsafe.Pointer, rowBytes int) {
	c := SendCall(t, selReplaceRegion)
	c.X[2], c.X[3], c.X[4] = 0, uintptr(bytes), uintptr(rowBytes)
	c.Stack[0], c.Stack[1], c.Stack[2], c.Stack[3], c.Stack[4], c.Stack[5] = r.X, r.Y, r.Z, r.W, r.H, r.D
	c.Do()
}

func setClearColor(a ID, color *[4]float64) {
	c := SendCall(a, selSetClearColor)
	for i, v := range color {
		c.Stack[i] = bits(v)
	}
	c.Do()
}

func dispatch(e ID, sel SEL, a, b *[3]uintptr) {
	c := SendCall(e, sel)
	copy(c.Stack[0:3], a[:])
	copy(c.Stack[3:6], b[:])
	c.Do()
}

// copyBufferToTexture: buffer, offset, row and image bytes fill rdx..r9;
// the size, then the remaining integers and the origin go to the stack.
func copyBufferToTexture(e ID, src ID, offset, rowBytes, imageBytes int, size *[3]uintptr, dst ID, origin *[3]uintptr) {
	c := SendCall(e, selCopyBufferToTexture)
	c.X[2], c.X[3], c.X[4], c.X[5] = uintptr(src), uintptr(offset), uintptr(rowBytes), uintptr(imageBytes)
	copy(c.Stack[0:3], size[:])
	c.Stack[3], c.Stack[4], c.Stack[5] = uintptr(dst), 0, 0
	copy(c.Stack[6:9], origin[:])
	c.Do()
}

func copyTexture(e ID, src ID, sourceOrigin, size *[3]uintptr, dst ID, destinationOrigin *[3]uintptr) {
	c := SendCall(e, selCopyTextureToTexture)
	c.X[2], c.X[3], c.X[4], c.X[5] = uintptr(src), 0, 0, uintptr(dst)
	copy(c.Stack[0:3], sourceOrigin[:])
	copy(c.Stack[3:6], size[:])
	c.Stack[6], c.Stack[7] = 0, 0
	copy(c.Stack[8:11], destinationOrigin[:])
	c.Do()
}

func copyTextureToBuffer(e ID, src ID, origin, size *[3]uintptr, dst ID, offset, rowBytes, imageBytes int) {
	c := SendCall(e, selCopyTextureToBuffer)
	c.X[2], c.X[3], c.X[4], c.X[5] = uintptr(src), 0, 0, uintptr(dst)
	copy(c.Stack[0:3], origin[:])
	copy(c.Stack[3:6], size[:])
	c.Stack[6], c.Stack[7], c.Stack[8] = uintptr(offset), uintptr(rowBytes), uintptr(imageBytes)
	c.Do()
}
