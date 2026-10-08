#include <metal_stdlib>
using namespace metal;
// Production Enhanced ground shaders (§15, §26, §29). Recorded marks own all
// histories/ages; the shared projected water mask owns receiving-medium tests.
struct nm_ground_Mark {float4 centreAxis,crossKind,values;};
struct nm_ground_Frame {float4 mapping,clip;};
struct nm_ground_Out {float4 position [[position]];float2 local;float4 values [[flat]],mapping [[flat]],clip [[flat]];float kind [[flat]];};
vertex nm_ground_Out nm_ground_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const nm_ground_Mark* marks [[buffer(0)]],constant float4& viewport [[buffer(3)]],device const nm_ground_Frame* frames [[buffer(30)]]){
 const float2 c[6]={float2(-1,-1),float2(1,-1),float2(-1,1),float2(-1,1),float2(1,-1),float2(1,1)};
 nm_ground_Mark m=marks[iid];float2 p=m.centreAxis.xy+c[vid].x*m.centreAxis.zw+c[vid].y*m.crossKind.xy;
 nm_ground_Out o;o.position=float4(p.x/viewport.x*2-1,1-p.y/viewport.y*2,0,1);o.local=c[vid];nm_ground_Frame g=frames[uint(m.crossKind.w)];o.values=m.values;o.mapping=g.mapping;o.clip=g.clip;o.kind=m.crossKind.z;return o;
}
float4 nm_ground_texel(texture2d<float> mask,int2 p){if(any(p<0)||p.x>=int(mask.get_width())||p.y>=int(mask.get_height()))return float4(0);return mask.read(uint2(p));}
float3 nm_ground_mask(texture2d<float> mask,float2 p){float2 q=p-.5,f=fract(q);int2 a=int2(floor(q));return mix(mix(nm_ground_texel(mask,a).rgb,nm_ground_texel(mask,a+int2(1,0)).rgb,f.x),mix(nm_ground_texel(mask,a+int2(0,1)).rgb,nm_ground_texel(mask,a+int2(1,1)).rgb,f.x),f.y);}
float nm_ground_noise(float2 p){float2 a=floor(p),f=fract(p);f=f*f*(3.0-2.0*f);float4 h=float4(dot(a,float2(43.17,97.53)),dot(a+float2(1,0),float2(43.17,97.53)),dot(a+float2(0,1),float2(43.17,97.53)),dot(a+float2(1,1),float2(43.17,97.53)));h=fract(sin(h)*17341.23);return mix(mix(h.x,h.y,f.x),mix(h.z,h.w,f.x),f.y);}
fragment float4 nm_ground_trail(nm_ground_Out in [[stage_in]]){
 if(any(in.position.xy<in.clip.xy)||any(in.position.xy>=in.clip.xy+in.clip.zw))discard_fragment();
 float d=in.values.w<.5?dot(in.local,in.local):in.local.y*in.local.y;float coverage=1.0-smoothstep(.6,1.0,d);float k=1.0-in.values.y*coverage;return float4(k,k,k,1);
}
fragment float4 nm_ground_color(nm_ground_Out in [[stage_in]],texture2d<float> maskTexture [[texture(0)]],constant float4& control [[buffer(0)]]){
 if(any(in.position.xy<in.clip.xy)||any(in.position.xy>=in.clip.xy+in.clip.zw))discard_fragment();
 if(control.x<=0)return float4(0);
 float3 mask=nm_ground_mask(maskTexture,(in.mapping.xy+in.position.xy*in.mapping.z)/control.x);
 // A flagged scorch carries whole ticks; control.w is the frame's fraction.
 float2 p=in.local;float age=in.values.x;if(in.kind<1.5&&in.values.w>.5)age+=control.w;
 if(in.kind>1.5){
  bool foam=age>=4.0;float coverage=0;float3 tint=1;
  if(!foam){coverage=smoothstep(.8,1.0,mask.b);}else{
   age-=4.0;float r=length(p),phase=fract(age*2.0),other=fract(phase+.5);
   float ring=(1.0-smoothstep(.015,.09,abs(r-(.58+phase*.30))))*sin(phase*3.141593);
   ring+=(1.0-smoothstep(.015,.09,abs(r-(.58+other*.30))))*sin(other*3.141593);
   float patches=smoothstep(-.1,.8,sin(p.x*8.0+sin(p.y*5.0)+age*6.283185));
   coverage=ring*patches*smoothstep(.8,1.0,mask.r);tint=float3(.70,.83,.87);
  }
  float alpha=clamp(coverage*in.values.y,0.0,1.0);return float4(tint*alpha,alpha);
 }
 float seed=in.values.z;bool landing=seed>=64.0;if(landing)seed-=64.0;
 float n=nm_ground_noise(p*4.1+float2(seed*2.7,seed*1.3));float fine=nm_ground_noise(p*10.0+float2(seed,31.0));
 float radius=length(p)*(.87+.25*n),edge=1.0-smoothstep(.80,1.0,length(p));float coverage=smoothstep(.8,1.0,mask.b)*edge;
 if(coverage<=0)return float4(0);
 float fade=1.0-smoothstep(control.y,control.z,age);
 float scorch=(1.0-smoothstep(.24,.86,radius))*(.6+.4*fine)*.34;
 float hot=(1.0-smoothstep(0.0,.30,radius))*(1.0-smoothstep(15.0,90.0,age))*(.45+.55*fine);
 float3 tint=mix(float3(.055,.042,.030),float3(.92,.24,.035),hot);float alpha=max(scorch,hot*.50)*fade;
 if(landing){float core=(1.0-smoothstep(.22,.78,radius))*(.65+.35*fine)*.62;float streak=(1.0-smoothstep(.10,.27,abs(p.x+.07*sin(p.y*11.0))));streak*=smoothstep(-.98,-.65,p.y)*(1.0-smoothstep(-.20,.10,p.y));alpha=max(core,streak*(.30+.18*n));tint=mix(float3(.035,.027,.020),float3(.58,.12,.018),hot*.4);}
 alpha=clamp(alpha*coverage,0.0,.65);return float4(tint*alpha,alpha);
}
