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

Known gaps remain: actual live dragging and RequestSize reentry during a drag,
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
