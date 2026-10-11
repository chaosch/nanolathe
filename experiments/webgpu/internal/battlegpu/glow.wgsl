// Port of our MIT Metal glow kernels; GPU design §§13.3/19. Byte
// contributions accumulate in RGBA16F, then every shrink/blur re-quantizes.
struct GlowUniform {weights0:vec4<f32>,weights1:vec4<f32>,blur:vec4<f32>,size:vec4<f32>,octaves:vec4<u32>}
struct GlowVertex {positionUV:vec4<f32>,color:vec4<f32>,params:vec4<f32>}
@group(0) @binding(0) var<uniform> glowU:GlowUniform;
@group(0) @binding(1) var<storage,read> glowVertices:array<GlowVertex>;
@group(0) @binding(2) var glowAtlas:texture_2d<f32>;
@group(0) @binding(3) var glowCompleted:texture_2d<f32>;
@group(0) @binding(4) var glowInput:texture_2d<f32>;
@group(0) @binding(5) var glowDestination:texture_storage_2d<rgba16float,write>;
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
fn glowRead(p:vec2<f32>,region:vec4<i32>)->vec4<f32> {
 let q=vec2<i32>(floor(p));if(q.x<region.x || q.y<region.y || q.x>=region.z || q.y>=region.w){return vec4<f32>(0);}
 return textureLoad(glowInput,q,0);
}
fn glowWeight(i:u32)->f32 {
 if(i<4){let weights=glowU.weights0;return weights[i];}
 let weights=glowU.weights1;return weights[i-4];
}
fn glowKernel(p:vec2<f32>,dir:vec2<f32>,region:vec4<i32>,far:bool)->vec4<f32> {
 var sum=vec4<f32>(0);
 if(!far){
  let centreWeight=glowWeight(0);sum=glowRead(p,region)*centreWeight;
  for(var i=1u;i<=4;i++){
   let distance=glowU.blur.x*f32(i);let weight=glowWeight(i);
   sum+=(glowRead(p+dir*distance,region)+glowRead(p-dir*distance,region))*weight;
  }
 }else{
  let k=0.5/(glowU.blur.y*glowU.blur.y);let reach=2.5*glowU.blur.y;let tail=exp(-0.5*2.5*2.5);var norm=0.0;
  for(var i=0u;i<48;i++){
   let d=f32(i)-23.5;if(abs(d)>reach){continue;}
   let weight=max(exp(-d*d*k)-tail,0.0);norm+=weight;sum+=glowRead(p+dir*d,region)*weight;
  }
  sum/=norm;
 }
 return glowByte(sum);
}
@compute @workgroup_size(16,16) fn glow_shrink(@builtin(global_invocation_id) invocation:vec3<u32>) {
 let q=invocation.xy;let dstSize=textureDimensions(glowDestination);if(q.x>=dstSize.x || q.y>=dstSize.y){return;}
 let srcSize=textureDimensions(glowInput);var sum=vec4<f32>(0);
 for(var y=0u;y<4;y++){for(var x=0u;x<4;x++){
  let p=q*4+vec2<u32>(x,y);if(p.x<srcSize.x && p.y<srcSize.y){sum+=clamp(textureLoad(glowInput,vec2<i32>(p),0),vec4<f32>(0),vec4<f32>(1));}
 }}
 textureStore(glowDestination,vec2<i32>(q),glowByte(sum/16));
}
@compute @workgroup_size(16,16) fn glow_across(@builtin(global_invocation_id) invocation:vec3<u32>) {
 let q=invocation.xy;let dstSize=textureDimensions(glowDestination);if(q.x>=dstSize.x || q.y>=dstSize.y){return;}
 let srcSize=textureDimensions(glowInput);let far=q.x>=srcSize.x;var p=vec2<f32>(q)+0.5;
 if(far){p=vec2<f32>((f32(q.x-srcSize.x)+0.5)*2,f32(q.y)+0.5);}
 let c=glowKernel(p,vec2<f32>(1,0),vec4<i32>(0,0,i32(srcSize.x),i32(srcSize.y)),far);
 textureStore(glowDestination,vec2<i32>(q),c);
}
@compute @workgroup_size(16,16) fn glow_down(@builtin(global_invocation_id) invocation:vec3<u32>) {
 let q=invocation.xy;let dstSize=textureDimensions(glowDestination);if(q.x>=dstSize.x || q.y>=dstSize.y){return;}
 let qw=glowU.octaves.x;let qh=glowU.octaves.y;let ew=glowU.octaves.z;let eh=glowU.octaves.w;let fx=qw+2;
 let near=q.x>=1 && q.x<qw+1 && q.y>=1 && q.y<qh+1;
 let far=q.x>=fx && q.x<fx+ew && q.y>=1 && q.y<eh+1;var c=vec4<f32>(0);
 if(near){c=glowKernel(vec2<f32>(q)-0.5,vec2<f32>(0,1),vec4<i32>(0,0,i32(qw),i32(qh)),false);}
 else if(far){c=glowKernel(vec2<f32>(f32(qw+(q.x-fx))+0.5,(f32(q.y-1)+0.5)*2),vec2<f32>(0,1),vec4<i32>(i32(qw),0,i32(qw+ew),i32(qh)),true);}
 textureStore(glowDestination,vec2<i32>(q),c);
}
