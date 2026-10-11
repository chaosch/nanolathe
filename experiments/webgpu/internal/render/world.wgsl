struct Uniforms { camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,reserved:vec4<f32> }
struct Vertex { position:vec4<f32>,uv:vec2<f32>,piece:u32,material:u32,color:vec4<f32> }
struct Instance { pose:u32,mesh:u32,id:u32,reserved:u32 }
@group(0) @binding(0) var<storage,read> vertices:array<Vertex>;
@group(0) @binding(1) var<storage,read> poses:array<mat4x4<f32>>;
@group(0) @binding(2) var<storage,read> instances:array<Instance>;
@group(0) @binding(3) var<uniform> uniforms:Uniforms;
@group(0) @binding(4) var atlas:texture_2d<f32>;
@group(0) @binding(5) var nearest_sampler:sampler;
@group(0) @binding(6) var<storage,read> materials:array<vec4<f32>>;
struct Out { @builtin(position) position:vec4<f32>,@location(0) uv:vec2<f32>,@location(1) color:vec3<f32>,@location(2) @interpolate(flat) id:u32,@location(3) @interpolate(flat) material:u32 }
@vertex fn vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out{
 let v=vertices[vid];let inst=instances[iid];let m=poses[inst.pose+v.piece];var out:Out;
 if m[3].w<0.5{out.position=vec4<f32>(2.0,2.0,2.0,1.0);return out;}
 let p=(m*vec4<f32>(v.position.xyz,1.0)).xyz;
 let screen=vec2<f32>(p.x-uniforms.camera.x,p.z-uniforms.camera.y-p.y*0.5)*uniforms.camera.z+uniforms.extent.xy*0.5;
 out.position=vec4<f32>(screen.x/uniforms.extent.x*2.0-1.0,1.0-screen.y/uniforms.extent.y*2.0,clamp(0.5-((p.z-uniforms.camera.y)+p.y*2.0)*0.00001,0.001,0.999),1.0);
 out.uv=v.uv;out.color=v.color.xyz;out.id=inst.id;out.material=v.material;return out;
}
struct MRT { @location(0) color:vec4<f32>,@location(1) identity:vec4<f32> }
@fragment fn fragment(input:Out)->MRT{
 let material=materials[input.material-1u];var out:MRT;
 out.color=textureSample(atlas,nearest_sampler,input.uv)*vec4<f32>(input.color*material.xyz,1.0);
 out.identity=vec4<f32>(f32(input.id&255u),f32((input.id>>8u)&255u),f32((input.id>>16u)&255u),material.w)/255.0;return out;
}
