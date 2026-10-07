// Concatenate after the retained base and group shaders. Every resolve thread
// owns one native record-pixel block, including fractional output transforms.
// Its complete ordered outline key/colour decision is local; no overlapping
// writers, texture feedback between threads, or host posed-corner stream.
struct nm_or_Ring {uint start,count,piece,cornerOffset;};
struct nm_or_Corner {int4 xyKey;};
struct nm_or_Prepared {int4 bounds;uint4 chain;};
struct nm_or_Control {uint4 span;int4 domain;};
kernel void nm_or_prepare(device const Vertex* vertices [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],device const nm_or_Ring* rings [[buffer(4)]],device const ModelRules* rules [[buffer(10)]],device nm_or_Corner* out [[buffer(21)]],device nm_or_Prepared* prepared [[buffer(22)]],device atomic_uint* diagnostics [[buffer(24)]],constant nm_or_Control& c [[buffer(23)]],device const nm_projection_Record* projections [[buffer(27)]],uint ordinal [[thread_position_in_grid]]) {
 if(ordinal>=c.span.y)return;uint iid=c.span.x;nm_or_Ring ring=rings[ordinal];nm_projection_Record p=projections[iid];float4x4 m=poses[offsets[iid]+ring.piece];
 int2 mn=int2(2147483647),mx=int2(-2147483647);uint top=0,bottom=0;bool fits=true;
 for(uint j=0;j<ring.count;j++){
  float3 w=(m*float4(float3(vertices[ring.start+j].p),1)).xyz;float3 local=w-p.origin.xyz;int y=int(floor(local.y));
  float2 xy=p.control.x*float2(floor(local.x),floor(local.z)-float(y>>1))+p.anchors.xy;
  bool valid=m[3].w>.5&&all(isfinite(xy))&&all(xy>=-8192)&&all(xy<=24575);
  int key=int(floor(local.y))+int(rules[iid].clip.w);
  int2 pos=valid?int2(xy):int2(0);if(j==0){mn=mx=pos;}else{if(pos.y<mn.y)top=j;if(pos.y>mx.y)bottom=j;mn=min(mn,pos);mx=max(mx,pos);}
  fits=fits&&valid;out[c.span.z+ring.cornerOffset+j].xyKey=int4(pos,key,valid?1:0);
 }
 bool supported=fits&&all(mx-mn<32768);prepared[c.span.w+ordinal]={int4(mn,mx),uint4(top,bottom,supported?1u:0u,0)};
 if(!supported&&m[3].w>.5)atomic_fetch_add_explicit(diagnostics,1u,memory_order_relaxed);
}
// Half-open chain ownership and later folded-edge override [03 R-RAST-01 §1].
bool nm_or_chain(device const nm_or_Corner* corners,nm_or_Ring ring,uint base,uint top,uint bottom,int step,int row,thread int& x,thread int& key) {
 #pragma clang fp contract(off)
 uint cur=top;bool found=false;
 for(uint k=0;k<=ring.count;k++){
  uint next=step<0?(cur?cur-1:ring.count-1):(cur+1==ring.count?0:cur+1);
  int4 a=corners[base+ring.cornerOffset+cur].xyKey,b=corners[base+ring.cornerOffset+next].xyKey;
  if(b.y>a.y&&row>=a.y&&row<b.y){
   int dy=b.y-a.y,dx=b.x-a.x,m=row-a.y;
   // Biased coordinates and strict slope bound keep all signed 16.16
   // products/sums in int32; arbitrary out-of-domain rings are diagnosed.
   int slope=(dx*65536)/dy;
   int value=(a.x+8192)*65536+65535+slope*m;
   x=(value>>16)-8192;
   float q=precise::divide(float(m),float(dy));float delta=float(b.z)-float(a.z);float product=delta*q;
   // TODO(retained-outline-admission): production's <=4-corner optimized
   // path truncates only after outlineEdgeExact proves its integer rational
   // matches the float walk on every owned row. Until that admission is
   // mirrored here, retain the CPU-walk colour shader's floor narrowing;
   // negative fractional optimized-ring keys can differ by one byte.
   key=int(floor(float(a.z)+product));found=true;
  }
  if(next==bottom)break;cur=next;
 }
 return found;
}
kernel void nm_or_resolve(device const nm_or_Ring* rings [[buffer(4)]],constant Uniforms& u [[buffer(3)]],device const ModelRules* rules [[buffer(10)]],device const ModelSlot* slots [[buffer(13)]],device const uint* selectors [[buffer(14)]],device const int4* groups [[buffer(20)]],device const nm_or_Corner* corners [[buffer(21)]],device const nm_or_Prepared* prepared [[buffer(22)]],constant nm_or_Control& c [[buffer(23)]],device const nm_projection_Record* projections [[buffer(27)]],constant float4& view [[buffer(29)]],texture2d<float,access::read_write> colors [[texture(0)]],texture2d<float,access::read_write> emissions [[texture(1)]],texture2d<float,access::read_write> metadata [[texture(2)]],texture2d<float> palette [[texture(4)]],uint2 at [[thread_position_in_grid]]) {
 int2 pixel=c.domain.xy+int2(at);if(any(pixel>=c.domain.zw))return;uint iid=c.span.x;uint selector=selectors[iid];if(!selector)return;
 ModelSlot slot=slots[selector-1];nm_projection_Record projection=projections[iid];float scale=projection.control.z;
 float2 lo=slot.atlas.xy+(float2(pixel)*scale+view.zw-slot.screen.xy)*slot.atlas.zw/slot.screen.zw;
 float2 hi=slot.atlas.xy+(float2(pixel+1)*scale+view.zw-slot.screen.xy)*slot.atlas.zw/slot.screen.zw;
 // Adjacent transformed record blocks share these exact integer boundaries;
 // clipping can produce empty blocks, but never overlapping ownership.
 int2 begin=int2(ceil(lo-.5)),end=int2(ceil(hi-.5));int2 boxLo=int2(slot.atlas.xy),boxHi=int2(slot.atlas.xy+slot.atlas.zw);
 begin=max(begin,boxLo);end=min(end,boxHi);if(any(begin>=end))return;
 float4 body=metadata.read(uint2(begin));float winner=body.y,outlineMaximum=0;bool accepted=false,hasEndpoint=false;int ownKey=0;
 // First pass is the complete maximum outline key plane, including endpoints
 // whose colour index later erases; second pass preserves ordered colour ties.
 for(uint pass=0;pass<2;pass++)for(uint r=0;r<c.span.y;r++){
  nm_or_Ring ring=rings[r];if(ring.count<3)continue;nm_or_Prepared f=prepared[c.span.w+r];uint top=f.chain.x,bottom=f.chain.y;
  if(!f.chain.z||pixel.y<f.bounds.y||pixel.y>=f.bounds.w||pixel.x<f.bounds.x||pixel.x>f.bounds.z)continue;
  int lx=0,rx=0,lk=0,rk=0;
  if(!nm_or_chain(corners,ring,c.span.z,top,bottom,-1,pixel.y,lx,lk)||!nm_or_chain(corners,ring,c.span.z,top,bottom,1,pixel.y,rx,rk)||rx<=lx)continue;
  int key;if(pixel.x==lx)key=lk;else if(pixel.x==rx)key=rk;else continue;
  key=((key%256)+256)%256;
  if(pass==0){hasEndpoint=true;outlineMaximum=max(outlineMaximum,float(key));winner=max(winner,float(key));}else if(float(key)>=winner){accepted=true;ownKey=key;}
 }
 if(!hasEndpoint)return;
 int4 member=groups[iid];ModelRules rule=nm_group_source_rule(rules[iid],member);
 uint index=uint(rule.shadow.w);bool blue=member.x>0?nm_group_blue(float(ownKey),member,rules):(rule.clip.x>1.5&&float(ownKey)<=rule.clip.y);
 bool erased=(rule.clip.x>.5&&rule.clip.x<1.5&&float(ownKey)<=rule.clip.y)||(rule.clip.z>.5&&float(ownKey)<=rule.clip.w)||index==1;
 float4 color=blue?palette.read(uint2(index,1)):palette.read(uint2(index,0));erased=erased||color.a<.5;color.a=1;
 // The live outline stage replaces erased winners with transparent colour
 // after preserving their key; it must not expose earlier finished art
 // [03 R-COMP-01 §3], modelDirectColourShaderSource live seed.
 for(int y=begin.y;y<end.y;y++)for(int x=begin.x;x<end.x;x++){
  uint2 p=uint2(x,y);float4 prior=metadata.read(p);prior.y=max(prior.y,outlineMaximum);if(accepted)prior.z=blue?1:0;prior.w=1;metadata.write(prior,p);
  if(accepted){colors.write(erased?float4(0):color,p);emissions.write(float4(0),p);}
 }
}
