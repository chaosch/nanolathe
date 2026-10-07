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
