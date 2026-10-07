// Include after faces.inc and grow. Root adds NMRenderer.faceReflections and
// NMRing.faceReflections properties, with the types below.
_Static_assert(sizeof(NMFaceReflectionRecord)==16,"reflection face order layout");
@interface NMFaceReflectionState : NSObject
@property id<MTLComputePipelineState> budget;
@end
@implementation NMFaceReflectionState
@end
@interface NMFaceReflectionFrame : NSObject
@property id<MTLBuffer> records,admission,budget;
@property NSUInteger recordCount,faceCount;
@property BOOL encoded;
@end
@implementation NMFaceReflectionFrame
@end
static BOOL nmFaceReflectionsInitialize(NMRenderer* r,id<MTLLibrary> lib,NSError** error){NMFaceReflectionState* s=[NMFaceReflectionState new];s.budget=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"nm_fr_budget"] error:error];if(!s.budget)return NO;r.faceReflections=s;return YES;}
static BOOL nmFaceReflectionsPrepare(NMRenderer* r,NMRing* ring){
 if(!r.faceReflections)return nmFacesFail(r,@"Reflection face budget pipeline is not initialized");
 NMFaceRing* faces=ring.faceRing;NSUInteger count=ring.compositionDrawCount;
 if((count&&(!faces||ring.compositionDraws.length<count*sizeof(NMCloakDraw)))||count>UINT32_MAX)return nmFacesFail(r,@"Invalid ordered reflection face draws");
 NMFaceReflectionFrame* f=ring.faceReflections;if(!f){f=[NMFaceReflectionFrame new];ring.faceReflections=f;}f.recordCount=0;f.faceCount=faces.faceCount;f.encoded=NO;
 if(count>r.device.maxBufferLength/16||f.faceCount>r.device.maxBufferLength/4)return nmFacesFail(r,@"Reflection face budget buffer limit");
 f.records=grow(r,f.records,MAX(count*16,16));f.admission=grow(r,f.admission,MAX(f.faceCount*4,4));f.budget=grow(r,f.budget,16);if(!f.records||!f.admission||!f.budget)return nmFacesFail(r,@"Reflection face budget allocation failed");
 const NMCloakDraw* draws=ring.compositionDraws.bytes;const uint32_t* bases=faces.bases.contents;NMFaceReflectionRecord* records=f.records.contents;
 for(NSUInteger i=0;i<count;i++){NMCloakDraw d=draws[i];if(d.mesh>=r.meshes.count||d.instance>=faces.instanceCount)return nmFacesFail(r,@"Reflection face order exceeds retained subject span");NMFaceMesh* mesh=r.meshes[d.mesh].faceMesh;uint32_t base=bases[d.instance];if(!base||!mesh.faceCount)continue;
  if((uint64_t)base-1+mesh.faceCount>faces.faceCount)return nmFacesFail(r,@"Reflection face order exceeds prepared face span");
  records[f.recordCount++]=(NMFaceReflectionRecord){d.instance,base-1,(uint32_t)mesh.faceCount,0};
 }
 return YES;
}
static BOOL nmFaceReflectionsEncode(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> objects,uint32_t objectCount){
 NMFaceReflectionFrame* f=ring.faceReflections;if(!f)return nmFacesFail(r,@"Reflection face budget was not prepared");f.encoded=NO;
 if(objectCount&&(!objects||objects.length<(NSUInteger)objectCount*16))return nmFacesFail(r,@"Reflection objects exceed admission buffer");
 id<MTLBlitCommandEncoder> clear=nmBlitEncoder(cb,"face_reflections.nmFaceReflectionsEncode#1");if(!clear)return nmFacesFail(r,@"Reflection face admission clear failed");[clear fillBuffer:f.admission range:NSMakeRange(0,MAX(f.faceCount*4,4)) value:0];[clear endEncoding];
 id<MTLComputeCommandEncoder> e=nmComputeEncoder(cb,"face_reflections.nmFaceReflectionsEncode#2");if(!e)return nmFacesFail(r,@"Reflection face budget encoder failed");
 uint32_t counts[4]={(uint32_t)f.recordCount,objectCount,(uint32_t)f.faceCount,0};[e setComputePipelineState:r.faceReflections.budget];[e setBuffer:f.records offset:0 atIndex:0];[e setBuffer:objects?:r.faceState.empty offset:0 atIndex:18];[e setBuffer:ring.faceRing.prepared?:r.faceState.empty offset:0 atIndex:21];[e setBuffer:f.admission offset:0 atIndex:25];[e setBuffer:f.budget offset:0 atIndex:26];[e setBytes:counts length:sizeof(counts) atIndex:27];[e dispatchThreads:MTLSizeMake(1,1,1) threadsPerThreadgroup:MTLSizeMake(1,1,1)];[e endEncoding];f.encoded=YES;return YES;
}
static void nmFaceReflectionsBind(NMRing* ring,id<MTLRenderCommandEncoder> e){NMFaceReflectionFrame* f=ring.faceReflections;if(!f||!f.encoded)return;[e setVertexBuffer:f.admission offset:0 atIndex:25];[e setVertexBuffer:f.budget offset:0 atIndex:26];}
static NSDictionary* nmFaceReflectionsInfo(NMRing* ring){NMFaceReflectionFrame* f=ring.faceReflections;if(!f||!f.encoded)return @{@"reflection_face_budget_enabled":@NO};const uint32_t* b=f.budget.contents;return @{@"reflection_face_budget_enabled":@YES,@"reflection_model_source_vertices":@(b[0]),@"reflection_admitted_faces":@(b[1]),@"reflection_cap_suppressed_faces":@(b[2]),@"reflection_eligible_faces":@(b[3]),@"reflection_order_records":@(f.recordCount)};}
