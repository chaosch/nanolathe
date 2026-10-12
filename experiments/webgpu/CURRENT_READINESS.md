# Current native prototype readiness

The worktree has a usable, bounded macOS arm64 prototype with a local path around
the pinned dependency problems. Waiting for upstream releases is unnecessary for
these qualified experiments. Shipping/native renderer behavior and dependency
pins are unchanged; main has not been modified or merged.

| Path | Default or opt-in | Evidence and limits |
|---|---|---|
| Independent model batching, authored hull aliases | Default prototype renderer batching; shared client aliases | Painter order and first canonical hull preserved; pixels/state/UV controls; aliases reduce allocation and CPU tails without a repeatable FPS gain |
| Viewport cache invalidation, generated host resize adaptation | Shared setter; generated interactive prototype host | Programmatic resize/restore and Hide/Show; source/target/injected pointer mapping; fixed sources remain fixed |
| Prepare ahead | Environment opt-in | One serial source owner, joined resize/close/boundaries; borrowed packet reads complete before next Next |
| Storage reservation | Environment opt-in | Initial mutable tiers below24MiB; normal growth retained; avoids measured capacity compilation inside the dense fixture |
| Owned async uploads and Metal pointer cache | Guarded external dependency build plus runtime async opt-in | Real completion/failure ownership checks; unknown submission/device-loss graphs remain quarantined |
| Runtime WGSL arrays | Default off; capability-gated opt-in | Native70 binding/OOB pixels, transitions/range refusals and19 UV/growth cases; zero growth pipeline builds; encoding cost remains above reservation reference |
| Presented handler | Separate guarded external build; telemetry default off | One permanent block, bounded pointer+drawableID routes, no extra drawable retains; forced GC/race/lifecycle checks and actual dense timestamps |

Use the reproducible command in README.md. `--present-handler` includes the
reviewed library prerequisites but enables no environment mode by itself. The
fast reservation command keeps runtime arrays off. Its measured scene is dense
normal-zoom geometry, not strategic icons. Near120 presentation rates are
observations from one host/fixture, not an all-platform or sustained120 guarantee.

Final-capture and terminal-packet repairs preserve the last consumed packet's
extent and exit/error result. Runtime mode is now latched at construction before
device teardown; older post-close `enabled=false` report fields cannot establish
the mode that executed. Exact sources, binary hashes and current native results
are recorded in WINDOW_LIFECYCLE_MEASUREMENTS.json and the follow-up report.

Known gaps remain: broader live dragging and RequestSize reentry during a drag,
minimize restoration, compositor occlusion, native pointer delivery/click count,
relative capture, fullscreen, display-scale migration, device-loss recovery, and
Linux/Windows/browser runtime qualification. Common client code cross-compiles
for those targets and the experiment native/Rust/wasm build checks pass; builds
do not establish hardware/browser behavior. The proposed browser Worker/SAB/
OffscreenCanvas restructuring was not introduced. GPU meshes and explicit UVs
remain resident. Full retail span/fidelity parity remains outside this result.

Independent review is recorded with the final qualification results. Selected
post-close descriptor counts are partial accounting, not a complete GPU-heap
leak proof. Missing/zero presentation timestamps around visibility/reconfigure/
close events establish no frame-drop count.


Additional practical evidence: one width drag recovered at1700×1080 and a
minimize/Raise sequence returned visibly; both bounded sessions required watchdog
SIGTERM and drained cleanly, so complete minimized scheduling remains open. The
packaged `run-play --fast` is explicit, verifies its binary and prerequisites,
resets prototype QA environment flags and preserves saved settings selection.
A 3600-frame unattended profile soak verified every presentation row, zero
capacity pipeline rebuilds and clean close, but measured114.22Hz under a growing
dense workload. This does not establish sustained120FPS or whole-GPU leak freedom.

The explicit fast profile now splits large mutable model writes into the
pinned queue's reusable256KiB staging chunks. Native19-case UV/growth checks and
all38 raw pixel files match the original path. The original3600-frame soak used
1786 camera positions due to explosion shake, so it is not a fixed-camera
performance control. The bounded neutral-pointer field diagnostic now uses the
existing NoShake gate on both A/B paths; this also changes diagnostic CRT shake
chronology. Six same-binary runs show median model upload preparation0.399→0.153ms
and median average encoding1.571→1.340ms. Display ranges overlap:111.61–118.24Hz
control versus116.87–119.11Hz chunked, so sustained120 is unproven. One control
reported a duplicate positive presentation timestamp, recorded explicitly.
Baseline and unsupported-platform writes remain unchanged; --no-chunked-uploads
restores the fast profile's control. TAIL_PROFILE_MEASUREMENTS.json records the
CPU evidence, workload matching and attribution/accounting limits.


A separate guarded `--gpu-timing` build/runtime diagnostic reads official Metal
command-buffer start/end doubles only from completed callbacks and records
immutable submission/batch identity. Native forced-GC smoke produced485 valid
CB observations, including both buffers for120 measured rows,243/243 queue drain
and0 outstanding routes. It adds no waits or extra native retains and stays off
in normal fast launches. Four same-binary3600-frame controls show complete batches and no observed CPU
encoding slowdown within variance. Mean observedGPU batches~4.18ms andp99~6.9ms
usually fit8.33ms while sourcep99~12.3ms. Rare GPU outliers remain. The~25ms
post-observedGPU→present gap includes unobservednativework/queue/display; it is
not pure compositor cost. UV/growth andfixedCoastimage/state controls preserve
allrawfiles/sidecars. Pooled interpolation on the first retained-effects read is now qualified below;
lookup/history preparation and effects remain serial. The timingdiagnostic stays off
in ordinaryfast launches.


The first retained-effects read now lends the existing joined unit-blend pool
before interpolation. New pools and blend helper dispatch respect active
GOMAXPROCS, including previously larger pools; stock WASM remains serial.
Focused race tests, the affected client gate, and a negative dispatch control
pass. WebGPU/classic/modern fixed coastal pixels and state match; WebGPU fields,
initial mesh/UV source JSON and all18 GPU sidecars match, with nonzero CRT use.
Linux/Windows/WASM client test binaries compile; runtime claims remain unchanged.

Four matched dense runs reduce average source preparation6.017→5.567ms (~7.5%)
and average sourcep9912.530→11.814ms (~5.7%). Simulation batch means/p99 do not
worsen, although one pooled trial has a7.21ms join outlier. Aggregate sampled CPU
is unchanged within this comparison and helper wake costs rise. Presentation
ranges118.75–118.81Hz control versus118.68–119.54Hz pooled overlap; steady120 and
an isolated FPS gain remain unproven. RETAINED_BLEND_MEASUREMENTS.json preserves
exact controls, workloads, profiles, allocation totals and timing scope.
The qualified build is /tmp/nanolathe-retained-blend-current/run-play --fast;
GPU timing stays off unless explicitly requested. Its private simulation timing
overlays were used only in matched measurement binaries.


Two longer12000-frame sessions preserve complete correlation in their final3600
rows despite expected bounded-ring evictions earlier. Both drain12123/12123,
with24246/24246 completed GPU observations, zero outstanding routes and no window
pipeline growth. Final-window rates118.29/119.02Hz still do not establish steady120.
The evolving scene has GPU means~7ms andp99~8.6–9.0ms; the earlier~4.18ms GPU result
is not an all-scene budget guarantee. Source means~5.05–5.10ms,p99~10.2–11.0ms.
Most late same-row presentations have source<=8.333ms; some also have observedGPU
within budget. Slow source rows largely coincide with world rebuilds. Individual
effect batches are small, and ordinary simulation joins are short with rare
outliers. Whole-session presentation Missing3/6 remains unknown despite complete
final windows. LONG_DENSE_PHASE_MEASUREMENTS.json records the conditional phase
metrics and scope limits. No further production change is justified yet: the
next attribution step is a bounded execution trace and separate Present-CB/queue
timing before another source, renderer or pacing change. The qualified launcher
remains unchanged.
