# Overnight real-battle prototype

User-authorized 2026-10-11 after P0–P4: continue the prototype in this branch,
without merging main, to one playable skirmish against Modern AI on Great Divide.
This supersedes the original stop-after-P4 scope for the isolated experiment;
it does not accept the dependency qualification or adopt a production renderer.

Reuse the ordinary command battle shell, client input/HUD, committed frame pair,
RetainedBattle and its production composition helpers. An experiment build overlay
may reuse the existing command sources without editing production files or root
module dependencies. Modern AI and simulation remain the existing implementation.
Windowed play is the first path; pinned fullscreen/native loss/browser gaps stay
explicit. No renderer may claim parity for an omitted pass.

## First parallel units and frozen API

All APIs live in experiments/webgpu/internal/battlegpu. Rendering borrows the
host encoder and render pass. Public Scene/LiveFrame inputs are meshscene's
existing types; slices remain borrowed only through the synchronous Encode.
All world pipelines target the supplied surface format and Depth32Float.
No optional GPU features. No per-pass submission or window/device ownership.
Each component owns and releases only the resources it creates, after host
completion. Prepare uploads detached presentation data, never simulation state.

Models unit owns models.go, model*.wgsl, liveposes.wgsl, models_test.go only:

    NewModels(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat,
              scene *meshscene.Scene) (*Models, error)
    (*Models).Prepare(f meshscene.LiveFrame, width, height int) error
    (*Models).Compute(enc *wgpu.CommandEncoder) error
    (*Models).Draw(pass *wgpu.RenderPassEncoder, phase uint32)
    (*Models).DrawShadows(pass *wgpu.RenderPassEncoder)
    (*Models).DrawSlot(pass *wgpu.RenderPassEncoder, slot uint32)
    (*Models).DrawShadowSlot(pass *wgpu.RenderPassEncoder, slot uint32)
    (*Models).Close()
    (*Models).Counters() ModelCounters

ModelCounters fields are Draws, Dispatches, Instances, Pieces, Triangles,
UploadBytes int. Draw phase is meshscene.PhaseGround/Effects/Air. Preserve
material/palette/SHD, hidden endpoints, reflection-aware rigid interpolation,
production rules and lighting as supported by current composition. Inspect
Metal and Modern sources; independently port our MIT implementation to WGSL.
Report unsupported face/painter/water/lighting treatments explicitly.

Layers unit owns layers.go, terrain.wgsl, sprites.wgsl, fog.wgsl,
overlay.wgsl, layers_test.go only:

    NewLayers(d *wgpu.Device, q *wgpu.Queue, format gputypes.TextureFormat,
              scene *meshscene.Scene) (*Layers, error)
    (*Layers).Prepare(f meshscene.LiveFrame, width, height int) error
    (*Layers).DrawTerrain(pass *wgpu.RenderPassEncoder)
    (*Layers).DrawSprites(pass *wgpu.RenderPassEncoder, phase uint32)
    (*Layers).SetWorldTexture(view *wgpu.TextureView) error // borrowed, rebind on resize
    (*Layers).DrawEffects(pass *wgpu.RenderPassEncoder, layer uint32) // 0..4
    (*Layers).Missing() map[string]int // explicit unsupported-operation counts
    (*Layers).DrawFog(pass *wgpu.RenderPassEncoder)
    (*Layers).DrawOverlay(pass *wgpu.RenderPassEncoder)
    (*Layers).SetFeaturePaintOrder(enabled bool)
    (*Layers).DrawFeature(pass *wgpu.RenderPassEncoder, originalSource uint32) bool
    (*Layers).DrawLooseSprites(pass *wgpu.RenderPassEncoder, phase uint32)
    (*Layers).Close()
    (*Layers).Counters() LayerCounters

DrawFog renders a full-screen world copy with fog into a distinct output
attachment, sampling the borrowed completed world texture; overlay follows.
LayerCounters fields are Draws, Sprites, Overlay, UploadBytes int. Sprite phase
0 means short ground features, 1 normal features/projectile effects. Preserve
original terrain tile lookup/detail filtering, sprite color/alpha/additive,
fog mask operations and foreground atlas/quad semantics. Stock effect layers
are the production source; report each unsupported operation explicitly.

Components are self-contained with private helpers prefixed model/layer to avoid
collisions. The coordinator owns renderer.go, host/build overlay, input mapping,
launch tools, reporting, runtime/visual verification and integration. It does
not edit unit-owned files while their agent is active. Agents read AGENTS.md,
ARCHITECTURE, INVARIANTS, DESIGN_GPU_RENDERER, DESIGN_METAL_RENDERER and the
relevant meshscene/Metal shaders before implementing. One bounded commit each;
coordinator reviews and integrates their commits into this prototype branch.

## Completion and evidence

One command opens a real Great Divide battle, Arm human versus Core Modern AI,
using retail assets from the existing install and writable outputs outside the
repository. Verify keyboard/mouse selection, movement/build orders, camera,
minimap/HUD, terrain, features, 3D material/shading and naturally emitted effects.
Compare matching live benchmark scenes at matching camera/seed/count/draw cadence
against classic and modern; preserve census and captures. Report CPU/GPU/display
observations separately; dependency APIs still limit native GPU/presentation time.
Run affected checks, experimental checks, independent review and root gates.
No main merge; retain this worktree and the launch command for morning testing.

## Live host dependency gate

The combined command's CGO-disabled link fails: GoFFI and Ebitengine purego both
define `_cgo_init` through their fakecgo packages. That path stops at linking.
The independent CGO-enabled build links and completed a bounded real-battle
launch on Apple M3 Pro; build-play deliberately selects CGO=1. No dependency
source patch, release substitution or root module change is adopted.

The first live capture verified terrain/features/fog/HUD but exposed missing
model body pixels despite submitted topology. Shader compilation and counters
were insufficient evidence. A narrow quad-load rewrite corrected pinned Naga
Metal output; actual model color/shadow/pose readbacks and later real captures
verify body pixels. A subsequent real-combat glow binding failure stopped that
path; separated source/blur modules then produced actual source/blur/terrain-light
pixels. See LIVE_REPORT.md for the failed and corrected evidence. Benchmark timing and public Native GPU
timing remain separate: callback/source/encode/completion CPU measurements do
not establish GPU duration or presentation cadence.

## Completed windowed handoff

One-command Great Divide play, real Modern AI growth, native keyboard/mouse
selection/orders/Solar Collector construction and graceful close were observed.
Matching coastal/field benchmark state snapshots and final captures were retained.
A local effect-barrier pipeline cache improved cadence while preserving every
WebGPU final capture pixel. Fullscreen, loss, Rust and browser dependency gates
remain stopped; root checks fail at the documented nested-module source scan.
No guard or production file was changed. LIVE_REPORT.md owns the final evidence,
fidelity boundaries and recommendation; keep this worktree, no main merge.
