// Asset-free manifest construction is outside any timed interval.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"os"
)

func main() {
	out := map[string]any{}
	for _, c := range [][4]int{{1000, 200, 1920, 1080}, {3000, 200, 1920, 1080}, {5000, 200, 1920, 1080}, {3000, 500, 1920, 1080}, {3000, 200, 3456, 2160}} {
		for _, mode := range []string{"animated", "static", "camera"} {
			w, e := fixture.New(c[0], c[1], c[2], c[3], mode)
			if e != nil {
				panic(e)
			}
			out[fmt.Sprintf("%d/%d/%d/%d/%s", c[0], c[1], c[2], c[3], mode)] = map[string]any{"version": fixture.Version, "seed": fixture.Seed, "hashes": w.Hashes, "instances": w.Count, "pieces": 3 * w.Count, "triangles": w.Count * w.Detail, "detail": w.Detail, "physical_width": w.Width, "physical_height": w.Height, "mode": w.Mode, "camera": w.Scene.Camera, "columns": w.Columns, "rows": w.Rows, "cell": w.Cell, "scale": w.Scale}
		}
	}
	if e := json.NewEncoder(os.Stdout).Encode(out); e != nil {
		panic(e)
	}
}
