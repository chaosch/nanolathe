# WebGPU renderer: prototype, decision gates and migration plan

Status: implementation handoff, 2026-10-10. No prototype has been implemented or
benchmarked by this planning task. Repository baseline: main at `1430571dc`.

The first assignment is **P0–P4 only**: qualify GoGPU, build an isolated retained
renderer with authored assets, measure it, and return a recommendation. Stop at
the prototype verdict. Replacing the production renderer or host is a later
maintainer decision. A successful triangle, cross-build, or average FPS number
does not complete this assignment.

## 1. Objective and boundaries

Evaluate one renderer written in Go with shared WGSL compute, vertex and fragment
shaders, running on macOS, Windows, Linux and browser WebGPU. Native builds must
work with `CGO_ENABLED=0`. The preferred outcome is a small presentation library
whose GPU resources, shader pipelines and frame scheduling we control, using
GoGPU for device/window integration instead of writing three native backends.

The prototype must answer:

1. Can the pinned libraries implement the same useful GPU workload on native
   and browser paths, with reliable diagnostics and resource ownership?
2. Can retained geometry, GPU pose interpolation and instancing sustain a
   well-paced 120 Hz workload with 3,000 visible instances on the reference Mac?
3. What limits remain at 5,000 instances, higher geometry detail and Retina
   resolution? Is the cost in CPU preparation, GPU execution, or presentation?
4. How much host and rendering work remains before a real Nanolathe battle can
   use this path without losing its existing behavior?

This is presentation work. Preserve [INVARIANTS](INVARIANTS.md) I1–I6 and I11:
authoritative simulation stays at 30 Hz on the CPU, reads the existing rules,
and owns its existing RNG streams. GPU work never changes authoritative state,
pathfinding, orders, collision, visibility or replay results. Production
interpolation remains an Enhanced presentation choice; Original continues to
sample committed frames according to its contract. An authored animation in
this experiment is not a new gameplay or retail contract.

WGSL provides the compute, vertex, fragment, buffer, texture and indexed draw
facilities needed for this approach. UVs are ordinary vertex data. General
triangulated meshes and bevels do not require geometry or tessellation shaders;
author their topology during asset preparation. Those shader stages, mesh
shaders and ray tracing are outside this portable baseline. GPU culling and
indirect draws are possible follow-ups, not prerequisites for the first result.
See the [WebGPU specification](https://www.w3.org/TR/webgpu/) and
[WGSL specification](https://www.w3.org/TR/WGSL/).

Keep existing Ebitengine and Metal entry points working throughout evaluation.
Do not change the default renderer, root module dependencies, production browser
launcher, simulation, asset formats, gameplay modes, or public settings in P0–P4.
Do not build a general ECS, editor, scene graph, plugin system or shader language.
Do not fork Kage, write a WGSL compiler, or implement a native graphics backend.

## 2. Read first and reuse

Read `AGENTS.md`, [ARCHITECTURE](ARCHITECTURE.md) §§2–4 and §6,
[INVARIANTS](INVARIANTS.md), [DESIGN_METAL_RENDERER](DESIGN_METAL_RENDERER.md),
[DESIGN_GPU_RENDERER](DESIGN_GPU_RENDERER.md),
[DESIGN_BROWSER_HOST](DESIGN_BROWSER_HOST.md),
[BATTLE_BENCHMARK](BATTLE_BENCHMARK.md), and
[SIM_BENCHMARK](SIM_BENCHMARK.md). Consult [SPEC_CONFLICTS](SPEC_CONFLICTS.md)
before interpreting a visual difference as a retail defect.

Established repository boundaries:

| Existing boundary | Use in this work |
| --- | --- |
| `internal/meshscene` `Mesh`, `Vertex`, `Instance`, `PoseFrame`, `Scene` | Reuse the retained geometry and pose concepts; construct the first fixtures directly. `Vertex` carries UVs and a piece index. |
| `meshscene.LiveFrame` and `LiveSource` | Later production input. `Generation` permits reuse of unchanged endpoints/instances. Returned slices are immutable only through the next `Next` call; GPU submission does not extend that lifetime. |
| `meshscene.RetainBattle`, `RetainedBattle.Frame`, `Covers`, `Recull` | Later asset/session adapter. Retention loads the mounted model namespace; it is too broad for an asset-free smoke test. Preserve session-selected seed and perspective. |
| `Client.PinPresentation`, `PresentedFramePair`, `ModelComposer.Prepare` | Preserve the publication and composition contracts when live battles are added. |
| `internal/platform/metalrender` | Native comparison and visual reference; do not copy its window loop and composition into every new platform. |
| `internal/metalhud` | Existing retained overlay concepts, for later host migration. |

The current command-level Metal battle adapter is private and Darwin-specific.
Extracting it is a separate production work unit after the prototype. Do not
make a macOS command importable from an experimental renderer.

The existing Metal result is useful evidence, not proof of the new target:
[DESIGN_METAL_RENDERER §8.3](DESIGN_METAL_RENDERER.md#83-measured) records roughly
119–120 draws/s on an M3 Pro. The [measurement history](METAL_RENDERER_HISTORY.md)
records 1,433–1,502 total simulated units in the field case and 729–821 in-view
anchors at the larger resolution. Draw throughput is not scanout evidence.
This is not a test of 3,000 or 5,000 visible models. The current battle fixture supports three
armies of up to 1,000 units, excluding commanders; it is not an exact
5,000-unit battle fixture.

## 3. Dependency decision and source baseline

### 3.1 Pin the audited source

Use `github.com/gogpu/gogpu` commit
`12620c9d50073b8591f075c97ba0c6f3b6ad92f7` as the initial experiment pin.
P0 resolved it as `v0.54.1-0.20260901095536-12620c9d5007`.
Resolve its canonical Go pseudo-version during P0, commit that version and
checksums, and record the resolved module graph. Do not use `@latest`.

**Do not substitute `v0.54.0`.** That release's synchronous application loop
does not include the browser RAF scheduling in the audited commit. Compare
the [release loop](https://github.com/gogpu/gogpu/blob/v0.54.0/app.go) with the
[pinned browser loop](https://github.com/gogpu/gogpu/blob/12620c9d50073b8591f075c97ba0c6f3b6ad92f7/app_run_browser.go).
Recheck these facts against the downloaded source before depending on them.

The [pinned module](https://github.com/gogpu/gogpu/blob/12620c9d50073b8591f075c97ba0c6f3b6ad92f7/go.mod)
requires `gogpu/wgpu v0.34.2`, `gogpu/gpucontext v0.31.3`,
`gogpu/gputypes v0.8.0`, `go-webgpu/goffi v0.6.3`,
`go-webgpu/webgpu v0.5.5`, `gogpu/naga v0.19.0`, and `x/sys v0.47.0`.
These are upstream requirements, not our resolved build list. The root already
requires `x/sys v0.48.0`; record minimal-version selection with `go list -m all`.

Qualify all three paths with the same shader source and scene:

| Path | Purpose | Packaging |
| --- | --- | --- |
| Native Go backend | Preferred candidate if it passes; Go WGSL compiler and native HAL | No cgo; normal OS graphics runtime dependencies still apply. |
| Native `-tags rust` backend | Comparison against wgpu-native; may become the pragmatic first native implementation | No cgo, but an FFI-loaded native library is required. Pin the matching wgpu-native `v29.0.0.0` artifact and its checksum for each OS/architecture. |
| `GOOS=js GOARCH=wasm`, without `rust` | Browser WebGPU through JavaScript | No native shared library; matching Go `wasm_exec.js`, secure context and an available WebGPU adapter. |

The Rust path satisfies **zero cgo**, not a requirement that every distributed
component be written in Go. Make that distinction explicit in the verdict.
Use the [binding requirements](https://github.com/go-webgpu/webgpu/blob/v0.5.5/README.md)
for native library matching. Do not run a floating `setup@latest` installer.
Check licenses and redistribution notices for the resolved modules and native
binaries against the repository policy; do not add GPL code.

### 3.2 Known qualification gaps

These are source findings at the pins above, not runtime results:

| Finding | Required response |
| --- | --- |
| GoGPU's [device creation](https://github.com/gogpu/gogpu/blob/12620c9d50073b8591f075c97ba0c6f3b6ad92f7/renderer.go) uses `RequestDevice(nil)` | Baseline must fit default requested limits and need no optional features. Adapter support alone does not enable a feature on the device. |
| The [Rust adapter](https://github.com/gogpu/wgpu/blob/v0.34.2/adapter_rust.go) does not forward `RequiredFeatures` | Timestamp queries or other optional features need a small, reviewed dependency correction/device-creation seam before use. |
| Browser [device methods](https://github.com/gogpu/wgpu/blob/v0.34.2/device_browser.go) have unimplemented error scopes, a no-op `WaitIdle` and an always-true `Poll`; [queue submission/poll indices](https://github.com/gogpu/wgpu/blob/v0.34.2/queue_browser.go) return zero | None proves completion. Qualify asynchronous error reporting and a real completion signal, such as a bounded bridge to `GPUQueue.onSubmittedWorkDone`, before using completion-dependent staging or lifecycle checks. Do not call the panic paths. |
| Native Go [Metal implementation](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/device.go) has gaps in compute constants, workgroup initialization, timestamps and render bundles | Avoid override-dependent compute, explicitly initialize shared workgroup data if used, and keep render bundles out of the required design. Report GPU timings unavailable when necessary. |
| Browser platform lacks relative cursor support | Basic pointer/keyboard input is sufficient for the prototype; production pointer capture is a named later host gap. |

P0 may make a bounded compatibility patch in an explicitly pinned local fork
or patch directory, with a regression case, license/provenance and separate
review. Do not patch the Go module cache. Limit the first investigation to one
working day; if required fixes expand into compiler/HAL work or a broad framework
fork, report a blocked path and complete independent paths. Do not silently
upgrade dependencies, drop tests, or start a second renderer architecture.

## 4. Isolation, ownership and public API

Create `experiments/webgpu` as a nested module named
`github.com/nanolathe-gg/nanolathe/experiments/webgpu`, with its own `go.mod` and
`go.sum`. Use `GOWORK=off`. If importing existing internal scene types, require
the root module at `v0.0.0` with `replace github.com/nanolathe-gg/nanolathe => ../..`.
This import path remains inside the root's internal-package boundary. Use the
repository Go version; do not create a committed `go.work` or alter root module
files. Verify the production dependency graph remains unchanged.

I12 orchestrator sign-off for the eventual implementation is limited to this
isolated GoGPU evaluation and its documented transitive dependencies. It is not
approval to add GoGPU to the production module or add unrelated libraries.

Root `go test ./...` skips the nested module, so run its checks explicitly.
Root architecture guards also walk source files and **do not currently skip a
nested `go.mod`**. Keep those guards enabled and run them after adding the module.
If an actual false positive needs a scope correction, report the exact failure
and propose a separate, narrowly reviewed change with a regression fixture;
do not weaken or add broad allowlists to get the experiment green.

Initial file ownership, all inside the new module unless stated otherwise:

| Unit | Files it owns | Depends on |
| --- | --- | --- |
| P0 coordinator | `go.mod`, `go.sum`, `README.md`, `cmd/webgpu-probe/*`, `internal/host/*`, `web/*`, `tools/*`, `patches/*`; this plan's progress section | Nothing |
| P1 fixtures | `internal/fixture/*`, authored `testdata/*` | P0 dependency smoke and agreed data contract |
| P2 renderer | `internal/render/*`, including embedded WGSL and layout tests | P1 fixture/data contract |
| P3 lifecycle | `internal/host/*`, `cmd/webgpu-probe/*`, `web/*` after P0 relinquishes them | P2 small scene |
| P4 measurement | `internal/measure/*`, `tools/*`, `README.md` after P0 relinquishes them | P2/P3 working paths |

Default to sequential implementation for the first handoff. Parallel work is
useful only after P0 freezes the small API; never assign the same files to two
agents. Each agent gets a worktree and one bounded unit/commit. No production
Go file is owned by P0–P4.

Freeze these contracts in the experimental README before P2:

- **Fixture:** constructs an immutable `meshscene.Scene` subset, plus a pure
  function producing two endpoint pose arrays and per-instance records for a
  requested 30 Hz sample. Inputs include workload version, fixed seed, count,
  detail tier and animation state. No retail asset access, session construction,
  simulation stepping or simulated state input. Reusing `meshscene` types may
  bring their existing compile-time dependencies; it does not run a session.
- **Renderer construction:** receives a device, queue, surface format and
  immutable scene. Uploads static resources and creates pipelines. It does not
  create a window, select an adapter, start a session or own the device.
- **Frame encode:** receives a borrowed command encoder and surface view,
  physical extent, camera, interpolation alpha and one owned snapshot of
  instance/pose data with a generation. Returns work counters and an error.
  It encodes passes but never finishes/submits the borrowed encoder.
- **Resize/close:** resize recreates only extent-dependent targets; zero-sized
  surfaces are suspended. Close releases renderer-owned resources exactly once
  after the host has established safe completion, without destroying borrowed
  device/surface objects.
- **Measurement:** receives separately named CPU, GPU, queue, callback and
  presentation observations, each with availability/provenance. It has no
  device or simulation ownership and never substitutes one timing for another.

Use concrete types and a few functions; do not create a generic render graph or
large interface hierarchy. Any necessary API change is reviewed before another
unit depends on it.

## 5. Portable GPU design

### 5.1 Retained resources and update cadence

Upload immutable vertex/index buffers, atlas pixels and material tables once.
Keep one GPU representation per mesh, with many instance records. CPU work
produces committed pose endpoints at 30 Hz. The first compute pass performs
rigid-pose interpolation into a storage buffer at draw cadence; the vertex
shader reads the instance/piece index and transforms resident geometry. This matches the
existing presentation approach and avoids rebuilding vertex arrays on the CPU.

Freeze the interpolation contract before the GPU implementation: column-major
matrices, the existing world-Z reflection separated from rotation, shortest-path
quaternion slerp for orientation and linear translation. Orthonormalize the
unreflected basis before quaternion extraction. Preserve exact endpoint
results and equal bases; elementwise matrix lerp would shrink rotating models.
Use the current `interpolate_poses` contract in Metal's `shaders.metal` as the
presentation reference. Its lanes also carry metadata: index 3 is current
shade, 7 is the interpolated tagged origin, 11 is true only if both endpoints
are tagged, and 15 is visibility (either hidden endpoint suppresses the piece). Define
authored fixture values for these lanes explicitly and keep them out of affine
rotation extraction. Lock reflection, near-identical rotations, shortest-path
rotation and hidden endpoints in P1. This preserves the existing input boundary;
it is not a new retail interpolation claim.

The prototype does not need GPU hierarchy evaluation to prove value. The
endpoint matrices may already include hierarchy, as existing `meshscene` frames
do. Record its CPU cost separately. Evaluate GPU hierarchy/local-pose animation
later if profiles justify it; do not move COB or authoritative animation state
to the GPU. Interpolation is not permitted to alter source snapshots.

On unchanged generations, update only small frame uniforms such as camera and
alpha. One changed generation may upload its pose/instance records once; do not
repeat that upload at 120 Hz. Treat topology/instance-count changes explicitly.
Batch draws by mesh/material where ordering allows. Do not use an immediate
triangle API or one draw per unit.

Document upload ownership for every Go slice and native FFI call. Copy into
renderer-owned staging when a source's lifetime is insufficient. Establish
whether each pinned upload call copies synchronously or retains memory; test
the selected path under GC pressure. Use queue ordering or bounded staging
rings with real completion signals, according to the API contract. Do not
assume that three elapsed frames, `runtime.KeepAlive` alone, or a browser
`WaitIdle` call makes retained memory safe. Never synchronously read GPU data
back in the ordinary draw loop.

### 5.2 Resource and shader layout

Use one WGSL source set on all paths. Baseline shader types are `f32`, `u32` and
`i32`; optional features start disabled. Begin with 64-invocation compute
workgroups. No dependency on `f16`, subgroups, bindless resource arrays,
nonzero indirect first-instance, render bundles or optional texture access.

Design within the [core WebGPU limits](https://www.w3.org/TR/webgpu/#limits):
at most four bind groups, eight storage buffers per shader stage, sixteen
sampled textures per stage, four storage textures per stage, 128 MiB per
storage-buffer binding and 256 MiB per buffer. Query and record actual limits;
these are baseline design ceilings, not permission to exceed the selected
device's limits. Respect queried offset alignments; baseline uniform/storage
binding alignment is 256 bytes. Use base indices within packed tables instead
of arbitrary small binding offsets.

The current Metal model vertex signature has twenty storage-style buffers and
the face preparation kernel seventeen. Extra bind groups do not bypass a
per-stage limit. Pack related immutable tables into documented arenas and
combine per-frame records before porting these passes. Keep independent tables
typed where possible; avoid an unreviewable byte-addressed interpreter.

Define explicit byte offsets, strides, matrix orientation and padding in the
Go packer and WGSL declarations together. The current 64-byte vertex uses
packed three-component fields; WGSL storage `vec3<f32>` alignment is different.
Vertex input layout and storage struct layout are separate contracts. Lock
representative offsets with small tests and a GPU round-trip outside timing.
See [WGSL alignment](https://www.w3.org/TR/WGSL/#alignment-and-size).

Reduce the large Metal vertex output: pass the necessary interpolants and flat
IDs, then fetch per-instance/material constants in the fragment shader. Fit
the core inter-stage budget; do not carry every input record through the
rasterizer. Verify the resulting buffer/texture access costs with profiles.

Use ordinary filterable texture formats, a supported depth format, and a small
MRT fixture. Fix color-space, alpha convention, winding, clip-depth range,
texture origin and depth compare in the README. For passes that currently read
and write one texture, start with separate source/destination textures. Do not
assume optional read/write storage formats or linear filtering of `R32Float`.
For shared workgroup memory, initialize it explicitly and obey uniform barrier
control flow; cross-workgroup dependencies need separate dispatches. Relevant
contracts: [texture capabilities](https://www.w3.org/TR/webgpu/#texture-format-caps)
and [WGSL uniformity](https://www.w3.org/TR/WGSL/#uniformity).

Use paged textures/material tables when required; do not bake one unbounded
atlas or per-scene giant storage binding into the API. The recorded Town &
Country feature/effect sprite atlas was 8,192 × 7,204 pixels; the shared model
atlas was much smaller, at 8 MiB or 32 MiB when doubled. See the
[scoped memory measurements](METAL_RENDERER_HISTORY.md#memory-and-latency-costs).
The tiny prototype atlas does not establish production atlas or memory acceptance.

## 6. Prototype work units and exit gates

### P0 — Reproducible dependency and host qualification

Create the nested module and minimal launchers. Resolve/pin the source and
record module versions, toolchain, native binary checksums and licenses. Build
with cgo disabled for native Go, native Rust and browser paths. Attempt Windows
amd64 and Linux amd64 cross-builds too, recording any unsupported path exactly;
compilation is not runtime acceptance. Include macOS arm64 on the reference Mac.

Run an asset-free compute-to-buffer plus textured indexed-triangle smoke test
on the Mac with both native paths and in a real WebGPU browser. Establish the
actual adapter, device features, limits, formats, device creation and submission
ownership. Use low-level `DeviceProvider`/context access; check the pinned API
rather than copying signatures from newer examples. GoGPU's
[context encoder](https://github.com/gogpu/gogpu/blob/12620c9d50073b8591f075c97ba0c6f3b6ad92f7/context.go)
is borrowed within the draw callback; GoGPU submits it at frame end.

Prove asynchronous shader/pipeline errors are visible. Deliberately compile an
invalid shader and exercise an invalid binding in a controlled diagnostic run.
Record whether errors are callbacks, promise rejections or returned errors;
they must not disappear or crash inside an unimplemented error API. Establish
device-loss reporting and a safe completion/release mechanism on each path.
Browser callbacks must return to the event loop: no blocking wait for a promise
whose completion requires that same loop.

Browser smoke must render, accept input, resize, hide/resume and shut down. Use
feature detection and an actionable unavailable-WebGPU message; no automatic
WebGL2 fallback in this experiment. Use localhost/HTTPS and the matching Go
runtime file. Do not touch the production launcher or publishing workflow.

**Exit:** a dated capability report distinguishes build-only, runtime-pass,
runtime-fail and untested for each path. Freeze §4's API and §5's layouts. A
blocked native Go path does not prevent testing Rust/browser, but cannot be
reported as a passing pure-Go implementation. A blocked browser path prevents
an overall WebGPU adoption recommendation. Stop and report an unbounded
dependency problem instead of building a workaround engine.

### P1 — Authored workload and correctness oracle

Implement a small 64-instance diagnostic fixture and exact 1,000, 3,000 and
5,000-instance workloads. These are **submitted visible instances**, not a
simulation census. Use exactly three articulated pieces per instance, three
distinct meshes and three material identities, an asymmetric colored UV atlas,
visible depth overlap, rotated copies, mirrored UVs and UV seams. Do not add
arbitrary negative scales/reflections beyond the defined world-Z conversion to
the rigid-pose contract. Include independently authored triangle faces as well
as quad-derived triangles. A small bevel must
have real topology and UVs; do not depend on the retail textured-quad builder.

Define two geometry tiers: 200 and 500 triangles per instance, excluding the
fixed background and diagnostic overlay; each of the three meshes must meet
the chosen total. Generate actual subdivisions/detail, not coincident duplicate
triangles. All instances must intersect the viewport;
state how much of each is visible and retain a per-instance visibility-ID check
outside timing. Disable frustum/occlusion culling and LOD for this benchmark.
Do not silently reduce count, resolution, passes or texture quality to meet FPS.

Use a sheared orthographic projection matching the existing Metal live camera
for the primary workload. Arrange units in a fixed grid: columns are
`ceil(sqrt(count * width / height))` using floating-point division, and rows
are integer `(count + columns - 1) / columns`. Reserve a viewport margin and
keep each unit's full animation envelope in its cell. Choose and record scale
so the model spans at least half its cell in both projected dimensions; the
ID capture must show at least sixteen pixels per instance at 1080p. Put
intentional inter-model occlusion cases in the small diagnostic scene. Freeze
camera, cell spacing, projection, animation bounds and scale in P1 and hash
them with the workload. Do not refit the view differently per backend.

Provide static, camera-only and fully animated cases. Fully animated means
translation, rotation and articulation for every instance. Record the precise
mesh, index, texture and animation hashes, instance/piece/triangle counts, seed,
workload version and camera. Keep the authored fixture reproducible across
paths; small floating-point raster differences do not imply a simulation change.

**Exit:** CPU layout and endpoint tests pass; fixture counts/hashes are stable.
Define expected interior color/UV/depth samples and known pose results at alpha
0, 0.25, 0.5, 0.75 and 1 before implementing the corresponding GPU results.
These checks must catch transposition, padding, UV flips and wrong piece
selection, not simply assert that the renderer produced a nonempty image.

### P2 — Retained WGSL renderer

Implement static uploads, generation-based dynamic uploads, compute pose
interpolation and indexed instanced rendering. Add depth, textured shading and
a small two-attachment render/composite case to exercise production-relevant
pass transitions. Add an overlay quad after the world. This is not a port of
water, retail painter order, shadows, fog or the HUD.

The same scene and WGSL must run through every working P0 path. A backend-specific
shader needed for a core operation is a qualification finding, not something to
hide behind build tags. Test compute results using small asynchronous readbacks
outside timed runs. Compare transformed points to the independent CPU oracle
with a documented floating-point tolerance; start at absolute error `1e-4` for
the small unit-scale diagnostic fixture and investigate larger error before
changing it. Check exact interior colors for unfiltered diagnostics; visually
review filtered/material captures with edge differences identified separately.

Do not demand whole-image bit identity across different GPU compilers. WGSL
[floating-point evaluation](https://www.w3.org/TR/WGSL/#floating-point-evaluation)
and rasterization need a reviewed visual contract. Preserve sensitive retail
arithmetic when porting real passes later; do not claim the prototype establishes
that parity.

**Exit:** every diagnostic case is correct on the working native and browser
paths; unchanged frames perform no geometry/atlas reupload; draw/dispatch and
byte-upload counters prove batching and update cadence. No renderer-owned
persistent buffer, texture, sampler, bind group or pipeline is created in
steady-state frames after warmup. Count framework-owned transient surface views
and command objects separately; their creation is not a failure of retention.

### P3 — Host lifecycle and bounded resources

Exercise resize, DPI change, minimize/restore, focus loss, fullscreen round
trip where supported, hide/resume in the browser, and close/relaunch. Bound
frames in flight and report waits separately. Do not increase queue depth until
FPS looks good while hiding input latency. GoGPU's synchronous native render
dispatch is not evidence that preparation and rendering overlap.

Run ten scene create/render/destroy cycles on each working path. After safe
completion, renderer-owned live resource counts and byte estimates must return
to the same baseline; no count may grow each cycle. Record Go heap, process RSS
and device-memory estimates separately, before/after warmup and after cycles.
Use collection only between lifecycle checks, never to improve timed results.
Driver caches can retain memory; explain plateaus rather than treating RSS as
an exact GPU leak detector. Verify input/JS callback handles are also released.

On device loss, stop submissions and report a clear error. Test re-creating the
renderer on a fresh device/session where the API permits; seamless in-battle
recovery is not required by this prototype. Do not simulate success by ignoring
the loss. For the Rust path, launch from a clean installation directory to
verify native library discovery, not just the developer working directory.

**Exit:** no stale surface use, use-after-release, callback leak, panic or
unbounded ring/resource growth in these cases. Record unsupported lifecycle
operations explicitly. Show the browser still responds while GPU work runs.

### P4 — Measurement, comparison and verdict

Implement §7's report schema and run matrix. Compile once, then measure the
binaries. Capture the diagnostic scenes and workload screenshots after the
timed interval. Review them; an assertion that they should look right is not
visual verification.

Use the same authored scene in the existing Metal standalone runner when its
public `Run`/`Options` contract permits it, through an experiment-owned Darwin
adapter. The current runner requires `Scene.Live` (a synthetic `LiveSource`)
and valid atlas, terrain and sprite textures; `Scene.Frames` alone is rejected.
Use tiny authored placeholder textures for unused inputs and the same projection
and synthetic clock. Leave production Metal files unchanged. Compare matching
geometry, shading and physical pixels only; label unmatched passes. If the existing
runner cannot consume this subset without production changes, report that
comparison unavailable and retain its published results as context only.

**Exit:** deliver evidence and an explicit verdict from §8, then stop. Do not
begin a live battle adapter, change defaults, or remove Ebitengine as part of
the prototype assignment.

## 7. Measurement protocol and success criteria

### 7.1 Controlled run matrix

Reference target: the user's M3 Pro Mac with a verified 120 Hz display mode.
Record the actual machine, memory, OS build, GPU/backend/driver, browser/version,
power mode, display refresh and device pixel ratio. Primary framebuffer is
**physical 1920 × 1080**. Also measure physical 3456 × 2160 as a Retina stress
case. A CSS size is not a framebuffer size. Record actual surface dimensions.

For every working path, run the animated 200-triangle tier at 1k, 3k and 5k at
1080p. Run the 3k/500-triangle tier at 1080p and 3k/200-triangle tier at Retina
as stress cases. Use static and camera-only runs at 3k/1080p to isolate upload
and animation costs. Keep the 64-instance diagnostic fixture separate.

Use 240 warmup draws (two seconds at target 120 Hz), then 3,600 measured draws.
Number submitted draws from zero across both intervals. For draw `d`, authored
tick is integer `d / 4`, endpoints are ticks `tick` and `tick + 1`, and alpha is
`(d % 4) / 4.0`. Warmup covers draws 0–239; measurement covers 240–3839. Exclude
the interval crossing from warmup into measurement. Diagnostic alpha 1 is tested
separately. This gives a nominal 30 Hz source at 120 Hz, with identical endpoint
sequences on slower paths; slower runs take longer wall time. Interactive/live
simulation scheduling is a separate contract, not this synthetic clock.
Record actual elapsed time; 3,600 draws can take more than 30 seconds. Perform
three rounds, alternating backend order to reduce thermal/order bias. Run
sequentially with no competing benchmark or heavyweight gate. Use the existing
host benchmark lock through `tools/host-run`; consult its command interface
before adding the wrapper. Do not invent a second lock.

Match the current Metal reference's `GOMAXPROCS=2` for native comparisons and
record it. Record the browser runtime's actual execution constraints separately;
do not pretend Wasm has the same worker/thread configuration. Measure profiles
in separate runs; profiling, GPU capture and image readback distort frame times.
Do not put `go run`, shader cold compilation, scene loading or forced GC inside
the steady-state interval. Report first-frame and pipeline creation times apart.

### 7.2 Required evidence

Write artifacts outside the repository into a run-specific output directory.
Browser runs expose a local download of their report; they do not upload data.
Each run produces:

- `manifest.json`: repository and dependency commits/versions, native artifact
  checksum, Go version, backend, adapter limits and enabled features, dimensions,
  display/refresh information, quality settings and complete workload hashes.
- `frames.csv`: sample index and generation; CPU fixture preparation, packing,
  encoding, submission and pacing/ring waits; GPU time and queue completion
  when available; RAF/callback interval and actual display time when available;
  draws, dispatches, instances, pieces, triangles, uploads and resource creations.
- `summary.json`: p50/p95/p99/max and counts for each metric, elapsed time,
  throughput, long-gap counts, allocation rate, peak/live resource estimates,
  and explicit availability/reason fields. Absent timings are `null`, not zero.
- Diagnostic captures at declared sample/alpha/camera values, a workload capture,
  error/lifecycle logs, exact commands and the human visual-review findings.

Do not turn browser RAF intervals or queue completion into display timestamps.
`meshscene.FrameTiming.Presented` means actual display time. Keep it unavailable
when the platform cannot supply it, and name the scheduling proxy separately.
CPU elapsed time is not GPU time. Queue completion includes waiting and does
not establish scanout. Do not sum CPU and GPU percentiles as though overlapped
work were serial. Where GPU timestamps are unavailable at the pin, use a
separate qualified profiler run or leave the GPU budget unverified.

Measure input-to-submission latency using a timestamped input event and the
first frame reflecting it. If actual presentation timestamps exist, additionally
report input-to-presentation. Do not claim photon latency without suitable
external measurement. Record queue depth and scheduling waits alongside FPS.

### 7.3 Proposed engineering budgets

These are decision thresholds for the experiment, **not measured results or
retail facts**. Do not relax them silently after observing a failure.

| Gate | Threshold |
| --- | --- |
| Correctness | All P1/P2 diagnostics pass at 1k, 3k and 5k on each path claimed working; lifecycle/error checks pass. |
| Primary performance | 3k animated, 200 triangles/instance, physical 1080p, reference 120 Hz hardware. |
| CPU preparation + packing + encoding/submission | p95 ≤ 2 ms, p99 ≤ 3 ms; pacing waits excluded and reported separately. |
| GPU execution, when measurable | p95 ≤ 6 ms, p99 ≤ 8.33 ms for the primary workload. Missing evidence is unverified. |
| Native actual presentation | With qualified presentation evidence: at least 119 displayed frames/s; p99 interval ≤ 1.1 × the measured refresh period; gaps > 1.5 periods ≤ 0.1%; no unexplained gap > 50 ms. Without such evidence this gate remains unverified and the verdict is at most “Promising with limits”; report submission cadence separately. |
| Browser scheduling at confirmed 120 Hz | Apply the same interval/gap targets to RAF-driven submitted frames, explicitly labeled scheduling rather than scanout. A 60 Hz-capped browser cannot pass the 120 Hz gate. |
| Update cost | No steady-state mesh/atlas upload or renderer-owned persistent resource creation; endpoint/instance uploads only on a new nominal 30 Hz generation; camera-only frames change uniforms only. Framework transient objects are counted separately. |
| Resource lifetime | Ten lifecycle cycles return tracked live resource counts/bytes to baseline after completion; no monotonically growing ring, callback or GPU allocation count. |
| Stress workloads | Report 5k, 500-triangle and Retina costs/correctness; 120 FPS in these cases is a stretch target, not an initial adoption gate. |

Evaluate per-run results, not one favorable pooled average. Explain warmup,
outliers, clock resolution and unavailable telemetry. A 1k success with a 3k
failure is a useful result but does not meet the primary performance objective.
An apparent 120 FPS average with recurring 16–33 ms gaps does not meet cadence.

Prototype throughput does not promise 120 FPS for a battle with thousands of
units. Simulation, UI, water, shadows, effects, composition and platform events
consume additional budgets. The 6 ms GPU target leaves some margin but does not
prove those later passes will fit. Real-battle and simulation gates in §9 remain.

## 8. Required prototype handoff

Add a dated result section to the experimental README and a short progress entry
here. Include commits, commands, artifact locations, reviewed images, resolved
dependencies, source patches and a platform matrix. Label **build**, **runtime**,
**correctness**, **cadence**, **GPU timing**, **lifecycle** and **packaging**
separately. Windows/Linux with no available hardware remain untested even when
cross-compilation or a software adapter succeeds.

Choose one verdict and explain it:

| Verdict | Meaning and next action |
| --- | --- |
| Prototype accepted | Native candidate and browser pass functional gates, primary cadence and CPU budgets; the native GPU budget is verified by timestamps or a qualified profiler. Browser GPU time may remain unavailable if its scheduling gate passes, and must be labeled. Request review for M1; no production adoption yet. |
| Promising with limits | An essential functional, native presentation/GPU timing, 3k budget or lifecycle gate remains unresolved on the selected native path or browser. Name a bounded follow-up and the evidence needed; do not declare full success. Failure of an unselected native backend and unavailable browser GPU timestamps are disclosed separately, not automatic blockers. |
| Dependency rejected/rescope | Required work entails broad compiler/HAL/framework changes, divergent rendering architectures, or an unworkable browser path. Preserve the fixture/results and recommend a different binding/host decision. |
| Unverified | Available environment could not execute essential tests. Report exact missing machine/browser/API evidence; compilation alone is insufficient. |

Recommend a native backend using evidence: choose the Go path if it passes;
choose Rust FFI provisionally if it passes and the Go path does not, with the
native binary packaging cost explicit. If both pass, compare cadence, memory,
diagnostics, packaging and maintenance. No claim of broad WebGPU conformance
follows from our fixtures.

The handoff must also list remaining production work, ordered by risk, and an
effort estimate revised from actual prototype experience. The experiment should
take days rather than become an open-ended engine rewrite; a complete host and
renderer migration is multiple independently reviewed milestones. Reassess
effort when qualification and the first live battle results exist.

## 9. Subsequent migration, requiring a new assignment

### M1 — A real battle through the retained boundary

Create `internal/platform/webgpurender` only after the dependency decision.
Specify its production API and extend the platform dependency guard to
GoGPU/goffi before adding them to the root module. Simulation and
device-independent packages must remain free of GPU/window dependencies.
Exercise the relevant build tags. Extract the neutral battle composition adapter
from the Darwin command files in an explicitly owned work unit. Preserve committed frame lifetimes,
generation semantics, perspective, model ordering and clean shutdown.

Port visual contracts in small reviewed groups: retained models and terrain;
retail material/face composition and depth/painter ordering; features and
sprites; fog and shadows; water and effects; retained HUD/cursor. Preserve
Original/Enhanced distinctions and cite the owning designs/research. The
Metal-to-WebGPU buffer/varying reductions in §5 are prerequisites, not optional
late optimizations. Compare captures at each group before stacking more passes.

Add a new explicit WebGPU benchmark renderer selector and launcher. The existing
`--benchmark-renderer` currently accepts `classic|modern|metal`; do not document
a WebGPU value as already implemented. Keep production browser build/publish
paths separate until browser host acceptance.

Gate: field and coast live benchmarks with matching metadata, census, camera,
resolution and effects; captures reviewed against the reference renderers.
Run Classic and Modern presentation checks required by `AGENTS.md`. Record
actual visible/submitted and total simulated counts. Run the displayless
simulation benchmark separately to identify 30 Hz tick costs; do not attribute
a CPU simulation limit to the renderer or offload deterministic gameplay to
make the graph improve. Agree production performance thresholds using matched
baselines; synthetic thresholds alone cannot accept a full battle renderer.

### M2 — Complete application host

Port menus, settings previews, unit viewer, text/font rendering, clipboard,
cursor capture, DPI/fullscreen, input routing, audio device output, preferences,
asset loading and return-to-menu/re-enter-battle lifecycle. Reuse existing
device-independent UI/audio contracts. GoGPU system sound support is not a
game mixer replacement. Bound each subsystem as its own work unit.

Browser work preserves the asset/cache/save/download and gesture contracts in
DESIGN_BROWSER_HOST. Audit GoGPU's visibility/RAF lifecycle against the existing
online background stepping contract: rendering suspension must not silently
change lockstep scheduling. Browser freeze/discard limitations still apply.
Do not promise a worker renderer or threaded Wasm runtime before a separate
feasibility test; ordinary browser main-thread submission is the baseline.

Gate: launch through menus, play/exit two battles, use the viewer, resize and
change display modes, exercise audio, load/save where supported, and join/play
online without simulation fingerprint changes. Run browser host checks and
real browser sessions in the supported browser matrix. Unsupported WebGPU
devices need a clear support policy; retaining WebGL2 is a separate scope choice.

### M3 — Windows/Linux and browser qualification

Build CI matrix: macOS arm64; Windows amd64; Linux amd64; js/wasm. Add other
architectures only when they become supported targets. Qualify the selected
native path on Metal, D3D12 and Vulkan through the binding; do not implement
these APIs ourselves. For Linux, state X11/Wayland support independently.

Use native-host CI for dependency/build/lifecycle smoke tests where available,
and software GPU runs for correctness when useful. Such runs do not qualify
hardware cadence, driver behavior or display pacing. Produce a self-contained
diagnostic bundle and obtain physical Windows/Linux GPU runs from testers or
an explicitly arranged GPU machine. The maintainer currently lacks those
machines; shipping claims remain blocked until this evidence exists. No paid
infrastructure or external publishing is part of this plan's implementation.

Test integrated and discrete GPUs, a constrained-memory machine, driver/device
loss, window transitions, packaging from a clean location and the actual browser
versions chosen for support. Record browser/OS/GPU combinations, not merely
“Chrome works.” Shared WGSL reduces code duplication; it does not remove
platform testing.

### M4 — Profile-driven scale and remastering

After a correct live renderer, investigate GPU culling/compaction and indexed
indirect draws, LOD, animation hierarchy evaluation, texture paging and render
bundles only where qualified. Preserve painter/transparent ordering. Keep a
portable baseline if an optional accelerator is enabled; avoid a scattered set
of compatibility flags. Bound capability choices at device/pipeline creation.

Define a separate general indexed mesh input for authored remaster assets with
positions, normals, UVs, material/piece IDs and source-face provenance where
needed. The retail mesh builder's textured-quad restrictions must not constrain
new authored triangles or bevels, and must not be changed into invented retail
mapping. Keep immutable topology resident; upload only changed presentation
state. New material or lighting effects need their own visual and cost gates.

### M5 — Adoption and library extraction

Adopt only after complete application coverage, real battle cadence, browser
acceptance, Windows/Linux runtime evidence and resource/packaging qualification.
Review the root module diff and I12 dependency decision explicitly. Re-run the
platform dependency guard introduced in M1 across supported build tags.

Keep a rollback path while the new renderer becomes opt-in and then, if approved,
the default. Removing the old host is a distinct cleanup assignment. Extract a
small shared game-library API only after a working application shows which
resource, frame and host contracts are reusable. Do not freeze our current
game-specific composition as a generic engine API prematurely.

## 10. Checks and implementation-agent prompt

The prototype must provide **new** experiment-owned wrappers; none exists yet:

- `experiments/webgpu/tools/check`: formatting, focused CPU/layout/lifetime tests,
  vet and explicit nested-module builds with cgo disabled. Runtime tests remain
  separately labeled when a device/browser is required.
- `experiments/webgpu/tools/build`: reproducible native Go, native Rust and Wasm
  artifacts, with exact commands and native-library locations in the README.
- `experiments/webgpu/tools/serve`: localhost authored browser harness only.
- `experiments/webgpu/tools/bench`: validated configuration, existing benchmark
  lock, sequential prebuilt runs and §7 artifacts. It must reject unknown
  backends/workloads rather than silently selecting a fallback.

Document the implemented flags with runnable examples after P0; do not invent
commands for APIs that were not qualified. A routine check must not launch long
benchmarks, access retail assets, publish artifacts or download floating tools.

During iteration run affected checks and root architecture guards. Before any
landing integrate current main and follow `AGENTS.md`: `tools/check`,
`tools/check-retail`, the explicit experimental checks, applicable runtime/visual
gates, independent review, and required gates again after landing. The ordinary
short retail tier is sufficient for this presentation-only experiment unless
scope changes. Missing retail assets or a graphics environment are reported as
unrun, not passing. A sub-agent commits, reports and stops before landing.

Suggested handoff message:

> Implement P0–P4 of `docs/PLAN_WEBGPU_RENDERER.md` in a new worktree from a
> commit containing this plan, integrating current main before work. Read
> `AGENTS.md` and the plan's read-first documents. The deliverable is an isolated
> GoGPU/WGSL prototype with authored fixtures, real native/browser qualification,
> measured cadence/resource results and an explicit verdict. Pin the exact
> GoGPU commit in §3; do not substitute its tagged release. Keep production
> code, root module files and defaults unchanged. Proceed sequentially, stop
> at each failed dependency gate without inventing success, and continue any
> independent path. Commit each bounded unit. Stop after P4 and provide the
> evidence and next recommendation; do not start M1–M5 or land without the
> repository's required review and gates.

## 11. Progress

- 2026-10-10: plan prepared from the current repository and pinned upstream
  source review. Dependency gaps and proposed budgets are documented. No
  GoGPU dependencies, prototype code, GPU runs or performance results were
  produced by this planning task.

- 2026-10-10: P0 implementation started in `webgpu-p0-p4`, from
  `webgpu-implementation-plan`, with main integrated. Exact source pin resolved;
  native Go/browser compute+indexed-texture smoke executed. Rust runtime aborts
  at the pinned binding/native API boundary; see experimental README. No
  production change or adoption is implied.
