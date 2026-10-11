struct Uniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32>}
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct Shadow {screen:vec4<f32>,bodyScreen:vec4<f32>,bodyAtlas:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,atlas:vec4<f32>}
@group(0) @binding(0) var<storage,read> slots:array<Slot>;
@group(0) @binding(1) var<storage,read> shadows:array<Shadow>;
@group(0) @binding(2) var<uniform> u:Uniforms;
@group(0) @binding(3) var bodies:texture_2d<f32>;
@group(0) @binding(4) var silhouettes:texture_2d<f32>;
@group(0) @binding(5) var nearestSampler:sampler;
// Store the guarded corner load before arithmetic: the pinned Metal writer
// otherwise scales only the robust-access fallback in its conditional.
const corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
struct Out { @builtin(position) position:vec4<f32>,@location(0) pixel:vec2<f32>,@location(1) @interpolate(flat) index:u32 }
@vertex fn resolve_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {let s=slots[iid];var corner=corners[vid];let pixel=s.screen.xy+corner*s.screen.zw;var out:Out;out.position=vec4<f32>(pixel.x/u.extent.x*2.0-1.0,1.0-pixel.y/u.extent.y*2.0,0.0,1.0);out.pixel=s.atlas.xy+corner*s.atlas.zw;out.index=iid;return out;}
@fragment fn resolve_fragment(input:Out)->@location(0) vec4<f32> {let s=slots[input.index];let at=clamp(vec2<i32>(floor(input.pixel)),vec2<i32>(s.atlas.xy),vec2<i32>(s.atlas.xy+s.atlas.zw)-vec2<i32>(1));let color=textureLoad(bodies,at,0)*s.params.x;if color.a<0.001{discard;}return color;}
@vertex fn shadow_resolve_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {let s=shadows[iid];var corner=corners[vid];let pixel=s.screen.xy+corner*s.screen.zw;var out:Out;out.position=vec4<f32>(pixel.x/u.extent.x*2.0-1.0,1.0-pixel.y/u.extent.y*2.0,0.0,1.0);out.pixel=vec2<f32>(0.0);out.index=iid;return out;}
fn mask(s:Shadow,pixel:vec2<f32>)->f32 {let at=vec2<i32>(floor((pixel-s.screen.xy)*(s.atlas.zw/s.screen.zw)+s.atlas.xy));if any(at<vec2<i32>(s.atlas.xy))||any(at>=vec2<i32>(s.atlas.xy+s.atlas.zw)){return 0.0;}return textureLoad(silhouettes,at,0).a;}
@fragment fn shadow_resolve_fragment(input:Out)->@location(0) vec4<f32> {
 let s=shadows[input.index];if s.atlas.w<=0.0{discard;}var alpha=mask(s,input.position.xy);
 // The production aircraft blur is a separate filter. This bounded 9-tap
 // silhouette treatment is an explicit prototype approximation.
 if s.params.x>2.5&&s.params.z>0.0 {let r=s.params.z*0.5;alpha*=0.25;alpha+=(mask(s,input.position.xy+vec2<f32>(r,0.0))+mask(s,input.position.xy-vec2<f32>(r,0.0))+mask(s,input.position.xy+vec2<f32>(0.0,r))+mask(s,input.position.xy-vec2<f32>(0.0,r)))*0.125;alpha+=(mask(s,input.position.xy+vec2<f32>(r,r))+mask(s,input.position.xy+vec2<f32>(-r,r))+mask(s,input.position.xy+vec2<f32>(r,-r))+mask(s,input.position.xy-vec2<f32>(r,r)))*0.0625;}
 let bodyPixel=(input.position.xy-s.bodyScreen.xy)*(s.bodyAtlas.zw/s.bodyScreen.zw)+s.bodyAtlas.xy;
 if all(bodyPixel>=s.bodyAtlas.xy)&&all(bodyPixel<s.bodyAtlas.xy+s.bodyAtlas.zw){alpha*=1.0-textureLoad(bodies,vec2<i32>(floor(bodyPixel)),0).a;}
 alpha*=0.5;if alpha<0.001{discard;}return vec4<f32>(u.shadow.rgb*alpha,alpha);
}
