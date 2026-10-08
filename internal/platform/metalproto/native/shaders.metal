#include <metal_stdlib>
using namespace metal;
// Replay uses deliberately synthetic lights and shock rings. Live uses only
// submitted sources; neither path claims retail composition or light arithmetic.
// Packed vector lanes keep the Go/Metal ABI exactly 64 bytes.
struct Vertex { packed_float3 p; packed_float3 n; float2 uv; packed_float3 color; uint piece; uint material; packed_float3 shadeNormal; };
struct Uniforms { float4 camera; float4 rect; float4 viewport; float4 timing; float4 mode; float4 heightRect; float4 fogRect; float4 visual; float4 composition; float4 lightControl; };
// Depth-slab experiment: the staged subject mapping is evaluated as usual, then
// emitted at physical screen pixels instead of the subject's atlas slot.
constant bool nmDirect [[function_constant(0)]];
constant bool nmDirectOn=is_function_constant_defined(nmDirect)&&nmDirect;
// slab: paint entries plus one, this subject's paint rank. screen/atlas: the
// slot's physical rectangle and its raster-scaled atlas rectangle.
struct NMDirect { float4 slab; float4 screen; float4 atlas; };
struct Light { float4 positionRadius; float4 colorStrength; };
struct Material { uint firstFrame,frameCount,kind,reserved; };
struct ModelRules { float4 reveal,below,band,above,clip,shadow,outline; };
ModelRules nm_group_source_rule(ModelRules own,int4 member);
bool nm_group_blue(float ownKey,int4 member,device const ModelRules* rules);
struct NativeModelMaterial { float4 uv,color,params; };
struct ModelVisual { float4 state,bounds,outline,emission; };
struct Distortion { float4 previous,current,shape,params; };
struct Sprite { float4 previous,current,rect,uv,color,params; };
// Presentation-only rigid interpolation of already composed endpoints
// (DESIGN_GPU_RENDERER §5.3). Their final world-Z reflection is not a rotation.
float4 poseQuaternion(float4x4 m) {
 float3 reflection=float3(1,1,-1);
 // Fixed-point endpoint composition introduces small scale/orthogonality
 // errors. Gram-Schmidt gives a right-handed unit basis before conversion.
 float3 x=normalize(m[0].xyz*reflection);
 float3 y=m[1].xyz*reflection;y=normalize(y-x*dot(x,y));
 float3 z=cross(x,y);
 float4 q;
 float trace=x.x+y.y+z.z;
 if(trace>0){float s=2*sqrt(trace+1);q=float4((y.z-z.y)/s,(z.x-x.z)/s,(x.y-y.x)/s,s*0.25);}
 else if(x.x>y.y&&x.x>z.z){float s=2*sqrt(1+x.x-y.y-z.z);q=float4(s*0.25,(y.x+x.y)/s,(z.x+x.z)/s,(y.z-z.y)/s);}
 else if(y.y>z.z){float s=2*sqrt(1+y.y-x.x-z.z);q=float4((y.x+x.y)/s,s*0.25,(z.y+y.z)/s,(z.x-x.z)/s);}
 else {float s=2*sqrt(1+z.z-x.x-y.y);q=float4((z.x+x.z)/s,(z.y+y.z)/s,s*0.25,(x.y-y.x)/s);}
 return normalize(q);
}
float4 poseSlerp(float4 a,float4 b,float alpha) {
 float cosine=dot(a,b);
 // q and -q denote the same rotation; select the shorter path.
 if(cosine<0){b=-b;cosine=-cosine;}
 cosine=clamp(cosine,0.0,1.0);
 // Avoid division by a vanishing sine for nearly identical rotations.
 if(cosine>0.9995)return normalize(mix(a,b,alpha));
 float angle=acos(cosine);
 return normalize((sin((1-alpha)*angle)*a+sin(alpha*angle)*b)/sin(angle));
}
float4x4 poseRotation(float4 q) {
 float x=q.x,y=q.y,z=q.z,w=q.w;float3 reflection=float3(1,1,-1);
 return float4x4(float4(float3(1-2*(y*y+z*z),2*(x*y+z*w),2*(x*z-y*w))*reflection,0),
                 float4(float3(2*(x*y-z*w),1-2*(x*x+z*z),2*(y*z+x*w))*reflection,0),
                 float4(float3(2*(x*z+y*w),2*(y*z-x*w),1-2*(x*x+y*y))*reflection,0),float4(0,0,0,1));
}
kernel void interpolate_poses(device const float4x4* endpoints [[buffer(0)]],device float4x4* poses [[buffer(1)]],constant Uniforms& u [[buffer(3)]],uint at [[thread_position_in_grid]]) {
 uint count=uint(u.timing.z);if(at>=count)return;
 float4x4 a=endpoints[at],b=endpoints[at+count];
 // Visibility remains an either-endpoint gate, including at alpha zero/one.
 if(a[3].w<0.5||b[3].w<0.5){poses[at]=float4x4(0.0);return;}
 float alpha=clamp(u.timing.x,0.0,1.0);float4x4 m;
 if(alpha<=0)m=a;
 else if(alpha>=1)m=b;
 else {
  // Keep equal bases exact, including translating but nonrotating pieces:
  // normalization noise would otherwise perturb integer corner-height ties.
  bool same=all(a[0].xyz==b[0].xyz)&&all(a[1].xyz==b[1].xyz)&&all(a[2].xyz==b[2].xyz);
  m=same?a:poseRotation(poseSlerp(poseQuaternion(a),poseQuaternion(b),alpha));
  if(!all(a[3].xyz==b[3].xyz))m[3].xyz=mix(a[3].xyz,b[3].xyz,alpha);
  else m[3].xyz=b[3].xyz;
 }
 // Mat4[3] is metadata, not part of the affine basis. Shade always follows
 // the current endpoint; Mat4[15] retains its current visibility marker.
 m[0].w=b[0].w;m[3].w=b[3].w;
 // Tagged live model origins follow the same endpoint interpolation as XYZ.
 // Equal lanes remain exact, preserving the integer face ties of still units.
 m[1].w=alpha<=0?a[1].w:alpha>=1||a[1].w==b[1].w?b[1].w:mix(a[1].w,b[1].w,alpha);
 m[2].w=a[2].w>0.5&&b[2].w>0.5?1:0;poses[at]=m;
}
struct ModelSlot { float4 screen,atlas,params; };
struct Out { uint faceIndex [[flat]];int4 groupMember [[flat]]; float3 battleLight [[flat]];float3 faceNormal [[flat]];float4 atlasRect [[flat]]; float4 position [[position]]; float2 uv; float3 color; float3 normal; float3 world; float heightKey; float keyOrigin [[flat]]; float shadeRow; float4 state [[flat]]; float4 bounds [[flat]]; float4 outline [[flat]]; float4 emission [[flat]]; float4 reveal [[flat]],below [[flat]],band [[flat]],above [[flat]],clip [[flat]],shadow [[flat]],material [[flat]]; };
float terrainHeight(float2 xz, texture2d<float> heights, constant Uniforms& u) {
 if(u.visual.y<0.5)return 0;
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 uv=(xz-u.heightRect.xy)/max(u.heightRect.zw-u.heightRect.xy,float2(1));
 return heights.sample(s,uv,level(0)).r;
}
struct MRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; };
struct ModelResult {float4 color,glow,metadata;float depth;};
struct AtlasModelMRT {float4 color [[color(0)]];float4 glow [[color(1)]];float4 metadata [[color(2)]];float depth [[depth(any)]];};
struct ModelMRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; float depth [[depth(any)]]; };
float4 project(float3 p, constant Uniforms& u) {
 // Live world art uses half-height shear; replay retains its measured basis.
 float heightScale=u.mode.x>0.5?0.5:1.0;float depthHeight=u.mode.x>0.5?2.0:1.0;
 float2 screen=float2(p.x-u.camera.x,p.z-u.camera.y-p.y*heightScale)*u.camera.z+u.viewport.xy*0.5;
 return float4(screen.x/u.viewport.x*2-1,1-screen.y/u.viewport.y*2,clamp(0.5-((p.z-u.camera.y)+p.y*depthHeight)*0.00001,0.001,0.999),1);
}
vertex Out model_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]],device const ModelVisual* visuals [[buffer(5)]],device const Material* materials [[buffer(6)]],device const float4* frames [[buffer(7)]],device const uint* selectors [[buffer(8)]],device const ModelRules* rules [[buffer(10)]],device const NativeModelMaterial* selected [[buffer(11)]],device const uint* materialOffsets [[buffer(12)]],device const ModelSlot* slots [[buffer(13)]],device const uint* slotSelectors [[buffer(14)]],device const float4* centers [[buffer(15)]],device const nm_light_source* retainedLights [[buffer(16)]],device const nm_light_subject* subjectLights [[buffer(17)]],device const int4* groups [[buffer(20)]],device const nm_face_Prepared* faces [[buffer(21)]],device const uint* faceBases [[buffer(22)]],device const nm_projection_Record* projection [[buffer(27)]],device const uint* projectionFlags [[buffer(28)]],constant float4& projectionView [[buffer(29)]]) {
 if(u.mode.z>0.5)iid=selectors[iid];
 Vertex v=vertices[vid]; uint at=offsets[iid]+v.piece;
 float4x4 m=poses[at];
 // Hidden pieces carry a zero matrix. Suppress either-hidden endpoint instead
 // of blending it toward world origin; appearance snaps one sample late.
 if(m[3].w<0.5){Out hidden={};hidden.position=float4(2,2,2,1);hidden.uv=0;hidden.color=0;hidden.normal=float3(0,1,0);hidden.world=0;return hidden;}
 float3 w=(m*float4(float3(v.p),1)).xyz;
 ModelVisual visual=visuals[iid];
 Out o={};o.faceIndex=0xffffffffu;if(u.mode.w==2)o.groupMember=groups[iid]; o.position=project(w,u);o.uv=v.uv;float3 vertexColor=float3(v.color);
 if(v.material!=0){Material material=materials[v.material];uint frame=material.kind==1?min(uint(max(visual.state.x,0.0)),max(material.frameCount,1u)-1):0;float4 rect=frames[material.firstFrame+frame];o.uv=mix(rect.xy,rect.zw,v.uv);}
 if(u.mode.x>0.5&&v.material!=0&&materialOffsets[iid]!=0){NativeModelMaterial material=selected[materialOffsets[iid]-1+materials[v.material].reserved];o.material=material.params;if(material.params.x<0.5){o.position=float4(2,2,2,1);return o;}o.uv=mix(material.uv.xy,material.uv.zw,v.uv);vertexColor=material.color.rgb;}
 if(u.visual.w>0.5){ModelRules rule=rules[iid];if(u.mode.w==2)rule=nm_group_source_rule(rule,o.groupMember);o.reveal=rule.reveal;o.below=rule.below;o.band=rule.band;o.above=rule.above;o.clip=rule.clip;o.shadow=rule.shadow;o.outline=rule.outline;}
 if(u.visual.x>0.5){o.state=visual.state;o.bounds=visual.bounds;if(u.visual.w<0.5)o.outline=visual.outline;o.emission=visual.emission;}
 float keyOrigin=m[2].w>0.5?m[1].w:visual.bounds.w;
 o.heightKey=floor(w.y-keyOrigin);o.keyOrigin=keyOrigin;
 // Production SHD uses the model-space light before final world-Z reflection.
 float3 shadeNormal=(m*float4(float3(v.shadeNormal),0)).xyz*float3(1,1,-1);
 uint shadeRow=uint(int(dot(shadeNormal,float3(-0.8,1,0.25))*5.0))&31;
 if(m[0].w>1.5)shadeRow=15;
 o.shadeRow=m[0].w>0.5&&m[0].w<1.5?-1.0:float(shadeRow);o.color=vertexColor;o.normal=normalize((m*float4(float3(v.n),0)).xyz);o.world=w;
 o.faceNormal=o.normal.xzy;
 if(u.lightControl.y>0.5&&v.material!=0&&u.mode.w>1.5&&slotSelectors[iid]>0){
  nm_light_subject local=subjectLights[slotSelectors[iid]-1];
  float3 face=(m*float4(centers[materials[v.material].reserved].xyz,1)).xyz;
  float3 physical=float3((face.x-u.camera.x)*u.camera.z+u.viewport.x*.5,(face.z-u.camera.y)*u.camera.z+u.viewport.y*.5,face.y*u.camera.z);
  o.battleLight=nm_light_irradiance(retainedLights,local,physical,o.faceNormal,false);
 }
 if(u.mode.w>1.5){uint slot=slotSelectors[iid];if(slot==0){o.position=float4(2,2,2,1);return o;}ModelSlot placement=slots[slot-1];o.atlasRect=placement.atlas;float2 screen=float2((o.position.x+1)*0.5,(1-o.position.y)*0.5)*u.viewport.xy;if(u.mode.w==2)screen=nm_projection_screen(w,projection[iid],projectionFlags[at],projectionView,screen);float2 pixel=(screen-placement.screen.xy)*(placement.atlas.zw/placement.screen.zw)+placement.atlas.xy;if(u.mode.w==2&&v.material!=0&&faceBases[iid]>0){o.faceIndex=faceBases[iid]-1+materials[v.material].reserved;nm_face_Prepared face=faces[o.faceIndex];if(!nm_face_admitted(face)){o.position=float4(2,2,2,1);return o;}pixel=placement.atlas.xy+nm_face_pixel(screen,face);}
  if(nmDirectOn){float2 at=(pixel-placement.atlas.xy)*(placement.screen.zw/placement.atlas.zw)+placement.screen.xy;o.position=float4(at.x/u.viewport.x*2-1,1-at.y/u.viewport.y*2,0.5,1);}
  else o.position=float4(pixel.x/u.composition.x*2-1,1-pixel.y/u.composition.y*2,0.5,1);}
 return o;
}
float4 modelTexel(texture2d<float> atlas,texture2d<float> palette,float2 uv,bool blue) {
 constexpr sampler nearest(coord::normalized,address::clamp_to_edge,filter::nearest);
 float4 tex=atlas.sample(nearest,uv);if(tex.g>0.5)return float4(1,1,1,tex.a);
 float3 color=palette.read(uint2(uint(round(tex.r*255)),blue?1:0)).rgb;return float4(color,tex.a);
}
float modelKey(Out in) { float k=floor(in.heightKey)+in.clip.w;return k-floor(k/256)*256; }
float4 modelVerdict(Out in,float key) { return key<in.reveal.y?in.below:key>=in.reveal.x?in.above:in.band; }
ModelResult shadeModel(Out in,texture2d<float> atlas,constant Uniforms& u,device const Light* lights,texture2d<float> palette,device const nm_face_Prepared* faces,device const ModelRules* groupRules,bool footprint=false) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 // Slab subjects shade one pixel where the staged image resolves four 2x2
 // samples: average the four sub-sample texels (premultiplied) instead.
 float2 duvx=footprint?dfdx(in.uv):0,duvy=footprint?dfdy(in.uv):0;bool mappedFace=false;
 if(u.mode.w==2&&in.faceIndex!=0xffffffffu){nm_face_Prepared f=faces[in.faceIndex];if(!nm_face_admitted(f))discard_fragment();if(nm_face_mapped(f)){mappedFace=true;float4 lanes=nm_face_lanes(f,in.position.xy-in.atlasRect.xy);in.uv=nm_face_uv(lanes,float2(atlas.get_width(),atlas.get_height()));in.heightKey=lanes.z-in.clip.w;in.shadeRow=nm_face_shade(f,lanes);}}
 float key=modelKey(in);bool ruled=u.visual.w>0.5;
 bool blue=ruled&&(u.mode.w==2&&in.groupMember.x?nm_group_blue(key,in.groupMember,groupRules):(in.clip.x>1.5&&key<=in.clip.y));
 bool staged=u.mode.w>1.5;
 if(staged&&(any(in.position.xy<in.atlasRect.xy)||any(in.position.xy>=in.atlasRect.xy+in.atlasRect.zw)))discard_fragment();
 float4 tex;
 if(footprint){
  float4 acc=0;float2 size=float2(atlas.get_width(),atlas.get_height());
  float2 lim=4/size;duvx=clamp(duvx,-lim,lim);duvy=clamp(duvy,-lim,lim);
  for(int k=0;k<4;k++){
   float2 o=float2((k&1)?.5:-.5,(k&2)?.5:-.5);float2 uv;
   if(mappedFace)uv=nm_face_uv(nm_face_lanes(faces[in.faceIndex],in.position.xy+o-in.atlasRect.xy),size);
   else uv=in.uv+o.x*.5*duvx+o.y*.5*duvy;
   float4 t=modelTexel(atlas,palette,uv,blue);acc+=float4(t.rgb*t.a,t.a);
  }
  tex=acc.a>0?float4(acc.rgb/acc.a,acc.a*.25):float4(0);
 }else tex=modelTexel(atlas,palette,in.uv,blue);bool erased=tex.a<0.02;
 if(ruled&&((in.clip.x>0.5&&in.clip.x<1.5&&key<=in.clip.y)||(in.clip.z>0.5&&key<=in.clip.w)))erased=true;
 float4 verdict=ruled&&in.reveal.z>0.5?modelVerdict(in,key):float4(0,0,0,-1);
 if(verdict.w<-1.5)erased=true;
 if(!ruled&&in.state.z>0 && in.world.y>mix(in.bounds.y,in.bounds.x,clamp(in.state.z,0.0,1.0)))erased=true;
 if(erased&&!staged)discard_fragment();
 float opacity=u.mode.w>0.5||all(in.state==float4(0))?1:clamp(in.state.y,0.0,1.0);
 // Interpolate SHD rows before truncation and the true-colour scale clamp.
 float3 light=float3(in.shadeRow<0?1.0:min(0.06875*floor(in.shadeRow),2.0));
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<8;i++){
  float a=float(i)*0.785398+u.timing.y*0.18;
  float3 p=float3(u.camera.x+cos(a)*260,60+sin(a*2)*30,u.camera.y+sin(a)*230);
  float3 d=p-in.world;float r=length(d);float fall=pow(max(1-r/230,0.0),2.0);
  light+=float3(0.6+0.4*sin(a),0.3+0.3*cos(a),0.5+0.5*cos(a+2))*fall*max(dot(in.normal,normalize(d)),0.0)*1.8;
 }
 if(u.timing.w>0.5&&u.mode.x>0.5&&u.lightControl.y<0.5)for(uint i=0;i<uint(u.mode.y);i++){
  Light l=lights[i];float3 d=l.positionRadius.xyz-in.world;float r=length(d);float fall=pow(max(1-r/max(l.positionRadius.w,0.001),0.0),2.0);
  light+=l.colorStrength.rgb*l.colorStrength.w*fall*max(dot(in.normal,normalize(d+0.0001)),0.0);
 }
 float3 source=tex.rgb*in.color;
 if(blue&&in.material.x>1.5)source=palette.read(uint2(uint(in.material.w),1)).rgb;
 float3 color=u.lightControl.y>0.5?nm_light_model(source,light.x,in.battleLight):source*light;
 if(verdict.w<0){color=nm_light_finish(source,color,uint(in.material.y),nm_light_finish_response(in.faceNormal),u.lightControl.z>0.5);color=nm_light_glint(source,color,nm_light_glint_weight(in.faceNormal,in.material.z,u.lightControl.w>0.5));}
 if(verdict.w>=0){color=blue?palette.read(uint2(uint(verdict.w),1)).rgb:verdict.rgb;}
 // Modern display approximation: screen-blended cooling tint plus HDR bloom.
 float3 emission=in.emission.rgb*max(in.emission.w,0.0);
 color=1-(1-color)*(1-clamp(emission,0.0,1.0));
 // Retain integer corner-height ties, then the production reverse piece
 // order decides ownership (opened Solar rim, DESIGN_GPU_RENDERER §22.1).
 // Inter-subject ordering and triangle lane interpolation remain approximate.
 ModelResult out={};float modelY=in.keyOrigin+floor(in.heightKey);
 float projectedY=(in.position.y-u.viewport.y*0.5)/u.camera.z;
 float depthHeight=u.mode.x>0.5?2.5:2.0;
 out.depth=clamp(0.5-(projectedY+modelY*depthHeight)*0.00001,0.001,0.999);
 if(staged)out.depth=in.reveal.w>0.5?(256-key)/257.0:0.5;
 out.metadata=float4(in.world.y,key,blue?1:0,1);
 if(erased){out.color=0;out.glow=0;return out;}
 float alpha=tex.a*opacity;out.color=float4(color*alpha,alpha);out.glow=u.mode.x>0.5?float4(emission*alpha*u.timing.w,u.mode.w>0.5?alpha:0):float4(max(color-0.7,0.0)*u.timing.w,1);return out;
}
fragment ModelMRT model_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]],texture2d<float> palette [[texture(4)]],device const nm_face_Prepared* faces [[buffer(21)]],device const ModelRules* groupRules [[buffer(24)]]) {ModelResult a=shadeModel(in,atlas,u,lights,palette,faces,groupRules);ModelMRT o;o.color=a.color;o.glow=a.glow;o.depth=a.depth;return o;}
// One staged subject drawn straight into the world. The fragment returns to
// the subject's atlas-local raster position so face lanes, keys and verdicts
// match the staged image; the band keeps later paint ranks in front.
fragment ModelMRT model_direct_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]],texture2d<float> palette [[texture(4)]],device const nm_face_Prepared* faces [[buffer(21)]],device const ModelRules* groupRules [[buffer(24)]],constant NMDirect& direct [[buffer(30)]]) {
 in.position.xy=(in.position.xy-direct.screen.xy)*(direct.atlas.zw/direct.screen.zw)+direct.atlas.xy;
 ModelResult a=shadeModel(in,atlas,u,lights,palette,faces,groupRules,true);
 if(a.color.a<0.001)discard_fragment();
 ModelMRT o;o.color=a.color;o.glow=a.glow;o.depth=(direct.slab.x-direct.slab.y-1+a.depth)/direct.slab.x;return o;
}
// Shadow silhouettes need only coverage alpha and the staged key depth: the
// commit never reads body colour or emission, so the compiler drops lighting
// and palette colour work for this pass.
struct ShadowBodyMRT {float4 color [[color(0)]];float4 glow [[color(1)]];float depth [[depth(any)]];};
fragment ShadowBodyMRT shadow_body_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]],texture2d<float> palette [[texture(4)]],device const nm_face_Prepared* faces [[buffer(21)]],device const ModelRules* groupRules [[buffer(24)]]) {
 ModelResult a=shadeModel(in,atlas,u,lights,palette,faces,groupRules);ShadowBodyMRT o;o.color=float4(0,0,0,a.color.a);o.glow=0;o.depth=a.depth;return o;
}
fragment AtlasModelMRT model_atlas_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]],texture2d<float> palette [[texture(4)]],device const nm_face_Prepared* faces [[buffer(21)]],device const ModelRules* groupRules [[buffer(24)]]) {ModelResult a=shadeModel(in,atlas,u,lights,palette,faces,groupRules);AtlasModelMRT o;o.color=a.color;o.glow=a.glow;o.metadata=a.metadata;o.depth=a.depth;return o;}
fragment AtlasModelMRT outline_atlas_fragment(Out in [[stage_in]],constant Uniforms& u [[buffer(3)]],texture2d<float> palette [[texture(4)]],device const ModelRules* groupRules [[buffer(24)]]) {
 if(in.state.z<=0||in.outline.a<=0)discard_fragment();float key=modelKey(in);bool blue=u.visual.w>.5&&(in.groupMember.x>0?nm_group_blue(key,in.groupMember,groupRules):(in.clip.x>1.5&&key<=in.clip.y));
 AtlasModelMRT o;float3 color=blue?palette.read(uint2(uint(in.shadow.w),1)).rgb:in.outline.rgb;o.color=float4(color*in.outline.a,in.outline.a);o.glow=0;o.depth=in.reveal.w>.5?(256-key)/257.0:.5;o.metadata=float4(in.world.y,key,blue?1:0,1);return o;
}
vertex Out terrain_vertex(uint v [[vertex_id]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 Out o={};o.uv=c[v];o.world=float3(mix(u.rect.x,u.rect.z,o.uv.x),0,mix(u.rect.y,u.rect.w,o.uv.y));o.position=project(o.world,u);o.position.z=0.9999;o.color=1;o.normal=float3(0,1,0);return o;
}
struct TerrainMRT { float4 color [[color(0)]]; float4 glow [[color(1)]]; float depth [[depth(any)]]; };
fragment TerrainMRT terrain_fragment(Out in [[stage_in]],texture2d<float> terrain [[texture(0)]],constant Uniforms& u [[buffer(3)]],device const Light* lights [[buffer(4)]],texture2d<float> heights [[texture(1)]],texture2d<float> shadows [[texture(2)]],texture2d<float> waterMask [[texture(3)]],constant WaterUniform& water [[buffer(9)]],texture2d<float> groundField [[texture(5)]],texture2d<float> terrainTiles [[texture(6)]],texture2d<uint> terrainLookup [[texture(7)]],texture2d<float> terrainDetail [[texture(8)]],constant nm_terrain_tiles_Parameters& terrainParams [[buffer(25)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 // TNT is painted background, not a reconstructed depth surface. A depth
 // plane would cut flat GAF art at its anchor [DESIGN_GPU_RENDERER C-G5].
 TerrainMRT out;out.depth=0.9999;
 out.color=terrainParams.controls.x>.5?nm_terrain_tiles_color(terrainTiles,terrainLookup,terrainDetail,in.world.xz,terrainParams):nmWaterColor(terrain,waterMask,in.uv,in.world.xz,water);out.glow=0;
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<8;i++){
  float a=float(i)*0.785398+u.timing.y*0.18;float2 p=float2(u.camera.x+cos(a)*260,u.camera.y+sin(a)*230);
  float fall=pow(max(1-length(p-in.world.xz)/120,0.0),2.0);
  float3 tint=float3(0.6+0.4*sin(a),0.3+0.3*cos(a),0.5+0.5*cos(a+2));out.color.rgb+=tint*fall*0.4;out.glow.rgb+=tint*fall*0.6;
 }
 if(u.timing.w>0.5&&u.mode.x>0.5&&u.lightControl.y<0.5)for(uint i=0;i<uint(u.mode.y);i++){
  Light l=lights[i];float r=length(l.positionRadius.xyz-in.world);float fall=pow(max(1-r/max(l.positionRadius.w,0.001),0.0),2.0);out.color.rgb+=l.colorStrength.rgb*l.colorStrength.w*fall*0.4;
 }
 if(u.lightControl.y>0.5)out.color=nm_light_ground_resolve(out.color,groundField.sample(s,in.position.xy/u.viewport.xy).rgb);
 if(u.visual.x>0.5){float mask=shadows.sample(s,in.position.xy/u.viewport.xy).r;out.color.rgb*=1-mask;}
 return out;
}
struct SpriteOut { float4 position [[position]];float2 uv;float4 color;float2 emissionAdditive;float detail [[flat]]; };
vertex SpriteOut sprite_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const Sprite* sprites [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 Sprite s=sprites[iid];float3 world=mix(s.previous.xyz,s.current.xyz,u.timing.x);float2 corner=s.rect.xy+c[v]*s.rect.zw;
 float cosine=cos(s.params.z),sine=sin(s.params.z);corner=float2(corner.x*cosine-corner.y*sine,corner.x*sine+corner.y*cosine)*u.camera.z;
 SpriteOut o;o.position=project(world,u);o.position.xy+=float2(corner.x/u.viewport.x*2,-corner.y/u.viewport.y*2);o.position.z=clamp(o.position.z+s.params.w,0.0001,0.9998);o.uv=mix(s.uv.xy,s.uv.zw,c[v]);o.color=s.color;o.emissionAdditive=s.params.xy;o.detail=s.previous.w;return o;
}
fragment MRT sprite_fragment(SpriteOut in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> detail [[texture(1)]],constant Uniforms& u [[buffer(3)]]) {
 // Authored GAF art is indexed: whole texels, as the production sprite path
 // draws them. A flagged sprite reads its 2x feature variant atlas.
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);float4 tex=in.detail>.5?detail.sample(s,in.uv):atlas.sample(s,in.uv);float alpha=tex.a*in.color.a;if(alpha<0.001)discard_fragment();
 float3 premult=tex.rgb*in.color.rgb*alpha;MRT out;out.color=float4(premult,in.emissionAdditive.y>0.5?0:alpha);out.glow=float4(premult*max(in.emissionAdditive.x,0.0)*smoothstep(0.45,1.0,max(tex.r,max(tex.g,tex.b)))*u.timing.w,0);return out;
}
// Selected-unit quads: physical-pixel rectangles drawn inside their unit's
// paint band, so the unit's body covers them as the production painter does.
struct Annotation { float4 rect,color; };
struct AnnotationOut { float4 position [[position]];float4 color; };
vertex AnnotationOut annotation_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const Annotation* a [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 Annotation q=a[iid];float2 p=q.rect.xy+c[v]*q.rect.zw;AnnotationOut o;o.position=float4(p.x/u.viewport.x*2-1,1-p.y/u.viewport.y*2,0,1);o.color=q.color;return o;
}
fragment MRT annotation_fragment(AnnotationOut in [[stage_in]]) { MRT out;out.color=float4(in.color.rgb*in.color.a,in.color.a);out.glow=float4(0);return out; }
struct Screen { float4 p [[position]];float2 uv; };
vertex Screen screen_vertex(uint v [[vertex_id]]) {
 float2 p=float2((v<<1)&2,v&2);Screen o;o.p=float4(p*2-1,0,1);o.uv=float2(p.x,1-p.y);return o;
}
struct DepthOnly { float depth [[depth(any)]]; };
fragment DepthOnly clear_depth_fragment() { DepthOnly out;out.depth=1;return out; }
// Resolve only covered pixels from the completed native subject. Applying
// opacity here prevents rear faces and equal-height winners from accumulating.
fragment ModelMRT subject_resolve_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> emission [[texture(1)]],depth2d<float> depth [[texture(2)]],constant float& opacity [[buffer(0)]]) {
 uint2 at=uint2(in.p.xy);float4 c=color.read(at);if(c.a<0.001)discard_fragment();
 ModelMRT out;out.color=c*opacity;out.glow=float4(emission.read(at).rgb*opacity,0);out.depth=depth.read(at);return out;
}
kernel void blur(texture2d<float,access::sample> src [[texture(0)]],texture2d<float,access::write> dst [[texture(1)]],constant float2& direction [[buffer(0)]],uint2 p [[thread_position_in_grid]]) {
 if(any(p>=uint2(dst.get_width(),dst.get_height())))return;
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 uv=(float2(p)+0.5)/float2(dst.get_width(),dst.get_height());float2 d=direction/float2(dst.get_width(),dst.get_height());
 float4 c=src.sample(s,uv)*0.227027;c+=(src.sample(s,uv+d*1.384615)+src.sample(s,uv-d*1.384615))*0.316216;c+=(src.sample(s,uv+d*3.230769)+src.sample(s,uv-d*3.230769))*0.070270;dst.write(c,p);
}
fragment float4 composite_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> glow [[texture(1)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 uv=in.uv;
 if(u.timing.w>0.5&&u.mode.x<0.5)for(uint i=0;i<4;i++){
  float phase=fract(u.timing.y*0.45+float(i)*0.25);float2 center=float2(0.3+0.4*float(i&1),0.3+0.4*float((i>>1)&1));
  float2 d=(uv-center)*u.viewport.xy;float r=length(d);float radius=25+phase*260;float band=(r-radius)/14;
  float shift=sin(band*3.14159)*max(1-abs(band),0.0)*(1-phase)*4;uv+=normalize(d+0.001)*shift/u.viewport.xy;
 }
 float3 c=color.sample(s,uv).rgb;if(u.timing.w>0.5)c+=glow.sample(s,in.uv).rgb*0.65;
 return float4(c,1);
}

// The production metadata selects quarter-sheared structures versus body
// silhouettes. Per-subject mask ownership and soft aircraft filtering are
// separate composition work; this path already preserves the correct shape.
vertex Out shadow_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]],device const ModelVisual* visuals [[buffer(5)]],texture2d<float> heights [[texture(1)]],device const Material* materials [[buffer(6)]],device const float4* frames [[buffer(7)]],device const uint* selectors [[buffer(8)]],device const ModelRules* rules [[buffer(10)]],device const NativeModelMaterial* selected [[buffer(11)]],device const uint* materialOffsets [[buffer(12)]],device const ModelSlot* slots [[buffer(13)]],device const uint* slotSelectors [[buffer(14)]]) {
 iid=selectors[iid];Out o={};ModelVisual visual=visuals[iid];Vertex v=vertices[vid];float4x4 m=poses[offsets[iid]+v.piece];
 if(visual.state.w<0.5||m[3].w<0.5){o.position=float4(2,2,2,1);return o;}
 float3 w=(m*float4(float3(v.p),1)).xyz;float origin=m[2].w>0.5?m[1].w:visual.bounds.w;
 o.heightKey=floor(w.y-origin);o.uv=v.uv;o.state=visual.state;
 if(v.material!=0){Material material=materials[v.material];uint frame=material.kind==1?min(uint(max(visual.state.x,0.0)),max(material.frameCount,1u)-1):0;float4 rect=frames[material.firstFrame+frame];o.uv=mix(rect.xy,rect.zw,v.uv);}
 if(u.mode.x>0.5&&v.material!=0&&materialOffsets[iid]!=0){NativeModelMaterial material=selected[materialOffsets[iid]-1+materials[v.material].reserved];o.material=material.params;o.uv=mix(material.uv.xy,material.uv.zw,v.uv);}
 if(u.visual.w>0.5){ModelRules rule=rules[iid];o.reveal=rule.reveal;o.below=rule.below;o.band=rule.band;o.above=rule.above;o.clip=rule.clip;o.shadow=rule.shadow;
  if(rule.shadow.x<0.5){o.position=float4(2,2,2,1);return o;}
  float relative=floor(w.y-origin);
  if(rule.shadow.x<1.5){float quarter=floor(relative/4);w.x+=quarter;w.z-=quarter;w.y=rule.shadow.y;}
  else {w.y=w.y-origin+rule.shadow.y;}
 }else w.y=u.visual.y>0.5?terrainHeight(w.xz,heights,u):visual.bounds.z;
 w.x+=5;o.position=project(w,u);return o;
}
fragment float4 shadow_fragment(Out in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> palette [[texture(4)]],constant Uniforms& u [[buffer(3)]]) {
 if(u.visual.w>0.5&&in.shadow.x>1.5){float key=modelKey(in);if(modelTexel(atlas,palette,in.uv,false).a<0.02||in.material.x<0.5)discard_fragment();if(in.reveal.z>0.5&&modelVerdict(in,key).w<-1.5)discard_fragment();if((in.clip.z>0.5&&key<=in.clip.w)||(in.reveal.w>0.5&&in.clip.x>0.5&&key<=in.clip.y))discard_fragment();}
 return float4(0.5);
}
fragment ModelMRT outline_fragment(Out in [[stage_in]],constant Uniforms& u [[buffer(3)]]) {
 if(u.mode.w>1.5&&(any(in.position.xy<in.atlasRect.xy)||any(in.position.xy>=in.atlasRect.xy+in.atlasRect.zw)))discard_fragment();
 if(in.state.z<=0||in.outline.a<=0)discard_fragment();ModelMRT o;o.color=float4(in.outline.rgb*in.outline.a,in.outline.a);o.glow=0;o.depth=u.mode.w>1.5?(in.reveal.w>0.5?(256-modelKey(in))/257.0:0.5):in.position.z;return o;
}
// bands>0: slab subjects are present. Their silhouettes are softened where a
// 4-neighbour lies in another paint band, approximating the staged image's
// 2x2 coverage resolve that slab bodies no longer get. Non-slab pixels keep
// depth 1 and are only blended next to a slab body.
fragment float4 world_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> glow [[texture(1)]],depth2d<float> depth [[texture(2)]],constant Uniforms& u [[buffer(3)]],constant float& bands [[buffer(4)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);float3 c=color.sample(s,in.uv).rgb;
 if(bands>0){
  int2 p=int2(in.p.xy),size=int2(color.get_width(),color.get_height());float d=depth.read(uint2(p));
  float band=d<1?floor((1-d)*bands)+1:0;float3 acc=0;float k=0;
  const int2 o[4]={int2(1,0),int2(-1,0),int2(0,1),int2(0,-1)};
  for(int i=0;i<4;i++){int2 q=p+o[i];if(any(q<0)||any(q>=size))continue;float dq=depth.read(uint2(q));if(min(d,dq)>=1)continue;
   if((dq<1?floor((1-dq)*bands)+1:0)==band)continue;acc+=color.read(uint2(q)).rgb;k++;}
  if(k>0)c=mix(c,acc/k,k>1?.4:.25);
 }
 if(u.timing.w>0.5)c+=glow.sample(s,in.uv).rgb*0.65;return float4(c,1);
}
// Same cell op encoding and ordered 2x2 mask walk as the production fog pass
// (DESIGN_GPU_RENDERER C-G7). Authored masks use integer texel reads: no blur.
float3 fogGray(float3 c) {
 return float3(floor(dot(floor(c*255.0+0.5),float3(1))/3.0)/255.0);
}
float3 applyFog(float3 col,float2 projected,float checker,texture2d<float> grid,texture2d<float> atlas,constant Uniforms& u) {
 float2 p=floor(projected);int2 cell=int2(floor((p-u.fogRect.xy)/32.0));
 float3 dark=atlas.read(uint2(0,512)).rgb;
 for(int j=-1;j<=0;j++)for(int i=-1;i<=0;i++){
  int2 c=cell+int2(i,j);if(any(c<0)||c.x>=int(grid.get_width())||c.y>=int(grid.get_height()))continue;
  uint2 codes=uint2(round(grid.read(uint2(c)).rg*255.0));if(all(codes==uint2(0)))continue;
  int2 d=int2(p-u.fogRect.xy-float2(c)*32.0);bool inCell=all(d>=0)&&all(d<32),inTile=all(d>=0)&&all(d<64);
  uint code1=codes.x,code0=codes.y;
  if(code1==1&&inCell)col=fogGray(col);
  else if(code1==2&&inCell&&checker<0.5)col=dark;
  else if(code1>=3&&inTile){
   uint slot=code1-3;bool dither=slot>=56;if(dither)slot-=56;
   uint2 at=uint2((slot%14)*64,(slot/14)*64)+uint2(d);
   if(atlas.read(at).a>=0.5){if(!dither)col=fogGray(col);else if(checker<0.5)col=dark;}
  }
  if(code0==1&&inCell)col=dark;
  else if(code0>=2&&inTile){
   uint slot=code0-2;uint2 at=uint2((slot%14)*64,(4+slot/14)*64)+uint2(d);float4 tex=atlas.read(at);
   if(tex.a>=0.5)col=tex.rgb;
  }
 }
 return col;
}
// The production world surface is black beyond the map rectangle, which a
// zoomed-out camera centres in the view. Live pixels whose ground position
// leaves the terrain rectangle resolve to black, so edge-row sprites do not
// overhang into that border.
float3 nmMapClip(float3 c,float2 pixel,constant Uniforms& u) {
 if(u.mode.x<0.5||u.rect.z<=u.rect.x||u.rect.w<=u.rect.y)return c;
 float2 p=(pixel-u.viewport.xy*0.5)/u.camera.z+u.camera.xy;
 return any(p<u.rect.xy)||any(p>=u.rect.zw)?float3(0):c;
}
fragment float4 fog_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> fog [[texture(1)]],texture2d<float> masks [[texture(2)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);float3 c=color.sample(s,in.uv).rgb;
 if(u.visual.z>0.5){
  float2 projected=(in.p.xy-u.viewport.xy*0.5)/u.camera.z+u.camera.xy;
  float2 cameraOrigin=floor(u.camera.xy-u.viewport.xy/(2*u.camera.z));
  float checker=float(int(floor(in.p.x)+floor(in.p.y)+cameraOrigin.x+cameraOrigin.y)&1);
  c=applyFog(c,projected,checker,fog,masks,u);
 }
 return float4(nmMapClip(c,in.p.xy,u),1);
}
// One pass on the drawable replaces world resolve, full-frame snapshot copy,
// distortion target and fog: the resolved world is never stored. bands>0
// softens slab silhouettes; bloom adds the half-size blur.
// gain rescales the finished world when the display gamma factor has moved
// since the world's art was baked: palette = min(255, base*factor), so a new
// factor is the old colour times new/old, clamped (zero means unchanged).
struct NMFinal { float bands,bloom,gain,pad1; };
float3 nmFinalGain(float3 c,constant NMFinal& f) { return f.gain>0?min(c*f.gain,float3(1)):c; }
// Production glow octaves, screened at the final pass instead of a separate
// full-screen composite into the world colour (same arithmetic as the glow
// resolve fragment). flags.x enables it for this frame.
struct NMGlowFinal { float4 blur; uint4 sizes; float4 flags; };
float3 nmFinalGlow(float3 c,float2 pixel,texture2d<float> octaves,constant NMGlowFinal& g) {
 if(g.flags.x<0.5)return c;
 constexpr sampler linear(coord::pixel,address::clamp_to_edge,filter::linear);
 float2 p=pixel/4;
 float3 n=clamp(octaves.sample(linear,p+1).rgb*g.blur.z,0.0,1.0);
 float3 f=clamp(octaves.sample(linear,p/2+float2(g.sizes.x+2,1)).rgb*g.blur.w,0.0,1.0);
 float3 glow=n+f-n*f;return glow+c*(1-glow);
}
float3 nmFinalFog(float3 c,float2 pixel,texture2d<float> fog,texture2d<float> masks,constant Uniforms& u) {
 if(u.visual.z<0.5)return c;
 float2 projected=(pixel-u.viewport.xy*0.5)/u.camera.z+u.camera.xy;
 float2 cameraOrigin=floor(u.camera.xy-u.viewport.xy/(2*u.camera.z));
 float checker=float(int(floor(pixel.x)+floor(pixel.y)+cameraOrigin.x+cameraOrigin.y)&1);
 return applyFog(c,projected,checker,fog,masks,u);
}
fragment float4 final_fragment(Screen in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> glow [[texture(1)]],texture2d<float> fog [[texture(2)]],texture2d<float> masks [[texture(3)]],depth2d<float> depth [[texture(4)]],texture2d<float> octaves [[texture(5)]],constant Uniforms& u [[buffer(3)]],constant NMFinal& f [[buffer(4)]],constant NMGlowFinal& g [[buffer(5)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);float3 c=color.sample(s,in.uv).rgb;
 if(f.bands>0){
  int2 p=int2(in.p.xy),size=int2(color.get_width(),color.get_height());float d=depth.read(uint2(p));
  float band=d<1?floor((1-d)*f.bands)+1:0;float3 acc=0;float k=0;
  const int2 o[4]={int2(1,0),int2(-1,0),int2(0,1),int2(0,-1)};
  for(int i=0;i<4;i++){int2 q=p+o[i];if(any(q<0)||any(q>=size))continue;float dq=depth.read(uint2(q));if(min(d,dq)>=1)continue;
   if((dq<1?floor((1-dq)*f.bands)+1:0)==band)continue;acc+=color.read(uint2(q)).rgb;k++;}
  if(k>0)c=mix(c,acc/k,k>1?.4:.25);
 }
 c=nmFinalGlow(c,in.p.xy,octaves,g);
 if(f.bloom>0.5)c+=glow.sample(s,in.uv).rgb*0.65;
 return float4(nmFinalGain(nmMapClip(nmFinalFog(c,in.p.xy,fog,masks,u),in.p.xy,u),f),1);
}
struct DistortionOut { float4 position [[position]]; float2 local; float4 shape [[flat]]; float4 params [[flat]]; };
vertex DistortionOut distortion_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const Distortion* sources [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(-1,-1),float2(1,-1),float2(-1,1),float2(-1,1),float2(1,-1),float2(1,1)};
 Distortion d=sources[iid];float3 world=mix(d.previous.xyz,d.current.xyz,u.timing.x);DistortionOut o;o.shape=d.shape;o.params=d.params;o.local=c[v];
 float2 halfSize=d.shape.w<0.5?float2(d.shape.x+d.shape.y):d.shape.xy;
 o.position=project(world,u);float2 pixels=c[v]*halfSize*u.camera.z;
 if(d.shape.w>=0.5)pixels.y+=(d.params.z-d.shape.y)*u.camera.z;
 o.position.xy+=float2(pixels.x/u.viewport.x*2,-pixels.y/u.viewport.y*2);return o;
}
// Displacement of one distortion source fragment, in world pixels.
inline float2 nmDistortionShift(DistortionOut in,constant Uniforms& u) {
 float2 shift=0;float clock=in.params.x+u.timing.x-1;
 if(in.shape.w<0.5){
  if(clock<0||clock>=15)discard_fragment();float radius=mix(12.0,in.shape.x,clock/15.0);float2 d=in.local*(in.shape.x+in.shape.y);float r=length(d);float band=(r-radius)/max(in.shape.y,0.001);if(abs(band)>=1)discard_fragment();
  float strength=min(clock,1.0)*pow(1-clock/15.0,2.0)*in.shape.z;
  if(r<0.001)discard_fragment();float envelope=1-band*band;shift=d/r*(band*envelope*envelope*3.5*strength);
 }else{
  // DESIGN_GPU_RENDERER §27: same authored drifting/widening plume profile.
  float rise=(1-in.local.y)*0.5;float t=(in.params.x+u.timing.x+in.params.y)*0.20943951;
  float center=sin(rise*5.0-t*0.5)*0.13*rise;
  float across=(in.local.x-center)/(0.58+0.42*rise);
  if(rise<=0||rise>=1||abs(across)>=1)discard_fragment();
  float edge=1-across*across;float sine=sin(rise*3.14159265);float envelope=edge*edge*sine*sine;
  float wave=sin(rise*18.0-t*2.0+across*2.5)+0.35*sin(rise*33.0-t*3.0-across*4.0);
  float strength=in.shape.z;
  if(in.params.w>0){float age=max(in.params.w+u.timing.x-1,0.0);strength=0.55*pow(max(1-age/300,0.0),2.0);}
  shift=float2(wave,0.24*sin(rise*22.0-t*2.0+across*3.0))*envelope*1.8*strength;
 }
 return shift;
}
fragment float4 distortion_fragment(DistortionOut in [[stage_in]],texture2d<float> source [[texture(0)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);float2 shift=nmDistortionShift(in,u);
 float2 uv=(in.position.xy+shift*u.camera.z)/u.viewport.xy;return float4(source.sample(s,clamp(uv,float2(0),float2(1))).rgb,1);
}

// Display-pixel HUD batch. It is composed after final world/fog, without depth.
struct OverlayQuad { float4 rect,uv,color,params; };
struct OverlayOut { float4 position [[position]]; float2 uv; float4 color; uint textureKind [[flat]]; };
vertex OverlayOut overlay_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const OverlayQuad* quads [[buffer(0)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 corners[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 OverlayQuad q=quads[iid];float2 pixels=q.rect.xy+corners[v]*q.rect.zw;OverlayOut out;out.position=float4(pixels.x/u.viewport.x*2-1,1-pixels.y/u.viewport.y*2,0,1);out.uv=mix(q.uv.xy,q.uv.zw,corners[v]);out.color=q.color;out.textureKind=uint(q.params.x);return out;
}
fragment float4 overlay_fragment(OverlayOut in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> terrain [[texture(1)]],texture2d<float> fog [[texture(2)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);
 // Fog stores grayscale world modulation with opaque alpha. Blend black by
 // inverse coverage over minimap terrain to reproduce that RGB modulation.
 if(in.textureKind==2){float alpha=(1-fog.sample(s,in.uv).r)*in.color.a;return float4(0,0,0,alpha);}
 float4 tex=in.textureKind==1?terrain.sample(s,in.uv):atlas.sample(s,in.uv);float alpha=tex.a*in.color.a;return float4(tex.rgb*in.color.rgb*alpha,alpha);
}

fragment float4 overlay_multiply_fragment(OverlayOut in [[stage_in]],texture2d<float> atlas [[texture(0)]],texture2d<float> terrain [[texture(1)]],texture2d<float> fog [[texture(2)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::nearest);
 float3 color=in.textureKind==1?terrain.sample(s,in.uv).rgb:in.textureKind==2?fog.sample(s,in.uv).rgb:atlas.sample(s,in.uv).rgb;
 // The factor is unpremultiplied. Fixed-function blending multiplies the
 // existing destination RGB while preserving its alpha (§13.3).
 return float4(color*in.color.rgb,1);
}

struct ModelCommitOut { float4 position [[position]];float2 atlasPixel;float opacity [[flat]]; };
vertex ModelCommitOut model_commit_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const ModelSlot* slots [[buffer(0)]],device const uint4* paint [[buffer(1)]],constant Uniforms& u [[buffer(3)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 ModelSlot slot=slots[paint[iid].x-1];float2 pixel=slot.screen.xy+c[v]*slot.screen.zw;ModelCommitOut o;o.position=float4(pixel.x/u.viewport.x*2-1,1-pixel.y/u.viewport.y*2,0,1);o.atlasPixel=slot.atlas.xy+c[v]*slot.atlas.zw;o.opacity=slot.params.x;return o;
}
fragment MRT model_commit_fragment(ModelCommitOut in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> emission [[texture(1)]],constant Uniforms& u [[buffer(3)]]) {
 constexpr sampler linear(coord::pixel,address::clamp_to_edge,filter::linear);float2 sample=u.composition.z>1.5?in.atlasPixel:floor(in.atlasPixel-.5)+.5;MRT out;out.color=color.sample(linear,sample)*in.opacity;out.glow=emission.sample(linear,sample)*in.opacity;return out;
}

struct GroundLightOut {float4 position [[position]];float2 projected;uint source [[flat]];};
vertex GroundLightOut ground_light_vertex(uint v [[vertex_id]],uint iid [[instance_id]],constant Uniforms& u [[buffer(3)]],device const nm_light_source* lights [[buffer(16)]]) {
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};nm_light_source light=lights[iid];float2 center=light.position_radius.xy-float2(0,light.position_radius.z*.5);float radius=light.position_radius.w;float2 pixel=center+(c[v]*2-1)*radius;
 GroundLightOut o;o.position=float4(pixel.x/u.viewport.x*2-1,1-pixel.y/u.viewport.y*2,0,1);o.projected=pixel;o.source=iid;return o;
}
fragment float4 ground_light_fragment(GroundLightOut in [[stage_in]],device const nm_light_source* lights [[buffer(16)]]) {return float4(1-exp(-nm_light_ground_energy(lights[in.source],in.projected)),1);}

kernel void select_subject_lights(device const ModelSlot* slots [[buffer(13)]],device const nm_light_source* lights [[buffer(16)]],device nm_light_subject* subjects [[buffer(17)]],constant Uniforms& u [[buffer(3)]],uint at [[thread_position_in_grid]]) {
 if(at>=uint(u.composition.w))return;ModelSlot s=slots[at];subjects[at]=nm_light_select(lights,uint(u.lightControl.x),s.screen.xy+s.screen.zw*.5,(s.screen.z+s.screen.w)*.5);
}
// Later sources replace earlier ones from the same unresolved world, as the
// immutable snapshot did; fog keeps the pixel's own undistorted position.
fragment float4 distortion_final_fragment(DistortionOut in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> glow [[texture(1)]],texture2d<float> fog [[texture(2)]],texture2d<float> masks [[texture(3)]],texture2d<float> octaves [[texture(5)]],constant Uniforms& u [[buffer(3)]],constant NMFinal& f [[buffer(4)]],constant NMGlowFinal& g [[buffer(5)]]) {
 constexpr sampler s(coord::normalized,address::clamp_to_edge,filter::linear);
 float2 shift=nmDistortionShift(in,u);
 float2 uv=clamp((in.position.xy+shift*u.camera.z)/u.viewport.xy,float2(0),float2(1));
 float3 c=nmFinalGlow(color.sample(s,uv).rgb,uv*u.viewport.xy,octaves,g);if(f.bloom>0.5)c+=glow.sample(s,uv).rgb*0.65;
 return float4(nmFinalGain(nmMapClip(nmFinalFog(c,in.position.xy,fog,masks,u),in.position.xy,u),f),1);
}
