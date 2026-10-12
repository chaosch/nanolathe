// Exported retained shadows: independent ordered solo-body and authored
// quarter-projection sources, then the MIT Metal block/aircraft commit law.
struct ModelShadowUniforms {camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,tint:vec4<f32>,water:vec4<f32>}
struct ModelShadowVertex {position:vec4<f32>,piece:u32,pad0:u32,pad1:u32,pad2:u32}
struct ModelShadowPose {matrix:mat4x4<f32>,flags:vec4<u32>}
// This projection-only view preserves the shared 416-byte instance ABI.
struct ModelShadowInstance {ids:vec4<u32>,prefix:array<vec4<f32>,9>,shadow:vec4<f32>,middle:array<vec4<f32>,4>,screen:vec4<f32>,atlas:vec4<f32>,suffix:array<vec4<f32>,10>}
@group(0) @binding(0) var<storage,read> vertices:array<ModelShadowVertex,__VERTICES__>;
@group(0) @binding(1) var<storage,read> poses:array<ModelShadowPose,__POSES__>;
@group(0) @binding(2) var<storage,read> instances:array<ModelShadowInstance,__INSTANCES__>;
@group(0) @binding(3) var<uniform> u:ModelShadowUniforms;
struct ModelShadowProjectOut {@builtin(position) position:vec4<f32>,@location(0) @interpolate(flat) rect:vec4<f32>}
@vertex fn model_shadow_project_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->ModelShadowProjectOut {
 var out:ModelShadowProjectOut;out.position=vec4<f32>(2.0,2.0,2.0,1.0);
 let v=vertices[vid];let i=instances[iid];let pose=poses[i.ids.x+v.piece];let m=pose.matrix;
 // Structure projection excludes DontCache, independently of DontShadow.
 if m[3].w<0.5||(pose.flags.y&1u)!=0u{return out;}
 var world=(m*vec4<f32>(v.position.xyz,1.0)).xyz;let origin=select(i.shadow.z,m[1].w,m[2].w>0.5);
 let relative=floor(world.y-origin);var quarter=floor(relative/4.0);
 if u.tint.w>1.5&&relative-floor(relative/4.0)*4.0>=2.0{quarter+=0.5;}
 world.x+=quarter+5.0;world.z-=quarter;world.y=i.shadow.y;
 let screen=vec2<f32>(world.x-u.camera.x,world.z-world.y*0.5-u.camera.y)*u.camera.z+u.extent.xy*0.5;
 let pixel=(screen-i.screen.xy)*(i.atlas.zw/i.screen.zw)+i.atlas.xy;out.rect=i.atlas;
 out.position=vec4<f32>(pixel.x/u.extent.z*2.0-1.0,1.0-pixel.y/u.extent.w*2.0,0.5,1.0);return out;
}
@fragment fn model_shadow_project_fragment(input:ModelShadowProjectOut)->@location(0) f32 {
 if any(input.position.xy<input.rect.xy)||any(input.position.xy>=input.rect.xy+input.rect.zw){discard;}return 1.0;
}
