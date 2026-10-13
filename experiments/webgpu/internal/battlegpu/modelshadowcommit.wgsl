// Both world commit modules declare the complete union, including globals
// unused here: pinned Metal assigns dense slots across all declarations.
struct Uniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32>}
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct ModelShadowUniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,tint:vec4<f32>,water:vec4<f32>}
struct ModelShadowSubject {screen:vec4<f32>,body:vec4<f32>,mask:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,order:vec4<u32>}
struct AirSlot {place:vec4<f32>,source:vec4<f32>}
@group(0) @binding(0) var<storage,read> slots:array<Slot,1>;
@group(0) @binding(1) var<storage,read> subjects:array<ModelShadowSubject,__SUBJECTS__>;
@group(0) @binding(2) var<uniform> mainU:Uniforms;
@group(0) @binding(3) var bodies:texture_2d<f32>;
@group(0) @binding(4) var<uniform> u:ModelShadowUniforms;
@group(0) @binding(5) var body:texture_2d<f32>;
@group(0) @binding(6) var keys:texture_depth_2d;
@group(0) @binding(7) var mask:texture_2d<f32>;
@group(0) @binding(8) var bodyKeys:texture_2d<f32>;
@group(0) @binding(9) var shadowSampler:sampler;
@group(0) @binding(10) var airBlur:texture_2d<f32>;
@group(0) @binding(11) var<storage,read> air:array<AirSlot,__SUBJECTS__>;
// Quad corners by vertex index (triangles 0,1,2 and 3,4,5), computed rather
// than read from a module-scope const array: naga's SPIR-V writer emits such
// an array as OpConstantNull, so on Vulkan every corner sat at the origin.
fn modelShadowCorner(v:u32)->vec2<f32>{return vec2<f32>(select(0.0,1.0,v==1u||v==4u||v==5u),select(0.0,1.0,v==2u||v==3u||v==5u));}
struct ModelShadowCommitOut {@builtin(position) position:vec4<f32>,@location(0) screen:vec2<f32>,@location(1) @interpolate(flat) subject:u32,@location(2) wet:f32}
@vertex fn model_shadow_commit_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->ModelShadowCommitOut {
 let s=subjects[iid];var corner=modelShadowCorner(vid);var out:ModelShadowCommitOut;
 out.screen=s.screen.xy+corner*s.screen.zw;out.subject=iid;
 // A soft subject's record carries its corners' water grade in place of a
 // mask rectangle; the rasterizer spreads it across the quad.
 out.wet=0.0;if s.params.x>2.5{out.wet=mix(mix(s.mask.x,s.mask.y,corner.x),mix(s.mask.z,s.mask.w,corner.x),corner.y);}
 out.position=vec4<f32>(out.screen.x/u.extent.x*2.0-1.0,1.0-out.screen.y/u.extent.y*2.0,0.5,1.0);return out;
}
// A reused subject (zero body rectangle) reads its own composed body raster
// instead of a separate silhouette source: the same coverage, as retail's
// shadow copies the cached body image [03 R-REN-03D §1]. Both sources keep
// the byte key in red (the body raster in its key plane, a silhouette in
// its colour) and coverage in alpha.
// Every source rectangle has a transparent margin of at least one doubled
// pixel, so a fetch that straddles its edge reads no neighbour's coverage;
// only a fetch wholly outside the rectangle is rejected.
fn modelShadowInside(t:vec2<f32>,size:vec2<f32>,rect:vec4<f32>)->bool {
 return ((t+size>rect.xy).x&&(t+size>rect.xy).y)&&((t<rect.xy+rect.zw).x&&(t<rect.xy+rect.zw).y);
}
// One gather returns the 2x2 block whose top-left texel is t, in gather
// order x:(0,1) y:(1,1) z:(1,0) w:(0,0); it replaces four loads.
fn modelShadowAlpha4(t:vec2<f32>,reuse:bool)->vec4<f32> {
 if reuse{let g=textureGather(3,bodies,shadowSampler,(t+vec2<f32>(1.0))/vec2<f32>(textureDimensions(bodies)));return g;}
 let g=textureGather(3,body,shadowSampler,(t+vec2<f32>(1.0))/vec2<f32>(textureDimensions(body)));return g;
}
fn modelShadowKey4(t:vec2<f32>,reuse:bool)->vec4<f32> {
 if reuse{let g=textureGather(0,bodyKeys,shadowSampler,(t+vec2<f32>(1.0))/vec2<f32>(textureDimensions(bodyKeys)));return round(g*255.0);}
 let g=textureGather(0,body,shadowSampler,(t+vec2<f32>(1.0))/vec2<f32>(textureDimensions(body)));return round(g*255.0);
}
// A soft subject's source, blurred once per frame at its dry radius
// (modelair.wgsl), sampled at slot position p. Outside its slot it is zero.
fn modelShadowAir(p:vec2<f32>,a:AirSlot)->f32 {
 if !modelShadowInside(p-vec2<f32>(0.5),vec2<f32>(1.0),a.place){return 0.0;}
 let c=textureSampleLevel(airBlur,shadowSampler,p/vec2<f32>(textureDimensions(airBlur)),0.0);return c.r;
}
@fragment fn model_shadow_commit_fragment(input:ModelShadowCommitOut)->@location(0) vec4<f32> {
 let s=subjects[input.subject];var alpha=0.0;
 // Source mapping: a silhouette source sits at the shadow raster; a reused
 // body sits in its model slot at that slot's own raster (production 2x).
 let reuse=s.body.z<=0.0;var rect=s.body;var origin=s.placement.xy;var bodyRaster=max(u.tint.w,1.0);
 if reuse{let slot=slots[s.order.x-1u];rect=slot.atlas;origin=slot.screen.xy;bodyRaster=slot.atlas.z/max(slot.screen.z,1.0);}
 if s.params.x>2.5 {
  let wet=clamp(input.wet,0.0,1.0);let a=air[input.subject];
  var at=a.place.xy+(rect.xy+(input.screen-origin-s.placement.zw)*bodyRaster-a.source.xy)/a.source.z;var cover=0.0;
  if wet>0.0{
   // Over water the shadow ripples and its filter widens by half a world
   // pixel: four taps at (+-h, +-h) add h^2 of variance to the pre-blurred
   // radius, making it radius+dr. Dry pixels skip all of this.
   let world=(input.screen-u.extent.xy*0.5)/u.camera.z+u.camera.xy;
   let p=world-u.water.yz*6.0;let t=u.water.x;
   let warp=vec2<f32>(sin(p.y*0.14+t*0.8)+0.35*sin(p.x*0.09-t*0.55),0.45*sin((p.x+p.y)*0.11+t*0.65));
   at+=warp*(wet*s.params.w)*bodyRaster/a.source.z;
   let dr=0.25*wet*s.params.w*bodyRaster;let h=sqrt(2.0*a.source.w*dr+dr*dr)/a.source.z;
   cover=(modelShadowAir(at+vec2<f32>(h,h),a)+modelShadowAir(at+vec2<f32>(-h,h),a)+modelShadowAir(at+vec2<f32>(h,-h),a)+modelShadowAir(at+vec2<f32>(-h,-h),a))*0.25;
  }else{cover=modelShadowAir(at,a);}
  alpha=cover*mix(0.5,0.20,wet);
 }else{
  // An output pixel's samples sit one source texel apart: all four of a
  // doubled block, or its top-left alone at one sample per pixel.
  let block=floor(input.screen);let raster=max(u.tint.w,1.0);
  var pick=vec4<f32>(0.0,0.0,0.0,1.0);if u.tint.w>1.5{pick=vec4<f32>(1.0);}
  let samples=dot(pick,vec4<f32>(1.0));let first=block+vec2<f32>(0.5)/raster;
  var bp=rect.xy+(first-origin)*bodyRaster;if s.params.x>1.5{bp-=s.placement.zw*bodyRaster;}
  let t=floor(bp);var covered=vec4<f32>(0.0);
  if modelShadowInside(t,vec2<f32>(2.0),rect){covered=select(vec4<f32>(0.0),vec4<f32>(1.0),modelShadowAlpha4(t,reuse)>vec4<f32>(0.5));}
  if s.params.x<1.5{
   let mt=floor(s.mask.xy+(first-s.placement.zw)*raster);var masked=vec4<f32>(0.0);
   if modelShadowInside(mt,vec2<f32>(2.0),s.mask){let m=textureGather(0,mask,shadowSampler,(mt+vec2<f32>(1.0))/vec2<f32>(textureDimensions(mask)));masked=select(vec4<f32>(0.0),vec4<f32>(1.0),m>vec4<f32>(0.5));}
   // Punching is all-samples only: a wholly covered block casts nothing.
   if dot(pick,covered)<samples{alpha=dot(pick,masked)/samples*0.5;}
  }else{
   var shadowed=covered;
   if s.params.y>=0.5{shadowed*=select(vec4<f32>(0.0),vec4<f32>(1.0),modelShadowKey4(t,reuse)>vec4<f32>(s.params.y));}
   alpha=dot(pick,shadowed)/samples*0.5;
  }
 }
 return vec4<f32>(u.tint.rgb*alpha,alpha);
}
