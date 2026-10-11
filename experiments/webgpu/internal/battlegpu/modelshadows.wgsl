// Exported retained shadows: independent ordered solo-body and authored
// quarter-projection sources, then the MIT Metal block/aircraft commit law.
struct ModelShadowUniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,tint:vec4<f32>,water:vec4<f32>}
struct ModelShadowVertex {position:vec4<f32>,piece:u32,pad0:u32,pad1:u32,pad2:u32}
struct ModelShadowPose {matrix:mat4x4<f32>,flags:vec4<u32>}
// This projection-only view preserves the shared 416-byte instance ABI.
struct ModelShadowInstance {ids:vec4<u32>,prefix:array<vec4<f32>,9>,shadow:vec4<f32>,middle:array<vec4<f32>,4>,screen:vec4<f32>,atlas:vec4<f32>,suffix:array<vec4<f32>,9>}
struct ModelShadowSubject {screen:vec4<f32>,body:vec4<f32>,mask:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,order:vec4<u32>}
@group(0) @binding(0) var<storage,read> vertices:array<ModelShadowVertex,__VERTICES__>;
@group(0) @binding(1) var<storage,read> poses:array<ModelShadowPose,__POSES__>;
@group(0) @binding(2) var<storage,read> instances:array<ModelShadowInstance,__INSTANCES__>;
@group(0) @binding(3) var<uniform> u:ModelShadowUniforms;
@group(0) @binding(4) var<storage,read> subjects:array<ModelShadowSubject,__SUBJECTS__>;
@group(0) @binding(5) var body:texture_2d<f32>;
@group(0) @binding(6) var keys:texture_depth_2d;
@group(0) @binding(7) var mask:texture_2d<f32>;
@group(0) @binding(8) var liquid:texture_2d<f32>;
struct ModelShadowProjectOut {@builtin(position) position:vec4<f32>,@location(0) @interpolate(flat) rect:vec4<f32>}
@vertex fn model_shadow_project_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->ModelShadowProjectOut {
 var out:ModelShadowProjectOut;out.position=vec4<f32>(2.0,2.0,2.0,1.0);
 let v=vertices[vid];let i=instances[iid];let pose=poses[i.ids.x+v.piece];let m=pose.matrix;
 // Structure projection excludes DontCache, independently of DontShadow.
 if m[3].w<0.5||(pose.flags.y&1u)!=0u{return out;}
 var world=(m*vec4<f32>(v.position.xyz,1.0)).xyz;let origin=select(i.shadow.z,m[1].w,m[2].w>0.5);
 let relative=floor(world.y-origin);var quarter=floor(relative/4.0);
 if u.tint.w>1.5&&relative-floor(relative/4.0)*4.0>=2.0{quarter+=0.5;}
 world.x+=quarter+5.0;world.z-=quarter;world.y=i.shadow.y;
 let screen=vec2<f32>(world.x-u.camera.x,world.z-world.y*0.5-u.camera.y)*u.camera.z+u.extent.xy*0.5;
 let pixel=(screen-i.screen.xy)*(i.atlas.zw/i.screen.zw)+i.atlas.xy;out.rect=i.atlas;
 out.position=vec4<f32>(pixel.x/u.extent.z*2.0-1.0,1.0-pixel.y/u.extent.w*2.0,0.5,1.0);return out;
}
@fragment fn model_shadow_project_fragment(input:ModelShadowProjectOut)->@location(0) f32 {
 if any(input.position.xy<input.rect.xy)||any(input.position.xy>=input.rect.xy+input.rect.zw){discard;}return 1.0;
}
const modelShadowCorners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
struct ModelShadowCommitOut {@builtin(position) position:vec4<f32>,@location(0) screen:vec2<f32>,@location(1) @interpolate(flat) subject:u32}
@vertex fn model_shadow_commit_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->ModelShadowCommitOut {
 let s=subjects[iid];var corner=modelShadowCorners[vid];var out:ModelShadowCommitOut;
 out.screen=s.screen.xy+corner*s.screen.zw;out.subject=iid;
 out.position=vec4<f32>(out.screen.x/u.extent.x*2.0-1.0,1.0-out.screen.y/u.extent.y*2.0,0.5,1.0);return out;
}
fn modelShadowTap(p:vec2<f32>,rect:vec4<f32>)->f32 {
 let at=vec2<i32>(floor(p));if any(vec2<f32>(at)<rect.xy)||any(vec2<f32>(at)>=rect.xy+rect.zw){return 0.0;}return textureLoad(body,at,0).a;
}
fn modelShadowCoverage(p:vec2<f32>,rect:vec4<f32>)->f32 {
 let a=floor(p-vec2<f32>(0.5))+vec2<f32>(0.5);let f=fract(p-vec2<f32>(0.5));
 return mix(mix(modelShadowTap(a,rect),modelShadowTap(a+vec2<f32>(1,0),rect),f.x),mix(modelShadowTap(a+vec2<f32>(0,1),rect),modelShadowTap(a+vec2<f32>(1,1),rect),f.x),f.y);
}
fn modelShadowLiquidTap(at:vec2<i32>)->f32 {
 if any(at<vec2<i32>(0))||any(at>=vec2<i32>(textureDimensions(liquid))){return 0.0;}return textureLoad(liquid,at,0).r;
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
 if s.params.x>2.5 {
  let world=(input.screen-u.extent.xy*0.5)/u.camera.z+u.camera.xy;var wet=0.0;
  if u.water.w>0.0{wet=smoothstep(0.8,1.0,modelShadowLiquid(world/u.water.w));}
  let p=world-u.water.yz*6.0;let t=u.water.x;
  let warp=vec2<f32>(sin(p.y*0.14+t*0.8)+0.35*sin(p.x*0.09-t*0.55),0.45*sin((p.x+p.y)*0.11+t*0.65));
  let raster=max(u.tint.w,1.0);let src=s.body.xy+(input.screen-s.placement.xy-s.placement.zw+warp*(wet*s.params.w))*raster;
  let radius=(s.params.z+0.5*wet*s.params.w)*0.5*raster;var sum=0.0;
  for(var y=0;y<5;y++){for(var x=0;x<5;x++){let weightX=modelShadowWeight(x);let weightY=modelShadowWeight(y);sum+=modelShadowCoverage(src+vec2<f32>(f32(x-2),f32(y-2))*radius,s.body)*weightX*weightY;}}
  alpha=sum/256.0*mix(0.5,0.20,wet);
 }else{
  let block=floor(input.screen);let raster=max(u.tint.w,1.0);var count=1;if u.tint.w>1.5{count=2;}
  var cover=0.0;var punch=0.0;
  for(var y=0;y<count;y++){for(var x=0;x<count;x++){
   let screen=block+(vec2<f32>(f32(x),f32(y))+vec2<f32>(0.5))/raster;
   var bp=s.body.xy+(screen-s.placement.xy)*raster;if s.params.x>1.5{bp-=s.placement.zw*raster;}
   let coverage=modelShadowTap(bp,s.body);
   if s.params.x<1.5{
    let mp=s.mask.xy+(screen-s.placement.zw)*raster;
    if all(mp>=s.mask.xy)&&all(mp<s.mask.xy+s.mask.zw){if textureLoad(mask,vec2<i32>(mp),0).r>0.5{cover+=1.0;}}
    if coverage>0.5{punch+=1.0;}
   }else if coverage>0.5{
    let key=256.0-round(textureLoad(keys,vec2<i32>(bp),0)*257.0);if s.params.y<0.5||key>s.params.y{cover+=1.0;}
   }
  }}
  let samples=f32(count*count);if s.params.x>1.5||punch<samples{alpha=cover/samples*0.5;}
 }
 return vec4<f32>(u.tint.rgb*alpha,alpha);
}
