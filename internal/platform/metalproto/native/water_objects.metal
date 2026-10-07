// Concatenate after the shared water field and retained base shader types.
// Enhanced presentation arithmetic from water_reflections.go/underwater.go,
// DESIGN_GPU_RENDERER §26.5–26.6/§32. No simulation histories live here.
struct nm_wo_Object {float4 params;};
struct nm_wo_Reflection {float4 position [[position]];float2 source;float height,key;float2 lo [[flat]],hi [[flat]];float keyed [[flat]];uint faceIndex [[flat]];float4 finalTransform [[flat]],finalBounds [[flat]];int2 group [[flat]];};
struct nm_wo_Source {float4 color [[color(0)]],soft [[color(1)]];};
float2 nm_wo_world(float2 pixel,constant Uniforms& u){return (pixel-u.viewport.xy*.5)/u.camera.z+u.camera.xy;}
vertex nm_wo_Reflection nm_wo_reflection_vertex(uint vid [[vertex_id]],device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant Uniforms& u [[buffer(3)]],device const ModelVisual* visuals [[buffer(5)]],device const ModelRules* rules [[buffer(10)]],device const ModelSlot* slots [[buffer(13)]],device const uint* slotSelectors [[buffer(14)]],device const nm_wo_Object* objects [[buffer(18)]],constant WaterUniform& water [[buffer(9)]],constant float4& control [[buffer(19)]],device const Material* materials [[buffer(6)]],device const int4* groups [[buffer(20)]],device const nm_face_Prepared* faces [[buffer(21)]],device const uint* faceBases [[buffer(22)]],device const uint* admission [[buffer(25)]],device const nm_projection_Record* projection [[buffer(27)]],device const uint* projectionFlags [[buffer(28)]],constant float4& projectionView [[buffer(29)]]){
 nm_wo_Reflection o={};o.position=float4(2,2,2,1);
 Vertex v=vertices[vid];float4x4 m=poses[offsets[0]+v.piece];uint selector=slotSelectors[0];if(m[3].w<.5||selector==0)return o;
 if(v.material==0||faceBases[0]==0)return o;o.faceIndex=faceBases[0]-1+materials[v.material].reserved;
 if(admission[o.faceIndex]==0)return o;nm_face_Prepared face=faces[o.faceIndex];
 ModelSlot slot=slots[selector-1];float3 w=(m*float4(float3(v.p),1)).xyz;float sea=objects[0].params.y;
 float4 original=project(w,u);float2 source=float2((original.x+1)*.5,(1-original.y)*.5)*u.viewport.xy;
 source=nm_projection_screen(w,projection[0],projectionFlags[offsets[0]+v.piece],projectionView,source);float2 corner=floor(source*face.pixel.xy+face.pixel.zw);o.source=slot.atlas.xy+corner+select(float2(0),float2(1),corner>face.centroid.xy);o.lo=slot.atlas.xy;o.hi=o.lo+slot.atlas.zw;
 uint finalSelector=uint(objects[0].params.w);int4 group=groups[0];o.group=int2(finalSelector!=selector&&group.x>0&&group.y!=2,group.z);
 ModelSlot finalSlot=slots[(finalSelector?finalSelector:selector)-1];float2 scale=finalSlot.atlas.zw/finalSlot.screen.zw*slot.screen.zw/slot.atlas.zw;
 o.finalTransform=float4(scale,finalSlot.atlas.xy+(slot.screen.xy-finalSlot.screen.xy)*finalSlot.atlas.zw/finalSlot.screen.zw-slot.atlas.xy*scale);o.finalBounds=float4(finalSlot.atlas.xy,finalSlot.atlas.xy+finalSlot.atlas.zw);
 float origin=m[2].w>.5?m[1].w:visuals[0].bounds.w;
 o.height=w.y-sea;o.key=floor(w.y-origin)+rules[0].clip.w;o.keyed=1;
 float heightScale=u.mode.x>.5?.5:1.0;float2 screen=slot.screen.xy+(o.source-slot.atlas.xy)*slot.screen.zw/slot.atlas.zw;screen.y+=2*o.height*heightScale*u.camera.z;
 float amount=clamp((o.height-64.0)/96.0,0.0,1.0);amount=max(.35,amount*amount*(3-2*amount));
 float2 world=nm_wo_world(screen,u);float time=water.phase.x*water.controls.x;
 float wave=sin(world.y*.65+time*1.7)+.35*sin(world.x*.11-world.y*.31-time*1.1);
 screen.x+=wave*3*amount*control.y;
 o.position=float4(screen.x/u.viewport.x*2-1,1-screen.y/u.viewport.y*2,0,1);return o;
}
fragment nm_wo_Source nm_wo_reflection_source(nm_wo_Reflection in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> metadata [[texture(1)]],constant Uniforms& u [[buffer(3)]],constant WaterUniform& water [[buffer(9)]],device const nm_face_Prepared* faces [[buffer(21)]]){
 nm_wo_Source out={};if(in.height<=0||any(in.source<in.lo)||any(in.source>=in.hi))discard_fragment();
 float2 p=floor(in.source);float4 stored=metadata.read(uint2(p));if(stored.w<.5)discard_fragment();
 if(in.keyed>.5){
  nm_face_Prepared face=faces[in.faceIndex];float key;
  if(nm_face_mapped(face))key=nm_face_key(nm_face_lanes(face,p+.5-in.lo));
  else {
   float2 a=dfdx(in.source),b=dfdy(in.source);float det=a.x*b.y-a.y*b.x;
   if(abs(det)<1e-8)discard_fragment();float ka=dfdx(in.key),kb=dfdy(in.key);
   float2 gradient=float2(ka*b.y-kb*a.y,a.x*kb-b.x*ka)/det;
   key=floor(in.key+dot(gradient,p+.5-in.source));key-=floor(key/256)*256;
  }
  if(key<stored.y)discard_fragment();
  if(in.group.x){float2 finalPixel=(p+.5)*in.finalTransform.xy+in.finalTransform.zw;if(any(finalPixel<in.finalBounds.xy)||any(finalPixel>=in.finalBounds.zw))discard_fragment();float groupKey=metadata.read(uint2(floor(finalPixel))).y;if(clamp(key+in.group.y,0.0,255.0)<groupKey)discard_fragment();}
 }

 float4 c=color.read(uint2(p));float distance=smoothstep(64.0,160.0,in.height);float time=water.phase.x*water.controls.x;
 float2 world=nm_wo_world(in.position.xy,u);
 float band=.5+.5*sin(world.y*.9+sin(world.x*.075-time*.7)+time*1.7);
 c*=(1-.65*distance)*(1-.3*distance*band)*(1-smoothstep(64.0,320.0,in.height));
 out.color=c;out.soft=float4(smoothstep(64.0,200.0,in.height)*c.a,0,0,c.a);return out;
}
struct nm_wo_Screen {float4 position [[position]];};
vertex nm_wo_Screen nm_wo_screen_vertex(uint v [[vertex_id]]){const float2 c[3]={float2(-1,-1),float2(3,-1),float2(-1,3)};nm_wo_Screen o;o.position=float4(c[v],0,1);return o;}
float4 nm_wo_texel(texture2d<float> source,float2 p){int2 at=int2(floor(p));if(any(at<0)||at.x>=int(source.get_width())||at.y>=int(source.get_height()))return float4(0);return source.read(uint2(at));}
float4 nm_wo_weighted(texture2d<float> source,texture2d<float> soft,float2 p,float low,float high){float4 c=nm_wo_texel(source,p),h=nm_wo_texel(soft,p);float strength=h.a>0?clamp(h.r/h.a,0.0,1.0):0;return c*mix(low,high,strength);}
float4 nm_wo_sample(texture2d<float> source,texture2d<float> soft,float2 p,float low,float high){float2 a=floor(p-.5)+.5,f=fract(p-.5);return mix(mix(nm_wo_weighted(source,soft,a,low,high),nm_wo_weighted(source,soft,a+float2(1,0),low,high),f.x),mix(nm_wo_weighted(source,soft,a+float2(0,1),low,high),nm_wo_weighted(source,soft,a+1,low,high),f.x),f.y);}
fragment float4 nm_wo_reflection_resolve(nm_wo_Screen in [[stage_in]],texture2d<float> source [[texture(0)]],texture2d<float> soft [[texture(1)]],texture2d<float> mask [[texture(3)]],constant Uniforms& u [[buffer(3)]],constant WaterUniform& water [[buffer(9)]],constant float4& control [[buffer(19)]],device const uint* tileFlags [[buffer(23)]]){
 float2 world=nm_wo_world(in.position.xy,u);float coverage=smoothstep(.8,1.0,nmWaterMask(mask,world/water.mask.x).r);if(coverage<=0)return float4(0);
 float t=water.phase.x*water.controls.x;float ripple=sin(world.y*.19+t*1.9+sin(world.x*.07-t*.6))*.65+sin(world.y*.37-t*1.3)*.35;
 float2 q=in.position.xy+float2(ripple*(.65+water.phase.w*.65)*control.y,0);
 uint bits=nm_water_tiles_resolve_flags(tileFlags,source,q,control);if(!(bits&1))return float4(0);
 if(!(bits&2)){float4 c=nm_water_tiles_color_sample(source,q,.5)+nm_water_tiles_color_sample(source,q+float2(control.y,0),.25)+nm_water_tiles_color_sample(source,q-float2(control.y,0),.25);c.rgb*=float3(.76,.88,.94);return c*(.25*coverage);}
 float4 c=nm_wo_sample(source,soft,q,.5,0)+nm_wo_sample(source,soft,q+float2(control.y,0),.25,0)+nm_wo_sample(source,soft,q-float2(control.y,0),.25,0);
 c+=nm_wo_weighted(source,soft,floor(q)+.5,0,.25);
 const float horizontal[4]={.12,.09,.06,.03};for(uint i=0;i<4;i++){float2 d=float2((i+1)*control.z,0);c+=nm_wo_weighted(source,soft,floor(q+d)+.5,0,horizontal[i])+nm_wo_weighted(source,soft,floor(q-d)+.5,0,horizontal[i]);}
 const float vertical[2]={.06,.015};for(uint i=0;i<2;i++){float2 d=float2(0,(i+1)*control.z);c+=nm_wo_weighted(source,soft,floor(q+d)+.5,0,vertical[i])+nm_wo_weighted(source,soft,floor(q-d)+.5,0,vertical[i]);}
 c.rgb*=float3(.76,.88,.94);return c*(.25*coverage);
}
struct nm_wo_Commit {float4 position [[position]];float2 source;float4 screen [[flat]],atlas [[flat]];float opacity [[flat]],underwater [[flat]];};
vertex nm_wo_Commit nm_wo_commit_vertex(uint v [[vertex_id]],uint iid [[instance_id]],device const ModelSlot* slots [[buffer(0)]],device const uint4* paint [[buffer(1)]],constant Uniforms& u [[buffer(3)]],device const float4* flags [[buffer(18)]]){
 const float2 c[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};uint index=paint[iid].x-1;ModelSlot slot=slots[index];float pad=flags[index].x>.5?ceil(1.32*u.camera.z)+1:0;
 float2 pixel=slot.screen.xy-pad+c[v]*(slot.screen.zw+2*pad);nm_wo_Commit o;o.position=float4(pixel.x/u.viewport.x*2-1,1-pixel.y/u.viewport.y*2,0,1);o.source=slot.atlas.xy+(pixel-slot.screen.xy)*slot.atlas.zw/slot.screen.zw;o.screen=slot.screen;o.atlas=slot.atlas;o.opacity=slot.params.x;o.underwater=flags[index].x;return o;
}
float4 nm_wo_body(texture2d<float> color,texture2d<float> metadata,float2 p,float4 rect,bool submerged){if(any(p<rect.xy)||any(p>=rect.xy+rect.zw))return float4(0);float4 m=metadata.read(uint2(floor(p)));if(m.w<.5||(m.z>.5)!=submerged)return float4(0);return color.read(uint2(floor(p)));}
fragment MRT nm_wo_commit_fragment(nm_wo_Commit in [[stage_in]],texture2d<float> color [[texture(0)]],texture2d<float> emission [[texture(1)]],texture2d<float> metadata [[texture(2)]],texture2d<float> mask [[texture(3)]],constant Uniforms& u [[buffer(3)]],constant WaterUniform& water [[buffer(9)]]){
 constexpr sampler linear(coord::pixel,address::clamp_to_edge,filter::linear);
 constexpr sampler nearest(coord::pixel,address::clamp_to_edge,filter::nearest);MRT out={};
 // The body page always has two texels per screen pixel. Supersample selects
 // its resolve only: off uses the block's top-left texel at four times weight,
 // while submerged samples keep the two-texel box (underwater.go, §26.5).
 bool point=u.composition.z<1.5;float2 start=floor(in.source-.5),resolve=point?start+.5:in.source;
 bool inside=all(in.source>=in.atlas.xy)&&all(in.source<in.atlas.xy+in.atlas.zw);
 if(inside)out.glow=(point?emission.sample(nearest,resolve):emission.sample(linear,resolve))*in.opacity;
 if(in.underwater<.5||water.controls.x<.5||water.mask.w<.5){if(inside)out.color=(point?color.sample(nearest,resolve):color.sample(linear,resolve))*in.opacity;return out;}
 float ratio=in.atlas.z/in.screen.z;float4 above=0;
 for(uint j=0;j<2;j++)for(uint i=0;i<2;i++){
  if(point&&i+j>0)continue;
  above+=nm_wo_body(color,metadata,start+float2(i,j)+.5,in.atlas,false);
 }
 if(point)above*=4.0;
 above/=4.0;
 float2 world=nm_wo_world(in.position.xy,u);float4 wet=nmWaterMask(mask,world/water.mask.x);float coverage=smoothstep(.8,1.0,wet.r)*smoothstep(0.0,nmEdgeFadePixels/32.0,wet.g),deep=smoothstep(.05,.55,wet.g);
 float3 field=nmWaterField(world/nmPatternSize,water.phase.yz*nmCurrentScale,water.phase.x);
 float texelsPerWorld=u.camera.z*ratio;float2 off=nmWaterOffset(field)*deep*coverage*texelsPerWorld*.5;
 float2 corner=in.source+off-1.0,base=floor(corner),f=corner-base;float4 sub=0;
 float wx[3]={1.0-f.x,1.0,f.x},wy[3]={1.0-f.y,1.0,f.y};
 for(uint j=0;j<3;j++)for(uint i=0;i<3;i++){
  float2 at=base+float2(i,j);
  float weight=wx[i]*wy[j];sub+=nm_wo_body(color,metadata,at+.5,in.atlas,true)*weight;
 }
 float cover=sub.a/4.0;float3 submerged=sub.rgb/max(sub.a,1e-4);float3 surface=submerged;
 if(water.controls.z>.5)surface=mix(submerged,mix(submerged,nmWaterShade(submerged,field,deep,1.0),coverage),nmSurfaceOpacity);
 float fs=(1-above.a)*cover;out.color=float4(above.rgb+surface*fs,above.a+fs)*in.opacity;return out;
}
// The native host supplies the viewport photograph's painted-map rectangle.
// Sampling the already drawn seabed keeps the existing shared water treatment
// rather than tinting its sprite again after the surface (§32.3).
fragment float4 nm_wo_surface_fragment(nm_wo_Screen in [[stage_in]],texture2d<float> painted [[texture(0)]],texture2d<float> mask [[texture(3)]],texture2d<float> shadows [[texture(2)]],texture2d<float> groundField [[texture(5)]],constant Uniforms& u [[buffer(3)]],constant WaterUniform& water [[buffer(9)]]){
 constexpr sampler linear(coord::normalized,address::clamp_to_edge,filter::linear);float2 uv=in.position.xy/u.viewport.xy;
 float4 color=nmWaterColor(painted,mask,uv,nm_wo_world(in.position.xy,u),water);
 if(u.lightControl.y>.5)color=nm_light_ground_resolve(color,groundField.sample(linear,uv).rgb);
 if(u.visual.x>.5)color.rgb*=1-shadows.sample(linear,uv).r;
 return color;
}

struct nm_wo_StockVertex {float4 positionHeight,uv,color,bounds;};
struct nm_wo_StockOut {float4 position [[position]];float2 uv;float4 color [[flat]],bounds [[flat]];float height;float kind [[flat]];};
vertex nm_wo_StockOut nm_wo_stock_vertex(uint vid [[vertex_id]],uint iid [[instance_id]],device const nm_wo_StockVertex* vertices [[buffer(0)]],constant Uniforms& u [[buffer(3)]],device const uint4* budget [[buffer(26)]]){
 const uint fan[6]={0,1,2,0,2,3};nm_wo_StockVertex v=vertices[iid*4+fan[vid]];nm_wo_StockOut o;if(!nm_fr_stock_admitted(iid,budget)){o={};o.position=float4(2,2,2,1);return o;}o.position=float4(v.positionHeight.x/u.viewport.x*2-1,1-v.positionHeight.y/u.viewport.y*2,0,1);o.uv=v.uv.xy;o.height=v.positionHeight.z;o.kind=v.positionHeight.w;o.color=v.color;o.bounds=v.bounds;return o;
}
fragment nm_wo_Source nm_wo_stock_source(nm_wo_StockOut in [[stage_in]],texture2d<float> atlas [[texture(0)]]){
 // Surface impacts/billboards admit height zero; interpolated beam strokes
 // drop the part at or below sea. Neither source has model wave/blur ramps.
 bool sprite=in.kind<1.5;if(sprite?in.height<0:in.height<=0)discard_fragment();
 float4 c=in.color;
 if(sprite){if(any(in.uv<in.bounds.xy)||any(in.uv>=in.bounds.zw))discard_fragment();constexpr sampler nearest(coord::pixel,address::clamp_to_edge,filter::nearest);float4 tex=atlas.sample(nearest,in.uv);if(tex.a<.5)discard_fragment();c=float4(tex.rgb,1);}
 c*=1-smoothstep(64.0,160.0,in.height);nm_wo_Source out;out.color=c;out.soft=float4(0,0,0,c.a);return out;
}
