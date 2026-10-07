// Uses nm_face_Prepared, declared by faces.metal. One invocation owns the
// ordered prefix: caps depend on every admitted face before this face (§26.6).
struct nm_fr_Record {uint instance,faceBase,faceCount,reserved;};
kernel void nm_fr_budget(device const nm_fr_Record* records [[buffer(0)]],device const float4* objects [[buffer(18)]],device const nm_face_Prepared* faces [[buffer(21)]],device uint* admission [[buffer(25)]],device uint4* budget [[buffer(26)]],constant uint4& counts [[buffer(27)]],uint at [[thread_position_in_grid]]) {
 if(at)return;
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
   if(reserve>28672-used){suppressed++;continue;}
   admission[index]=1;used+=(flags&2u)!=0?n:fan;admitted++;
  }
 }
 budget[0]=uint4(used,admitted,suppressed,eligible);
}
// Billboard/beam sources follow models and may consume the reserved tail.
inline bool nm_fr_stock_admitted(uint quad,device const uint4* budget){return quad<(32768u-budget[0].x)/4u;}
