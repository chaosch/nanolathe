@group(0) @binding(0) var<storage, read_write> result: array<u32>;
@compute @workgroup_size(64) fn smoke(@builtin(global_invocation_id) id: vec3<u32>) {
 if id.x < 4u { result[id.x] = 17u + id.x * 3u; }
}
@group(1) @binding(0) var atlas: texture_2d<f32>;
@group(1) @binding(1) var nearest_sampler: sampler;
struct VertexOut { @builtin(position) position: vec4<f32>, @location(0) uv: vec2<f32> }
@vertex fn vertex(@builtin(vertex_index) i: u32) -> VertexOut {
 var points = array<vec2<f32>, 3>(vec2<f32>(-0.8,-0.8), vec2<f32>(0.8,-0.8), vec2<f32>(0.0,0.8));
 var uvs = array<vec2<f32>, 3>(vec2<f32>(0.0,1.0), vec2<f32>(1.0,1.0), vec2<f32>(0.5,0.0));
 var out: VertexOut; out.position = vec4<f32>(points[i],0.5,1.0); out.uv = uvs[i]; return out;
}
@fragment fn fragment(in: VertexOut) -> @location(0) vec4<f32> {
 return textureSample(atlas, nearest_sampler, in.uv);
}
