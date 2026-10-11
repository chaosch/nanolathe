// The public Metal runner consumes an experiment-owned LiveSource; no battle adapter is imported.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
	"github.com/nanolathe-gg/nanolathe/internal/platform/metalrender"
	"os"
	"runtime"
)

type source struct {
	w    *fixture.Workload
	draw int
	s    fixture.Snapshot
}

func (s *source) Next(_ float64, _ meshscene.Input) (meshscene.LiveFrame, error) {
	d := s.draw
	s.draw++
	tick := uint64(d / 4)
	if s.s.Generation == 0 || s.w.Mode == "animated" && s.s.Generation != tick+1 {
		s.s = s.w.Sample(tick)
	}
	camera := s.w.Scene.Camera
	if s.w.Mode == "camera" {
		camera[0] += .015 * float32(d%240) / 240
	}
	return meshscene.LiveFrame{Generation: s.s.Generation, Previous: s.s.Previous, Current: s.s.Current, Instances: s.s.Instances, Camera: camera, Alpha: float32(d%4) / 4, Tick: uint32(tick)}, nil
}
func (s *source) Close() map[string]any {
	return map[string]any{"synthetic_draws": s.draw, "synthetic_clock": "draw/4 endpoints; draw%4/4 alpha"}
}
func main() {
	runtime.LockOSThread()
	runtime.GOMAXPROCS(2)
	count := flag.Int("count", 64, "instances")
	detail := flag.Int("detail", 200, "triangles per instance")
	width := flag.Int("width", 1920, "physical pixels")
	height := flag.Int("height", 1080, "physical pixels")
	frames := flag.Int("frames", 60, "measured frames")
	warmup := flag.Int("warmup", 0, "warmup draws")
	mode := flag.String("mode", "animated", "fixture mode")
	out := flag.String("out", "", "outside-repo output")
	flag.Parse()
	w, e := fixture.New(*count, *detail, *width, *height, *mode)
	if e != nil {
		panic(e)
	}
	// Tiny authored unused terrain/sprite inputs satisfy Run's public contract.
	w.Scene.Terrain = meshscene.Texture{Width: 1, Height: 1, RGBA: []byte{5, 8, 10, 255}}
	w.Scene.SpriteAtlas = meshscene.Texture{Width: 1, Height: 1, RGBA: []byte{0, 0, 0, 0}}
	w.Scene.TerrainRect = [4]float32{0, 0, 16, float32(*height) * 16 / float32(*width)}
	w.Scene.Materials = []meshscene.Material{{}, {FrameCount: 1}, {FrameCount: 1}, {FrameCount: 1}}
	w.Scene.TextureFrames = [][4]float32{{0, 0, 1, 1}}
	w.Scene.Live = &source{w: w}
	// Metal's model sampler expects palette-index RGBA, not the original true-color bytes.
	// This lossless experiment adapter preserves nearest sampled colors and UVs.
	original := append([]byte(nil), w.Scene.Atlas.RGBA...)
	palette := make([]byte, 256*2*4)
	colorIndex := map[[4]byte]byte{}
	for i := 0; i < len(original); i += 4 {
		c := [4]byte(original[i : i+4])
		index, ok := colorIndex[c]
		if !ok {
			index = byte(len(colorIndex) + 1)
			colorIndex[c] = index
			for row := 0; row < 2; row++ {
				copy(palette[(row*256+int(index))*4:], c[:])
			}
		}
		copy(w.Scene.Atlas.RGBA[i:i+4], []byte{index, 0, 0, c[3]})
	}
	w.Scene.ModelPalette = meshscene.Texture{Width: 256, Height: 2, RGBA: palette}
	w.Scene.Metadata = map[string]any{"hashes": w.Hashes, "metal_encoded_atlas_sha256": fmt.Sprintf("%x", sha256.Sum256(w.Scene.Atlas.RGBA)), "metal_palette_sha256": fmt.Sprintf("%x", sha256.Sum256(palette)), "version": fixture.Version, "seed": fixture.Seed, "mode": *mode, "comparison": "matching geometry/projection/clock; Metal directional shade, quantized height depth, ground/composite passes differ from nearest WGSL MRT-ID/overlay"}
	r, e := metalrender.Run(&w.Scene, metalrender.Options{Width: *width, Height: *height, Frames: *frames, Warmup: *warmup, FPS: 120, VSync: true, OutputDir: *out})
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	b, _ := json.Marshal(r)
	fmt.Println(string(b))
}
