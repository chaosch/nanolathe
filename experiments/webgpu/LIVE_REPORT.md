# Windowed real-battle prototype — 2026-10-11

**Verdict: ready for a windowed macOS play test; retain the shipping Modern
renderer.** The isolated GoGPU/WGSL prototype runs a real Great Divide skirmish
against the existing Modern AI with working selection, orders and construction.
It renders terrain, features, animated lit 3D units, shadows, stock effects,
terrain illumination, blurred glow, fog and the production foreground. This is
useful integration evidence, not Modern pixel parity or production acceptance.
Dense-combat host cadence is 71–73 draws/s, versus 51–53 Classic and 90–91 Modern;
allocation and native call overhead still need substantial work.

## Play in one command

```sh
/Users/daniel/.codex/worktrees/webgpu-p0-p4/nanolathe/experiments/webgpu/tools/play
```

The first invocation builds an external cached application bundle; later runs
reuse it. Requires this Mac's Go toolchain and the existing retail install at
`~/TotalAnnihilation`. It opens an Arm human versus Core Modern computer on
Great Divide, Modern gameplay, no mod, with a 1920×1080 physical surface and
120-draw/s target. The game is unbounded until closed. It prints a fresh external
report directory and writes timing/resource reports on graceful exit.

Left-click the commander to select it; left-click ground to move it. Right-click
cancels selection. Ctrl+C selects/follows the commander; arrow keys move the
camera. Click a build icon, then a valid location, to build. These were exercised
through real native keyboard/mouse events. A blocked placement was red; a valid
Solar Collector completed, unfolded and raised energy income from +25 to +45.
Use the window's close button to exit and join the simulation/AI cleanly.
**Avoid the green fullscreen button: the pinned dependency hangs during its
fullscreen resize callback.** The prototype deliberately starts windowed.

Extra options are appended to the ordinary command, for example another
`--root`, `--map` or `--seed`. A bounded capture is:

```sh
experiments/webgpu/tools/play --webgpu-frames 3600
```

That adds an untimed final PNG to the automatic report. Indefinite play writes
reports on close but does not promise a final PNG. Output directories and build
artifacts stay outside the repository. The host is currently macOS-only; the
browser authored-fixture harness remains separate from real-battle play.
Menus, save/load, relative pointer capture, double-click counts, minimap
navigation and arbitrary display/DPI transitions are not qualified by this test.
The minimap/HUD draw through the original foreground source; a largely dark
minimap during initial exploration is not proof of a renderer defect.

## Scope and reproducibility

Worktree: `/Users/daniel/.codex/worktrees/webgpu-p0-p4/nanolathe`, branch
`webgpu-p0-p4`. Started from plan commit `afe0c4254` on
`webgpu-implementation-plan`; integrated current main
`1430571dcb5d475ad52dd2044d9393f884343aeb` before implementation.
Nothing is merged to main. Production Go sources, root module files and defaults
are unchanged. The later overnight request authorized this live experiment
after P0–P4; it does not start production M1–M5 or pass their gates.

Exact GoGPU commit, also pinned in plan §3:
`12620c9d50073b8591f075c97ba0c6f3b6ad92f7`, canonical pseudo-version
`v0.54.1-0.20260901095536-12620c9d5007`. No tagged-release substitute or dependency
source patch is adopted. The experiment's nested module owns its dependencies.

`tools/build-play NEW_EXTERNAL_DIRECTORY` writes a child module, explicit local
replacements, an overlay and build manifest outside the tree. The overlay
reuses the unchanged production command battle shell, retained meshscene source,
HUD, input adapter, audio, Modern AI and benchmark fixture. It redirects the
private native renderer import to the experiment and adds experimental selectors.
The application borrows GoGPU's device/queue/encoder/surface; it does not add a
second simulation, gameplay policy or renderer selection to production.

The combined host requires **CGO_ENABLED=1**. Its CGO-disabled path stops at a
GoFFI/purego duplicate `_cgo_init` link failure. The independent CGO-enabled
path builds and runs. The small P0–P4 harness remains buildable without cgo.
Build manifests record overlay hashes, full module graph, source revision and
binary SHA-256. Files authored here use existing MIT implementation contracts;
no executable analysis or retail asset bytes are committed.

## What was implemented and observed

The GPU computes reflected rigid piece poses from the retained frame pair.
Models use existing mesh/material/palette/SHD data, modern finish/glint and light
inputs. Private body/shadow targets compose in the original feature/model paint
stream. Feature sprites use the original source identities; selection boxes,
health/nanoframe annotations and foreground lists come from production sources.
Terrain uses the retained tile/detail lookup. Stock effect operations retain
barrier layer and blend order where supported. A separate half-resolution
terrain-light field resolves before objects; source glow shrinks and blurs into
near/far octaves and screens into the final world before fog and HUD.

Native verification established actual pixels, beyond compilation/draw counts:

| Evidence | Observation |
| --- | --- |
| Great Divide interactive session | 387.318 s, 45,901 draws, 31 native events, no event overflow; completed Solar Collector; AI grew to 26 units, human had two; final tick 11,617 |
| Later one-command bounded launch | 3,720 draws including 120 warmup, 32.003 s; tick 953; four AI units and one human; actual final terrain/features/commander/fog/HUD PNG inspected |
| Fixed real coastal combat | 420 draws including 240 warmup; tick 405; 336 units, 222 moving, 101 projectiles, 631 effects, four burning features; units/ships/planes, beams/explosions/smoke and shadows inspected |
| Native terrain-light readback | 57,346 pixels with nonzero RGB, maximum byte 130, in its owned 1024² allocation |
| Native source-glow readback | 215,954 nonzero RGB channels, maximum half-float 4.69140625, at 1920×1080 |
| Completed near/far glow readback | 61,107 nonzero RGB channels, maximum 1, in the 723×272 bordered octave atlas |

The interactive session precedes the final light/glow additions; its input and
host paths are unchanged. Subsequent combat and bounded launch qualify the
later rendering additions separately. Light readbacks are untimed **final-frame**
GPU copies. The large debug JSON describes the **first** source packet; it was
not used as a matching source-to-final-field oracle. Nonzero field pixels prove
active GPU work, not exact lighting parity.

Two native shader problems were found through missing pixels/failed real combat:

- The pinned Naga Metal output scaled only a conditional corner fallback,
  collapsing the model resolve quad to one pixel. Loading the corner separately
  fixes the emitted expression; a compiler-output regression check and real
  model pose/color/shadow readbacks verify the correction.
- A combined glow module emitted unused storage-buffer metadata into its Metal
  compute entry and collided with the uniform buffer binding. The first combat
  run failed with zero draws. Separate source/blur modules, with checked binding
  layouts, fix the experiment without altering the dependency.

The failed combat log is retained. Neither failure was described as successful
qualification before the independent corrected path produced pixels.

## Fidelity boundaries

The matching coastal PNGs show the same battle/camera/subjects. Modern has a
stronger glow halo and different water/reflection/effect treatment. The prototype
has the following explicit limits:

- Hardware triangle coverage approximates production integer spans. Cached/live
  grouped solo chronology and erased holes remain approximated at
  `models.go`'s `TODO(question)`; overlapping cargo/units need an independently
  checked painter contract before claiming parity.
- Projectile body/marker barriers are approximate; aircraft shadow silhouettes
  and penumbra differ. Selected annotations and original feature paint identities
  work, but do not settle every overlapping painter case.
- Water surface/reflections, enhanced distortion, final depth-band smoothing and
  normalized HDR effect accumulation are omitted. The world target is RGBA8.
  Missing-operation counts remain visible in every frame report.
- Native straight RGBA alpha approximates destination-palette ALP; smoke does
  not yet receive scattering. The retained source uses its existing strongest
  light selection rather than all shipping light-family reserves/nano clusters.
- The ground-light target clears its full owned extent instead of production's
  packed partial-clear storage. World preparation is synchronous and each frame
  waits for completion before mutable resource replacement. This simplifies
  ownership but costs cadence and differs from shipping preparation overlap.

During measured coastal combat, peak explicit omissions were 30 distortion
operations, 78 projectile marker-order operations, 112 normalized accumulation
operations and one water/reflection pass. Successful terrain light and glow are
no longer reported as missing. The comparison measures the implemented subset,
so it cannot establish the cost of a fully equivalent renderer.

## Matched cadence and resource measurements

Apple M3 Pro, 12 CPU / 18 GPU cores, 18 GiB; macOS 26.6.2; Go 1.27.1;
GOMAXPROCS=2; 1920×1080 **physical** surface, DPR=2, VSync and 120-draw/s target.
Each run measures 720 draws after 240 warmup draws. CPU profiles are enabled in
all compared runs. Runs were sequential with no concurrent build/test benchmark.
The baseline binary's production source matches integrated main above; its VCS
stamp includes the early documentation-only worktree commit. Native measurements
before the small pipeline optimization use `1160644ee`; final renderer
measurements use `ab499fc92`.

Coastal scene v5: Expanded Confluence, seed 7, 160 mobiles and 20 buildings per
side, ordinary factory orders, normal visibility, 300 pre-window ticks, zoom 1.
Final tick 540 has 320 units, 211 moving, 105 projectiles, 767 effects and four
burning features. The first two rounds alternate
Classic → Modern → WebGPU → Modern → WebGPU → Classic. Two final WebGPU runs
measure the pipeline correction afterward.

Field scene v1: Town & Country, seed 7, three 250-unit **Classic** computer armies,
normal visibility, 1,200 pre-window ticks. Final tick 1,440 has 737 units, 346
moving, 13 projectiles and 156 effects. This deterministic benchmark is separate
from the interactive Modern-AI play test.

Every `benchmark-state.json` is byte-identical within each scene, including
fixture/pre-window/warmup/end frame hashes, both RNG draw counts and the existing
partial state fingerprint. This is strong matching-workload evidence, not a
claim that the partial fingerprint covers every simulation field.

| Scene / executor | Host draws/s | Cadence p95 / p99, ms | Go allocation, MB/draw | Go mallocs/draw |
| --- | ---: | ---: | ---: | ---: |
| Coastal Classic, two runs | 51.32 / 52.75 | 28.67 / 30.59; 27.79 / 29.51 | 0.311 | 1,908 |
| Coastal Modern, two runs | 91.22 / 89.50 | 16.64 / 19.08; 16.73 / 19.75 | 0.930 / 0.960 | 24,969 / 24,978 |
| Coastal WebGPU before correction | 53.80 / 53.61 | 21.93 / 28.69; 21.99 / 28.77 | 16.544 | 394,865 |
| Coastal WebGPU final | **71.34 / 73.10** | 17.53 / 22.22; 16.99 / 21.20 | **11.293** | **222,653** |
| Field Classic | 94.48 | 16.62 / 18.82 | 0.264 | 1,471 |
| Field Modern | 111.90 | 10.39 / 14.21 | 0.506 | 13,213 |
| Field WebGPU before correction | 83.27 | 15.86 / 21.46 | 8.582 | 212,457 |
| Field WebGPU final | **100.90** | 15.41 / 21.05 | **5.795** | **121,200** |

Draws/s is `1000 / mean(valid measured callback cadence ms)`; quantiles select
`floor(p*(n-1))` from sorted valid samples. MB means 1,000,000 bytes. Cadence is
host callback spacing, **not scanout or GPU time**. Public native GPU,
queue-submission and presentation timestamps remain unavailable. Historical
benchmark `Submit` is encode-only for WebGPU and cannot be compared as equal GPU
work with the other executors. In the final coastal run, mean source recording
is 2.73 ms, encoding 7.61 ms, previous-frame completion wait 2.03 ms; these are
CPU observations. No executor here establishes a 120 Hz acceptance pass.

The correction caches pipeline identity **only inside one DrawEffects barrier**;
intervening painters still own state. It changes no draws, order or shaders.
All four coastal WebGPU final PNG pixel arrays have SHA-256
`3fb57123922eaab55382c1ae59f1c72810fa97eb288f8aabb425dc2db956c923`.
Field before/after pixel arrays also match exactly.

Process maximum RSS (including asset load, runtime and native allocations) was
0.660–0.700 GiB Classic, 1.013–1.083 GiB Modern and 1.428 GiB in the first final
WebGPU coastal run. Field RSS was 0.914 / 1.017 / 2.123 GiB respectively. macOS
peak memory footprint was 0.965 / 1.711 / 2.095 GiB coastal and
1.293 / 1.884 / 3.288 GiB field. These are process high-water observations, not
exact GPU allocation. Final coastal WebGPU allocated 8.13 GB and 160.31 million
Go objects over 720 measured draws, with 12 GCs and 1.54 ms total stop-the-world
pause; field allocated 4.17 GB / 87.26 million objects, three GCs, 0.47 ms pause.
Low GC pause does not remove allocator/FFI CPU cost.

The pre-correction coastal CPU profile attributes 62.47% cumulatively to
Renderer.Encode and 67.99% to Metal MsgSend paths; SetPipeline alone is 26.83%
cumulative. These overlapping call stacks must not be summed. Allocation
accounts for 19.15% cumulatively. This motivated the bounded pipeline correction;
more draw-call/binding/allocation work remains. The already-cached official
Google pprof tool read the profiles externally because this Go toolchain has no
`go tool pprof`; no repository dependency was added.

Live peak measured counters include 411 submitted model instances / 3,811 pieces /
53,265 triangles, 1 model compute dispatch, up to 5,589 layer draws, 1 glow draw +
3 blur dispatches and 1 ground-light draw. They explain native-call cost, not
pixel correctness. The original authored-fixture lifecycle separately returned
32 tracked resources / 26,724,848 estimated bytes to zero across ten real native
and ten browser cycles. A six-minute live close and short matched runs do not
prove a long-match native-memory plateau; that remains an acceptance task.

## Gates, review and evidence

The P0–P4 [frozen report](README.md) preserves its original result: exact native
and Chrome fixture pose/pixel/depth/identity checks and ten resource cycles pass;
Rust's required artifact fails at ABI/mapping, native fullscreen hangs, native
loss remains unverified, browser fullscreen/hide/DPI/first-input qualification
is incomplete. Their dependent performance matrices remain stopped. The
contextual Metal-only runs are not a WebGPU performance ranking.

The windowed live path above is independently authorized and measured. It does
not turn those failed dependencies into successful P3/P4 qualification.
Pinned GoGPU logs deferred encoder Finish and queue Submit errors instead of
returning them to the host. Allocation/map/file errors do propagate, but a saved
PNG/report alone cannot prove command-buffer success. Qualified captures above
require clean native logs and inspected nonzero/expected GPU pixels. This API
limitation is also included in runtime reports.

`experiments/webgpu/tools/check` passes tests/vet, native Go/Rust-tag builds,
js/wasm build and mock browser bookkeeping. Affected CGO-enabled live package
checks and the real overlay build pass. These are distinct from Rust runtime
qualification. Independent static reviews checked painter/projection arithmetic,
SHD lookup, growth/cleanup ownership, light/glow arithmetic and bindings, and
final local pipeline state lifetime. The two final evidence issues (deferred
errors and first/final debug packet distinction) are disclosed above; reviewers
did not claim runtime success from static inspection.

**Both required root scripts, `tools/check` and `tools/check-retail`, fail** at
`internal/architecture.TestNoUnreferencedExported`: its source walk crosses the
nested module and reports stale allowlist entries for `meshscene.ModelPhaseAir`,
`ModelPhaseGround`, `ModelPhaseShortFeatures` and `PhaseGround`. Other package
checks in those runs pass; retail assets are present. Plan §4 explicitly says to
keep those guards enabled and report a nested-module false positive. No guard,
allowlist, root dependency or production export was changed to force green.
A separate narrowly reviewed module-boundary scan fix, with a nested-module
regression fixture, would settle this gate. Landing remains blocked.

Raw reports, captures, profiles, command/build manifests and gate logs are under
`/private/tmp/nanolathe-webgpu-live-evidence/`; the manifest hashes each included
file and records revisions. Full originals remain in the `/private/tmp/nanolathe-webgpu-*`
run directories. The private evidence bundle excludes the first-frame raw retail
art/mesh JSON and binary debug arrays. Its captures are rendered observations,
not authored test fixtures. The earlier P0–P4 bundle remains indexed separately.

## Recommendation

Keep this branch for play testing. Retain Modern as the shipping renderer and
resolve the native-call/allocation budget before extending the backend. Prefer
batching compatible draws/bindings while preserving painter barriers; measure
profile and pixel/state equivalence after each bounded change. Reconsider
whether the pinned native Go HAL can meet the draw-call workload before paying
for full water/HDR/group coverage. The single local correction helped, but
11.3 MB / 223k allocations per dense frame and weaker p99 cadence remain material.

Then qualify the upstream fullscreen/error/loss paths at an explicitly reviewed
pin, finish real browser interaction and Windows/Linux runtime qualification,
and address the visible water/distortion/HDR/group/coverage gaps with exact
sources and captures. Repeat long-match resource and matching performance gates
only after those changes. Do not adopt, merge or start production migration on
the strength of this play test.
