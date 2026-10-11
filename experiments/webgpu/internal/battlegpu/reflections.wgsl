// Stock sources and resolve are compiled as separate modules at this marker.
// Keeping the resource maps separate also keeps native Metal slots explicit.
struct ReflectionUniform {
 camera:vec4<f32>,viewport:vec4<f32>,rect:vec4<f32>,fogRect:vec4<f32>,
 tiles:vec4<u32>,flags:vec4<u32>,ground:vec4<f32>,glow:vec4<f32>,lightRect:vec4<f32>,lightValues:vec4<f32>,
 waterPhase:vec4<f32>,waterMask:vec4<f32>,waterControls:vec4<f32>,waterTerrain:vec4<f32>,waterSample:vec4<f32>
}
@group(0) @binding(0) var<uniform> reflectionU:ReflectionUniform;
@group(0) @binding(1) var reflectionAtlas:texture_2d<f32>;
struct ReflectionVertex {
 @location(0) positionHeight:vec4<f32>,@location(1) uv:vec4<f32>,
 @location(2) color:vec4<f32>,@location(3) bounds:vec4<f32>
}
struct ReflectionStockOut {
 @builtin(position) position:vec4<f32>,@location(0) uv:vec2<f32>,
 @location(1) @interpolate(flat) color:vec4<f32>,@location(2) @interpolate(flat) bounds:vec4<f32>,
 @location(3) height:f32,@location(4) @interpolate(flat) kind:f32
}
struct ReflectionPlanes {@location(0) color:vec4<f32>,@location(1) soft:vec4<f32>}
@vertex fn reflection_stock_vertex(v:ReflectionVertex)->ReflectionStockOut {
 var out:ReflectionStockOut;
 out.position=vec4<f32>(v.positionHeight.x/reflectionU.viewport.x*2.0-1.0,1.0-v.positionHeight.y/reflectionU.viewport.y*2.0,0.0,1.0);
 out.uv=v.uv.xy;out.height=v.positionHeight.z;out.kind=v.positionHeight.w;out.color=v.color;out.bounds=v.bounds;return out;
}
@fragment fn reflection_stock_fragment(in:ReflectionStockOut)->ReflectionPlanes {
 // GPU design §32.2: a billboard admits sea-level impacts; an interpolated
 // stroke still clips the portion at and below sea. Source order is retained.
 let sprite=in.kind<1.5;
 if((sprite && in.height<0.0) || (!sprite && in.height<=0.0)){discard;}
 var c=in.color;
 if(sprite){
  if(in.uv.x<in.bounds.x || in.uv.y<in.bounds.y || in.uv.x>=in.bounds.z || in.uv.y>=in.bounds.w){discard;}
  let at=clamp(vec2<i32>(floor(in.uv)),vec2<i32>(0),vec2<i32>(textureDimensions(reflectionAtlas))-1);
  let tex=textureLoad(reflectionAtlas,at,0);if(tex.a<0.5){discard;}
  c=vec4<f32>(tex.rgb,1.0);
 }
 c*=1.0-smoothstep(64.0,160.0,in.height);
 var out:ReflectionPlanes;out.color=c;out.soft=vec4<f32>(0.0,0.0,0.0,c.a);return out;
}

// --- resolve module ---
struct ReflectionUniform {
 camera:vec4<f32>,viewport:vec4<f32>,rect:vec4<f32>,fogRect:vec4<f32>,
 tiles:vec4<u32>,flags:vec4<u32>,ground:vec4<f32>,glow:vec4<f32>,lightRect:vec4<f32>,lightValues:vec4<f32>,
 waterPhase:vec4<f32>,waterMask:vec4<f32>,waterControls:vec4<f32>,waterTerrain:vec4<f32>,waterSample:vec4<f32>
}
@group(0) @binding(0) var<uniform> reflectionU:ReflectionUniform;
@group(0) @binding(1) var<uniform> reflectionControl:vec4<f32>;
@group(0) @binding(2) var reflectionColor:texture_2d<f32>;
@group(0) @binding(3) var reflectionSoft:texture_2d<f32>;
@group(0) @binding(4) var reflectionMask:texture_2d<f32>;
@group(0) @binding(5) var reflectionFlags:texture_2d<u32>;
struct ReflectionScreen {@builtin(position) position:vec4<f32>}
@vertex fn reflection_screen_vertex(@builtin(vertex_index) v:u32)->ReflectionScreen {
 let corners=array<vec2<f32>,3>(vec2<f32>(-1,-1),vec2<f32>(3,-1),vec2<f32>(-1,3));
 var corner=corners[v];var out:ReflectionScreen;out.position=vec4<f32>(corner,0,1);return out;
}
fn reflectionTexel(source:texture_2d<f32>,p:vec2<f32>)->vec4<f32> {
 let at=vec2<i32>(floor(p));let size=vec2<i32>(textureDimensions(source));
 if(at.x<0 || at.y<0 || at.x>=size.x || at.y>=size.y){return vec4<f32>(0);}
 return textureLoad(source,at,0);
}
fn reflectionWeighted(p:vec2<f32>,low:f32,high:f32)->vec4<f32> {
 let c=reflectionTexel(reflectionColor,p);let h=reflectionTexel(reflectionSoft,p);
 var strength=0.0;if(h.a>0.0){strength=clamp(h.r/h.a,0.0,1.0);}
 return c*mix(low,high,strength);
}
fn reflectionSample(p:vec2<f32>,low:f32,high:f32)->vec4<f32> {
 let a=floor(p-0.5)+0.5;let f=fract(p-0.5);
 return mix(mix(reflectionWeighted(a,low,high),reflectionWeighted(a+vec2<f32>(1,0),low,high),f.x),mix(reflectionWeighted(a+vec2<f32>(0,1),low,high),reflectionWeighted(a+1.0,low,high),f.x),f.y);
}
fn reflectionColorSample(p:vec2<f32>,low:f32)->vec4<f32> {
 let a=floor(p-0.5)+0.5;let f=fract(p-0.5);
 return mix(mix(reflectionTexel(reflectionColor,a)*low,reflectionTexel(reflectionColor,a+vec2<f32>(1,0))*low,f.x),mix(reflectionTexel(reflectionColor,a+vec2<f32>(0,1))*low,reflectionTexel(reflectionColor,a+1.0)*low,f.x),f.y);
}
fn reflectionMaskAt(p:vec2<f32>)->vec4<f32> {
 let a=floor(p-0.5)+0.5;let f=fract(p-0.5);
 return mix(mix(reflectionTexel(reflectionMask,a),reflectionTexel(reflectionMask,a+vec2<f32>(1,0)),f.x),mix(reflectionTexel(reflectionMask,a+vec2<f32>(0,1)),reflectionTexel(reflectionMask,a+1.0),f.x),f.y);
}
fn reflectionFinite(v:f32)->bool {return (bitcast<u32>(v)&0x7f800000u)!=0x7f800000u;}
fn reflectionResolveFlags(q:vec2<f32>)->u32 {
 // GPU design §26.6: classify actual quantized planes, then union the full
 // filter reach AFTER ripple. One physical pixel covers bilinear neighbours.
 let radius=vec2<f32>(max(abs(reflectionControl.y)+1.0,4.0*abs(reflectionControl.z)+1.0),max(1.0,2.0*abs(reflectionControl.z)+1.0));
 if(!reflectionFinite(q.x) || !reflectionFinite(q.y) || !reflectionFinite(radius.x) || !reflectionFinite(radius.y)){return 3u;}
 let size=vec2<f32>(textureDimensions(reflectionColor));let lo=q-radius;let hi=q+radius;
 if(hi.x<0.0 || hi.y<0.0 || lo.x>=size.x || lo.y>=size.y){return 0u;}
 let first=vec2<i32>(floor(clamp(lo,vec2<f32>(0),size-1.0)))/64;
 let last=vec2<i32>(floor(clamp(hi,vec2<f32>(0),size-1.0)))/64;
 var bits=0u;
 for(var y=first.y;y<=last.y;y++){for(var x=first.x;x<=last.x;x++){
  bits|=textureLoad(reflectionFlags,vec2<i32>(x,y),0).r;if(bits==3u){return bits;}
 }}return bits;
}
@fragment fn reflection_resolve(in:ReflectionScreen)->@location(0) vec4<f32> {
 let world=(in.position.xy-reflectionU.viewport.xy*0.5)/reflectionU.camera.z+reflectionU.camera.xy;
 let coverage=smoothstep(0.8,1.0,reflectionMaskAt(world/reflectionU.waterMask.x).r);
 if(coverage<=0.0){return vec4<f32>(0);}
 let t=reflectionU.waterPhase.x*reflectionU.waterControls.x;
 let ripple=sin(world.y*0.19+t*1.9+sin(world.x*0.07-t*0.6))*0.65+sin(world.y*0.37-t*1.3)*0.35;
 let q=in.position.xy+vec2<f32>(ripple*(0.65+reflectionU.waterPhase.w*0.65)*reflectionControl.y,0.0);
 let bits=reflectionResolveFlags(q);if((bits&1u)==0u){return vec4<f32>(0);}
 if((bits&2u)==0u){
  var c=reflectionColorSample(q,0.5)+reflectionColorSample(q+vec2<f32>(reflectionControl.y,0),0.25)+reflectionColorSample(q-vec2<f32>(reflectionControl.y,0),0.25);
  c=vec4<f32>(c.rgb*vec3<f32>(0.76,0.88,0.94),c.a);return c*(0.25*coverage);
 }
 var c=reflectionSample(q,0.5,0.0)+reflectionSample(q+vec2<f32>(reflectionControl.y,0),0.25,0.0)+reflectionSample(q-vec2<f32>(reflectionControl.y,0),0.25,0.0);
 c+=reflectionWeighted(floor(q)+0.5,0.0,0.25);
 let horizontal=array<f32,4>(0.12,0.09,0.06,0.03);
 for(var i=0u;i<4u;i++){let d=vec2<f32>(f32(i+1u)*reflectionControl.z,0);var weight=horizontal[i];c+=reflectionWeighted(floor(q+d)+0.5,0.0,weight)+reflectionWeighted(floor(q-d)+0.5,0.0,weight);}
 let vertical=array<f32,2>(0.06,0.015);
 for(var i=0u;i<2u;i++){let d=vec2<f32>(0,f32(i+1u)*reflectionControl.z);var weight=vertical[i];c+=reflectionWeighted(floor(q+d)+0.5,0.0,weight)+reflectionWeighted(floor(q-d)+0.5,0.0,weight);}
 c=vec4<f32>(c.rgb*vec3<f32>(0.76,0.88,0.94),c.a);return c*(0.25*coverage);
}
