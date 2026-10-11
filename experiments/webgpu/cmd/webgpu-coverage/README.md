# Authored native model coverage fixture

Build from `experiments/webgpu` without changing either module:

```sh
GOWORK=off CGO_ENABLED=0 go build -o /private/tmp/webgpu-coverage ./cmd/webgpu-coverage
/private/tmp/webgpu-coverage -out /private/tmp/webgpu-coverage-NEW
```

The output directory must be new, its parent must exist, and it must be outside
every checkout. Native execution is
the qualification owner's responsibility; CPU checks/builds alone do not pass
this gate. The body pipeline's pinned Metal runtime-array-size ABI correction
must be integrated and independently qualified before native execution. This
command does not change the Models API or provide that prerequisite.

The command opens a small GoGPU host window and borrows its actual Metal device
and framework encoder. It renders only three authored material-zero meshes to
an 8×8 RGBA16Float target with transparent clear. Green in the one-texel authored
texture takes the model shader's explicit white sentinel path, so the vertices
give exact red/blue without retail assets or a palette guess. Pose metadata
selects unshaded, visible identity transforms.

The explicit doubled direct projection is
`screen=.5*floor(2*x,2*z-y)+(4,4)`, with camera zero and step/scale one. One-pixel
screen slots start at `(2,2)`, `(4,2)` and `(6,2)`, and must pack to even-origin
2×2 atlas blocks. Source coordinates follow
`(screen-slot.screen.xy)*2+slot.atlas.xy`. All four source sample centres stay
strictly away from every authored triangle edge:

| Pixel | Authored 2×2 source | Bit 8 on | Bit 8 off |
|---|---|---|---|
| `(2,2)` | TL red, other three clear | `(0.25,0,0,0.25)` | `(1,0,0,1)` |
| `(4,2)` | Left column red, right column blue, all opaque | `(0.5,0,0.5,1)` | `(1,0,0,1)` |
| `(6,2)` | BR blue, other three clear | `(0,0,0.25,0.25)` | `(0,0,0,0)` |

Values are premultiplied RGBA. Four source-only cases and four retained-group
cases cover both switches and opacity one/one-half. A half-opacity case halves
the world values once while every source/final atlas texel stays unchanged.
Groups have independent immutable member source slots 4–6 and reserved final
slots 1–3; only the three final slots are committed. `DebugResources` exposes
the source atlas without groups and the merged final atlas with groups.

Each case submits through the framework, polls `host.FrameFence` in a later
callback, maps both readbacks, and checks every logical atlas byte and every HDR
channel including clear padding around the three world pixels. Only then may
the next case call `Prepare` or reuse render targets. Texture-to-buffer copies
are the last texture uses in that encoder. The acquired surface is never read.
The command never finishes or submits the borrowed encoder itself.

Raw `.atlas.rgba8` and `.world.rgba16f` files include aligned row padding; each
comparison records the actual dimensions and row byte counts. The world format
is little-endian IEEE half float. `.slots.bin` and `.instances.bin` are detached
Models packing snapshots. `result.json` and stdout include actual/expected
samples, exact half-float bits, mismatch counts, adapter metadata and failures.
Any framework Error log is fatal: both the public GoGPU logger and default
`slog` catch the pin's deferred Finish/Submit errors. Missing cases, mismatches,
nonfinite output, readback/file errors and watchdog expiry cannot report success.

This is hardware mesh-raster/retained-resolve evidence for GPU design
§17.4–§17.7, not retail span-raster equivalence or GPU/scanout timing.
