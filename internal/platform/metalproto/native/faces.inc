// Include after renderer/ring/mesh declarations and grow. Root adds properties:
// NMMesh.faceMesh; NMRenderer.faceState; NMRing.faceRing.
_Static_assert(sizeof(NMFace)==32,"authored face layout");
_Static_assert(sizeof(NMFaceUpload)==40,"authored face upload layout");
@interface NMFaceMesh : NSObject
@property id<MTLBuffer> faces,corners,indices;
@property NSUInteger faceCount,cornerCount,indexCount;
@end
@implementation NMFaceMesh
@end
@interface NMFaceState : NSObject
@property id<MTLComputePipelineState> prepare;
@property id<MTLBuffer> empty;
@end
@implementation NMFaceState
@end
@interface NMFaceRing : NSObject
@property id<MTLBuffer> prepared,bases;
@property NSUInteger instanceCount,faceCount;
@end
@implementation NMFaceRing
@end
static BOOL nmFacesFail(NMRenderer* r,NSString* error){r.lastError=error;return NO;}
static BOOL nmFacesInitialize(NMRenderer* r,id<MTLLibrary> lib,NSError** error){
 NMFaceState* s=[NMFaceState new];s.prepare=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"nm_face_prepare"] error:error];s.empty=[r.device newBufferWithLength:144 options:MTLResourceStorageModeShared];if(!s.prepare||!s.empty)return NO;memset(s.empty.contents,0,144);r.faceState=s;return YES;
}
int nm_mesh_faces(void* renderer,uint32_t meshIndex,const NMFaceUpload* u){
 NMRenderer* r=(__bridge NMRenderer*)renderer;if(!r||!u||meshIndex>=r.meshes.count)return 0;
 if(u->face_count>INT32_MAX||u->corner_count>INT32_MAX||u->index_count>INT32_MAX||(u->face_count&&!u->faces)||(u->corner_count&&!u->corners)||(u->index_count&&!u->indices))return nmFacesFail(r,@"Invalid authored face upload");
 NSUInteger fb=(NSUInteger)u->face_count*32,cb=(NSUInteger)u->corner_count*64,ib=(NSUInteger)u->index_count*4;
 if(fb>r.device.maxBufferLength||cb>r.device.maxBufferLength||ib>r.device.maxBufferLength)return nmFacesFail(r,@"Authored face device buffer limit");
 for(NSUInteger i=0;i<u->face_count;i++){NMFace f=u->faces[i];if(f.corner_count<3||(uint64_t)f.corner_start+f.corner_count>u->corner_count||(uint64_t)f.index_start+f.index_count>u->index_count||(uint64_t)f.index_count!=3ull*(f.corner_count-2)||(f.flags&~1u)||!!(f.flags&1u)!=(f.corner_count==4))return nmFacesFail(r,@"Authored face exceeds immutable corner span");}
 for(NSUInteger i=0;i<u->index_count;i++)if(u->indices[i]>=u->corner_count)return nmFacesFail(r,@"Authored face index exceeds immutable corner span");
 NMFaceMesh* m=[NMFaceMesh new];m.faces=[r.device newBufferWithLength:MAX(fb,32) options:MTLResourceStorageModeShared];m.corners=[r.device newBufferWithLength:MAX(cb,64) options:MTLResourceStorageModeShared];m.indices=[r.device newBufferWithLength:MAX(ib,4) options:MTLResourceStorageModeShared];if(!m.faces||!m.corners||!m.indices)return nmFacesFail(r,@"Authored face allocation failed");
 if(fb)memcpy(m.faces.contents,u->faces,fb);if(cb)memcpy(m.corners.contents,u->corners,cb);if(ib)memcpy(m.indices.contents,u->indices,ib);m.faceCount=u->face_count;m.cornerCount=u->corner_count;m.indexCount=u->index_count;r.meshes[meshIndex].faceMesh=m;return 1;
}
static BOOL nmFacesPrepare(NMRenderer* r,NMRing* ring,uint32_t instanceCount){
 if(!r.faceState)return nmFacesFail(r,@"Authored face pipeline is not initialized");
 if(instanceCount>INT32_MAX||ring.spans.length<r.meshes.count*8)return nmFacesFail(r,@"Invalid authored face instance spans");
 const uint32_t* spans=ring.spans.bytes;uint64_t total=0;
 for(NSUInteger i=0;i<r.meshes.count;i++){NSUInteger start=spans[i*2],count=spans[i*2+1];if(start>instanceCount||count>instanceCount-start)return nmFacesFail(r,@"Authored face span exceeds instance storage");NMFaceMesh* m=r.meshes[i].faceMesh;total+=(uint64_t)count*m.faceCount;}
 if(total>UINT32_MAX-1||total>r.device.maxBufferLength/144||(uint64_t)instanceCount*4>r.device.maxBufferLength)return nmFacesFail(r,@"Prepared face device buffer limit");
 NMFaceRing* f=ring.faceRing;if(!f){f=[NMFaceRing new];ring.faceRing=f;}
 f.prepared=grow(r,f.prepared,MAX((NSUInteger)total*144,144));f.bases=grow(r,f.bases,MAX((NSUInteger)instanceCount*4,4));if(!f.prepared||!f.bases)return nmFacesFail(r,@"Prepared face allocation failed");
 memset(f.bases.contents,0,MAX((NSUInteger)instanceCount*4,4));uint32_t* bases=f.bases.contents;uint32_t base=1;
 for(NSUInteger i=0;i<r.meshes.count;i++){NMFaceMesh* m=r.meshes[i].faceMesh;for(NSUInteger j=0;j<spans[i*2+1];j++){if(m.faceCount){bases[spans[i*2]+j]=base;base+=(uint32_t)m.faceCount;}}}
 f.faceCount=(NSUInteger)total;f.instanceCount=instanceCount;return YES;
}
static BOOL nmFacesEncode(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,id<MTLBuffer> waterObjects,NMUniforms u){
 NMFaceRing* f=ring.faceRing;if(!f||!f.faceCount)return YES;
 if(waterObjects&&waterObjects.length<f.instanceCount*16)return nmFacesFail(r,@"Water objects do not cover prepared face instances");
 const uint32_t* spans=ring.spans.bytes;id<MTLComputeCommandEncoder> e=[cb computeCommandEncoder];if(!e)return nmFacesFail(r,@"Authored face encoder failed");[e setComputePipelineState:r.faceState.prepare];
 [e setBuffer:ring.instances offset:0 atIndex:1];[e setBuffer:poses offset:0 atIndex:2];[e setBytes:&u length:sizeof(u) atIndex:3];[e setBuffer:ring.visuals offset:0 atIndex:5];[e setBuffer:r.materials offset:0 atIndex:6];[e setBuffer:r.textureFrames offset:0 atIndex:7];[e setBuffer:ring.rules offset:0 atIndex:10];[e setBuffer:ring.materialOverrides offset:0 atIndex:11];[e setBuffer:ring.materialOffsets offset:0 atIndex:12];[e setBuffer:ring.compositionSlots offset:0 atIndex:13];[e setBuffer:ring.compositionSelectors offset:0 atIndex:14];[e setBuffer:waterObjects?:r.faceState.empty offset:0 atIndex:18];[e setBuffer:f.prepared offset:0 atIndex:21];[e setBuffer:f.bases offset:0 atIndex:22];
 for(NSUInteger i=0;i<r.meshes.count;i++){NMFaceMesh* m=r.meshes[i].faceMesh;NSUInteger count=spans[i*2+1];if(!count||!m.faceCount)continue;
  struct {uint32_t span[4];float texture[4];} control={{spans[i*2],(uint32_t)count,(uint32_t)m.faceCount,ring.composed?1u:0u},{(float)r.atlas.width,(float)r.atlas.height,waterObjects?1.f:0.f,0}};
  [e setBuffer:m.corners offset:0 atIndex:0];[e setBuffer:m.faces offset:0 atIndex:4];[e setBytes:&control length:sizeof(control) atIndex:23];NSUInteger group=MIN((NSUInteger)64,r.faceState.prepare.maxTotalThreadsPerThreadgroup);[e dispatchThreads:MTLSizeMake(count*m.faceCount,1,1) threadsPerThreadgroup:MTLSizeMake(group,1,1)];
 }
 [e endEncoding];return YES;
}
static void nmFacesBind(NMRing* ring,id<MTLRenderCommandEncoder> e,NSUInteger start){NMFaceRing* f=ring.faceRing;if(!f)return;[e setVertexBuffer:f.prepared offset:0 atIndex:21];[e setFragmentBuffer:f.prepared offset:0 atIndex:21];[e setVertexBuffer:f.bases offset:start*4 atIndex:22];}
