# Retained Metal parity and comparison work

User scope: continue unattended through renderer parity fixes, then compare Classic CPU, production Modern GPU and retained Metal with the full simulation on the existing coastal and large field scenes. No tests; build, visual inspection and runtime measurements. Prototype branch only, no main landing.

## Current work

- Shared deterministic full-simulation benchmark driver: benchmark_contract, isolated worktree /private/tmp/nanolathe-metal-benchmark-parity.
- Production-selected texture/material export and atlas: parity_audit, /private/tmp/nanolathe-metal-material-parity.
- Water surface input/shader port reviewed and integrated (fbcb9f445); root wiring native bridge.
- Production model reveal/waterline/Digger/shadow metadata: play_controller, /private/tmp/nanolathe-metal-water-parity.
- Root owns native bridge/shaders, live frame packing, host integration and runtime review.

## Remaining sequence

1. Integrate and inspect water, materials and model rules. Fix runtime/visual failures.
2. Replace global cross-subject depth with ordered subject composition; preserve retained mesh uploads and GPU transforms. Reuse production order and stock effect producers where possible.
3. Restore missing stock effects and inspect coastal/field captures against production. Audit enhanced water/reflections/finishes/shadow/wake differences explicitly.
4. Measure all three renderers sequentially on matching scene state, full simulation and viewport, including larger mesh/texture Metal stress. Validate fingerprints, census and captures before comparing timings. Diagnose and address material regressions.
5. Package a playable candidate and concise findings/recommendations with reproducible artifacts and honest remaining differences.

## Review constraints

Do not claim parity from appearance alone; track unsupported families and exact reference comparisons. Do not compare Metal GPU milliseconds with Ebit CPU submission milliseconds. Keep saved settings/input/window geometry controlled. No simulation changes or invented retail behavior. Existing production/research inconsistencies are separate follow-ups, not a reason for native-only corrections.

## 2026-10-07 checkpoint

Water/material/rule exports, isolated subject atlases, production feature order,
stock effect replay, exact projectile callback identities, shared lighting/glow,
smoke receiving and ordered model shadows are integrated and runtime compiled.
The feature publication's zero identity collision no longer interpolates unrelated
wrecks. Projectile pieces now preserve the production unshaded effect entry.
Water surface/seabed promotion, model reflections and BLUE refraction run; stock
reflection sources are the active last module. Prepared material resolver work is
in review. No tests were added or run.

Matching short coastal captures preserve all four simulation state boundaries,
frame hashes, RNG counts and census. Short 120 Hz profiling found CPU material
selection and atlas allocation growth: stable atlas aspect/64-row allocation and
RGBA8 body storage reduced Metal allocation from 1.48 GB to 0.92 GB; dropping
unused fallback material work and repeated piece matching reduced Record p95
from 13.54 to 10.81 ms. These are diagnostic runs, not the final comparison.

Next: finish stock reflections and review new water arithmetic, freeze a single
binary/library/settings snapshot, run sequential coastal and large-field
Classic/Modern/Metal comparisons plus 2×/5× mesh and 2× texture stress, inspect
captures/state/census, fix measured issues, save report and playable launcher.
Native triangle/quad pixel coverage and historical cached/live group staging are
explicit parity gaps; do not claim exact renderer parity from a scene census.


## Reviewed integration checkpoint: full-resolution terrain and source faces

Isolated keyed/keyless groups, authored face rings, source-local two-chain
UV/key/SHD mapping, ordered GPU reflection admission, group reflection ownership,
reflection edge expansion, underwater outline BLUE, and unique full-resolution
TNT tile storage are wired. The reviewer caught atlas-global coordinate gates and
outline palette loss; both are corrected. Reflection empty-tile bypass produced
an identical diagnostic PNG and reduced the short sample's GPU time; this is not
a long-run performance conclusion. Supersample/local projection metadata is the
last active bounded integration before the frozen comparison build.

Runtime checks completed without native errors: six measured coastal frames
with matching Classic/Modern/Metal state/RNG/census (different build revisions,
so diagnostic only), plus the 1,500-unit field scene at 5x geometry/2x textures.
The field retained all 1,500 units in simulation, 1,121 moving, 30 construction
records and 171 units in view at tick1266. Benchmarks must distinguish full battle
size from visible model count. No tests have been added or run.

Remaining exactness gaps are source-stated: fixed-point transform rounding,
cached/direct and cached/live chronology, row-extreme outline coverage, and
edge-only shadow admission. Do not label full pixel parity achieved. Measure
real end-to-end cadence separately from GPU command time and report 120/144Hz
as targets, never inferred achievements.


Projection metadata is now integrated: keyed units and 3DO features use the
production blended/arrival origin, native/doubled anchors and record-space
rounding, including Supersample off. Keyless cached/direct mode is explicitly
unknown and falls back rather than guessing a cache decision. Reflection
far-side source expansion follows the production original-face centroid.
The frozen comparison runner pins the reflection diagnostic override to off,
and its full-simulation frame wall metric is not total CPU consumption:
production Modern overlaps background recording with pacing.


## Full comparison and final correction checkpoint

The frozen 8ffe9a70 build completed all ten sequential runs: Classic, Modern and
retained Metal on coastal and 1,500-record field battles, plus native 2x/5x
geometry with 2x atlas dimensions. Each run includes 900 measured simulation
ticks across 3,600 draws at a requested 120 Hz and 1920x1080 physical pixels.
Four boundary fingerprints/RNG counts, three available full publication pairs,
endpoint state snapshots and all authoritative census rows match. Classic's
uninterpolated view counters intentionally differ. No per-draw frame hash was
recorded. The field fixture sets its own 1,500-record capacity.

Root inspected matching final captures. Construction outlines still used every
authored edge and produced visible extra cages; the active correction replaces
that atlas pass with production two-chain row endpoints on the GPU. For active
construction, the current All-vertex extent contains every outline endpoint with
a two-pixel margin; cached/live union only widens it, so original packet clipping
is redundant when the edge arithmetic is overflow-safe. This is an implementation
consequence of the established source contracts, not a new retail rule.

Profiled native allocation was 3.93 MiB/draw coastal and 4.31 MiB/draw field.
Reviewed follow-up commits reuse adapter-owned effect receiver arrays and replace
per-frame feature value maps with checked publication-index lookup. The latter
uses the same one-based identity as the retained feature poses. No borrowed
recorded list or GPU ring lifetime is shortened. A short post-change coastal
capture and its simulation state are byte-identical to the previous capture.
Allocation savings and final outline cost still require the repeated matrix.

Initial host throughput was about 55/101/102 FPS coastal and 64/96/95 FPS field
for Classic/Modern/Metal. These are host cadence rates with full simulation, not
GPU throughput or observed scanout. The current full-feature workloads do not
hold steady 120 Hz. Prior prototype measurements with fewer completed passes
are historical experiments, not interchangeable with this comparison.

## GPU outline integration

The composed battle path now prepares immutable authored rings on the GPU and
resolves each native pixel block's two-chain endpoints after body raster and
before group merge. It preserves each texel's maximum key independently of the
single block-level colour test, clears erased live winners, and keeps transformed
blocks disjoint at fractional output scale. Prepared corner storage is compact
and does not grow with face subdivision. Root and a read-only peer reviewed the
module; Go/native builds and runtime Metal compilation succeeded. The short
coastal capture rendered eight active outline subjects with zero unsupported
rings across all 66 completed frames. No tests were run.

Remaining implementation gap: production admits some three/four-corner rings to
an exact rational key path after checking float32 equivalence. The prototype
currently takes the float32 walk/floor path for all rings; negative fractional
keys may differ by one byte. This is recorded as
TODO(retained-outline-admission), alongside the existing transform/cache gaps.
Do not claim exact pixel parity. The final frozen comparison repeats all ten
1080p cases and adds four 3456x2160 field cases before the report is finalized.


## Final measured checkpoint

All fourteen frozen 193e663f cases completed and passed their saved-artifact
validation: ten 1920x1080 cases and four 3456x2160 field cases, full simulation
throughout. Root inspected matching captures at both resolutions and the final
ordinary-match opening; the extra construction cage is gone. Native errors and
all-frame unsupported-outline counts are zero. No tests were run, no production
landing attempted, and no complete victory/defeat match played.

The report and launcher are packaged under the renderer-comparison artifact
directory and the findings are copied into docs/PROTOTYPE_METAL.md, whose opening
now points to the latest comparison. Baseline host FPS is 55/101/103 coastal,
65/94/96 field at 1080p and 30/69/66 field at 3456x2160 (Classic/Modern/Metal).
The larger view contains 729–821 unit anchors, with the same 1,433–1,502 units
in simulation. Fivefold geometry with doubled atlas dimensions measures about
90/93/57 native FPS across those scenes. Remaining fidelity contracts above
and 120/144 Hz remain unmet; the recommendation is opt-in experimentation,
per-pass GPU/bandwidth work and focused visual parity work before adoption.
