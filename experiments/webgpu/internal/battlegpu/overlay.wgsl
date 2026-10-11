struct LayerUniform {
 camera:vec4<f32>, viewport:vec4<f32>, rect:vec4<f32>, fogRect:vec4<f32>,
 tiles:vec4<u32>, flags:vec4<u32>, ground:vec4<f32>, glow:vec4<f32>, lightRect:vec4<f32>, lightValues:vec4<f32>,
 waterPhase:vec4<f32>, waterMask:vec4<f32>, waterControls:vec4<f32>, waterTerrain:vec4<f32>, waterSample:vec4<f32>,
}
struct LayerQuad {rect:vec4<f32>,uv:vec4<f32>,color:vec4<f32>,params:vec4<f32>}
struct LayerVertex {positionUV:vec4<f32>,color:vec4<f32>,bounds:vec4<f32>}
struct LayerGround {centreAxis:vec4<f32>,crossKind:vec4<f32>,values:vec4<f32>,mapping:vec4<f32>,clip:vec4<f32>}
struct LayerSmoke {quad:LayerQuad,selection:vec4<f32>,bounds:vec4<f32>,indices0:vec4<u32>,indices1:vec4<u32>,meta:vec4<u32>}
struct LayerLight {positionRadius:vec4<f32>,colorGroundGain:vec4<f32>,groundAgeFadeKind:vec4<f32>,flags:vec4<u32>}
@group(0) @binding(0) var<uniform> layerU:LayerUniform;
@group(0) @binding(1) var<storage,read> layerQuads:array<LayerQuad>;
@group(0) @binding(2) var<storage,read> layerVertices:array<LayerVertex>;
@group(0) @binding(3) var<storage,read> layerGrounds:array<LayerGround>;
@group(0) @binding(4) var<storage,read> layerSmokes:array<LayerSmoke>;
@group(0) @binding(5) var<storage,read> layerLights:array<LayerLight>;
@group(0) @binding(6) var layerAtlas:texture_2d<f32>;
@group(0) @binding(7) var layerTerrain:texture_2d<f32>;
@group(0) @binding(8) var layerFog:texture_2d<f32>;
@group(0) @binding(9) var layerWaterMask:texture_2d<f32>;
fn layerCorner(v:u32)->vec2<f32> {
 let c=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));return c[v];
}
fn layerPosition(p:vec2<f32>)->vec4<f32> {return vec4<f32>(p.x/layerU.viewport.x*2-1,1-p.y/layerU.viewport.y*2,0,1);}
struct LayerAnnotationOut {@builtin(position) position:vec4<f32>,@location(0) color:vec4<f32>}
@vertex fn layer_annotation_vertex(@builtin(vertex_index) v:u32,@builtin(instance_index) iid:u32)->LayerAnnotationOut {
 let q=layerQuads[iid];var o:LayerAnnotationOut;o.position=layerPosition(q.rect.xy+layerCorner(v)*q.rect.zw);o.color=q.color;return o;
}
@fragment fn layer_annotation_fragment(in:LayerAnnotationOut)->@location(0) vec4<f32> {
 return vec4<f32>(in.color.rgb*in.color.a,in.color.a);
}
fn layerTexel(tex:texture_2d<f32>,uv:vec2<f32>)->vec4<f32> {
 let size=textureDimensions(tex);return textureLoad(tex,clamp(vec2<i32>(floor(uv*vec2<f32>(size))),vec2<i32>(0),vec2<i32>(size)-1),0);
}
struct LayerQuadOut {
 @builtin(position) position:vec4<f32>,@location(0) uv:vec2<f32>,@location(1) color:vec4<f32>,
 @location(2) @interpolate(flat) bounds:vec4<f32>,@location(3) @interpolate(flat) params:vec4<f32>,
}
@vertex fn layer_quad_vertex(@builtin(vertex_index) v:u32,@builtin(instance_index) iid:u32)->LayerQuadOut {
 let q=layerQuads[iid];let c=layerCorner(v);var o:LayerQuadOut;
 o.position=layerPosition(q.rect.xy+c*q.rect.zw);o.uv=mix(q.uv.xy,q.uv.zw,c);o.color=q.color;o.bounds=vec4<f32>(0,0,1,1);o.params=q.params;return o;
}
@vertex fn layer_triangle_vertex(@builtin(vertex_index) vid:u32)->LayerQuadOut {
 let v=layerVertices[vid];var o:LayerQuadOut;o.position=layerPosition(v.positionUV.xy);o.uv=v.positionUV.zw;o.color=v.color;o.bounds=v.bounds;o.params=vec4<f32>(0);return o;
}
fn layerQuadTexel(in:LayerQuadOut)->vec4<f32> {
 if(in.params.x==1){return layerTexel(layerTerrain,in.uv);}if(in.params.x==2){return layerTexel(layerFog,in.uv);}return layerTexel(layerAtlas,in.uv);
}
@fragment fn layer_quad_fragment(in:LayerQuadOut)->@location(0) vec4<f32> {
 if(in.uv.x<in.bounds.x || in.uv.y<in.bounds.y || in.uv.x>=in.bounds.z || in.uv.y>=in.bounds.w){discard;}
 let tex=layerQuadTexel(in);
 if(in.params.x==2){return vec4<f32>(0,0,0,(1-tex.r)*in.color.a);}
 let a=tex.a*in.color.a;return vec4<f32>(tex.rgb*in.color.rgb*a,a);
}
@fragment fn layer_multiply_fragment(in:LayerQuadOut)->@location(0) vec4<f32> {
 let tex=layerQuadTexel(in);let a=tex.a*in.color.a;var k=tex.rgb*in.color.rgb;
 // Stock flashes decode retained LHT rows, never interpolate index bytes.
 if(in.params.z>0.5){k=vec3<f32>(min(2.0,1+round(tex.r*255)/30));}
 // Foreground multiplication has an opaque factor; stock atlas coverage
 // preserves the destination on keyed/half-alpha texels (§13.3).
 return vec4<f32>(mix(vec3<f32>(1),k,a),1);
}
@fragment fn layer_overlay_multiply_fragment(in:LayerQuadOut)->@location(0) vec4<f32> {
 return vec4<f32>(layerQuadTexel(in).rgb*in.color.rgb,1);
}
struct LayerGroundOut {
 @builtin(position) position:vec4<f32>,@location(0) local:vec2<f32>,
 @location(1) @interpolate(flat) values:vec4<f32>,@location(2) @interpolate(flat) mapping:vec4<f32>,
 @location(3) @interpolate(flat) clip:vec4<f32>,@location(4) @interpolate(flat) kind:f32,
}
@vertex fn layer_ground_vertex(@builtin(vertex_index) v:u32,@builtin(instance_index) iid:u32)->LayerGroundOut {
 let m=layerGrounds[iid];let c=layerCorner(v)*2-1;let p=m.centreAxis.xy+c.x*m.centreAxis.zw+c.y*m.crossKind.xy;
 var o:LayerGroundOut;o.position=layerPosition(p);o.local=c;o.values=m.values;o.mapping=m.mapping;o.clip=m.clip;o.kind=m.crossKind.z;return o;
}
fn layerWaterTexel(p:vec2<i32>)->vec3<f32> {
 let size=vec2<i32>(textureDimensions(layerWaterMask));if(p.x<0 || p.y<0 || p.x>=size.x || p.y>=size.y){return vec3<f32>(0);}return textureLoad(layerWaterMask,p,0).rgb;
}
fn layerWaterAt(p:vec2<f32>)->vec3<f32> {
 let q=p-0.5;let f=fract(q);let a=vec2<i32>(floor(q));return mix(mix(layerWaterTexel(a),layerWaterTexel(a+vec2<i32>(1,0)),f.x),mix(layerWaterTexel(a+vec2<i32>(0,1)),layerWaterTexel(a+vec2<i32>(1,1)),f.x),f.y);
}
fn layerGroundNoise(p:vec2<f32>)->f32 {
 let a=floor(p);var f=fract(p);f=f*f*(3-2*f);
 var h=vec4<f32>(dot(a,vec2<f32>(43.17,97.53)),dot(a+vec2<f32>(1,0),vec2<f32>(43.17,97.53)),dot(a+vec2<f32>(0,1),vec2<f32>(43.17,97.53)),dot(a+vec2<f32>(1,1),vec2<f32>(43.17,97.53)));
 h=fract(sin(h)*17341.23);return mix(mix(h.x,h.y,f.x),mix(h.z,h.w,f.x),f.y);
}
@fragment fn layer_trail_fragment(in:LayerGroundOut)->@location(0) vec4<f32> {
 if(in.position.x<in.clip.x || in.position.y<in.clip.y || in.position.x>=in.clip.x+in.clip.z || in.position.y>=in.clip.y+in.clip.w){discard;}
 var d=in.local.y*in.local.y;if(in.values.w<0.5){d=dot(in.local,in.local);}
 let k=1-in.values.y*(1-smoothstep(0.6,1.0,d));return vec4<f32>(k,k,k,1);
}
@fragment fn layer_ground_fragment(in:LayerGroundOut)->@location(0) vec4<f32> {
 if(in.position.x<in.clip.x || in.position.y<in.clip.y || in.position.x>=in.clip.x+in.clip.z || in.position.y>=in.clip.y+in.clip.w){discard;}
 if(layerU.ground.x<=0){return vec4<f32>(0);}
 let mask=layerWaterAt((in.mapping.xy+in.position.xy*in.mapping.z)/layerU.ground.x);
 let p=in.local;var age=in.values.x;if(in.kind<1.5 && in.values.w>0.5){age+=layerU.ground.w;}
 if(in.kind>1.5){
  let foam=age>=4;var coverage=0.0;var tint=vec3<f32>(1);
  if(!foam){coverage=smoothstep(0.8,1.0,mask.b);}
  else {
   age-=4;let r=length(p);let phase=fract(age*2);let other=fract(phase+0.5);
   var ring=(1-smoothstep(0.015,0.09,abs(r-(0.58+phase*0.30))))*sin(phase*3.141593);
   ring+=(1-smoothstep(0.015,0.09,abs(r-(0.58+other*0.30))))*sin(other*3.141593);
   let patches=smoothstep(-0.1,0.8,sin(p.x*8+sin(p.y*5)+age*6.283185));coverage=ring*patches*smoothstep(0.8,1.0,mask.r);tint=vec3<f32>(0.70,0.83,0.87);
  }
  let alpha=clamp(coverage*in.values.y,0.0,1.0);return vec4<f32>(tint*alpha,alpha);
 }
 var seed=in.values.z;let landing=seed>=64;if(landing){seed-=64;}
 let n=layerGroundNoise(p*4.1+vec2<f32>(seed*2.7,seed*1.3));let fine=layerGroundNoise(p*10+vec2<f32>(seed,31));
 let radius=length(p)*(0.87+0.25*n);let edge=1-smoothstep(0.80,1.0,length(p));let coverage=smoothstep(0.8,1.0,mask.b)*edge;
 if(coverage<=0){return vec4<f32>(0);}
 let fade=1-smoothstep(layerU.ground.y,layerU.ground.z,age);
 let scorch=(1-smoothstep(0.24,0.86,radius))*(0.6+0.4*fine)*0.34;
 let hot=(1-smoothstep(0.0,0.30,radius))*(1-smoothstep(15.0,90.0,age))*(0.45+0.55*fine);
 var tint=mix(vec3<f32>(0.055,0.042,0.030),vec3<f32>(0.92,0.24,0.035),hot);var alpha=max(scorch,hot*0.50)*fade;
 if(landing){
  let core=(1-smoothstep(0.22,0.78,radius))*(0.65+0.35*fine)*0.62;
  var streak=1-smoothstep(0.10,0.27,abs(p.x+0.07*sin(p.y*11)));streak*=smoothstep(-0.98,-0.65,p.y)*(1-smoothstep(-0.20,0.10,p.y));
  alpha=max(core,streak*(0.30+0.18*n));tint=mix(vec3<f32>(0.035,0.027,0.020),vec3<f32>(0.58,0.12,0.018),hot*0.4);
 }
 alpha=clamp(alpha*coverage,0.0,0.65);return vec4<f32>(tint*alpha,alpha);
}
struct LayerSmokeOut {
 @builtin(position) position:vec4<f32>,@location(0) uv:vec2<f32>,@location(1) irradiance:vec3<f32>,
 @location(2) @interpolate(flat) bounds:vec4<f32>,@location(3) @interpolate(flat) filter:f32,
}
@vertex fn layer_smoke_vertex(@builtin(vertex_index) v:u32,@builtin(instance_index) iid:u32)->LayerSmokeOut {
 let s=layerSmokes[iid];let c=layerCorner(v);var o:LayerSmokeOut;let p=s.quad.rect.xy+c*s.quad.rect.zw;
 o.position=layerPosition(p);o.uv=mix(s.quad.uv.xy,s.quad.uv.zw,c);o.bounds=s.bounds;o.filter=s.quad.params.x;
 let receiver=s.quad.rect.xy+c*s.quad.params.yz;let height=s.selection.w;let physical=vec3<f32>(receiver.x,receiver.y+height*0.5,height);
 var rgb=vec3<f32>(0);
 for(var i=0u;i<s.meta.x;i++){
  var index=0u;if(i<4u){index=s.indices0[i];}else{index=s.indices1[i-4u];}
  let light=layerLights[index];let d=light.positionRadius.xyz-physical;let d2=dot(d,d);let r2=light.positionRadius.w*light.positionRadius.w;
  if(d2>=r2){continue;}var f=1-d2/r2;f*=f;rgb+=light.colorGroundGain.rgb*(f*0.8*2.5);
 }
 o.irradiance=rgb;return o;
}
fn layerSmokeTexel(uv:vec2<f32>,bounds:vec4<f32>)->vec4<f32> {
 if(uv.x<bounds.x || uv.y<bounds.y || uv.x>=bounds.z || uv.y>=bounds.w){return vec4<f32>(0);}
 let t=layerTexel(layerAtlas,uv);return vec4<f32>(t.rgb*t.a,t.a);
}
@fragment fn layer_smoke_fragment(in:LayerSmokeOut)->@location(0) vec4<f32> {
 var source:vec4<f32>;
 if(in.filter>0.5){
  let size=vec2<f32>(textureDimensions(layerAtlas));let q=in.uv*size-0.5;let b=floor(q);let f=q-b;let o=(b+0.5)/size;let step=1/size;
  source=mix(mix(layerSmokeTexel(o,in.bounds),layerSmokeTexel(o+vec2<f32>(step.x,0),in.bounds),f.x),mix(layerSmokeTexel(o+vec2<f32>(0,step.y),in.bounds),layerSmokeTexel(o+step,in.bounds),f.x),f.y);
  source=vec4<f32>(min(source.rgb+(vec3<f32>(0.2)*source.a+source.rgb*0.8)*in.irradiance,vec3<f32>(source.a)),source.a);
 }else{
  source=layerSmokeTexel(in.uv,in.bounds);if(source.a>0){source=vec4<f32>(min(source.rgb+(0.2+0.8*source.rgb)*in.irradiance,vec3<f32>(1)),source.a);}
 }
 return source*0.5;
}
