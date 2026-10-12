# Parity and allocation follow-up — 2026-10-11

Completed bounded follow-up on `webgpu-p0-p4`; no main merge, production source, root
module or default changes. GoGPU remains pinned to
`12620c9d50073b8591f075c97ba0c6f3b6ad92f7`. This report supersedes the historical
fidelity/allocation snapshot in LIVE_REPORT.md for the qualified units described below. Final measured rendering candidate is 42aabae98, with later evidence wording
commits. All native gates below
use the exact pin and the CGO_ENABLED=0 combined host.

## Qualified independent paths

The windowed Metal-backed Go path runs the unchanged battle/input producer via
an external source overlay. New retained rendering work includes staged model
source/key/group composition, current Modern shifted-key saturation and keyless
BLUE bypass, HDR world accumulation, ordered projectile body barriers, the
water surface, stock sprite/beam reflection sources and conservative tile
classification, blast/heat distortion, and published submerged-feature seabed
promotion. These are presentation changes; gameplay and AI are unchanged.

Evidence is external under `/private/tmp/nanolathe-webgpu-followup-*`.
Allocation-only changes require identical final pixels and simulation state.
Native work requires clean logs and actual readbacks because the pinned host
logs deferred Finish/Submit errors without returning them.

Stock reflection output changed 4,275 pixels in a matched coastal frame. All
510 64-pixel tile flags matched the captured source color/soft planes; 23 had
bit 1, none bit 2. Model reflections and their mixed source budget are absent.
The group winner oracle matched all 96,425 pixels across four construction
groups (zero mismatches; 1,634 admitted visible child pixels, 53 blocked,
59 ties). This packet has zero deltas and no PreserveHoles cases, so it does
not qualify signed extremes, erased holes, cached-source lifecycle or integer
triangle coverage. Authored packing/oracle checks cover additional contracts.

A real RGBA16F world capture contained 8,439 RGB channels above 1, maximum
4.7265625, and no nonfinite values. Enabling distortion changed 191,062 pixels;
enabling the water surface changed 462,994, with matching simulation state.
The seabed native packet promoted one source. Its 16,588,800-byte HDR target
contained no nonfinite values; enabling it changed 96,206 pixels, principally
through viewport water sampling, with matching simulation state.

## Failed gates and corrections

The first actual DMA lens operation failed the pinned core texture usage
tracker at CopySrc-to-RenderAttachment. Public explicit transitions update the
HAL but do not settle this core conflict. That attempt stays failed. An
independent cropped raster snapshot/Load-resume path runs on native hardware:
19,968 changed final pixels, all inside the 200×100 destination; unchanged
simulation. Of 20,000 source/destination half-value comparisons, 19,901 matched
after all subsequent painting. The remaining 99 differences are not yet
attributed; later paint can alter the copied area. This is activity evidence,
not an exact whole-destination oracle. The natural crop
contains no HDR source samples or key matches. A separate authored controlled
packet at f44d5e7f3 preserved all 3,600 HDR destination pixels exactly and
rejected the matching key in all 3,600 other destination pixels, with zero
semantic mismatches. The first auxiliary oracle expected Python nearest-half
conversion for the source green channel, but the native target stored the
neighboring lower half; shader floor(channel*255+.5) still equals the exact
8-bit key. Both oracle results are preserved.

The first retained-shadow port populated 1,140,537 body texels, 1,140,714
nonclear depth texels and 189,123 projected-mask texels in a 2048×4096 atlas, but
shadow-on/off final captures had zero changed pixels. This is a failed visible
gate, despite 277 admitted subjects and draw counters. The cause is the pinned
Metal compiler/backend resource mapping: the combined shader expects uniform
and subject buffers at 3/4, while the compact commit layout binds them at 0/1.
A split module with checked emitted resource indices fixed that binding gap.
Its matched native shadow-on/off captures now differ at 94,737 pixels inside
(130,195)–(1819,1031), with identical simulation state and clean logs. A CPU source-plane
check predicts 217,395 nonzero structure/mobile subject samples; that does not
replace actual GPU output.

Earlier failed fullscreen, native loss, Rust ABI/map, and browser host gates
remain stopped. No new cross-backend parity or 120 Hz cadence claim is made.

## Allocation investigation

Initial measured coastal source+encode callbacks allocated 11.293 MB and
222,653 Go objects per draw. Reusing effect/geometry/glow packing reduced bytes
to 8.118 MB (28.1%) with identical pixels and state. Eliminating discarded
outline submissions and caching compatible explicit pipeline/group selections
reduced the group-enabled variant from 8.284 MB/226,989 objects to
4.972 MB/130,153, also with identical pixels and state. Further scratch reuse
and strictly adjacent effect coalescing reduced that variant to
2.530 MB/70,520; 5,194 layer draws became 229 without changing pixels or state.
These steps add different parity work, so their timings are not a controlled
end-to-end speedup series. Final matched measurements follow below.

The sampled allocation-object profiles attribute approximately 99% to the
pinned Metal Objective-C bridge: message dispatch, argument pointer storage,
and ABI argument arrays. The initial sampled cumulative API costs concentrate
in SetBindGroup/SetPipeline/Draw/DrawIndexed. After state caching, draws dominate.
The exact process counters are authoritative; sampled pprof estimates differ
and include profile bookkeeping outside the exact measured window.

Post-GC retained Go heap was 637.02 MiB in the baseline: production HUD and
painted terrain alone account for 128 MiB and 119.63 MiB; decoded GAF art,
atlases, mesh data and shadow-composer storage follow. These producer storage
owners are outside this prototype's permitted production edit scope. Retained
heap, per-frame allocation rate, native RSS, and GPU resources are distinct
metrics; a Go heap profile does not measure GPU memory.

GoGPU reaches Metal through the pinned Go-written goffi Objective-C bridge;
its macOS window uses purego. Both dynamically call native system libraries.
“Go-only” describes the binding/build, not Apple's graphics driver. The combined
host now builds with CGO_ENABLED=0 and goffi's documented nofakecgo tag, which
avoids duplicate runtime shims with purego. A real b40b570c8 coastal capture
matched its cgo-enabled build in final pixels, all eight diagnostic planes and
all eight state files. No dependency substitution was needed.

The preunion CPU profile sampled 7.08 seconds: msgSend was cumulative 50.42%,
goFFI CallFunction 38.70%, SetBindGroup 13.84%, and runtime memory clearing
11.02% flat. These overlapping stacks cannot be added. runtime.cgocall is also
used by the Go FFI trampoline for system-stack and GC coordination; that symbol
alone does not imply C-authored bindings.

The pinned bridge prepares a dynamic ABI signature/type list and heap argument
pointers per Objective-C message, even though selectors are cached. A narrow
upstream signature cache and typed argument path is the main next experiment.
No module-cache patch or dependency substitution was made.

## Intermediate allocation comparison

Clean Coast comparisons use seed 7, scene v5, 1920×1080 physical pixels,
DPR 2, GOMAXPROCS 2, 120 target draws/s, 240 warmup and 720 measured draws.
All eight end-state files match across the renderer variants. FPS is the
reciprocal mean of 719 valid measured callback cadences; p95/p99 use sorted
floor(p·(n−1)). GPU execution and scanout timestamps are unavailable.

| Variant | Host draw FPS | p95 / p99 cadence ms | MB / draw | Mallocs / draw |
|---|---:|---:|---:|---:|
| CPU Classic, unchanged integrated main | 48.76 | 30.07 / 31.87 | 0.311 | 1,908 |
| Modern GPU, unchanged integrated main | 86.16 | 17.53 / 19.76 | 0.948 | 24,976 |
| WebGPU fixed-capacity separate groups, b40b570c8, zero cgo | 84.53 | 16.56 / 17.26 | 4.636 | 131,289 |
| WebGPU shared group, 6fbbafbcc, zero cgo | 92.79 | 16.25 / 16.79 | 2.818 | 78,578 |
| WebGPU shared group repeat | 92.82 | 16.26 / 16.80 | 2.818 | 78,578 |

The shared group cut bytes 39.2% and objects 40.2%, with byte-identical final
pixels and state in both the qualification and measured captures. These remain
incomplete-fidelity executor comparisons, not a feature-complete renderer win.
The earlier separate-group cgo-enabled run at 4dd51c156 measured 79.81 FPS;
it is retained as a historical sample rather than used as the optimization
baseline. No build/test work ran during the clean measurements.

The updated sampled profile still assigns 98.21% of allocated objects and
93.28% of allocated bytes to the Objective-C bridge. SetBindGroup dropped from
59.84% to 32.07% cumulative object attribution; SetPipeline now accounts for
33.44%. Profile samples undercount exact process-wide MemStats totals, so
percentages describe sampled stack attribution, not exact object counts.
Retained heap remains dominated by unchanged producer storage (128 MiB HUD,
119.63 MiB painted terrain). The new post-GC sampled total is 642.39 MiB;
it is distinct from the 1.23–1.35 GB peak RSS and approximately 2.35 GB macOS
peak footprint of these shared-group runs. Neither is an exact GPU heap count.

This table predates doubled body coverage and the broader render ABI fixes.
It isolates the shared-group optimization and is not the current-build renderer
comparison. Final matching measurements supersede it below.


## Boundaries discovered by the water audit

The producer already exports the retained face topology, source/final slots,
per-piece projections and water admission needed to express a bounded model
reflection contract. The missing work is in the experimental executor: prepared
integer face rings and quad key mapping, a source-order shared vertex budget,
and final merged key/BLUE/coverage metadata. Stock-only reflections currently
use the zero-model corner budget; a mixed scene cannot be called complete.
Underwater refraction additionally needs the production 2× body raster/resolve
and marked BLUE metadata before its padded area-weighted gather can be ported.
Cached/live seed chronology still needs producer history for an exact lifecycle
claim. These are separate gates; no approximate reflected geometry was invented.

The earlier “final depth-band smoothing” limit was misleading for the Modern
renderer target. That filter is Metal's direct-slab approximation, enabled only
when direct slots actually write rank bands to world depth. This prototype
resolves isolated atlases in painter order and has no such signal; adding that
filter alone would do no useful parity work.

Relative mouse capture remains blocked by the empty exact-pinned macOS
SetCursorMode implementation; the public pointer API also lacks native click
count. Ordinary mouse/keyboard/wheel input keeps the existing production host
semantics. No synthetic double-click timing was invented.


## Render argument qualification

The existing model resolve shader declared two runtime arrays. Pinned Naga
emitted an unannotated `_mslBufferSizes` argument and size guards, but the
Metal render HAL supplies that table only for compute. The implicit native
argument slot is unknown; no alias location is claimed. Specializing slots to
the allocated capacity and the unused legacy shadow array to length one removes
the hidden argument while preserving declared bindings and executable shader
arithmetic. The b40b570c8 native gate matched 4dd51c156 final pixels, all eight
diagnostic planes and all eight state files exactly.

The separate 6fbbafbcc unit shares the model/shadow world commit layout and
group, with full declared prefixes to match emitted Metal slots. Its native
gate likewise matches the corrected baseline exactly; sources, draw order and
draw count are unchanged. Clean allocation/cadence measurements follow after
that prerequisite and optimization qualification.


## Stock feature/effect audit

Tagged static feature body/shadow seabed promotion is implemented. The earlier
ground-decal promotion limit was stale: production dry-admits/clips trails and
scorches, while wakes stay above water. Five-barrier replay, feature remapping,
shadow-before-body order, promoted-source deduplication, ordinary blending, smoke
lighting and ground clipping have no additional defect from the static audit.

Direct debris geometry still uses fan UV interpolation instead of the owning
two-chain quad mapper and does not consume its exported doubled raster. Exact
QuadSpanFaces and SupersampleIgnored counts now appear in Missing rather than
being discarded. Whole-piece debris local face lighting is now qualified by the authored
facing/back/zero-normal fixture described below. Suppressed packets requiring subject composition and exact destination
palette ALP/SHD remain outside this RGBA executor's qualified path.


## Doubled bodies, render ABI and active direct lighting

Body source/final targets always raster at 2×. LightControls bit 8 selects the
four premultiplied sample mean at commit; disabled selects the 2×2 block's
top-left sample. Group composition stays at 2× and subject opacity applies
once, at final commit. The native coastal gate changed 287,199 final pixels
while all eight state files and independent shadow planes stayed identical.
Source/final body dimensions grew from 2048² to 2048×8192.

The authored native command in `cmd/webgpu-coverage` qualifies eight cases on
the actual Apple M3 Pro adapter: grouped/ungrouped × resolve off/on × opacity
1/.5. It checks every logical source/final RGBA8 byte and 8×8 HDR output half
channel/padding after actual completion. All eight cases passed before and
after shared-pipeline integration, with zero atlas/world mismatches and no
framework error. This is hardware raster/resolve evidence; it does not qualify
retail integer span coverage or the production two-chain quad mapper. The
hardware outline width is currently half a native pixel in the doubled atlas;
production whole-native-row blocks remain an explicit executor gap.

The broader ABI audit found hidden runtime-array size arguments in body, solo
shadow, sprite, annotation, quad, ground, smoke and glow vertex entries. The
pinned render HAL does not bind those arguments. Body, layer and glow units
now specialize arrays to actual allocated tiers, retaining the full declared
resource prefixes. Exact-pinned emitted MSL checks require no hidden size
metadata and the expected native indices. Each bounded unit passed its own
real native gate: no changed final pixels; all 16 captured files and eight
state files identical; clean logs. Current-only pipeline bundles replace
transactionally after the host completion boundary, freeing obsolete growth
tiers and partial failed candidates.

Direct debris now carries BL2–BL4 face-centroid lighting through a dedicated
64-byte vertex and fragment lane. The first native attempt failed: its initial
48-byte buffer did not satisfy the new 64-byte binding minimum. A forward
constructor fix settled that dependency. The natural final coastal frame had
no observable lighting difference, so it cannot qualify active lighting alone.
An opt-in authored facing/back/zero-normal packet replaces detached replay
art while preserving retained projectile markers and simulation. The facing
16×16 interior changes all 256 pixels. Packed codes 22/44/87 match the BL2–BL4
expected RGB within one destination half ULP; disabled, back and zero-normal
interiors are exact. All eight end-state files match and both logs are clean.
The first oracle incorrectly omitted the 2.5-pixel unsheared Y separation; its
failed result remains alongside the corrected coordinate oracle. Renderer and
native captures were unchanged.

## Resource and native-call improvements

Glow now borrows the current Layers effect atlas after Layers.Prepare. Both
previous uploads consumed exactly StockFrame.Atlas, version and dirty region;
foreground overlay art has a different owner and is kept independent. The
standalone Glow component still owns its own source atlas. Its borrowed view
is rebound after replacement and never released by Glow.Close. Native final
pixels, all captured files and simulation state are identical after sharing.

Selected current descriptor texture payloads decreased by 67,108,864 bytes
from 808,942,241 to 741,833,377 (8.3%). Current buffer payloads are 53,948,064
bytes in that frame. Largest selected owners include 125.44 MB painted terrain,
four 67.11 MB body planes, independent 67.11 MB overlay/effect atlases and
33.55 MB shadow body/depth planes. This inventory excludes immutable model
texture/palette owners, older model buffer tiers, distortion/lens buffers,
driver alignment and pipeline/staging caches. Selected owners reach zero after
Close; this is not actual GPU heap measurement or proof against driver leaks.

The final common body/shadow commit pipeline partitions FirstInstance by the
allocated slot capacity, branches before record reads, and invokes unchanged
canonical body/shadow equations. Layout, shared group, paint order and draw
counts stay unchanged. A full 96-byte empty shadow subject satisfies the
reachable no-shadow declaration, independently exercised by the authored
native coverage command. This optimization passed the separate actual native
pixel/plane/state gate before its clean performance measurement.


## Current-build comparison and verdict

All runs use the standard opt-in live-battle benchmark on Apple M3 Pro,
macOS 26.6.2, Go 1.27.1, GOMAXPROCS=2, seed 7, 1920×1080 physical pixels,
DPR 2 and 120 target draws/s. They ran sequentially with no builds/tests or
external profile processing during measurement. Standard benchmark CPU/heap
profile collection and process counters were enabled for each renderer; this
is a benchmark-instrumented callback comparison, not scanout or GPU timing.
Each has 240 warmup and 720 measured draws, with 719 valid callback cadences.
FPS is 1000/mean cadence ms; p95/p99 select floor(p·(n−1)) from sorted cadences.

Coast uses v5 Expanded Confluence, pre-tick 300. The final tick is 540 with
320 units, 211 moving, 105 projectiles, 767 effects and eight nanoframes; the
view includes 281 units, 93 projectiles and 627 effects. Field uses Town &
Country with three 250-unit computer armies, pre-tick 1200; final tick 1440 has
737 units, 346 moving, 13 projectiles, 156 effects and 11 nanoframes. All eight
end-state files match among every renderer within each scene.

| Scene / renderer | Host draw FPS | p95 / p99 cadence ms | MB / draw | Objects / draw |
|---|---:|---:|---:|---:|
| Coast / WebGPU before pipeline consolidation | 92.06 | 16.23 / 16.79 | 2.819 | 78,585 |
| Coast / WebGPU current | 94.67 | 15.80 / 16.80 | 2.300 | 61,634 |
| Coast / WebGPU current repeat | 95.59 | 15.63 / 17.05 | 2.300 | 61,634 |
| Coast / CPU Classic | 54.89 | 26.77 / 29.07 | 0.311 | 1,908 |
| Coast / Modern GPU | 95.31 | 16.54 / 16.66 | 0.947 | 24,973 |
| Field / CPU Classic | 100.77 | 15.39 / 16.21 | 0.264 | 1,471 |
| Field / Modern GPU | 115.40 | 9.52 / 10.27 | 0.507 | 13,217 |
| Field / WebGPU current | 119.19 | 8.73 / 10.58 | 0.944 | 25,094 |

The common pipeline cuts another 18.4% of allocated bytes
and 21.6% of objects per draw against the same-fidelity preceding build.
Both current and repeat Coast final pixels are byte-identical to the preceding
build; all native logs are clean. The current callback FPS is close to Modern
on Coast and higher on Field, but executor fidelity differs. This is not an
exact-parity renderer performance win. Neither scene qualifies 120 Hz native
presentation, and GPU execution/scanout timestamps remain unavailable.

The final Coast sampled allocation profile attributes 97.97% of objects and
92.90% of bytes to msgSend/msgSendID, pointerArgs and argPointer. Cumulative
object attribution is SetBindGroup 41.34%, SetPipeline 13.55%, Draw 22.14% and
DrawIndexed 15.80%. The CPU profile samples 4.69 s over 7.76 s; msgSend is
53.30% cumulative, goffi CallFunction 46.48% cumulative, runtime.cgocall 46.27%
flat and memory clearing 7.68% flat. These overlapping stacks cannot be summed.
Sampled post-GC in-use heap is 639.32 MiB; unchanged HUD/terrain remain 128 and
119.63 MiB. Final Coast peak RSS is 1.29 GB (repeat 1.48), macOS footprint
2.39 GB (repeat 2.32); Field peak RSS is 2.06 GB and footprint 3.35 GB.
Resource descriptor estimates, native footprint, sampled retained Go heap and
per-draw allocations are different measurements.

**Verdict: Promising with limits; ready for windowed macOS play testing.** Keep
the shipping Modern renderer. Bounded parity/resource units are committed and
reviewed; no production migration or M1–M5 is started. The exact pin's native
fullscreen/loss and browser host dependencies still prevent acceptance.

Recommended next bounded work, before broader adoption:

1. Bring a minimal reproducible allocation profile upstream: cache immutable
   Objective-C call signatures, avoid dynamic heap argument arrays for common
   typed calls, and honor stage visibility in the Metal HAL. Re-qualify ABI,
   completion, pixel/state equivalence and matching cadence before depending
   on a source correction. This branch has no dependency fork/cache patch.
2. Port authored integer face rings, two-chain quad mapping and whole-native-row
   outlines; qualify diagonal/edge/key cases with actual readbacks. The current
   doubled hardware line width is a visible limit, not claimed parity.
3. Carry winning key/BLUE/coverage metadata through merges, then implement
   ordered shared model/stock water budgets, whole-piece reflection and padded
   underwater gather. Cached/live chronology and omitted offscreen shadow-only
   subjects require producer history/admission evidence for an exact lifecycle.
4. Resolve the native fullscreen callback deadlock and loss/reporting/input
   dependencies; complete browser fullscreen/hide/DPI/first-reflected-input
   qualification, then the stopped P4 matrix. Rust remains rejected at its
   bind-layout/map ABI gates. Windows/Linux have no hardware qualification.

## Play test and verification

```sh
/Users/daniel/.codex/worktrees/webgpu-p0-p4/nanolathe/experiments/webgpu/tools/play
```

The command builds an external cached CGO_ENABLED=0, nofakecgo application and
opens Great Divide, Arm human versus Core Modern AI, Modern rules/renderer,
no mod, using ~/TotalAnnihilation. The final bounded one-command test ran
7,320 draws (120 warmup), 67.015 s, 19 actual native input events and zero
overflow, then closed normally at tick 1926. Human had two units and AI five.
Ctrl+C selection, a Solar Collector build and camera key/wheel response were
observed: the collector completed and energy income rose +25→+45. A restore
view action met the expected bounded window closure; no further input was
claimed. Play-test timing is diagnostic, not a clean performance run. The
older longer session qualified move/right-click cancellation; minimap, menus,
save/load, relative capture, native double-click count and arbitrary DPI/display
transitions remain unqualified. Avoid the pinned fullscreen button.

`experiments/webgpu/tools/check` passes formatting, all experiment tests/vet,
browser telemetry bookkeeping (mocked), native Go/Rust-tag builds and Wasm
build. CGO_ENABLED=1 battlegpu tests/vet also pass. An independent read-only
review through 42aabae98 found no actionable defect; it ran affected tests,
decoded actual eight-case coverage output independently, and checked BL2–BL4
and coverage/opacity arithmetic against owning Modern contracts. Native gates
are root-run, separately from static review.

Both actual `tools/check` and `tools/check-retail` fail
`internal/architecture.TestNoUnreferencedExported`: its source walker includes
the nested experiment, finds unused `battlegpu.NewLayers`, and treats existing
meshscene ModelPhaseAir/ModelPhaseGround/ModelPhaseShortFeatures/PhaseGround
allowlist entries as stale because experiment references them. Other package
checks pass; retail assets were available. No production architecture guard or
allowlist was edited to hide that failure. Required whole-tree gates are not
green, so the branch is not eligible to land.

Evidence is outside the repository under `/private/tmp/nanolathe-webgpu-followup-*`.
The final summary is `nanolathe-webgpu-followup-final-perf-summary.json`; actual
coverage is `nanolathe-webgpu-followup-native-coverage-final/result.json`.
Raw native captures, exact build manifests, profiles, state checks, failed and
corrected oracles, commands and gate logs are preserved in the follow-up bundle.
Retail debug source-art/topology JSON, retail instance/pose/slot arrays and
binaries are excluded from the bundle. Only authored fixture arrays are included.


## Continued optimization: resident-source batching (2026-10-11)

The review found a local alternative to waiting for an upstream allocation
fix. Main body/outline sources have separately packed, half-open atlas
rectangles; they can be grouped by resident mesh without changing their final
painter order. `modelbatch.go` rearranges whole original instance records after
shadow cloning, retains group/pose/material identities and shadow suffixes,
and makes one body and optional outline draw per mesh. Accepted aliased
standalone source selectors fall back to the original source chronology.
Carrier shadow bodies share slots: their sequence is retained, merging only
adjacent same-mesh records with consecutive addresses. Standalone copy slots
merge only consecutive selectors. Geometry, UVs, equations, endpoint cadence,
world paint entries and resource lifetimes are unchanged. No optional GPU
feature or additional binding is required.

`NANOLATHE_WEBGPU_DISABLE_MODEL_BATCHING=1` supplies a matched reference.
The authored native gate now verifies sixteen cases, including repeated meshes
and per-instance translated poses. Both paths passed on Apple M3 Pro Metal:
zero atlas/HDR mismatches; all 32 raw image files byte-identical. The real
Coast runs, including a third run with render-state caching disabled, had
byte-identical final PNG, source/key/merged atlas, world HDR, shadow body,
height/depth, mask, pose and effect-field readbacks. Sixteen start/end state
files and the intermediate fingerprint stream match. The original prefix
intentionally changes physical order; the appended shadow-record suffix is
byte-identical. This establishes equivalence to the current hardware-raster
prototype, not retail integer-span parity.

Matched runs are sequential, CGO_ENABLED=0, Go 1.27.1, GOMAXPROCS=2,
Apple M3 Pro, macOS 26.6.2, seed 7, physical 1920×1080/DPR2,
240 warmup + 720 measured draws and standard profiles. Coast uses v5 Expanded
Confluence, pre-tick 300, final tick 540: 320 units/281 visible, 406 retained
instances, 3692 pieces and 51551 triangles. Field uses Town & Country,
pre-tick 1200, three 1000-unit armies; final tick 1440 has 2600 units/349 visible,
784 retained instances, 7301 pieces and 68512 triangles. Cadence is callback
start-to-start, excluding the first sample; quantiles select floor(p·(n−1)).
It is not GPU execution or native presentation timing.

| Case | Model draws in final frame | MB/draw | Objects/draw | Mean host draws/s | p50/p95/p99 cadence ms |
|---|---:|---:|---:|---:|---|
| Coast reference |1805|2.300|61634|100.34|8.90/14.42/15.61|
| Coast batched |1102|2.023|55284|101.94|8.72/14.46/15.08|
| Field,3000 initially staged, batched |1377|1.781|46806|74.97|10.27/24.13/25.70|

Coast encode mean/p95 is 2.776/3.169 ms reference and 2.586/2.989 ms
batched. Field source preparation mean/p95/p99 is 7.964/20.122/21.189 ms;
encode 3.136/3.926/5.004 ms; completion wait 1.240/4.161/5.503 ms. Its CPU
profile attributes 44.77% cumulative samples to `metalBenchmarkSource.Next`,
19.31% to the simulation/viewer step and 14.39% to `metalVisuals.Prepare`;
these overlap and are not sums. The source includes simulation, visual
recording and packing: it is not GPU rendering time or an upstream blocker.

The per-frame `driver_darwin.go` WaitIdle is a runtime boundary before every
ordinary draw, separate from resize/final readback. **Correction from the exact
public-API audit:** public Queue.WriteBuffer already uses PendingWrites staging;
its lower-level Metal HAL direct copy was previously mistaken for this path.
An application buffer ring is therefore not inherently required. The actual
risks were direct HostWritable texture uploads, copy-only destination lifetime,
and false completion on failed completion-block allocation. See the owned-upload
follow-up below. Resize, capture and teardown retain checked completion boundaries.

A steady120Hz draw cadence is not established. A committed30Hz presentation
producer remains the next CPU architecture boundary, sampled at draw-time
alpha/time without blocking on authoritative simulation. Preserve carrier/group/
world order, effects and the30Hz simulation contract. Queue-owned immutable
staging can protect mutable uploads with one renderer; do not clone atlas/geometry
or assume an app ring is necessary before auditing the actual public path.

The body batching code is portable Go/WGSL and keeps normalized UV attributes
resident. The real-game command/host remains macOS-only. Linux/Vulkan,
Windows/DX12 and browser WebGPU need host/input/lifecycle integration and real
runtime qualification; cross-compilation alone does not deliver those hosts.
Browser optional compute/indirect/bundles cannot become baseline assumptions.

Evidence: `/private/tmp/nanolathe-modelbatch-coverage-native-{on,off}`,
`nanolathe-modelbatch-coast-{off,on,nocache}`,
`nanolathe-modelbatch-field3000-on`, matched `*-debug.json.*` native fields,
and `/private/tmp/nanolathe-modelbatch-build/build-manifest.json`. The sandbox
attempt selected Empty/software and is explicitly failed; only the native
Metal gate counts as qualification. Root/default module and shipping renderer
remain unchanged. No push, merge, deployment or upstream issue was performed.


## Metal comparison and bounded source/bridge experiments (2026-10-11)

Main `5dbc6fe222cce820f4f54e21be12c5989bb1f0c5` contains a materially more mature
native renderer. Its `internal/platform/metalrender` and `internal/platform/mtl`
packages are identical to this worktree's versions. The matched Metal binary
was built from the same worktree game/producer source as WebGPU, without the
experimental command overlay; main was only read. Current main additionally
contains unrelated newer online/UI changes and an unrelated untracked conversion
directory. No code, Git state or artifacts in main were changed.

The matched Coast benchmark has byte-identical benchmark-state streams for
Metal and every WebGPU variant below. Counts, seed, resolution, simulation,
30Hz tick publication and four render phases per tick are equal. This is stronger
than comparing a simplified model benchmark to the full game. Native Metal's
fidelity still differs: it rendered 44,301 model reflection instances over the
960 total frames, while WebGPU still reports model reflection/refraction gaps.
Native and WebGPU images are not established renderer-parity equivalents.

Metal's measured 720 Coast rows have distinct source times and `(tick,alpha)`
values, distinct completed submissions with positive GPU spans, and 720 distinct
increasing drawable presentation timestamps. Every row has
submit <= GPU start <= GPU end <= presentation. Ticks361..540 each have
alpha0,.25,.5,.75. The fresh-drawable span rate is118.84Hz; p50/p95/p99 presentation
intervals are8.333/8.339/8.340ms. Five intervals exceed12.5ms. This is near120 fresh
rendered/presented updates, not exactly120 and not proof of different pixels on
every refresh or direct scanout timing. Native submission-to-presentation p50
is31.63ms with three frames in flight: throughput is not single-frame latency.

For the same Field3000 fixture and final state, native Metal presents719 of720
measured rows, at104.82 fresh drawable updates/s; one presentation remains
unavailable after the bounded drain. Its GPU p50/p95/p99 is3.743/4.907/5.771ms,
but source Next is4.475/20.006/21.261ms. Native Metal therefore also fails steady
120Hz for this workload. Its source mean7.896ms is similar to WebGPU's7.964ms;
source production includes simulation and visual recording, not just GPU upload.

### What explains the gap

Native `run_darwin.go` prepares source and packed upload records on a persistent
worker while the render thread presents/paces. `gobridge_darwin.go` waits only
for the reused slot in a three-entry mutable resource ring and commits without
an immediate global drain. WebGPU had synchronous Next and per-draw WaitIdle.
The native ring is one ownership design. Public WebGPU buffer writes already
stage immutable bytes on this pin; the immediate-copy concern applies to the
lower-level HAL path. Public mutable texture writes and failure/lifetime edges
need the reviewed repair before removing the per-draw drain.

Native Coast source p50 is2.741ms plus separate packing0.146ms, versus WebGPU
Next2.428ms before preparation/encoding. Native is not faster at median source
production. Native CPU encode p50 is0.684ms versus WebGPU2.542ms. Native uses
fixed-arity allocation-free integer ObjC sends and cached buffer bindings;
pinned wgpu prepares goffi signatures and argument/type arrays per general send,
and binds resources to both shader stages. Process Go allocations are about
399/draw native versus55,284/draw WebGPU, with native's scope additionally
including final drain. Object-share attribution is not CPU-time attribution.

Both body paths use2x raster sampling. Native skips eligible direct depth-slab
subjects in body-atlas packing/rasterization; WebGPU allocates isolated body
slots for all subjects. Coast's final391 slots include263 direct subjects,
accounting for56% of doubled rectangle pixels. Native body planes use
4096x1152 and94,371,840 descriptor bytes; WebGPU uses2048x8192 and268,435,456.
Native uses20 bytes/pixel across body planes versus WebGPU16: formats do not
explain its smaller allocation. Native also height-sorts slots and limits
attachment clears/stores to the current page; WebGPU retains a power-of-two
extent and clears/stores it. Native does not persistently cache body pixels:
body/shadow raster, face preparation and group composition run every frame.
Input tables and generations are retained. Native merges groups in place;
WebGPU uses a separate full merged plane and copies ungrouped subjects.

### Local experiments, preserved rather than over-promoted

`sourceworker.go` implements one serial Next request. `renderer.encodeSourceReady`
first copies/uploads every borrowed frame operand and derives owned paint
records; only then may the next Next reuse producer slices while current
commands encode. No GPU handle/encoder crosses to the worker. Warmup boundaries,
final capture and joined shutdown stop preparation. Input data is reused only
after join; predictive source time follows the existing native host contract.
Input is sampled one draw ahead; interactive cursor/lifecycle qualification is
still required. Independent review found and fixed an unused-worker startup/close
race by capturing channel locals; tests cover it, serial join, error shutdown
and measurement boundaries, including the Go race detector.

Results are mixed, so the worker remains opt-in through
`NANOLATHE_WEBGPU_PREPARE_AHEAD=1` when Options.PrepareAhead supplies a policy.
The selected implementation joins the worker before error-checked WaitIdle.
An attempted wait-before-join ordering is discarded. That measured worker unit retained
one protected mutable set. The later owned-upload diagnostic below removes the
per-draw drain only with explicitly checked dependency contracts.

| Coast variant | Mean callback Hz | p50/p95/p99 callback ms |
|---|---:|---|
| Qualified batched default |101.94|8.72/14.46/15.08|
| Pointer-cache diagnostic, synchronous source |108.97|8.41/14.57/15.69|
| Late source worker |103.92|8.61/14.43/15.78|
| Early worker, wait before join (discarded ordering) |111.73|8.40/13.70/15.68|
| Early worker + cache, wait before join |108.97|8.43/14.04/15.74|
| Early worker, join before wait (selected opt-in) |99.25|8.93/15.05/17.32|

Field3000 callback means: batched default74.97Hz; early worker wait-first71.30Hz;
selected join-first77.57Hz, with p50/p95/p99=10.28/23.39/25.37ms. These are bounded
single runs, not thermal/background-load-controlled repeated estimates. The
worker and pointer cache gains are not reliably additive: changing encode time
changes outstanding work observed by WaitIdle. WebGPU still cannot expose
actual GPU/presentation clocks through the borrowed context, so completion-wait
wall time must not be called GPU execution. Every source-worker variant retains
720 distinct tick/alpha pairs and byte-identical final state and PNG to its
corresponding default. Coast captures also retain every raw GPU field byte.

`modelpacking.go` preserves logical slot IDs but can height-sort allocation
traversal. Enable only with `NANOLATHE_WEBGPU_COMPACT_SLOT_PACKING=1` or the
authored gate's `-compact-slot-packing`. It halves Coast body planes to128MiB
(2048x4096); selected texture descriptors fall from741,833,377 to607,615,649bytes.
It did not materially improve FPS (102.28Hz). The authored16-case gate passes,
but it uses equal-height slots and does not establish production relocation
parity. Normalized production slot comparison changed3,595/7,612,076 source
pixels and8,045 key pixels (key differences<=1). Final PNG changed3,323/2,073,600
pixels, maximum8-bit channel delta106. World HDR changed2,870 pixels. Moving
rectangles changes floating-point raster/interpolation edge decisions. Therefore
compact packing remains opt-in and is not an accepted allocation-only fix.
Shadow, pose, terrain, stock reflection and seabed readbacks stayed identical.

The exact wgpu v0.34.2 pointer-call cache is preserved as
`patches/metal-pointercache-diagnostic.patch`. It uses immutable signatures and
pooled exclusive argument/result/CIF frames; mixed float/struct sends retain
the original implementation. The13 explicit-argument bound matches goffi's15
including self/selector. Concurrent16x500 Foundation round-trips, return kinds,
register/stack bounds, nil/error reuse, recursive callback+GC, checkptr=2 and
race tests pass on macOS arm64. Microbenchmark:111.8ns/136B/5alloc reference versus
66.15ns/0B/0alloc cached. Coast goes from2.023MB and55,284 objects/draw to0.167MB
and1,248 objects/draw with unchanged images/state. Shipping benchmark binaries
remain CGO_ENABLED=0; race testing alone used existing CGO-enabled test tooling.

`tools/build-play NEW_EXTERNAL_DIR --metal-cache` applies the patch only to a
private copied module through `tools/prepare-metal-cache`, verifies v0.34.2,
and records the replacement and patch digest. Default graphs, module cache,
shipping renderer and root sources remain unchanged. This is a reproducible
macOS arm64 diagnostic, not an adopted fork or other-platform ABI qualification.

### Portable architecture and remaining dependency limits

Resident indexed geometry, normalized UVs, compute pose interpolation, fragment
depth and ordered composition have portable Go/WGSL paths. The body shader uses
exactly the baseline eight storage bindings; new direct-path records should
extend packed instance/material data rather than require a ninth binding.
Eligible direct subjects can bypass atlas raster, with atlas fallback for groups,
erasure/key rules and other ordered cases. Metal's in-place texture composition
can require a ping-pong alternative on baseline WebGPU; that is an implementation
and bandwidth tradeoff, not evidence that equivalent semantics are impossible.
Native shared-memory mapping, raw ObjC call assembly and completion blocks are
backend-specific. Keep a shared renderer/model ABI with small backend host and
completion adapters, using Metal as fidelity/performance reference; four separate
full renderer implementations would multiply correctness work.

A hard current-pin lifetime issue is Metal completion-block creation failure:
`hal/metal/queue.go` reports the submission complete immediately. Native Queue.Poll
must not be treated as unconditionally failure-safe. GoGPU Finish/Submit happens
after OnDraw and logs errors rather than returning them. Browser public Poll and
WaitIdle are no-ops; use its real post-submit onSubmittedWorkDone promise and
asynchronous yielding. The existing browser harness already has that bridge.
A local bounded backend/host repair or pinned fork is possible; there is no need
to wait indefinitely for an upstream release. Latest upstream's general ObjC
allocation path remains per-call; exposed render bundles alone do not implement
Metal render bundles.

The next substantial CPU unit is a committed30Hz producer packet owner,
independently sampled by draw-time alpha/time. The exact public API supports one
renderer plus queue-owned staging after the local ownership/completion repair;
no application-level buffer ring is intrinsically needed. Browser remains on the
shared renderer path, with its real post-submit promise for capture/completion;
user requested deferring browser-specific worker/thread machinery. Do not clone
entire Renderers/atlases, remove a wait blindly, alter simulation cadence, or
claim120Hz from target settings. Linux/Windows/core wasm builds are compile
checks; real-game host and asynchronous runtime qualification remain macOS-only.

All fifteen measured variants and native timing scopes are summarized in
`METAL_GAP_MEASUREMENTS.json`; raw runs/build manifests remain in the named
`/private/tmp/nanolathe-*` directories. Experiment tools/check passes tests,
vet, native/Rust/wasm probe builds and mocked browser telemetry; portable core
builds pass for Linuxamd64, Windowsamd64 and js/wasm. Native reads were sequential,
with no heavy builds/tests concurrent. Main, shipping defaults, push/merge/deploy
and external messages remain untouched. Steady120Hz is not established.

Final default validation (`nanolathe-final-default-coast`) confirms worker=false,
compact=false,720 distinct tick/alpha inputs, and byte-identical final PNG/state
and every captured GPU field against the qualified batched reference. Its
callback mean97.07Hz and p50/p95/p99=8.65/15.54/16.56ms also demonstrate run
variability: neither the small batching timing delta nor worker improvements
should be sold as a controlled repeatable FPS estimate. Command-count reduction
and allocation/pixel equivalence are stronger established results. The final
build manifest records106 experiment Go/WGSL source hashes and CGO_ENABLED=0.


### Owned-upload and submission-failure diagnostic

`patches/wgpu-owned-submission.patch` and `patches/gogpu-owned-submission.patch`
apply only to guarded private copies of the exact pins. Build with
`tools/build-play NEW_EXTERNAL_DIR --metal-cache --owned-submission`. Enable the
per-draw drain removal with `NANOLATHE_WEBGPU_ASYNC_UPLOADS=1`; the host rejects
that setting unless both dependency capability contracts exist, and the device
contract is true only for the reviewed Metal HAL. Defaults/dependency graph and
module cache remain unchanged.

Public buffer writes own copied staging bytes. Copy-only destination ResourceRefs
now survive flush until real completion, and discard/rejected batches balance
refs. Reviewed Metal mutable texture writes also stage immutable bytes. Dirty
rectangle packing honors source Offset/row stride, copies only the requested
texels including a partial last row, and bounds the staging write. Complete block
rows/layers split into separate GPU staging allocations under the belt limit;
the1GiB CPU packing bound is distinct from the64MiB default per-GPU-allocation
limit. The first real setup failed at the earlier64MiB packing cap and produced
no timing result; this exposed and fixed a partial-constructor diagnostic access
panic. Failed callbacks now abort before shared-frame submit/present.

Metal validates all CBs and allocates both completion blocks before any commit.
Block-allocation failure rejects the whole batch; empty HAL submission invents
no work. Failed/nil idle barriers publish no completion or semaphore credits.
Submission and barrier/maintenance serialize, including mapping. Known rejected
CBs release unsubmitted ownership; error after possible execution preserves a
snapshot of the full batch, marks CBs quarantined, poisons further writes/submits/
maps and preserves native teardown. A final void Device.Release error is exposed
to the host; unknown completion retains device, instance, surface, window and
command graphs. CPU source/profiling cleanup still runs. The patch does not
implement device-loss recovery or retry/rollback of rejected tracker state.

Injected tests cover completion allocation/barrier failures, barrier-vs-later-
submit serialization, immutable uploads, balanced lifetime/cancellation,
short/offset/dirty texture rows and chunk boundaries, caller-slice reuse,
unknown-submit Release/Free/map guards, hidden Finish/Submit/headless-readback
errors, callback abort and final release wait failure. Both dependency full
suites and focused race suites have passed during this unit; final-source checks
and native results are recorded below. Non-Metal texture packing is retained
from the pin; compressed layout/runtime qualification there remains incomplete.
Metal completion callbacks still establish safe retirement rather than proof
of successful execution; native GPU errors/loss are not qualified.


Matched native results (same guarded patched binary plus pointer cache,
GOMAXPROCS2,1920x1080,DPR2,seed7,240 warmup+720 measured draws):

| Fixture / mode | Callback Hz | Cadence p50 / p95 / p99 ms |
|---|---:|---:|
| Coast, per-draw drain |100.92|8.52 /15.73 /16.73|
| Coast, queued uploads |115.99|8.35 /10.21 /15.26|
| Coast, queued uploads + source ahead |119.53|8.34 /8.88 /10.20|
| Field3000, per-draw drain |78.48|9.88 /22.97 /23.89|
| Field3000, queued uploads |79.90|8.71 /23.63 /24.66|
| Field3000, queued uploads + source ahead |87.06|9.07 /21.31 /22.60|

Every case retained720 distinct tick/alpha samples and720 distinct prior-submit
indices (241..960), submitted/completed index963 at checked close, byte-identical
final PNG and all eight end-state files against the prior fixture reference.
Coast's18 and Field's14 GPU sidecars match their new synchronous controls byte
for byte, including raw model/pose/slot records and source/key/shadow/world fields.
The runs are sequential bounded samples, not repeated thermal-controlled tests.
Callback cadence is not a presented/scanout clock. Near120Hz Coast mean does not
establish steady120FPS or dense all-platform success.

Field source p95 stays about19–20ms and source-ahead join p95 remains18.13ms.
The fixed fixture explicitly runs a synchronous simulation step every four draw
phases; keeping its workload/state contract means that burst can still block
presentation. Interactive Metal already uses asynchronous simulation and a
borrowed per-draw presentation sampler. A first independent-simulation field
run is excluded from dense proof: edge scrolling moved the scene out of view,
and608/720 measured frames had zero model instances. Do not promote its117.75Hz
callback result as dense rendering.

The source boundary needs owned publication inputs, not reused finished frames.
Per-publication pose endpoints, committed identity/flags/fog and stable subject
mappings can be owned separately. Base RetainedBattle.Frame is already cached
between draws of a pair. Keep current per-draw effects/CRT, camera-dependent
painter order/admission, rounded body/shadow bounds, arrival sampling, water
phase/drift, HUD and annotations with a presentation owner. Unit and feature
bucket formulas intentionally differ at non-multiple-of16 camera Z. Copying a
LiveFrame header or rendering one finished30Hz frame four times would silently
change those semantics. See cmd/nanolathe/metal_{host,benchmark}_darwin.go,
internal/client/retained_effects.go, retained_model_projection.go, world_draw.go,
and internal/meshscene/model_composition.go, retained_shadow_types.go.

Final dependency full tests pass. Focused ownership/failure/chunk/retirement race
tests pass using CGO1/nofakecgo testing tooling; shipping binaries are CGO0.
Default full wgpu vet reports six unsafe.Pointer warnings: four stock native
FFI/software sites and two pointer-cache diagnostic test sites. No warning comes
from owned-upload code. Vet with only the unsafeptr analyzer disabled passes;
this is not a clean default-vet claim. Full GoGPU tests pass; default vet reports seven pre-existing Darwin FFI
unsafe.Pointer sites, and its remaining analyzers pass with unsafeptr disabled.
Both patched dependency roots build with CGO0 for Linuxamd64, Windowsamd64 and
js/wasm. These are compilation checks; the asynchronous runtime marker remains
reviewed Metal only. OWNED_UPLOAD_MEASUREMENTS.json records exact
quantiles, counts, equality/hash evidence, paths and excluded attempts.


Patch scope is773 production added lines and252 removed lines across the two
saved diffs, plus681 regression-test lines; the wgpu total includes the existing
public format/block geometry table copied into portable packing. This is a
nontrivial diagnostic fork, not a shipping dependency adoption. The minimum
correctness areas are (1) owned pending uploads including initial/replaced
textures, (2) real Metal completion and checked idle/teardown, and (3) host error
propagation/callback abort. Conservative unknown-submit guards avoid unsafe
recycling on HALs that can fail after executing work. No shader compiler, render
backend, adapter or game renderer was reconstructed. An app-only mutable ring
would still depend on truthful library completion and rejected/unknown submit
semantics; duplicating already staged uploads alone does not remove those risks.


The repository experiment `tools/check` passes after these edits: formatting,
unit tests, default experiment vet, native/Rust/wasm probe builds and mocked
browser telemetry. Its nonfatal self-module stat-cache warnings reflect the
read-only module cache; no cache permission was bypassed. The six measured
binaries' source/patch digests match the preserved diagnostics. Default build
metadata now points to report.json for both preparation and asynchronous upload
policy. Main and native Metal implementation sources remain unchanged.


## Bounded storage reservation and dense interactive control — 2026-10-12

The stable-camera diagnostic restores the staged field camera after entry placement and appends a centered pointer sample. It is opt-in, bounded, requires a report directory, and leaves ordinary entry and fixed benchmark input unchanged. Normal zoom is required: `.5` uses production strategic icons and is not a body-throughput qualification. The host overlay and renderer now report copied source phase durations, requested-sample/ready ages, publication tick, camera, renderer preparation phases and actual pipeline-create counts. Ages are not a publication wall-age or presentation timestamp.

Confirmed stalls include capacity-dependent fixed-array shader compilation: an instrumented unreserved run created 33 model and 48 layer pipelines during 720 measured frames. A split-pipeline experiment did not improve measured throughput and was restored to the baseline; its rejected patch is preserved only in `/tmp/nanolathe-layer-split-rejected.patch`.

`NANOLATHE_WEBGPU_RESERVE_STORAGE=1` is an opt-in shared renderer diagnostic. It reserves separate endpoint, pose, instance, material, light, slot/group/member and effect buffer budgets before play and compiles the optional group family during setup. The combined initial mutable buffer tiers are below 24 MiB; textures and immutable mesh/UV data are unchanged. Ordinary geometric growth remains enabled. Defaults, dependency pins, shipping Metal and browser execution are unchanged.

With a private settings snapshot at nominal game speed 10, the dense 3×1000-army scene at 1920×1080, normal zoom and seed 7 completed 720 measured draws with 26,970–30,046 body triangles every draw. GOMAXPROCS=2 measured 114.263 callback Hz, callback p95/p99 13.013/15.964 ms and encode p95/p99 1.944/2.126 ms. GOMAXPROCS=4 measured 119.700 callback Hz, callback p95/p99 8.565/8.740 ms and encode p95/p99 2.093/2.392 ms. Both created zero measured model/layer pipelines, consumed 720 unique tick/alpha identities, reported no run error and drained submitted/completed indices to 843/843. These are callback cadences, not WebGPU presentation measurements. The native Metal control produced 720 distinct real presentation timestamps at approximately 120 Hz, with GPU median/p95 4.786/5.668 ms. The control's host is temporarily instrumented; its native renderer is unchanged.

The first larger reservation observed 2.42 s host/renderer initialization; the reduced plan observed 2.48 s in its initial run and 0.67 s on a subsequent run. Metal shader-cache state is uncontrolled, so these observations do not establish a stable cold-start cost. The reduced dense plan reports 70,747,472 current buffer descriptor bytes including immutable geometry; this is not actual GPU heap accounting. A matched Coast pair adds 17,308,864 descriptor bytes and leaves texture descriptor bytes identical.

The user's saved game speed changed to 15 during investigation. A reference run then rejected 300 lead-in calls producing 450 ticks. No settings were changed in place: subsequent matched runs use a private snapshot with only speed changed to 10. Earlier unsnapshotted preference runs are observations, not matched causal timing comparisons.

Fixed-state Coast qualification against the exact preserved `674428f3e` binary passes: final PNG, final fields, complete benchmark state and all 18 GPU sidecars are byte-identical, including poses/instances, source/key planes, shadows, HDR world, lighting, reflections and glow. This checks explicit resident UV/material output as rendered by the existing prototype; it does not claim retail span-raster equivalence. The exact reference benchmark clock still advances one 30 Hz tick per four 120 Hz draws.

The native authored oracle passes 19/19 cases with `-pose-growth`: a 3-pose packet uses a 2 MiB pose buffer, 40,000 poses grow it to 4 MiB and create four replacement model pipelines, then shrinking to three poses retains that 4 MiB tier and creates no pipeline. Every authored atlas/world pixel and the last GPU-computed pose is checked after completion. Floors do not truncate growing workloads.

The remaining clean architectural alternative is native render runtime-array size binding, which could remove capacity-dependent shader variants without reservation. The pinned Metal backend implements that metadata only for compute; current upstream main still calls render compilation with `bindSizes=false` (`hal/metal/device.go`), independently verified against the primary repository on 2026-10-12. A narrow local backend diagnostic can be qualified without waiting for a release. It must preserve native binding indices, effective bound ranges, robust out-of-bounds reads and pipeline/resource lifetime before replacing the fixed-array workaround.

Raw evidence, binary/settings hashes, per-case quantiles and all pixel equality results are recorded in `STORAGE_RESERVATION_MEASUREMENTS.json`. Windows/Linux/browser runtime tests and WebGPU GPU/presentation timestamps remain unqualified.


## Private render runtime-array qualification (2026-10-12)

The Metal render-size ABI has a local diagnostic implementation; no upstream adoption is required for this investigation. The guarded dependency patch and optional renderer candidate are preserved in `patches/wgpu-render-runtime.patch`, `patches/runtime-patch-manifest.json`, and `patches/runtime-wgsl-candidate.patch`. They target the reviewed owned-upload plus pointer-cache copy, not pristine upstream or the module cache. Default renderer sources remain at the verified fixed-array/reservation baseline.

70 exact native float pixel cases cover separate and mixed runtime/fixed stages, shuffled binding/layout inputs, struct prefixes, growth beyond 4 MiB, exact dynamic tails and explicit spans, robust OOB zero reads, direct/indexed/indirect drawing, negative base vertices and nonzero index/indirect offsets with multiple records. Four same-pass pipeline-switch pixel cases and seven malformed-range recording refusals pass; rejected ranges produce no command buffer or submission. Independent code review corrected binding-map gaps, eager validation, aligned and fixed shader minima, stage/vertex-slot collisions, and retained state restoration.

The optional renderer candidate passes all 19 model/UV cases, including 40000-pose growth and shrink with zero model pipeline creation during growth (fixed-array baseline creates four). All 19 atlas and HDR-world files match the fixed oracle. Full Coast final PNG, fields, simulation state and 18 GPU sidecars are byte-identical. A matched 720-row field:1000 run at GOMAXPROCS4 using the private nominal-speed10 settings snapshot retained unique tick/alpha pairs, zero measured model/layer pipeline creation, and completion843/843. It ran near119 callbacks/s but added CPU encoding cost; see `RUNTIME_ARRAY_MEASUREMENTS.json` for exact quantiles.

The latest dependency patch now reuses Metal's copied inline size metadata until pipeline/binding/vertex state changes; all native tests pass, but this refinement still needs fresh 19-case and full Coast/dense qualification before optional renderer source adoption. GPU/presentation telemetry is not implemented. Independent review recommends observing existing completion callbacks for GPU spans and bounded retained-drawable polling for actual presentedTime; callback FPS and render completion cannot establish display cadence. Global-block presented callbacks cannot safely unpin their Go block on a timeout.


### Metadata reuse and optional source adoption

The metadata-reuse refinement passed the19-case growth/UV gate again with0/0/0 pipeline creates and repeated full Coast final PNG/fields/state plus18-sidecar byte equivalence. The same nominal-speed10, centered field:1000, GOMAXPROCS4 run produced720 unique measured tick/alpha pairs, zero measured model/layer pipeline creation, completion843/843 and119.59248 callbackHz. Encode CPU mean/p95/p99 were1.76997/2.56642/3.03454ms, still above the reservation reference. This is a qualified optional growth-stall alternative; no FPS improvement is claimed.

The generic WGSL/runtime cache changes are now adopted behind the default-off environment flag and native capability marker. `tools/build-play --render-runtime` reproducibly creates and verifies the private dependency copy. Fixed-array declarations/defaults and the pure-Go/four-platform architecture remain the fallback. Actual drawable presentation telemetry is the next separate private diagnostic.


## Bounded drawable presentation polling investigation

The private telemetry patch adds no callback blocks, GPU work or waits. It takes at most2 extra drawable retains, proactively retires unresolved observations to at most1 before timeout-disabled acquisition, and releases each extra retain exactly once on observation/pressure/timeout/reconfigure/close. Independent review and synthetic ownership tests plus race checks passed. A separate driver-close path handles quarantined device shutdown; actual acquisition texture ownership is also balanced on a nil presentation-command-buffer failure.

The native dense run finished720 unique source tick/alpha rows at119.62375 callbackHz with submission843/843 and no run error. Telemetry retained and released843 drawables, ended with0 outstanding, and reached high-water2. However only3 of843 actual presentedTime values were observed;839 observations retired for acquisition pool credit and1 at final close. This coverage cannot establish display cadence or frame drops. The polling code is preserved as a diagnostic patch and is not adopted. No causal overhead claim is made from the off run, whose startup overlapped race compilation. Actual occlusion/minimization/resize and pixel/pacing A/B gates remain open.

Next investigate a single immutable permanent process-global presented-handler block, shared across drawables with a bounded drawableID/row-generation lookup and late-callback discard. This may request timely presentation feedback without accumulating per-frame pinned Go block literals or retaining drawables. Its ABI/identity/lifecycle must be reviewed before native use; current global blocks cannot be unpinned just because a timeout elapsed.


## Shared presented-handler diagnostic

The isolated Mac ARM64 diagnostic uses one immutable, permanently rooted process-global block/trampoline, a bounded4096 route/value registry keyed by drawable pointer plus per-layer drawableID, and generation/token checks. Registration precedes native presentation; callbacks remove routes before typed presentedTime access and tracker publication. Timeout, reconfigure and close remove Go bookkeeping but never unpin the native helper. It adds no drawable retains, GPU work or waits. Independent review and synthetic race cases pass. The private class lookup corrects an inherited dlsym-address dereference inconsistency without editing the existing event helper. Ten forced-GC C block-copy/release and Objective-C dispatch checks pass; actual native callbacks after forced collections and final pixel/sidecar equality are separately qualified.

Final same-binary, sequential dense off/on runs recorded callback interval cadences 118.45999/118.38457Hz. On measured 720/720 uniquely matched positive presentation timestamps, 118.51607 actual displayHz across719 consecutive pairs, and interval median/p95/p99/max 8.33337/8.33929/16.66350/16.66675ms. All720 source tick/alpha identities were unique, everydraw carried dense body triangles, no model/layer pipelines were created during measurement and completion drained843/843. Final owner/global registry counts are0 with one permanent helper and no extra drawable retains. This establishes native display coverage, not a speedup.

Some double-refresh intervals in earlier on runs coincided with roughly10–11ms source or encode bursts; others were not assigned. Without GPU/compositor timing this is correlation, not complete causality. The final matching comparison, per-frame tail context, Coast PNG/fields/state and18-sidecar equalities, forced-GC counts, exact binary/source/settings hashes and test logs are recorded in PRESENT_HANDLER_MEASUREMENTS.json. The first measured callback row has interval0; this report excludes it when computing callback cadence, and also preserves the earlier mean-including-zero formula for comparison.

The handler remains a preserved default-absent diagnostic patch, not a shipping dependency change. Native occlusion/minimization/resize and other platforms are open; small overhead cannot be excluded by one sequential pair. Optional render runtime arrays already eliminate growth-time pipeline rebuilding but do not demonstrate higher steady FPS. The next useful change is a bounded CPU-tail reduction validated with the same display coverage and fixed Coast/UV output, rather than waiting for upstream or extending telemetry indefinitely.


## Authored hull-name lookup allocation reduction

The actual dense CPU/allocation profile identified Client.HullModel repeatedly normalizing unchanged authored model names during pointer picking: ckey→strings.ToLower accounted for1,376,277 sampled objects (38.8% of allocation-delta samples) and approximately0.43s of5.66s CPU samples. The common pure-Go client now pre-binds exact authored aliases to the existing first canonical hull during immutable registry construction and tries that exact lookup before the unchanged Unicode lowercase/whitespace fallback. It adds no runtime cache or mutation, alters no hull arithmetic/precedence and needs no platform branch. Independent review found no correctness blocker; the focused authored case locks duplicate precedence, fallback/misses and zero exact-alias allocations.

Three sequential runs per build (A1 B1 B2 A2 A3 B3) each produced720 unique source identities and720 unique matched positive display timestamps with dense bodies everydraw,0 model/layer pipeline creates and drain843/843. Median allocation count fell 5,088,675→2,378,509, bytes 378,118,776→359,868,680; host-step p99 4.50516→4.10739ms and preparation-join p99 3.41318→2.60662ms. Display ranges 118.84256–119.66663/119.50096–119.50097Hz overlap; baselineA2 was faster than all candidates. This qualifies a durable allocation/CPU-tail reduction, not an FPS guarantee or end-to-end latency improvement.

The affected client repository gate passes formatting/build/vet/all short synthetic tests. The fixed Coast final image/fields/state and all18 GPU sidecars remain byte-identical. Classic and modern shared-renderer fixed-clock controls also preserve images/state. The rebuilt native19-case model/UV growth oracle passes; all38 atlas/world files equal the fixed oracle, with zero growth pipeline creation and checked tail poses. The shared client also compiles with CGO disabled for Linux amd64, Windows amd64 and browser wasm; this is compilation evidence, not runtime qualification. Exact source/binary/settings hashes, commands, per-run tail/age/coverage metrics and profile references are in HULL_ALIAS_MEASUREMENTS.json. WINDOW_LIFECYCLE_QUALIFICATION_PLAN.json specifies the next bounded native resize/visibility/close unit; minimization restoration remains a verified-UI requirement, not an assumed public API capability.
