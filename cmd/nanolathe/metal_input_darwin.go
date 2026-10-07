//go:build darwin

package main

import (
	"math"
	"strings"

	"github.com/ebitengine/purego/objc"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/input"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// metalInput is the native producer for the existing production input state.
// It owns physical holds; the client owns the semantic queues and publishes
// exactly one pointer record per host service [07 §2][01 R-PLAT-01 §6].
type metalInput struct {
	cl        *client.Client
	scale     float32
	held      [input.KeyCount]bool
	physical  [128]input.Key
	modifiers uint32
	buttons   input.MouseButtons
}

func newMetalInput(cl *client.Client, deviceScale float32) *metalInput {
	if deviceScale <= 0 || math.IsNaN(float64(deviceScale)) || math.IsInf(float64(deviceScale), 0) {
		deviceScale = 1
	}
	m := &metalInput{cl: cl, scale: deviceScale}
	if cl != nil {
		in := cl.Input()
		for key := input.Key(1); key < input.KeyCount; key++ {
			m.held[key] = in.Kbd.KeyHeld(key)
		}
		m.buttons = input.MouseButtons{Left: in.Mouse.Held(input.MouseButtonLeft), Middle: in.Mouse.Held(input.MouseButtonMiddle), Right: in.Mouse.Held(input.MouseButtonRight)}
		if m.held[input.KeyShift] {
			m.modifiers |= 1
		}
		if m.held[input.KeyCtrl] {
			m.modifiers |= 2
		}
		if m.held[input.KeyAlt] {
			m.modifiers |= 4
		}
	}
	return m
}

// Apply receives physical drawable coordinates and an already-scaled host
// timestamp, as ebitenapp.applyInput does. Empty batches release only a prior
// service's short-press retention, never a physical hold. Ordered native key
// repeats already exist; no second repeat generator is run [07 §2].
func (m *metalInput) Apply(events []meshscene.NativeEvent, now uint32) {
	if m == nil || m.cl == nil {
		return
	}
	in := m.cl.Input()
	mouse, kbd := in.Mouse, in.Kbd
	mouse.ResetEdges()
	kbd.ResetEdges()
	in.ShortcutTokenMode = true
	in.ShortcutToken = input.Token{}
	in.DroppedPaths = nil
	var pressed [input.KeyCount]bool
	var middlePressed bool
	var wheelX, wheelY, zoomY float32
	var panX, panY float64
	for _, e := range events {
		if e.Kind == 8 {
			m.cl.SetFocused(e.Key != 0)
			if e.Key == 0 {
				m.held = [input.KeyCount]bool{}
				m.physical = [128]input.Key{}
				pressed = [input.KeyCount]bool{}
				m.modifiers = 0
				m.buttons = input.MouseButtons{}
				middlePressed = false
				mouse.SetButton(input.MouseButtonLeft, false)
				mouse.SetButton(input.MouseButtonMiddle, false)
				mouse.SetButton(input.MouseButtonRight, false)
			}
			continue
		}
		m.modifiers = e.Modifiers
		for _, modifier := range [...]struct {
			key input.Key
			bit uint32
		}{{input.KeyShift, 1}, {input.KeyCtrl, 2}, {input.KeyAlt, 4}} {
			down := e.Modifiers&modifier.bit != 0
			pressed[modifier.key] = pressed[modifier.key] || down && !m.held[modifier.key]
			m.held[modifier.key] = down
		}
		mods := input.Modifiers{Shift: e.Modifiers&1 != 0, Ctrl: e.Modifiers&2 != 0, Alt: e.Modifiers&4 != 0}
		switch e.Kind {
		case 1, 2, 3:
			x, y := int32(e.X/m.scale), int32(e.Y/m.scale)
			pointer := input.PointerEvent{X: x, Y: y, Modifiers: mods, Timestamp: now}
			if e.Kind == 2 || e.Kind == 3 {
				down := e.Kind == 2
				switch e.Button {
				case 0:
					m.buttons.Left = down
					if down {
						pointer.Kind = input.LeftDown
						if e.Key >= 2 && e.Key%2 == 0 {
							pointer.Kind = input.LeftDoubleClick
						}
					} else {
						pointer.Kind = input.LeftUp
					}
				case 1:
					m.buttons.Right = down
					if down {
						pointer.Kind = input.RightDown
						if e.Key >= 2 && e.Key%2 == 0 {
							pointer.Kind = input.RightDoubleClick
						}
					} else {
						pointer.Kind = input.RightUp
					}
				case 2:
					middlePressed = middlePressed || down && !m.buttons.Middle
					m.buttons.Middle = down
				}
			}
			pointer.Buttons = m.buttons
			in.UpdatePointerMotion(pointer)
			if pointer.Kind != input.PointerEventNone {
				in.EnqueuePointer(pointer)
			}
			if e.Kind == 1 && e.Key&4 != 0 {
				// Native scroll flags retain the production distinction between precise
				// pan, ordinary wheel zoom and momentum-only input (§16.6).
				precise, momentum := e.Key&1 != 0, e.Key&2 != 0
				if precise && !momentum {
					panX += float64(e.WheelX)
					panY += float64(e.WheelY)
				} else if !precise && !momentum {
					zoomY += float32(int32(e.Button))
				}
				if precise {
					wheelX += e.WheelX * 0.1
					wheelY += e.WheelY * 0.1
				} else {
					wheelX += e.WheelX
					wheelY += e.WheelY
				}
			}
		case 9:
			in.UpdatePointerMotion(input.PointerEvent{X: int32(e.X / m.scale), Y: int32(e.Y / m.scale), Modifiers: mods, Timestamp: now, Buttons: m.buttons})
			mouse.Pinches = append(mouse.Pinches, input.PinchEvent{Delta: float64(e.WheelY), Began: e.Key&1 != 0, Ended: e.Key&2 != 0, Cancelled: e.Key&4 != 0})
		case 4, 5:
			key := metalEventKey(e)
			if e.Kind == 5 && e.Button < uint32(len(m.physical)) && m.physical[e.Button] != input.KeyNone {
				key = m.physical[e.Button]
			}
			if key == input.KeyNone {
				continue
			}
			down := e.Kind == 4
			wasHeld := m.held[key]
			command := e.Modifiers&8 != 0
			// Cmd+V is the production host alias for a Ctrl+V token. Other Command
			// chords do not provide gameplay physical edges or keyboard tokens.
			m.held[key] = down && (!command || key == input.KeyV)
			if e.Button < uint32(len(m.physical)) {
				m.physical[e.Button] = input.KeyNone
				if m.held[key] {
					m.physical[e.Button] = key
				}
				m.held[key] = false
				for _, heldKey := range m.physical {
					m.held[key] = m.held[key] || heldKey == key
				}
			}
			if m.held[key] && !wasHeld {
				pressed[key] = true
			}
			if down {
				token, ok := metalKeyToken(key, mods, command)
				if ok {
					if token.Kind == input.TokenEdit && (token.Key == input.KeyInsert || token.Key == input.KeyV && token.Ctrl) {
						token.Clipboard = metalClipboard()
					}
					in.EnqueueToken(token)
				}
			}
		case 7:
			if e.Modifiers&(2|4|8) == 0 {
				in.EnqueueToken(input.Token{Kind: input.TokenText, Rune: rune(e.Key)})
			}
		}
	}
	// Retain a physical key tap for this host service even if its native up was
	// in the same batch, matching production refresh-to-host press retention.
	for key := input.Key(1); key < input.KeyCount; key++ {
		down := m.held[key] || pressed[key]
		if pressed[key] && kbd.KeyHeld(key) {
			kbd.SetKey(key, false)
		}
		kbd.SetKey(key, down)
	}
	mouse.SetButton(input.MouseButtonMiddle, m.buttons.Middle || middlePressed)
	mouse.SetWheel(wheelX, wheelY)
	mouse.ZoomScrollY = zoomY
	mouse.PanX, mouse.PanY = panX, panY
	in.PublishPointer()
}

// Virtual keycodes are Cocoa host API identities, not executable evidence.
// Printable bindings are physical keys; special Unicode function-key values
// cover keyboards that provide Insert/Pause without a standard Mac keycode.
func metalEventKey(e meshscene.NativeEvent) input.Key {
	switch e.Key {
	case 0xf727:
		return input.KeyInsert
	case 0xf730:
		return input.KeyPause
	}
	if e.Button < uint32(len(metalMacKeys)) {
		return metalMacKeys[e.Button]
	}
	return input.KeyNone
}

var metalMacKeys = [128]input.Key{
	0: input.KeyA, 1: input.KeyS, 2: input.KeyD, 3: input.KeyF, 4: input.KeyH, 5: input.KeyG, 6: input.KeyZ, 7: input.KeyX, 8: input.KeyC, 9: input.KeyV,
	11: input.KeyB, 12: input.KeyQ, 13: input.KeyW, 14: input.KeyE, 15: input.KeyR, 16: input.KeyY, 17: input.KeyT,
	18: input.Key1, 19: input.Key2, 20: input.Key3, 21: input.Key4, 22: input.Key6, 23: input.Key5, 24: input.KeyEqual, 25: input.Key9, 26: input.Key7, 27: input.KeyMinus, 28: input.Key8, 29: input.Key0,
	31: input.KeyO, 32: input.KeyU, 34: input.KeyI, 35: input.KeyP, 36: input.KeyEnter, 37: input.KeyL, 38: input.KeyJ, 40: input.KeyK, 43: input.KeyComma, 45: input.KeyN, 46: input.KeyM, 47: input.KeyPeriod, 48: input.KeyTab, 49: input.KeySpace, 50: input.KeyBackquote, 51: input.KeyBackspace, 53: input.KeyEscape,
	69: input.KeyNumpadAdd, 76: input.KeyEnter, 78: input.KeyNumpadSubtract,
	82: input.Key0, 83: input.Key1, 84: input.Key2, 85: input.Key3, 86: input.Key4, 87: input.Key5, 88: input.Key6, 89: input.Key7, 91: input.Key8, 92: input.Key9,
	96: input.KeyF5, 97: input.KeyF6, 98: input.KeyF7, 99: input.KeyF3, 100: input.KeyF8, 101: input.KeyF9, 103: input.KeyF11, 109: input.KeyF10, 111: input.KeyF12,
	114: input.KeyInsert, 115: input.KeyHome, 116: input.KeyPrior, 117: input.KeyDelete, 118: input.KeyF4, 119: input.KeyEnd, 120: input.KeyF2, 121: input.KeyNext, 122: input.KeyF1, 123: input.KeyLeft, 124: input.KeyRight, 125: input.KeyDown, 126: input.KeyUp,
}

// This is the same token split as ebitenapp.translatedKeyToken: printable
// text comes from native character events; editing/Ctrl/Alt keys translate
// once from the key event [07 §2][07 R-CAM-01 §14].
func metalKeyToken(key input.Key, mods input.Modifiers, command bool) (input.Token, bool) {
	if command {
		if key == input.KeyV && !mods.Alt {
			return input.Token{Kind: input.TokenEdit, Key: input.KeyV, Ctrl: true}, true
		}
		return input.Token{}, false
	}
	composable := key >= input.KeyA && key <= input.KeyZ || key >= input.Key0 && key <= input.Key9 || key >= input.KeyF1 && key <= input.KeyF12
	if mods.Ctrl && composable {
		return input.Token{Kind: input.TokenEdit, Key: key, Ctrl: true}, true
	}
	switch key {
	case input.KeyBackspace, input.KeyDelete, input.KeyInsert, input.KeyHome, input.KeyEnd, input.KeyPrior, input.KeyNext, input.KeyPause, input.KeyLeft, input.KeyRight, input.KeyUp, input.KeyDown, input.KeyTab, input.KeyEnter, input.KeyEscape:
		return input.Token{Kind: input.TokenEdit, Key: key}, true
	}
	if key >= input.KeyF1 && key <= input.KeyF12 {
		return input.Token{Kind: input.TokenEdit, Key: key}, true
	}
	if mods.Alt || mods.Ctrl {
		var r rune
		switch {
		case key >= input.KeyA && key <= input.KeyZ:
			r = 'a' + rune(key-input.KeyA)
		case key >= input.Key0 && key <= input.Key9:
			r = '0' + rune(key-input.Key0)
		default:
			switch key {
			case input.KeyMinus:
				r = '-'
			case input.KeyEqual:
				r = '='
			case input.KeyBackquote:
				r = '`'
			case input.KeyComma:
				r = ','
			case input.KeyPeriod:
				r = '.'
			}
		}
		if r != 0 {
			return input.Token{Kind: input.TokenText, Rune: r}, true
		}
	}
	return input.Token{}, false
}

// Clipboard admission is production's ASCII-only policy, including its
// successful-empty distinction and first-NUL boundary [07 §2].
func metalClipboard() input.ClipboardText {
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	board := objc.ID(objc.GetClass("NSPasteboard")).Send(objc.RegisterName("generalPasteboard"))
	kind := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "public.utf8-plain-text")
	value := board.Send(objc.RegisterName("stringForType:"), kind)
	if value == 0 {
		return input.ClipboardText{}
	}
	text := objc.Send[string](value, objc.RegisterName("UTF8String"))
	if end := strings.IndexByte(text, 0); end >= 0 {
		text = text[:end]
	}
	for i := range len(text) {
		if text[i] > 0x7f {
			// TODO(T25): Unicode-to-retail-codepage conversion remains unestablished;
			// retain the editor rather than introducing replacement bytes [07 §2].
			return input.ClipboardText{}
		}
	}
	return input.ClipboardText{Text: text, Available: true}
}
