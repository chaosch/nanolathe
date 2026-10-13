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

Aircraft soft shadows are now blurred once per frame instead of per pixel:
each soft subject's coverage is box-filtered to a slot texel of D source
texels (a power of two, so the radius is one to two slot texels) and blurred
with the same binomial kernel in two separable passes into a small R8 atlas;
the commit samples it once (four times over water). The water grade under a
soft shadow is computed on the CPU at the quad's corners, so the commit no
longer reads the liquid mask. Same benchmark, two alternating reps: 25 taps
6.07 ms, pre-blurred 5.99 ms; the passes cost ~0.14 ms, and soft shadows are
now within ~0.2 ms of hard ones, the benchmark's noise floor. Final frames
differ along aircraft shadow edges only (2.33% of pixels, two by more than
15 levels).

The body, shadow and blur atlases grow in 512-row steps. They used to shrink
after a second below three quarters of their size; in play the field's need
wanders across that bound, so they reallocated in grow/shrink pairs 120
frames apart, and the frame after each reallocation spent 20-60 ms on the
GPU. They now shrink only to half their area, after 600 frames: play at
1440p went from 117.8 Hz / 1.11% late to 119.2 Hz / 0.29% (two alternating
reps), and GPU frames over 10 ms from 13-15 to 4-5 per 3,600 frames. The
rest are growth, which still costs one 15-90 ms GPU frame: Metal System
Trace shows the GPU idle for that long before the first pass that uses the
new textures, while their memory is mapped. Zeroing them from the CPU at
creation only moves the cost to the CPU, and private storage does not
avoid it.

(Play rates in this section exclude the final capture frame, a 200-275 ms
interval the earlier summaries counted; with it the same runs read 115.7
and 117.7 Hz.)
`NANOLATHE_WEBGPU_SKIP_SHADOW_COMMITS=1` and
`NANOLATHE_WEBGPU_SKIP_AIR_PASSES=1` isolate the commit's and the blur's GPU
cost.

## Model records and atlases (overnight, 2026-10-13)

- Instances whose material selection is the same share one span of the
  selected-material buffer (the producer already shares the slice), and the
  buffer is rewritten only when its bytes change. Materials were 2.1-2.4 MB
  of the ~3 MB of model records written per frame. GPU-bound benchmark
  (three alternating reps): GPU span 5.97 -> 5.29 ms, record build 1.24 ->
  0.46 ms, uploads 4.96 -> 1.27 MB/frame, final frames pixel-identical. In
  play the render thread's model preparation halved (0.98 -> 0.43 ms) and
  uploads fell to 0.75 MB/frame.
- Atlas depth is Depth16Unorm and the key plane RG8Unorm: both hold only
  key ranks and flags. Pixel-identical; GPU time unchanged (bandwidth was not
  the limit); growth stalls 15-30% shorter; play 119.20 -> 119.58 Hz.
- Every allocation of a large texture mid-battle costs one frame a 20-60 ms
  GPU stall (see Shadows). After the shrink fix the remaining ones were the
  body atlas growing, the shadow atlas growing, and the merged plane, which
  was allocated only when the first composed group appeared.
  The body atlas is now reserved up front at one supersampled screen
  (four texels per drawable pixel at the packing width; 2048x8192 at 1440p,
  ~0.1 ms of clear and store), the shadow atlas at one texel per pixel, the
  merged plane is allocated with the atlas, and height-ordered (compact)
  slot packing is the default: it needs ~27% fewer rows, so the busy field
  no longer outgrows 2048x8192 into a 256 MB 4096-wide atlas at tick 647.
  Play at 1440p, two alternating reps: GPU stalls over 10 ms 2-3 (up to
  56 ms) -> 0-1 (18 ms) per 3,600 frames, 119.8 Hz, 0.13% late. Compact
  placement moves raster rectangles, so interpolated edges round
  differently: 0.16% of a final frame's pixels, scattered single pixels.
- Private storage for render targets (a wgpu Metal HAL change: textures
  without CopyDst use MTLStorageModePrivate) saved only ~0.15 ms of GPU
  and did not shorten the stalls; it is not worth a patch.

Play, 3,000-unit field, async simulation, two alternating reps per row
(final capture frame excluded; Metal is main's pure-Go renderer built from
this worktree, same shared host):

| Drawable, zoom | Start of night (WebGPU) | Now (WebGPU) | Metal |
| --- | --- | --- | --- |
| 2560x1440, default | 117.8 Hz / 1.11% late | 119.8 Hz / 0.19% | 119.2 Hz / 0.59% |
| 2560x1440, 0.625 | 119.75 Hz / 0.17% | 119.8 Hz / 0.13% | 119.4 Hz / 0.46% |
| 1920x1080, default | 119.5 Hz / 0.27% | 119.95 Hz / 0.04% | - |
| 1920x1080, 0.625 | 119.75 Hz / 0.21% | 119.65 Hz / 0.21% | - |

GPU per frame at 1440p is ~5.6-5.8 ms for WebGPU against ~5.9-6.5 ms for
Metal, so ~2.5 ms of slack at 120 Hz. Remaster stress at 1440p, zoom 0.625
(`--webgpu-quads=3`, 103K -> 308K model triangles): 119.82 -> 119.72 Hz, GPU
5.70 -> 5.74 ms, model preparation unchanged at 0.71 ms. The remaining late frames are host CPU
hitches (source work of 14-18 ms in a single frame), not GPU stalls.

### CPU headroom

Tick frames are now the CPU risk: at zoom 0.625 their source work averages
7.5 ms with p95 8.2-8.3 ms, at the edge of the 8.33 ms budget (default zoom
6.2 / 7.1 ms). Stage timers (`effects_*` in `phase_timing_scope`; the
macOS CPU profiler misattributes waits and is not reliable here) show:

- The client's effects capture is 2.2 ms (3.5 ms on tick frames). Its eight
  batches total ~1.5 ms; the rest is the presentation blend of every unit,
  which grows by ~1.2 ms on tick frames.
- Projectile models cost 0.45 ms a frame although the GPU model lane draws
  them: the client still projects every face on the CPU only to emit a paint
  marker. Debris models (0.59 ms) are drawn from CPU-projected geometry.
  Both scale with model detail, so remastered models would multiply them.
  Giving projectile markers a geometry-free path and moving debris onto the
  retained lane would remove ~1 ms; both touch shared client code with retail
  paint-order rules and need their own review.
- `NANOLATHE_WEBGPU_GO_TRACE_SECONDS=N` writes `go.trace` for the first N
  measured seconds; use the module's toolchain (`go tool trace -pprof=sync`
  from the worktree) to read it.

### Browser

Headless Chrome, 1080p: the 750-unit field runs at Chrome's 60 Hz cap
(callback 17.2 ms, model uploads 238 -> 64 KB/frame). The 3,000-unit field
now completes 1,800 frames instead of exhausting memory, at ~10 fps: the
simulation takes 73 ms a frame (2.1 ticks of ~35 ms on one wasm thread), the
renderer ~4 ms. The wasm heap reaches 2.2 GB, so long sessions may still run
out.

## Next

- Instance records (~0.4-0.6 MB/frame) are rebuilt every frame though most
  fields change only per tick; splitting per-tick and per-frame fields would
  cut uploads further.
- Effects capture and projectile/debris models on the CPU (CPU headroom).
- Vulkan: the MoltenVK proxy now plays a battle without errors (V4/V6 were
  resources released with queue writes pending; PORTABILITY.md). Its ground
  is black (V7, the R32Uint terrain lookup arrives filled); settle with
  validation layers or a Linux/Windows machine. wgpu's Vulkan WriteBuffer
  waits for the GPU before every write, an upstream cost for Linux/Windows.
- Browser: the 3,000-unit field is simulation-bound and holds a 2.2 GB
  heap. (The missing Wind/Tidal and Game Time text was the harness's default
  settings: the "Wind/tide in game" readout, `weatherReport`, is off by
  default, and the native runs used settings with it on.)
