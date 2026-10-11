// An integer texel stores exactly the production uint tile bits. Unlike a
// runtime storage array this needs no native Metal buffer-size metadata.
@group(0) @binding(0) var reflectionColor:texture_2d<f32>;
@group(0) @binding(1) var reflectionSoft:texture_2d<f32>;
@group(0) @binding(2) var reflectionFlags:texture_storage_2d<r32uint,write>;
var<workgroup> reflectionPartial:array<u32,128>;
@compute @workgroup_size(128) fn reflection_classify(@builtin(workgroup_id) tile:vec3<u32>,@builtin(local_invocation_index) lane:u32) {
 var bits=0u;let size=textureDimensions(reflectionColor);
 for(var i=lane;i<4096u;i+=128u){
  let p=tile.xy*64u+vec2<u32>(i%64u,i/64u);
  if(p.x>=size.x || p.y>=size.y){continue;}
  let c=textureLoad(reflectionColor,vec2<i32>(p),0);
  if(c.r>0.0 || c.g>0.0 || c.b>0.0 || c.a>0.0){bits|=1u;}
  if(textureLoad(reflectionSoft,vec2<i32>(p),0).r>0.0){bits|=2u;}
  if(bits==3u){break;}
 }
 reflectionPartial[lane]=bits;workgroupBarrier();
 for(var stride=64u;stride>0u;stride/=2u){
  if(lane<stride){reflectionPartial[lane]|=reflectionPartial[lane+stride];}
  workgroupBarrier();
 }
 if(lane==0u){textureStore(reflectionFlags,vec2<i32>(tile.xy),vec4<u32>(reflectionPartial[0],0u,0u,0u));}
}
