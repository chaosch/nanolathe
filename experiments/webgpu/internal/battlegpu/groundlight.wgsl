// Enhanced presentation policy from DESIGN_GPU_RENDERER §31.8. Source RGB is
// displayed emission; groundGain already includes family, age/fade and strength.
struct GroundSource {
 positionRadius:vec4<f32>, colorGroundGain:vec4<f32>,
 groundAgeFadeKind:vec4<f32>, flags:vec4<u32>
}
struct GroundDisc {rect:vec4<f32>,source:vec4<u32>}
struct GroundUniform {extent:vec4<f32>}
@group(0) @binding(0) var<storage,read> sources:array<GroundSource,64>;
@group(0) @binding(1) var<storage,read> discs:array<GroundDisc,64>;
@group(0) @binding(2) var<uniform> u:GroundUniform;
// Quad corners by vertex index (triangles 0,1,2 and 3,4,5), computed rather
// than read from a module-scope const array: naga's SPIR-V writer emits such
// an array as OpConstantNull, so on Vulkan every corner sat at the origin.
fn groundCorner(v:u32)->vec2<f32>{return vec2<f32>(select(0.0,1.0,v==1u||v==4u||v==5u),select(0.0,1.0,v==2u||v==3u||v==5u));}
struct GroundOut {@builtin(position) position:vec4<f32>,@location(0) @interpolate(flat) source:u32}
@vertex fn ground_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->GroundOut {
 var corner=groundCorner(vid);var disc=discs[iid];
 let pixel=disc.rect.xy+corner*disc.rect.zw;
 var out:GroundOut;
 out.position=vec4<f32>(pixel.x/u.extent.x*2.0-1.0,1.0-pixel.y/u.extent.y*2.0,0.0,1.0);
 out.source=disc.source.x;return out;
}
fn ground_linear(c:vec3<f32>)->vec3<f32> {
 return select(c/12.92,pow((c+vec3<f32>(0.055))/1.055,vec3<f32>(2.4)),c>=vec3<f32>(0.04045));
}
@fragment fn ground_fragment(input:GroundOut)->@location(0) vec4<f32> {
 var light=sources[input.source];
 if light.flags.y==0u||light.colorGroundGain.w<=0.0{discard;}
 let center=light.positionRadius.xy-vec2<f32>(0.0,light.positionRadius.z*0.5);
 // Both source and receiver use physical recording pixels. A field texel
 // covers two device pixels; no camera/world transform is applied again.
 let d=input.position.xy*2.0-center;
 let height=max(light.positionRadius.z-light.groundAgeFadeKind.x,0.0);
 let r2=light.positionRadius.w*light.positionRadius.w;
 let d2=dot(d,d)+height*height;
 if d2>=r2{discard;}
 var falloff=1.0-d2/r2;falloff=falloff*falloff*(3.0-2.0*falloff);
 let color=light.colorGroundGain.rgb*light.colorGroundGain.w;
 let peak=max(color.x,max(color.y,color.z));
 let energy=ground_linear(color/max(peak,0.000001))*peak*(falloff*2.0);
 // Screen blending of each bounded contribution is 1-exp(-sum(E)), up to
 // the production RGBA8 target's intermediate rounding. Alpha is not coverage.
 return vec4<f32>(vec3<f32>(1.0)-exp(-energy),1.0);
}
