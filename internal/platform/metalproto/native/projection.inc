// CPU metadata only: no posed vertices or device readback.
typedef struct {const void* records;const uint32_t* flags;uint32_t recordCount,pieceCount;float view[4];} NMProjectionUpload;
_Static_assert(sizeof(NMProjectionUpload)==40,"projection upload layout");
static BOOL nmProjectionPrepare(NMRenderer* r,NMRing* ring,const NMProjectionUpload* p,uint32_t instances,uint32_t pieces,uint64_t flagGeneration){
 if(p&&(p->recordCount!=instances||p->pieceCount!=pieces||(instances&&!p->records)||(pieces&&!p->flags))){r.lastError=@"Projection metadata span mismatch";return NO;}
 NSUInteger rb=MAX((NSUInteger)instances*48,48),fb=MAX((NSUInteger)pieces*4,4);
 if(rb>r.device.maxBufferLength||fb>r.device.maxBufferLength){r.lastError=@"Projection metadata exceeds device limit";return NO;}
 ring.projections=grow(r,ring.projections,rb);ring.projectionFlags=nmResidentBuffer(r,ring,NMResidentProjectionFlags,p?flagGeneration:0,p?(const void*)p->flags:NULL,(NSUInteger)pieces*4,NMUpProjection);if(!ring.projections||!ring.projectionFlags){r.lastError=@"Projection metadata allocation failed";return NO;}
 if(p){if(instances)NMCOPY(NMUpProjection,ring.projections.contents,p->records,(NSUInteger)instances*48);ring.projectionView=(vector_float4){p->view[0],p->view[1],p->view[2],p->view[3]};}
 else{memset(ring.projections.contents,0,rb);ring.projectionView=(vector_float4){0,0,0,0};}return YES;
}
static void nmProjectionBind(NMRing* ring,id<MTLRenderCommandEncoder> e,NSUInteger start){if(!ring.projections)return;[e setVertexBuffer:ring.projections offset:start*48 atIndex:27];[e setVertexBuffer:ring.projectionFlags offset:0 atIndex:28];vector_float4 view=ring.projectionView;[e setVertexBytes:&view length:16 atIndex:29];}
