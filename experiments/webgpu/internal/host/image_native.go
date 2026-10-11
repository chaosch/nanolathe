//go:build !js

package host

import (
	"os"
	"path/filepath"
)

func SaveCapture(directory, name string, png []byte) error {
	if directory == "" {
		return nil
	}
	if e := os.MkdirAll(directory, 0755); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(directory, name), png, 0644)
}
