// Exact, conservative source rejection for DESIGN_GPU_RENDERER §26.6.
// Physical 64-pixel tiles describe the ACTUAL rendered RGBA8 planes, including
// blended overlaps. Bit 0 is any color content; bit 1 is positive softness.
// RGB must participate: an HDR source faded into RGBA8 can retain RGB while
// alpha quantizes to zero. No coverage threshold or source admission is added.
constant uint nm_water_tile_side=64;
constant uint nm_water_tile_content=1;
constant uint nm_water_tile_soft=2;
kernel void nm_water_tiles_classify(texture2d<float,access::read> color [[texture(0)]],texture2d<float,access::read> soft [[texture(1)]],device uint* flags [[buffer(0)]],uint2 tile [[threadgroup_position_in_grid]],uint lane [[thread_index_in_threadgroup]]){
 threadgroup uint partial[128];uint bits=0;uint2 size=uint2(color.get_width(),color.get_height());
 // Each of 128 lanes scans 32 pixels, then a shared OR reduction writes once.
 // Every group overwrites its own tile, including partial viewport edge tiles.
 for(uint i=lane;i<4096;i+=128){uint2 p=tile*nm_water_tile_side+uint2(i%nm_water_tile_side,i/nm_water_tile_side);if(any(p>=size))continue;
  if(any(color.read(p)>float4(0)))bits|=nm_water_tile_content;
  if(soft.read(p).r>0)bits|=nm_water_tile_soft;
  if(bits==3)break;
 }
 partial[lane]=bits;threadgroup_barrier(mem_flags::mem_threadgroup);
 for(uint stride=64;stride>0;stride/=2){if(lane<stride)partial[lane]|=partial[lane+stride];threadgroup_barrier(mem_flags::mem_threadgroup);}
 if(lane==0)flags[tile.y*((size.x+63)/64)+tile.x]=partial[0];
}
// Call AFTER computing q with the existing ripple equation. Thus no guessed
// wind/ripple maximum is required. The union covers the three low bilinear
// samples (q and q +/- control.y), all horizontal high taps (+/-4*control.z),
// and all vertical high taps (+/-2*control.z). One extra physical pixel covers
// bilinear neighbors and floor-to-texel addressing; whole tiles enlarge it
// further. Return both bits on nonfinite input so rejection stays conservative.
uint nm_water_tiles_resolve_flags(device const uint* flags,texture2d<float> source,float2 q,float4 control){
 float2 radius=float2(max(abs(control.y)+1.0,4.0*abs(control.z)+1.0),max(1.0,2.0*abs(control.z)+1.0));
 if(!all(isfinite(q))||!all(isfinite(radius)))return 3;
 float2 size=float2(source.get_width(),source.get_height()),lo=q-radius,hi=q+radius;
 if(any(hi<0)||any(lo>=size))return 0;
 uint2 first=uint2(floor(clamp(lo,float2(0),size-1)))/nm_water_tile_side;
 uint2 last=uint2(floor(clamp(hi,float2(0),size-1)))/nm_water_tile_side;
 uint columns=(source.get_width()+63)/64,bits=0;
 for(uint y=first.y;y<=last.y;y++)for(uint x=first.x;x<=last.x;x++){bits|=flags[y*columns+x];if(bits==3)return bits;}
 return bits;
}
// The no-soft branch keeps the production manual bilinear operation order.
// A hardware linear sampler is not guaranteed to use the same fractional
// precision. Supply the EXISTING low weights (.5/.25/.25) at the root site.
float4 nm_water_tiles_color_texel(texture2d<float> source,float2 p,float low){
 int2 at=int2(floor(p));if(any(at<0)||at.x>=int(source.get_width())||at.y>=int(source.get_height()))return float4(0)*low;return source.read(uint2(at))*low;
}
float4 nm_water_tiles_color_sample(texture2d<float> source,float2 p,float low){
 float2 a=floor(p-.5)+.5,f=fract(p-.5);
 return mix(mix(nm_water_tiles_color_texel(source,a,low),nm_water_tiles_color_texel(source,a+float2(1,0),low),f.x),mix(nm_water_tiles_color_texel(source,a+float2(0,1),low),nm_water_tiles_color_texel(source,a+1,low),f.x),f.y);
}
