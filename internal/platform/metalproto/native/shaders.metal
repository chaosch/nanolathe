#include <metal_stdlib>
using namespace metal;
// Replay uses deliberately synthetic lights and shock rings. Live uses only
// submitted sources; neither path claims retail composition or light arithmetic.
// Packed vector lanes keep the Go/Metal ABI exactly 64 bytes.
struct Vertex { packed_float3 p; packed_float3 n; float2 uv; packed_float3 color; uint piece; uint material; packed_float3 shadeNormal; };
struct Uniforms { float4 camera; float4 rect; float4 viewport; float4 timing; float4 mode; float4 heightRect; float4 fogRect; float4 visual; };
struct Light { float4 positionRadius; float4 colorStrength; };
struct Material { uint firstFrame,frameCount,kind,reserved; };
struct ModelVisual { float4 state,bounds,outline,emission; };
struct Distortion { float4 previous,current,shape,params; };
struct Sprite { float4 previous,current,rect,uv,color,params; };
struct Out { float4 position [[position]]; float2 uv; float3 color; float3 normal; float3 world; float heightKey; float keyOrigin [[flat]]; float shadeRow; float4 state [[flat]]; float4 bounds [[flat]]; float4 outline [[flat]]; float4 emission [[flat]]; };
float terrainHeight(float2 xz, texture2d<float> heights, constant Uniforms& u) {
 if(u.visual.y<0.5)return 0;
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 uv=(xz-u.heightRect.xy)/max(u.heightRect.zw-u.heightRect.xy,float2(1));
 return heights.sample(s,uv,level(0)).r;
}
struct MRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; };
struct ModelMRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; float depth [[depth(any)]]; };
float4 project(float3 p, constant Uniforms& u) {
 // Live world art uses half-height shear; replay retains its measured basis.
 float heightScale=u.mode.x>0.5?0.5:1.0;float depthHeight=u.mode.x>0.5?2.0:1.0;
 float2 screen=float2(p.x-u.camera.x,p.z-u.camera.y-p.y*heightScale)*u.camera.z+u.viewport.xy*0.5;
 return float4(screen.x/u.viewport.x*2-1,1-screen.y/u.viewport.y*2,clamp(0.5-((p.z-u.camera.y)+p.y*depthHeight)*0.00001,0.001,0.999),1);
}
vertex Out model_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]],device const ModelVisual* visuals [[buffer(5)]],device const Material* materials [[buffer(6)]],device const float4* frames [[buffer(7)]],device const uint* selectors [[buffer(8)]]) {
 if(u.mode.z>0.5)iid=selectors[iid];
 Vertex v=vertices[vid]; uint at=offsets[iid]+v.piece;
 float4x4 a=poses[at], b=poses[at+uint(u.timing.z)];
 // Hidden pieces carry a zero matrix. Suppress either-hidden endpoint instead
 // of blending it toward world origin; appearance snaps one sample late.
 if(a[3].w<0.5||b[3].w<0.5){Out hidden={};hidden.position=float4(2,2,2,1);hidden.uv=0;hidden.color=0;hidden.normal=float3(0,1,0);hidden.world=0;return hidden;}
 // Linear matrix blending is deliberately approximate for large rotations.
 float4x4 m=float4x4(mix(a[0],b[0],u.timing.x),mix(a[1],b[1],u.timing.x),mix(a[2],b[2],u.timing.x),mix(a[3],b[3],u.timing.x));
 float3 w=(m*float4(float3(v.p),1)).xyz;
 ModelVisual visual=visuals[iid];
 Out o={}; o.position=project(w,u);o.uv=v.uv;
 if(v.material!=0){Material material=materials[v.material];uint frame=material.kind==1?min(uint(max(visual.state.x,0.0)),max(material.frameCount,1u)-1):0;float4 rect=frames[material.firstFrame+frame];o.uv=mix(rect.xy,rect.zw,v.uv);}
 if(u.visual.x>0.5){o.state=visual.state;o.bounds=visual.bounds;o.outline=visual.outline;o.emission=visual.emission;}
 o.heightKey=floor(w.y-visual.bounds.w);o.keyOrigin=visual.bounds.w;
 // Production SHD uses the model-space light before final world-Z reflection.
 float3 shadeNormal=(m*float4(float3(v.shadeNormal),0)).xyz*float3(1,1,-1);
 uint shadeRow=uint(int(dot(shadeNormal,float3(-0.8,1,0.25))*5.0))&31;
 if(b[0].w>1.5)shadeRow=15;
 o.shadeRow=b[0].w>0.5&&b[0].w<1.5?-1.0:float(shadeRow);o.color=float3(v.color);o.normal=normalize((m*float4(float3(v.n),0)).xyz);o.world=w;return o;
}
fragment ModelMRT model_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float4 tex=atlas.sample(s,in.uv);if(tex.a<0.02)discard_fragment();
 if(in.state.z>0 && in.world.y>mix(in.bounds.y,in.bounds.x,clamp(in.state.z,0.0,1.0)))discard_fragment();
 float opacity=all(in.state==float4(0))?1:clamp(in.state.y,0.0,1.0);
 // Interpolate SHD rows before truncation and the true-colour scale clamp.
 float3 light=float3(in.shadeRow<0?1.0:min(0.06875*floor(in.shadeRow),2.0));
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<8;i++){
  float a=float(i)*0.785398+u.timing.y*0.18;
  float3 p=float3(u.camera.x+cos(a)*260,60+sin(a*2)*30,u.camera.y+sin(a)*230);
  float3 d=p-in.world;float r=length(d);float fall=pow(max(1-r/230,0.0),2.0);
  light+=float3(0.6+0.4*sin(a),0.3+0.3*cos(a),0.5+0.5*cos(a+2))*fall*max(dot(in.normal,normalize(d)),0.0)*1.8;
 }
 if(u.timing.w>0.5&&u.mode.x>0.5)for(uint i=0;i<uint(u.mode.y);i++){
  Light l=lights[i];float3 d=l.positionRadius.xyz-in.world;float r=length(d);float fall=pow(max(1-r/max(l.positionRadius.w,0.001),0.0),2.0);
  light+=l.colorStrength.rgb*l.colorStrength.w*fall*max(dot(in.normal,normalize(d+0.0001)),0.0);
 }
 float3 color=tex.rgb*in.color*light;
 // Modern display approximation: screen-blended cooling tint plus HDR bloom.
 float3 emission=in.emission.rgb*max(in.emission.w,0.0);
 color=1-(1-color)*(1-clamp(emission,0.0,1.0));
 // Retain integer corner-height ties, then the production reverse piece
 // order decides ownership (opened Solar rim, DESIGN_GPU_RENDERER §22.1).
 // Inter-subject ordering and triangle lane interpolation remain approximate.
 ModelMRT out;float modelY=in.keyOrigin+floor(in.heightKey);
 float projectedY=(in.position.y-u.viewport.y*0.5)/u.camera.z;
 float depthHeight=u.mode.x>0.5?2.5:2.0;
 out.depth=clamp(0.5-(projectedY+modelY*depthHeight)*0.00001,0.001,0.999);
 float alpha=tex.a*opacity;out.color=float4(color*alpha,alpha);out.glow=u.mode.x>0.5?float4(emission*alpha*u.timing.w,0):float4(max(color-0.7,0.0)*u.timing.w,1);return out;
}
vertex Out terrain_vertex(uint v [[vertex_id]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 Out o={};o.uv=c[v];o.world=float3(mix(u.rect.x,u.rect.z,o.uv.x),0,mix(u.rect.y,u.rect.w,o.uv.y));o.position=project(o.world,u);o.position.z=0.9999;o.color=1;o.normal=float3(0,1,0);return o;
}
struct TerrainMRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; float depth [[depth(any)]]; };
fragment TerrainMRT terrain_fragment(Out in [[stage_in]],texture2d<float> terrain [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]],texture2d<float> heights [[texture(1)]],texture2d<float> shadows [[texture(2)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 // TNT is painted background, not a reconstructed depth surface. A depth
 // plane would cut flat GAF art at its anchor [DESIGN_GPU_RENDERER C-G5].
 TerrainMRT out;out.depth=0.9999;
 out.color=terrain.sample(s,in.uv);out.glow=0;
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<8;i++){
  float a=float(i)*0.785398+u.timing.y*0.18;float2 p=float2(u.camera.x+cos(a)*260,u.camera.y+sin(a)*230);
  float fall=pow(max(1-length(p-in.world.xz)/120,0.0),2.0);
  float3 tint=float3(0.6+0.4*sin(a),0.3+0.3*cos(a),0.5+0.5*cos(a+2));out.color.rgb+=tint*fall*0.4;out.glow.rgb+=tint*fall*0.6;
 }
 if(u.timing.w>0.5&&u.mode.x>0.5)for(uint i=0;i<uint(u.mode.y);i++){
  Light l=lights[i];float r=length(l.positionRadius.xyz-in.world);float fall=pow(max(1-r/max(l.positionRadius.w,0.001),0.0),2.0);out.color.rgb+=l.colorStrength.rgb*l.colorStrength.w*fall*0.4;
 }
 if(u.visual.x>0.5){float mask=shadows.sample(s,in.position.xy/u.viewport.xy).r;out.color.rgb*=1-mask;}
 return out;
}
struct SpriteOut { float4 position [[position]];float2 uv;float4 color;float2 emissionAdditive; };
vertex SpriteOut sprite_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const Sprite* sprites [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 Sprite s=sprites[iid];float3 world=mix(s.previous.xyz,s.current.xyz,u.timing.x);float2 corner=s.rect.xy+c[v]*s.rect.zw;
 float cosine=cos(s.params.z),sine=sin(s.params.z);corner=float2(corner.x*cosine-corner.y*sine,corner.x*sine+corner.y*cosine)*u.camera.z;
 SpriteOut o;o.position=project(world,u);o.position.xy+=float2(corner.x/u.viewport.x*2,-corner.y/u.viewport.y*2);o.position.z=clamp(o.position.z+s.params.w,0.0001,0.9998);o.uv=mix(s.uv.xy,s.uv.zw,c[v]);o.color=s.color;o.emissionAdditive=s.params.xy;return o;
}
fragment MRT sprite_fragment(SpriteOut in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);float4 tex=atlas.sample(s,in.uv);float alpha=tex.a*in.color.a;if(alpha<0.001)discard_fragment();
 float3 premult=tex.rgb*in.color.rgb*alpha;MRT out;out.color=float4(premult,in.emissionAdditive.y>0.5?0:alpha);out.glow=float4(premult*max(in.emissionAdditive.x,0.0)*smoothstep(0.45,1.0,max(tex.r,max(tex.g,tex.b)))*u.timing.w,0);return out;
}
struct Screen { float4 p [[position]];float2 uv; };
vertex Screen screen_vertex(uint v [[vertex_id]]) {
 float2 p=float2((v<<1)&2,v&2);Screen o;o.p=float4(p*2-1,0,1);o.uv=float2(p.x,1-p.y);return o;
}
kernel void blur(texture2d<float,access::sample> src [[texture(0)]],texture2d<float,access::write> dst [[texture(1)]],constant float2& direction [[buffer(0)]],uint2 p [[thread_position_in_grid]]) {
 if(any(p>=uint2(dst.get_width(),dst.get_height())))return;
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 uv=(float2(p)+0.5)/float2(dst.get_width(),dst.get_height());float2 d=direction/float2(dst.get_width(),dst.get_height());
 float4 c=src.sample(s,uv)*0.227027;c+=(src.sample(s,uv+d*1.384615)+src.sample(s,uv-d*1.384615))*0.316216;c+=(src.sample(s,uv+d*3.230769)+src.sample(s,uv-d*3.230769))*0.070270;dst.write(c,p);
}
fragment float4 composite_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> glow [[texture(1)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 uv=in.uv;
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<4;i++){
  float phase=fract(u.timing.y*0.45+float(i)*0.25);float2 center=float2(0.3+0.4*float(i&1),0.3+0.4*float((i>>1)&1));
  float2 d=(uv-center)*u.viewport.xy;float r=length(d);float radius=25+phase*260;float band=(r-radius)/14;
  float shift=sin(band*3.14159)*max(1-abs(band),0.0)*(1-phase)*4;uv+=normalize(d+0.001)*shift/u.viewport.xy;
 }
 float3 c=color.sample(s,uv).rgb;if(u.timing.w>0.5)c+=glow.sample(s,in.uv).rgb*0.65;
 return float4(c,1);
}

// Shadow placement is a prototype display departure: vertical terrain receiver,
// five-world-pixel X offset; no retail silhouette copy, ALP or body punch.
vertex Out shadow_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]],device const ModelVisual* visuals [[buffer(5)]],texture2d<float> heights [[texture(1)]],device const uint* selectors [[buffer(8)]]) {
 iid=selectors[iid];Out o={};ModelVisual visual=visuals[iid];Vertex v=vertices[vid];uint at=offsets[iid]+v.piece;float4x4 a=poses[at],b=poses[at+uint(u.timing.z)];
 if(visual.state.w<0.5||a[3].w<0.5||b[3].w<0.5){o.position=float4(2,2,2,1);return o;}
 float4x4 m=float4x4(mix(a[0],b[0],u.timing.x),mix(a[1],b[1],u.timing.x),mix(a[2],b[2],u.timing.x),mix(a[3],b[3],u.timing.x));
 float3 w=(m*float4(float3(v.p),1)).xyz;w.x+=5;w.y=u.visual.y>0.5?terrainHeight(w.xz,heights,u):visual.bounds.z;
 o.position=project(w,u);o.state=visual.state;return o;
}
fragment float4 shadow_fragment(Out in [[stage_in]]) { return float4(0.35); }
fragment MRT outline_fragment(Out in [[stage_in]]) {
 if(in.state.z<=0||in.outline.a<=0)discard_fragment();MRT o;o.color=float4(in.outline.rgb*in.outline.a,in.outline.a);o.glow=0;return o;
}
fragment float4 world_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> glow [[texture(1)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);float3 c=color.sample(s,in.uv).rgb;
 if(u.timing.w>0.5)c+=glow.sample(s,in.uv).rgb*0.65;return float4(c,1);
}
// Same cell op encoding and ordered 2x2 mask walk as the production fog pass
// (DESIGN_GPU_RENDERER C-G7). Authored masks use integer texel reads: no blur.
float3 fogGray(float3 c) {
 return float3(floor(dot(floor(c*255.0+0.5),float3(1))/3.0)/255.0);
}
float3 applyFog(float3 col,float2 projected,float checker,texture2d<float> grid,texture2d<float> atlas,constant Uniforms& u) {
 float2 p=floor(projected);int2 cell=int2(floor((p-u.fogRect.xy)/32.0));
 float3 dark=atlas.read(uint2(0,512)).rgb;
 for(int j=-1;j<=0;j++)for(int i=-1;i<=0;i++){
  int2 c=cell+int2(i,j);if(any(c<0)||c.x>=int(grid.get_width())||c.y>=int(grid.get_height()))continue;
  uint2 codes=uint2(round(grid.read(uint2(c)).rg*255.0));if(all(codes==uint2(0)))continue;
  int2 d=int2(p-u.fogRect.xy-float2(c)*32.0);bool inCell=all(d>=0)&&all(d<32),inTile=all(d>=0)&&all(d<64);
  uint code1=codes.x,code0=codes.y;
  if(code1==1&&inCell)col=fogGray(col);
  else if(code1==2&&inCell&&checker<0.5)col=dark;
  else if(code1>=3&&inTile){
   uint slot=code1-3;bool dither=slot>=56;if(dither)slot-=56;
   uint2 at=uint2((slot%14)*64,(slot/14)*64)+uint2(d);
   if(atlas.read(at).a>=0.5){if(!dither)col=fogGray(col);else if(checker<0.5)col=dark;}
  }
  if(code0==1&&inCell)col=dark;
  else if(code0>=2&&inTile){
   uint slot=code0-2;uint2 at=uint2((slot%14)*64,(4+slot/14)*64)+uint2(d);float4 tex=atlas.read(at);
   if(tex.a>=0.5)col=tex.rgb;
  }
 }
 return col;
}
fragment float4 fog_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> fog [[texture(1)]],texture2d<float> masks [[texture(2)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);float3 c=color.sample(s,in.uv).rgb;
 if(u.visual.z>0.5){
  float2 projected=(in.p.xy-u.viewport.xy*0.5)/u.camera.z+u.camera.xy;
  float2 cameraOrigin=floor(u.camera.xy-u.viewport.xy/(2*u.camera.z));
  float checker=float(int(floor(in.p.x)+floor(in.p.y)+cameraOrigin.x+cameraOrigin.y)&1);
  c=applyFog(c,projected,checker,fog,masks,u);
 }
 return float4(c,1);
}
struct DistortionOut { float4 position [[position]]; float2 local; float4 shape [[flat]]; float4 params [[flat]]; };
vertex DistortionOut distortion_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const Distortion* sources [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(-1,-1),float2(1,-1),float2(-1,1),float2(-1,1),float2(1,-1),float2(1,1)};
 Distortion d=sources[iid];float3 world=mix(d.previous.xyz,d.current.xyz,u.timing.x);DistortionOut o;o.shape=d.shape;o.params=d.params;o.local=c[v];
 float2 halfSize=d.shape.w<0.5?float2(d.shape.x+d.shape.y):d.shape.xy;
 o.position=project(world,u);float2 pixels=c[v]*halfSize*u.camera.z;
 if(d.shape.w>=0.5)pixels.y+=(d.params.z-d.shape.y)*u.camera.z;
 o.position.xy+=float2(pixels.x/u.viewport.x*2,-pixels.y/u.viewport.y*2);return o;
}
fragment float4 distortion_fragment(DistortionOut in [[stage_in]],texture2d<float> source [[texture(0)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);float2 shift=0;float clock=in.params.x+u.timing.x-1;
 if(in.shape.w<0.5){
  if(clock<0||clock>=15)discard_fragment();float radius=mix(12.0,in.shape.x,clock/15.0);float2 d=in.local*(in.shape.x+in.shape.y);float r=length(d);float band=(r-radius)/max(in.shape.y,0.001);if(abs(band)>=1)discard_fragment();
  float strength=min(clock,1.0)*pow(1-clock/15.0,2.0)*in.shape.z;
  if(r<0.001)discard_fragment();float envelope=1-band*band;shift=d/r*(band*envelope*envelope*3.5*strength);
 }else{
  // DESIGN_GPU_RENDERER §27: same authored drifting/widening plume profile.
  float rise=(1-in.local.y)*0.5;float t=clock*0.20943951+in.params.y;
  float center=sin(rise*5.0-t*0.5)*0.13*rise;
  float across=(in.local.x-center)/(0.58+0.42*rise);
  if(rise<=0||rise>=1||abs(across)>=1)discard_fragment();
  float edge=1-across*across;float sine=sin(rise*3.14159265);float envelope=edge*edge*sine*sine;
  float wave=sin(rise*18.0-t*2.0+across*2.5)+0.35*sin(rise*33.0-t*3.0-across*4.0);
  shift=float2(wave,0.24*sin(rise*22.0-t*2.0+across*3.0))*envelope*1.8*in.shape.z;
 }
 float2 uv=(in.position.xy+shift*u.camera.z)/u.viewport.xy;return float4(source.sample(s,clamp(uv,float2(0),float2(1))).rgb,1);
}

// Display-pixel HUD batch. It is composed after final world/fog, without depth.
struct OverlayQuad { float4 rect,uv,color,params; };
struct OverlayOut { float4 position [[position]]; float2 uv; float4 color; uint textureKind [[flat]]; };
vertex OverlayOut overlay_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const OverlayQuad* quads [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 corners[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 OverlayQuad q=quads[iid];float2 pixels=q.rect.xy+corners[v]*q.rect.zw;OverlayOut out;out.position=float4(pixels.x/u.viewport.x*2-1,1-pixels.y/u.viewport.y*2,0,1);out.uv=mix(q.uv.xy,q.uv.zw,corners[v]);out.color=q.color;out.textureKind=uint(q.params.x);return out;
}
fragment float4 overlay_fragment(OverlayOut in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> terrain [[texture(1)]],texture2d<float> fog [[texture(2)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);
 // Fog stores grayscale world modulation with opaque alpha. Blend black by
 // inverse coverage over minimap terrain to reproduce that RGB modulation.
 if(in.textureKind==2){float alpha=(1-fog.sample(s,in.uv).r)*in.color.a;return float4(0,0,0,alpha);}
 float4 tex=in.textureKind==1?terrain.sample(s,in.uv):atlas.sample(s,in.uv);float alpha=tex.a*in.color.a;return float4(tex.rgb*in.color.rgb*alpha,alpha);
}

fragment float4 overlay_multiply_fragment(OverlayOut in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> terrain [[texture(1)]],texture2d<float> fog [[texture(2)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);
 float3 color=in.textureKind==1?terrain.sample(s,in.uv).rgb:in.textureKind==2?fog.sample(s,in.uv).rgb:atlas.sample(s,in.uv).rgb;
 // The factor is unpremultiplied. Fixed-function blending multiplies the
 // existing destination RGB while preserving its alpha (§13.3).
 return float4(color*in.color.rgb,1);
}
