struct LayerUniform {
 camera:vec4<f32>, viewport:vec4<f32>, rect:vec4<f32>, fogRect:vec4<f32>,
 tiles:vec4<u32>, flags:vec4<u32>, ground:vec4<f32>,
}
@group(0) @binding(0) var<uniform> layerU:LayerUniform;
@group(0) @binding(1) var layerTiles:texture_2d<f32>;
@group(0) @binding(2) var layerLookup:texture_2d<u32>;
@group(0) @binding(3) var layerDetail:texture_2d<f32>;
@group(0) @binding(4) var layerFallback:texture_2d<f32>;
struct LayerTerrainOut {
 @builtin(position) position:vec4<f32>, @location(0) world:vec2<f32>, @location(1) uv:vec2<f32>,
}
fn layerCorner(v:u32)->vec2<f32> {
 let corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
 return corners[v];
}
@vertex fn layer_terrain_vertex(@builtin(vertex_index) v:u32)->LayerTerrainOut {
 var out:LayerTerrainOut;out.uv=layerCorner(v);out.world=mix(layerU.rect.xy,layerU.rect.zw,out.uv);
 let pixel=(out.world-layerU.camera.xy)*layerU.camera.z+layerU.viewport.xy*0.5;
 out.position=vec4<f32>(pixel.x/layerU.viewport.x*2-1,1-pixel.y/layerU.viewport.y*2,0.9999,1);return out;
}
fn layerTileTexel(tex:texture_2d<f32>,origin:vec2<u32>,p:vec2<i32>,side:u32)->vec4<f32> {
 return textureLoad(tex,vec2<i32>(origin)+clamp(p,vec2<i32>(0),vec2<i32>(i32(side)-1)),0);
}
fn layerTileCell(tex:texture_2d<f32>,origin:vec2<u32>,local:vec2<f32>,side:u32,filtered:bool)->vec4<f32> {
 if(!filtered){return layerTileTexel(tex,origin,vec2<i32>(floor(local)),side);}
 let at=floor(local-0.5);let f=fract(local-0.5);let p=vec2<i32>(at);
 // Clamp all four colour reads to this authored tile (GPU design §16.3).
 return mix(mix(layerTileTexel(tex,origin,p,side),layerTileTexel(tex,origin,p+vec2<i32>(1,0),side),f.x),mix(layerTileTexel(tex,origin,p+vec2<i32>(0,1),side),layerTileTexel(tex,origin,p+vec2<i32>(1,1),side),f.x),f.y);
}
@fragment fn layer_terrain_fragment(in:LayerTerrainOut)->@location(0) vec4<f32> {
 if(layerU.flags.y==0u){
  let size=textureDimensions(layerFallback);let at=clamp(vec2<i32>(floor(in.uv*vec2<f32>(size))),vec2<i32>(0),vec2<i32>(size)-1);
  return textureLoad(layerFallback,at,0);
 }
 let pixel=clamp(in.world,vec2<f32>(0),vec2<f32>(layerU.tiles.xy*32u));
 let tile=min(vec2<u32>(floor(pixel/32)),layerU.tiles.xy-1u);let local=pixel-vec2<f32>(tile*32u);
 let cell=textureLoad(layerLookup,vec2<i32>(tile),0).r;let filtered=(layerU.flags.x&1u)!=0u;
 if((layerU.flags.x&2u)!=0u && layerU.tiles.w>0u){
  let origin=vec2<u32>(cell%layerU.tiles.w,cell/layerU.tiles.w)*64u;
  return layerTileCell(layerDetail,origin,local*2,64u,filtered);
 }
 let origin=vec2<u32>(cell%layerU.tiles.z,cell/layerU.tiles.z)*32u;
 return layerTileCell(layerTiles,origin,local,32u,filtered);
}
