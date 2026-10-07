#include <metal_stdlib>
using namespace metal;

// Port of the production Enhanced surface field and pass, not retail water
// behavior (DESIGN_GPU_RENDERER §26.3, §30, §32.3). The host supplies the
// production observer's phase/current and BuildWaterMask's copied pixels.
struct WaterUniform {
 float4 phase, mask, controls, terrainRect, sampleRect;
};
constant float nmPatternSize=0.5;
constant float nmSurfaceOpacity=0.5;
constant float nmCurrentScale=3.0;
constant float nmRippleDeformation=5.0;
constant float nmSurfaceEnergy=0.5;
constant float nmShoreFoamOpacity=0.6;
constant float nmEdgeFadePixels=11.2;

float nmWaterNoise(float2 p) {
 float2 f=fract(p), a=floor(p);
 a=a-floor(a/289.0)*289.0;
 float2 b=a+float2(1.0);
 b=b-floor(b/289.0)*289.0;
 f=f*f*(3.0-2.0*f);
 float4 h=float4(dot(a,float2(43.17,97.53)),
  dot(float2(b.x,a.y),float2(43.17,97.53)),
  dot(float2(a.x,b.y),float2(43.17,97.53)),dot(b,float2(43.17,97.53)));
 h=fract(sin(h)*17341.23);
 return mix(mix(h.x,h.y,f.x),mix(h.z,h.w,f.x),f.y);
}

float3 nmWaterField(float2 pattern,float2 drift,float t) {
 float gust=smoothstep(0.30,0.80,nmWaterNoise((pattern-drift*22.0)*0.0055+float2(3.0,7.0)));
 float2 p=(pattern-drift*6.0)*0.07;
 float a=nmWaterNoise(pattern*0.018+float2(7.0,13.0))*6.283185;
 float b=nmWaterNoise(pattern*0.023+float2(31.0,3.0))*6.283185;
 float2 warp=float2(0.5)+float2(sin(t*0.65+a),sin(t*0.83+b))*0.5*nmRippleDeformation;
 p+=(warp-float2(0.5))*1.6;
 float broad=nmWaterNoise(p);
 float2 q=(pattern-drift*11.0)*0.166;
 q=float2(q.x*0.7986-q.y*0.6018,q.x*0.6018+q.y*0.7986)+(warp-float2(0.5))*0.9;
 return float3(broad,nmWaterNoise(q),gust);
}

float2 nmWaterOffset(float3 field) {
 return (field.xy-float2(0.5))*(3.2+nmSurfaceEnergy*2.4)*(0.7+0.5*field.z);
}

float3 nmWaterShade(float3 c,float3 field,float deep,float colour) {
 float ripple=field.x*0.65+field.y*0.35-0.5;
 float shade=1.0+(ripple*(0.112+nmSurfaceEnergy*0.08)*(0.7+0.5*field.z)*deep-0.05*nmSurfaceEnergy*field.z*deep);
 float crest=smoothstep(0.10,0.32,ripple);
 return mix(c*shade,float3(0.40,0.67,0.78),clamp(colour*crest*(0.04+nmSurfaceEnergy*0.04)*deep,0.0,1.0));
}

float4 nmWaterMaskTexel(texture2d<float> mask,int2 at) {
 // The production source fetch returns zero outside its image; clamping an
 // edge texel would grow the medium and damp band beyond the map.
 if(any(at<0)||at.x>=int(mask.get_width())||at.y>=int(mask.get_height()))return float4(0);
 return mask.read(uint2(at));
}

float4 nmWaterMask(texture2d<float> mask,float2 p) {
 float2 q=p-float2(0.5), f=fract(q);
 int2 a=int2(floor(q));
 return mix(mix(nmWaterMaskTexel(mask,a),nmWaterMaskTexel(mask,a+int2(1,0)),f.x),
  mix(nmWaterMaskTexel(mask,a+int2(0,1)),nmWaterMaskTexel(mask,a+int2(1,1)),f.x),f.y);
}

// world is the painted-map point (world X, world Z minus half height), not
// a terrain-height point. UV addresses the full painted terrain photograph.
// sampleRect is the viewport's physical pixel-centre bounds in those same
// coordinates, so refraction never samples the production frame's padding.
float4 nmWaterColor(texture2d<float> terrain,texture2d<float> mask,float2 uv,float2 world,constant WaterUniform& u) {
 constexpr sampler linear(coord::normalized,address::clamp_to_edge,filter::linear);
 float4 base=terrain.sample(linear,uv,level(0));
 if(u.mask.w<0.5)return base;
 float4 wet=nmWaterMask(mask,world/u.mask.x);
 float coverage=smoothstep(0.8,1.0,wet.x), t=u.phase.x;
 float2 drift=u.phase.yz*nmCurrentScale, pattern=world/nmPatternSize;
 float shoreDistance=wet.y;
 float4 original=base;
 float shading=min(u.controls.z,1.0);
 float waterColour=shading*clamp(2.0-u.controls.z,0.0,1.0);
 float dry=smoothstep(0.05,0.5,wet.z)*(1.0-coverage);
 float damp=wet.w*dry*waterColour;
 if(damp>0.0) {
  float lapDry=pow(max(0.0,sin(t*1.6+nmWaterNoise(pattern*0.025)*3.0)),2.0);
  base=float4(base.rgb*(1.0-0.12*damp*(0.5+0.5*lapDry)),base.a);
 }
 coverage*=smoothstep(0.0,nmEdgeFadePixels/32.0,wet.y);
 if(coverage<=0.0)return float4(clamp(mix(original.rgb,base.rgb,nmSurfaceOpacity),float3(0),float3(base.a)),base.a);
 float3 field=nmWaterField(pattern,drift*u.controls.x,t*u.controls.x);
 float deep=smoothstep(0.05,0.55,wet.y);
 // Production first scales the offset to physical pixels, then maps the
 // sample back to painted-map coordinates. Those scale factors cancel here.
 float2 offset=nmWaterOffset(field)*deep*coverage*u.controls.x;
 float2 sample=clamp(world+offset,u.sampleRect.xy,u.sampleRect.zw);
 float2 sampleUV=(sample-u.terrainRect.xy)/(u.terrainRect.zw-u.terrainRect.xy);
 float4 warped=terrain.sample(linear,sampleUV,level(0));
 float3 result=warped.rgb;
 if(shading>0.0)result=nmWaterShade(warped.rgb,field,deep,waterColour);
 float shore=(1.0-smoothstep(0.35,0.95,shoreDistance))*smoothstep(0.0,0.25,shoreDistance);
 float shorePatch=nmWaterNoise(world*0.025);
 float lap=pow(max(0.0,sin(shoreDistance*10.0+t*1.6+shorePatch*3.0)),2.0);
 float foam=shore*lap*(0.09+u.phase.w*0.105)*(0.50+0.50*shorePatch)*coverage*u.controls.y*(1.0-smoothstep(0.1,0.4,wet.z));
 result=mix(result,float3(0.62,0.80,0.84),clamp(waterColour*0.08*smoothstep(0.0,0.10,shoreDistance)*(1.0-smoothstep(0.10,0.40,shoreDistance)),0.0,1.0));
 float3 effect=mix(base.rgb,min(result,float3(base.a)),coverage);
 float3 surface=clamp(mix(original.rgb,effect,nmSurfaceOpacity),float3(0),float3(base.a));
 surface=mix(surface,float3(0.72,0.84,0.87)*base.a,clamp(foam*nmShoreFoamOpacity*coverage,0.0,1.0));
 return float4(surface,base.a);
}
