# Dependency patches

The WebGPU prototype builds and plays on the stock pinned dependencies: GoGPU
`v0.54.1-0.20260901095536-12620c9d5007` and wgpu `v0.34.2`. No fork is kept.
Every edit below is applied by `tools/build-play` to a private copy of the exact
pin, outside the repository; the module cache and the committed dependency
graph never change.

## What remains

| Edit | Applied by | Kind | Why it stays |
|---|---|---|---|
| Browser write staging (inline in `tools/build-play`) | every `--goos=js` build | required | Stock `internal/browser/queue.go` allocates a fresh `Uint8Array` per `writeBuffer`/`writeTexture`; at battle upload volume Chrome runs out of ArrayBuffer memory in about 14 s. One growable staging array, viewed per call, is safe because the browser copies write data before the call returns. |
| `metal-pointercache-diagnostic.patch` | `build-play --metal-cache` (`tools/prepare-metal-cache`) | performance, macOS arm64 | Caches prepared Objective-C call signatures in `hal/metal/objc.go`. On the 1440p field it is worth 2–3.5 Hz (final builds: 112.8 to 116.3 Hz at default zoom, 113.4 to 115.6 Hz zoomed out), halves command encoding and halves late frames; see the measurements below. |
| `wgpu-present-handler.patch` + `present-handler-manifest.json` | `build-play --present-handler` (`tools/apply-present-handler`) | measurement only | Presented timestamps (`addPresentedHandler:`) for presented rate, late frames and motion evenness. Never part of a shipping build. |
| `wgpu-gpu-timing.patch` + `gpu-timing-manifest.json` | `build-play --gpu-timing` (`tools/apply-gpu-timing`) | measurement only | GPU command-buffer start/end times; applies after the presented-handler patch. Never part of a shipping build. |
| `--vulkan-proxy` (inline in `tools/build-play`) | `build-play --vulkan-proxy` | diagnostic | Links the stock Vulkan HAL beside Metal so MoltenVK can stand in for Linux/Windows (PORTABILITY.md §4). |
| `native-artifacts.json` | none | evidence | Pinned wgpu-native release digests for the Rust probe; not a patch. |

The present-handler and GPU-timing patches were rebased on 2026-10-12 from the
owned-submission/render-runtime base to the stock pin alone. The hunks are the
same except that the presentation observer reads the submission index without
the owned-submission lock (stock `Submit` and `Present` both run on the render
thread), and the GPU-timing observer hooks stock `Queue.Submit` before commit
and skips any batch entry that is not a Metal command buffer. Each manifest
records the stock and result SHA-256 of every file it touches; the apply tools
refuse any other base. Both stack on `--metal-cache`, which touches no file
they touch.

The pointer-cache patch uses zero-context hunks to avoid whitespace-only blank
context lines; `tools/prepare-metal-cache` verifies the exact pinned
`hal/metal/objc.go` digest before applying it with `git apply --unidiff-zero`.
Modified upstream ObjC source is MIT, Copyright 2025 The GoGPU Authors; the
added cache code is MIT, Copyright 2026 The Nanolathe Authors. Test the copy
with `CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run TestPointerCache
./hal/metal`. Other architectures and full loss/shutdown failure paths are
unqualified. Upstreaming the signature cache would retire this patch.

## What was removed, and why

Removed on 2026-10-12 once the renderer stopped needing them. Git history keeps
the files; the `*_MEASUREMENTS.json` evidence is unchanged.

| Patch | What it did | Why it is no longer needed |
|---|---|---|
| `wgpu-owned-submission.patch`, `gogpu-owned-submission.patch`, `owned-patch-manifest.json`, `tools/prepare-owned-submission` | Staged mutable Metal texture writes, retained copy destinations, returned submission errors | The renderer stages its own texture rewrites (`internal/battlegpu/uploads.go`): dirty rectangles are packed into one arena, uploaded with `Queue.WriteBuffer` (which stock wgpu always stages) and copied on the frame encoder before any pass. Stock wgpu already defers releasing textures, views and shader modules to the last submission index and keeps buffers, bind groups and pipelines alive by reference until their submissions complete, so the per-frame `WaitIdle` is gone without it. |
| `gogpu-deferred-acquire.patch` + manifest | Deferred-acquire state, pre-command lease cancellation, no outdated replay, same-size restored-surface reconfigure | Stock GoGPU acquires the drawable lazily on the first `CommandEncoder()`/`SurfaceView()` call, and the driver requests them only after preparation. Its single outdated replay re-presents the last prepared frame with `Renderer.Redraw`. See "Stock limitations" for what was not carried over. |
| `wgpu-render-runtime.patch`, `runtime-patch-manifest.json`, `tools/apply-render-runtime` | Metal render-size metadata for runtime-sized WGSL storage arrays | Runtime arrays were never on in the measured profiles (`run-play` forced them off); fixed arrays plus storage reservation are the fast path. The renderer's runtime-array branch stays behind a capability check that no stock wgpu provides. |
| `runtime-wgsl-candidate.patch` | WGSL runtime-array candidate for the renderer's own files | Already in the renderer source. |
| `window-lifecycle-candidate.patch` + manifest | Resize protocol and lifecycle fixture for our own driver, host and retained files | The resize protocol is in place (driver `resizeSource`, build-play's `ResizePhysicalExtent`, `RetainedBattle.viewportDirty`); the scripted resize/Hide/Show fixture was never adopted. |
| `wgpu-present-polling.patch`, `present-polling-manifest.json`, `present-polling-driver.patch` | Drawable polling for presentation times | Superseded by the presented handler; its timestamp coverage was insufficient. |
| `present-handler-driver.patch`, `gpu-timing-driver.patch` | Driver hooks for the two diagnostics | The driver calls both through interface checks; the file they patched no longer exists. |
| `bindgroup-v29-diagnostic.patch` | go-webgpu v0.5.5 bind-group ABI field | A stopped Rust-binding investigation; no build used it. |

`NANOLATHE_WEBGPU_ASYNC_UPLOADS` and the `--owned-submission`/`--render-runtime`
build flags are gone. Chunked model uploads, which the owned profile
introduced, are not a dependency patch and stay: stock wgpu stages every buffer
write but gives each write above its 256 KiB belt chunk a one-off staging
buffer, and splitting the model writes at that size is worth about 12 Hz on
the zoomed-out field (table below). `renderer.go` turns them on by default
(`NANOLATHE_WEBGPU_CHUNKED_UPLOADS=0` is the control), overriding the older gate
in `modeluploads.go`, which still asks for the removed owned-submission marker.

## Stock limitations to report upstream

- `Queue.WriteTexture` (wgpu `pending_writes.go`) writes a host-writable
  texture at once, and `hal/metal` makes every single-sampled texture Shared on
  Apple GPUs, so a rewrite can race a frame in flight. The renderer avoids the
  call for any texture a submission may still sample.
- GoGPU `RenderTarget.resize` skips a same-size resize even when an earlier
  zero-size event unconfigured the surface, so a window restored at its old
  size after such an event stays unconfigured. The deferred-acquire patch fixed
  this in our copy. On macOS the platform reports the content view's backing
  size, which a minimize does not zero, and a stock build played through a
  minimize/restore; Windows and Linux are unverified.
- GoGPU logs `Finish`/`Submit` failures without returning them.
- wgpu's Metal `Submit` reports a submission complete at once if its completion
  block cannot be allocated, which would release deferred resources early.
- The Objective-C call path prepares a call interface on every message send;
  the pointer-cache patch is the measured cost of that.
- wgpu's Vulkan `Queue.WriteBuffer` (`hal/vulkan/queue.go`) waits for the
  previous submission to finish before every CPU write (`waitForGPU`), the
  same per-write stall Apple's shared textures caused on Metal. Expect it to
  cost Linux/Windows frame rate; staging those writes would remove it.
- A texture released while its `WriteTexture` is still pending fails the next
  submit ("destination texture retired before submit"), and a buffer released
  with a pending `WriteBuffer` leaves a staged copy aimed at freed memory
  (MoltenVK faults). WebGPU allows destroying a resource after a queue write;
  wgpu should keep the destination alive until the write is submitted. The
  renderer now defers its own releases past a submission (2026-10-13).
- Texture storage on Apple GPUs (above) also forgoes lossless compression for
  render targets: making textures without `CopyDst` private saved ~0.15 ms
  of GPU time in a 1440p frame. Small, but free for the backend.
- wgpu stages a `WriteTexture` on Vulkan, DX12 and discrete Metal through
  buffers capped at 64 MiB, and a larger write copies past its staging
  buffer into the next queue write's destination (Metal API validation on
  MoltenVK: "totalBytesUsed(236060672) must be <= [sourceBuffer
  length](67108864)"). It should split or refuse the write. The renderer
  writes large textures in 32 MiB row bands.
- wgpu's Metal HAL issues every `Draw`/`DrawIndexed` as
  `MTLPrimitiveTypeTriangle` whatever the pipeline's topology, so line-list
  and point-list pipelines draw triangles. The renderer draws no lines.
- naga's SPIR-V writer emits a module-scope `const` array as
  `OpConstantNull` (every element reads zero) and has no case for
  `any`/`all` (`ir.ExprRelational`); its Metal writer emits `select()` as an
  unparenthesised ternary, so a select inside arithmetic miscompiles. The
  shaders avoid all three and the model tests check them.
- Metal: the frame after a large texture allocation spends 20-90 ms idle on
  the GPU while the memory is mapped, for shared and private storage alike.
  Not a wgpu bug; renderers should allocate up front (this one now does).

## Measurements (2026-10-12)

Apple M3 Pro, 120 Hz panel. `--live-scene=field:1000` (3,000 units) on Town &
Country at `--webgpu-size=2560x1440`, `NANOLATHE_HOST_FIELD_CAMERA=1`,
prepare-ahead, storage reservation, late acquisition unless noted,
GOMAXPROCS=8, a private benchmark settings copy, 3,600 frames with the last
two thirds analysed. Builds alternated within each matrix (tags in the Reps
column; every matrix included the patched reference); the machine was shared,
so treat ±1–2 Hz as noise. Every build carried the measurement-only
presented-handler and GPU-timing patches. Rate and late frames (intervals over
12 ms) come from presented timestamps; motion is the mean |world-time step −
presented step| per frame; source is the source preparation of frames that
start a new tick; preparation and commands are the renderer's CPU preparation
and command encoding; GPU is the per-submission command-buffer span. The
patched reference is `proto/wgpu-next-20261012` at `6a721741f` built with
`--metal-cache --render-runtime --present-handler --gpu-timing` (owned
submission, deferred acquisition, render runtime, pointer cache) with
asynchronous and chunked uploads on.

| Scene | Build | Reps | Rate Hz | Late % | Motion ms | Source ms (tick) | Preparation ms | Commands ms | GPU ms |
|---|---|---|---|---|---|---|---|---|---|
| default | patched (all fork patches, reference) | 4 (fA+mA) | 115.3 | 1.73 | 0.45 | 6.27 | 1.48 | 0.67 | 6.09 |
| default | stock, renderer changes only | 2 (mA) | 113.2 | 2.06 | 0.61 | 6.12 | 1.85 | 1.34 | 5.94 |
| default | stock, renderer changes + chunked uploads | 2 (mA) | 113.1 | 2.40 | 0.64 | 6.32 | 1.52 | 1.31 | 5.98 |
| default | stock, renderer changes + pointer cache | 2 (mA) | 115.5 | 1.75 | 0.44 | 6.22 | 1.79 | 0.67 | 6.20 |
| default | stock final (renderer changes + chunked), no pointer cache | 2 (fA) | 112.8 | 3.33 | 0.73 | 6.68 | 1.57 | 1.53 | 5.89 |
| default | stock final + pointer cache | 2 (fA) | 116.3 | 1.44 | 0.40 | 5.99 | 1.42 | 0.63 | 6.19 |
| default | stock final + pointer cache, early acquisition | 2 (fA) | 112.8 | 3.15 | 0.75 | 6.84 | 1.50 | 0.78 | 5.83 |
| zoom 0.625 | patched (all fork patches, reference) | 6 (fZ+gZ+mZ) | 115.5 | 2.14 | 0.61 | 7.16 | 2.26 | 1.20 | 5.79 |
| zoom 0.625 | stock, renderer changes only | 2 (mZ) | 86.4 | 29.16 | 3.22 | 9.84 | 3.21 | 3.13 | 6.93 |
| zoom 0.625 | stock, renderer changes + chunked uploads | 2 (mZ) | 108.7 | 6.53 | 1.21 | 7.90 | 2.38 | 2.52 | 5.75 |
| zoom 0.625 | stock, renderer changes + pointer cache | 2 (mZ) | 108.4 | 7.78 | 1.41 | 7.22 | 2.76 | 1.23 | 5.91 |
| zoom 0.625 | stock final (renderer changes + chunked), no pointer cache | 2 (fZ) | 113.4 | 3.42 | 0.73 | 7.31 | 2.33 | 2.34 | 5.67 |
| zoom 0.625 | stock final + pointer cache | 4 (fZ+gZ) | 115.6 | 2.25 | 0.61 | 7.13 | 2.25 | 1.20 | 5.91 |
| zoom 0.625 | stock final + pointer cache, early acquisition | 2 (fZ) | 115.1 | 2.44 | 0.60 | 7.30 | 2.25 | 1.23 | 5.71 |

Reading it:

- The renderer changes alone remove the per-frame drain; with chunked uploads
  and the pointer cache, stock matches the patched build at both zooms
  (116.3 against 115.3 Hz at default zoom; 115.6 against 115.5 Hz zoomed out,
  over four and six repetitions). The owned-submission, deferred-acquire and render-runtime
  patches therefore cost nothing measurable to drop.
- Without the pointer cache the stock build loses 2–3.5 Hz and late frames
  roughly double; command encoding doubles. It stays as an optional build flag.
- Without chunked uploads the zoomed-out scene collapses (86 Hz, 29% late in
  the first matrix): stock wgpu gives each model write above 256 KiB a one-off
  staging buffer. Chunking is now the stock default.
- Late acquisition is worth about 3.5 Hz at default zoom and nothing measurable
  zoomed out; `run-play --fast` keeps it on.

A stock build with the pointer cache also played the scene through a mid-run
minimize/restore (presentation resumed; no surface stall) and four live window
resizes (five surface generations, no errors), and its live window and final
capture match the patched build's. Neither run hit GoGPU's outdated-present
replay, so the `Redraw` path that handles it is unexercised on hardware.
Evidence (reports, logs, cropped live captures) is outside the repository under
the session scratchpad's `nofork/runs/`.

## Licences

GoGPU, wgpu, gpucontext, gputypes, naga, goffi and go-webgpu at the resolved
pins carry MIT licences; x/sys carries BSD-3-Clause. wgpu-native is
MIT/Apache-2.0; redistribution must keep its source and licence notices and its
Rust transitive notices. The P4 Metal comparison imports purego v0.11.1
(Apache-2.0) into this nested module only; a distributed comparison binary needs
its notice. No GPL code was imported.
