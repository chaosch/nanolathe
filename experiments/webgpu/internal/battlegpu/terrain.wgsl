struct LayerUniform {
 camera:vec4<f32>, viewport:vec4<f32>, rect:vec4<f32>, fogRect:vec4<f32>,
 tiles:vec4<u32>, flags:vec4<u32>, ground:vec4<f32>, glow:vec4<f32>, lightRect:vec4<f32>, lightValues:vec4<f32>,
 waterPhase:vec4<f32>, waterMask:vec4<f32>, waterControls:vec4<f32>, waterTerrain:vec4<f32>, waterSample:vec4<f32>,
}
@group(0) @binding(0) var<uniform> layerU:LayerUniform;
@group(0) @binding(1) var layerTiles:texture_2d<f32>;
@group(0) @binding(2) var layerLookup:texture_2d<u32>;
@group(0) @binding(3) var layerDetail:texture_2d<f32>;
@group(0) @binding(4) var layerFallback:texture_2d<f32>;
@group(0) @binding(5) var layerGroundField:texture_2d<f32>;
@group(0) @binding(6) var layerWaterMask:texture_2d<f32>;
@group(0) @binding(7) var layerSeabed:texture_2d<f32>;
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
fn layerRawTerrainAt(world:vec2<f32>,filtered:bool)->vec4<f32> {
 if(layerU.flags.y==0u){
  let size=textureDimensions(layerFallback);let uv=(world-layerU.rect.xy)/(layerU.rect.zw-layerU.rect.xy);
  let lo=vec2<i32>(0);let hi=vec2<i32>(size)-1;
  if(!filtered){return textureLoad(layerFallback,clamp(vec2<i32>(floor(uv*vec2<f32>(size))),lo,hi),0);}
  let q=uv*vec2<f32>(size)-0.5;let at=vec2<i32>(floor(q));let f=fract(q);
  return mix(mix(textureLoad(layerFallback,clamp(at,lo,hi),0),textureLoad(layerFallback,clamp(at+vec2<i32>(1,0),lo,hi),0),f.x),mix(textureLoad(layerFallback,clamp(at+vec2<i32>(0,1),lo,hi),0),textureLoad(layerFallback,clamp(at+vec2<i32>(1,1),lo,hi),0),f.x),f.y);
 }
 let pixel=clamp(world,vec2<f32>(0),vec2<f32>(layerU.tiles.xy*32u));
 let tile=min(vec2<u32>(floor(pixel/32)),layerU.tiles.xy-1u);let local=pixel-vec2<f32>(tile*32u);
 let cell=textureLoad(layerLookup,vec2<i32>(tile),0).r;
 if((layerU.flags.x&2u)!=0u && layerU.tiles.w>0u){
  let origin=vec2<u32>(cell%layerU.tiles.w,cell/layerU.tiles.w)*64u;
  return layerTileCell(layerDetail,origin,local*2,64u,filtered);
 }
 let origin=vec2<u32>(cell%layerU.tiles.z,cell/layerU.tiles.z)*32u;
 return layerTileCell(layerTiles,origin,local,32u,filtered);
}
fn layerRawTerrainColor(in:LayerTerrainOut)->vec4<f32> {
 if(layerU.flags.y==0u){
  // Keep the original inactive fallback's interpolated UV arithmetic.
  let size=textureDimensions(layerFallback);let at=clamp(vec2<i32>(floor(in.uv*vec2<f32>(size))),vec2<i32>(0),vec2<i32>(size)-1);
  return textureLoad(layerFallback,at,0);
 }
 return layerRawTerrainAt(in.world,(layerU.flags.x&1u)!=0u);
}

// The completed bed occupies the physical viewport, not the map texture's
// extent. Ripple displacement remains in painted-map world coordinates
// (GPU design §26.3; native waterObjectsSurface viewport override).
fn layerSeabedAt(world:vec2<f32>,filtered:bool)->vec4<f32> {
 let pixel=(world-layerU.camera.xy)*layerU.camera.z+layerU.viewport.xy*0.5;
 let lo=vec2<i32>(0);let hi=vec2<i32>(textureDimensions(layerSeabed))-1;
 if(!filtered){return textureLoad(layerSeabed,clamp(vec2<i32>(floor(pixel)),lo,hi),0);}
 let q=pixel-0.5;let at=vec2<i32>(floor(q));let f=fract(q);
 return mix(mix(textureLoad(layerSeabed,clamp(at,lo,hi),0),textureLoad(layerSeabed,clamp(at+vec2<i32>(1,0),lo,hi),0),f.x),mix(textureLoad(layerSeabed,clamp(at+vec2<i32>(0,1),lo,hi),0),textureLoad(layerSeabed,clamp(at+vec2<i32>(1,1),lo,hi),0),f.x),f.y);
}
fn layerTerrainAt(world:vec2<f32>,filtered:bool)->vec4<f32> {
 if(layerU.flags.w!=0u){return layerSeabedAt(world,filtered);}
 return layerRawTerrainAt(world,filtered);
}
fn layerTerrainColor(in:LayerTerrainOut)->vec4<f32> {
 if(layerU.flags.w!=0u){return layerSeabedAt(in.world,true);}
 return layerRawTerrainColor(in);
}
@fragment fn layer_seabed_fragment(in:LayerTerrainOut)->@location(0) vec4<f32> {
 // Raw terrain does not sample the bed or apply water and ground light.
 return layerRawTerrainColor(in);
}

fn layerLinear(c:vec3<f32>)->vec3<f32> {
 return select(c/12.92,pow((c+0.055)/1.055,vec3<f32>(2.4)),c>=vec3<f32>(0.04045));
}
fn layerDisplay(c:vec3<f32>)->vec3<f32> {
 return select(c*12.92,1.055*pow(c,vec3<f32>(1.0/2.4))-0.055,c>=vec3<f32>(0.0031308));
}
fn layerGroundPixel(pixel:vec2<f32>)->vec3<f32> {
 let q=pixel/2-0.5;let at=vec2<i32>(floor(q));let f=fract(q);
 // Spare geometric capacity is never part of the field's visible extent.
 let lo=vec2<i32>(0);let hi=vec2<i32>(layerU.lightValues.xy)-1;
 return mix(mix(textureLoad(layerGroundField,clamp(at,lo,hi),0).rgb,textureLoad(layerGroundField,clamp(at+vec2<i32>(1,0),lo,hi),0).rgb,f.x),mix(textureLoad(layerGroundField,clamp(at+vec2<i32>(0,1),lo,hi),0).rgb,textureLoad(layerGroundField,clamp(at+vec2<i32>(1,1),lo,hi),0).rgb,f.x),f.y);
}
@fragment fn layer_terrain_fragment(in:LayerTerrainOut)->@location(0) vec4<f32> {
 let original=layerWaterColor(in.world,layerTerrainColor(in));let pixel=in.position.xy;
 if(layerU.lightValues.z<0.5 || pixel.x<layerU.lightRect.x || pixel.y<layerU.lightRect.y || pixel.x>=layerU.lightRect.x+layerU.lightRect.z || pixel.y>=layerU.lightRect.y+layerU.lightRect.w){return original;}
 let field=layerGroundPixel(pixel);if(max(field.x,max(field.y,field.z))<=0){return original;}
 // GPU design §31.8: bounded optical-density field resolves before objects.
 let base=layerLinear(original.rgb/max(original.a,0.000001));
 var reflected=mix(base,vec3<f32>(dot(base,vec3<f32>(0.2126,0.7152,0.0722))),0.25);
 reflected*=2-reflected;
 return vec4<f32>(layerDisplay(base+(1-base)*reflected*field)*original.a,original.a);
}
