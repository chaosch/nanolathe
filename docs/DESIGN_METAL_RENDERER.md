# Design — The native Metal renderer (macOS)

`internal/meshscene`, `internal/metalhud`, `internal/platform/metalrender`,
`internal/platform/mtl` and the `cmd/nanolathe/metal_*_darwin.go` host. An
experimental third executor for one battle on macOS: it draws the world from
retained meshes and textures through Metal, called from pure Go, and the HUD
from the production client's foreground draw list. `--metal` selects it for
the `--map` or `--mission` battle. It is not the default and not a persisted
option; Classic and Modern (DESIGN_GPU_RENDERER) remain the game's renderers
on every platform, macOS included.

This document states what the code does now. The prototype's round-by-round
measurements, the mechanisms it tried and retired, and the reasoning behind
them are in [METAL_RENDERER_HISTORY.md](METAL_RENDERER_HISTORY.md).
[ARCHITECTURE.md](ARCHITECTURE.md) owns package boundaries;
[DESIGN_PRESENTATION_CLIENT.md](DESIGN_PRESENTATION_CLIENT.md) owns what a
frame contains; [DESIGN_GPU_RENDERER.md](DESIGN_GPU_RENDERER.md) owns the draw
list, the Enhanced visual policies this renderer reproduces, and the Modern
executor it is compared against.

**Section numbers are stable.** Code comments cite them.

## 1. Purpose and status

Modern replays a recorded draw list through Ebitengine every frame, so its
CPU cost grows with what is on screen. This renderer keeps meshes, atlases and
per-map tables resident on the GPU, uploads only what changed each frame, and
does the per-piece and per-face work in shaders. On the development Mac (Apple
M3 Pro) it holds a 120 Hz cap where Modern does not (§8.3).

It is opt-in while it has these limits:

- **One battle per launch.** No front-end menus, Nanolathe screen, unit viewer,
  load/save screens or second battle; a menu action that leaves the battle
  ends the run.
- **Fixed drawable.** The window does not resize and has no fullscreen.
- **One machine.** It has run only on Apple silicon (M3 Pro). The Intel build
  has run only under Rosetta on an Apple GPU; Intel and AMD GPUs, whose memory
  is not shared with the CPU, are untested.
- **Visual differences** listed in §7.

Making it the macOS default needs a host that owns the whole Mac window (menus,
screens and battles), and is separate work.

## 2. Packages and files

| Package | Responsibility |
|---|---|
| `internal/platform/mtl` | Calls the Objective-C runtime, Metal, AppKit and Core Graphics from Go (§5). Imports nothing from the game. darwin/arm64 and darwin/amd64. |
| `internal/platform/metalrender` | The renderer: device, pipelines, resident resources, per-frame packing and encoding, the Cocoa window and input pump, timing rows and reports. `shaders/*.metal` is the whole MSL library, embedded and compiled at startup. |
| `internal/meshscene` | Platform-neutral scene preparation: compiled 3DO meshes and the model atlas, terrain tile atlases, sprite atlases (authored and 2× detail), fog masks, and `RetainedBattle`, which turns a committed frame pair into a `LiveFrame` of instances, poses, sprites, lights, fog and effects. |
| `internal/metalhud` | Converts the client's foreground draw list (HUD, cursor, labels, minimap) into overlay quads over one RGBA atlas with dirty-rectangle uploads. |
| `cmd/nanolathe/metal_*_darwin.go` | The battle host (`metalBattleHost`), the Metal battle benchmark source, and the production composition the host adds to each `LiveFrame`: model and shadow composers, projection, selection, subject rules, materials, water objects and visuals. |

The non-darwin builds compile `Run` and the host as stubs that report macOS is
required.

## 3. Contracts

- **Presentation only.** The renderer reads the committed frame pair the
  production client pins for presentation, and the client's foreground draw
  list. It writes no simulation state, owns no simulation clock and draws no
  random number; the simulation cannot tell it is running [I6]. Its float
  arithmetic is presentation-only (INVARIANTS I2).
- **The ordinary shell owns the battle.** `newDirectBattleView` composes the
  battle; the production client steps it, owns input service, the HUD, the
  camera, audio and asynchronous simulation. The host feeds native input to
  the client and replaces only the window and the draw executor.
- **Same frame as Modern.** World and HUD come from the same pinned pair and
  camera. Model poses blend the pair in float32 on the CPU (user-approved,
  2026-10-07: captures differ from fixed-point composition in about one face
  edge per scene).
- **Chrome text.** Magnified chrome regions use their virtual bounds for
  text admission and scale glyph dimensions, baseline and advance with the
  region's sprites and primitives (DESIGN_INTERFACE_HUD_INPUT §3.3). World
  text continues to project only its anchor and keeps native
  glyph dimensions and screen offsets (DESIGN_GPU_RENDERER §16.3).
- **No allocation churn.** Steady-state frames reuse their publication and
  upload storage; a few hundred allocations per draw remain, mostly the
  simulation's.

## 4. The frame

Each displayed frame:

1. **Input.** `Run` polls the native event queue on the main thread (§6) and
   passes the events and their timestamp to the host.
2. **Step and sample** (`metalBattleHost.Next`). At most once per 1/30 s the
   host applies the queued input and steps the client. Then it pins the
   presented pair, sets the camera fraction and records the client's
   foreground list.
3. **Retained world** (`RetainedBattle.Frame`). A new pair builds a
   publication. A camera-only change keeps the last build when `Covers` says
   its widened culling region still holds the view, and `Recull` reselects
   only sprites, lights and distortions. Model culling widens by a twelfth of
   the view width while the camera moves.
4. **Composition.** The host adds production paint order, model and shadow
   composition, projection and selection, then `metalhud` converts the HUD.
5. **Packing** (`livePacking.prepare`). The `LiveFrame` becomes one upload:
   instance records, pose matrices, sprites, lights, overlay quads and changed
   atlas rectangles. Resident tables are re-sent only when their generation
   changes.
6. **Encode and present** (`liveUpload`, `livePresent`). Three in-flight
   command buffers. Passes: pose interpolation and face preparation (compute),
   ground light, terrain, the composed world in painter phases with effects
   between them, water and reflections when visible water needs them, glow,
   then one final drawable pass (resolve, slab silhouettes, distortion, fog,
   map-edge black, HUD).

Steps 2–5 for frame N+1 run on a worker goroutine while the main thread
presents frame N and paces to the 120 Hz deadline; the late cursor moves to the
newest pointer sample before submission. Missed deadlines are skipped, never
repaid with a burst.

## 5. The Objective-C call layer (`mtl`)

- **Integer-only sends** go through the runtime's own libc trampoline,
  `syscall.rawSyscall9`, reached with `go:linkname`. Measured cost on the M3
  Pro: about 9 ns, no allocation. Blocking calls (`commit`,
  `waitUntilCompleted`) use `syscall.syscall9` so the scheduler sees a system
  call.
- **Floating-point arguments or results, and aggregates** use a small
  hand-written trampoline per architecture under `runtime.cgocall`, with a
  `Call` register file on the caller's stack. Apple silicon follows AAPCS64:
  `NSRect` returns in d0..d3. Intel follows System V: aggregates over 16 bytes
  travel on the stack, and `NSRect` returns through `objc_msgSend_stret`.
  About 18 ns.
- **purego** only opens the frameworks, looks up symbols, and provides the
  `fakecgo` runtime support that `runtime.cgocall` needs with `CGO_ENABLED=0`.
  No other compiler is needed.
- **No callbacks.** Command-buffer completion, GPU timestamps and drawable
  presentation are polled, so no C code ever calls into Go and a `Call` cannot
  move under a foreign frame.
- **Threading.** Cocoa and every renderer call run on the process main thread,
  which `cmd/nanolathe`'s darwin `main` locks before anything else runs. Objects are
  released explicitly, with autorelease pools around set-up and each frame.

**Risk.** The three `go:linkname` targets (`syscall.rawSyscall9`,
`syscall.syscall9`, `runtime.cgocall`) are Go internals. Go has restricted such
links since 1.23; these three remain linkable today (Go 1.27). If a Go release
closes them, the calls can fall back to purego's own entry points, which are
slower and allocate. `mtl`'s tests
exercise each path (§9).

## 6. Window and input

The renderer creates an `NSApplication` and one titled window, and pumps
events itself each frame. Each frame it drains queued `NSEvent`s without
waiting. Before and after each drain it services the main run loop in one
non-blocking pass of at most eight sources, so AppleEvents, accessibility and
timers still run. An AppKit call that takes longer than 50 ms is logged with
its frame and event type in the report's `event_pump_stalls`.

`meshscene.NativeEvent` carries ordered events to the host:

| Kind | Meaning | Fields |
|---|---|---|
| 1 | pointer move or wheel | Key bit 4 marks a wheel, plus bit 1 precise and bit 2 momentum; Button is the signed zoom notch count; WheelX/Y are point deltas |
| 2 / 3 | button down / up | Button 0 left, 1 right, 2 middle; on a down, Key is the click count |
| 4 / 5 | key down / up | Button is the Cocoa virtual keycode; Key is the first unmodified character |
| 6 | modifiers changed | Modifiers: Shift 1, Control 2, Alt 4, Command 8 |
| 7 | translated text | Key is the Unicode scalar, case preserved |
| 8 | focus | Key 0 lost, 1 gained |
| 9 | pinch | WheelY is magnification; Key phase bits are began 1, ended 2, cancelled 4 |

Coordinates are top-left-origin drawable pixels. The host divides them by the
device scale, which is 2 because the HUD is drawn at half the drawable size.
`metal_input_darwin.go` maps these events onto the production client's input
service, so orders, selection, build placement and hotkeys are the production
ones. The queue holds 8,192 events; only consecutive wheel-free moves
coalesce.

**Pointer capture** follows the client's `PointerCaptured` request. Capture
centres and disassociates the OS cursor and accumulates relative motion;
release restores the saved point. Losing focus releases capture and every held
key and button. The system cursor is hidden while the HUD draws its own.
Command-Q closes the window through the ordinary path, so a run with
`--metal-report` still drains and writes its records.

## 7. Visual coverage and known differences

The renderer draws the production battle's content: terrain from the TNT's
unique tiles (2× detail tiles above one device pixel per world pixel), the
authored feature sprites and their 2× detail variants in a second atlas,
models with production materials, shadows, construction reveal and outlines,
cloak, water surfaces, seabed and reflections, the stock effect families,
lights, ground light, glow, distortion, fog and the HUD. Beyond the map edge
the world is black.

Known differences from Modern, none of them simulation-visible:

- **Model geometry.** Opaque single-member subjects use depth slabs instead
  of the isolated subject atlas. Slab bodies average four sub-samples, and
  their silhouettes are softened by an estimate rather than geometric
  coverage.
- **Pose blending.** Float32 parent-chain composition (§3) and GPU rigid-piece
  interpolation differ slightly from the fixed-point hierarchy and its cache
  history.
- **Raster detail.** Some shadow boundaries, ALP destination-palette blending,
  the optimized outline-ring key admission, deep cargo chains and secondary
  projectile models are approximate or missing.
- **Effects.** The host forwards the production effect switches and dithered
  fog every frame. No one has audited whether every Enhanced option changes
  the Metal world exactly as it changes Modern. The Metal copy of the Enhanced
  blast boosts is in `meshscene/battle_distortion.go`.
- **Brightness.** The second prototype round measured trees and some units
  drawing lighter than Modern while terrain matched exactly; this has not been
  re-measured since.

Reports carry `render_parity_verified: false`. A difference found by
inspection is fixed in the Metal path, or recorded here, never by changing
Modern or the research.

## 8. Running it

### 8.1 Play

```sh
tools/metal-play                       # The Pass against a Modern computer player
go run ./cmd/nanolathe --metal --map 'The Pass' --metal-report /tmp/metal-run
```

| Flag | Meaning |
|---|---|
| `--metal` | Play the `--map` or `--mission` battle in this renderer (macOS only; not with `--shot`, `--headless` or `--load-save`) |
| `--metal-size WxH` | Drawable pixels, even, at least 1280×960; default 2880×1800 (a 1440×900 HUD) |
| `--metal-frames N` | Stop after N frames; 0 plays until the window closes |
| `--metal-report DIR` | On exit, write `report.json`, `frames.csv`, `live-frames.csv` and `final.png` to this new directory |
| `--metal-model-capture NAME` | With `--metal-report`: capture one model fixture offscreen (`armsolar`, `armcom`, `armcom-ground`/`tree`/`air`, `armcom-cloaked`/`underlay`, `armflash-wreck[-sink]`) |

### 8.2 Benchmark

The live battle benchmark (BATTLE_BENCHMARK.md) takes
`--benchmark-renderer=metal`. It runs the same fixture, viewer step and census
as Classic and Modern, and writes the same files plus `report.json`,
`frames.csv` and `live-frames.csv` from the native rows. Its stress options,
`--metal-quads 1|2|5` (mesh subdivision) and `--metal-textures 1|2` (model
atlas scale), change the workload without changing the picture.

Two environment variables remain for diagnosis. `NANOLATHE_METAL_OFFSCREEN=1`
runs the benchmark without a window, for capture comparisons only; its timings
are not comparable. `NANOLATHE_METAL_PASS_TIMING=<file.csv>` samples per-pass
GPU timestamps with Metal counter sampling.

### 8.3 Measured

The last prototype matrix (2026-10-07, M3 Pro, GOMAXPROCS=2, 120 draws/s cap,
two alternating repetitions, full simulation) recorded these draws per second
for Classic / Modern / Metal:

| Scene | Classic | Modern | Metal |
|---|---:|---:|---:|
| Field, 3456×2160 large view | 29.3 | 69.8 | 119.0 |
| Field, 1920×1080 | 65.0 | 95.6 | 119.6 |
| Coastal, 1920×1080 | 53.1 | 101.1 | 119.9 |

Go allocations per draw were 21,000–37,000 for Modern (4–6 collections per
run) and 290–435 for Metal (none). Doubling the quad count left Metal at 118.8
in the large view, where Modern fell to 47.2. Device memory is 1–2 GB with
remastered detail art adding about 280 MB, beside about 1.2 GB of Go heap in
the large view. METAL_RENDERER_HISTORY.md has the method and every earlier
round.

## 9. Verification

- **`tools/check`** runs `mtl`'s tests on macOS. They round-trip Foundation
  values through every calling path: integer sends, a double argument and
  result, `NSPoint` and `NSRect` results. They need no window or GPU. Run them
  under Rosetta too (`GOARCH=amd64 go test ./internal/platform/mtl`) when
  touching the Intel trampoline.
- **`tools/check-retail`** runs `TestDevicePipelines` with
  `NANOLATHE_METAL_DEVICE_TEST=1` on macOS with a window server. `TestMain`
  builds the whole offscreen renderer on the main thread, which compiles the
  shader library and every pipeline, and checks that each of the library's
  entry points resolves. Metal compiles shaders when the game starts, so this
  is the only gate that sees a shader error.
- **Renderer changes** are checked by capture. Take offscreen benchmark
  captures (`NANOLATHE_METAL_OFFSCREEN=1`, coastal and field, a few hundred
  draws) from the build before and after. Compare `final.png` pixel for pixel:
  a refactor must be identical, and anything else is inspected beside Modern's
  capture of the same scene. Then measure the windowed benchmark
  sequentially, on a quiet host, against the same scene metadata.

No pixel fixture locks the Metal output yet. Captures of retail art cannot be
committed, so such a fixture needs an authored synthetic scene.

## 10. Limits and next work

The [WebGPU implementation plan](PLAN_WEBGPU_RENDERER.md) stages an isolated
GoGPU/WGSL prototype, quantitative acceptance and a later cross-platform host
migration. Its first gate is evaluation only; it does not change this renderer's
status or the production defaults.

- **Becoming the macOS default** needs a host that owns the Mac window from
  launch: menus, the Nanolathe screen and unit viewer (which draw on
  Ebitengine images today), resizing and fullscreen, and more than one battle
  per launch with GPU memory released in between. Ebitengine would remain the
  host on Windows, Linux and the browser, and the fallback on macOS.
- **Hardware coverage**: Intel and AMD GPUs, 8 GB Macs and older macOS
  releases.
- **Every new visual feature costs twice.** It must be implemented here as
  well as in Modern, and compared by capture.
