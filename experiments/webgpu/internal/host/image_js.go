//go:build js && wasm

package host

import (
	"encoding/base64"
	"syscall/js"
)

func SaveCapture(_, name string, png []byte) error {
	js.Global().Get("probeBridge").Call("capture", name, base64.StdEncoding.EncodeToString(png))
	return nil
}
