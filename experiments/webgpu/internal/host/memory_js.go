//go:build js && wasm

package host

func ProcessMemory() map[string]any {
	return map[string]any{"rss_bytes": nil, "reason": "browser does not expose dedicated process RSS; Go heap is reported separately"}
}
