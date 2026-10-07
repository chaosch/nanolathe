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
including meshes with no initial instances.

Every live submission packs per-mesh instance spans and pose offsets in Go,
then passes one 96-byte `NMLiveUpload` pointer through purego. The bridge copies
all slices synchronously before returning. Each of the three reusable slots
owns pose, instance-offset, sprite and light buffers plus CPU draw spans. A slot
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
sprite overlaps are not sorted by depth; material lighting, palette brightening
and exact retail overlap/composition behavior remain outside this prototype.

Live mode uses up to the first 32 submitted lights for diffuse model lighting
and terrain spill. Additional lights are counted as dropped. Only actual sprite
emission feeds bloom; replay's synthetic lights, model threshold emission and
shock rings are suppressed. The Effects option controls lights/emission/bloom,
while ordinary sprites remain visible. Real battle distortion is omitted.

Native input polls held arrows/WASD, accumulated vertical wheel delta and a
non-repeating space pause toggle. The source owns camera movement, zoom and
pause behavior; the renderer only delivers input and uses the returned camera.
Keys reset on focus loss. Offscreen input is empty. No command/order UI or other
input is implemented here.

`live-frames.csv` includes warmup rows, source `Next` time, Go validation/packing
preparation time, tick, dynamic draw/geometry counts and exact uploads (poses,
offsets, sprites and lights). Native encoding includes native buffer growth,
memcpy and command encoding; GPU span covers the encoded passes. These costs
are separate; `Next` may sample a background source and is not the simulation's
per-tick CPU time. Go allocation totals are process-wide and include a live
source's concurrent worker, rather than isolating render-thread allocations.
JSON contains their percentiles and per-frame quantity ranges,
means and totals. Live counters replace replay's constant geometry/upload fields.
