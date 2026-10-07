// Full native TNT raster without a map-sized photograph. Concatenate before
// terrain_fragment; all positions are painted-map world X/Z (not height).
// Layout is map tile width/height, native atlas columns, detail atlas columns.
// Controls selects enabled, production world-filter gate, optional detail.
struct nm_terrain_tiles_Parameters {uint4 layout;float4 controls;};
float4 nm_terrain_tiles_texel(texture2d<float> atlas,uint2 origin,int2 p,uint side){return atlas.read(origin+uint2(clamp(p,int2(0),int2(side-1))));}
float4 nm_terrain_tiles_cell(texture2d<float> atlas,uint2 origin,float2 local,uint side,bool filtered){
 if(!filtered)return nm_terrain_tiles_texel(atlas,origin,int2(floor(local)),side);
 float2 at=floor(local-.5),f=fract(local-.5);int2 p=int2(at);
 // Never interpolate into another packed tile, nor across a map tile seam.
 // Production pads each tile with its own edge before fractional filtering
 // (DESIGN_GPU_RENDERER §16.3); clamping these four reads is equivalent.
 return mix(mix(nm_terrain_tiles_texel(atlas,origin,p,side),nm_terrain_tiles_texel(atlas,origin,p+int2(1,0),side),f.x),mix(nm_terrain_tiles_texel(atlas,origin,p+int2(0,1),side),nm_terrain_tiles_texel(atlas,origin,p+int2(1,1),side),f.x),f.y);
}
float4 nm_terrain_tiles_color(texture2d<float> base,texture2d<uint> lookup,texture2d<float> detail,float2 world,constant nm_terrain_tiles_Parameters& p){
 if(p.controls.x<.5||any(p.layout.xy==uint2(0)))return float4(0);
 // The terrain quad owns raster coverage. Clamp the last texel at the full
 // authored map edge, as the legacy backdrop's edge-addressing sampler did.
 float2 pixel=clamp(world,float2(0),float2(p.layout.xy*32));uint2 tile=min(uint2(floor(pixel/32)),p.layout.xy-1);float2 local=pixel-float2(tile*32);
 uint cell=lookup.read(tile).r;bool filtered=p.controls.y>.5;
 if(p.controls.z>.5&&p.layout.w>0){uint2 origin=uint2(cell%p.layout.w,cell/p.layout.w)*64;return nm_terrain_tiles_cell(detail,origin,local*2,64,filtered);}
 uint2 origin=uint2(cell%p.layout.z,cell/p.layout.z)*32;return nm_terrain_tiles_cell(base,origin,local,32,filtered);
}
