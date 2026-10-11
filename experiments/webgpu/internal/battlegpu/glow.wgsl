// Port of our MIT Metal glow kernels; GPU design §§13.3/19. Byte
// contributions accumulate in RGBA16F, then every shrink/blur re-quantizes.
struct GlowUniform {weights0:vec4<f32>,weights1:vec4<f32>,blur:vec4<f32>,size:vec4<f32>,octaves:vec4<u32>}
struct GlowVertex {positionUV:vec4<f32>,color:vec4<f32>,params:vec4<f32>}
@group(0) @binding(0) var<uniform> glowU:GlowUniform;
@group(0) @binding(1) var<storage,read> glowVertices:array<GlowVertex>;
@group(0) @binding(2) var glowAtlas:texture_2d<f32>;
@group(0) @binding(3) var glowCompleted:texture_2d<f32>;
struct GlowOut {@builtin(position) position:vec4<f32>,@location(0) uv:vec2<f32>,@location(1) color:vec4<f32>,@location(2) params:vec4<f32>}
fn glowCorner(vid:u32)->u32 {let corners=array<u32,6>(0,1,2,1,2,3);return corners[vid%6];}
@vertex fn glow_source_vertex(@builtin(vertex_index) vid:u32)->GlowOut {
 let corner=glowCorner(vid);let index=vid/6*4+corner;let a=glowVertices[index];var o:GlowOut;
 o.position=vec4<f32>(a.positionUV.x/glowU.size.x*2-1,1-a.positionUV.y/glowU.size.y*2,0,1);
 o.uv=a.positionUV.zw;o.color=a.color;o.params=a.params;return o;
}
fn glowByte(c:vec4<f32>)->vec4<f32> {return floor(clamp(c,vec4<f32>(0),vec4<f32>(1))*255+0.5)/255;}
@fragment fn glow_source_fragment(in:GlowOut)->@location(0) vec4<f32> {
 let op=u32(in.params.x+0.5);var c=in.color.rgb;
 if(op==1 || op==2){
  let p=vec2<i32>(floor(in.uv));let size=vec2<i32>(textureDimensions(glowAtlas));
  if(p.x<0 || p.y<0 || p.x>=size.x || p.y>=size.y){return vec4<f32>(0);}
  let tex=textureLoad(glowAtlas,p,0);if(tex.a<0.5){return vec4<f32>(0);}
  if(op==1){c=tex.rgb*smoothstep(in.params.y,1.0,max(tex.r,max(tex.g,tex.b)));}
  else {let high=min(1+floor(tex.r*255+0.5)/30,2.0)-1;c=textureLoad(glowCompleted,vec2<i32>(in.position.xy),0).rgb*high;}
 }else if(op==3){
  let d=floor(in.uv);if(dot(d,d)>in.params.z){return vec4<f32>(0);}
  c=textureLoad(glowCompleted,vec2<i32>(in.position.xy),0).rgb*in.params.w;
 }
 c*=in.color.a;return glowByte(vec4<f32>(c,max(c.x,max(c.y,c.z))));
}
