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
