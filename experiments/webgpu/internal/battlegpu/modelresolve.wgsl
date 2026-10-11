struct Uniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32>}
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct ModelShadowUniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,tint:vec4<f32>,water:vec4<f32>}
struct ModelShadowSubject {screen:vec4<f32>,body:vec4<f32>,mask:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,order:vec4<u32>}
// Both world commit modules declare the complete union, including globals
// unused here: pinned Metal assigns dense slots across all declarations.
// Fixed arrays keep bounds guards independent of runtime size metadata,
// which the pinned Metal render path does not supply.
@group(0) @binding(0) var<storage,read> slots:array<Slot,__SLOTS__>;
@group(0) @binding(1) var<storage,read> subjects:array<ModelShadowSubject,1>;
@group(0) @binding(2) var<uniform> u:Uniforms;
@group(0) @binding(3) var bodies:texture_2d<f32>;
@group(0) @binding(4) var<uniform> shadowU:ModelShadowUniforms;
@group(0) @binding(5) var body:texture_2d<f32>;
@group(0) @binding(6) var keys:texture_depth_2d;
@group(0) @binding(7) var mask:texture_2d<f32>;
@group(0) @binding(8) var liquid:texture_2d<f32>;
// Store the guarded corner load before arithmetic: the pinned Metal writer
// otherwise scales only the robust-access fallback in its conditional.
const corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
struct Out { @builtin(position) position:vec4<f32>,@location(0) pixel:vec2<f32>,@location(1) @interpolate(flat) index:u32 }
@vertex fn resolve_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {let s=slots[iid];var corner=corners[vid];let pixel=s.screen.xy+corner*s.screen.zw;var out:Out;out.position=vec4<f32>(pixel.x/u.extent.x*2.0-1.0,1.0-pixel.y/u.extent.y*2.0,0.0,1.0);out.pixel=s.atlas.xy+corner*s.atlas.zw;out.index=iid;return out;}
@fragment fn resolve_fragment(input:Out)->@location(0) vec4<f32> {let s=slots[input.index];let at=clamp(vec2<i32>(floor(input.pixel)),vec2<i32>(s.atlas.xy),vec2<i32>(s.atlas.xy+s.atlas.zw)-vec2<i32>(1));let color=textureLoad(bodies,at,0)*s.params.x;if color.a<0.001{discard;}return color;}
