# Isolated WebGPU evaluation

P0–P4 only, per [the plan](../../docs/PLAN_WEBGPU_RENDERER.md). No production
renderer, defaults or root module changes. Use `GOWORK=off` throughout.

## Dependency qualification — 2026-10-10

Exact GoGPU source: `12620c9d50073b8591f075c97ba0c6f3b6ad92f7`, canonical
`v0.54.1-0.20260901095536-12620c9d5007`. The tagged `v0.54.0` is not used.
The downloaded pin has the RAF/visibility loop and borrowed context encoder
specified in plan §3. Device creation requests no optional features.

Initial builds: Darwin arm64 Go/Rust and js/wasm pass with `CGO_ENABLED=0`.
Windows/Linux amd64 Go cross-builds pass; neither has hardware runtime evidence.
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
