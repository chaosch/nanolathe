struct LayerUniform {
 camera:vec4<f32>, viewport:vec4<f32>, rect:vec4<f32>, fogRect:vec4<f32>,
 tiles:vec4<u32>, flags:vec4<u32>, ground:vec4<f32>, glow:vec4<f32>, lightRect:vec4<f32>, lightValues:vec4<f32>,
 waterPhase:vec4<f32>, waterMask:vec4<f32>, waterControls:vec4<f32>, waterTerrain:vec4<f32>, waterSample:vec4<f32>,
}
struct LayerSprite {previous:vec4<f32>,current:vec4<f32>,rect:vec4<f32>,uv:vec4<f32>,color:vec4<f32>,params:vec4<f32>}
@group(0) @binding(0) var<uniform> layerU:LayerUniform;
@group(0) @binding(1) var<storage,read> layerSprites:array<LayerSprite>;
@group(0) @binding(2) var layerAtlas:texture_2d<f32>;
@group(0) @binding(3) var layerDetail:texture_2d<f32>;
struct LayerSpriteOut {
 @builtin(position) position:vec4<f32>, @location(0) uv:vec2<f32>, @location(1) color:vec4<f32>,
 @location(2) @interpolate(flat) detail:f32, @location(3) @interpolate(flat) additive:f32,
}
@vertex fn layer_sprite_vertex(@builtin(vertex_index) v:u32,@builtin(instance_index) iid:u32)->LayerSpriteOut {
 let corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
 let s=layerSprites[iid];let world=mix(s.previous.xyz,s.current.xyz,layerU.camera.w);let c=corners[v];
 let corner=s.rect.xy+c*s.rect.zw;let cosine=cos(s.params.z);let sine=sin(s.params.z);
 let rotated=vec2<f32>(corner.x*cosine-corner.y*sine,corner.x*sine+corner.y*cosine)*layerU.camera.z;
 let pixel=(vec2<f32>(world.x,world.z-world.y*0.5)-layerU.camera.xy)*layerU.camera.z+layerU.viewport.xy*0.5+rotated;
 let depth=clamp(0.5-((world.z-layerU.camera.y)+world.y*2)*0.00001,0.001,0.999);
 var out:LayerSpriteOut;out.position=vec4<f32>(pixel.x/layerU.viewport.x*2-1,1-pixel.y/layerU.viewport.y*2,clamp(depth+s.params.w,0.0001,0.9998),1);
 out.uv=mix(s.uv.xy,s.uv.zw,c);out.color=s.color;out.detail=s.previous.w;out.additive=s.params.y;return out;
}
fn layerSpriteTexel(tex:texture_2d<f32>,uv:vec2<f32>)->vec4<f32> {
 let size=textureDimensions(tex);return textureLoad(tex,clamp(vec2<i32>(floor(uv*vec2<f32>(size))),vec2<i32>(0),vec2<i32>(size)-1),0);
}
@fragment fn layer_sprite_fragment(in:LayerSpriteOut)->@location(0) vec4<f32> {
 var tex=layerSpriteTexel(layerAtlas,in.uv);if(in.detail>0.5){tex=layerSpriteTexel(layerDetail,in.uv);}
 let alpha=tex.a*in.color.a;if(alpha<0.001){discard;}
 return vec4<f32>(tex.rgb*in.color.rgb*alpha,select(alpha,0.0,in.additive>0.5));
}
