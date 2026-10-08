//go:build darwin && arm64

// Package mtl calls the Objective-C runtime, Metal, AppKit and Core Graphics
// from Go without cgo or another compiler. Integer-only calls go through the
// runtime's own libc trampoline with no allocation; calls that pass or return
// floating-point values use a small AAPCS64 trampoline under runtime.cgocall.
// Every call made here must come from the goroutine that owns the renderer.
package mtl

import (
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

//go:linkname rawSyscall9 syscall.rawSyscall9
func rawSyscall9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname syscall9 syscall.syscall9
func syscall9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (r1, r2 uintptr, err syscall.Errno)

// runtime.cgocall is exported to purego by the runtime; purego's fakecgo
// makes it usable without cgo. A Call lives on the caller's stack: no Go
// callback runs during these calls, so the stack cannot move under it.
//
//go:linkname runtime_cgocall runtime.cgocall
//go:noescape
func runtime_cgocall(fn uintptr, arg unsafe.Pointer) int32

var floatTrampolineABI0 uintptr

// Call is one C call's register file under AAPCS64: integer and pointer
// arguments in X, floating-point arguments (as their bit patterns, a float32
// in the low half) in D, further integer arguments in Stack, and X8 for an
// indirect result. R and F return x0, x1 and d0..d3.
type Call struct {
	Fn    uintptr
	X     [8]uintptr
	D     [8]uint64
	Stack [4]uintptr
	X8    uintptr
	R     [2]uintptr
	F     [4]uint64
}

// Do runs c. The scheduler sees it as a system call, so it may block.
func (c *Call) Do() {
	if CountCalls && c.Fn == msgSend {
		CallCounts[SEL(c.X[1])]++
	}
	runtime_cgocall(floatTrampolineABI0, unsafe.Pointer(c))
}

// CPtr converts a C address returned by a call into a pointer. C memory is
// never moved or collected by Go.
func CPtr(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

var (
	libobjc, foundation, appkit, metal, quartz, coregraphics, corefoundation uintptr

	msgSend, getClass, registerName, poolPush, poolPop           uintptr
	createSystemDefaultDevice, mediaTime                         uintptr
	cgWarp, cgAssociate, cgEventField, cfRunLoopRunInMode        uintptr
	defaultRunLoopMode, cfDefaultMode, commonCounterSetTimestamp uintptr
	loadErr                                                      error
)

func open(path string) uintptr {
	if loadErr != nil {
		return 0
	}
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		loadErr = err
	}
	return h
}
func symbol(lib uintptr, name string) uintptr {
	if loadErr != nil {
		return 0
	}
	s, err := purego.Dlsym(lib, name)
	if err != nil {
		loadErr = err
	}
	return s
}

// Load opens the frameworks and resolves the C entry points once.
func Load() error {
	if msgSend != 0 || loadErr != nil {
		return loadErr
	}
	libobjc = open("/usr/lib/libobjc.A.dylib")
	foundation = open("/System/Library/Frameworks/Foundation.framework/Foundation")
	appkit = open("/System/Library/Frameworks/AppKit.framework/AppKit")
	metal = open("/System/Library/Frameworks/Metal.framework/Metal")
	quartz = open("/System/Library/Frameworks/QuartzCore.framework/QuartzCore")
	coregraphics = open("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
	corefoundation = open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	msgSend = symbol(libobjc, "objc_msgSend")
	getClass = symbol(libobjc, "objc_getClass")
	registerName = symbol(libobjc, "sel_registerName")
	poolPush = symbol(libobjc, "objc_autoreleasePoolPush")
	poolPop = symbol(libobjc, "objc_autoreleasePoolPop")
	createSystemDefaultDevice = symbol(metal, "MTLCreateSystemDefaultDevice")
	mediaTime = symbol(quartz, "CACurrentMediaTime")
	cgWarp = symbol(coregraphics, "CGWarpMouseCursorPosition")
	cgAssociate = symbol(coregraphics, "CGAssociateMouseAndMouseCursorPosition")
	cgEventField = symbol(coregraphics, "CGEventGetIntegerValueField")
	cfRunLoopRunInMode = symbol(corefoundation, "CFRunLoopRunInMode")
	defaultRunLoopMode = symbol(appkit, "NSDefaultRunLoopMode")
	cfDefaultMode = symbol(corefoundation, "kCFRunLoopDefaultMode")
	commonCounterSetTimestamp = symbol(metal, "MTLCommonCounterSetTimestamp")
	if loadErr != nil {
		msgSend = 0
		return loadErr
	}
	initSelectors()
	return nil
}

// ID is an Objective-C object; SEL a selector; Class is an ID.
type ID uintptr
type SEL uintptr

// Send calls objc_msgSend with integer and pointer arguments only. It does
// not tell the scheduler: use it for calls that return promptly.
func Send(id ID, sel SEL, args ...uintptr) uintptr {
	count(sel)
	var a [7]uintptr
	copy(a[:], args)
	r, _, _ := rawSyscall9(msgSend, uintptr(id), uintptr(sel), a[0], a[1], a[2], a[3], a[4], a[5], a[6])
	return r
}

// SendBlocking is Send for calls that may wait (drawables, completion,
// shader compilation, the event queue).
func SendBlocking(id ID, sel SEL, args ...uintptr) uintptr {
	var a [7]uintptr
	copy(a[:], args)
	r, _, _ := syscall9(msgSend, uintptr(id), uintptr(sel), a[0], a[1], a[2], a[3], a[4], a[5], a[6])
	return r
}

// Fixed-arity forms keep hot paths free of slice copies.
// CallCounts counts sends per selector when CountCalls is set (diagnostic).
var CountCalls bool
var CallCounts = map[SEL]int{}

func count(sel SEL) {
	if CountCalls {
		CallCounts[sel]++
	}
}

func send0(id ID, sel SEL) uintptr {
	count(sel)
	r, _, _ := rawSyscall9(msgSend, uintptr(id), uintptr(sel), 0, 0, 0, 0, 0, 0, 0)
	return r
}
func send1(id ID, sel SEL, a uintptr) uintptr {
	count(sel)
	r, _, _ := rawSyscall9(msgSend, uintptr(id), uintptr(sel), a, 0, 0, 0, 0, 0, 0)
	return r
}
func send2(id ID, sel SEL, a, b uintptr) uintptr {
	count(sel)
	r, _, _ := rawSyscall9(msgSend, uintptr(id), uintptr(sel), a, b, 0, 0, 0, 0, 0)
	return r
}
func send3(id ID, sel SEL, a, b, c uintptr) uintptr {
	count(sel)
	r, _, _ := rawSyscall9(msgSend, uintptr(id), uintptr(sel), a, b, c, 0, 0, 0, 0)
	return r
}

// SendCall prepares a Call for objc_msgSend with self and sel in x0 and x1.
func SendCall(id ID, sel SEL) Call {
	return Call{Fn: msgSend, X: [8]uintptr{uintptr(id), uintptr(sel)}}
}

// Double returns d0 of a completed call as a float64.
func (c *Call) Double(i int) float64 { return *(*float64)(unsafe.Pointer(&c.F[i])) }

// SetDouble stores a float64 argument in d register i.
func (c *Call) SetDouble(i int, v float64) { c.D[i] = *(*uint64)(unsafe.Pointer(&v)) }

// SendDouble calls a method returning a double (or CGFloat).
func SendDouble(id ID, sel SEL) float64 {
	c := SendCall(id, sel)
	c.Do()
	return c.Double(0)
}

// SendPoint and SendRect call methods returning NSPoint/CGSize and NSRect,
// which AAPCS64 returns in d0..d1 and d0..d3.
func SendPoint(id ID, sel SEL, args ...uintptr) (float64, float64) {
	c := SendCall(id, sel)
	copy(c.X[2:], args)
	c.Do()
	return c.Double(0), c.Double(1)
}
func SendRect(id ID, sel SEL) [4]float64 {
	c := SendCall(id, sel)
	c.Do()
	return [4]float64{c.Double(0), c.Double(1), c.Double(2), c.Double(3)}
}

// GetClass looks up a class by name.
func GetClass(name string) ID {
	b := append([]byte(name), 0)
	r, _, _ := rawSyscall9(getClass, uintptr(unsafe.Pointer(&b[0])), 0, 0, 0, 0, 0, 0, 0, 0)
	keep(b)
	return ID(r)
}

var selNames = map[SEL]string{}

// Sel registers a selector by name.
func Sel(name string) SEL {
	b := append([]byte(name), 0)
	r, _, _ := rawSyscall9(registerName, uintptr(unsafe.Pointer(&b[0])), 0, 0, 0, 0, 0, 0, 0, 0)
	keep(b)
	selNames[SEL(r)] = name
	return SEL(r)
}

//go:noinline
func keep(any) {}

// PoolPush and PoolPop bracket autoreleased objects.
func PoolPush() uintptr {
	r, _, _ := rawSyscall9(poolPush, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	return r
}
func PoolPop(p uintptr) { rawSyscall9(poolPop, p, 0, 0, 0, 0, 0, 0, 0, 0) }

// MediaTime is CACurrentMediaTime, the host clock drawables report in.
func MediaTime() float64 {
	c := Call{Fn: mediaTime}
	c.Do()
	return c.Double(0)
}

// CreateSystemDefaultDevice returns the default Metal device (+1 retained).
func CreateSystemDefaultDevice() ID {
	r, _, _ := syscall9(createSystemDefaultDevice, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	return ID(r)
}

// WarpMouse is CGWarpMouseCursorPosition; Associate is
// CGAssociateMouseAndMouseCursorPosition. Both return a CGError.
func WarpMouse(x, y float64) int32 {
	c := Call{Fn: cgWarp}
	c.SetDouble(0, x)
	c.SetDouble(1, y)
	c.Do()
	return int32(c.R[0])
}
func AssociateMouse(on bool) int32 {
	r, _, _ := rawSyscall9(cgAssociate, boolArg(on), 0, 0, 0, 0, 0, 0, 0, 0)
	return int32(r)
}

// EventIntegerField is CGEventGetIntegerValueField.
func EventIntegerField(event uintptr, field uint32) int64 {
	r, _, _ := rawSyscall9(cgEventField, event, uintptr(field), 0, 0, 0, 0, 0, 0, 0)
	return int64(r)
}

// RunLoopRunDefault is CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0, true).
func RunLoopRunDefault() int32 {
	c := Call{Fn: cfRunLoopRunInMode, X: [8]uintptr{*(*uintptr)(CPtr(cfDefaultMode)), 1}}
	c.Do()
	return int32(c.R[0])
}

// DefaultRunLoopMode is AppKit's NSDefaultRunLoopMode string.
func DefaultRunLoopMode() ID { return ID(*(*uintptr)(CPtr(defaultRunLoopMode))) }

// CommonCounterSetTimestamp is Metal's timestamp counter-set name.
func CommonCounterSetTimestamp() ID { return ID(*(*uintptr)(CPtr(commonCounterSetTimestamp))) }

func boolArg(v bool) uintptr {
	if v {
		return 1
	}
	return 0
}
