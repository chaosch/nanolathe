// Presentation policy port of our MIT distortion_vertex/nmDistortionShift
// (DESIGN_GPU_RENDERER §§25, 27–28). Shared declarations and final-world
// sampling are supplied by the host; the source list is already ordered.
struct DistortionSource {previous:vec4<f32>,current:vec4<f32>,shape:vec4<f32>,params:vec4<f32>}
@group(0) @binding(5) var<storage,read> distortionSources:array<DistortionSource,192>;
struct DistortionOut {
 @builtin(position) position:vec4<f32>,@location(0) local:vec2<f32>,
 @location(1) @interpolate(flat) shape:vec4<f32>,@location(2) @interpolate(flat) params:vec4<f32>
}
@vertex fn distortion_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->DistortionOut {
 let corners=array<vec2<f32>,6>(vec2<f32>(-1,-1),vec2<f32>(1,-1),vec2<f32>(-1,1),vec2<f32>(-1,1),vec2<f32>(1,-1),vec2<f32>(1,1));
 // Local loads isolate the pinned Naga bounds conditional from arithmetic.
 var corner=corners[vid];var source=distortionSources[iid];
 let world=mix(source.previous.xyz,source.current.xyz,layerU.ground.w);
 var halfSize=source.shape.xy;
 if(source.shape.w<0.5){halfSize=vec2<f32>(source.shape.x+source.shape.y);}
 var offset=corner*halfSize*layerU.camera.z;
 if(source.shape.w>=0.5){offset.y+=(source.params.z-source.shape.y)*layerU.camera.z;}
 let anchor=vec2<f32>(world.x-layerU.camera.x,world.z-layerU.camera.y-world.y*0.5)*layerU.camera.z+layerU.viewport.xy*0.5;
 let pixel=anchor+offset;
 var out:DistortionOut;out.position=vec4<f32>(pixel.x/layerU.viewport.x*2-1,1-pixel.y/layerU.viewport.y*2,0,1);
 out.local=corner;out.shape=source.shape;out.params=source.params;return out;
}
fn distortionShift(input:DistortionOut)->vec2<f32> {
 var shift=vec2<f32>(0);let clock=input.params.x+layerU.ground.w-1;
 if(input.shape.w<0.5){
  if(clock<0 || clock>=15){discard;}
  let radius=mix(12.0,input.shape.x,clock/15.0);
  let d=input.local*(input.shape.x+input.shape.y);let r=length(d);
  let band=(r-radius)/max(input.shape.y,0.001);if(abs(band)>=1){discard;}
  let strength=min(clock,1.0)*pow(1-clock/15.0,2.0)*input.shape.z;
  if(r<0.001){discard;}
  let envelope=1-band*band;shift=d/r*(band*envelope*envelope*3.5*strength);
 }else{
  let rise=(1-input.local.y)*0.5;
  let t=(input.params.x+layerU.ground.w+input.params.y)*0.20943951;
  let center=sin(rise*5.0-t*0.5)*0.13*rise;
  let across=(input.local.x-center)/(0.58+0.42*rise);
  if(rise<=0 || rise>=1 || abs(across)>=1){discard;}
  let edge=1-across*across;let sine=sin(rise*3.14159265);let envelope=edge*edge*sine*sine;
  let wave=sin(rise*18.0-t*2.0+across*2.5)+0.35*sin(rise*33.0-t*3.0-across*4.0);
  var strength=input.shape.z;
  if(input.params.w>0){let age=max(input.params.w+layerU.ground.w-1,0.0);strength=0.55*pow(max(1-age/300,0.0),2.0);}
  shift=vec2<f32>(wave,0.24*sin(rise*22.0-t*2.0+across*3.0))*envelope*1.8*strength;
 }
 return shift;
}
@fragment fn distortion_fragment(input:DistortionOut)->@location(0) vec4<f32> {
 let shift=distortionShift(input);
 let samplePixel=clamp(input.position.xy+shift*layerU.camera.z,vec2<f32>(0),layerU.viewport.xy);
 // Fog/map clipping/gain stay on destinationPixel; every source reads the
 // original world/glow. This is replacement, never recursive refraction.
 return layerFinalWorld(samplePixel,input.position.xy);
}
