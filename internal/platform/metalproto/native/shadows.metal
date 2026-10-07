// Included after the shared native model and water shaders. Source contracts:
// DESIGN_GPU_RENDERER §22/§34, production model_direct/aircraft_shadow, and
// [03 R-REN-03D]. These masks never multiply the scene-wide terrain image.
struct NMShadowSubject {float4 screen,body,mask,params,placement;uint4 order;};
struct NMShadowRasterOut {float4 position [[position]];float4 rect [[flat]];};
vertex NMShadowRasterOut nm_shadow_project_vertex(uint vid [[vertex_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]],constant ModelRules& rule [[buffer(10)]],constant ModelSlot& slot [[buffer(13)]],device const uint* flags [[buffer(17)]],constant float4& tint [[buffer(18)]]) {
 NMShadowRasterOut o={};Vertex v=vertices[vid];uint at=offsets[0]+v.piece;float4x4 m=poses[at];
 // Production skips DontCache, not DontShadow, in the structure shadow lane.
 if(m[3].w<0.5||(flags[at]&1u)!=0){o.position=float4(2,2,2,1);return o;}
 float3 w=(m*float4(float3(v.p),1)).xyz;
 float origin=m[2].w>0.5?m[1].w:rule.shadow.z;
 float relative=floor(w.y-origin);float q=floor(relative/4);
 // Enhanced's doubled quarter shear carries the second low height bit as a
 // half-world-pixel addition (model_geometry.doubledPlacement, shearX=true).
 if(tint.w>1.5)q+=(relative-floor(relative/4)*4>=2?0.5:0.0);w.x+=q+5;w.z-=q;w.y=rule.shadow.y;
 float2 screen=float2(w.x-u.camera.x,w.z-w.y*0.5-u.camera.y)*u.camera.z+u.viewport.xy*0.5;
 float2 pixel=(screen-slot.screen.xy)*slot.atlas.zw/slot.screen.zw+slot.atlas.xy;o.rect=slot.atlas;
 o.position=float4(pixel.x/u.composition.x*2-1,1-pixel.y/u.composition.y*2,0.5,1);return o;
}
fragment float nm_shadow_project_fragment(NMShadowRasterOut in [[stage_in]]) {
 if(any(in.position.xy<in.rect.xy)||any(in.position.xy>=in.rect.xy+in.rect.zw))discard_fragment();return 1;
}
struct NMShadowCommitOut {float4 position [[position]];float2 screen;uint subject [[flat]];};
vertex NMShadowCommitOut nm_shadow_commit_vertex(uint vid [[vertex_id]],constant Uniforms& u [[buffer(3)]],device const NMShadowSubject* subjects [[buffer(0)]],constant uint& subject [[buffer(1)]]) {
 constexpr float2 corners[6]={float2(0,0),float2(1,0),float2(0,1),float2(1,0),float2(1,1),float2(0,1)};
 NMShadowCommitOut o;NMShadowSubject s=subjects[subject];o.screen=s.screen.xy+corners[vid]*s.screen.zw;o.subject=subject;
 o.position=float4(o.screen.x/u.viewport.x*2-1,1-o.screen.y/u.viewport.y*2,0.5,1);return o;
}
float nmShadowTap(texture2d<float> body,float2 p,float4 rect) {
 int2 at=int2(floor(p));if(any(float2(at)<rect.xy)||any(float2(at)>=rect.xy+rect.zw))return 0;return body.read(uint2(at)).a;
}
float nmShadowCoverage(texture2d<float> body,float2 p,float4 rect) {
 float2 a=floor(p-0.5)+0.5,f=fract(p-0.5);
 return mix(mix(nmShadowTap(body,a,rect),nmShadowTap(body,a+float2(1,0),rect),f.x),mix(nmShadowTap(body,a+float2(0,1),rect),nmShadowTap(body,a+1,rect),f.x),f.y);
}
fragment MRT nm_shadow_commit_fragment(NMShadowCommitOut in [[stage_in]],device const NMShadowSubject* subjects [[buffer(0)]],constant Uniforms& u [[buffer(3)]],constant float4& tint [[buffer(5)]],constant float4& water [[buffer(6)]],texture2d<float> body [[texture(0)]],depth2d<float> keys [[texture(1)]],texture2d<float> mask [[texture(2)]],texture2d<float> liquid [[texture(3)]]) {
 NMShadowSubject s=subjects[in.subject];float alpha=0;
 if(s.params.x>2.5){
  float2 world=(in.screen-u.viewport.xy*0.5)/u.camera.z+u.camera.xy;
  float wet=water.w>0?smoothstep(0.8,1.0,nmWaterMask(liquid,world/water.w).r):0;
  float2 p=world-water.yz*6;float t=water.x;
  float2 warp=float2(sin(p.y*0.14+t*0.8)+0.35*sin(p.x*0.09-t*0.55),0.45*sin((p.x+p.y)*0.11+t*0.65));
  float raster=max(tint.w,1.0);
  float2 src=s.body.xy+(in.screen-s.placement.xy-s.placement.zw+warp*(wet*s.params.w))*raster;
  // Radius is supplied in physical pixels. The production kernel offsets
  // twice-resolution texels; this ratio preserves its world-space reach.
  float radius=(s.params.z+0.5*wet*s.params.w)*0.5*raster;
  constexpr float weights[5]={1,4,6,4,1};float sum=0;
  for(int y=0;y<5;y++)for(int x=0;x<5;x++)sum+=nmShadowCoverage(body,src+float2(x-2,y-2)*radius,s.body)*weights[x]*weights[y];
  alpha=sum/256*mix(0.5,0.20,wet);
 }else {
  // Same block resolve as production §22. A structure punches only an
  // entirely covered body block; a mobile clips each of its own keyed samples.
  float2 block=floor(in.screen);float raster=max(tint.w,1.0);
  float cover=0,punch=0;int count=tint.w>1.5?2:1;
  for(int y=0;y<count;y++)for(int x=0;x<count;x++){
   float2 screen=block+(float2(x,y)+0.5)/raster;
   float2 bp=s.body.xy+(screen-s.placement.xy)*raster;
   if(s.params.x>1.5)bp-=s.placement.zw*raster;
   float coverage=nmShadowTap(body,bp,s.body);
   if(s.params.x<1.5){
    float2 mp=s.mask.xy+(screen-s.placement.zw)*raster;
    // Half-open source rectangles remain independent of viewport clipping.
    if(all(mp>=s.mask.xy)&&all(mp<s.mask.xy+s.mask.zw))cover+=mask.read(uint2(mp)).r>0.5?1:0;
    punch+=coverage>0.5?1:0;
   }else if(coverage>0.5){
    float key=256-round(keys.read(uint2(bp))*257);
    if(s.params.y<0.5||key>s.params.y)cover+=1;
   }
  }
  float samples=float(count*count);if(s.params.x>1.5||punch<samples)alpha=cover/samples*0.5;
 }
 MRT out;out.color=float4(tint.rgb*alpha,alpha);out.glow=float4(0);return out;
}
