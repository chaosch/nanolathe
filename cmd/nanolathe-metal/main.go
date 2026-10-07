// Command nanolathe-metal runs the isolated macOS retained-mesh experiment.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nanolathe-gg/nanolathe/internal/platform/benchlock"
	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

func init() {
	// Cocoa's application and window must be created on the process main thread.
	runtime.LockOSThread()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	home, _ := os.UserHomeDir()
	var load meshscene.LoadOptions
	var render metalproto.Options
	var prepareOnly bool
	flag.StringVar(&load.Root, "root", filepath.Join(home, "TotalAnnihilation"), "retail asset installation")
	flag.StringVar(&load.Map, "map", "Expanded Confluence", "retail terrain map")
	flag.IntVar(&load.Units, "units", 1000, "submitted unit instances")
	flag.IntVar(&load.QuadMultiplier, "quads", 1, "authored-face subdivision multiplier (1, 2 or 5)")
	flag.IntVar(&load.TextureScale, "textures", 1, "shared atlas linear scale (1 or 2)")
	flag.IntVar(&load.Samples, "samples", 120, "30 Hz animation samples prepared before measurement")
	flag.IntVar(&render.Width, "width", 1920, "render target pixel width")
	flag.IntVar(&render.Height, "height", 1080, "render target pixel height")
	flag.IntVar(&render.Frames, "frames", 1200, "measured frames")
	flag.IntVar(&render.Warmup, "warmup", 240, "warmup frames")
	flag.IntVar(&render.FPS, "fps", 120, "target frame cadence; zero runs uncapped")
	flag.BoolVar(&render.Offscreen, "offscreen", false, "render without display presentation")
	flag.BoolVar(&render.VSync, "vsync", true, "synchronize visible presentation with display")
	flag.BoolVar(&render.Effects, "effects", true, "enable prototype lighting, bloom and distortion stress")
	flag.StringVar(&render.OutputDir, "out", "", "new output directory outside the repository (required)")
	flag.StringVar(&render.LibraryPath, "library", os.Getenv("NANOLATHE_METAL_LIBRARY"), "native Metal bridge dylib")
	flag.BoolVar(&prepareOnly, "prepare-only", false, "write scene census and texture previews without rendering")
	flag.Parse()
	if render.OutputDir == "" {
		return fmt.Errorf("nanolathe: Metal prototype: --out is required")
	}
	if load.Units < 1 || load.Units > 1500 || load.Samples < 2 || load.Samples > 240 || render.Frames < 1 || render.Frames > 100000 || render.Warmup < 0 || render.Width < 64 || render.Height < 64 || render.Width > 8192 || render.Height > 8192 || render.FPS < 0 || render.FPS > 1000 {
		return fmt.Errorf("nanolathe: Metal prototype: invalid workload bounds")
	}
	if load.QuadMultiplier != 1 && load.QuadMultiplier != 2 && load.QuadMultiplier != 5 {
		return fmt.Errorf("nanolathe: Metal prototype: --quads must be 1, 2 or 5")
	}
	if load.TextureScale != 1 && load.TextureScale != 2 {
		return fmt.Errorf("nanolathe: Metal prototype: --textures must be 1 or 2")
	}
	if _, err := os.Stat(render.OutputDir); !os.IsNotExist(err) {
		return fmt.Errorf("nanolathe: Metal prototype: output directory must be new: %s", render.OutputDir)
	}
	lockPath, err := benchlock.Path()
	if err != nil {
		return err
	}
	lock, err := benchlock.Acquire(lockPath, func() { fmt.Fprintln(os.Stderr, "nanolathe: Metal prototype: waiting for host benchmark lock") })
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := os.MkdirAll(render.OutputDir, 0755); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "nanolathe: Metal prototype: preparing retail meshes and animation samples")
	start := time.Now()
	scene, err := meshscene.Load(load)
	if err != nil {
		return err
	}
	// Keep framing comparable when varying render-target resolution.
	scene.Camera[2] *= min(float32(render.Width)/1600, float32(render.Height)/900)
	census := map[string]any{"name": scene.Name, "metadata": scene.Metadata, "options": load, "preparation_seconds": time.Since(start).Seconds(), "runtime": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "instances": len(scene.Instances), "frames": len(scene.Frames), "camera": scene.Camera, "terrain_rect": scene.TerrainRect}
	var vertices, indices, faces, pieces int
	for _, m := range scene.Meshes {
		vertices += len(m.Vertices)
		indices += len(m.Indices)
		faces += m.SourceFaces
		pieces += m.Pieces
	}
	census["unique_meshes"] = len(scene.Meshes)
	census["unique_vertices"] = vertices
	census["unique_triangles"] = indices / 3
	census["source_faces"] = faces
	census["unique_pieces"] = pieces
	census["mesh_bytes"] = vertices*48 + indices*4
	census["atlas_bytes"] = len(scene.Atlas.RGBA)
	census["terrain_bytes"] = len(scene.Terrain.RGBA)
	census["atlas_size"] = []int{scene.Atlas.Width, scene.Atlas.Height}
	census["terrain_size"] = []int{scene.Terrain.Width, scene.Terrain.Height}
	var drawnTriangles int
	for _, inst := range scene.Instances {
		drawnTriangles += len(scene.Meshes[inst.Mesh].Indices) / 3
	}
	census["triangles_per_frame"] = drawnTriangles
	if err = writeJSON(filepath.Join(render.OutputDir, "scene.json"), census); err != nil {
		return err
	}
	for name, texture := range map[string]meshscene.Texture{"atlas": scene.Atlas, "terrain": scene.Terrain} {
		if err = writePNG(filepath.Join(render.OutputDir, name+".png"), texture); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "nanolathe: Metal prototype: %d units, %d meshes, %d triangles/frame, atlas %.2f MiB\n", len(scene.Instances), len(scene.Meshes), drawnTriangles, float64(len(scene.Atlas.RGBA))/(1024*1024))
	if prepareOnly {
		return nil
	}
	result, err := metalproto.Run(scene, render)
	if err != nil {
		return err
	}
	result["scene"] = census
	result["options"] = render
	if err = writeJSON(filepath.Join(render.OutputDir, "summary.json"), result); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
	return nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func writePNG(path string, t meshscene.Texture) error {
	if t.Width == 0 || t.Height == 0 {
		return nil
	}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	e = png.Encode(f, &image.RGBA{Pix: t.RGBA, Stride: t.Width * 4, Rect: image.Rect(0, 0, t.Width, t.Height)})
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
