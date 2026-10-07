# Retained Metal renderer experiment

This is the user-requested throwaway macOS prototype of 2026-10-06, isolated on
`metal-prototype`. It does not select a new production renderer or change any
simulation behavior. The user explicitly requested no tests; verification is
builds, real-device runs, captures and measured frame records. It has not passed
the production landing gates and is intentionally not merged into `main`.

## Question and implementation boundary

Can retained meshes, GPU rigid-piece pose interpolation, ordinary depth testing
and shared UV textures remove the CPU projection/repacking cost that grows with
model complexity? Can the resulting render path fit a 120 Hz (8.33 ms) or
144 Hz (6.94 ms) budget on the development Mac?

The experimental command is `cmd/nanolathe-metal`. The existing VFS, content,
model, palette, animation and session code prepares the scene. The narrow scene
boundary is `internal/prototype/meshscene`; the renderer is
`internal/platform/metalproto`. No module dependencies are added.

`purego` loads a small native Metal bridge. The prototype uses Cocoa for its own
window, Metal buffers/textures/pipelines for drawing, and Metal shader source
for the vertex and fragment stages. This keeps native shader and resource
control explicit without changing Ebitengine or the shipping game's draw path.
The native library is compiled separately by the Mac's Xcode command-line tools;
the Go executable does not require cgo for this bridge.

The current renderer's relevant costs and visual contracts are described in
[DESIGN_GPU_RENDERER](DESIGN_GPU_RENDERER.md), especially §13.5, §22 and §35.
This experiment deliberately uses modern depth geometry and sampled floating
point transforms; it is not a reproduction of retail's byte height-key raster,
per-subject images, or its palette composition arithmetic.

## Measurement discipline

The replay workload uses actual retail model and texture content, and its scene
metadata says exactly how the poses were obtained. Replicated pose playback is
not a live battle: it does not include ongoing simulation, dynamic visibility,
recording all effect families, GUI, audio or asset streaming. The existing live
Classic and Modern benchmark runs provide context, not a controlled backend
speedup comparison. Their scene and feature census differ from the replay.

`--quads=2` and `--quads=5` subdivide authored surfaces and increase vertex/index
work. They do not create new artistic detail, more complicated silhouettes,
more material variety, or more animation bones. `--textures=2` doubles atlas
texture dimensions with nearest-neighbor scaling; it measures four times the
texel storage, not an actual remastered art pack.

Warmup, scene preparation, texture uploads, screenshot readback and PNG writing
are excluded from steady frame timing. GPU duration means completed Metal
command-buffer start/end timestamps. CPU submission, wall frame intervals and
display presentation are reported separately. An offscreen throughput number
is never presented as observed display refresh.

The reference Mac is an Apple M3 Pro (12 CPU cores, 18 GPU cores, 18 GB unified
memory), macOS 26.6.2. Its active built-in panel reports 120 Hz, so this machine
can establish visible 120 Hz pacing and offscreen 144 Hz headroom; it cannot
establish visible 144 Hz delivery. Native target dimensions are explicit pixels.

Raw artifacts are outside the repository under
`/tmp/nanolathe-metal-prototype/`. Retail art is not committed.

## Current game baseline

Both existing renderers ran the standard scene version 5 coastal battle at
1920×1080, seed 7, 300 lead-in ticks, 240 warmup draws, 600 measured draws,
and a requested 120 draws/s with `GOMAXPROCS=2`. Their final censuses match:
318 living units, 278 in-view anchors, 205 moving units, 810 effects (690
in-view), 116 projectiles (101 in-view), 8 nanoframes, and 4 burning features.
A concurrent verification gate was reported during the Classic run; the
numbers are development-host observations, not noise-free laboratory results.

| Current renderer | Average host cadence | Median interval | p95 interval | Median draw work |
|---|---:|---:|---:|---:|
| Classic | 46.9 draws/s | 19.325 ms | 30.716 ms | 17.584 ms |
| Modern | 92.2 draws/s | 9.115 ms | 16.624 ms | 6.237 ms |

Modern's median `Record` is 1.744 ms and `Submit` is 4.100 ms. Those are host
measurements; Ebitengine's public API does not provide GPU or actual display
presentation timing here. Modern averages 29,698 submitted vertices and 23
passes per frame; the primitive types and work differ from this prototype.

## Native results

Measurements and reviewed captures will be recorded here after the integrated
prototype runs.

## Reuse and next decision

The prototype reuses the existing `purego` dependency and engine content APIs.
Ebitengine's lower-level Cocoa/Metal wrappers live in Go `internal` packages;
ordinary application code cannot import them. Reusing those implementations
would mean an explicitly maintained fork or extraction, including their
Apache-2.0 licensing obligations. Its public window loop also owns rendering
and presentation, so replacing its layer behind its back is not a clean public
extension point.

The experiment keeps that choice reversible. Its rendering observations should
guide whether to extract a narrow retained-resource API and keep the rest of
Nanolathe, rather than treating a small native benchmark as a finished engine
migration.
