// Uses nm_face_Prepared, declared by faces.metal. One invocation owns the
// ordered prefix: caps depend on every admitted face before this face (§26.6).
struct nm_fr_Record {uint instance,faceBase,faceCount,reserved;};
kernel void nm_fr_budget(device const nm_fr_Record* records [[buffer(0)]],device const float4* objects [[buffer(18)]],device const nm_face_Prepared* faces [[buffer(21)]],device uint* admission [[buffer(25)]],device uint4* budget [[buffer(26)]],constant uint4& counts [[buffer(27)]],uint at [[thread_position_in_grid]]) {
 if(at)return;
 // nm_fr_sum admitted every eligible face and summed it. Every face fits when
 // the whole sum plus the largest reservation excess fits: a face's prefix
 // never exceeds the sum minus its own cost. Otherwise redo the ordered walk.
 uint4 sums=budget[1];
 if(sums.x+sums.z<=28672u){budget[0]=uint4(sums.x,sums.y,0,sums.y);return;}
 uint used=0,admitted=0,suppressed=0,eligible=0;
 for(uint i=0;i<counts.x;i++){
  nm_fr_Record r=records[i];if(r.instance>=counts.y||objects[r.instance].x<=.5)continue;
  for(uint j=0;j<r.faceCount;j++){
   uint index=r.faceBase+j;nm_face_Prepared f=faces[index];uint n=f.control.w,flags=f.control.z;
   if((flags&1u)==0||(flags&16u)==0||n<3||n>256)continue;
   eligible++;uint fan=3*(n-2),reserve=max(n,fan);
   // Production checks the conservative reservation before appending. A
   // mapped quad then stores its four corners; an unmapped face stores its
   // expanded triangle fan. Equality at the cap is admitted, not rejected.
   if(reserve>28672-used){admission[index]=0;suppressed++;continue;}
   admission[index]=1;used+=(flags&2u)!=0?n:fan;admitted++;
  }
 }
 budget[0]=uint4(used,admitted,suppressed,eligible);
}
// Billboard/beam sources follow models and may consume the reserved tail.
inline bool nm_fr_stock_admitted(uint quad,device const uint4* budget){return quad<(32768u-budget[0].x)/4u;}

// Parallel pre-pass for nm_fr_budget: one threadgroup per order record admits
// every eligible face and accumulates cost, count and reservation excess.
kernel void nm_fr_sum(device const nm_fr_Record* records [[buffer(0)]],device const float4* objects [[buffer(18)]],device const nm_face_Prepared* faces [[buffer(21)]],device uint* admission [[buffer(25)]],device atomic_uint* sums [[buffer(26)]],constant uint4& counts [[buffer(27)]],uint group [[threadgroup_position_in_grid]],uint lane [[thread_index_in_threadgroup]],uint width [[threads_per_threadgroup]]) {
 if(group>=counts.x)return;
 nm_fr_Record r=records[group];
 if(r.instance>=counts.y||objects[r.instance].x<=.5)return;
 for(uint j=lane;j<r.faceCount;j+=width){
  uint index=r.faceBase+j;nm_face_Prepared f=faces[index];uint n=f.control.w,flags=f.control.z;
  if((flags&1u)==0||(flags&16u)==0||n<3||n>256)continue;
  uint fan=3*(n-2),reserve=max(n,fan),cost=(flags&2u)!=0?n:fan;
  admission[index]=1;
  atomic_fetch_add_explicit(&sums[0],cost,memory_order_relaxed);
  atomic_fetch_add_explicit(&sums[1],1u,memory_order_relaxed);
  atomic_fetch_max_explicit(&sums[2],reserve-cost,memory_order_relaxed);
 }
}
