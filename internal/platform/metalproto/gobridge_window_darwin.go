//go:build darwin

package metalproto

import (
	"fmt"
	"os"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// NSEvent types and flags used by the event pump.
const (
	evLeftMouseDown     = 1
	evLeftMouseUp       = 2
	evRightMouseDown    = 3
	evRightMouseUp      = 4
	evMouseMoved        = 5
	evLeftMouseDragged  = 6
	evRightMouseDragged = 7
	evKeyDown           = 10
	evKeyUp             = 11
	evFlagsChanged      = 12
	evScrollWheel       = 22
	evOtherMouseDown    = 25
	evOtherMouseUp      = 26
	evOtherMouseDragged = 27
	evMagnify           = 30

	flagShift   = 1 << 17
	flagControl = 1 << 18
	flagOption  = 1 << 19
	flagCommand = 1 << 20

	phaseBegan     = 1
	phaseEnded     = 8
	phaseCancelled = 16
)

// gbApp holds the AppKit objects and selectors of the visible window.
type gbApp struct {
	app, distantPast, mode, view                                               mtl.ID
	drawableW, drawableH                                                       float64
	nextEvent, sendEvent, typeSel, window, modifierFlags, keyCode, isARepeat   mtl.SEL
	characters, charactersIgnoring, locationInWindow, buttonNumber, clickCount mtl.SEL
	scrollX, scrollY, precise, momentumPhase, phase, magnification             mtl.SEL
	deltaX, deltaY, cgEvent, convertFromView, bounds, isFlipped, isKeyWindow   mtl.SEL
	isActive, makeKeyWindow, close, updateWindows, keyWindow, isMainWindow     mtl.SEL
	length, characterAtIndex                                                   mtl.SEL
	focusLog                                                                   bool
}

var gbAppState gbApp

func (r *gbRenderer) openWindow(width, height int, vsync bool) error {
	a := &gbAppState
	s := mtl.Sel
	a.app = mtl.ID(mtl.GetClass("NSApplication").Get(s("sharedApplication")))
	a.app.Send(s("setActivationPolicy:"), 0)
	// The loop pumps events instead of calling NSApplication.run. Complete
	// launch explicitly so Launch Services and accessibility can attach.
	a.app.Send(s("finishLaunching"))
	screen := mtl.ID(mtl.GetClass("NSScreen").Get(s("mainScreen")))
	scale := mtl.SendDouble(screen, s("backingScaleFactor"))
	r.window = mtl.NewWindow(0, 0, float64(width)/scale, float64(height)/scale, 1|2, 2, false)
	if r.window == 0 {
		return fmt.Errorf("window creation failed")
	}
	r.window.Send(s("setReleasedWhenClosed:"), 0)
	r.window.Send(s("setTitle:"), uintptr(mtl.String("Nanolathe retained Metal prototype")))
	r.layer = mtl.Retain(mtl.ID(mtl.GetClass("CAMetalLayer").Get(s("layer"))))
	r.layer.Send(s("setDevice:"), uintptr(r.device))
	r.layer.Send(s("setPixelFormat:"), mtl.PixelFormatBGRA8Unorm)
	size := mtl.SendCall(r.layer, s("setDrawableSize:"))
	size.SetDouble(0, float64(width))
	size.SetDouble(1, float64(height))
	size.Do()
	contents := mtl.SendCall(r.layer, s("setContentsScale:"))
	contents.SetDouble(0, scale)
	contents.Do()
	r.layer.Send(s("setFramebufferOnly:"), 1)
	r.layer.Send(s("setMaximumDrawableCount:"), 3)
	sync := uintptr(0)
	if vsync {
		sync = 1
	}
	r.layer.Send(s("setDisplaySyncEnabled:"), sync)
	a.view = mtl.ID(r.window.Get(s("contentView")))
	a.view.Send(s("setWantsLayer:"), 1)
	a.view.Send(s("setLayer:"), uintptr(r.layer))
	r.window.Send(s("setAcceptsMouseMovedEvents:"), 1)
	r.window.Send(s("center"))
	r.window.Send(s("makeKeyAndOrderFront:"), 0)
	a.app.Send(s("activateIgnoringOtherApps:"), 1)
	a.drawableW, a.drawableH = float64(width), float64(height)
	a.distantPast = mtl.Retain(mtl.ID(mtl.GetClass("NSDate").Get(s("distantPast"))))
	a.mode = mtl.DefaultRunLoopMode()
	a.nextEvent, a.sendEvent, a.typeSel, a.window = s("nextEventMatchingMask:untilDate:inMode:dequeue:"), s("sendEvent:"), s("type"), s("window")
	a.modifierFlags, a.keyCode, a.isARepeat = s("modifierFlags"), s("keyCode"), s("isARepeat")
	a.characters, a.charactersIgnoring, a.locationInWindow = s("characters"), s("charactersIgnoringModifiers"), s("locationInWindow")
	a.buttonNumber, a.clickCount, a.scrollX, a.scrollY = s("buttonNumber"), s("clickCount"), s("scrollingDeltaX"), s("scrollingDeltaY")
	a.precise, a.momentumPhase, a.phase, a.magnification = s("hasPreciseScrollingDeltas"), s("momentumPhase"), s("phase"), s("magnification")
	a.deltaX, a.deltaY, a.cgEvent, a.convertFromView = s("deltaX"), s("deltaY"), s("CGEvent"), s("convertPoint:fromView:")
	a.bounds, a.isFlipped, a.isKeyWindow, a.isActive = s("bounds"), s("isFlipped"), s("isKeyWindow"), s("isActive")
	a.makeKeyWindow, a.close, a.updateWindows, a.keyWindow, a.isMainWindow = s("makeKeyWindow"), s("close"), s("updateWindows"), s("keyWindow"), s("isMainWindow")
	a.length, a.characterAtIndex = s("length"), s("characterAtIndex:")
	a.focusLog = os.Getenv("NANOLATHE_METAL_FOCUS_LOG") != ""
	r.serviceRunLoop()
	a.app.Send(a.updateWindows)
	return nil
}

// serviceRunLoop gives AppleEvents and accessibility requests a nonblocking
// pass: dequeuing NSEvents need not service the main thread's other sources.
// Draining is bounded so presentation keeps control under a flood.
func (r *gbRenderer) serviceRunLoop() {
	r.runLoopPumps++
	for i := 0; i < 8; i++ {
		if mtl.RunLoopRunDefault() != 4 { // kCFRunLoopRunHandledSource
			break
		}
		r.runLoopSources++
	}
}

func (r *gbRenderer) pushEvent(e meshscene.NativeEvent) {
	// Only adjacent pointer motion with no wheel can collapse. Edges never merge.
	if e.Kind == 1 && e.WheelX == 0 && e.WheelY == 0 && r.eventCount > 0 {
		last := &r.eventQueue[r.eventCount-1]
		if last.Kind == 1 && last.WheelX == 0 && last.WheelY == 0 {
			*last = e
			return
		}
	}
	if r.eventCount == gbEventCapacity {
		r.eventOverflow++
		return
	}
	r.eventQueue[r.eventCount] = e
	r.eventCount++
}

func eventModifiers(flags uintptr) uint32 {
	var m uint32
	if flags&flagShift != 0 {
		m |= 1
	}
	if flags&flagControl != 0 {
		m |= 2
	}
	if flags&flagOption != 0 {
		m |= 4
	}
	if flags&flagCommand != 0 {
		m |= 8
	}
	return m
}

// quartzPoint converts a Cocoa screen point to Quartz's top-left origin.
func quartzPoint(x, y float64) (float64, float64) {
	screens := mtl.ID(mtl.GetClass("NSScreen").Get(mtl.Sel("screens")))
	primary := mtl.ID(screens.Get(mtl.Sel("firstObject")))
	frame := mtl.SendRect(primary, mtl.Sel("frame"))
	return x, frame[1] + frame[3] - y
}

func (r *gbRenderer) viewToScreen(x, y float64) (float64, float64) {
	a := &gbAppState
	c := mtl.SendCall(a.view, mtl.Sel("convertPoint:toView:"))
	c.SetDouble(0, x)
	c.SetDouble(1, y)
	c.Do()
	w := mtl.SendCall(r.window, mtl.Sel("convertPointToScreen:"))
	w.D[0], w.D[1] = c.F[0], c.F[1]
	w.Do()
	return w.Double(0), w.Double(1)
}

func (r *gbRenderer) restorePointer() bool {
	if !r.pointerCaptured {
		return true
	}
	a := &gbAppState
	r.pointerCaptured = false
	r.pointerWarpX, r.pointerWarpY = 0, 0
	sx, sy := r.viewToScreen(r.pointerRestore[0], r.pointerRestore[1])
	warp := mtl.WarpMouse(quartzPoint(sx, sy))
	// Reassociation follows the warp, matching the production Cocoa host
	// path's ordering so the restored cursor resumes ordinary movement.
	associate := mtl.AssociateMouse(true)
	if warp != 0 || associate != 0 {
		r.lastError = "Native pointer restoration failed"
	}
	bounds := mtl.SendRect(a.view, a.bounds)
	r.pointerX = float32((r.pointerRestore[0] - bounds[0]) * a.drawableW / max(bounds[2], 1))
	if a.view.Bool(a.isFlipped) {
		r.pointerY = float32((r.pointerRestore[1] - bounds[1]) * a.drawableH / max(bounds[3], 1))
	} else {
		r.pointerY = float32((bounds[1] + bounds[3] - r.pointerRestore[1]) * a.drawableH / max(bounds[3], 1))
	}
	r.pushEvent(meshscene.NativeEvent{Kind: 1, Modifiers: r.pointerModifiers, X: r.pointerX, Y: r.pointerY})
	return warp == 0 && associate == 0
}

func (r *gbRenderer) pointerCapture(captured int32) int32 {
	if captured == 0 {
		if r.restorePointer() {
			return 1
		}
		return 0
	}
	if r.offscreen || !r.isPlayable || !r.window.Bool(gbAppState.isKeyWindow) || r.pointerCaptured {
		return 1
	}
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	a := &gbAppState
	bounds := mtl.SendRect(a.view, a.bounds)
	mx, my := mtl.SendPoint(mtl.GetClass("NSEvent"), mtl.Sel("mouseLocation"))
	w := mtl.SendCall(r.window, mtl.Sel("convertPointFromScreen:"))
	w.SetDouble(0, mx)
	w.SetDouble(1, my)
	w.Do()
	v := mtl.SendCall(a.view, a.convertFromView)
	v.D[0], v.D[1] = w.F[0], w.F[1]
	v.Do()
	r.pointerRestore = [2]float64{v.Double(0), v.Double(1)}
	cx, cy := r.viewToScreen(bounds[0]+bounds[2]/2, bounds[1]+bounds[3]/2)
	if mtl.WarpMouse(quartzPoint(cx, cy)) != 0 {
		r.lastError = "Native pointer centering failed"
		return 0
	}
	if mtl.AssociateMouse(false) != 0 {
		mtl.WarpMouse(quartzPoint(mx, my))
		mtl.AssociateMouse(true)
		r.lastError = "Native relative pointer capture failed"
		return 0
	}
	r.pointerWarpX, r.pointerWarpY = cx-mx, my-cy
	r.pointerCaptured = true
	return 1
}

func (r *gbRenderer) clearFocus() {
	// Clear the native poll state and publish releases for the production held
	// state. Modifier flags are their own event, including a complete chord tap.
	if r.isPlayable {
		for key := uint32(0); key < 128; key++ {
			if r.held[key] {
				r.pushEvent(meshscene.NativeEvent{Kind: 5, Button: key})
			}
		}
	}
	r.held, r.pressed = [128]bool{}, [128]bool{}
	r.pauseToggle, r.wheel = false, 0
	if r.isPlayable {
		r.pushEvent(meshscene.NativeEvent{Kind: 6})
	}
	for button := uint32(0); button < 3; button++ {
		if r.mouseHeld&(1<<button) != 0 {
			r.pushEvent(meshscene.NativeEvent{Kind: 3, Button: button, X: r.pointerX, Y: r.pointerY})
		}
	}
	r.mouseHeld, r.pointerModifiers = 0, 0
	r.restorePointer()
}

func (r *gbRenderer) focusLog(what string) {
	a := &gbAppState
	if !a.focusLog {
		return
	}
	fmt.Fprintf(os.Stderr, "focus: %s key=%v active=%v appKey=%v main=%v wasKey=%v\n", what, r.window.Bool(a.isKeyWindow), a.app.Bool(a.isActive), mtl.ID(a.app.Get(a.keyWindow)) == r.window, r.window.Bool(a.isMainWindow), r.wasKey)
}

func (r *gbRenderer) observeFocus() {
	focused := r.window.Bool(gbAppState.isKeyWindow)
	if r.wasKey == focused {
		return
	}
	if focused {
		r.focusLog("gained")
	} else {
		r.focusLog("lost")
		r.clearFocus()
	}
	if r.isPlayable {
		key := uint32(0)
		if focused {
			key = 1
		}
		r.pushEvent(meshscene.NativeEvent{Kind: 8, Key: key})
	}
	r.wasKey = focused
}

func (r *gbRenderer) appendText(event mtl.ID, flags uint32) {
	// The production key translator owns Ctrl/Alt/Command tokens. Ordinary
	// translated characters alone go through the text stream, preserving case.
	if flags&(2|4|8) != 0 {
		return
	}
	a := &gbAppState
	chars := mtl.ID(event.Get(a.characters))
	n := int(chars.Get(a.length))
	for i := 0; i < n; i++ {
		value := uint32(chars.Send(a.characterAtIndex, uintptr(i)) & 0xffff)
		if value >= 0xd800 && value <= 0xdbff && i+1 < n {
			low := uint32(chars.Send(a.characterAtIndex, uintptr(i+1)) & 0xffff)
			if low >= 0xdc00 && low <= 0xdfff {
				value = 0x10000 + ((value - 0xd800) << 10) + (low - 0xdc00)
				i++
			}
		}
		// Editing controls and Cocoa's function-key characters already have a
		// key token. They must not acquire a companion printable command.
		if value < 32 || value == 127 || (value >= 0xf700 && value <= 0xf8ff) {
			continue
		}
		r.pushEvent(meshscene.NativeEvent{Kind: 7, Key: value, Modifiers: flags})
	}
}

func (r *gbRenderer) pump() {
	if r.offscreen {
		return
	}
	a := &gbAppState
	r.serviceRunLoop()
	r.observeFocus()
	for {
		event := mtl.ID(mtl.SendBlocking(a.app, a.nextEvent, ^uintptr(0), uintptr(a.distantPast), uintptr(a.mode), 1))
		if event == 0 {
			break
		}
		consumed := false
		ours := mtl.ID(event.Get(a.window)) == r.window
		kind := event.Get(a.typeSel)
		flags := eventModifiers(event.Get(a.modifierFlags))
		if ours {
			r.pointerModifiers = flags
		}
		if ours && kind == evFlagsChanged && r.isPlayable {
			r.pushEvent(meshscene.NativeEvent{Kind: 6, Modifiers: flags})
			consumed = true
		}
		if ours && (kind == evKeyDown || kind == evKeyUp) {
			k := uint32(event.Get(a.keyCode) & 0xffff)
			down := kind == evKeyDown
			if k < 128 {
				r.held[k] = down
				if down {
					r.pressed[k] = true
				}
				if k == 49 && down && !event.Bool(a.isARepeat) && !r.isPlayable {
					r.pauseToggle = true
				}
			}
			// Close through the ordinary window lifetime, so Run drains and
			// writes the final records instead of AppKit ending the process.
			switch {
			case k == 12 && down && flags&8 != 0:
				r.window.Send(a.close)
				consumed = true
			case r.isPlayable:
				chars := mtl.ID(event.Get(a.charactersIgnoring))
				key := uint32(0)
				if chars != 0 && chars.Get(a.length) > 0 {
					key = uint32(chars.Send(a.characterAtIndex, 0) & 0xffff)
				}
				ek := uint32(5)
				if down {
					ek = 4
				}
				r.pushEvent(meshscene.NativeEvent{Kind: ek, Key: key, Button: k, Modifiers: flags})
				if down {
					r.appendText(event, flags)
				}
				consumed = true
			default:
				consumed = k == 0 || k == 1 || k == 2 || k == 13 || k == 49 || k >= 123
			}
		}
		down := kind == evLeftMouseDown || kind == evRightMouseDown || kind == evOtherMouseDown
		up := kind == evLeftMouseUp || kind == evRightMouseUp || kind == evOtherMouseUp
		move := kind == evMouseMoved || kind == evLeftMouseDragged || kind == evRightMouseDragged || kind == evOtherMouseDragged
		scroll, pinch := kind == evScrollWheel, kind == evMagnify
		if ours && (down || up || move || scroll || pinch) {
			lx, ly := mtl.SendPoint(event, a.locationInWindow)
			v := mtl.SendCall(a.view, a.convertFromView)
			v.SetDouble(0, lx)
			v.SetDouble(1, ly)
			v.Do()
			px, py := v.Double(0), v.Double(1)
			bounds := mtl.SendRect(a.view, a.bounds)
			x := float32((px - bounds[0]) * a.drawableW / max(bounds[2], 1))
			var y float32
			if a.view.Bool(a.isFlipped) {
				y = float32((py - bounds[1]) * a.drawableH / max(bounds[3], 1))
			} else {
				y = float32((bounds[1] + bounds[3] - py) * a.drawableH / max(bounds[3], 1))
			}
			inside := px >= bounds[0] && py >= bounds[1] && px < bounds[0]+bounds[2] && py < bounds[1]+bounds[3]
			button := uint32(0)
			switch event.Get(a.buttonNumber) {
			case 1:
				button = 1
			case 2:
				button = 2
			}
			if r.pointerCaptured {
				// Relative deltas remain device-independent Cocoa points. Scale
				// into the same drawable-pixel lane as ordinary absolute
				// positions; a centre warp is host motion, never camera movement
				// [07 R-CAM-01 §11].
				if move {
					r.pointerX += float32((mtl.SendDouble(event, a.deltaX) - r.pointerWarpX) * a.drawableW / max(bounds[2], 1))
					r.pointerY += float32((mtl.SendDouble(event, a.deltaY) - r.pointerWarpY) * a.drawableH / max(bounds[3], 1))
					r.pointerWarpX, r.pointerWarpY = 0, 0
				}
				x, y = r.pointerX, r.pointerY
			}
			if inside || r.mouseHeld != 0 || r.pointerCaptured {
				r.pointerX, r.pointerY = x, y
				var scrollX, scrollY float64
				if scroll {
					scrollX, scrollY = mtl.SendDouble(event, a.scrollX), mtl.SendDouble(event, a.scrollY)
					r.wheel += float32(scrollY)
					consumed = true
				}
				if r.isPlayable {
					if down && (!a.app.Bool(a.isActive) || !r.window.Bool(a.isKeyWindow)) {
						// A click returning to the game activates it only when
						// AppKit sees the mouse-down; the view ignores the click.
						r.focusLog("down while not key")
						a.app.Send(a.sendEvent, uintptr(event))
						if !r.window.Bool(a.isKeyWindow) {
							r.window.Send(a.makeKeyWindow)
						}
						r.focusLog("after forwarding")
					}
					if down {
						r.mouseHeld |= 1 << button
					}
					if up {
						r.mouseHeld &^= 1 << button
					}
					key := uint32(0)
					if down {
						key = uint32(event.Get(a.clickCount))
					}
					if scroll {
						precise := event.Bool(a.precise)
						momentum := event.Get(a.momentumPhase) != 0
						key = 4
						if precise {
							key |= 1
						}
						if momentum {
							key |= 2
						}
						var notch int64
						if !precise && !momentum {
							if cg := event.Get(a.cgEvent); cg != 0 {
								notch = mtl.EventIntegerField(cg, 11) // kCGScrollWheelEventDeltaAxis1
							}
							if (scrollY > 0 && notch < 0) || (scrollY < 0 && notch > 0) {
								notch = -notch
							}
							if notch == 0 {
								switch {
								case scrollY > 0:
									notch = 1
								case scrollY < 0:
									notch = -1
								}
							}
						}
						button = uint32(int32(max(-1<<31, min(1<<31-1, notch))))
					}
					if pinch {
						phase := event.Get(a.phase)
						key = 0
						if phase&phaseBegan != 0 {
							key |= 1
						}
						if phase&phaseEnded != 0 {
							key |= 2
						}
						if phase&phaseCancelled != 0 {
							key |= 4
						}
					}
					ev := meshscene.NativeEvent{Key: key, Button: button, Modifiers: flags, X: x, Y: y}
					switch {
					case pinch:
						ev.Kind = 9
						ev.WheelY = float32(mtl.SendDouble(event, a.magnification))
					case down:
						ev.Kind = 2
					case up:
						ev.Kind = 3
					default:
						ev.Kind = 1
					}
					if scroll {
						ev.WheelX, ev.WheelY = float32(scrollX), float32(scrollY)
					}
					r.pushEvent(ev)
					consumed = true
				}
			}
		}
		if !consumed {
			a.app.Send(a.sendEvent, uintptr(event))
		}
		r.observeFocus()
	}
	r.serviceRunLoop()
	r.observeFocus()
	a.app.Send(a.updateWindows)
}

func (r *gbRenderer) playable(on int32) {
	r.isPlayable = on != 0
	if r.isPlayable && !r.cursorHidden && !r.offscreen {
		mtl.GetClass("NSCursor").Send(mtl.Sel("hide"))
		r.cursorHidden = true
	} else if !r.isPlayable && r.cursorHidden {
		mtl.GetClass("NSCursor").Send(mtl.Sel("unhide"))
		r.cursorHidden = false
	}
}

func (r *gbRenderer) events(out []meshscene.NativeEvent) int32 {
	n := min(len(out), r.eventCount)
	copy(out, r.eventQueue[:n])
	r.eventCount -= n
	if r.eventCount > 0 {
		copy(r.eventQueue[:], r.eventQueue[n:n+r.eventCount])
	}
	return int32(n)
}

func (r *gbRenderer) input(in *nativeInput) {
	pool := mtl.PoolPush()
	defer mtl.PoolPop(pool)
	r.pump()
	held := func(k int) bool { return r.held[k] || r.pressed[k] }
	// Retain a short tap for one sample even when down/up share an event pump.
	right := held(124) || (!r.isPlayable && held(2))
	left := held(123) || (!r.isPlayable && held(0))
	down := held(125) || (!r.isPlayable && held(1))
	up := held(126) || (!r.isPlayable && held(13))
	in.PanX = b2f(right) - b2f(left)
	in.PanZ = b2f(down) - b2f(up)
	in.Zoom = r.wheel
	in.PauseToggle = 0
	if r.pauseToggle {
		in.PauseToggle = 1
	}
	r.pressed = [128]bool{}
	r.wheel, r.pauseToggle = 0, false
}
