#include <metal_stdlib>
using namespace metal;
// Replay uses deliberately synthetic lights and shock rings. Live uses only
// submitted sources; neither path claims retail composition or light arithmetic.
struct Vertex { packed_float3 p; packed_float3 n; float2 uv; packed_float3 color; uint piece; };
struct Uniforms { float4 camera; float4 rect; float4 viewport; float4 timing; float4 mode; };
struct Light { float4 positionRadius; float4 colorStrength; };
struct Sprite { float4 previous,current,rect,uv,color,params; };
struct Out { float4 position [[position]]; float2 uv; float3 color; float3 normal; float3 world; };
struct MRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; };
float4 project(float3 p, constant Uniforms& u) {
 // Live world art uses half-height shear; replay retains its measured basis.
 float heightScale=u.mode.x>0.5?0.5:1.0;float depthHeight=u.mode.x>0.5?2.0:1.0;
 float2 screen=float2(p.x-u.camera.x,p.z-u.camera.y-p.y*heightScale)*u.camera.z+u.viewport.xy*0.5;
 return float4(screen.x/u.viewport.x*2-1,1-screen.y/u.viewport.y*2,clamp(0.5-((p.z-u.camera.y)+p.y*depthHeight)*0.00001,0.001,0.999),1);
}
vertex Out model_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]]) {
 Vertex v=vertices[vid]; uint at=offsets[iid]+v.piece;
 float4x4 a=poses[at], b=poses[at+uint(u.timing.z)];
 // Hidden pieces carry a zero matrix. Suppress either-hidden endpoint instead
 // of blending it toward world origin; appearance snaps one sample late.
 if(a[3].w<0.5||b[3].w<0.5){Out hidden;hidden.position=float4(2,2,2,1);hidden.uv=0;hidden.color=0;hidden.normal=float3(0,1,0);hidden.world=0;return hidden;}
 // Linear matrix blending is deliberately approximate for large rotations.
 float4x4 m=float4x4(mix(a[0],b[0],u.timing.x),mix(a[1],b[1],u.timing.x),mix(a[2],b[2],u.timing.x),mix(a[3],b[3],u.timing.x));
 float3 w=(m*float4(float3(v.p),1)).xyz;
 Out o; o.position=project(w,u);o.uv=v.uv;o.color=float3(v.color);o.normal=normalize((m*float4(float3(v.n),0)).xyz);o.world=w;return o;
}
fragment MRT model_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float4 tex=atlas.sample(s,in.uv);if(tex.a<0.02)discard_fragment();
 float3 light=float3(0.32)+float3(0.68)*max(dot(in.normal,normalize(float3(-0.5,1,-0.3))),0.0);
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
 MRT out;out.color=float4(tex.rgb*in.color*light,tex.a);out.glow=u.mode.x>0.5?float4(0):float4(max(out.color.rgb-0.7,0.0)*u.timing.w,1);return out;
}
vertex Out terrain_vertex(uint v [[vertex_id]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 Out o;o.uv=c[v];o.world=float3(mix(u.rect.x,u.rect.z,o.uv.x),0,mix(u.rect.y,u.rect.w,o.uv.y));o.position=project(o.world,u);o.position.z=0.9999;o.color=1;o.normal=float3(0,1,0);return o;
}
fragment MRT terrain_fragment(Out in [[stage_in]],texture2d<float> terrain [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 MRT out;out.color=terrain.sample(s,in.uv);out.glow=0;
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<8;i++){
  float a=float(i)*0.785398+u.timing.y*0.18;float2 p=float2(u.camera.x+cos(a)*260,u.camera.y+sin(a)*230);
  float fall=pow(max(1-length(p-in.world.xz)/120,0.0),2.0);
  float3 tint=float3(0.6+0.4*sin(a),0.3+0.3*cos(a),0.5+0.5*cos(a+2));out.color.rgb+=tint*fall*0.4;out.glow.rgb+=tint*fall*0.6;
 }
 if(u.timing.w>0.5&&u.mode.x>0.5)for(uint i=0;i<uint(u.mode.y);i++){
  Light l=lights[i];float r=length(l.positionRadius.xyz-in.world);float fall=pow(max(1-r/max(l.positionRadius.w,0.001),0.0),2.0);out.color.rgb+=l.colorStrength.rgb*l.colorStrength.w*fall*0.4;
 }return out;
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
 float3 premult=tex.rgb*in.color.rgb*alpha;MRT out;out.color=float4(premult,in.emissionAdditive.y>0.5?0:alpha);out.glow=float4(premult*max(in.emissionAdditive.x,0.0)*u.timing.w,0);return out;
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
