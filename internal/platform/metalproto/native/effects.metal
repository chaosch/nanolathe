#include <metal_stdlib>
using namespace metal;

struct nm_fx_Quad {float4 rect,uv,color,params;};
struct nm_fx_Vertex {float4 positionUV,color,bounds;};
struct nm_fx_Sample {float4 rect,source,key;};
struct nm_fx_Out {float4 position [[position]];float2 uv;float4 color;float4 bounds [[flat]];float4 params [[flat]];};

vertex nm_fx_Out nm_fx_quad_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const nm_fx_Quad* quads [[buffer(0)]],constant float4& viewport [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 nm_fx_Quad q=quads[iid];float2 a=c[vid],p=q.rect.xy+a*q.rect.zw;nm_fx_Out o;
 o.position=float4(p.x/viewport.x*2-1,1-p.y/viewport.y*2,0,1);o.uv=mix(q.uv.xy,q.uv.zw,a);o.color=q.color;o.bounds=float4(0,0,1,1);o.params=q.params;return o;
}
vertex nm_fx_Out nm_fx_triangle_vertex(uint vid [[vertex_id]],device const nm_fx_Vertex* vertices [[buffer(0)]],constant float4& viewport [[buffer(3)]]) {
 nm_fx_Vertex v=vertices[vid];nm_fx_Out o;o.position=float4(v.positionUV.x/viewport.x*2-1,1-v.positionUV.y/viewport.y*2,0,1);
 o.uv=v.positionUV.zw;o.color=v.color;o.bounds=v.bounds;o.params=0;return o;
}
fragment float4 nm_fx_color_fragment(nm_fx_Out in [[stage_in]],texture2d<float> atlas [[texture(0)]]) {
 if(any(in.uv<in.bounds.xy)||any(in.uv>=in.bounds.zw))discard_fragment();
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);float4 t=atlas.sample(s,in.uv);float a=t.a*in.color.a;
 return float4(t.rgb*in.color.rgb*a,a);
}
fragment float4 nm_fx_multiply_fragment(nm_fx_Out in [[stage_in]],texture2d<float> atlas [[texture(0)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);float4 t=atlas.sample(s,in.uv);float a=t.a*in.color.a;
 float3 k=t.rgb*in.color.rgb;if(in.params.z>0.5)k=float3(min(2.0,1.0+round(t.r*255.0)/30.0));
 return float4(mix(float3(1),k,a),1);
}
vertex nm_fx_Out nm_fx_lens_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const nm_fx_Sample* samples [[buffer(0)]],constant float4& viewport [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 nm_fx_Sample s=samples[iid];float2 a=c[vid],p=s.rect.xy+a*s.rect.zw;nm_fx_Out o;
 o.position=float4(p.x/viewport.x*2-1,1-p.y/viewport.y*2,0,1);o.uv=s.source.xy+a*s.source.zw;o.color=s.key;o.bounds=0;o.params=0;return o;
}
fragment float4 nm_fx_lens_fragment(nm_fx_Out in [[stage_in]],texture2d<float> source [[texture(0)]]) {
 constexpr sampler s(coord::pixel,address::clamp_to_edge,filter::nearest);float4 t=source.sample(s,in.uv);
 if(all(floor(t.rgb*255.0+0.5)==in.color.rgb))discard_fragment();return t;
}
