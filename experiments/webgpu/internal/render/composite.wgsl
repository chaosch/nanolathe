struct Uniforms { camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,reserved:vec4<f32> }
@group(0) @binding(0) var world:texture_2d<f32>;
@group(0) @binding(1) var identity:texture_2d<f32>;
@group(0) @binding(2) var<uniform> uniforms:Uniforms;
@vertex fn composite_vertex(@builtin(vertex_index) i:u32)->@builtin(position) vec4<f32>{
 var p=array<vec2<f32>,3>(vec2<f32>(-1.0,-1.0),vec2<f32>(3.0,-1.0),vec2<f32>(-1.0,3.0));return vec4<f32>(p[i],0.0,1.0);
}
@fragment fn composite_fragment(@builtin(position) p:vec4<f32>)->@location(0) vec4<f32>{
 let at=vec2<i32>(p.xy);let color=textureLoad(world,at,0);let id=textureLoad(identity,at,0);
 if id.r+id.g+id.b>0.0{return color;}return vec4<f32>(0.02,0.03,0.04,1.0);
}
@vertex fn overlay_vertex(@builtin(vertex_index) i:u32)->@builtin(position) vec4<f32>{
 var p=array<vec2<f32>,6>(vec2<f32>(8.0,8.0),vec2<f32>(40.0,8.0),vec2<f32>(40.0,24.0),vec2<f32>(8.0,8.0),vec2<f32>(40.0,24.0),vec2<f32>(8.0,24.0));
 return vec4<f32>(p[i].x/uniforms.extent.x*2.0-1.0,1.0-p[i].y/uniforms.extent.y*2.0,0.0,1.0);
}
@fragment fn overlay_fragment()->@location(0) vec4<f32>{return vec4<f32>(0.0,1.0,1.0,1.0);}
