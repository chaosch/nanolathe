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
are excluded from steady frame timing. GPU span means completed Metal
command-buffer start/end timestamps. These spans can overlap between the three
in-flight buffers; they are not exclusive GPU busy time and must not be added.
CPU submission, wall frame intervals and display presentation are reported separately. An offscreen throughput number
is never presented as observed display refresh.

The reference Mac is an Apple M3 Pro (12 CPU cores, 18 GPU cores, 18 GB unified
memory), macOS 26.6.2. Its active built-in panel reports 120 Hz, so this machine
can establish visible 120 Hz pacing and offscreen 144 Hz headroom; it cannot
establish visible 144 Hz delivery. Native target dimensions are explicit pixels.

Raw artifacts are outside the repository under
`/tmp/nanolathe-metal-prototype/`. A durable local copy is also saved under
`/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/metal-prototype/`.
Retail art is not committed.

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

**Result: pursue this architecture.** The rendering-only prototype delivered
120 Hz on the development Mac at both 1920×1080 and 3456×2234, with 1,500
submitted instances, 5× geometry, 2× atlas dimensions, lighting, glow and
distortion. That is 986,875 triangles per frame. The 144 Hz offscreen runs at both resolutions also
have substantial rendering headroom, but visible 144 Hz cannot be checked on
this panel, and the simple host timer does not produce uniformly spaced
144 Hz submissions.

These runs use the cgo-disabled Go executable built at commit `26708532` and
the bridge from `b9fb7bb8`. `build.txt` and `build-sha256.txt` beside the raw
results identify the binaries. All runs use `GOMAXPROCS=2`. No tests were
written or run; Go/native builds, runtime shader compilation, real-device
execution, native command completion status, source review and PNG inspection
provided verification. Review corrected the depth axis, hidden-piece handling,
texture-flag dispatch, normal orientation and measurement collection before
these results were taken.

### Sustained pacing

Every row below measures 3,600 frames after 240 warmup frames. The visible runs
last approximately 30 seconds, the offscreen 144 Hz runs approximately 25 seconds each.
Every measured visible drawable supplied a timestamp; none timed out. Display
intervals are Metal drawable presentation timestamps, not an external optical
measurement of panel scanout.

| Target / pacing | Mean submission rate | p99 present interval | p99 GPU span | Median CPU encoding | Peak Metal allocation |
|---|---:|---:|---:|---:|---:|
| 1080p, host cap 120 + VSync | 120.0 Hz | 8.338 ms | 6.630 ms | 0.425 ms | 106.1 MiB |
| 1080p, VSync backpressure | 120.0 Hz | 8.339 ms | 5.584 ms | 0.398 ms | 106.1 MiB |
| 3456×2234, VSync backpressure | 120.0 Hz | 8.340 ms | 6.690 ms | 0.389 ms | 324.4 MiB |
| 1080p offscreen, host cap 144 | 144.0 Hz | unavailable | 3.718 ms | 0.381 ms | 81.8 MiB |
| 3456×2234 offscreen, host cap 144 | 144.0 Hz | unavailable | 5.153 ms | 0.332 ms | 234.3 MiB |

The 1080p VSync-backpressure run has **no missed refresh intervals** in its
3,599 measured intervals. The host-capped 1080p and Retina runs each have one
interval near 16.67 ms; their other intervals are near 8.33 ms. This is a strong
30-second result, not an hours-long thermal or cross-machine guarantee.

At 144 Hz offscreen, p99 submission spacing is 7.840 ms at 1080p and 8.000 ms
at Retina resolution. Only 90.1% and 85.8%, respectively, of intervals fit
within 105% of the 6.944 ms target. Average throughput is on target
and GPU spans fit the budget, but timer jitter remains. Use a display-link
scheduler in the production host rather than promising steady 144 Hz from this
prototype's timer.

### Geometry and texture sweep

These are short **uncapped offscreen throughput samples**, 1,200 measured frames
after 240 warmup frames at 1920×1080. All enable the prototype effects except
the last row. They are separate from the sustained presentation evidence above.

| Instances | Geometry | Atlas | Triangles/frame | Median CPU encoding | p95 GPU span | Mean completed-pipeline submission rate |
|---:|---:|---:|---:|---:|---:|---:|
| 360 | 1× | 1 MiB | 47,370 | 0.038 ms | 2.345 ms | 1,185/s |
| 1,000 | 1× | 1 MiB | 131,513 | 0.046 ms | 2.865 ms | 988/s |
| 1,000 | 2× | 4 MiB | 263,026 | 0.053 ms | 3.254 ms | 875/s |
| 1,000 | 5× | 4 MiB | 657,565 | 0.065 ms | 4.146 ms | 689/s |
| 1,500 | 5× | 4 MiB | 986,875 | 0.071 ms | 4.578 ms | 626/s |
| 1,500, effects off | 5× | 4 MiB | 986,875 | 0.057 ms | 2.913 ms | 971/s |

The queue is bounded to three in-flight submissions and drained at finish;
these rates do not represent an indefinitely growing CPU command queue. GPU
spans overlap under saturation, so their reciprocal is not pipeline throughput.
Paced runs also let the GPU change operating frequency; compare matching modes.
The 1× to 2× rows change both geometry and atlas dimensions intentionally, while
2× to 5× holds the atlas fixed.

### Memory and actual work

The twelve unique meshes issue twelve instanced model draws. Terrain uses one
draw, composition one, and enabled bloom uses two compute encoders. Vertex and
index buffers and both input textures upload once. The 5× unique mesh storage
is 912,420 bytes (about 0.87 MiB), reused by all 1,500 instances. The immutable
instance-offset buffer is 6,000 bytes.

All 128 model texture names resolve, with no missing textures. A 512×512 RGBA
atlas is 1 MiB; its nearest-doubled 1024×1024 version is 4 MiB. It is shared
across types and instances, and does not grow with unit count. These figures
cover this twelve-type roster, not the entire game's catalog or future skins.

Each dense frame copies 1,824,000 bytes of adjacent piece matrices into a ring
of three buffers: 1.740 MiB/frame, about 209 MiB/s at 120 Hz. Geometry complexity
does not increase that upload. There were about 176 Go allocation bytes/frame
in the sustained runs (purego call overhead), with no per-frame mesh rebuilding.
Native Objective-C allocation counts are not measured.

Peak Metal allocations include input textures, retained buffers, HDR color and
emission, half-size bloom targets, depth, capture output and visible drawables.
The increase from 106 to 324 MiB at Retina resolution is primarily render-target
and drawable storage, not per-unit texture growth. The baked terrain input for
the dense scene is about 13.6 MiB.

The replay also retains 104.4 MiB of CPU-side sampled matrices (120 samples).
That is convenient benchmark storage, not the proposed production resource
contract: production would use the actual committed frame pair. Metal's
allocation count is not process RSS or total Go/content memory.

### Fidelity boundary

The twelve templates are sampled from an ordinary session using the normal
allocator, staged order binding, movement dispatch and committed COB pieces.
Metadata records exact sampled ticks and each template's changing positions
and pieces. Dense instances replicate those samples with different phase
and grid offsets. Instance and triangle counts mean **submitted**, not a
measured visible census; aircraft and moving samples can cross the framing.

The capture demonstrates real textured 3D units, interpolation, lighting,
colored ground illumination, bloom and shock-ring distortion. Terrain is a
flat baked TNT crop. Land units may appear over water because this is a
presentation grid, not legal battlefield placement. Lighting and post effects
are synthetic workload inputs, not ports of the game's full effect selection.
No live simulation, fog, features, projectiles, GUI, unit shadows, construction
reveal, cargo composition, animated texture playback, faction owner variation,
or streaming is in the measured loop. The recorded pose loop can jump at its
wrap. Matrix blending can shrink a rapidly rotating piece and must be replaced
with rigid transform interpolation before production.

The 5× faces are spatial subdivisions, not 5× richer silhouettes or bones.
The atlas enlargement adds storage and filtering work, not new texture detail.
A full-game speedup ratio versus the live Ebitengine benchmark would therefore
be unsupported.

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


The public `purego/objc` API is another way to express the native calls without
an Objective-C build step; the small bridge kept the prototype's callbacks and
ABI straightforward. A production choice between that and extracted Ebitengine
bindings should be made after the live integration, not as an abstraction
project ahead of it. No Ebitengine implementation was copied into this branch.

**Advantages:** geometry cost moves off the Go per-frame path; indexed instancing
keeps uploads and draw count small; atlas memory is independent of unit count;
custom shaders, depth, HDR targets, compute and completion timing are available
directly. The measured 120 Hz result holds with substantially more submitted
triangles than the stock roster.

**Costs and risks:** the Mac host and rendering backend now have lifecycle work
of their own; Windows/Linux need another backend; production assets need bounded
resource lifetimes, invalidation, atlas growth/mip handling and GPU-safe releases.
Conventional depth and triangle mapping change retail composition and coplanar
face behavior. Most game-specific composition and front-end work is still ahead.
The native bridge is intentionally prototype-quality, with no GPU-hang watchdog.

**Recommendation after the initial replay (completed below):** connect this resource/pose boundary to a live
committed battle frame before the current CPU model projection, using an opt-in
experimental host. Keep simulation, content loading and existing frame ownership.
Add the required terrain/fog, feature/effect, shadow, construction/cargo and HUD
passes incrementally; use the same live battle fixture to compare both hosts.
Keep Original as the compatibility path. Use that live comparison and actual
visual review as the replacement decision gate.

For host reuse, Ebitengine's existing window/input implementation is a valuable
extraction or fork candidate, provided there is one explicit owner of drawable
acquisition and presentation. Do not put a competing layer behind a running
Ebitengine renderer. Its public audio package also requires a running game;
public Oto (already a dependency) is a better independent device boundary for a
standalone host. Preserve Apache-2.0 headers, license and applicable notices if
extracting Ebitengine code. Reuse the existing Nanolathe presentation scheduling
and input policy where it is independent of Ebitengine.

## Running and artifacts

From this prototype worktree, with the retail install and Xcode command-line
tools available:

```sh
tools/metal-proto --out /tmp/metal-preview-new --units 1500 --quads 5 \
  --textures 2 --fps 0 --frames 3600 --width 3456 --height 2234

tools/metal-proto --out /tmp/metal-offscreen-new --units 1500 --quads 5 \
  --textures 2 --fps 144 --offscreen --frames 3600

tools/metal-proto-report /tmp/metal-preview-new /tmp/metal-offscreen-new
```

The output directory must be new. `--frames` controls the bounded run; closing
the native window also ends it. Output includes `scene.json`, `summary.json`,
`report.json`, warmup-inclusive `frames.csv`, texture previews and `final.png`.
The offline report adds `analysis.json`. The wrapper serializes builds through
the repository's host lock; the command acquires the benchmark lock for its run.

The measured directories are `mesh-360-q1`, `mesh-1000-q1`, `mesh-1000-q2`,
`mesh-1000-q5`, `mesh-1500-q5`, `mesh-1500-q5-no-fx`, `visible-1500-120`,
`visible-1500-vsync`, `paced-1500-144`, `retina-1500-vsync`, and `retina-1500-144`. The two live game
baselines are `baseline-classic` and `baseline-modern`. Captures were inspected
for both the existing renderers and the native prototype. Only authored code
and this report are committed; retail images and raw run artifacts remain local.


## Live battle continuation

The next experiment connects the retained backend to the existing
`headless.ComposeSimBenchBattle` fixture on Town & Country: three computer
armies, ordinary production/orders/weapons/deaths, Modern gameplay and seed 7.
It uses the same fixture contract as `--live-scene=field:250`, with difficulty
1 and a requested unit limit of at least 400; the Modern rule set resolves the
actual per-owner limit to 1500 in the recorded metadata. It begins at committed
tick 1200, after the
armies have moved into contact. It is an omniscient spectator prototype, with
arrow/WASD pan, wheel zoom and space pause. It has no order-entry UI or audio.

The simulation owns a worker at 30 Hz. After each tick it reads pinned published
frames and builds detached immutable presentation data. The renderer never
reads the mutable session. Allocation identities match previous/current unit,
feature and projectile transforms; births snap and deaths disappear. A fixed
one-tick presentation buffer gives the worker time to finish before interpolation
begins. Only the current and immediately previous publication are retained;
there is no unbounded history or replay loop. Matrix interpolation and conventional
floating point depth are still deliberate display approximations.

All 3DO meshes in the mounted asset namespace upload once, including future
products and wrecks. Draw counts and pose-buffer spans change with the live
battle. Pose/instance/sprite/light buffers reuse three native ring slots. Sprite
art is prepared from the map's committed feature definitions, all catalog unit
corpses and their authored dead/burnt/reclaim successors. The first attempt to
prepare every map's feature animations exceeded the prototype's atlas limit;
this scope reduction preserves reachable battle art without loading unrelated
maps. The model and sprite atlases remain shared across instances.

Actual GAF feature bodies, shadows and event animations, effect sprites, strip
particles, weapon sprites and beam endpoints now feed the native renderer.
Projectile selection reuses the production dispatch. Terrain/feature anchors use
`z - y/2`; feature height shear preserves the four-cell integer sum divided by
8 [03 §5.1.4]. Lighting comes from published flash geometry. The live path has
no synthetic point lights or shock rings. Bloom uses actual submitted sprites;
real displacement effects are still omitted.

The first short visible run reached approximately 120 Hz, but its motion trace
showed an interpolation stall each time the worker had not finished the next
pose pair. The fixed presentation buffer was added in response. Frame rate alone
would have missed this defect. `live-frames.csv` records the committed tick and
alpha alongside native frame timing, and `battle.json` records every simulation
and pose-preparation cost and census, including missed 30 Hz deadlines.

Live limitations remain: no fog/LOS/cloak admission, HUD, audio, unit shadows,
terrain height occlusion, cargo composition, nanoframe reveal, animated model
textures or per-team logos. Rest feature animations use their first frame;
destination-palette ALP and its composite children are approximated/omitted.
Strip particles and beam spans snap per tick, while their supported moving
anchors interpolate. Tall billboards use anchor depth rather than pixel height.
Detached/model effect fragments, secondary projectile models, segmented beams
and displacement lenses are counted but omitted. Native lights have a 32-source
cap. These omissions matter when comparing with the production renderer.

The matching production live trace averaged 77.7 host draws/s at 0.75× zoom
and 97.6 at 1× during the final 32 seconds of separate 72-second runs at
1920×1080, GOMAXPROCS=2 and a requested 120 cap. Their p99 draw intervals were
29.95 and 23.18 ms. This is host draw timing, not Metal presentation callbacks.
A 0.5× exploratory run is excluded because production switches to strategic
icons there. The attempted Classic live trace produced no rows: the trace's
start hook exists only in the Modern draw path. The bounded attempt was stopped;
no Classic timing is inferred from it. The earlier coastal Classic measurement
above remains a different workload.

### Live measurements

Established by local runs at code commit `be4ba9da` (the later report wording
correction changes no rendering). Native bridge sources last changed at
`bcaf24e6`. Both use the versions and Mac recorded above, `GOMAXPROCS=2`, and
an explicitly cgo-disabled Go build. Each row excludes 240 warmup frames and
includes 3,600 measured frames: about 30 seconds visible or 25 seconds at 144
submissions/s. Visible runs use uncapped submission with vsync. All four have
3,600 presentation timestamps and no missing callbacks or native run errors.

| Initial computer armies | Pixels | Geometry / model texture dimensions | Presented Hz | p99 presentation interval | GPU mean / p99 | Peak Metal allocation |
|---|---|---|---:|---:|---:|---:|
| 3 × 250 | 1920×1080 | 1× / 1× | 119.6 | 8.338 ms | 4.08 / 5.19 ms | 399 MiB |
| 3 × 250 | 1920×1080 | 5× / 2× | 119.8 | 8.338 ms | 4.88 / 6.64 ms | 468 MiB |
| 3 × 500 | 1920×1080 | 5× / 2× | 119.6 | 8.339 ms | 4.22 / 6.65 ms | 472 MiB |
| 3 × 500 | 3456×2234 | 5× / 2× | 119.9 | 8.340 ms | 5.54 / 6.99 ms | 690 MiB |

The Retina run uses zoom 1.35 versus 0.75 at 1080p to preserve horizontal world
coverage; its taller aspect ratio shows somewhat more world vertically. It
submitted 761–917 model instances, 536k–616k model triangles and 2,437–3,025
sprites per frame. The conservative culling margin means these are submitted,
not exact visible, counts. There are additional off-camera combatants. During
the measured window the session reached 1,485 units, 1,086 moving units, 44
nanoframes, 213 projectiles, 830 effect records, 6,353 strip objects and 24
burning features. Those maxima are not all from the same tick. It recorded 23
unit births and 128 deaths. No model or sprite art failed to resolve; explicitly
omitted effect families still apply.

The 750-unit run recorded 10 births and 44 deaths during measurement. Its
simulation plus presentation preparation averaged 8.68 ms/tick, p99 12.15 ms.
The 1,500-unit Retina run averaged 12.27 ms/tick, p99 16.05 ms: simulation
7.40 ms mean, pose/sprite preparation 4.87 ms mean. This work is at 30 Hz on the
worker, not every display frame. Native encoding p99 was 0.23–0.30 ms across
the four runs; it excludes Go preparation and worker time.

World motion is a separate result from display pacing. The fixed 33.33 ms
presentation buffer reduced the small run's p99 motion-step error from 8.33 ms
in the initial smoke run to 1.12 ms in the sustained run. Here error is the
absolute difference between the change in `(tick + alpha)/30` and consecutive
presentation timestamps; it is a timing proxy, not optical motion analysis.
The 1,500-unit 1080p run still missed four 30 Hz worker deadlines and had
6.25 ms p99 motion-step error, despite its 119.6 Hz presentation. Its slowest
combined tick took 46.06 ms. The Retina run had no missed worker deadlines in
the measured window and 1.15 ms p99 motion-step error. Host scheduling variation
and occasional worker stalls remain; the prototype does not guarantee perfect
world pacing. Warmup/PNG-drain ticks are recorded separately in `battle.json`.

The full Retina battle also ran offscreen at a requested 144 Hz: **144.0 mean
submissions/s**, GPU mean 4.32 ms and p99 6.16 ms. Submission p99 was 8.63 ms,
so the timer-driven host did not deliver every interval within 6.94 ms. It had
no drawable/presentation cost and a shorter committed-tick window, and cannot
establish 144 Hz visible delivery on this 120 Hz panel. The result supports
continuing toward 144 Hz, not declaring it finished.

A longer 18,000-frame visible run at 1600×900 completed after approximately
150 seconds, at 119.5 presented Hz, with ordinary ongoing battle activity. It
was launched to inspect controls; the first automation attempt timed out without
sending input. A subsequent authorized retry exposed an incomplete Cocoa launch
and short key taps disappearing within one event-pump batch. Commit `8f205587`
explicitly completes launch and retains taps for one input sample. Native UI
inspection then verified pause/resume, wheel zoom, arrow/WASD camera movement,
and clean window-close termination; the recorded pause lasted 33.55 seconds in
one check. A separate 24-instance, 12-frame offscreen replay smoke run completed
after the streaming changes. No tests were added or run.

The final host was re-measured after the interactive fixes at `8f205587`, with
3,600 visible Retina frames and the same large fixture: **119.9 presented Hz**,
p99 presentation interval 8.340 ms, GPU mean/p99 5.61/6.94 ms and native encode
p99 0.288 ms. It recorded all presentation timestamps, no missed worker deadlines
in the measured window, 0.93 ms p99 motion-step error and the same 690 MiB peak
Metal allocation. This confirmation is `battle-1500-retina-final`.

The production comparisons above share the fixture family, map, seed and
settings, but differ in presentation coverage, effects, camera/HUD composition
and host scheduling. Their end ticks differ. They establish that the new path
is promising on a real workload; **77.7 versus 119.8 is not a controlled,
feature-equivalent backend speedup ratio**.

### Memory and latency costs

The full mounted model namespace has 608 retained meshes and 496 resolved
texture names. Its shared model atlas is 8 MiB at original dimensions or
32 MiB at double dimensions, independent of army size. Increasing geometry
from 1× to 5× leaves the same battle's dynamic upload essentially unchanged
at 0.632 MiB/display frame. The larger Retina battle averages 1.304 MiB/frame
for poses, instance offsets, sprites and lights, about 156.5 MiB/s at 120 Hz.
This is copied input, not hardware bandwidth measurement.

The larger cost is **225.1 MiB for the shared feature/effect sprite atlas**,
8192×7204 RGBA, plus 64 MiB for the 4096×4096 terrain crop. The atlas preloads
3,457 authored animation frames from 22 banks, including future building
collapse states and reachable corpses. Three urban-building banks dominate.
Scoping preload to this battle fixed the initial over-capacity failure;
transparent-margin cropping saved zero bytes for these already tight assets.
Sprite textures are not doubled by `--textures=2`. This prototype therefore
answers the shared *unit texture* question well but still needs bounded sprite
resource management. Metal's peak includes inputs, meshes, triple buffers,
HDR/depth/bloom targets and drawables. It is not total process memory.

Go heap at the end of the four visible runs was 577–740 MiB; the large Retina
run had about 1.27 GiB reserved heap and allocated 3.27 GiB cumulatively across
measured rendering and final drain (warmup excluded). These are process-wide measurements including
simulation and detached presentation construction. Seven GC cycles accumulated
0.61 ms of stop-the-world pause; concurrent GC work is not isolated. Asset
preload, retained CPU texture/mesh copies and per-tick allocation are deliberately
rough. Pooling publications and releasing upload-only CPU data are worthwhile
next steps, with explicit frame ownership.

Latency also needs attention: visible submission-to-presentation median is
about 32 ms, on top of the 33.33 ms interpolation buffer and the ordinary
30 Hz state sampling. This is not a measured input-to-photon figure. Smooth
120 Hz cadence alone is insufficient; reduce queue depth and schedule near
presentation while maintaining enough worker headroom. Rigid translation/
rotation interpolation should replace matrix-column blending.

### Recommendation after the live battle

Continue the opt-in native backend. The experiment now demonstrates that actual
battle updates, persistent geometry, shared atlases, instanced custom shaders,
interpolated poses and sprite/effect submission fit 120 Hz on this Mac even with
5× surface subdivision. There is no reason from these measurements to rebuild
or upload unit geometry on every frame. Keep the existing content/session/frame
contracts and `purego` boundary; do not generalize a cross-platform abstraction
before another backend needs it.

The next useful work is visual and lifecycle integration: per-team atlas
selection, fog/cloak, construction/cargo composition, terrain occlusion and
shadows, then the missing effect families, real distortion and the existing HUD
and commands. Give the sprite atlas a bounded lifetime/streaming policy; evaluate
indexed palette textures for legacy art instead of expanding all frames to RGBA.
Choose a lower-latency presentation policy and reduce per-tick temporary data.
Re-run a matched scene with comparable coverage as each missing pass is added.

Ebitengine's host/input code remains a useful candidate for a narrow attributed
extraction or maintained fork. Reusing its public draw pipeline would give back
the limitations this experiment removes. The prototype's small Objective-C ABI
has been adequate through `purego`; rewriting it into `purego/objc` solely to
remove a build step is less urgent than fidelity, resource lifetime and pacing.
Keep the shipping renderer available until those gaps are closed and Windows/
Linux have an explicit backend plan.

### Reproduce the live run

From the `metal-prototype` worktree (output directories must be new):

```sh
# Visible Retina battle: about 30 seconds measured, plus preparation and warmup.
tools/metal-proto --battle --army-size 500 --preticks 1200 --seed 7 \
  --quads 5 --textures 2 --width 3456 --height 2234 --zoom 1.35 \
  --fps 0 --warmup 240 --frames 3600 --out /tmp/metal-battle-new

# Longer interactive spectator session. Arrows/WASD, wheel, space; close to exit.
tools/metal-proto --battle --quads 5 --textures 2 --fps 0 \
  --frames 100000 --out /tmp/metal-spectator-new

# 144 Hz throughput only; this creates no window.
tools/metal-proto --battle --army-size 500 --quads 5 --textures 2 \
  --width 3456 --height 2234 --zoom 1.35 --fps 144 --offscreen \
  --frames 3600 --out /tmp/metal-battle-offscreen-new

tools/metal-proto-report /tmp/metal-battle-new /tmp/metal-battle-offscreen-new
```

Live artifacts are in `/tmp/nanolathe-metal-live/` with a durable copy under
`/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/metal-live/`.
The four visible cases are `battle-750-q1`, `battle-750-q5`, `battle-1500-q5`,
`battle-1500-retina`; the offscreen case is `battle-1500-retina-144`.
`manifest.json` identifies the measured binaries and settings. Each case includes
CSV records, JSON census/timing reports and a captured `final.png`. Retail-derived
artifacts remain outside version control. The durable directory also includes a prebuilt `run-live.sh` launcher and both
required binaries for this Mac. The branch is committed and retained
for iteration; no production landing or tests are claimed.
