# Parity and allocation follow-up — 2026-10-11

User request: address remaining prototype parity gaps and investigate/reduce
allocations. Continue webgpu-p0-p4, no main merge, production/root/default edits
or dependency substitution. Native fullscreen/loss, Rust and browser gates stay
stopped; renderer fixes use the independent windowed path.

## First ready units and ownership

Base 36943a8c7. Each editor has a separate worktree, bounded commit and exclusive
files. Root owns integration, profiling, layers.go and all files not assigned.
No heavy build/test work during announced native measurements. Static reading
and edits may continue; root performs real GPU qualification sequentially.

Water unit owns terrain.wgsl, water.wgsl, water_test.go. Port the current MIT
water surface/tiles contracts, not invented retail behavior. Shader contract:
LayerUniform appends waterPhase, waterMask, waterControls, waterTerrain,
waterSample (five vec4<f32>, offsets 160..224, total 240 bytes). Binding 6 is
layerWaterMask:texture_2d<f32>. Root packs f.Water and binds the existing mask.
Root embeds/appends water.wgsl to terrain.wgsl. Terrain helper
layerTerrainAt(world:vec2<f32>, filtered:bool)->vec4<f32> samples retained
painted terrain with authored tile clamping; water helper
layerWaterColor(world:vec2<f32>, original:vec4<f32>)->vec4<f32> applies surface
before the existing terrain-light resolve. Inactive water preserves pixels.
Report reflections/refraction source-plane needs; surface is one bounded unit.

Model unit owns models.go, models.wgsl, modelresolve.wgsl, modelgroups.wgsl,
models_test.go, modelgroups_test.go. Port production staged group source/merge
contracts using exported Composition.Groups/Members. Keep NewModels/Prepare/
Compute/DrawSlot/DrawShadowSlot/Close/Counters signatures unchanged. Additional
private source/key/merge resources must be core compatible and completion-owned.
Rasterize immutable member source slots and paint only final slot; preserve
signed wrapped key shifts and visible/erased coverage where provided. The host
explicitly lacks cached/live source chronology: report that upstream gap; do
not manufacture lifecycle inputs or call the approximation exact parity.

Distortion unit owns distortion.go, distortion.wgsl, distortion_test.go. Port
current MIT radial blast and wreck/tree heat field, source order and undistorted
fog position. API:

 NewDistortions(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat,
                finalSource string) (*Distortions,error)
 (*Distortions).Prepare(f meshscene.LiveFrame,width,height int) error
 (*Distortions).SetInputs(uniform *wgpu.Buffer,
                         world,grid,masks,glow *wgpu.TextureView) error
 (*Distortions).Draw(pass *wgpu.RenderPassEncoder)
 (*Distortions).Close()
 (*Distortions).Counters() DistortionCounters // Draws, Sources, UploadBytes int

Inputs borrowed; own only data/layout/pipeline/groups. SetInputs rebinds only on
replacement. Root supplies finalSource (fog declarations/helpers/entries) with
layerFinalWorld(samplePixel:vec2<f32>, destinationPixel:vec2<f32>)->vec4<f32>.
It samples world/glow at samplePixel, fog/map clip/gain at destinationPixel.
Module appends distortion shader, storage data at group0/binding5; shared final
bindings 0 uniform,1 world,2 grid,3 masks,4 glow. Draw after base final fog and
before HUD, reading the unchanged world/blur sources so later sources replace
earlier sources without feedback. This unit excludes stock palette lenses and
water reflection integration; root owns their ordered replay/integration.

## Acceptance

Read AGENTS, architecture/invariants and owning modern renderer design sections
before editing. Compile/test exact-pinned WGSL and packing arithmetic, inspect
diffs and run real native captures with operation activity before declaring
support. Compare matching simulation states and distinguish expected parity
changes from accidental pixels. Allocation-only changes require unchanged
pixels and state. Preserve failed attempts and benchmark metadata externally.
Re-profile after changes, measure classic/modern/WebGPU matching scenes; do not
claim GPU or scanout timing from callback/CPU metrics. Update evidence/report and
commit bounded units. No landing while required gates fail.
