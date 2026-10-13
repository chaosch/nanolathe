// Pre-blurred aircraft shadow sources (Nanolathe Enhanced presentation, GPU
// design §34). Each soft subject's body coverage is blurred once, in two
// separable passes, into its own slot of a reduced-resolution atlas; the world
// commit then takes one filtered sample instead of 25 sparse taps. The kernel
// is §34's binomial 1-4-6-4-1 at the subject's dry radius, applied to coverage
// box-filtered to the slot's texel size, so wide penumbrae no longer show the
// sparse taps' offset copies. A slot texel spans D source texels (a power of
// two, chosen so the radius is one to two slot texels).
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct ModelShadowSubject {screen:vec4<f32>,body:vec4<f32>,mask:vec4<f32>,params:vec4<f32>,placement:vec4<f32>,order:vec4<u32>}
// place: slot origin and size in atlas texels; source: source-texel origin of
// the slot's first texel, D, and the dry radius in source texels.
struct AirSlot {place:vec4<f32>,source:vec4<f32>}
// Atlas width and height, silhouette-source raster, unused.
struct AirUniforms {extent:vec4<f32>}
@group(0) @binding(0) var<storage,read> slots:array<Slot,__SLOTS__>;
@group(0) @binding(1) var<storage,read> subjects:array<ModelShadowSubject,__SUBJECTS__>;
@group(0) @binding(2) var<storage,read> air:array<AirSlot,__SUBJECTS__>;
@group(0) @binding(3) var<uniform> u:AirUniforms;
@group(0) @binding(4) var bodies:texture_2d<f32>;
@group(0) @binding(5) var body:texture_2d<f32>;
@group(0) @binding(6) var across:texture_2d<f32>;
@group(0) @binding(7) var airSampler:sampler;
const airCorners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
struct AirOut {@builtin(position) position:vec4<f32>,@location(0) @interpolate(flat) subject:u32}
@vertex fn air_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->AirOut {
 var out:AirOut;out.subject=iid;out.position=vec4<f32>(2.0,2.0,2.0,1.0);
 let s=subjects[iid];let a=air[iid];
 if s.params.x<2.5||a.place.z<=0.0{return out;}
 // Store the guarded corner load before arithmetic for the pinned writer.
 var corner=airCorners[vid];
 let p=a.place.xy+corner*a.place.zw;
 out.position=vec4<f32>(p.x/u.extent.x*2.0-1.0,1.0-p.y/u.extent.y*2.0,0.5,1.0);return out;
}
fn airWeight(at:i32)->f32 {if at==2{return 6.0;}if at==1||at==3{return 4.0;}return 1.0;}
// Bilinear coverage at continuous source position p (texel centres at +0.5).
// Every source rectangle keeps a transparent margin, so only a footprint
// wholly outside it is rejected.
fn airCoverage(p:vec2<f32>,rect:vec4<f32>,reuse:bool)->f32 {
 let t=p-vec2<f32>(0.5);
 if !(((t+vec2<f32>(1.0)>rect.xy).x&&(t+vec2<f32>(1.0)>rect.xy).y)&&((t<rect.xy+rect.zw).x&&(t<rect.xy+rect.zw).y)){return 0.0;}
 if reuse{let c=textureSampleLevel(bodies,airSampler,p/vec2<f32>(textureDimensions(bodies)),0.0);return c.a;}
 let c=textureSampleLevel(body,airSampler,p/vec2<f32>(textureDimensions(body)),0.0);return c.a;
}
// Mean coverage of the D x D source block centred on p, which lies on a texel
// corner: one bilinear tap per 2 x 2 texels, also on corners.
fn airBox(p:vec2<f32>,d:f32,rect:vec4<f32>,reuse:bool)->f32 {
 if d<1.5{return airCoverage(p,rect,reuse);}
 let n=i32(d*0.5);var sum=0.0;
 for(var y=0;y<n;y++){for(var x=0;x<n;x++){
  sum+=airCoverage(p+vec2<f32>(f32(2*x-n+1),f32(2*y-n+1)),rect,reuse);
 }}
 return sum/f32(n*n);
}
@fragment fn air_across(input:AirOut)->@location(0) vec4<f32> {
 let s=subjects[input.subject];let a=air[input.subject];
 let reuse=s.body.z<=0.0;var rect=s.body;
 if reuse{let slot=slots[s.order.x-1u];rect=slot.atlas;}
 let d=a.source.z;let q=a.source.xy+(input.position.xy-a.place.xy)*d;var sum=0.0;
 for(var i=0;i<5;i++){sum+=airWeight(i)*airBox(q+vec2<f32>(f32(i-2)*a.source.w,0.0),d,rect,reuse);}
 return vec4<f32>(sum/16.0,0.0,0.0,1.0);
}
@fragment fn air_down(input:AirOut)->@location(0) vec4<f32> {
 let a=air[input.subject];let spacing=a.source.w/a.source.z;let p=input.position.xy;var sum=0.0;
 for(var i=0;i<5;i++){
  // A tap beyond this slot reads nothing: the blurred value there is zero.
  let v=p+vec2<f32>(0.0,f32(i-2)*spacing);
  if v.y>a.place.y-0.5&&v.y<a.place.y+a.place.w+0.5{let c=textureSampleLevel(across,airSampler,v/u.extent.xy,0.0);sum+=airWeight(i)*c.r;}
 }
 return vec4<f32>(sum/16.0,0.0,0.0,1.0);
}
