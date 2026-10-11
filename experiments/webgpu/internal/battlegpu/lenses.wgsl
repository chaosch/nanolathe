// MIT native stock-lens replay (DESIGN_GPU_RENDERER C-G3 and §2.5).
// Samples are already cropped/ordered by the production retained publisher.
struct LensSample {@location(0) rect:vec4<f32>,@location(1) source:vec4<f32>,@location(2) key:vec4<f32>}
@group(0) @binding(0) var<uniform> lensViewport:vec4<f32>;
@group(0) @binding(1) var lensSnapshot:texture_2d<f32>;
struct LensOut {
 @builtin(position) position:vec4<f32>,@location(0) source:vec2<f32>,
 @location(1) @interpolate(flat) key:vec3<f32>
}
@vertex fn lens_vertex(@builtin(vertex_index) vid:u32,sample:LensSample)->LensOut {
 let corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
 // Local load keeps pinned Naga's bounds conditional outside multiplication.
 var corner=corners[vid];let pixel=sample.rect.xy+corner*sample.rect.zw;
 var out:LensOut;out.position=vec4<f32>(pixel.x/lensViewport.x*2-1,1-pixel.y/lensViewport.y*2,0,1);
 out.source=sample.source.xy+corner*sample.source.zw;out.key=sample.key.rgb;return out;
}
@fragment fn lens_fragment(input:LensOut)->@location(0) vec4<f32> {
 // Nearest sampling in absolute pixel coordinates, clamped to the viewport.
 let size=vec2<i32>(textureDimensions(lensSnapshot));
 let texel=clamp(vec2<i32>(floor(input.source)),vec2<i32>(0),size-1);
 let color=textureLoad(lensSnapshot,texel,0);let key=floor(color.rgb*255.0+0.5);
 if(key.x==input.key.x && key.y==input.key.y && key.z==input.key.z){discard;}
 // Preserve copied alpha and HDR values: stock lenses do no colour treatment.
 return color;
}

// The snapshot pass binds the completed world at the same texture binding.
// Its fullscreen geometry is clipped to ReadRect by the host scissor. No
// interpolation, filtering, keying, alpha conversion or HDR clamp occurs here.
@vertex fn lens_snapshot_vertex(@builtin(vertex_index) vid:u32)->@builtin(position) vec4<f32> {
 let corners=array<vec2<f32>,6>(vec2<f32>(0,0),vec2<f32>(1,0),vec2<f32>(0,1),vec2<f32>(0,1),vec2<f32>(1,0),vec2<f32>(1,1));
 var corner=corners[vid];
 return vec4<f32>(corner.x*2-1,1-corner.y*2,0,1);
}
@fragment fn lens_snapshot_fragment(@builtin(position) pixel:vec4<f32>)->@location(0) vec4<f32> {
 return textureLoad(lensSnapshot,vec2<i32>(floor(pixel.xy)),0);
}
