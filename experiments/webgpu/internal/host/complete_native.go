//go:build !js

package host

import (
	"encoding/json"
	"github.com/gogpu/wgpu"
	"os"
	"path/filepath"
)

func Complete(d *wgpu.Device) error { return d.WaitIdle() }
func Publish(report string)         {}
func DeviceLost() string            { return "" }

func StopRequested() bool { return false }

func MarkEncoded(_ int, _ uint64) {}

func PublishMeasurement(out, text string) error {
	if out == "" {
		return nil
	}
	var data struct {
		Summary json.RawMessage `json:"summary"`
		CSV     string          `json:"frames_csv"`
	}
	if e := json.Unmarshal([]byte(text), &data); e != nil {
		return e
	}
	if e := os.MkdirAll(out, 0755); e != nil {
		return e
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{"summary.json", data.Summary}, {"frames.csv", []byte(data.CSV)}, {"observations.json", []byte(text)}} {
		if e := os.WriteFile(filepath.Join(out, file.name), file.data, 0644); e != nil {
			return e
		}
	}
	return nil
}
