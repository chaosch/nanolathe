// Immutable source rasters feed disjoint final slots. A fragment walks the
// producer's member order, so no optional read-write storage texture or source
// mutation is needed. Cached-plane seeds/chronology remain an upstream gap.
struct Uniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32>}
struct Slot {screen:vec4<f32>,atlas:vec4<f32>,params:vec4<f32>}
struct Group {info:vec4<u32>,counts:vec4<u32>,clip:vec4<f32>}
struct Member {instance:u32,slot:u32,delta:i32,reserved:u32}
@group(0) @binding(0) var<storage,read> groups:array<Group,__GROUPS__>;
@group(0) @binding(1) var<storage,read> members:array<Member,__MEMBERS__>;
@group(0) @binding(2) var<storage,read> slots:array<Slot,__SLOTS__>;
@group(0) @binding(3) var<uniform> u:Uniforms;
@group(0) @binding(4) var sourceColor:texture_2d<f32>;
@group(0) @binding(5) var sourceMetadata:texture_2d<f32>;
const corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
struct Out {@builtin(position) position:vec4<f32>,@location(0) screen:vec2<f32>,@location(1) @interpolate(flat) index:u32}
fn modelGroupVertex(slot:Slot,corner:vec2<f32>,index:u32)->Out {
 let pixel=slot.atlas.xy+corner*slot.atlas.zw;
 var out:Out;out.position=vec4<f32>(pixel.x/u.extent.z*2.0-1.0,1.0-pixel.y/u.extent.w*2.0,0.0,1.0);
 out.screen=slot.screen.xy+corner*slot.screen.zw;out.index=index;return out;
}
@vertex fn group_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {
 var group=groups[iid];var slot=slots[group.info.z-1u];var corner=corners[vid];return modelGroupVertex(slot,corner,iid);
}
@vertex fn copy_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {
 var slot=slots[iid];var corner=corners[vid];return modelGroupVertex(slot,corner,iid);
}
fn modelGroupLocal(slot:Slot,screen:vec2<f32>)->vec2<f32> {return (screen-slot.screen.xy)*(slot.atlas.zw/slot.screen.zw);}
// Current Modern/Metal retained groups saturate shifted keys before admission
// and storage (GPU design §22.4). This presentation contract differs from the
// retail widened-compare/byte-store law; source keys still retain their own wrap.
fn modelGroupStored(key:u32,delta:i32)->u32 {
 if delta>=256{return 255u;}if delta<=-256{return 0u;}
 return u32(clamp(i32(key)+delta,0,255));
}
fn modelGroupAdmit(prior:u32,key:u32,delta:i32,strict:bool)->bool {
 let shifted=modelGroupStored(key,delta);
 if strict{return prior<shifted;}return prior<=shifted;
}
@fragment fn copy_fragment(input:Out)->@location(0) vec4<f32> {
 var slot=slots[input.index];let local=modelGroupLocal(slot,input.screen);let at=vec2<i32>(slot.atlas.xy+floor(local));
 return textureLoad(sourceColor,at,0);
}
@fragment fn group_fragment(input:Out)->@location(0) vec4<f32> {
 var group=groups[input.index];var color=vec4<f32>(0.0);var key=0u;
 for(var j=0u;j<group.counts.x;j++) {
  var member=members[group.info.w+j];var slot=slots[member.slot-1u];let local=modelGroupLocal(slot,input.screen);
  if ((local<vec2<f32>(0.0)).x||(local<vec2<f32>(0.0)).y)||((local>=slot.atlas.zw).x||(local>=slot.atlas.zw).y){continue;}
  let at=vec2<i32>(slot.atlas.xy+floor(local));let nextColor=textureLoad(sourceColor,at,0);let metadata=textureLoad(sourceMetadata,at,0);
  let ownKey=u32(round(metadata.r*255.0));let keyed=group.counts.z!=0u;
  var delta=0;if keyed{delta=member.delta;}
  let parent=member.instance==group.info.x;
  // Production PreserveHoles retains an erased higher-key winner for ordinary
  // groups. Construction groups admit only visible child coverage (§22.4).
  let hole=keyed&&group.counts.y!=0u&&metadata.g>0.5&&modelGroupAdmit(key,ownKey,delta,true);
  let visible=nextColor.a>0.0&&(!keyed||modelGroupAdmit(key,ownKey,delta,false));
  if parent||hole||visible {color=nextColor;key=modelGroupStored(ownKey,delta);}
 }
 if group.counts.z!=0u&&group.counts.w!=0u {
  let k=f32(key);let clip=group.clip;
  if (clip.x>0.5&&clip.x<1.5&&k<=clip.y)||(clip.z>0.5&&k<=clip.w){color=vec4<f32>(0.0);}
 }
 return color;
}
