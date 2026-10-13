# Isolated WebGPU evaluation

Frozen P0–P4 result, per [the plan](../../docs/PLAN_WEBGPU_RENDERER.md). No production
renderer, defaults or root module changes. Use `GOWORK=off` throughout.

The current parity/allocation follow-up is documented in
[FOLLOWUP_REPORT.md](FOLLOWUP_REPORT.md). The original overnight result below is
historical; its cgo restriction and performance/fidelity snapshot are superseded.

The later user-authorized overnight real-battle experiment is described in
[LIVE_REPORT.md](LIVE_REPORT.md), including its one-command play test, matched
measurements, fidelity gaps and failed gates. It preserves the P0–P4 dependency
verdict below and does not start the production migration.

The later opt-in interactive ordering seam and explicit engine-archive build are
documented in [INTERACTIVE_LATE_ACQUISITION.md](INTERACTIVE_LATE_ACQUISITION.md);
its deferred-host patch is superseded by stock late acquisition (below).

## Stock dependencies — 2026-10-12

The prototype now builds and plays on the stock pinned GoGPU and wgpu; no
dependency fork is needed. These renderer changes replaced the owned-submission
and deferred-acquire patches:

- **Staged texture rewrites** (`internal/battlegpu/uploads.go`). Stock
  `Queue.WriteTexture` writes a host-visible texture at once, and every
  single-sampled texture on an Apple GPU is shared memory, so rewriting an atlas
  could race a frame in flight. Rewrites of textures earlier frames sampled
  (fog grid, HUD overlay and effect atlases, the standalone glow atlas) are
  packed into one reused arena with 256-byte rows, uploaded with one chunked
  `Queue.WriteBuffer` (always staged and ordered with GPU work) and copied by
  `CopyBufferToTexture` on the frame's encoder before any pass. A texture
  created the same frame is still written directly. Rectangles stay pending
  until an encoder records them, so a frame that loses its drawable never
  leaves a consumed atlas version unwritten.
- **No per-frame completion wait.** wgpu v0.34.2 defers releasing textures,
  views and shader modules to the last submission index and keeps buffers, bind
  groups and pipelines alive by reference until their submissions complete, so
  replaced atlases and buffers outlive the frames that used them. Resize,
  capture and teardown keep their `WaitIdle`.
- **Chunked model uploads.** Large mutable model writes are split at the
  staging belt's 256 KiB chunk size; stock wgpu otherwise allocates a one-off
  staging buffer for each larger write.
- **Late acquisition on the stock host.** GoGPU acquires the drawable lazily on
  the first `CommandEncoder()`/`SurfaceView()` call. With
  `NANOLATHE_WEBGPU_LATE_ACQUIRE=1` the driver requests them only inside the
  acquisition callback, after preparation and the source handoff. An
  unavailable drawable leaves the prepared frame unshown (its source is never
  reread; its staged copies stay pending). GoGPU replays a callback once when a
  present finds the surface outdated; the driver detects that from GoGPU's frame
  statistics and re-presents the last prepared frame with `Renderer.Redraw`,
  consuming no source or input and keeping the pacing deadline.

What remains patched, why, and the stock-versus-patched measurements are in
[patches/README.md](patches/README.md). In short: the browser build's write
staging is required; the Metal pointer cache (`--metal-cache`) is an optional
macOS arm64 speed-up worth 2–3.5 Hz at 1440p, with which stock matches the
former patched build; the presented-handler and GPU-timing patches are
measurement-only.

```sh
# Shipping-shaped macOS build (stock pins plus the optional pointer cache):
GOWORK=off experiments/webgpu/tools/build-play NEW_DIR --metal-cache
# Add --present-handler --gpu-timing only to measure presentation and GPU time.
NEW_DIR/run-play --fast -- --root="$HOME/TotalAnnihilation" --map='Town & Country'
```

`run-play --fast` selects prepare-ahead, storage reservation, late acquisition
and GOMAXPROCS between 4 and 8 on every native target; `--no-late-acquire`
selects the early-acquisition control.

## Dependency qualification — 2026-10-10

Exact GoGPU source: `12620c9d50073b8591f075c97ba0c6f3b6ad92f7`, canonical
`v0.54.1-0.20260901095536-12620c9d5007`. The tagged `v0.54.0` is not used.
The downloaded pin has the RAF/visibility loop and borrowed context encoder
specified in plan §3. Device creation requests no optional features.

Initial builds: Darwin arm64 Go/Rust and js/wasm pass with `CGO_ENABLED=0`.
Windows/Linux amd64 Go and Rust-tag cross-builds pass; neither has hardware runtime evidence.
Native Go on Apple M3 Pro and Chrome 153/macOS 26.6.2 run the identical
64-invocation compute shader (four exact integer readbacks) and textured indexed
triangle. Controlled invalid WGSL and undersized binding return native errors;
browser compilation info and asynchronous error scopes expose both errors.
The browser bridge uses the actual device's `onSubmittedWorkDone`, never the
binding's no-op browser `WaitIdle` or its always-true `Poll` as completion.
Browser device.destroy reports loss and stops submission. Native Metal loss
injection/reporting is **unverified**: the pin's public device provider exposes
no Metal loss callback. Context.CheckDeviceHealth is a no-op for that backend.
This unresolved functional gate must be carried into the verdict.

Rust runtime **fails** at the required v29.0.0.0 artifact: bind-group layout
wire data omits the public C header's binding-array-size field and aborts the
process. A temporary, bounded ABI correction gets past that operation, then
buffer mapping aborts in wgpu-native's unimplemented GetMapState entry point.
The correction is retained as diagnostic evidence, not an adopted fork. Stop
this dependency path; no Rust performance/lifecycle claim follows from a build.

Initial artifacts: `/private/tmp/nanolathe-webgpu-20261010/` (logs, browser
report and reviewed triangle capture). These are qualification runs, not P4
performance results. Browser telemetry is local; Download report uses a Blob.

## Frozen small API and layout contract

Fixture: immutable `meshscene.Scene` subset, three meshes/material identities,
three articulated pieces, authored RGBA texture; pure sample generation from
version/seed/count/detail/animation and integer 30 Hz tick. No sessions/assets.
Snapshots own their instance and endpoint slices through encode; input is never
modified. Column-major pose matrices separate world-Z reflection from rotation.
Shade lane 3 follows current; lane 7 blends tagged origin; lane 11 is both-tagged;
lane 15 suppresses a piece if either endpoint is hidden. Quaternion shortest-path
slerp and linear translation preserve exact endpoints and equal bases.

Renderer: `New(device, queue, format, scene)` borrows device/queue, uploads static
resources and creates pipelines. `Encode(encoder, view, width, height, camera,
alpha, snapshot)` borrows the encoder/view, returns counters/error and never
finishes/submits. Generation controls endpoint/instance uploads. `Resize` changes
only extent targets; zero extent suspends. `Close` is idempotent and follows
real completion established by host. No window/session/device ownership.

Packing: 48-byte storage vertex (position vec4 at 0, UV/piece/material vec4 at
16, color vec4 at 32); 64-byte column-major pose; 16-byte instance (pose base,
mesh, ID, reserved); 256-byte uniform binding with documented lanes. No WGSL
storage vec3 ABI. One shared WGSL set; ≤4 bind groups/≤8 storage buffers per stage.
Filterable RGBA8Unorm, straight opaque diagnostic colors, top-left texture rows,
CCW/no cull, WebGPU clip depth 0–1 and Less depth compare. No optional features,
render bundles, LOD or culling. Depth and MRT/composite/overlay are P2 work.

Measurement: separate CPU, GPU, queue-completion, callback and actual-display
observations, with null/reason when unavailable. Neither RAF nor queue completion
is scanout. No GPU ownership or simulation input in report collection.

## Commands implemented at P0

From this directory:

```sh
tools/check
tools/build /tmp/webgpu-build-new
tools/serve /tmp/webgpu-build-new
GOMAXPROCS=2 /tmp/webgpu-build-new/probe-go -diagnostics
WGPU_NATIVE_PATH=/absolute/path/libwgpu_native.dylib /tmp/webgpu-build-new/probe-rust
```

Open localhost port 8766 in Chrome; Run smoke, Resize, Invalid shader/binding,
Stop and Destroy device exercise the authored harness. `-frames=120` is a bounded
native smoke; `-frames=0` keeps it open. Runtime tests are separate from check.
The Rust command is a reproduction of the documented failure, not a passing path.

## P1 fixture/oracle — 2026-10-10

`fixture.New` accepts only 64/1000/3000/5000 instances, 200/500 triangles and
animated/static/camera modes. Every mesh has exactly the selected triangle
count across three pieces. Geometry includes subdivided top surfaces, true
bevel faces, sides, bottoms and two independently authored nose triangles.
UVs have face seams and one mesh mirrors U. Material identities are 1–3.
The 64-instance scene intentionally overlaps subjects 1 and 2; benchmark grids
have no inter-model occlusion. Every benchmark instance's full envelope fits
its cell. Body radius ≤sqrt(.642²+.6²), height .4, scale .44 cell, translation
±.015 cell and centering .16 scale bound the body envelope below half a cell;
articulated pieces remain inside that envelope. The smallest projected body
span exceeds half a cell. CPU tests sweep all submitted instances at four
animation phases; GPU visibility IDs still must establish actual coverage.

The sheared camera's world width is 16, height is 16×physical-height/width,
zoom is physical-width/16, with a 32-physical-pixel margin. Columns use the
plan's floating-point aspect ratio; rows use integer ceiling. Scale is baked
into immutable vertices and local piece translations. Pose bases are rigid
unit bases with one final world-Z reflection; interpolation cannot discard a
model scale accidentally. Sample generation is pure and allocating; its real
CPU/allocation cost must be measured without hiding it behind GPU time.

The independent float64 oracle locks exact endpoints, either-hidden admission,
metadata, equal bases, near-identical rotations and the 170°→−170° short arc.
Tests freeze primary workload mesh/texture/configuration/endpoints hashes.
Before GPU implementation, static diagnostic pixels (100,100), (116,105),
(80,120), (115,145), (280,115), (460,100) have frozen ID/RGBA/depth expectations.
These test atlas quadrants, opaque background, mirrored mesh placement and
world projection; further animated interior samples use independent barycentric
projection. GPU pose tolerance starts at absolute 1e-4.

Adding the existing root scene types selects x/sys **v0.48.0** by MVS, with a
local root replace. Root go.mod/go.sum remain unchanged; the final graph is
recorded separately from the initial P0 graph.

## P2 retained renderer (2026-10-10)

The single embedded WGSL set implements 64-invocation rigid-pose compute, three indexed instanced mesh batches, nearest atlas/material shading, Depth32Float, two RGBA8 attachments, composite and overlay. Storage vertices are 48 bytes, poses 64, instances 16; uniforms have a 256-byte allocation with a 64-byte shader structure. Explicit groups fit the default core budget; no optional feature is requested. Encode borrows the host encoder/view and never finishes or submits it. Constructor static uploads and extent targets are separate; Close is idempotent and the host establishes completion first, including on partial construction failure.

Native Go and Chrome 153 on this M3 Pro passed alpha 0/.25/.5/.75/1 readback at 64/1k/3k/5k and physical 1920x1080. The diagnostic adds equal bases, near-equal rotations, the 170-to-minus-170 short arc, either-endpoint hidden suppression, current shade, interpolated origin and both-tagged metadata. Maximum observed matrix error is below 1e-6 against the independently computed oracle (threshold 1e-4). Every large-workload identity has at least 110 pixels in these captures (required 16). The 64 fixture also checks more than 220 exact interior nearest colors and depth samples at each alpha.

Coplanar intentionally overlapping faces have no stable exact color contract at float32 depth precision. The independent oracle identifies different-instance depth separations below 2e-7 and excludes those samples from exact color checks, preserving all unambiguous tests. This corrects a diagnostic-conditioning defect, not a shader or tolerance relaxation. The source contains a regression case and independently reviewed mirror samples. Native diagnostic copies require the public explicit texture-transition API; browser transitions are implicit. Depth-only attachments set stencil read-only so the pinned browser translation omits invalid stencil operations. The surface must be acquired before reading its final physical dimensions, because GoGPU updates browser DPR during lazy acquisition.

Evidence: `/private/tmp/nanolathe-webgpu-20261010/qualify-go-{64,1000,3000,5000}.log` and `qualify-browser-{64,1000,3000,5000}.json`. Rust remains dependency-blocked; these results do not qualify it. Lifecycle and cadence remain P3/P4 gates.

P2 review corrections: diagnostic readback now includes an untimed surface-format target through the same composite/overlay pipelines, exact background and cyan-overlay probes, and all unambiguous final interior colors. The extra diagnostic render is excluded from timing. Generation counters record one 400-byte-per-instance endpoint/instance upload followed by uniform-only 256-byte frames, with zero persistent creations. The resource tracker counts actual retain operations rather than assuming zero. Both hidden-endpoint directions and both tagging directions are exercised, and 170-to-minus-170 crosses the trigonometric slerp threshold. Native and Chrome alpha-.5 PNGs were visually reviewed: full grid, articulated tops/barrels, rotated/beveled textured boxes, first-cell overlap and cyan overlay are present; coplanar overlap and raster-edge differences remain explicitly outside exact-color comparisons. Captures are under `captures-go/` and `captures-browser/` beside the qualification logs.

## P3 lifecycle (2026-10-10)

Native Go and Chrome each completed ten 3k/200-triangle scene cycles at physical
1920×1080. Real queue completion precedes Close. Each cycle returns the tracked
32 objects / 26,724,848 bytes to zero. Native process RSS rose from 111.75 MB
to 117.29 MB, Go allocated heap from about 3.63 to 3.72 MB; browser allocated Go heap
from 3.175 to 3.187 MB. HeapAlloc includes uncollected garbage: the old artifact field heap_live_bytes is mislabeled. These short cycles do not prove a driver-memory plateau.
Browser dedicated RSS is unavailable. Counts estimate renderer allocations,
not device-wide memory. The browser queue observer measured at most two
outstanding submits and zero completion handles after shutdown. Native fencing
waits on the next callback after submission; browser fencing uses a microtask
and the real onSubmittedWorkDone promise. Neither measures display time.

Native resize to physical 1600×900 and focus loss/return passed with a reviewed
capture. Fullscreen **failed**: at the exact GoGPU pin, the main-thread window
method holds its mutex across the native fullscreen call. A synchronous resize
delegate re-enters the same mutex and hangs. The preserved QUIT stack and
pinned source establish this dependency failure. No source patch was adopted.
The native P3 path stops here: DPI, minimize/restore and native loss remain
unverified, and dependent native Go P4 timing is not run. Controls are serviced
on the main-thread event source; an earlier render-callback UI misuse was fixed
before this reproduction and is not attributed to the dependency.

Browser resize visibly changed the canvas and rendering continued. The saved
image named browser-fullscreen.png is **not** fullscreen evidence: it still
shows the document chrome and small canvas. During subsequent hide/resume and
zoom/stop checks, browser control became unavailable; reconnecting the owned
tab also failed. Do not infer a GPU/browser deadlock from an automation failure.
Fullscreen round trip, hide/resume, DPI and first-reflected-input timing for
that interactive run remain unverified; no final report was recovered. Thus
the browser P3 gate is incomplete and dependent P4 matrix timing stops too.
P0 device.destroy/stop and fresh-session recreation, P2 captures and the
separate ten-cycle completion report remain valid independent evidence.

Artifacts: lifecycle-go.log, lifecycle-browser.json, lifecycle-ui-go.log,
native-resize.png, browser-resize.png and browser-fullscreen.png under
/private/tmp/nanolathe-webgpu-20261010. The screenshots were inspected; fixed
fixture placement is cropped during resize rather than silently refitted.
Interactive native: prebuilt probe-go -workload -frames=0 (R resize, F fullscreen,
M minimize, Q close). Cycle checks: probe-go -workload -count=3000 -cycles=10
-frames=0; browser ?workload=true&count=3000&cycles=10.

## P4 result and verdict — 2026-10-10

**Promising with limits. No native backend is accepted for production.** Keep
native Go as the candidate for a bounded qualification follow-up; Rust FFI is
not a viable fallback at these pins. The common retained shader workload works,
but native fullscreen fails, native device loss/presentation/GPU timestamps
lack qualification, and the browser interactive gate is incomplete. These are
essential gates. Following the requested dependency order, the Go/Rust/browser
performance matrices were stopped. No 120 Hz WebGPU success or CPU/GPU budget
pass is claimed, and no M1–M5 work or landing was started.

Implementation changes this session are isolated to this nested module and the
plan's pin/progress. The plan branch already contained the explanatory link in
docs/DESIGN_METAL_RENDERER.md; that inherited change was left intact.
The branch is webgpu-p0-p4, created from the plan-containing commit afe0c4254
and integrated with main at 1430571dc before editing. Bounded commits include
9415015c0 (pin/probes), 30b0b1845/98dd5fdff (fixture/oracle),
31f6efdd5/4020b78b5/9285554b8 (retained renderer/corrections),
60aeaa652 (lifecycle), 1a7f3688f/211079ccc/1741f594b (reports/review fixes),
and 28cbdfa4a (GPU point readback). No root module, production Go file, default,
or authoritative simulation change is part of this branch.

### What was measured, and what was stopped

Reference environment: Apple M3 Pro, 12 CPU/18 GPU cores, 18 GiB memory,
macOS 26.6.2 build25G83, Go1.27.1, AC power/low-power disabled, Chrome153/DPR2.
CoreGraphics confirms the main display at 120 Hz, logical1728×1117,
physical3456×2234. Primary qualification surfaces actually acquired1920×1080.
Native comparison GOMAXPROCS=2; Wasm executes Go on the browser main thread
without a worker renderer. See environment.json and display.txt in the artifact
root. Interactive desktop/browser activity was not controlled, so the independent
Metal results below are context and do not establish a clean adoption benchmark.

For each blocked WebGPU path, tools/bench --inventory-only emitted all seven
workloads × three rounds: manifest.json with the fixture/module/shader hashes,
empty frames.csv and summary.json with status unrun, zero measured draws,
null timings/throughput and the precise failed/incomplete dependency gate.
There are **63 unrun entries**, not 63 completed runs. Qualification metadata
is in qualification.json. A normal launch through tools/bench rejects these
statuses before creating output. There was no qualified backend pair to alternate.
The 5k, 500-triangle, Retina, static and camera performance results are unrun;
large-count correctness at200 triangles/1080p is separate evidence.

The observation schema preserves missing values as null; it excludes the
warmup-crossing interval and distinguishes fixture preparation, encoding,
fence-poll CPU, queue submit/completion, callback cadence and presentation.
Packing is currently included in Encode rather than separately observed;
total CPU-budget telemetry therefore stays unavailable. Browser raw queue
observations are downloaded separately from Go observations, without substituting
queue completion for GPU execution. GPU timestamps and native actual display
observations remain unavailable through the pinned borrowed-context API.
Framework-owned transient object counts are also not instrumented; zero
persistent creations refers only to renderer-owned resources. This scaffold is
CPU/build-reviewed, **not runtime-qualified P4 timing infrastructure**.

Source snapshots are now prepared only on a new generation; static/camera
reuse endpoints and camera changes affect uniforms only. CPU tests and actual
native point runs validate the unchanged fixture contract. Timed runs need to
confirm update/allocation cadence; source inspection is not their substitute.
First-frame/pipeline creation is separated as cold work when observations exist.
Missing input-to-submission and presentation latency remain null. Browser input
bookkeeping now consumes a marker only on the next actual submit and records
latency only for the first reflected serial; unlimited interactive runs retain
at most8192 submissions and1024 events. Authored Node mocks lock those properties;
they provide no real GPU or browser lifecycle evidence.

The added untimed points.wgsl pass transforms (.25,-.13,.17,1) through every
interpolated GPU piece, then reads back XYZ. Native64/1k/3k/5k × five alphas
passed against the independent oracle at absolute1e-4; maximum error2.158e-6.
It is excluded from timed rendering and its resources are owned by Capture.
Browser verification of this added point readback remains **unverified** because
control was unavailable. Earlier browser matrix/pixel/depth/ID diagnostics remain
passing evidence, with their exact scope preserved.

### Independent public Metal comparison

An experiment-owned LiveSource feeds the same meshes, endpoint sequence,
projection, count and pixels to metalrender.Run; tiny authored terrain/sprite
textures satisfy its public contract. Its model atlas expects palette-index
bytes. An authored lossless conversion preserves the original nearest sampled
colors and records encoded-atlas/palette hashes separately from the semantic
fixture hash. An initial main-thread setup mistake was corrected in the adapter
before measuring; its failed log is preserved, not attributed to Metal.

Metal shading, quantized-height depth, attachments and composition differ from
WGSL's diagnostic MRT/ID/overlay passes. Thus these are **contextual measurements,
not matched end-to-end FPS or GPU rankings**. No production shader was changed.
The public runner exposes actual drawable presentation callbacks and command-
buffer GPU start/end. No profiler, readback or forced collection ran inside the
measurement interval. Captures were made afterward.

Each round submitted240 warmup +3600 measured draws at1920×1080, with exactly
3000 instances/9000 pieces/600000 model triangles in every CSV row. Every tick
and alpha was checked against d/4 and(d%4)/4. All command statuses completed.
The warmup boundary is excluded from intervals, and all3600 measured rows have
presentation timestamps.

| Round | Submission elapsed s | Actual displayed frames/s | GPU p95/p99 ms | Source preparation p95/p99 ms | Display interval p99 ms | Gaps >1.5 refresh periods |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 30.050 | 119.80 | 5.11 / 5.91 | 5.09 / 5.79 | 8.34 | 6/3599 |
| 2 | 30.016 | 119.93 | 4.88 / 5.63 | 4.57 / 5.55 | 8.34 | 2/3599 |
| 3 | 30.375 | 118.52 | 5.41 / 5.67 | 4.52 / 5.62 | 16.67 | 45/3599 |

No gap exceeded50ms. Rounds1 and3 fail the proposed gap fraction; round3 also
fails119 displayed frames/s and the p99 interval limit (1.1×8.333≈9.167ms).
Do not pool away those failures. Source preparation
alone exceeds the proposed CPU budget in this context; this identifies authored
endpoint generation as a follow-up cost to measure, not GoGPU's GPU cost.
Each round allocated about1.047GB of Go memory (process-wide, includes final
submission drain; see report scope), roughly34.5–34.9MB/s. This is distinct from
live heap and device memory. Device allocation and native waits are in each
original report. Missing cold/input telemetry is null in the supplemental summary.

Artifacts: metal-primary-round-{1,2,3}-v2/{manifest.json,frames.csv,
live-frames.csv,report.json,summary.json,final.png}; exact command vectors in
metal-primary-round-{1,2,3}-command.json. Independent reviewer checked all row
counts/schedules and the64/3k captures. Round1 and3 final images were visually
inspected: complete colored grid, articulation, rotated/beveled forms and viewport
margins, with expected shader/pass differences. No whole-image parity claim.

### Platform qualification matrix

| Platform/path | Build | Runtime/correctness | Cadence / GPU timing | Lifecycle | Packaging |
| --- | --- | --- | --- | --- | --- |
| macOS arm64 Go | CGO0 pass | smoke, poses/pixels/IDs and GPU points pass64/1k/3k/5k | unrun / unavailable |10 cycles pass; resize/focus pass; fullscreen fails; native loss/DPI/minimize unverified | normal binary and diagnostic app bundle launch; distributable signing/notarization unverified |
| macOS arm64 Rust FFI | CGO0 pass | pinned ABI abort; diagnostic patch then map-state abort | unrun | stopped | exact v29 artifact locally hashed; clean-install discovery/notices incomplete |
| Chrome153 macOS Wasm | pass | smoke and prior pose/pixel/ID tests pass; added GPU point readback pending | unrun / timestamps unavailable at requested features |10 cycles, loss/stop/fresh session pass; interactive gate incomplete | localhost + matching wasm_exec.js works; other browser/deployment environments untested |
| Windows amd64 Go/Rust | CGO0 cross-build pass | hardware untested | untested | untested | Go runtime and Rust native-library discovery/checksums not locally qualified |
| Linux amd64 Go/Rust | CGO0 cross-build pass | hardware untested | untested | untested | graphics/runtime and Rust native-library discovery/checksums not locally qualified |

Resolved graph: exact GoGPU pseudo-version from §3; wgpu0.34.2,
gpucontext0.31.3, gputypes0.8.0, goffi0.6.3, webgpu0.5.5, naga0.19.0,
x/sys0.48.0. The optional Metal comparison imports the root's existing
purego0.11.1 (Apache-2.0) only into the nested graph. License/provenance audit
is in patches/README.md. No dependency patch is adopted. The native-artifact
publisher pins are recorded separately; only the macOS artifact was downloaded
and independently hashed. No GPL dependency was introduced.

### Reproduction and bounded next recommendation

Artifact root: /private/tmp/nanolathe-webgpu-20261010. Run from this module:

```sh
tools/check
tools/build /tmp/webgpu-followup-new
# This report-only command deliberately records blocked work:
tools/bench --backend go --inventory-only   --qualification /private/tmp/nanolathe-webgpu-20261010/qualification.json   --environment /private/tmp/nanolathe-webgpu-20261010/environment.json   --output /tmp/webgpu-go-unrun-new
# Independent context only, prebuilt on Darwin; run sequentially:
../../tools/host-run env GOMAXPROCS=2 /tmp/webgpu-followup-new/metal-probe   -count=3000 -detail=200 -width=1920 -height=1080 -warmup=240 -frames=3600   -mode=animated -out=/tmp/webgpu-metal-context-new
```

The next assignment should be2–5 engineering days of bounded qualification:
review a pinned fullscreen correction with an actual round-trip regression;
qualify native loss/error and real submission/presentation/GPU timing seams;
restore a stable browser test session and finish point/resize/DPI/fullscreen/
hide-resume/first-input/close-relaunch checks; then run the complete controlled
three-round matrix with alternated backend order. If required fixes grow into
compiler/HAL/framework reconstruction, reject/rescope the dependency instead
of expanding this experiment. Current report tooling still needs real runtime
qualification, completion/wait accounting and browser report joining before
that measurement can be accepted.

Subsequent production risks, only after a new assignment: (1) host/device
lifecycle and Windows/Linux/browser runtime evidence; (2) presentation timing,
retained uploads and CPU preparation costs; (3) real-battle immutable sample
lifetimes/composition and Original/Enhanced parity; (4) terrain/water/fog/shadow/
effect/material visual contracts; (5) menus/input/audio/assets/save/online host
behavior; (6) packaging, fallback policy and library extraction. Based on the
prototype's binding and host friction, budget1–2 weeks for a first battle proof
and roughly8–16 engineer-weeks for full host/renderer migration after qualified
dependencies, with hardware/platform scheduling additional. These are planning
ranges, not authorization or a promise. Keep the production defaults until the
later gates actually pass.

Independent read-only reviews covered P0/P1, P2/P3 and P4; their telemetry and
clock-label findings were fixed forward with separate commits. Experiment
checks and root architecture/docs guards passed on the earlier P4 candidate;
final checks are recorded in experiment-check-final.log/root-guards-final.log.
Root tools/check and the short tools/check-retail gate passed, including the
Rosetta amd64 and Wasm fingerprint locks, Wasm relay, real-device GPU fixtures
and Metal device test. Logs are root-check.log and root-check-retail.log. Final
Windows/Linux Go and Rust-tag cross-builds also passed. These repository gates
do not turn the failed/incomplete prototype runtime gates into passes.

The final artifact inventory is evidence-index.json under the artifact root,
with relative paths, sizes and SHA-256 hashes. Final unrun matrices are in
unrun-final-{go,rust,browser}; older inventories are retained as earlier source
snapshots. modules-p4.txt records the final nested graph. This branch is retained
for review and has not been landed.


## Continued Metal-gap investigation (2026-10-11)

The qualified default keeps resident-source batching. See the latest section of
FOLLOWUP_REPORT.md and METAL_GAP_MEASUREMENTS.json for matched full-game Metal
comparisons, fresh-presentation accounting and all successful/failed variants.
Source prepare-ahead is explicit (`NANOLATHE_WEBGPU_PREPARE_AHEAD=1`); it has
mixed performance and still waits for GPU completion per frame. Compact slot
placement (`NANOLATHE_WEBGPU_COMPACT_SLOT_PACKING=1`) halves Coast body memory
but changes raster edges, so remains a diagnostic. `tools/build-play OUT
--metal-cache` builds a private exact-v0.34.2 cache patch for macOS arm64 ABI
investigation; ordinary builds use unchanged pins. These experiments do not
establish steady120Hz or qualify the real game on Linux, Windows or browser.


Owned-upload follow-up (historical; the patches were removed on 2026-10-12, see
"Stock dependencies" above): guarded private dependencies with
`NANOLATHE_WEBGPU_ASYNC_UPLOADS=1` removed per-draw completion. See
FOLLOWUP_REPORT.md and OWNED_UPLOAD_MEASUREMENTS.json. Matched Coast improved100.92→115.99 callback Hz,
and source-ahead reached119.53Hz with identical final image/state/GPU fields.
Dense fixed Field3000 reached87.06Hz; steady120FPS remains unproven. Public buffer
writes were already staged: the repairs cover mutable texture staging, pending
copy lifetime, truthful completion and propagated/quarantined failures. One
renderer can use queue-owned staging; a second application buffer ring is not
intrinsically needed. Browser threading is deferred per user preference.


### Optional bounded storage reservation

`NANOLATHE_WEBGPU_RESERVE_STORAGE=1` moves capacity-dependent model/effect shader compilation to setup by reserving less than 24 MiB of mutable buffer tiers. Texture planes and resident geometry/explicit UVs are unchanged; larger workloads retain ordinary growth. It is opt-in and part of `run-play --fast`. `FOLLOWUP_REPORT.md` and `STORAGE_RESERVATION_MEASUREMENTS.json` contain the fixed-state pixel, native growth, memory and timing qualification.

For repeatable native comparisons, point `NANOLATHE_SETTINGS` at a private settings snapshot with nominal `gameSpeed: 10`; do not edit saved preferences. The bounded interactive field diagnostic additionally uses `NANOLATHE_WEBGPU_BENCHMARK_NEUTRAL_POINTER=1`, `--live-scene=field:1000`, normal `--zoom=1`, and a fresh report directory. Callback Hz is not presentation Hz.

The authored native oracle accepts `-pose-growth` to test 40,000-pose growth above the reservation and shrink-back, with exact pixel checks and GPU tail-pose readback. Standalone coverage uses CGO_ENABLED=0 without the combined game command's `nofakecgo` tag.


Optional render runtime arrays (historical): the `--render-runtime` patch that
supplied Metal render-size metadata was removed on 2026-10-12. Without it every
build uses the fixed-array path; `NANOLATHE_WEBGPU_RUNTIME_ARRAYS=1` has no effect.

The runtime path passed native binding/offset tests, the19-case model/UV growth oracle with zero growth pipeline creation, and byte-equal Coast state/final pixels/18 GPU sidecars. It keeps meshes and explicit UVs resident. Current dense performance is near119 callbacks/s with encoding CPU above the reservation reference; neither callback FPS nor this optional path establishes actual presentation. Exact measurements and scopes are in `RUNTIME_ARRAY_MEASUREMENTS.json`.


### Current reproducible native prototype

On macOS arm64, from this worktree:

```sh
experiments/webgpu/tools/build-play /tmp/nanolathe-play --metal-cache
/tmp/nanolathe-play/run-play --fast -- --root="$HOME/TotalAnnihilation" \
  --map='Town & Country' --webgpu-report=/tmp/nanolathe-play-report
```

The output build directory must be new. Without flags the build uses the stock
pinned dependencies unchanged; `--metal-cache` applies the pointer-cache patch
to a private copy of wgpu, and `--present-handler`/`--gpu-timing` add the
measurement-only diagnostics (`run-play --telemetry`/`--gpu-timing` turn them
on). The helper produces a binary, `.app` bundle and exact
source/module/patch manifest; it installs nothing and never patches the module
cache or the shipping renderer. Go 1.27.1 is required. Ordinary interactive
play reads the user's current settings; benchmark evidence uses a private
nominal-speed-10 snapshot (`NANOLATHE_SETTINGS`) and must not be extrapolated to
other game speeds.

The generated production-source host supports serial programmatic resize:
join old preparation, preserve its terminal/error result, update client/HUD and
culling extent, then render with the actual framebuffer size. Fixed benchmark
sources retain their fixed window contract. Final capture stays at the last
consumed packet's rendered extent. Actual dragging, minimize/restore, compositor
occlusion, display-scale migration and device-loss recovery remain unqualified.
See CURRENT_READINESS.md and WINDOW_LIFECYCLE_MEASUREMENTS.json for evidence and
remaining limits. No repeatable FPS gain is claimed for hull aliases; their
measured improvement is allocations and CPU tails.


### Explicit launch profiles

Every `tools/build-play` output includes `run-play`:

```sh
BUILD/run-play --fast [--no-late-acquire] [--telemetry] [--gpu-timing] -- \
  --root="$HOME/TotalAnnihilation" --map='Town & Country'
```

Or invoke `experiments/webgpu/tools/run-play --build BUILD_DIRECTORY --fast ...`.
Omit `--fast` for the baseline profile. `--fast` sets prepare-ahead, storage
reservation, late acquisition and GOMAXPROCS between 4 and 8; it needs no
dependency patch, so it is the speed profile on every native target. The
launcher verifies the binary digest, refuses telemetry flags for builds without
the matching diagnostic, clears only inherited prototype diagnostic variables,
preserves `NANOLATHE_SETTINGS` and never writes saved preferences. `execve`
keeps signal cleanup attached to the launched process. `--neutral-pointer` is
restricted by the renderer to a bounded interactive field diagnostic; it is not
ordinary-play or fixed-benchmark input.

One native width drag and a minimize/Raise visible-recovery sequence were
observed through supported accessibility controls. The UI sessions closed with
balanced submissions after bounded watchdog SIGTERM; they did not complete their
planned frame budgets, so minimized scheduling and broader live-resize behavior
remain unqualified. Compositor occlusion and display migration were not tested.
The 3600-frame unattended fast-profile soak had3600 unique positive display
observations at114.22336Hz as visible body triangles grew to38,374, with no model/
layer pipeline creation. This lower long-window cadence limits any120FPS claim.
Go heap grew before GC and a post-measurement heap sample returned to about1.88GiB;
selected post-close descriptors and handler routes drained. This is bounded
short-run accounting, not an all-native-memory leak proof.


Chunked mutable model uploads: large model writes are split at the staging
belt's 256 KiB chunk size so they reuse completed chunks instead of one-off
staging buffers. Introduced with the owned-upload profile, they are now on by
default on the stock pins (`NANOLATHE_WEBGPU_CHUNKED_UPLOADS=0` is the control)
and worth about 12 Hz on the zoomed-out 1440p field (patches/README.md).
TAIL_PROFILE_MEASUREMENTS.json keeps the original evidence.
