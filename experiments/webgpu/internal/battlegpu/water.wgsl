// Enhanced surface presentation from the MIT native water shader
// (GPU design §26.3, §30, §32.3). Inputs are the published observer and mask;
// reflection sources and marked underwater model commits are separate passes.
const layerWaterPatternSize:f32=0.5;
const layerWaterSurfaceOpacity:f32=0.5;
const layerWaterCurrentScale:f32=3.0;
const layerWaterRippleDeformation:f32=5.0;
const layerWaterSurfaceEnergy:f32=0.5;
const layerWaterShoreFoamOpacity:f32=0.6;
const layerWaterEdgeFadePixels:f32=11.2;

fn layerWaterNoise(p:vec2<f32>)->f32 {
 var f=fract(p);var a=floor(p);
 a=a-floor(a/289.0)*289.0;
 var b=a+vec2<f32>(1.0);b=b-floor(b/289.0)*289.0;
 f=f*f*(3.0-2.0*f);
 var h=vec4<f32>(dot(a,vec2<f32>(43.17,97.53)),dot(vec2<f32>(b.x,a.y),vec2<f32>(43.17,97.53)),dot(vec2<f32>(a.x,b.y),vec2<f32>(43.17,97.53)),dot(b,vec2<f32>(43.17,97.53)));
 h=fract(sin(h)*17341.23);
 return mix(mix(h.x,h.y,f.x),mix(h.z,h.w,f.x),f.y);
}
fn layerWaterField(pattern:vec2<f32>,drift:vec2<f32>,t:f32)->vec3<f32> {
 let gust=smoothstep(0.30,0.80,layerWaterNoise((pattern-drift*22.0)*0.0055+vec2<f32>(3.0,7.0)));
 var p=(pattern-drift*6.0)*0.07;
 let a=layerWaterNoise(pattern*0.018+vec2<f32>(7.0,13.0))*6.283185;
 let b=layerWaterNoise(pattern*0.023+vec2<f32>(31.0,3.0))*6.283185;
 let warp=vec2<f32>(0.5)+vec2<f32>(sin(t*0.65+a),sin(t*0.83+b))*0.5*layerWaterRippleDeformation;
 p+=(warp-vec2<f32>(0.5))*1.6;
 let broad=layerWaterNoise(p);
 var q=(pattern-drift*11.0)*0.166;
 q=vec2<f32>(q.x*0.7986-q.y*0.6018,q.x*0.6018+q.y*0.7986)+(warp-vec2<f32>(0.5))*0.9;
 return vec3<f32>(broad,layerWaterNoise(q),gust);
}
fn layerWaterOffset(field:vec3<f32>)->vec2<f32> {
 return (field.xy-vec2<f32>(0.5))*(3.2+layerWaterSurfaceEnergy*2.4)*(0.7+0.5*field.z);
}
fn layerWaterShade(c:vec3<f32>,field:vec3<f32>,deep:f32,colour:f32)->vec3<f32> {
 let ripple=field.x*0.65+field.y*0.35-0.5;
 let shade=1.0+(ripple*(0.112+layerWaterSurfaceEnergy*0.08)*(0.7+0.5*field.z)*deep-0.05*layerWaterSurfaceEnergy*field.z*deep);
 let crest=smoothstep(0.10,0.32,ripple);
 return mix(c*shade,vec3<f32>(0.40,0.67,0.78),clamp(colour*crest*(0.04+layerWaterSurfaceEnergy*0.04)*deep,0.0,1.0));
}
fn layerWaterMaskTexel(at:vec2<i32>)->vec4<f32> {
 // Outside-mask taps are zero, rather than extending the edge medium.
 let size=vec2<i32>(textureDimensions(layerWaterMask));
 if(at.x<0 || at.y<0 || at.x>=size.x || at.y>=size.y){return vec4<f32>(0);}
 return textureLoad(layerWaterMask,at,0);
}
fn layerWaterMaskAt(p:vec2<f32>)->vec4<f32> {
 let q=p-vec2<f32>(0.5);let f=fract(q);let at=vec2<i32>(floor(q));
 return mix(mix(layerWaterMaskTexel(at),layerWaterMaskTexel(at+vec2<i32>(1,0)),f.x),mix(layerWaterMaskTexel(at+vec2<i32>(0,1)),layerWaterMaskTexel(at+vec2<i32>(1,1)),f.x),f.y);
}
fn layerWaterColor(world:vec2<f32>,original:vec4<f32>)->vec4<f32> {
 if(layerU.waterMask.w<0.5){return original;}
 var base=original;
 let wet=layerWaterMaskAt(world/layerU.waterMask.x);
 var coverage=smoothstep(0.8,1.0,wet.x);let t=layerU.waterPhase.x;
 let drift=layerU.waterPhase.yz*layerWaterCurrentScale;let pattern=world/layerWaterPatternSize;
 let shoreDistance=wet.y;
 let shading=min(layerU.waterControls.z,1.0);
 let waterColour=shading*clamp(2.0-layerU.waterControls.z,0.0,1.0);
 let dry=smoothstep(0.05,0.5,wet.z)*(1.0-coverage);
 let damp=wet.w*dry*waterColour;
 if(damp>0.0){
  let lapDry=pow(max(0.0,sin(t*1.6+layerWaterNoise(pattern*0.025)*3.0)),2.0);
  base=vec4<f32>(base.rgb*(1.0-0.12*damp*(0.5+0.5*lapDry)),base.a);
 }
 coverage*=smoothstep(0.0,layerWaterEdgeFadePixels/32.0,wet.y);
 if(coverage<=0.0){return vec4<f32>(clamp(mix(original.rgb,base.rgb,layerWaterSurfaceOpacity),vec3<f32>(0),vec3<f32>(base.a)),base.a);}
 let field=layerWaterField(pattern,drift*layerU.waterControls.x,t*layerU.waterControls.x);
 let deep=smoothstep(0.05,0.55,wet.y);
 // Native's physical-pixel scale and inverse painted-map scale cancel.
 let offset=layerWaterOffset(field)*deep*coverage*layerU.waterControls.x;
 let sample=clamp(world+offset,layerU.waterSample.xy,layerU.waterSample.zw);
 let warped=layerTerrainAt(sample,true);
 var result=warped.rgb;
 if(shading>0.0){result=layerWaterShade(warped.rgb,field,deep,waterColour);}
 let shore=(1.0-smoothstep(0.35,0.95,shoreDistance))*smoothstep(0.0,0.25,shoreDistance);
 let shorePatch=layerWaterNoise(world*0.025);
 let lap=pow(max(0.0,sin(shoreDistance*10.0+t*1.6+shorePatch*3.0)),2.0);
 let foam=shore*lap*(0.09+layerU.waterPhase.w*0.105)*(0.50+0.50*shorePatch)*coverage*layerU.waterControls.y*(1.0-smoothstep(0.1,0.4,wet.z));
 result=mix(result,vec3<f32>(0.62,0.80,0.84),clamp(waterColour*0.08*smoothstep(0.0,0.10,shoreDistance)*(1.0-smoothstep(0.10,0.40,shoreDistance)),0.0,1.0));
 let effect=mix(base.rgb,min(result,vec3<f32>(base.a)),coverage);
 var surface=clamp(mix(original.rgb,effect,layerWaterSurfaceOpacity),vec3<f32>(0),vec3<f32>(base.a));
 surface=mix(surface,vec3<f32>(0.72,0.84,0.87)*base.a,clamp(foam*layerWaterShoreFoamOpacity*coverage,0.0,1.0));
 return vec4<f32>(surface,base.a);
}
