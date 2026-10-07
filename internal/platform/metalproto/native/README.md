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
Three pose buffers hold two adjacent sampled frames. Every frame copies those
matrices and draws one indexed instanced call per uploaded mesh; transforms,
projection and normal lighting run in custom shaders. The bounded queue permits
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
- Matrix columns blend linearly; large rotations may lose rigidity. A zero
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
then passes one 152-byte `NMLiveUpload` pointer through purego. The bridge copies
all slices synchronously before returning. Each of the three reusable slots
owns pose, instance-offset, 64-byte visual, sprite, light, selector and distortion buffers, fog texture, plus CPU draw spans. A slot
is modified only after bounded-queue acquisition; the serial Metal command queue
has finished the earlier work that used it. Growth replaces only that free
slot's buffer, doubling capacity; it does not wait for or resize other slots.
Command buffers retain their encoded resources. The final capture uses the last
slot's poses, spans, sprites, lights and uniform snapshot after the GPU drains.
Actual ring capacities are reported separately from exact per-frame upload bytes.

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

Native input polls held arrows/WASD, accumulated vertical wheel delta and a
non-repeating space pause toggle. The source owns camera movement, zoom and
pause behavior; the renderer only delivers input and uses the returned camera.
Keys reset on focus loss. Offscreen input is empty. No command/order UI or other
input is implemented here.

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
no new render passes run and fog/distortion payloads are not copied. Existing
lighting, sprite bodies and bloom remain. Replay retains its original synthetic
light and ring workload; these new passes run only for live scenes.

Enabled live pass order is:

1. Eligible mesh instances draw into a full-size HDR shadow mask under maximum
   blending. Models with no shadow flag are absent from its sparse instance list.
2. Flat painted TNT renders with that mask. Four inverse half-height-shear
   iterations query the immutable R32F height field to write terrain surface depth
   without changing the painted terrain UVs. Model bodies then render with
   construction reveal, team UV, owner-admitted cloak opacity and cooling tint.
3. Constructing instances alone draw their original authored edge pairs as native
   lines. Subdividing the body mesh creates no extra outline edges. Sprites follow
   with ordinary depth testing and no depth writes. Sprite emission includes the
   production bright-texel threshold; smoke with zero supplied emission stays dark.
4. Existing two half-size bloom compute passes run when Effects is enabled.
5. Color and bloom resolve into HDR world color. An active real-distortion frame
   copies that immutable image into a second HDR target, then draws one ordered
   batch of bounded source quads into it. Rings precede wreck heat and tree heat;
   a covered later source replaces an earlier one from the same immutable sample.
   Discards preserve prior coverage, so there is no recursive refraction.
6. Final composition samples fog modulation in original projected world X/Z,
   after bloom and distortion. Fog coordinates are never distorted.

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
- Cloak alpha blends each ordinary depth-tested mesh surface. It is not a single
  resolved subject-image ALP blend, so overlapping faces may blend differently.
- Shadows are opaque geometric triangle footprints at a vertical sampled-terrain
  receiver, offset five world pixels in X, with fixed 35 percent darkness.
  Maximum blending prevents stacked darkness. Texture alpha is not a silhouette
  cutout; construction is not applied to the shadow. Retail mobile image copies,
  structure projected placement, body punch and palette ALP are not reproduced.
- Four inverse terrain iterations with bilinear samples approximate the painted
  terrain's original projection. Steep slopes, liquid depth and sentinel cells
  need capture review; no terrain tessellation changes the photograph.
- Real distortion retains **two** extra full-size RGBA16F world targets and a
  full-size RGBA16F shadow mask, 24 bytes per output pixel. The immutable snapshot
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
