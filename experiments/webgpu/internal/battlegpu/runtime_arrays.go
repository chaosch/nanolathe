package battlegpu

import (
	"os"
	"regexp"

	"github.com/gogpu/wgpu"
)

// Optional portable WGSL arrays require a qualified native render binding ABI.
// Browser/Rust and pristine native dependency pins keep fixed declarations.
func runtimeArraysEnabled(d *wgpu.Device) bool {
	if os.Getenv("NANOLATHE_WEBGPU_RUNTIME_ARRAYS") != "1" {
		return false
	}
	contract, ok := any(d).(interface{ RenderRuntimeArraysSafe() bool })
	return ok && contract.RenderRuntimeArraysSafe()
}

// Restrict conversion to declared storage arrays. Local literal arrays and all
// retained vertex/UV records stay exactly as authored.
var runtimeStoragePattern = regexp.MustCompile(`(var<storage,\s*read(?:_write)?>\s+\w+:\s*array<)([^;\n]+),\s*(?:[0-9]+|__[A-Z_]+__)(>;)`)

func runtimeStorageArrays(source string) string {
	return runtimeStoragePattern.ReplaceAllString(source, "${1}${2}${3}")
}

func runtimeLayerKey(key layerABIKey, enabled bool) layerABIKey {
	if enabled {
		key.capacities = [5]int{1, 1, 1, 1, 1}
	}
	return key
}
