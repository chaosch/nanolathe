# Retained Metal experiment

An independently authored, presentation-only prototype. It borrows the existing
purego dependency to call a small Objective-C ABI rather than using cgo. It does
not copy Ebitengine's internal Metal implementation. Ebitengine's internal
wrappers are not a public importable rendering API; a public exported wrapper
or a properly attributed vendored fork would be another approach.

Build alongside the Go executable:

```sh
clang -dynamiclib -fobjc-arc -O3 -Wall -Wextra \
  -framework Cocoa -framework Metal -framework QuartzCore \
  internal/platform/metalproto/native/bridge.m -o /path/to/libnanometal.dylib
```

No standalone Metal toolchain is needed: `Run` embeds `shaders.metal`, and the
bridge compiles it through `newLibraryWithSource:options:error:` at startup,
outside measurement. Only the bridge needs clang and the macOS SDK. The Go
binary remains cgo-free. Set `Options.LibraryPath`, or place the dylib beside
the executable. Lock the process main OS thread in the executable's `init`.

The resource interface uploads an immutable vertex/index buffer and an instance
pose-offset buffer per mesh, and one model atlas plus a 2D terrain texture.
Three pose input buffers hold two adjacent sampled frames. Every frame copies
those matrices, computes one interpolated rigid transform per submitted piece
into a private GPU buffer, then draws one indexed instanced call per uploaded
mesh; projection and normal lighting run in custom shaders. The bounded queue permits
three in-flight command buffers, waits for a slot before modifying its pose
buffer, and drains only at finish or capture. Completion records errors and
GPU timing asynchronously. `nm_capture` re-renders the final sampled pose after
the measurement drains, then performs one synchronized readback for PNG.

Visible output uses a native Cocoa window and CAMetalLayer, with pixel-size
output and backing-scale-sized view points. Offscreen output creates no window,
layer, drawable or application. It still requires Metal hardware. The host FPS
cap uses timer scheduling with missed slots skipped after stalls, not a
display-link scheduler; animation continues against its independent wall-time
origin. The submission deadline rebases after a missed slot to avoid bursts.
144 FPS offscreen does not imply 144 Hz display presentation. Window close ends a run; no watchdog
recovers an indefinitely stuck GPU, drawable acquisition or in-flight wait.

These are deliberate synthetic presentation approximations, not retail claims:

- The sample stream wraps; last-to-first transforms can be discontinuous.
- Rotations use shortest-path quaternion slerp after removing the endpoint
  basis's final world-Z reflection, restored after interpolation. A zero
  matrix is a hidden piece, suppressed if either endpoint is hidden, producing
  a visibility snap of up to one sample instead of motion toward world origin.
- Projection is the scene's x / z-minus-height shear. Depth uses independent
  z-plus-height ordering with a fixed conventional scale, and terrain is a flat
  backdrop. There is no palette composition, retail depth key or shadow lane.
- Effects deliberately stress eight moving synthetic point lights, emission
  extraction, two half-resolution bloom compute passes, and four radial
  distortion rings in the full-screen composite. They do not consume battle
  events or reproduce the production effects' admission policy.
- Metal `currentAllocatedSize` is an actual device allocation counter. Go
  `runtime.MemStats` counts Go allocations during measured submission plus the
  final drain; Objective-C allocation counts and hardware bandwidth counters
  are unavailable. Uploaded pose bytes are exact; driver-side copies are not
  measured.
- GPU timings are command-buffer `GPUStartTime`/`GPUEndTime` after completion.
  They include every encoded pass, not presentation or per-stage counters.
  Presentation intervals use drawable callbacks when available. A separate
  callback group drains for at most 250 ms after GPU completion, outside the
  measured submission window. The report records exact available/missing counts
  and the fraction of measured rows with timestamps. Offscreen reports none;
  callbacks delayed past that bounded drain and row snapshot are excluded.
  The dylib remains loaded for process lifetime because presentation callbacks
  may finish after command-buffer completion.

The Go report writes `report.json`, `frames.csv` (including warmup rows), and
`final.png`. Percentiles exclude warmup and exclude zero GPU/presentation
measurements. Native errors fail the run; there are no performance claims in
this implementation alone.

Apple's primary API contracts:
[GPU start](https://developer.apple.com/documentation/metal/mtlcommandbuffer/gpustarttime),
[GPU end](https://developer.apple.com/documentation/metal/mtlcommandbuffer/gpuendtime),
[drawable presentation](https://developer.apple.com/documentation/metal/mtldrawable),
[drawables guidance](https://developer.apple.com/library/archive/documentation/3DDrawing/Conceptual/MTLBestPracticesGuide/Drawables.html),
[device allocation](https://developer.apple.com/documentation/metal/mtldevice/currentallocatedsize).
The installed SDK headers also document these methods and timing availability.

## Live streaming

`Scene.Live != nil` switches `Run` to `LiveSource.Next(seconds, input)`; an absent
source retains the original sampled replay. The caller owns `LiveSource.Close`.
Live scenes need no `Frames` array. They supply a third immutable input texture,
`Scene.SpriteAtlas`, uploaded once as slot 2, and all meshes upload at startup,
including meshes with no initial instances or empty/nonrendering geometry.
Empty vertex/index storage has a minimum allocation and is never copied or drawn.

Every live submission packs per-mesh instance spans and pose offsets in Go,
then passes one 232-byte `NMLiveUpload` pointer through purego. The bridge copies
all slices synchronously before returning. Each of the three reusable slots
owns pose input and computed output, instance-offset, 64-byte visual, sprite, light, selector, distortion and overlay buffers, fog texture, plus CPU draw spans. A slot
is modified only after bounded-queue acquisition; the serial Metal command queue
has finished the earlier work that used it. Growth replaces only that free
slot's buffer, doubling capacity; it does not wait for or resize other slots.
Command buffers retain their encoded resources. The final capture uses the last
slot's poses, spans, sprites, lights and uniform snapshot after the GPU drains.
Actual ring capacities are reported separately from exact per-frame upload bytes.

## Rigid pose interpolation

The endpoint ABI is unchanged: two column-major 64-byte matrices per piece,
including the final world-Z reflection from `meshscene.battleAffine` and
`committedMatrices`. A compute pass runs before the first model render pass,
once per submitted piece, in replay and live mode. All body, outline and shadow
vertices read its single output matrix. Final capture recomputes through that
same path after draining the queue. Quaternion conversion never runs per vertex.

For a changing rotation the shader removes reflection from the three basis
columns, normalizes the first column, orthogonalizes and normalizes the second,
and obtains the third by their cross product. This repairs small fixed-point
composition errors into a proper rotation for quaternion conversion. The
quaternions are normalized, and a negative dot product flips the second one to
choose the shortest arc. Dot products above 0.9995 use normalized linear
interpolation to avoid division by a vanishing sine; other pairs use slerp.
The resulting rotation restores the world-Z reflection. Origins interpolate
linearly between the already composed endpoint translations.

Alpha zero and one copy their respective spatial endpoint lanes exactly.
Equal basis columns bypass quaternion conversion at every alpha, including
pieces translating without rotation; equal translations bypass mixing too.
Thus static poses retain their original corner heights without normalization
noise. The current endpoint's `Mat4[3]` base-shade flag and `Mat4[15]` visibility
marker are preserved, with visibility still suppressed whenever either endpoint
is hidden. These flags are not interpolated rotation or translation data.
Live units and 3DO features also tag `Mat4[11]=1` and store their endpoint
model-origin Y in `Mat4[7]`. When both endpoints are tagged, the compute pass
interpolates that origin on the GPU with the position; integer corner-height
keys subtract this displayed origin. Untagged replay/projectile/debris poses
keep their existing visual-origin fallback. XYZ products ignore these row lanes.
This avoids changing face ownership merely because a wreck is sinking.

This is an Enhanced presentation approximation under DESIGN_GPU_RENDERER §5.3,
with no simulation writes or RNG. The per-piece hierarchy was composed before
upload: origins still take straight paths between composed endpoints rather
than being recomputed from interpolated local hierarchy rotations. A rotating
parent can therefore change child spacing during an interval even though each
piece's basis remains rigid. Endpoint fixed-point scale errors are retained at
alpha zero/one and equal bases; changing rotations normalize those errors away.

Each in-flight slot owns its computed output buffer, with 64 bytes per piece
before capacity rounding. Replay allocates the exact pose capacity (minimum
64 bytes); live capacity doubles from 4096 bytes only in the acquired free
slot. `nm_info` reports their summed actual buffer lengths as
`pose_interpolation_output_buffer_capacity_bytes`, including both allocated
replay and live storage, and names the interpolation method. No CPU readback,
per-stage timing or independent performance claim is added; the existing
command-buffer GPU span includes the compute pass.

Sprites match `meshscene.Sprite`'s six float4 lanes (96 bytes); lights match its
two float4 lanes (32 bytes). Sprite positions blend on the GPU. Live projection uses the production world
anchor shear, z minus half of world height; replay retains its original z minus
full-height basis. Live depth uses the independent z-plus-twice-height axis.
Normals and light distances stay in physical world coordinates. Rect corners are
rotated about the world anchor in screen space, scaled by camera zoom and drawn
as instanced billboards. `Params.w` is added directly to normalized clip depth:
negative values bring a sprite forward. Billboards test model depth without
writing depth, retain supplied source order, and blend premultiplied RGB with
ordinary alpha or additive color. Sprite glow is additive emission. Transparent
sprite ordering is owned by the source: the battle adapter sorts by anchor depth,
which cannot reproduce per-pixel height. Material lighting, palette brightening
and exact retail overlap/composition behavior remain outside this prototype.

Live mode uses up to the first 32 submitted lights for diffuse model lighting
and terrain spill. Additional lights are counted as dropped. Only actual sprite
emission feeds bloom; replay's synthetic lights, model threshold emission and
shock rings are suppressed. The Effects option controls lights/emission/bloom,
while ordinary sprites remain visible. `VisualPasses` enables the additional live presentation described below.

Native input polls held arrows (also WASD for spectators), accumulated vertical wheel delta and a
non-repeating space pause toggle. The source owns camera movement, zoom and
pause behavior; the renderer only delivers input and uses the returned camera.
Keys reset on focus loss. Offscreen input is empty. Playable scenes also receive
ordered pointer/key edges and compose their supplied HUD, as described below.

`live-frames.csv` includes warmup rows, source `Next` time, Go validation/packing
preparation time, committed tick, the wall seconds passed to `Next`, its returned
interpolation alpha, dynamic draw/geometry counts and exact uploads (poses,
offsets, visuals, pass selectors, sprites, lights, distortion and fog). Native encoding includes native buffer growth,
memcpy and command encoding; GPU span covers the encoded passes. These costs
are separate; `Next` may sample a background source and is not the simulation's
per-tick CPU time. Go allocation totals are process-wide and include a live
source's concurrent worker, rather than isolating render-thread allocations.
JSON contains their percentiles and per-frame quantity ranges,
means and totals. Live counters replace replay's constant geometry/upload fields.

## Retained live visual passes

The 2026-10-06 continuation uses 64-byte retained vertices (material selector plus
three scalar reserved words), 16-byte materials and normalized UV frame rectangles.
Material zero retains replay's direct atlas UV. Other vertices contain local UV;
the vertex shader maps it into the selected immutable rectangle, clamping a team
frame to the material's authored count. Material lookup remains in the A/B path
so that both paths can consume the same live scene topology.

`Options.VisualPasses` selects these presentation additions. The CLI owns the
default. When disabled, no shadow mask or additional HDR target is allocated,
fog/distortion payloads are not copied and cloak subjects are not isolated.
The grounded/effects/airborne ordering remains. Existing lighting, sprite bodies
and bloom remain. Replay retains its original synthetic
light and ring workload; these new passes run only for live scenes.

Enabled live pass order is:

1. Eligible mesh instances draw into a full-size R8 shadow mask under maximum
   blending. Models with no shadow flag are absent from its sparse instance list.
2. Flat painted TNT renders with that mask and no scene-depth writes. Short
   sprite features follow in source order, then grounded model bodies/outlines.
3. Projectile/debris models and the remaining sprite batch follow. Airborne
   units (committed MoverMode other than 1) render last on a fresh depth plane.
   Opaque frames keep all three phases in one render encoder: a fullscreen
   depth-only draw clears pass B's depth without colour/emission stores and
   reloads. Frames with cloaked subjects split encoders to resolve scratch.
   Constructing instances use original authored edge pairs; subdivision creates
   no extra outlines. Sprites depth-test without writing depth. Sprite emission
   keeps the production bright-texel threshold; smoke stays non-emissive.
4. Existing two half-size bloom compute passes run when Effects is enabled.
5. Color and bloom resolve into HDR world color. An active real-distortion frame
   copies that immutable image into a second HDR target, then draws one ordered
   batch of bounded source quads into it. Rings precede wreck heat and tree heat;
   a covered later source replaces an earlier one from the same immutable sample.
   Discards preserve prior coverage, so there is no recursive refraction.
6. Final composition samples fog modulation in original projected world X/Z,
   after bloom and distortion. Fog coordinates are never distorted. Effects off
   suppresses real distortion as well as lighting/emission/bloom.

The distortion GPU clock is committed age/clock plus Alpha minus one. Alpha one
therefore freezes at the committed tick. Ring expansion, attack and quadratic
fade use DESIGN_GPU_RENDERER §25's 15-tick curve; supplied shape lanes already
contain admitted extent/profile tuning. Heat uses the production shared drifting/widening plume profile
from `gpurender/tree_heat.go`, with source phase and strength multiplier. The
blast bipolar polynomial is the production shared distortion formula too. Plume coordinates and displacement scale once
with camera zoom. Sampling is clamped to the viewport. No fragment loops over
all effects; one fragment evaluates only its rasterized source quad.

These are prototype display approximations, not new retail claims:

- Construction clips continuous world Y from min to max bounds. Native edge
  lines approximate retail's row endpoint outlines and can differ at joins.
- Cloak first renders opaque geometry into reusable private colour/emission/depth
  targets, then resolves each covered pixel once with half opacity and native
  scene depth. A keyed presenter and its direct Cargo share that resolve in
  published Cargo order. This fixes per-triangle alpha accumulation but is not
  destination-palette ALP or staged wrapped byte-height composition. Keyless
  parent cached/live lane separation remains approximate; direct cargo is opaque.
  Missing-parent models retain independent child handling. Only roots and their
  direct published cargo are traversed, as in the current production present walk.
- Shadows are opaque geometric triangle footprints at a vertical sampled-terrain
  receiver, offset five world pixels in X, with fixed 35 percent darkness.
  Maximum blending prevents stacked darkness. Texture alpha is not a silhouette
  cutout; construction is not applied to the shadow. Retail mobile image copies,
  structure projected placement, body punch and palette ALP are not reproduced.
- Painted terrain has no reconstructed scene depth. The immutable height field
  remains only for geometric shadow receivers; water and submerged model
  appearance still need their own presentation implementation.
- Real distortion retains **two** extra full-size RGBA16F world targets and a
  full-size R8 shadow mask, 17 bytes per output pixel. The immutable snapshot
  copies the whole viewport on active frames, unlike production's clipped-region
  copy. This memory/bandwidth cost is explicitly included in native allocation
  and frame timing. No extra copy or distortion batch runs without sources.
- Fog is bilinear RGB modulation rather than palette arithmetic. Smoke receiver
  scattering is omitted: the shared sprite ABI has no semantic smoke lane.

`live-frames.csv` and JSON report sparse shadow/outline draw counts and submitted
triangles/edges, eligible construction/shadow instance counts, distortion count,
fog upload bytes and dynamic GPU resource copies. Native info records actual ring
capacities, retained material/height storage and extra HDR target storage. CPU
encoding and command-buffer GPU spans continue to cover every added operation;
completion callbacks and final capture retain their original lifetime rules.

Verification for this unit is cgo-disabled Go build and native clang bridge build.
Standalone `xcrun metal` is unavailable on the host without the optional Metal
Toolchain; startup compilation through `newLibraryWithSource` is the required
runtime shader check. No tests or benchmarks were run by the backend agent;
real-device runs and visual inspection are performed by the coordinating agent.


## Playable input and HUD

`Scene.Playable` selects the native playable input mode through `nm_playable`.
`nm_input` pumps Cocoa events and samples camera arrows, accumulated wheel zoom
and spectator space pause. Spectators also retain WASD pan; playable WASD keys are command
keys. Held keys clear when the window loses focus, and unfinished pointer drags
receive synthetic button releases on that transition. Ordinary received drag
releases outside the content bounds are retained.

`nm_events` drains ordered 32-byte `NMEvent` records without another event pump.
Pointer buttons are left/right/middle 0/1/2. Kinds are move 1, down 2,
up 3, key-down 4, key-up 5, modifiers 6, text 7, focus 8 and pinch 9.
Modifiers are Shift 1, Control 2, Alt 4 and Command 8. Key records carry the
first `charactersIgnoringModifiers` scalar in Key and the Cocoa virtual keycode
in Button; text records carry the translated Unicode scalar and preserve case.
The production adapter translates editing/Control/Alt tokens from key records,
and accepts ordinary text only from text records, preventing duplicate commands.
Focus Key is 0 for loss and 1 for gain. Pointer-down Key retains Cocoa's click
count; even counts at least two identify an OS double-click. Wheel records are
move records: Key has precise 1, momentum 2 and wheel-present 4, Button carries the signed 32-bit
integer zoom notch count, and WheelX/Y retain raw scrolling point deltas. Precise
GUI wheel distances scale by 0.1; pan keeps raw logical-point distances; zoom
uses only ordinary nonmomentum integer notches. Pinch WheelY is magnification,
with Key phase bits began 1, ended 2 and cancelled 4.

Pointer coordinates convert from Cocoa window coordinates through the content
view, flip Y for an unflipped view and scale its bounds into physical drawable
pixels. The production adapter divides by the supplied device scale. The native
queue holds 8,192 events. Only consecutive wheel-free moves coalesce. If
exhausted, newest events are dropped and `input_queue_overflow` reports every
drop explicitly. Wheel events retain their ordered position and also feed
`Input.Zoom`; production uses only the ordered records. Right clicks are
captured before Cocoa can interpret them as contextual-menu input. Playable
mode hides the system cursor while the production HUD draws its own cursor.
Command-Q closes the window so the run can drain and write its report.

The synchronous live upload is 232 bytes. It includes a 64-byte-per-quad HUD
stream copied into the reusable three-slot native resource ring. The initial
overlay atlas uploads once at texture slot 3; model/terrain/sprite remain slots
0/1/2. A nonzero changed `LiveFrame.OverlayVersion` uploads `OverlayTexture`;
version zero or an unchanged version leaves it alone. `OverlayDirty` is x/y/w/h
within the full strided RGBA image; an empty rectangle means full upload. First
version and dimension changes also upload the whole image. Each free ring slot
owns an aligned upload buffer. The CPU copies only the dirty rows into it, and
a GPU blit writes the retained atlas before HUD drawing on the same serial
command buffer. Earlier readers finish before that write; no in-use atlas is
CPU-written. Resizing replaces the texture, and earlier command buffers retain
the old one. Reports count exact copied pixel bytes separately from staging
buffer capacity.

HUD params select overlay atlas 0, terrain 1 or current fog 2. Params.y selects
normal alpha 0 or destination-RGB multiplication 1. Consecutive blend runs are
split without reordering commands. Atlas sampling uses nearest filtering.
Normal colors and atlas pixels are straight RGBA, premultiplied exactly once
by the fragment shader; multiply factors stay unpremultiplied with alpha one.
The HUD runs after final world/fog composition, including when visual passes
are disabled, into BGRA8 with no depth attachment. Final capture executes the
same HUD pass. Reports include quad counts, exact upload bytes, retained atlas
bytes and actual native ring capacities. `LiveFrame.Exit` ends the loop before
packing or submitting the returned frame, then drains and writes its report.

`Options.Frames == 0` is interactive until window close (visible output only).
Go and native timing histories retain the same chronological last 8,192 submitted
frames; global frame IDs and total submission count remain explicit. Native
creation uses negative capacity to select this bounded circular history.
Each record carries a separate generation so a delayed presentation callback
cannot write into a reused row. The ring never drains periodically; only the
existing final GPU/presentation drain runs on exit or capture. Positive finite
frame counts retain their complete warmup-inclusive history as before.

## Production pointer capture

`nm_pointer_capture(renderer, captured)` follows the production client's
`PointerCaptured` request after host service. It returns zero if CoreGraphics
cannot establish relative capture. Capture saves the native content-view point,
centers the OS cursor and disassociates it from mouse motion. Received motion
accumulates Cocoa relative deltas into unbounded physical drawable coordinates,
subtracting the center warp from the first motion event. The existing production
drag-scroll controller spends successive coordinate differences, and the input
adapter divides by device scale as for ordinary pointer positions.

Release restores the saved content-view point and reassociates the cursor. Its
ordered motion record becomes the following host service's absolute fallback;
the production client already paints the release frame at its semantic restore
point. Focus loss and renderer destruction also release native capture. As with
the existing Ebitengine adapter, native capture starts after the host poll, so
its saved native point can differ from an older queued button record's point
(T25). The bridge does not reconstruct movement from an unfocused interval.

## Cocoa service while the prototype runs

The manual pump drains queued NSEvents without waiting, then dispatches or
captures them in order. It also explicitly services the main thread's default
CFRunLoop at startup and before/after each event drain. Each pass has a zero
wait interval and consumes at most eight ready port sources; a source flood
cannot take over the presentation loop. This lets AppleEvents, accessibility
requests, ready timers and observers run while Go owns frame scheduling.
`finishLaunching` alone does not replace ongoing run-loop service. Native info
records service calls and handled-source counts. These are implementation
counters; whether a particular external app activation succeeds still requires
a visible runtime check.

Apple's contracts: [CFRunLoopRunInMode](https://developer.apple.com/documentation/corefoundation/cfrunloopruninmode(_:_:_:))
defines a zero interval as one nonblocking pass, and the [run-loop guide](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/RunLoopManagement/RunLoopManagement.html)
explains source, timer and observer dispatch.


Short sprite features (authored height below 10) carry Current.w=1. Go packs
these in publication order at the beginning of the sprite upload, preserving
each feature's shadow/body order. NMLiveUpload.ground_sprite_count marks the
prefix; the native encoder draws it between TNT and the models, with no depth
writes. Other sprites draw between grounded and airborne models and are stably
sorted at display alpha using the biased/clamped anchor depth of the vertex
shader. Tall-feature row ordering and individual strip barriers remain approximate.

The upload includes 16-byte CPU cloak-member records (mesh, packed instance,
subject group, phase) and an airborne-instance count. There are nine mesh-span
lanes: all instances, shadows, one reserved lane, three body phases, and three
outline phases. Selectors need at most three entries per submitted instance.
Cloaked members bypass the opaque body/outline lists but retain ordinary shadows.
Their records are copied synchronously into each in-flight slot. Opaque mesh
instances stay batched. Shared cloak scratch is allocated lazily at first use,
20 bytes per drawable pixel: two RGBA16F targets plus Depth32F. A serial GPU queue
and tracked resources order scratch reuse; it remains resident until shutdown.
Capture drains live commands and runs the same path. The native info record
reports its bytes. No cloak means no scratch allocation.


Fresh wreck body emission is sampled while packing each displayed frame, using
committed age plus alpha like the production client. Wreck plume strength and
heat clock use that same fractional age on the GPU. The authored cell phase is
added in ticks before conversion to radians. Plume anchors interpolate matched
feature identities, snapping only new identities. Wreck light positions/strength
still sample committed ticks, and plume bounds remain prepared neutral bounds.
The production HUD host forwards the existing independent BlastRings,
FireShimmer, WreckGlow and WreckShimmer switches and invalidates its cached
publication when they change. Other Enhanced effect controls remain unsupported
by the native world; this is not a complete settings implementation.
