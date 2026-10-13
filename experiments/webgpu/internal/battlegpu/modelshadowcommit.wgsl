// Both world commit modules declare the complete union, including globals
// unused here: pinned Metal assigns dense slots across all declarations.
struct Uniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32>}
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct ModelShadowUniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,tint:vec4<f32>,water:vec4<f32>}
struct ModelShadowSubject {screen:vec4<f32>,body:vec4<f32>,mask:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,order:vec4<u32>}
@group(0) @binding(0) var<storage,read> slots:array<Slot,1>;
@group(0) @binding(1) var<storage,read> subjects:array<ModelShadowSubject,__SUBJECTS__>;
@group(0) @binding(2) var<uniform> mainU:Uniforms;
@group(0) @binding(3) var bodies:texture_2d<f32>;
@group(0) @binding(4) var<uniform> u:ModelShadowUniforms;
@group(0) @binding(5) var body:texture_2d<f32>;
@group(0) @binding(6) var keys:texture_depth_2d;
@group(0) @binding(7) var mask:texture_2d<f32>;
@group(0) @binding(8) var liquid:texture_2d<f32>;
@group(0) @binding(9) var bodyKeys:texture_2d<f32>;
const modelShadowCorners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
struct ModelShadowCommitOut {@builtin(position) position:vec4<f32>,@location(0) screen:vec2<f32>,@location(1) @interpolate(flat) subject:u32}
@vertex fn model_shadow_commit_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->ModelShadowCommitOut {
 let s=subjects[iid];var corner=modelShadowCorners[vid];var out:ModelShadowCommitOut;
 out.screen=s.screen.xy+corner*s.screen.zw;out.subject=iid;
 out.position=vec4<f32>(out.screen.x/u.extent.x*2.0-1.0,1.0-out.screen.y/u.extent.y*2.0,0.5,1.0);return out;
}
// A reused subject (zero body rectangle) reads its own composed body raster
// instead of a separate silhouette source: the same coverage, as retail's
// shadow copies the cached body image [03 R-REN-03D §1].
fn modelShadowTap(p:vec2<f32>,rect:vec4<f32>,reuse:bool)->f32 {
 let at=vec2<i32>(floor(p));if ((vec2<f32>(at)<rect.xy).x||(vec2<f32>(at)<rect.xy).y)||((vec2<f32>(at)>=rect.xy+rect.zw).x||(vec2<f32>(at)>=rect.xy+rect.zw).y){return 0.0;}
 if reuse{let c=textureLoad(bodies,at,0);return c.a;}
 let c=textureLoad(body,at,0);return c.a;
}
fn modelShadowCoverage(p:vec2<f32>,rect:vec4<f32>,reuse:bool)->f32 {
 let a=floor(p-vec2<f32>(0.5))+vec2<f32>(0.5);let f=fract(p-vec2<f32>(0.5));
 return mix(mix(modelShadowTap(a,rect,reuse),modelShadowTap(a+vec2<f32>(1,0),rect,reuse),f.x),mix(modelShadowTap(a+vec2<f32>(0,1),rect,reuse),modelShadowTap(a+vec2<f32>(1,1),rect,reuse),f.x),f.y);
}
// The body raster stores the exact byte key in its metadata red channel; a
// silhouette source stores it as depth (256-key)/257.
fn modelShadowKey(at:vec2<i32>,reuse:bool)->f32 {
 if reuse{let k=textureLoad(bodyKeys,at,0);return round(k.r*255.0);}
 let d=textureLoad(keys,at,0);return 256.0-round(d*257.0);
}
fn modelShadowLiquidTap(at:vec2<i32>)->f32 {
 if ((at<vec2<i32>(0)).x||(at<vec2<i32>(0)).y)||((at>=vec2<i32>(textureDimensions(liquid))).x||(at>=vec2<i32>(textureDimensions(liquid))).y){return 0.0;}return textureLoad(liquid,at,0).r;
}
fn modelShadowLiquid(p:vec2<f32>)->f32 {
 let q=p-vec2<f32>(0.5);let f=fract(q);let a=vec2<i32>(floor(q));
 return mix(mix(modelShadowLiquidTap(a),modelShadowLiquidTap(a+vec2<i32>(1,0)),f.x),mix(modelShadowLiquidTap(a+vec2<i32>(0,1)),modelShadowLiquidTap(a+vec2<i32>(1,1)),f.x),f.y);
}
// Fixed five-tap weights avoid guarded array-load arithmetic in the pinned
// Metal writer; their outer product sums to 256 (GPU design §34).
fn modelShadowWeight(at:i32)->f32 {if at==2{return 6.0;}if at==1||at==3{return 4.0;}return 1.0;}
@fragment fn model_shadow_commit_fragment(input:ModelShadowCommitOut)->@location(0) vec4<f32> {
 let s=subjects[input.subject];var alpha=0.0;
 // Source mapping: a silhouette source sits at the shadow raster; a reused
 // body sits in its model slot at that slot's own raster (production 2x).
 let reuse=s.body.z<=0.0;var rect=s.body;var origin=s.placement.xy;var bodyRaster=max(u.tint.w,1.0);
 if reuse{let slot=slots[s.order.x-1u];rect=slot.atlas;origin=slot.screen.xy;bodyRaster=slot.atlas.z/max(slot.screen.z,1.0);}
 if s.params.x>2.5 {
  let world=(input.screen-u.extent.xy*0.5)/u.camera.z+u.camera.xy;var wet=0.0;
  if u.water.w>0.0{wet=smoothstep(0.8,1.0,modelShadowLiquid(world/u.water.w));}
  let p=world-u.water.yz*6.0;let t=u.water.x;
  let warp=vec2<f32>(sin(p.y*0.14+t*0.8)+0.35*sin(p.x*0.09-t*0.55),0.45*sin((p.x+p.y)*0.11+t*0.65));
  let src=rect.xy+(input.screen-origin-s.placement.zw+warp*(wet*s.params.w))*bodyRaster;
  let radius=(s.params.z+0.5*wet*s.params.w)*0.5*bodyRaster;var sum=0.0;
  for(var y=0;y<5;y++){for(var x=0;x<5;x++){let weightX=modelShadowWeight(x);let weightY=modelShadowWeight(y);sum+=modelShadowCoverage(src+vec2<f32>(f32(x-2),f32(y-2))*radius,rect,reuse)*weightX*weightY;}}
  alpha=sum/256.0*mix(0.5,0.20,wet);
 }else{
  let block=floor(input.screen);let raster=max(u.tint.w,1.0);var count=1;if u.tint.w>1.5{count=2;}
  var cover=0.0;var punch=0.0;
  for(var y=0;y<count;y++){for(var x=0;x<count;x++){
   let screen=block+(vec2<f32>(f32(x),f32(y))+vec2<f32>(0.5))/raster;
   var bp=rect.xy+(screen-origin)*bodyRaster;if s.params.x>1.5{bp-=s.placement.zw*bodyRaster;}
   let coverage=modelShadowTap(bp,rect,reuse);
   if s.params.x<1.5{
    let mp=s.mask.xy+(screen-s.placement.zw)*raster;
    if ((mp>=s.mask.xy).x&&(mp>=s.mask.xy).y)&&((mp<s.mask.xy+s.mask.zw).x&&(mp<s.mask.xy+s.mask.zw).y){if textureLoad(mask,vec2<i32>(mp),0).r>0.5{cover+=1.0;}}
    if coverage>0.5{punch+=1.0;}
   }else if coverage>0.5{
    let key=modelShadowKey(vec2<i32>(bp),reuse);if s.params.y<0.5||key>s.params.y{cover+=1.0;}
   }
  }}
  let samples=f32(count*count);if s.params.x>1.5||punch<samples{alpha=cover/samples*0.5;}
 }
 return vec4<f32>(u.tint.rgb*alpha,alpha);
}
