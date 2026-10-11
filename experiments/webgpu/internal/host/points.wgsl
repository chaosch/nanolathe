// Untimed diagnostic: exercise GPU matrix-vector multiplication on each interpolated piece.
@group(0) @binding(0) var<storage,read> poses:array<mat4x4<f32>>;
@group(0) @binding(1) var<storage,read_write> points:array<vec4<f32>>;
@compute @workgroup_size(64) fn transform(@builtin(global_invocation_id) id:vec3<u32>){
 if id.x>=arrayLength(&poses){return;}
 let m=poses[id.x];let p=m*vec4<f32>(0.25,-0.13,0.17,1.0);
 points[id.x]=vec4<f32>(p.xyz,m[3].w);
}
