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
current Modern saturated shifted keys and visible/erased coverage where provided. The host
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

## Next independent units

Stock lens unit: branch webgpu-lens-followup, separate worktree, based on
910ec98f8. Own only lenses.go, lenses.wgsl and lenses_test.go in battlegpu.
API: NewLenses(device,queue,format); Prepare(frame,width,height);
Snapshot(encoder,world,layer,index); Draw(pass,layer,index); Close();
Counters() LensCounters with Draws, Copies, Samples and UploadBytes. Snapshot
copies the completed world crop at the operation barrier, preserving absolute
coordinates. Root ends and resumes the world/depth pass with Load at each
barrier. Key comparison and source coordinates follow production's stock lens;
HDR values survive copies. Authored injected operations qualify this dormant
path without changing the default battle producer or borrowed packets.

Stock reflection unit: branch webgpu-stock-water-followup, separate worktree,
based on 0e3bfaff9. Own only reflections.go, reflections.wgsl,
reflectiontiles.wgsl and reflections_test.go in battlegpu. API:
NewStockReflections(device,queue,format); Prepare(frame,width,height);
SetInputs(uniform,atlas,mask); Encode(encoder); Draw(pass); Close();
Counters() ReflectionCounters with Draws, Dispatches, Sources and UploadBytes.
Sources are the already mirrored/admitted WaterSources. Borrow the shared
240-byte uniform and existing effect atlas/mask; own source color/soft planes,
conservative tile flags and output resolve. Root draws immediately after
terrain and before bodies. This unit has no model reflection sources and uses
the native zero-model-source stock corner budget of 32768. Mixed admission
cannot be claimed until the producer exports admitted model corner cost;
record that missing input at the cap. Model reflection, refraction and seabed
promotion remain separate gaps.

Root's pass-local binding cache spans compatible Models/Layers group-0
layouts. It tracks explicit Set calls only, resets at pass boundaries and
untracked distortion/reflection/lens draws, and never reorders submissions.
Every allocation-only unit requires byte-identical final pixels and simulation
states. A clean repeated benchmark separates timing from concurrent builds.


## Retained shadows and submerged features

Shadow unit owns models.go/models.wgsl/modelresolve.wgsl/models_test.go and new
modelshadows.go/modelshadows.wgsl/modelshadows_test.go in its separate worktree.
It consumes ordered BodyDraws/BodyRuleSlots and independent ProjectedDraws,
Topology and DontCache flags. Body RGBA8, depth32 keys and R8 projected masks
have their own atlas and CopySrc diagnostic views. The old resolve shadow
binding is a transparent 1×1 compatibility dummy. Cached/live chronology and
producer-omitted offscreen shadow-only subjects stay explicit gaps. The first native shadow-on/off gate failed with zero changed pixels despite
populated source planes. Split commit/projection shader modules with checked
emitted Metal resource indices fixed the binding mismatch at 4dd51c156. The
matched corrected native gate changed 94,737 pixels with identical simulation
state; preserve the failed attempt as evidence.

Seabed unit owns layers.go/terrain.wgsl and new seabed.go/seabed_test.go;
water_test.go changes only its ABI checks. API adds EncodeSeabed(encoder,depth)
and DebugSeabedResource(). It draws raw terrain and published Submerged feature
sources into a lazy viewport RGBA16F target before water and ground light.
Inactive frames retain the direct path. Model refraction/reflection, ground
seabed decals and source chronology remain independent gaps.

The pinned public texture-transition call does not update the core texture
usage tracker after a source copy. The original DMA lens path failed its real
native CopySrc-to-RenderAttachment gate. The independent replacement uses a
cropped same-format raster snapshot with integer textureLoad and no blend,
then resumes the world/depth attachments with Load. Copies in lens counters
means these raster snapshots. The actual lens fixture changed 19,968 pixels
inside its 200×100 destination without simulation change; later paint can
replace copied pixels. A separate authored controlled packet at f44d5e7f3 qualifies exact HDR
preservation in 3,600 destination pixels and key-match discard in 3,600 more
with zero semantic mismatches. Its raw source green half differs from Python
nearest-half conversion by one neighboring half value, while shader
floor(channel*255+.5) still equals the authored 8-bit key. Preserve the failed
auxiliary rounding oracle and the successful semantic oracle.

Native macOS pointer capture is also dependency-blocked: SetCursorMode is an
empty method in the exact pinned darwin platform. PointerEvent carries relative
deltas but this host cannot produce them while locked. Do not manufacture a
platform click count or a capture implementation inside the prototype.


## Shared commit binding dependency

The allocation investigation identified retained model/shadow interleaving as
remaining SetBindGroup traffic. A shared layout/group unit is ready, but its
new fixed-capacity resolve array also removes a preexisting hidden Metal
`_mslBufferSizes` argument. Isolate and qualify that ABI correction first with
unchanged independent bindings, then qualify the shared group against the
corrected baseline. No performance or pixel claim before actual native gates.
The shader compiler numbers all declared resources by type, including unused
globals; both commit modules must declare the full shared prefix.
