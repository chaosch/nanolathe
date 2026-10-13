# Cadence, motion and portability round — 2026-10-12

Branch `proto/wgpu-next-20261012` (from Codex's checkpoint `d68574eab`), with
the portability unit merged from `proto/wgpu-port-20261012`. Prototype only;
nothing is merged to main. Measurements are on the development Mac (Apple
M3 Pro, 120 Hz panel) with another agent session intermittently active, so
compare variants only within one alternating matrix.

## How to measure what players get

The fixed battle benchmark (`--battle-benchmark`) steps the simulation
synchronously on the frame that owns the tick. At 3,000 units the sim step
is ~15 ms, so every fourth frame costs ~25 ms and that benchmark cannot reach
120 Hz whatever the renderer does. Interactive play runs the simulation on
its own goroutine. Measure play instead:

```sh
BUILD/nanolathe-webgpu --webgpu --root=... '--map=Town & Country' \
  --live-trace=OUT-source --live-scene=field:1000 \
  --webgpu-report=OUT --webgpu-frames=3600 --webgpu-size=2560x1440 [--zoom=0.625]
```

with the `--fast` profile environment, plus either
`NANOLATHE_WEBGPU_BENCHMARK_NEUTRAL_POINTER=1` (WebGPU driver only) or
`NANOLATHE_HOST_FIELD_CAMERA=1` (shared host: camera on the armies, shake off,
pointer pinned at the view centre — use it to compare Metal and WebGPU). Do
not pass `--zoom=1`; at the play host's 2x scale `--zoom=0.5` is the
strategic icon view, so `0.625` is the densest model view. Presented
timestamps need the window visible; an occluded window reports zeros.

`--webgpu-quads=N` (`--metal-quads` on main builds) splits every model quad
into N partial-texture strips in play as well as the benchmark: the remaster
stress.

## Changes

Shared native host (`cmd/nanolathe/metal_*_darwin.go`, used by Metal and WebGPU):

- The retained world rebuild runs on its own goroutine while the effects
  record; the effects' client-free half (adapter, lighting, glow, water)
  runs beside the foreground, frame sources and projection.
- The presentation clock is sampled at the frame's predicted instant
  (`Client.PinPresentationAt`), not when the prepare-ahead worker ran
  `Next`; the camera blends from the host step's ideal instant.
- The cursor hover pick memoizes hull bounds per model and skips units whose
  rotated hull cannot reach the pointer (a differential test proves no pick
  changes). Walk pose histories are recycled.
- `--zoom` and `--metal-quads` apply in native play; `NANOLATHE_HOST_FIELD_CAMERA`.
- The texture-name memo is a concurrent map (the overlap exposed a race).

WebGPU renderer (`internal/battlegpu`):

- Portable shader/binding fixes R1–R7 (PORTABILITY.md): Tint syntax and
  reserved words, layout minimums, `any`/`all` for naga SPIR-V, no
  placeholder textures released before the first submit, pipeline before
  bind group.
- Browser: the frame callback never waits on the source (`Renderer.Redraw`
  re-presents the last prepared frame), and js/wasm builds stage queue
  writes through one growable `Uint8Array` (the stock binding's per-write
  ArrayBuffers exhausted Chrome within ~14 s).
- Opt-in direct model subjects (`NANOLATHE_WEBGPU_DIRECT_MODELS=1`): the
  composer's depth-slab subjects draw straight into the world pass, banded
  by paint rank, with four sub-sample taps. Off by default: at 1440p it
  costs more GPU time than the 2x atlas it replaces.

## Results

See the round's report for tables. Headlines (1440p, 3,000-unit field,
async simulation, `--fast` + late acquisition):

- World motion evenness: mean |world-time step − display step| fell from
  ~2.1 ms to ~0.1–0.4 ms per frame; at 120 Hz frames were already presented
  on a steady 8.33 ms grid but showed the world at uneven instants.
- Tick-frame source work: ~10.9 → ~8.5 ms mean with the two overlaps.
- Remaster stress, 3x quads (104K → 313K model triangles in view): GPU
  +0.23 ms (+4%), CPU unchanged — geometry is resident; only poses upload.
- Per-pass GPU (Metal System Trace, 1440p, zoom 0.625, ~830 units): world
  pass 1.81 ms, model shadow silhouettes 1.28 + mask 0.62 ms, body atlas
  1.18 ms, final pass 0.95 ms, glow 0.61 ms, ground light 0.26 ms.

- GOMAXPROCS: with the host overlaps, 4 -> 6 -> 8 Ps cut 1440p zoomed-out
  late frames 4.1% -> 3.0% -> 2.0% (tick-frame source p95 9.8 -> 8.4 ms);
  `run-play`'s prepare-ahead profiles now use min(8, cores).
- Deferring renderer queue writes until after the source handoff
  (`NANOLATHE_WEBGPU_DEFER_WRITES=1`) was slower: zoomed-out late frames
  1.0% -> 4.4%. It stays opt-in; the render-thread profile that motivated it
  (half of preparation inside the queue's pending-write setup) still stands.

## Dependencies

The prototype now runs on the stock pinned gogpu and wgpu. Our own code
stages rewrites of textures earlier frames sampled (stock `WriteTexture`
writes Apple's shared textures at once, which forced a per-frame wait),
chunks model uploads at the staging belt's chunk size, and acquires the
drawable late through gogpu's lazy acquisition. The owned-submission,
deferred-acquire, render-runtime and other unused patches are gone. What
remains: the browser staging edit (required), the Metal pointer cache
(`--metal-cache`, worth 2-3.5 Hz and half the late frames at 1440p), and the
measurement-only presented-handler and GPU-timing patches. Play at 1440p,
3,000-unit field: stock + pointer cache 116.3 Hz / 1.44% late against the
fully patched 115.3 / 1.73 (default zoom), 115.6 / 2.25 against 115.5 / 2.14
zoomed out. Details, upstream issues and the full table are in
`patches/README.md`.

## Shadows

A mobile's shadow is its body's own silhouette [03 R-REN-03D §1], so the
renderer no longer draws a solo subject a second time: the shadow composer
(`meshscene.ShadowComposer.Reuse`, chosen by the renderer package) gives a
reused subject a zero body rectangle and no silhouette draws, and the commit
reads coverage and the byte key from the body atlas through the subject's
model slot. Keyed children, composed groups and viewport-clipped slots keep
their own silhouette source (in this scene 67 of 901 subjects); the Metal
renderer, whose depth-slab subjects have no atlas image, keeps all of them.
Shadows now follow the body's production projection rather than the
unsnapped silhouette projection: final frames differ only along shadow edges.

Whole-frame GPU span (first command buffer start to last end) is what decides
the frame; Apple GPUs overlap the shadow encoders with other work, so per-pass
busy time overstates their cost. GPU-bound battle benchmark (2560x1440
non-Retina, zoom 1, 1,395 instances, 901 shadow subjects; two alternating
reps per row):

| Build | GPU span |
| --- | --- |
| before (silhouette redraw, four loads per sample) | 6.50 ms |
| solo subjects read their body raster | 6.20-6.36 ms |
| + one gather per 2x2 block, hardware bilinear aircraft taps | 5.97 ms |
| same, soft aircraft shadows off (`softShadows: 0`) | 5.65 ms |
| same, shadow commits skipped | 5.48 ms |
| no model shadows | 5.43-5.54 ms |

So shadows fell from ~0.95 ms to ~0.45 ms of a saturated frame, and the
render thread's encode time from 4.34 to 3.86 ms (fewer silhouette clones and
draws; the world pass also looked up each slot's shadows by scanning every
subject). Nearly all of the rest is the Enhanced aircraft blur: 186 aircraft
cover as many shadow pixels as 642 ground units, at 25 filtered taps each.
With the GPU unsaturated (play window, ~830 units, 1440p drawable) shadows
were already nearly free: 5.38 ms with them, 5.34 ms without.

Both atlases grow in 512-row steps and shrink after a second; the shadow atlas
now holds only structure projections and the few remaining silhouettes.
`NANOLATHE_WEBGPU_SKIP_SHADOW_COMMITS=1` isolates the commit's GPU cost.

## Next

- Aircraft soft shadows are most of the remaining shadow cost (above): fewer
  taps, or a mipmapped source sampled at the blur radius, would change their
  look and await a decision.
- ~5 MB/frame of model records upload between ticks (instances, slots,
  materials, shadow records rebuilt every frame); matters for discrete GPUs
  and the browser.
- Effects recording (~3.5 ms/frame of client work) is the largest per-frame
  source cost.
- Vulkan: V6 (MoltenVK buffer copy without memory) needs validation layers.
- Browser: HUD Wind/Tidal and Game Time text is missing; the 3,000-unit
  field exhausts wasm memory.
