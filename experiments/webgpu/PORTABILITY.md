# WebGPU battle portability (2026-10-12, updated 2026-10-13)

The real-battle WebGPU prototype (production `metalBattleHost` source plus
`internal/battlegpu`) now builds for darwin/arm64, linux/amd64, windows/amd64
and js/wasm from one `tools/build-play`. Runtime evidence on this host:

| Target | Build | Runtime evidence | State |
|---|---|---|---|
| macOS arm64, Metal, stock GoGPU/wgpu | yes | bounded 600-frame field runs, capture renders units, terrain, features, HUD | works |
| macOS arm64, Metal, stock + `--metal-cache`, `run-play --fast` | yes | 1440p 3,000-unit field, alternating against the former patched build (patches/README.md) | works; no dependency fork |
| macOS arm64, Vulkan via MoltenVK (Linux/Windows proxy) | `--vulkan-proxy` | deterministic 2560x1440 benchmark: final frame identical to WebGPU-on-Metal (2 of 3.7 M pixels differ) | works; V1–V9 below |
| linux/amd64 | yes (ELF, dynamically linked loader, CGO 0) | none (no hardware here) | compile only; the proxy's fixes apply to its Vulkan path |
| windows/amd64 | yes (PE32+) | none | compile only; DX12 shares V4/V7's staged-write paths, now handled |
| js/wasm, Chrome WebGPU | yes (+ `wasm_exec.js`) | headless Chrome plays the field battle with no WebGPU errors (750 units at the 60 Hz cap) | works; B1–B6 handled in tree |

Nothing here is a hardware qualification beyond the runs listed. No module
requirement changed; dependency edits exist only in external build outputs.
Since 2026-10-12 every target plays on the stock pins: the owned-submission and
deferred-acquire patches are gone (see §1 and patches/README.md).

## Building and running

```sh
# Native (default target = host toolchain), stock pinned dependencies:
GOWORK=off experiments/webgpu/tools/build-play NEW_DIR
# Other targets (the macOS diagnostic patch flags are refused here):
GOWORK=off experiments/webgpu/tools/build-play NEW_DIR --goos=linux --goarch=amd64
GOWORK=off experiments/webgpu/tools/build-play NEW_DIR --goos=windows --goarch=amd64
GOWORK=off experiments/webgpu/tools/build-play NEW_DIR --goos=js --goarch=wasm
# macOS Vulkan proxy (stock dependencies only):
GOWORK=off experiments/webgpu/tools/build-play NEW_DIR --vulkan-proxy

# Launch (refuses another platform's build and js/wasm):
NEW_DIR/run-play [--fast ...] -- --root=… '--map=Town & Country'
# Browser: serve a js/wasm output and open http://127.0.0.1:PORT/
experiments/webgpu/tools/serve-battle --build JS_DIR --root ~/TotalAnnihilation \
  --out NEW_RESULTS_DIR --port 8767 --args '{"argv":["--webgpu","--map=Town & Country",
  "--webgpu-frames=120","--webgpu-report=/tmp/report","--webgpu-size=1920x1080"],"timeoutMs":80000}'
```

Bounded smoke command used throughout (add `NANOLATHE_WEBGPU_BENCHMARK_NEUTRAL_POINTER=1`
or `run-play --neutral-pointer` to keep the camera on the staged field; without
it the entry camera shows the player's start, with no units):

```sh
NANOLATHE_SETTINGS=<private copy> GOMAXPROCS=4 tools/host-run BUILD/nanolathe-webgpu --webgpu \
  --root=$HOME/TotalAnnihilation '--map=Town & Country' --live-trace=OUT-source \
  --live-scene=field:250 --webgpu-report=OUT --webgpu-frames=600 --webgpu-size=1920x1080
```

`--live-scene=coastal` needs `--map='Expanded Confluence'`; otherwise the run
exits with status 2 and prints nothing. In the WebGPU host the entry camera then
moves off the coastal scene, so field + neutral pointer is the usable smoke.

## 1. Stock dependencies

Every target now builds and plays on the stock pinned GoGPU and wgpu.
`run-play --fast` (prepare-ahead, storage reservation, late acquisition,
GOMAXPROCS between 4 and 8) needs no dependency patch and is the speed profile
on every native target. What used to need the owned-submission and
deferred-acquire patches is now done by the renderer and driver:

- **Texture rewrites are staged by the renderer** (`uploads.go`). Rewrites of
  textures earlier frames sampled are packed into one arena with 256-byte rows
  and 512-byte region offsets (the strictest buffer-to-texture placement any
  backend asks for, DX12's), uploaded with `Queue.WriteBuffer` in 256 KiB
  pieces and copied with `CommandEncoder.CopyBufferToTexture` on the frame's
  encoder before any pass. Both calls exist with the same meaning on the
  native HALs (Metal, Vulkan, DX12), wgpu-native and the browser; in js/wasm
  the write goes through build-play's staging edit. Textures created in the
  same frame keep `Queue.WriteTexture`.
- **No per-frame completion wait.** Releasing a replaced texture, view, shader
  module, buffer, bind group or pipeline is deferred by wgpu until the
  submissions that used it complete (native); browsers defer `destroy()` the
  same way. Resize, capture and teardown keep their `WaitIdle`.
- **Late acquisition** uses GoGPU's lazy acquire: the driver requests the
  encoder and drawable only after preparation. GoGPU's one replay after an
  outdated present re-presents the last prepared frame (`Renderer.Redraw`).
- **Model uploads** are split at the staging belt's 256 KiB chunk size, which
  keeps large writes off one-off staging buffers on every staged backend.

What stock still lacks: Metal pointer-call caching (optional `--metal-cache`,
macOS arm64), presented and GPU timestamps (measurement-only
`--present-handler`/`--gpu-timing`), runtime-sized WGSL arrays (fixed arrays
are used), submission failures returned from `Run` (GoGPU logs them), and
reconfiguring a surface restored at its old size after a zero-size event
(GoGPU skips that same-size resize; macOS does not report a zero size on
minimize, and a stock build played through a minimize/restore and four live
resizes).

Effects were not exercised in the original 600-frame stock smoke: the field
armies do not engage within 600 frames (tick ~200). The 3,600-frame
measurement runs do reach combat.

## 2. Driver portability

`driver.go`, `capture.go` and `debug.go` lost their darwin constraint; they use
only the portable GoGPU/wgpu API. Small platform files hold what differs:

| | darwin | linux/windows | js/wasm |
|---|---|---|---|
| `windowScaleHint` | 2 (Retina window, doubled drawable) | 1 | `devicePixelRatio` |
| `hostPacesFrames` (deadline sleep in callback) | yes | yes | no: rAF owns cadence |
| `clipboardReadable` | yes (host keeps NSPasteboard) | yes (GoGPU, unqualified) | no: GoGPU returns "" |
| `callbackFileIO` | yes | yes | no (see B5) |

- The first draw now reads the actual scale; if the drawable differs from
  `--webgpu-size` and the scale differs from the hint it requests the matching
  logical size once and waits up to 120 callbacks. On Retina macOS nothing
  changes. Pointer scaling follows the measured scale.
- `ClipboardText` exposes GoGPU's clipboard read to hosts without native code.
- If GoGPU falls back to its CPU rasterizer (requested backend has no adapter)
  the driver stops with that cause instead of a later compute-pipeline error.
  Browser adapters also report an empty backend and are exempt.
- Reports add `target`, `graphics_backend`, measured `layer_contents_scale`,
  `host_paces_frames` and `profiles_recorded`.
- Measurement-only diagnostics (presented and GPU timing) are reached through
  interface checks and are absent on stock builds; nothing else is patch-only.

## 3. Portable host build

For non-darwin targets build-play's overlay:

1. removes `//go:build darwin` from every `cmd/nanolathe/metal_*_darwin.go` and
   renames the virtual file to `*_webgpu.go` (a `_darwin` suffix is itself a
   constraint);
2. omits `metal_host_other.go` and `metal_benchmark_other.go`;
3. rewrites the one other macOS dependency, the `purego/objc` pasteboard read
   in `metal_input_darwin.go`, to `battlegpu.ClipboardText`; the ASCII
   admission after it is unchanged. (The host is therefore not "metalrender
   only": this objc read is the second darwin dependency.)

The build refuses if any objc or `internal/platform/mtl` import survives.
Windows gets `.exe`, js gets `.wasm` plus the toolchain's `wasm_exec.js`, only
darwin gets the `.app`. A default darwin build produces the identical overlay,
override files, source rewrites and module list as before this change.
Linux binaries are dynamically linked to the system loader (purego's runtime
shim with CGO 0) and need `libvulkan.so.1` plus X11/Wayland libraries at run
time; Windows Auto selects among DX12, Vulkan and GLES.

## 4. Vulkan proxy (MoltenVK 1.4.2 on Apple M3 Pro)

Run: `tools/host-run /usr/bin/env GOGPU_GRAPHICS_API=vulkan
DYLD_LIBRARY_PATH=<dir with libvulkan.dylib -> /opt/homebrew/lib/libMoltenVK.dylib>
PROXY_BUILD/nanolathe-webgpu …`. SIP strips `DYLD_*` when `host-run`'s
`/usr/bin/env python3` starts, so set them with `/usr/bin/env` after host-run.

Findings in order; each later one was reached by a private, uncommitted probe
build working around the earlier ones:

- **V1 (GoGPU, fixed by `--vulkan-proxy`).** On macOS GoGPU links only its Metal
  HAL and ignores `GOGPU_GRAPHICS_API`; the env alone still selects Metal.
- **V2 (wgpu Vulkan HAL).** No `VK_KHR_portability_enumeration`, so the Khronos
  loader hides MoltenVK: *"vkCreateInstance: Found drivers that contain devices
  which support the portability subset, but the instance does not enumerate
  portability drivers! … Found no drivers!"* GoGPU then silently picked its
  software adapter and failed with *"software: compute pipeline requires SPIR-V
  shader"* (now named by the driver). Workaround: load MoltenVK directly as
  `libvulkan.dylib`. The device is also created without
  `VK_KHR_portability_subset`, which MoltenVK tolerates.
- **V3 (naga v0.19.0 SPIR-V; affects Linux/Windows Vulkan).** *"vulkan: naga WGSL
  compilation failed: SPIR-V generation error: unsupported expression kind:
  ir.ExprRelational"* — the SPIR-V emitter has no case for `any`/`all`. MSL,
  HLSL/DXIL and GLSL emitters do. 19 calls (see R5). A diagnostic rewrite to
  component-wise `||`/`&&` compiled every module on Vulkan.
- **V4 (renderer + wgpu; affects every staged-write backend: Vulkan, DX12,
  non-UMA Metal).** First submit: *"wgpu: flush pending writes: wgpu: pending
  writes: destination texture retired before submit: wgpu: resource already
  released"*, then *"vulkan: no successful swapchain submission is ready for
  presentation"*. `newLayersFormats` releases placeholder terrain textures it
  wrote moments earlier, before any submit (R6). wgpu's pending-writes code
  writes host-writable textures (Apple-silicon Metal) immediately, which hides it. WebGPU allows destroy after
  `writeTexture`; wgpu should keep the destination alive (upstream).
- **V5 (wgpu Vulkan HAL + renderer).** Nil dereference in
  `hal/vulkan.(*RenderPassEncoder).SetBindGroup` (`command.go:1150`, reads
  `e.pipeline.layout`) from `Models.Compute` (`models.go:1155`): three render
  passes bind group 0 before their first pipeline (R7). The compute encoder
  silently drops such a binding instead. DX12 binds with no root mappings in the
  same case (unverified risk). Upstream fix: hold bindings until a pipeline is
  set; the probe's replay of held groups removed the panic.
- **V6 (open).** The first battle-frame submission (submit #2) then crashes
  inside MoltenVK: SIGSEGV, fault address 0x58, in
  `MVKCmdCopyBuffer<1>::encode` → `MVKBuffer::getMTLBuffer() + 20` (symbolized
  from the loaded `vkGetInstanceProcAddr` address), i.e. a buffer copy whose
  buffer has no bound device memory. That submission's only buffer copies are
  wgpu's queue-write staging copies for 11 small `WriteBuffer` calls (layer
  storage and uniforms, model records). No buffer with an unsubmitted write was
  released. **Unknown:** the resource and owner. Settle with Vulkan validation
  layers (not installed; the loader route also needs V2's fix) or a Linux/Windows
  run with validation.

**2026-10-13 update.** After the stock-dependency merge V4 had returned on
the first submit, and V6 shares its cause: the renderer replaced resources
that still had a queue write pending in wgpu's pending-writes encoder (layer
textures replacing their init placeholders, layer storage, glow and
reflection buffers growing, and the model atlas's 1x1 diagnostic plane when
the atlas was reallocated twice before the first submit). Released before
that submit, a texture fails it ("destination texture retired before
submit") and a buffer leaves a staged copy aimed at freed memory, which is
the MoltenVK fault. Replaced resources now go to the renderer's
`textureUploads.retire`, which releases them only once the queue's
submission index has passed their replacement (immediately in the browser,
whose queue tracks no index and whose WebGPU permits it). With that, the
proxy plays the battle on Vulkan with no errors or crashes:

- **V7 (MoltenVK proxy; closed below).** The ground renders black. The terrain tile
  atlas (RGBA8) reads back from the GPU byte-identical to Metal, but the
  R32Uint tile lookup does not: in each 360-texel row ~310 texels read
  0xFFFFFFFF (and 0xFFFF00FF when the lookup is stored as RGBA8 instead), so
  the fill depends on the format and is not another resource's bytes.
  Pre-padding rows to 256 bytes changes nothing, and the lookup is already
  wrong when captured after the first frame. A standalone wgpu program that
  writes and reads back the same 360x240 R32Uint and RGBA8 textures on
  MoltenVK, alone or after a 41 MB RGBA8 write in the same batch, round-trips
  them intact; so plain queue writes are sound and the cause lies in the
  renderer's first-frame work on a staged backend. The damage is
  deterministic: the same 7,117 of 86,400 texels survive whether the lookup
  is R32Uint or RGBA8 and whether it is allocated first or last, so it is
  not an allocation-order overlap. **Unknown:** the writer; settle with
  validation layers or a Linux/Windows run. Sprites, models, shadows,
  HUD and effects render.
- **V7 closed (2026-10-13).** Metal API validation under MoltenVK
  (`MTL_DEBUG_LAYER=1`) named it: wgpu stages a texture write on Vulkan,
  DX12 and discrete Metal through buffers capped at 64 MiB, and one larger
  write (the 236 MB sprite atlas) copies past its staging buffer; the
  overflow lands in the next queue write's destination, the terrain lookup.
  Large textures are now written in row bands of at most 32 MiB
  (`writeTextureBands`); Apple-silicon Metal writes such textures directly,
  which hid it. Upstream: wgpu should split or refuse oversized writes.
- **V8 (fixed; naga SPIR-V, affects Linux/Windows Vulkan).** Models drew
  only bodies; every commit quad was empty. naga emits a module-scope
  `const` array as `OpConstantNull`, so each corner table read zeros (the
  Metal writer keeps the values). The five shaders that indexed such tables
  now compute corners; a test rejects module-scope const arrays.
- **V9 (fixed; wgpu Metal HAL, affects WebGPU-on-Metal only).** Nanoframe
  outlines filled as white polygons on Metal while Vulkan drew lines: the
  Metal HAL issues every `Draw`/`DrawIndexed` as triangles whatever the
  pipeline's topology. Outlines no longer use a line list: one quad per
  authored ring, whose fragments walk the ring's chains row by row as
  retail's polygon outline does [03 R-RAST-01 §1] (CADENCE_ROUND.md). Upstream:
  pass the pipeline's primitive type to the Metal draw calls.
- With V7–V9 fixed, the deterministic benchmark's final 2560x1440 frame on
  the proxy matches WebGPU-on-Metal exactly but for 2 pixels, and model
  tests also compile every model shader through naga's SPIR-V writer, which
  still lacks `any`/`all` (V3), so they fail there rather than at runtime.
- wgpu's Vulkan `Queue.WriteBuffer` waits for the previous submission to
  finish before every CPU write (`waitForGPU`), the same stall Apple's
  shared textures caused on Metal; expect it to cost Linux/Windows frame
  rate until upstream stages those writes.

The debug capture (`NANOLATHE_WEBGPU_DEBUG_FRAME`) now includes the terrain
tile atlas and lookup.

## 5. Browser (Chrome headless, WebGPU on Metal)

`tools/serve-battle` + `tools/browser-battle` run the real host in a page: the
content root mounts at `/game` through the repository's `web/fs.js` with lazy
HTTP range-request Blobs, so archives load on demand (content index 165–183 ms);
console output, uncaptured WebGPU errors and the report files come back to the
server. Chrome: `--headless=new --enable-unsafe-webgpu --window-size=1920,1080`
with a fresh profile; adapter `apple / metal-3`.

- **B1–B3 (WGSL rejected by Tint; naga accepts them).** R1–R3 below.
- **B4 (layout sizes).** *"The shader uses more bytes of the buffer (12288) than
  the layout's minBindingSize (96)"* (sprite pipelines), *(8192) … (64)*
  (`layer_quad_vertex`, overlay binding 1), *(160) … (80)* (`layer_ground_vertex`,
  binding 3). R4. Every frame's command buffer was then invalid and the capture
  blank.
- **B5 (fixed in the driver).** Go code inside a `requestAnimationFrame`
  callback cannot wait for work the page's event loop completes. Writing
  `alloc-base.pprof` deadlocked (*"fatal error: all goroutines are asleep -
  deadlock!"*, goroutine in `syscall.fsCall` under `writeBattleProfile`); the
  heap profile also reads the memory map. Captures and JSON are now held in
  memory until `Run` returns; the browser records no profiles.
- With B1–B4 corrected in a private overlay, a 240-frame run finished with no
  WebGPU errors and a capture matching Metal's scene: same field, units, features,
  minimap, cursor and resource bar. It stopped at tick 136 (Metal ~200), and the
  Wind/Tidal and Game Time text present in Metal's tick ~200 capture is absent.
  The ticks differ, so this is not a renderer conclusion. Observed, not
  benchmarked: rAF cadence 16.7 ms, median source 9.25 ms, single thread.

Remaining steps for a real browser target:

- **B6 event loop (structural).** GoGPU runs every frame inside the rAF callback.
  Anything a frame waits on that JavaScript completes — an archive read for
  content not yet cached, a fetch, the prepare-ahead worker blocked on either —
  deadlocks the page. The 240-frame smoke hit none, so a longer session is
  expected to. Options: GoGPU hands frames to a goroutine as Ebitengine's browser
  loop does, or the driver never blocks in the callback (non-blocking source
  join, keeping the last frame when the source is not ready). `PREPARE_AHEAD`
  joins inside the callback and is unsafe in the browser for the same reason.
- Launcher: reuse `web/` (folder import or demo download already feed BrowserFS)
  with a WebGPU entry passing `--webgpu --map=…`; GoGPU draws into the page's
  first `<canvas>` and creates one otherwise.
- Size: the host requires an even `--webgpu-size` of at least 1280×960 at the
  canvas's device pixels; a fitted canvas needs the existing resize source.
- Input, focus and audio were not exercised (headless). Clipboard reads are
  unavailable. Go wasm is single-threaded: a Worker/SharedArrayBuffer split is a
  separate design.

## Renderer and shader changes (all made in tree by 2026-10-13)

The table records what each change was; R5's `any`/`all` remain unusable
while naga's SPIR-V writer lacks them, and the model tests check it.

| Id | File | Problem | Proposed change |
|---|---|---|---|
| R1 | `models.wgsl:121` | Tint: *"mixing '\|\|' and '&&' requires parenthesis"* in `var erased=tex.a<0.02\|\|ruled&&clipped(i,key);` | `tex.a<0.02\|\|(ruled&&clipped(i,key))` |
| R2 | `overlay.wgsl:9,140` | Tint: *"'meta' is a reserved keyword"* (`LayerSmoke.meta`) | rename the field, e.g. `smokeMeta` |
| R3 | `overlay.wgsl:133,137,153` | Tint: *"'filter' is a reserved keyword"* (vertex output field) | rename, e.g. `sampleFilter` |
| R4 | `layers.go:474` (sprite binding 1, 96), `:495` (overlay binding 1, 64; binding 3, 80) | layout `MinBindingSize` is one element; the substituted fixed array is larger | use capacity × stride from the same ABI capacity, or 0 (the probe used 0 for read-only storage) |
| R5 | `liveposes.wgsl:35,37`; `modelgroups.wgsl:47`; `models.wgsl:120,126,133`; `modelshadowcommit.wgsl:24,31,60`; `modelshadows.wgsl:27` | `any`/`all` unsupported by naga v0.19.0 SPIR-V (V3) | expand component-wise, or fix naga to emit `OpAny`/`OpAll` |
| R6 | `layers.go:412,421,432` (`newLayersFormats`) | placeholder terrain textures written then released before the first submit (V4) | create the terrain textures once when `TerrainTiles` exist, or keep placeholders until after a submit |
| R7 | `models.go:1155`, `models.go:1214`, `modelshadows.go:454` | `SetBindGroup` before the pass's first `SetPipeline` (V5) | set the pipeline first |

## Evidence (outside the repository, `/private/tmp/claude-501/wgpu-port/`)

- Stock Metal: `run-stock-metal-2/` (field camera), `run-stock-metal-3-prepare/`
  (prepare-ahead + reservation), `run-final-metal-stockfast2/` (final tree via
  `run-play --stock-fast`, the profile now named `--fast`); `run-stock-metal-1/` shows the entry camera without units.
- Builds: `final-{darwin-arm64,linux-amd64,windows-amd64,js-wasm}/` with manifests;
  `build-darwin-fast/` (patched `--gpu-timing`, builds); `build-darwin-vulkan2/` (proxy).
- Vulkan: `run-vulkan-loader-4.log` (V2), `run-vulkan-direct-1.log` (V3),
  `run-vulkan-direct-2-relational.log` (V4), `run-vulkan-direct-5-probe.log` (V5),
  `run-vulkan-direct-10-sym.log` and `-12-writes.log` (V6). Probe sources:
  `relational-probe/`, `release-probe/`.
- Browser: `browser-run-1/console.log` (B1, B2, B5), `browser-run-2/` (B3, first
  B4), `browser-run-3/` (remaining B4, blank capture), `browser-run-4/final.png`
  (rendered, no WebGPU errors); the private corrections are in `browser-wgsl-diag3/`.
