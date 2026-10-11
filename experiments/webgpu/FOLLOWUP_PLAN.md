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


## Zero-cgo combined host qualification

The old combined CGO_ENABLED=0 build failed with duplicate fakecgo symbols;
that attempt remains recorded. Exact-pinned goffi v0.6.3 documents the
`nofakecgo` build tag for coexistence with purego. No dependency replacement or
module-cache patch is required. A b40b570c8 external combined build with
CGO_ENABLED=0 and this tag ran the real coastal native path: final pixels, all
eight diagnostic planes and all eight state files match its CGO_ENABLED=1
build exactly. The prototype build-play tool now uses that qualified Go-only
build configuration and records it in its external manifest. Metal remains an
Apple native API reached through goffi; Go-only describes our binding/build,
not a graphics driver implemented in Go. Production build/defaults are untouched.


## Direct effects audit

The direct-effect publisher reports QuadSpanFaces and SupersampleIgnored;
the experimental fan executor now retains these exact omission counts. A
two-chain mapper and a doubled subject raster remain executor units, using the
original exported geometry. Whole-piece debris also lacks the BL2–BL4 local
face-light response; source normals/heights are available, fragments have zero
normals. The current Metal inspection lane shares this light gap, so it is a
gap against the owning Modern design, not evidence of a regression from Metal.
Suppressed subject-composition packets remain producer-limited.

The former independent ground-decal seabed limitation was stale: current
production promotes tagged static feature bodies/shadows, now implemented.
Scorches/trails are dry-admitted or dry-clipped and wakes remain above water.
No missing ground-decal promotion contract should be claimed.


## Doubled body and render ABI follow-up

The 5a729b790 native coastal run changed 287,199 final pixels, with all eight
end-state files unchanged and every independent shadow plane byte-identical.
Actual body source/final extents grew from 2048×2048 to 2048×8192. This is
activity and isolation evidence, not exact hardware triangle coverage. An
authored native fixture checks quarter coverage, color mean, top-left selection,
group-final resolve and opacity once through the real retained executor.
Hardware outline lines currently occupy half a native pixel; production native
row blocks remain an explicit unresolved executor contract.

The direct vertex ABI prerequisite at 1c536c755 passed its real native gate:
zero changed final pixels; all diagnostic planes and eight state files match
5a729b790 exactly; clean native log. Direct face lighting at 45422ae27 awaits
its separate real native active-light gate.

The emitted-MSL audit additionally found an unbound hidden runtime-array size
argument in every body/solo-shadow entry of models.wgsl. The group merge,
shadow projection and shadow commit modules already specialize capacities and
have no hidden size operand. Fix body arrays from their actual owned buffer
tiers as a bounded ABI prerequisite, preserve arithmetic, and native-qualify
before any dependent coverage, underwater or pipeline consolidation claim.
Direct-effect ordinary quad/ground/smoke entries have the same separate audit
issue; they are not corrected by fixing direct vertices alone. No native
implicit argument slot is established by these findings.


## Qualified follow-up boundary — 42aabae98

The body ABI correction 2c1990137 passed its native comparison against the
corrected direct-light build: final pixels, all 16 captured files and all eight
state files are unchanged; native log is clean. Glow ABI 19432d8b5 and layer
ABI 031be2393 each passed their own subsequent identical comparisons. All
reachable layer/glow entries now use counted allocated capacities; emitted MSL
checks establish no hidden render size argument and unchanged native indices.

Direct-light constructor 45422ae27 first failed its native gate: the initial
48-byte buffer could not satisfy the new 64-byte layout. The forward fix
106716315 aligns that initial allocation. A natural coastal frame had zero
observable light difference, so an opt-in authored facing/back/zero-normal
fixture 6849c71b8 qualifies activity independently. Facing changed all 256
interior pixels, with expected packed RGB codes 22/44/87 and one destination
half-ULP tolerance; disabled, back and zero-normal outputs are exact. The first
oracle omitted the fixture's 2.5-pixel unsheared Y separation; preserve that
failed oracle alongside the corrected BL2–BL4 semantic oracle. Renderer and
captures were unchanged. All eight state files match and logs are clean.

The eight authored native model coverage cases passed on actual Apple M3 Pro
Metal before and after the common pipeline: ungrouped/grouped, resolve bit 8
off/on and opacity 1/.5. Every logical source/final RGBA8 byte and HDR output
half channel/padding matched; no framework errors. This qualifies doubled
coverage resolve, not retail integer span coverage or outline row blocks.

Atlas sharing 7bd79d564 passed unchanged native pixels/captured files/state.
Layers owns the exact StockFrame.Atlas upload; Glow borrows its current view
after Layers.Prepare, and never releases it. Selected current texture descriptor
payloads decreased by 67,108,864 bytes, from 808,942,241 to 741,833,377. This
inventory excludes other owners and driver caches, and is not actual GPU heap
usage. Selected tracked owners are zero after Close; no whole-driver leak claim.

Common body/shadow commit pipeline 42aabae98 passed its separate unchanged
pixel/plane/state native gate. Capacity partitions FirstInstance before either
record load; no arithmetic, ordering or draw count change. Empty shadow storage
contains one full 96-byte subject, also exercised by the actual no-shadow
authored fixture. Clean Coast/Field comparison and independent review follow.


## Final bounded handoff

Final rendering candidate 42aabae98 and wording 6c7587764. Sequential matching
Coast/Field runs completed; state files match across renderers. Common pipeline
reduces Coast bytes/objects by 18.4%/21.6% against the same-fidelity predecessor.
One-command cgo-disabled Great Divide test completed a Solar Collector and
closed normally; ordinary camera key/wheel events were exercised. Read-only
review and complete experiment checks pass. Actual root fast/retail scripts
fail the architecture export/allowlist guard because it counts nested-module
sources; required landing gates stay failed. Keep production/root/defaults
unchanged and preserve the failed host/Rust/browser paths. Report and next
bounded recommendations are in FOLLOWUP_REPORT.md; no M1–M5 or main merge.
