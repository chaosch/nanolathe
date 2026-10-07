//go:build !darwin

package metalproto

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

func Run(_ *meshscene.Scene, _ Options) (map[string]any, error) {
	return nil, fmt.Errorf("metalproto: macOS Metal backend required")
}
