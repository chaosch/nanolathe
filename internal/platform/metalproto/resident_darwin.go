//go:build darwin

package metalproto

import (
	"bytes"
	"unsafe"
)

// residentTracker numbers the contents of one packed array for the native
// resident copies: unchanged contents keep the last generation, so native
// copies nothing. Comparing costs far less than the copy it saves.
type residentTracker struct {
	last       []byte
	generation uint64
}

func (t *residentTracker) observe(b []byte) uint64 {
	if t.generation != 0 && bytes.Equal(t.last, b) {
		return t.generation
	}
	t.last = append(t.last[:0], b...)
	t.generation++
	return t.generation
}

// residentBytes views a packed slice's elements as bytes.
func residentBytes[T any](s []T) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
}
