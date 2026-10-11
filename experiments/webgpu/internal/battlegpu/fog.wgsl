struct LayerUniform {
 camera:vec4<f32>, viewport:vec4<f32>, rect:vec4<f32>, fogRect:vec4<f32>,
 tiles:vec4<u32>, flags:vec4<u32>, ground:vec4<f32>,
}
@group(0) @binding(0) var<uniform> layerU:LayerUniform;
@group(0) @binding(1) var layerWorld:texture_2d<f32>;
@group(0) @binding(2) var layerGrid:texture_2d<f32>;
@group(0) @binding(3) var layerMasks:texture_2d<f32>;
@vertex fn layer_fog_vertex(@builtin(vertex_index) v:u32)->@builtin(position) vec4<f32> {
 let p=vec2<f32>(f32((v<<1u)&2u),f32(v&2u));return vec4<f32>(p*2-1,0,1);
}
fn layerGray(c:vec3<f32>)->vec3<f32> {
 let bytes=floor(c*255+0.5);return vec3<f32>(floor((bytes.x+bytes.y+bytes.z)/3)/255);
}
fn layerApplyFog(original:vec3<f32>,projected:vec2<f32>,checker:i32)->vec3<f32> {
 var col=original;let p=floor(projected);let cell=vec2<i32>(floor((p-layerU.fogRect.xy)/32));
 let dark=textureLoad(layerMasks,vec2<i32>(0,512),0).rgb;let size=vec2<i32>(textureDimensions(layerGrid));
 // Ordered predecessor/current cells in the same 2x2 walk as C-G7.
 for(var j=-1;j<=0;j++){for(var i=-1;i<=0;i++){
  let c=cell+vec2<i32>(i,j);if(c.x<0 || c.y<0 || c.x>=size.x || c.y>=size.y){continue;}
  let codes=vec2<u32>(round(textureLoad(layerGrid,c,0).rg*255));if(codes.x==0u && codes.y==0u){continue;}
  let d=vec2<i32>(p-layerU.fogRect.xy-vec2<f32>(c)*32);let inCell=d.x>=0 && d.y>=0 && d.x<32 && d.y<32;let inTile=d.x>=0 && d.y>=0 && d.x<64 && d.y<64;
  let code1=codes.x;let code0=codes.y;
  if(code1==1u && inCell){col=layerGray(col);}
  else if(code1==2u && inCell && checker==0){col=dark;}
  else if(code1>=3u && inTile){
   var slot=code1-3u;let dither=slot>=56u;if(dither){slot-=56u;}
   let at=vec2<i32>(i32(slot%14u)*64,i32(slot/14u)*64)+d;
   if(textureLoad(layerMasks,at,0).a>=0.5){if(!dither){col=layerGray(col);}else if(checker==0){col=dark;}}
  }
  if(code0==1u && inCell){col=dark;}
  else if(code0>=2u && inTile){
   let slot=code0-2u;let at=vec2<i32>(i32(slot%14u)*64,(4+i32(slot/14u))*64)+d;let tex=textureLoad(layerMasks,at,0);
   if(tex.a>=0.5){col=tex.rgb;}
  }
 }}return col;
}
@fragment fn layer_fog_fragment(@builtin(position) pixel:vec4<f32>)->@location(0) vec4<f32> {
 var color=textureLoad(layerWorld,vec2<i32>(pixel.xy),0).rgb;
 let projected=(pixel.xy-layerU.viewport.xy*0.5)/layerU.camera.z+layerU.camera.xy;
 if(layerU.flags.z!=0u){
  let origin=floor(layerU.camera.xy-layerU.viewport.xy/(2*layerU.camera.z));
  let checker=(i32(floor(pixel.x)+floor(pixel.y)+origin.x+origin.y)&1);
  color=layerApplyFog(color,projected,checker);
 }
 if(layerU.rect.z>layerU.rect.x && layerU.rect.w>layerU.rect.y && (projected.x<layerU.rect.x || projected.y<layerU.rect.y || projected.x>=layerU.rect.z || projected.y>=layerU.rect.w)){color=vec3<f32>(0);}
 if(layerU.viewport.z>0){color=min(color*layerU.viewport.z,vec3<f32>(1));}
 return vec4<f32>(color,1);
}
