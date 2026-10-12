package battlegpu

import (
	"bytes"
	"os"
	"runtime/pprof"
)

// outputWriter writes the run's diagnostic files. Native hosts write at once.
// In the browser a frame callback holds the page's event loop, on which the
// filesystem adapter completes every file operation, so files produced inside
// a callback wait in memory until Run has returned from the host loop.
type outputWriter struct {
	deferred bool
	pending  []pendingOutput
}

type pendingOutput struct {
	path string
	data []byte
}

func (o *outputWriter) write(path string, data []byte) error {
	if !o.deferred {
		return os.WriteFile(path, data, 0644)
	}
	o.pending = append(o.pending, pendingOutput{path, data})
	return nil
}

// flush writes deferred files outside any frame callback, keeping the first error.
func (o *outputWriter) flush() error {
	var first error
	for _, p := range o.pending {
		if err := os.WriteFile(p.path, p.data, 0644); err != nil && first == nil {
			first = err
		}
	}
	o.pending = nil
	return first
}

func battleProfile(name string) ([]byte, error) {
	var b bytes.Buffer
	err := pprof.Lookup(name).WriteTo(&b, 0)
	return b.Bytes(), err
}
