# Retained Metal renderer experiment

**Latest result (2026-10-07):** the matched full-simulation comparison is in
[Final full-simulation comparison](#final-full-simulation-comparison).
It supersedes the earlier near-120 Hz recommendation: baseline Metal is close
to Modern, and exact renderer parity and steady 120 Hz remain unmet.

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
The initial replay experiment deliberately used modern depth geometry and sampled floating
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

This section records the first live implementation. The later visual-pass
continuation below supersedes its list of omitted passes and its recommendation
to add them; the original measurements remain useful as a historical baseline.

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


## Visual-pass continuation

The next user-authorized throwaway iteration adds retained team materials,
construction reveal and outlines, cloak opacity and attached-unit admission,
terrain height occlusion, geometric unit/wreck shadows, fog, detached whole-piece
debris, explosion rings, burning-tree shimmer and cooling wrecks. It also replaces
generic effect lights with explicit authored explosion/fire/projectile/wreck
sources. Smoke stops supplying its own glow. All inputs come from committed
frames, immutable content or existing Enhanced presentation policy; authoritative
simulation code is unchanged.

Team logo frames share the same atlas with ordinary unit textures. The 496
texture names now resolve through 497 material records (including reserved zero)
and 650 UV rectangles, yet the doubled model atlas remains **32 MiB**. Meshes,
material tables, edge indices, atlas and terrain heights upload once. Per-frame
copies contain poses, instance state, sprites, up to 32 chosen lights, bounded
distortion descriptors and optional fog pixels. Original authored edges remain
independent of the 5× surface subdivision.

The new native order is shadow coverage, terrain/body/outline/sprite HDR color
and emission, bloom, world resolve, one immutable distortion snapshot/batch,
then fog. The blast and plume displacement profiles reuse
[DESIGN_GPU_RENDERER §25–28](DESIGN_GPU_RENDERER.md#25-explosion-distortion).
Root review corrected the plume's bottom-center offset and replaced the first
shadow mask's eight-byte HDR pixels with one-byte R8 pixels. Shadows use maximum
blending so overlapping casters do not repeatedly darken the terrain. There are
still two additional full-resolution HDR post targets; a frame with distortion
copies the full world snapshot. Clipped regions and target reuse remain useful
future optimizations.

### Visual scope and remaining approximations

The native captures show three team colors, shadows, live fire and combat,
construction outlines/revealed surfaces, and detached pieces. Ring/heat counters
prove the native batch was submitted. A still capture cannot establish the
quality of every temporal effect. This is still an approximate modern renderer:

- Construction uses continuous world-height clipping and authored mesh edges,
  rather than the retail five-stage raster reveal and scanline endpoints.
- Cloak uses per-surface alpha and normal depth; cargo follows published poses
  and carrier admission, without the retail staged subject-image composition.
- Terrain height uses a four-iteration inverse shear for depth under the painted
  TNT background; shadows use a constant 35% geometric footprint and fixed
  five-pixel offset. Texture cutouts, soft aircraft shadows and retail body punch
  are not reproduced.
- Fog samples authored Gray/Black GAF masks every eight world pixels and applies
  scalar RGB gray modulation; it does not reproduce destination-palette ALP.
- Matrix-column interpolation can shrink rotating parts. Billboards use anchor
  depth and may clip incorrectly against tall terrain/geometry. Whole-piece
  debris snaps because its published slot has no allocation identity; paired
  shatter triangles and their trails remain omitted.
- Ordinary animated model textures, water/reflections, scorch/trails, smoke
  scattering, segmented-beam CRT geometry, secondary projectile replacement,
  palette flash discs, projectile lenses, HUD/order input and audio remain owed.
  Light selection uses 32 strongest sources rather than the shipping family's
  reserved budgets. Neutral-pose bounds approximate wreck plume extents.

No tests were added or run. Root reviewed the agent diffs, checked blast/plume
constants and material/frame bounds, compiled the Go host with cgo disabled and
the Objective-C bridge with warnings enabled, then compiled shaders on the actual
Metal device through real battle runs. The experiment remains isolated on its
branch and has not passed production landing gates.


### Re-measured visual-pass results

These are historical measurements from before the visual corrections below.
The old captures contain clipped sprites, blurred fog and incorrect Solar
ownership; performance results do not establish visual correctness.

These sequential runs use the same M3 Pro, Go/purego/Metal setup, Town & Country,
Modern three-army fixture, seed 7, tick 1200 lead-in, `GOMAXPROCS=2`, 240 warmup
frames and 3,600 measured frames. Every row uses 5× face subdivision and doubled
model-atlas dimensions. The binary's source tree matches `a82d6212` (built just
before that integration commit); the final native bridge is `223dd3440`. Binary
hashes and exact flags are preserved in the artifact manifest. These are local
single-run observations, not confidence intervals.

| Case | Actual presented Hz | p99 present interval | GPU mean / p99 | Peak Metal allocation |
|---|---:|---:|---:|---:|
| 3 × 250, 1920×1080, all new passes | 119.7 | 8.340 ms | 5.79 / 6.78 ms | 533 MiB |
| 3 × 500, 3456×2234, all new passes | 119.9 | 8.340 ms | 6.51 / 6.92 ms | 849 MiB |
| Same Retina setup, new passes disabled | 120.0 | 8.340 ms | 6.11 / 6.59 ms | 721 MiB |
| 3 × 500, Retina, ordinary player 1 with fog | 120.0 | 8.340 ms | 5.96 / 6.54 ms | 863 MiB |

The new-pass toggle isolates model visual state, height depth, shadows, outlines,
distortion and fog. Team material lookup, debris, the revised light selection and
scene preparation remain active in both sides. On this pair the passes cost
0.40 ms mean GPU span and 127 MiB peak Metal allocation. Compared with the earlier
complete native baseline (5.61 ms GPU mean, 690 MiB), the entire visual iteration
adds about 0.90 ms and 158 MiB. The ordinary player view submits fewer hidden
objects, so its lower GPU span is not a measure of fog being free.

All visible runs had zero missed 30 Hz worker deadlines during measurement.
The 1080p and full Retina runs recorded all 3,600 presentation callbacks; the
no-pass and fog runs each lacked three timestamps, which are excluded. Their
measured intervals fall within 105% of the 120 Hz budget 99.72%, 99.94%, 99.97%
and 99.97% of the time respectively. Occasional 16.67 ms intervals remain;
this is near-steady 120 Hz, not a promise of zero dropped refreshes. P99 motion
step error was 0.71 ms in the full Retina run and 0.91 ms with fog. Submission
to presentation still has roughly 32 ms median latency, plus the one-tick
presentation buffer; input-to-photon latency has not been measured.

The full Retina battle again reached 1,485 live units and 1,086 moving units,
44 nanoframes, 213 projectiles, 830 effects, 6,353 strip objects and 24 burning
features in the measured window. Submitted native maxima include 937 model
instances, 629,310 body triangles, 872 model shadows, 23 construction outlines,
90 detached whole-piece instances, 10 hot wreck models and 38 distortion quads.
These are separate maxima, not one frame; conservative model culling and hidden
pieces mean submitted triangles are not a count of visible pixels. The pass
census and captures agree that the new paths are exercised. Cloaked units were
zero in this fixture; cloak and carrier-specific composition are implemented
approximations that still need their own visual scenes.

At an offscreen 144 Hz target the large Retina battle averaged **143.9
submissions/s**, with GPU mean/p99 **6.46/7.83 ms** and p99 submission interval
8.43 ms. Only 78.0% of intervals fit 105% of the 6.94 ms budget. There was one
missed worker deadline. No display is involved, and this Mac's panel is limited
to 120 Hz. This supports 144 Hz as a next optimization target, not a steady
144 Hz claim.

That build uploaded the complete 1092×1092 RGBA image, 4.55 MiB, every display
frame. Total dynamic copies rise from 1.408 MiB/frame in the spectator run to
5.429 MiB/frame with fog (about 652 MiB/s at 120 Hz). Native encode p99 rises
from 0.243 to 0.738 ms. On the worker, pose/sprite/fog preparation averages
5.76 ms without fog and 7.36 ms with it, still at 30 Hz. Retaining unchanged
fog texture regions and storing scalar coverage would address both copying and
memory. End-of-run Go heap is 717 MiB for the full spectator and 993 MiB for the
fog case; this is separate from Metal allocation and includes retained CPU assets
and temporary publication data.

### Recommendation after the visual passes

Continue with this retained Metal/purego design: the extra world passes fit the
120 Hz target on a real battle with the enlarged geometry workload. The shared
unit atlas is effective, and the vertex/fragment pipeline handles materials,
pose sampling, reveal, depth, lighting and real refraction without uploading
unit geometry per frame.

Next prioritize resource lifetime and visible correctness: retain changed fog
regions, replace full-frame distortion copies with clipped work/target reuse,
release upload-only CPU assets, and bound the 225 MiB sprite atlas. Resolve
billboard/transparent-subject depth and cloak/cargo composition, then integrate
the existing HUD and order input. Rigid transform interpolation and lower queue
latency remain worthwhile. Water, remaining weapon/effect families and broader
maps must be measured before replacing the shipping renderer. No new
cross-platform abstraction is justified by this Mac-only result yet.

A 24-instance replay smoke and a short real-battle effects-off smoke also
completed without native errors. The latter exposed an incorrect report flag
(the distortion count and native draw were correctly disabled); `97d869de` fixes
that report and the replay visual-pass label without changing rendering. The
final review binary passed the effects-off smoke again and is the saved launcher
version. The measured binary is also retained so results remain reproducible.

### Visual-pass artifacts and reproduction

Raw runs are in `/tmp/nanolathe-metal-visual-runs/`. Durable captures, CSV/JSON
records, a report, binary hashes and a prebuilt launcher are under
`/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/metal-passes/`.
The earlier `metal-live` artifacts remain unchanged. Existing commands above now
enable the added passes by default; use `--visual-passes=false` for the native
comparison, `--view-player=1` for ordinary visibility/fog and `--effects=false`
to disable lighting, bloom and distortion. Team atlas lookup stays enabled in
the pass comparison. The prebuilt launcher accepts the same options and starts
an interactive spectator session by default.


## Playable match through the production HUD

The playable entry now uses `cmd/nanolathe`'s ordinary `newDirectBattleView`,
`gameShell.step`, client input, camera, battle HUD, audio and asynchronous session.
The native host feeds mouse, keyboard, wheel, pinch, text and focus events into
those existing services. It does not implement another order controller or HUD.
The earlier `nanolathe-metal --play` route now directs callers to `tools/metal-play`.

The existing foreground draw list supplies the interface art, build products,
resource bars, minimap, labels, selection, placement and cursor. A Metal executor
caches its GAF/PCX/FNT and dynamic surfaces in a 4096-square RGBA atlas, emits
ordered alpha/multiply batches and stages only changed atlas rectangles. World
meshes, shared model atlas, vertex/fragment shaders and visual passes continue
through the retained native pipeline. World and interface use the same pinned
production frame pair and camera; the native host has no separate simulation
clock. The existing late cursor positioning also runs at the presentation rate.

Run from the prototype worktree:

```sh
tools/metal-play --seed 7
```

This compiles the cgo-disabled Go host and native bridge, then starts The Pass
against the existing Modern computer player. Existing gameplay preferences still
select the battle's rules. Usual command-line options can override the map and
AI. `--metal-size 2880x1800` is the default physical drawable (1440x900 logical),
with 5× surface subdivision, doubled model textures and a 120 Hz cap.
`--metal-frames N` makes a bounded measurement; zero runs until the window closes.
Closing the window or Command-Q writes the capture, timings and report into the
new output directory printed by the launcher. Retail assets are loaded from the
normal external install.

A prebuilt `Play Metal Match.command`, host, bridge, hashes and measured
runs are saved outside the repository under
`/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/metal-match/`.
The launcher can be opened directly and accepts the same command-line options.

### Initial playable measurements and inspection

These runs predate the visual corrections below and retain those defects.
Both runs use this Mac's M3 Pro and 120 Hz panel, The Pass, seed 7, the ordinary
human-versus-Modern-computer opening, 2880x1800 drawable, 5× geometry, 2× model
textures and 120 warmup frames. The host used 12 Go processors for these runs;
`tools/metal-play` defaults to two, as the earlier stress launcher does. These are
interactive opening checks, not matched performance comparisons or completed
matches: camera/input and host activity differ. They do not replace the large
battle measurements above.

| Run | Measured frames / seconds | Presented Hz | p99 presentation interval | p99 GPU span | p99 host preparation | Peak Metal allocation |
|---|---:|---:|---:|---:|---:|---:|
| production-02 | 12,000 / 100.22 | 119.74 | 8.339 ms | 3.970 ms | 1.834 ms | 590 MiB |
| production-03 | 18,000 / 150.57 | 119.54 | 8.340 ms | 6.735 ms | 1.939 ms | 590 MiB |

The latter run includes the reviewed native pointer capture, dynamic HUD palette
and Cocoa run-loop servicing. All measured presentation timestamps were returned,
with no native error or input queue overflow. 99.68% of its intervals fit 105% of
the 120 Hz budget; the longest was 58.34 ms. This is near-steady 120 Hz with
occasional stalls. GPU, host preparation and presentation interval are distinct
measurements and must not be added as if they ran sequentially. The HUD atlas is
64 MiB, and its staged upload buffers retained about 66 MiB; 72 uploads copied
80.91 MiB in total, including the initial full atlas. End-of-run Go heap was
509 MiB, separate from Metal allocations. Median submit-to-present remained
about 32 ms; input-to-photon latency is still unmeasured.

Root inspected the native production HUD and exercised commander selection,
movement and the existing Solar build button. Its down/up input arms `armsolar`
and displays the production placement preview. The production-03 final capture
also shows a completed Solar Collector beside the commander; the final census
has two human-owned units and eleven computer-owned units. The check does not
establish every command, modal, save/load flow or a complete match. The native
run loop now services Cocoa sources while rendering, so accessibility inspection
no longer waits until the game exits.

The raw production-02/03 scene metadata inherited zero seed fields and a stale
"no GUI/audio" limitation from the earlier fixture helper. Those fields are a
reporting defect, not the actual command-line seed or host: both runs were
launched with `--seed 7` through the production battle shell. The packaged source
now records the session's actual two seeds, gameplay mode, unit limit, production
camera and foreground ownership. The immutable raw reports are preserved, with
this correction called out in the artifact manifest.

### Remaining scope and recommendation

The corrected build is available for another manual match attempt with the
normal HUD and controls; a complete match has not yet been validated.
It still supports one battle per launch and a fixed window surface. A menu action
that replaces or leaves the battle ends this host; restart/load-to-another-battle
and full front-end navigation are not integrated. The foreground executor does
not support embedded model commands in GUI previews, and world labels/selections
are drawn after world depth/fog rather than inside their original painter slot.
The later correction section supersedes the earlier fog, terrain-depth and
base-shading descriptions. HUD, ordinary order input and audio are reused from
production; the other visual limits still apply. Palette changes rebuild the HUD
atlas; this needs a cheaper path if continuous fades prove costly. Audio is wired
through the existing engine but was not separately auditioned in this check.

Keep the production shell/HUD/controller and iterate on the renderer underneath
it. Next useful work is a complete manual match, command/modal coverage, rigid
pose interpolation, transparent-subject depth, remaining water/effect families,
and resource lifetime/memory reduction. The previous offscreen result still does
not establish steady 144 Hz. No tests were added or run, per the prototype request;
Go/native compilation, shader compilation on-device, visual inspection, source
review and the recorded live runs are the validation. The work remains committed
on the isolated prototype branch, without production landing gates or a main merge.


## Corrections after the first manual match

The first match exposed three separate renderer defects. Root reproduced them
with the opening battle and fixed model captures, then reviewed the native
results against the existing production renderer:

- **Clipped feature and effect sprites:** the reconstructed terrain depth plane
  rejected billboard pixels below their world anchor. TNT now draws as the
  painted background without writing scene depth, as the existing world
  composition requires (DESIGN_GPU_RENDERER C-G5). The height field remains for
  shadow receivers. Complete metal patches and tree bottoms are visible again.
- **Solar base and commander shading:** source pieces now draw last-to-first,
  and interpolated integer model-height ties preserve later-face ownership.
  Both changes were necessary for the opened Solar's continuous base rim.
  Retained averaged normals feed the existing SHD row arithmetic and mobile /
  piece shading admission; rows interpolate before fragment flooring and the
  true-colour scale clamp. This replaces the unrelated base Lambert light that
  darkened the commander and structures. Dynamic lights still add separately.
- **Blurred unexplored/fog boundaries:** the CPU no longer reduces the masks to
  eight-world-pixel samples for a linearly filtered coverage texture. A retained
  896×513 atlas contains the original Gray/Black masks and palette colours. The
  fragment pass reads integer mask texels and executes the production cell
  operations in order, including the authored overhang and gray desaturation
  (DESIGN_GPU_RENDERER C-G7). The existing dither display preference is forwarded.

The fixed Solar capture uses the production COB `Create`/`Activate` pose; the
commander capture copies the actual opening publication. Both submit an identical
previous/current pair, without changing the authoritative session. Reproduce with
`tools/metal-play --metal-model-capture armsolar --metal-size 1280x960 --seed 7`
(or `armcom`). These are offscreen inspection fixtures, not performance runs.

This is still a triangle renderer with approximate shared scene depth. It does
not reproduce wrapped byte-height keys, quad scanline interpolation, Digger
clipping or carrier subject composition. At subdivisions above one, generated
corners are quantized separately and can change shading/ownership. Float normal
transforms differ from fixed-point normals; matrix interpolation can still shrink
large rotations. Debug changes to the model light are not forwarded. The fog
checker uses native drawable parity; exact doubled logical-pixel checker parity
needs a separate comparison. Stock fog masks work; unsupported composite or
out-of-tile mask art fails preparation explicitly. These are presentation limits,
not changes to gameplay or published visibility.


### Final corrected-build measurement

Sequential final-build runs used `GOMAXPROCS=2`, seed 7, 5× subdivision, doubled
model-atlas dimensions, all native visual passes, and 3,600 measured frames.
The large scene repeats Town & Country, three 500-unit armies, tick 1200 lead-in,
ordinary player 1 visibility, zoom 1.35 and 240 warmup frames. The opening uses
The Pass with the production HUD, Modern computer, default camera and 120 warmup
frames. These are single local runs, not confidence intervals or a complete match.

| Case | Presented Hz | p99 presentation interval | GPU mean / p99 | Peak Metal allocation |
|---|---:|---:|---:|---:|
| Corrected large battle, 3456×2234 | 120.00 | 8.339 ms | 6.34 / 6.98 ms | 851 MiB |
| Corrected production-HUD opening, 2880×1800 | 119.53 | 8.338 ms | 3.85 / 5.45 ms | 589 MiB |

All measured presentation timestamps were returned, with no native errors,
failed GPU command rows or input overflows. Every large-battle interval fit 105%
of the 120 Hz budget; the opening fit 99.75%, with a longest interval of 29.16 ms.
There were no missed worker deadlines within the large run's measured tick range
1262–2162; one occurred at tick 1203 during warmup. The measured census reached
1,502 live units, 1,123 moving units, 45 nanoframes, 139 projectiles, 631 effects
and 25 burning features (separate maxima). Native submission reached 843 model
instances, 557,305 body triangles, 1,771 sprites and 35 distortion quads. The final
combat capture visibly retains complete explosion discs and feature sprites.
The opening capture shows the crisp authored fog fringe and full metal patch.
Root inspected these and the activated Solar / opening commander captures.

The large map now copies a 273×273 operation grid, **0.284 MiB per frame**, instead
of the previous 4.55 MiB sampled fog image: exactly 16× fewer fog bytes. The mask
atlas is a one-time 1.75 MiB upload. Total dynamic copies averaged 1.165 MiB/frame,
and native encoding p99 was 0.291 ms, versus the earlier 5.429 MiB and 0.738 ms.
Those total-work timing comparisons span live runs and renderer changes, so they
are observations rather than an isolated fog speedup. End-of-run Go heap was
921 MiB for the battle and 585 MiB for the opening, separate from Metal memory.
The 32 MiB shared unit atlas and 225 MiB stress-scene sprite atlas are unchanged.

Artifacts `battle-1500-fog-02`, `opening-03`, `solar-04` and `commander-02` are in
the `metal-match` directory above, with binary hashes and command manifests.
The corrected benchmark executable is included alongside the playable host.
The earlier production-02/03 artifacts are retained as historical results.

Keep this backend for the next manual match. It now has credible 120 Hz headroom
with corrected stock fog and model appearance, but do not replace the shipping
renderer yet: subject overlap, rigid interpolation, remaining effects/water,
resource lifetime and a complete match remain the useful next checks. Steady
144 Hz has not been re-established. No tests were added or run.


## Rigid motion and short-feature ordering follow-up

The next prototype iteration keeps the production HUD/input host and changes
only presentation. Rotating pieces now use shortest-path quaternion slerp in
one GPU compute invocation per piece. Body, shadow and outline draws reuse its
64-byte output, rather than blending matrices separately in every vertex
invocation. The conversion removes and restores the existing world-Z reflection,
keeps equal bases and spatial endpoints exact, and preserves current shading
metadata and the existing either-endpoint visibility gate. This prevents the
matrix-blending shrink during turns. Already-composed piece origins still blend
linearly: joint spacing can change during articulated rotation, so full hierarchy
interpolation remains unfinished.

Short sprite features (authored height below 10) now draw in publication order,
shadow before body, between TNT and models. That restores the existing client's
short-feature rule [03 R-RAST-01 §6]. The remainder of the sprite batch sorts
stably at the displayed interpolation fraction, including depth bias and clamps,
instead of sorting once at the current tick's position. Native uploads use one
216-byte descriptor, with a count marking the ground-sprite prefix. Publication
slices remain immutable; sorting uses reusable presentation scratch. This adds
one sprite draw when both groups are present. Tall-feature rows, airborne/strip
phases and cloak subject composition are still approximations.

### Verification and measurements

Root reviewed the delegated compute implementation and the sprite changes,
including reflection/quaternion signs, endpoint ownership, the 216-byte ABI,
slot lifetime and shader depth arithmetic. Review caught and fixed a default
position W lane that would otherwise have tagged every sprite as a ground
feature. Both Go commands built with cgo disabled, the native bridge built with
Clang, and runtime Metal shader compilation and command completion succeeded.
No tests were added or run. The branch is still unmerged.

The fixed Solar and commander PNGs are byte-identical to the preceding corrected
build. A new `--metal-model-capture armcom-ground` fixture copies the opening
commander pose onto the nearest actual short sprite feature, without modifying
the simulation. Against a control bridge that draws that prefix late, it changes
49 pixels around the lower legs in the inspected capture; the rest of the image
is identical. This is a narrow overlap check, not proof of general composition.
The retained replay path also ran and produced an inspected capture with the new
compute pass. Root inspected the battle and production-HUD opening captures;
complete effects/metal art and the authored crisp fog fringe remain visible.

Sequential M3 Pro runs used seed 7, GOMAXPROCS=2, 5× subdivision, doubled model
atlas dimensions, all visual passes and the 120 Hz panel. The larger run uses
Town & Country, three 500-unit armies, 1200 lead-in ticks, player 1 visibility,
zoom 1.35, 240 warmup frames and **7200 measured frames**. The ordinary opening
uses The Pass, the Modern computer, the production HUD, 120 warmup frames and
3600 measured frames. These are local observations, not confidence intervals.

| Case | Presented Hz | GPU mean / p99 | Presentation p99 / max | Peak Metal allocation |
|---|---:|---:|---:|---:|
| Longer large battle, 3456×2234 | 118.41 | 6.73 / 12.44 ms | 8.340 / 133.33 ms | 854 MiB |
| Production-HUD opening, 2880×1800 | 119.11 | 3.69 / 6.34 ms | 8.338 / 91.66 ms | 605 MiB |

The large run delivered 99.33% of presentation intervals within 105% of the
120 Hz budget; the opening delivered 99.78%. All measured presentation timestamps
arrived, all command rows completed, and neither run reported a native error or
input overflow. The longer battle had seven missed simulation-worker deadlines
within measured ticks 1262–3079. Its separate census maxima were 1502 units,
1123 moving units, 51 nanoframes, 139 projectiles, 631 effects and 38 burning
features; submissions peaked at 843 model instances, 557305 body triangles,
2254 sprites and 39 distortion quads. Both sprite passes ran in every measured
frame. The three computed-pose slots added about 3 MiB of GPU buffer capacity.
Go heap at the end was 981 MiB for the battle and 422 MiB for the opening,
separate from Metal allocations. Battle packing p99 was 0.223 ms and native
encoding p99 was 0.508 ms; dynamic uploads averaged 1.122 MiB/frame.

This longer result does **not** establish steady 120 Hz. It has occasional
stalls despite an 8.33 ms median display interval. The prior 30-second result
covered less simulation time and was smoother; these runs do not isolate the
cause of the difference. Profile slow-frame clusters and allocation/resource
lifetime next, alongside tall-feature/aircraft/cloak composition. Retain the
architecture and try the new playable build, but defer replacing the shipping
renderer. Visible 144 Hz and a complete match through victory/defeat remain
unverified.

The preceding user match's saved report reached tick 9313 (5:10), with six human
units, factory/construction activity and no native error; its final image was
inspected. That is useful play evidence, not a completed-match result. Interactive
reports keep a bounded timing tail, so its timing cannot characterize the entire
five minutes. This iteration's automated opening check does not claim fresh
manual verification of all mouse/keyboard actions.

Current binaries, launch command, hashes, manifests and captures are retained at
`/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/metal-motion/`.
Runs are `battle-1500-fog-01`, `opening-01`, `solar-01`, `commander-01`,
`ground-early-01`, `ground-late-01` and `replay-01`. The earlier `metal-match`
package remains available for comparison.


## Airborne composition, cloak and wreck stability follow-up

The prototype now draws grounded models, effects and airborne subjects in the
production broad order. Committed MoverMode chooses the unit pass; attached
subjects inherit the presenting carrier. Air starts a fresh depth plane. Opaque
frames keep one tile render encoder, using a depth-only fullscreen reset before
aircraft, avoiding two extra HDR attachment store/load cycles.

Cloaked subjects render opaque into shared scratch first, then resolve once at
50% opacity. This prevents rear faces and equal-height surfaces accumulating
opacity. Keyed carriers include their direct published cargo in the same
resolve, parent first and then cargo order. Admission belongs to the carrier;
self-carrier sentinels remain roots. Deeper chains follow the production walk's
omission. Native height ownership, keyless cached/live separation, destination
palette ALP and tall-feature row barriers remain approximations. Scratch is
allocated only on first cloak: two RGBA16F images plus Depth32F, 20 bytes per
physical pixel (23.44 MiB at 1280×960, about 147 MiB at the large battle size).
It stays resident until shutdown. The live descriptor is now 232 bytes.

The user's flashing/jiggling wreck report exposed a reproducible depth bug.
Integer face-height keys subtracted the current tick's origin from an
interpolated vertex. A sinking model could therefore exchange face ownership
even when its displayed pose was unchanged. The model origin now travels in
unused homogeneous matrix-row metadata with the actual selected pose endpoint,
and the GPU interpolates it alongside position. This adds no upload bytes and
keeps static integer ties exact. Untagged replay/projectile/debris poses keep
their prior fallback. Geometry, normals and quaternions read only XYZ lanes.

A detached diagnostic uses the authored ARM Flash corpse, placed at the opening
commander location. The control and a symmetric half-pixel sinking pair render
the same displayed pose. Before the fix, 124 pixels on the hull changed; after
the fix, every pixel matches. Effects are disabled in this fixture, establishing
a model-depth defect independently of intentional heat shimmer. This does not
prove every reported flicker is gone. Reproduce with `--metal-model-capture
armflash-wreck` and `armflash-wreck-sink` at 1280×960.

Wreck body cooling and plume strength now sample fractional display age using
the production cooling arithmetic; the heat cell phase is added in ticks before
conversion to radians. The plume interpolates the previous/current feature
anchor rather than jumping ahead of a sinking body. Existing production HUD
switches for wreck glow, wreck shimmer, vegetation shimmer and blast rings now
reach the native source builder. Other Enhanced settings still need native
implementations. Wreck light position/colour remain committed-tick samples,
plume bounds remain neutral-model bounds, and overlapping distortions still
sample one immutable world image. Those limitations remain explicit.

### Visual review and allocation work

The opaque commander and activated Solar remain pixel-identical to the prior
corrected build. The cloak diagnostic matches one blend of the opaque model and
its shadow-preserving background within one 8-bit RGB level at all 19,909 covered
pixels. The aircraft-over-tree capture uses the ordinary opening pose relocated
onto a real tall feature, with a detached pass-B selector; it is an ordering
fixture, not a simulated flight. The single-encoder depth reset matches the
separate-pass control capture exactly. Root inspected these captures, the final
battle, the production HUD opening and the wreck diagnostic. Attached cargo
cloak has code review but no dedicated visual fixture yet.

A reviewed delegated change caches prepared sprite sequence lookup by raw bank
and entry, preserving normalization, default-bank behavior, missing art and
negative lookups. It removes repeated key building and a duplicate first-frame
lookup. The profiled 60-second baseline allocated 52.96 million Go objects;
the final fixed-camera run allocated 6.56 million (about 88% fewer). Process-wide
allocated bytes fell from 6.88 GB to 6.12 GB; these include simulation and
publication, not only rendering. Mean publication preparation was 4.47 ms in
the baseline and 3.60 ms in the final run. This is not an isolated cache-only
experiment because composition and wreck fixes also changed.

### Measurements and recommendation

Sequential M3 Pro runs used GOMAXPROCS=2, Town & Country, three 500-unit armies,
seed 7, 1200 lead-in ticks, player 1 visibility, zoom 1.35, 5× face subdivision,
doubled model atlas dimensions, 3456×2234 output, 240 warmup frames and 7200
measured frames. All listed runs held that camera. The production HUD opening
used The Pass, the Modern computer, 2880×1800, 120 warmup and 1800 measured frames.

| Case | Presented Hz | GPU mean / p99 | Presentation p99 / max |
|---|---:|---:|---:|
| Profiled preceding build | 119.27 | 6.27 / 6.99 ms | 8.340 / 95.83 ms |
| First split-encoder composition candidate | 117.61 | 7.68 / 15.96 ms | 16.666 / 125.00 ms |
| Opaque encoder optimization | 118.49 | 6.90 / 13.59 ms | 8.340 / 120.83 ms |
| Final wreck-corrected build | 119.93 | 6.25 / 6.97 ms | 8.340 / 25.00 ms |
| Production HUD opening | 119.14 | 4.18 / 6.64 ms | 8.339 / 54.16 ms |

The final battle delivered 99.958% of intervals within 8.75 ms, no measured
worker deadline misses during ticks 1262–3063, all presentation timestamps,
all GPU commands complete, no native error and no input overflow. Separate
census maxima were 1502 units, 1123 moving, 51 nanoframes, 139 projectiles,
631 effects and 38 burning features. Submission reached 843 model instances,
557305 body triangles, 2254 sprites, 71 airborne models and 39 distortions;
no cloaked subjects occurred in this performance run. Peak Metal allocation
was 854 MiB and ending Go heap 1109 MiB, separately measured. The shared model
atlas remains 32 MiB. The opening was captured before the final one-tick heat
clock alignment; its three static model fixtures are unaffected by that change.

Run `battle-1500-03` was interrupted by camera zoom input (including zoom 0.1),
so its smoother timings are retained as diagnostics but excluded from the
fixed-camera comparison. The following `battle-1500-04` is the final measurement.

Profiling the preceding build showed some long presentation gaps after normal
GPU completion; drawable blocking followed several slots later. Metal memory
was steady at those stalls, and aggregate Go GC pause was about 1.18 ms for the
whole measured baseline. This narrows investigation but does not establish a
compositor/driver cause. The final run is smoother, yet preceding candidates
still stalled and the opening had a 54 ms gap: do not claim the cache or wreck
fix solved pacing, steady 120 Hz, or 144 Hz. A display scheduling trace and
publication lifetime/allocation work are more useful next than speculative
native buffer changes.

Keep using this as the playable experiment with the production HUD and controls.
It demonstrates retained 5× geometry and shared atlases with useful 120 Hz
headroom, while the remaining costs are native resource ownership, macOS-only
bridge maintenance, full-screen cloak scratch and unresolved fidelity/pacing.
Continue manual matches and subject/terrain/effect comparisons before replacing
the shipping renderer. No full victory/defeat match was completed in this
iteration. Both Go commands and the Objective-C bridge built; Metal shader
compilation and runtime captures succeeded. No tests were added or run, and the
prototype remains unmerged.

Binaries, launcher, native source, hashes, commands, captures and reports are in
`/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/metal-composition/`.
Earlier `metal-match` and `metal-motion` packages are preserved.


## Final full-simulation comparison

The prototype is playable with the existing HUD, controls, audio and simulation.
It retains meshes/shared UV textures on the GPU and uses vertex, fragment and
compute shaders for animation and visual passes, through a purego Metal bridge.

Exact renderer parity and steady 120 FPS remain unmet. Keep Modern as the
default and Metal opt-in: retained geometry scales usefully, but baseline
throughput is close to Modern and does not yet justify replacing it.

### Performance
Host draws/second, followed by p95 frame interval in milliseconds:

| Scene / physical viewport | Classic CPU | Modern GPU | Metal |
|---|---:|---:|---:|
| Coastal, 1920x1080 | 54.6 / 26.6 | 101.3 / 15.6 | 103.0 / 15.1 |
| Field, 1920x1080 | 65.0 / 27.7 | 93.7 / 17.7 | 95.5 / 17.0 |
| Field, 3456x2160 | 29.7 / 51.5 | 69.2 / 24.9 | 66.5 / 25.8 |

Metal density stress; both columns use doubled atlas dimensions:

| Scene / physical viewport | 2x geometry | 5x geometry |
|---|---:|---:|
| Coastal, 1920x1080 | 101.4 draws/s | 89.8 draws/s |
| Field, 1920x1080 | 96.5 draws/s | 92.6 draws/s |
| Field, 3456x2160 | not run | 57.4 draws/s |

The large final frame submits 103,954 body triangles, or 519,770 at 5x.
Subdivision adds vertex/index work, not new silhouettes or bones. Doubled
textures use nearest scaling and 4x texel storage, not authored HD art.
Small timing differences are not evidence that more geometry is faster.

Metal GPU spans p50/p95: 8.0/10.6 ms coastal, 7.3/14.9 ms field, and 16.0/21.3
ms in the larger view. These command spans can overlap and cannot be compared
to Ebitengine CPU submission time. The 120/144 Hz budgets are 8.33/6.94 ms;
visible 144 Hz was not tested on this 120 Hz panel. Native presentation p95
was 16.67 ms at 1080p and 25.0 ms in the larger view.

### What was fixed / what remains
Corrections cover clipped sprites, model face ordering including the Solar
base, terrain/fog filtering, wreck identity/pose instability, production effect
ordering and missing visual passes. The final GPU row-extrema outline pass
removes the stray grey construction cage. Matching Modern/Metal captures were
inspected at both resolutions; outline-comparison.png shows the correction.

Remaining differences include fixed-point transform rounding, cache/direct
selection, cached/live pose and key-plane history, fractional rasterization,
some shadow boundaries, destination-palette ALP blending, and optimized small
outline-ring key admission. Deep cargo chains, secondary projectile models
and live terrain gamma/skin reload also need coverage. Not every reported
wreck flicker is proven resolved. Reports keep render_parity_verified=false.

Suppressed effect-marker counters include offscreen body markers without an
admitted slot; they are not a count of missing projectile shadows. Actual
projectile shadows use the stock sprite pass. The aggregate needs finer
classification before using it to claim complete effect coverage.

### Costs and benefits
Reviewed allocation fixes reduced native bytes/draw from 3.93 to 1.74 MiB
coastal and 4.31 to 2.52 MiB field: 56% and 41% reductions, with identical
captures/state in the isolated follow-up. The large view uses 3.35 MiB/draw.
Allocation stays effectively flat as geometry grows, but still exceeds
Modern's corresponding 0.92/0.80/1.32 MiB/draw. Profiles still show significant
clearing and native-call time; native driver waits need better attribution.

The shared model atlas is only 8 MiB, or 32 MiB at doubled dimensions. Total
Metal allocation is about 1,020 MiB at baseline 1080p, 1,818 MiB in the larger
view and 1,959 MiB there at 5x geometry. Composition/shadow atlases, effects
and staging dominate beyond the art atlas. Large-view baseline Go heap after
GC is separately 1,227 MiB; these numbers are not combined process RSS.
Direct shader/resource control is useful, but native resource ownership and
macOS bridge maintenance add substantial complexity.

### Measurement scope
Apple M3 Pro, 18 GB, Go 1.27.1, Ebitengine 2.10.4, GOMAXPROCS=2. Fourteen
sequential runs used one frozen build/settings, seed 7, zoom 1, Modern gameplay,
no mods/restrictions/mutators and active factory construction. Each warms 240
draws, then measures 3,600 draws and 900 full simulation ticks. Coastal has 300
lead-in ticks; field has 1,200 and three 500-unit fixture armies.

Field census: 1,433–1,502 units, 439–1,123 moving, 29–45 nanoframes. In-view
unit anchors rise from 123–172 at 1080p to 729–821 at 3456x2160. This includes
the whole simulation, not merely pose replay. Four draws per tick keep saved
states comparable; slow renderers take longer wall time. The playable host
instead runs simulation in the background. Classic retains its own visual
style; these are backend comparisons, not identical pixel workloads.

All audits passed: settings, surfaces, schedules, authoritative census, four
boundary fingerprints/RNG counts, three full publication pairs and complete
start/end snapshots. This proves the saved scope, not every-draw state or
pixel parity. All native GPU commands completed with no native errors and zero
unsupported outline rings. Four of 28,800 expected measured-frame presentation callbacks
were missing. No tests were added or run; the prototype remains unmerged.

### Recommendation
1. Close the specific visual contracts above using matched pose, cargo,
   construction, waterline and fractional-zoom captures. Keep production HUD,
   input and presentation policy; do not build another frontend.
2. Measure individual Metal passes and native encode/drawable waits, then
   reduce composition/effect bandwidth and allocation/clearing. Large-view
   GPU time already exceeds the 120 Hz budget before other work.
3. Keep retained meshes/atlases and a narrow backend interface. Remove
   duplicate preparation only with proven publication/buffer lifetimes.
   Evaluate genuinely denser authored UV-mapped art next.
4. Repeat full-sim comparisons and finish a complete playable match before
   adoption, portability work or polishing a general graphics library.

### Try it / artifacts
Open Play Metal Match.command: The Pass versus Modern AI, production HUD and
controls, 2560x1600, retail assets from ~/TotalAnnihilation. It reads normal
saved preferences without saving changes. Optional Terminal arguments include
--metal-quads=5 --metal-textures=2. The final opening ran 1,800 measured frames
without native errors, input overflow or missing presentation callbacks; no
victory/defeat match was completed.

Frozen build: 193e663f82aac5d6bfad9d9912cf38cb63ce98a2.
build-final/manifest.json records hashes/build commands. final-batches.json
locates both final matrices, with exact commands/settings, profiles, captures,
state snapshots, validation-audit.json and summary-metrics.json. Repeat a saved
case command with a fresh output path. Earlier near-120 Hz experiments used a
less complete presentation path and are not the final comparison above.

Artifact directory: `/Users/daniel/.codex/visualizations/2026/10/07/01a113ae-9262-76f0-a2ca-d4dbd037c497/renderer-comparison`.

## Depth-slab experiment and per-pass timing (2026-10-07, branch `metal-depth-slabs`)

Question: can ordinary depth testing reproduce the production painter
composition, without per-subject atlas images, and is that composition what
keeps Metal near Modern's speed?

**Encoding.** Each subject's depth is `(bands - paintIndex - 1 + local) / bands`,
where `local` is its staged height-key depth (`(256-key)/257`, or 0.5 when
keyless). A later paint rank is always nearer, so it covers every earlier
subject regardless of local depth, as the row painter does; within one subject
the key still orders faces, with equal keys resolved by draw order (LessEqual).
`NANOLATHE_METAL_DEPTH_SLABS=1` draws every opaque, single-member subject
outside the effects phase, with no construction reveal/outline and no water
reflection/refraction source, straight into the world with the unchanged staged
face mapping (function constant 0 specializes `model_vertex`). The remaining
subjects keep the atlas path. Commit, shadow and sprite quads keep paint order
and are depth tested inside their own band through a zero-height viewport
depth range. On the frozen coastal frame the result has no solid difference
from the atlas render (after a 2-pixel erosion, nothing differs by more than 32).

**Diagnostics added.** `NANOLATHE_METAL_PASS_TIMING=<csv>` records
stage-boundary GPU timestamps for all 45 encoder sites (labelled for GPU
captures); `NANOLATHE_BENCH_FREEZE=1` stops the simulation after warmup so
measured draws contain presentation work only, for all three renderers;
`NANOLATHE_METAL_ABLATE=effects,glow,water` skips families for whole-frame A/B.
Tile-GPU passes overlap, so per-pass spans are attribution hints; decisions use
whole-frame spans and ablations.

**Other changes found by the timers.** Projectile commits and ground marks each
closed the stock-effect encoder and opened a world pass: 116 passes per coastal
frame. Effects now share one world-format encoder per layer (36 passes,
pixel-identical), the largest single GPU win. The reflection face budget ran
on one GPU thread (1.6 ms span) but overlapped other work; a parallel pre-pass
with the exact serial fallback removed it without changing whole-frame time.
The HUD draws inside the final drawable pass.

**Results** (M3 Pro, GOMAXPROCS=2, 120 Hz requested; frozen = 480 draws,
renderer only; full = 3,600 draws with the synchronous simulation every fourth
draw; host load 3–7 from other agents during the full runs). Draws/s, with
Metal GPU span p50 for frozen runs:

| Scene | Modern | Metal atlas (193e663f) | Metal slab + fixes |
|---|---:|---:|---:|
| Coastal 1080p frozen | 110.4 | 117.5 (9.32 ms) | 119.9 (6.31 ms) |
| Field 1080p frozen | 117.0 | 119.8 (5.42 ms) | 119.7 (5.36 ms) |
| Field 3456×2160 frozen | 80.0 | 89.6 (14.00 ms) | 104.2 (11.85 ms) |
| Coastal 1080p full | 101.0 | 96.4 | 101.9 |
| Field 1080p full | 92.9 | 92.9 | 94.4 |
| Field 3456×2160 full | 69.4 | 71.3 | 73.6 |

Renderer-only, the slab build is 30% faster than Modern in the large view and
holds the 120 Hz cap at 1080p. With the simulation on the draw thread all three
stay within a few percent: Metal's Go preparation (`Record` p50 2.8–4.4 ms
against Modern's pre-recorded 0.5–0.9 ms) and native encoding (1.1–3.8 ms) are
the bottleneck. Full-simulation GPU spans are not comparable between builds,
because an idle GPU lowers its clock.

**Fidelity gaps found.** (1) Slab bodies shade once per pixel: edges are aliased
and interiors show texel speckle that the 2×2-resolved atlas, like Modern's
supersampling, filters away (`edges-4x.png`). Parity needs supersampled
shading for slab bodies (4× MSAA with per-sample shading, or four averaged
sub-samples in the fragment), not only edge AA. (2) Both Metal paths draw tree
sprites and some units lighter than Modern while terrain matches exactly
(`ratio-map.png`), and explosion ground light is smaller or weaker.

**Next.** Move Metal's per-draw Go preparation to per-tick or background
pre-recording (effect list cloning, publication rebuild, composer/shadow
preparation); reduce the full-screen post chain (resolve, full-frame
distortion copy, fog; ~2.8 ms at 3456×2160), the ground light field (~1.7 ms)
and glow (~1 ms); then add supersampled slab shading and settle the sprite
brightness gap with matched captures. Artifacts, tools and captures:
`~/nanolathe-bench/metal-depth-slabs-2026-10-07/` (`matrix.sh`, `summarize.py`,
`timeline.py`, `summary.txt`). No tests were added or run; the prototype
remains unmerged.
