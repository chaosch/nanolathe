//go:build darwin && arm64

package mtl

import "unsafe"

var floatTrampolineABI0 uintptr

// Call is one C call's register file under AAPCS64: integer and pointer
// arguments in X, floating-point arguments (as their bit patterns, a float32
// in the low half) in D, further integer arguments in Stack, and X8 for an
// indirect result (unused so far). R and F return x0, x1 and d0..d3. Composites larger than
// 16 bytes that are not floating-point aggregates pass by reference.
type Call struct {
	Fn    uintptr
	X     [8]uintptr
	D     [8]uint64
	Stack [4]uintptr
	x8    uintptr
	R     [2]uintptr
	F     [4]uint64
}

func loadArch() {}

// SendRect calls a method returning NSRect, an aggregate of four doubles
// returned in d0..d3.
func SendRect(id ID, sel SEL) [4]float64 {
	c := SendCall(id, sel)
	c.Do()
	return [4]float64{c.Double(0), c.Double(1), c.Double(2), c.Double(3)}
}

// NewWindow is [[NSWindow alloc] initWithContentRect:styleMask:backing:defer:].
func NewWindow(x, y, w, h float64, style, backing uintptr, deferred bool) ID {
	window := ID(send0(GetClass("NSWindow"), selAlloc))
	c := SendCall(window, Sel("initWithContentRect:styleMask:backing:defer:"))
	c.SetDouble(0, x)
	c.SetDouble(1, y)
	c.SetDouble(2, w)
	c.SetDouble(3, h)
	c.X[2], c.X[3], c.X[4] = style, backing, boolArg(deferred)
	c.Do()
	return ID(c.R[0])
}

func setViewport(e ID, v *Viewport) { send1(e, selSetViewport, uintptr(unsafe.Pointer(v))) }

func replaceRegion(t ID, r *Region, bytes unsafe.Pointer, rowBytes int) {
	Send(t, selReplaceRegion, uintptr(unsafe.Pointer(r)), 0, uintptr(bytes), uintptr(rowBytes))
}

// setClearColor passes MTLClearColor, four doubles, in d0..d3.
func setClearColor(a ID, color *[4]float64) {
	c := SendCall(a, selSetClearColor)
	for i, v := range color {
		c.SetDouble(i, v)
	}
	c.Do()
}

func dispatch(e ID, sel SEL, a, b *[3]uintptr) {
	send2(e, sel, uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)))
}

func copyBufferToTexture(e ID, src ID, offset, rowBytes, imageBytes int, size *[3]uintptr, dst ID, origin *[3]uintptr) {
	c := SendCall(e, selCopyBufferToTexture)
	c.X[2], c.X[3], c.X[4], c.X[5] = uintptr(src), uintptr(offset), uintptr(rowBytes), uintptr(imageBytes)
	c.X[6], c.X[7] = uintptr(unsafe.Pointer(size)), uintptr(dst)
	c.Stack[0], c.Stack[1], c.Stack[2] = 0, 0, uintptr(unsafe.Pointer(origin))
	c.Do()
}

func copyTexture(e ID, src ID, sourceOrigin, size *[3]uintptr, dst ID, destinationOrigin *[3]uintptr) {
	c := SendCall(e, selCopyTextureToTexture)
	c.X[2], c.X[3], c.X[4], c.X[5] = uintptr(src), 0, 0, uintptr(unsafe.Pointer(sourceOrigin))
	c.X[6], c.X[7] = uintptr(unsafe.Pointer(size)), uintptr(dst)
	c.Stack[0], c.Stack[1], c.Stack[2] = 0, 0, uintptr(unsafe.Pointer(destinationOrigin))
	c.Do()
}

func copyTextureToBuffer(e ID, src ID, origin, size *[3]uintptr, dst ID, offset, rowBytes, imageBytes int) {
	c := SendCall(e, selCopyTextureToBuffer)
	c.X[2], c.X[3], c.X[4], c.X[5] = uintptr(src), 0, 0, uintptr(unsafe.Pointer(origin))
	c.X[6], c.X[7] = uintptr(unsafe.Pointer(size)), uintptr(dst)
	c.Stack[0], c.Stack[1], c.Stack[2] = uintptr(offset), uintptr(rowBytes), uintptr(imageBytes)
	c.Do()
}
