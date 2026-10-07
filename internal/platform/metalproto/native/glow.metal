#include <metal_stdlib>
using namespace metal;
struct nm_glow_vertex { float4 position_uv,color,params; };
struct nm_glow_parameters { float weights[8]; float4 blur; };
struct nm_glow_out { float4 position [[position]]; float2 uv; float4 color,params; };
vertex nm_glow_out nm_glow_source_vertex(uint vid [[vertex_id]],device const nm_glow_vertex *v [[buffer(0)]],constant float2 &size [[buffer(1)]]) {
    nm_glow_vertex a=v[vid];nm_glow_out o;
    o.position=float4(a.position_uv.x/size.x*2-1,1-a.position_uv.y/size.y*2,0,1);
    o.uv=a.position_uv.zw;o.color=a.color;o.params=a.params;return o;
}
inline float4 nm_glow_byte(float4 c) { return floor(clamp(c,0.0,1.0)*255+0.5)/255; }
fragment float4 nm_glow_source_fragment(nm_glow_out in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> completed [[texture(1)]]) {
    uint op=uint(in.params.x+0.5);float3 c=in.color.rgb;
    if(op==1||op==2) {
        int2 p=int2(floor(in.uv));if(any(p<0)||p.x>=int(atlas.get_width())||p.y>=int(atlas.get_height()))return 0;
        float4 tex=atlas.read(uint2(p));if(tex.a<0.5)return 0;
        if(op==1)c=tex.rgb*smoothstep(in.params.y,1.0,max(tex.r,max(tex.g,tex.b)));
        else {
            // Match the production high lane's float subtraction (§13.3/§19).
            float high=min(1+floor(tex.r*255+0.5)/30.0,2.0)-1;
            c=completed.read(uint2(in.position.xy)).rgb*high;
        }
    } else if(op==3) {
        float2 d=floor(in.uv);if(dot(d,d)>in.params.z)return 0;
        c=completed.read(uint2(in.position.xy)).rgb*in.params.w;
    }
    c*=in.color.a;
    // Production saturates/quantizes each contribution into RGBA8. The native
    // reusable RGBA16F plane accumulates these byte contributions; shrink clamps
    // their sum before sampling, preserving that emission law (not HDR bloom).
    return nm_glow_byte(float4(c,max(c.x,max(c.y,c.z))));
}
inline float4 nm_glow_read(texture2d<float,access::read> t,float2 p,int4 region) {
    int2 q=int2(floor(p));if(q.x<region.x||q.y<region.y||q.x>=region.z||q.y>=region.w)return 0;
    return t.read(uint2(q));
}
kernel void nm_glow_shrink(texture2d<float,access::read> src [[texture(0)]],texture2d<float,access::write> dst [[texture(1)]],uint2 q [[thread_position_in_grid]]) {
    if(q.x>=dst.get_width()||q.y>=dst.get_height())return;
    float4 sum=0;
    for(uint y=0;y<4;y++)for(uint x=0;x<4;x++){
        uint2 p=q*4+uint2(x,y);if(p.x<src.get_width()&&p.y<src.get_height())sum+=clamp(src.read(p),0.0,1.0);
    }
    dst.write(nm_glow_byte(sum/16),q);
}
inline float4 nm_glow_kernel(texture2d<float,access::read> src,float2 p,float2 dir,int4 region,bool far,constant nm_glow_parameters &u) {
    float4 sum=0;
    if(!far) {
        sum=nm_glow_read(src,p,region)*u.weights[0];
        for(uint i=1;i<=4;i++)sum+=(nm_glow_read(src,p+dir*(u.blur.x*i),region)+nm_glow_read(src,p-dir*(u.blur.x*i),region))*u.weights[i];
    } else {
        float k=0.5/(u.blur.y*u.blur.y),reach=2.5*u.blur.y,tail=exp(-0.5*2.5*2.5),norm=0;
        for(uint i=0;i<48;i++) {float d=float(i)-23.5;if(abs(d)>reach)continue;float w=max(exp(-d*d*k)-tail,0.0);norm+=w;sum+=nm_glow_read(src,p+dir*d,region)*w;}
        sum/=norm;
    }
    return nm_glow_byte(sum);
}
kernel void nm_glow_across(texture2d<float,access::read> src [[texture(0)]],texture2d<float,access::write> dst [[texture(1)]],constant nm_glow_parameters &u [[buffer(0)]],uint2 q [[thread_position_in_grid]]) {
    if(q.x>=dst.get_width()||q.y>=dst.get_height())return;
    uint qw=src.get_width();bool far=q.x>=qw;
    float2 p=far?float2((q.x-qw+0.5)*2,q.y+0.5):float2(q)+0.5;
    dst.write(nm_glow_kernel(src,p,float2(1,0),int4(0,0,src.get_width(),src.get_height()),far,u),q);
}
kernel void nm_glow_down(texture2d<float,access::read> src [[texture(0)]],texture2d<float,access::write> dst [[texture(1)]],constant nm_glow_parameters &u [[buffer(0)]],constant uint4 &sizes [[buffer(1)]],uint2 q [[thread_position_in_grid]]) {
    if(q.x>=dst.get_width()||q.y>=dst.get_height())return;
    uint qw=sizes.x,qh=sizes.y,ew=sizes.z,eh=sizes.w,fx=qw+2;
    bool near=q.x>=1&&q.x<qw+1&&q.y>=1&&q.y<qh+1;
    bool far=q.x>=fx&&q.x<fx+ew&&q.y>=1&&q.y<eh+1;
    float4 c=0;
    if(near)c=nm_glow_kernel(src,float2(q)-0.5,float2(0,1),int4(0,0,qw,qh),false,u);
    else if(far)c=nm_glow_kernel(src,float2(qw+(q.x-fx)+0.5,(q.y-1+0.5)*2),float2(0,1),int4(qw,0,qw+ew,qh),true,u);
    dst.write(c,q);
}
struct nm_glow_screen_out { float4 position [[position]]; };
vertex nm_glow_screen_out nm_glow_resolve_vertex(uint id [[vertex_id]]) {
    nm_glow_screen_out o;o.position=float4(id==1?3.0:-1.0,id==2?-3.0:1.0,0,1);return o;
}
fragment float4 nm_glow_resolve_fragment(nm_glow_screen_out in [[stage_in]],texture2d<float> octaves [[texture(0)]],constant nm_glow_parameters &u [[buffer(0)]],constant uint4 &sizes [[buffer(1)]]) {
    constexpr sampler linear(coord::pixel,address::clamp_to_edge,filter::linear);
    float2 p=in.position.xy/4;
    float3 n=clamp(octaves.sample(linear,p+1).rgb*u.blur.z,0.0,1.0);
    float3 f=clamp(octaves.sample(linear,p/2+float2(sizes.x+2,1)).rgb*u.blur.w,0.0,1.0);
    return float4(n+f-n*f,1);
}
