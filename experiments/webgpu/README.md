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
to 117.29 MB, Go live heap from about 3.63 to 3.72 MB; browser live Go heap
from 3.175 to 3.187 MB. These short cycles do not prove a driver-memory plateau.
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
