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
