struct Uniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32>}
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct ModelShadowUniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,tint:vec4<f32>,water:vec4<f32>}
struct ModelShadowSubject {screen:vec4<f32>,body:vec4<f32>,mask:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,order:vec4<u32>}
struct AirSlot {place:vec4<f32>,source:vec4<f32>}
// Keep every declaration in union layout order: pinned Metal assigns dense
// native indices across the full module, including an entry's unused globals.
@group(0) @binding(0) var<storage,read> slots:array<Slot,__SLOTS__>;
@group(0) @binding(1) var<storage,read> subjects:array<ModelShadowSubject,__SUBJECTS__>;
@group(0) @binding(2) var<uniform> mainU:Uniforms;
@group(0) @binding(3) var bodies:texture_2d<f32>;
@group(0) @binding(4) var<uniform> shadowU:ModelShadowUniforms;
@group(0) @binding(5) var body:texture_2d<f32>;
@group(0) @binding(6) var keys:texture_depth_2d;
@group(0) @binding(7) var mask:texture_2d<f32>;
@group(0) @binding(8) var bodyKeys:texture_2d<f32>;
@group(0) @binding(9) var shadowSampler:sampler;
@group(0) @binding(10) var airBlur:texture_2d<f32>;
@group(0) @binding(11) var<storage,read> air:array<AirSlot,__SUBJECTS__>;

__BODY_HELPERS__
__SHADOW_HELPERS__

// The allocated body slot capacity partitions FirstInstance. No tag bit or
// source operand changes; decode the family before either record is loaded.
const modelCommitSlotCapacity:u32=__SLOTS__u;
struct ModelCommitOut {@builtin(position) position:vec4<f32>,@location(0) pixel:vec2<f32>,@location(1) @interpolate(flat) index:u32,@location(2) wet:f32}
@vertex fn model_commit_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->ModelCommitOut {
 var out:ModelCommitOut;out.index=iid;out.wet=0.0;
 if iid<modelCommitSlotCapacity {
  var value=modelCommitBodyVertex(vid,iid);out.position=value.position;out.pixel=value.pixel;
 }else{
  var value=modelCommitShadowVertex(vid,iid-modelCommitSlotCapacity);out.position=value.position;out.pixel=value.screen;out.wet=value.wet;
 }
 return out;
}
@fragment fn model_commit_fragment(input:ModelCommitOut)->@location(0) vec4<f32> {
 if input.index<modelCommitSlotCapacity {
  var value:Out;value.position=input.position;value.pixel=input.pixel;value.index=input.index;
  return modelCommitBodyFragment(value);
 }
 var value:ModelShadowCommitOut;value.position=input.position;value.screen=input.pixel;value.subject=input.index-modelCommitSlotCapacity;value.wet=input.wet;
 return modelCommitShadowFragment(value);
}
