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
