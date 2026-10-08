// Include after NMRenderer/NMRing/NMMesh, faces/group declarations and grow.
// Root properties: NMMesh.outlineRowsMesh, NMRenderer.outlineRowsState,
// NMRing.outlineRowsRing. Body line outlines MUST be disabled before Encode.
_Static_assert(sizeof(NMOutlineRing)==16,"outline ring layout");
_Static_assert(sizeof(NMOutlineRowsUpload)==16,"outline upload layout");
@implementation NMOutlineRowsMesh
@end
@implementation NMOutlineRowsState
@end
@implementation NMOutlineRowsRing
@end
typedef struct {uint32_t span[4];int32_t domain[4];} NMOutlineRowsControl;
typedef struct {uint32_t mesh,reserved;NMOutlineRowsControl control;} NMOutlineRowsSubject;
static BOOL nmOutlineRowsFail(NMRenderer* r,NSString* error){r.lastError=error;return NO;}
static BOOL nmOutlineRowsInitialize(NMRenderer* r,id<MTLLibrary> lib,NSError** error){
 NMOutlineRowsState* s=[NMOutlineRowsState new];s.prepare=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"nm_or_prepare"] error:error];s.resolve=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"nm_or_resolve"] error:error];if(!s.prepare||!s.resolve)return NO;r.outlineRowsState=s;return YES;
}
int nm_mesh_outline_rows(void* renderer,uint32_t meshIndex,const NMOutlineRowsUpload* upload){
 NMRenderer* r=(__bridge NMRenderer*)renderer;if(!r||!upload||meshIndex>=r.meshes.count)return 0;
 if(upload->ring_count>INT32_MAX||(upload->ring_count&&!upload->rings))return nmOutlineRowsFail(r,@"Invalid outline ring upload");
 NMMesh* mesh=r.meshes[meshIndex];NSUInteger count=mesh.vertices.length/64,bytes=(NSUInteger)upload->ring_count*16;
 if(bytes>r.device.maxBufferLength)return nmOutlineRowsFail(r,@"Outline ring device buffer limit");
 uint64_t cornerCount=0;
 for(NSUInteger i=0;i<upload->ring_count;i++){NMOutlineRing ring=upload->rings[i];if(ring.count<2||ring.start>count||ring.count>count-ring.start||ring.corner_offset!=cornerCount)return nmOutlineRowsFail(r,@"Outline ring exceeds immutable mesh vertices");cornerCount+=ring.count;}
 NMOutlineRowsMesh* m=[NMOutlineRowsMesh new];m.rings=[r.device newBufferWithLength:MAX(bytes,16) options:MTLResourceStorageModeShared];if(!m.rings)return nmOutlineRowsFail(r,@"Outline ring allocation failed");if(bytes)memcpy(m.rings.contents,upload->rings,bytes);m.ringCount=upload->ring_count;m.cornerCount=(NSUInteger)cornerCount;mesh.outlineRowsMesh=m;return 1;
}
static BOOL nmOutlineRowsPrepare(NMRenderer* r,NMRing* ring){
 NMOutlineRowsRing* f=ring.outlineRowsRing;if(!f){f=[NMOutlineRowsRing new];f.subjects=[NSMutableData data];ring.outlineRowsRing=f;}[f.subjects setLength:0];
 NSUInteger n=ring.faceRing.instanceCount;if(ring.projections.length<n*48||ring.visuals.length<n*64||ring.rules.length<n*112)return nmOutlineRowsFail(r,@"Outline subject metadata span mismatch");
 const float* projections=ring.projections.contents;const float* visuals=ring.visuals.contents;const float* slots=ring.compositionSlots.contents;const uint32_t* selectors=ring.compositionSelectors.contents;const NMCloakDraw* draws=ring.compositionDraws.bytes;
 uint64_t corners=0,rings=0;vector_float4 view=ring.projectionView;
 for(NSUInteger i=0;i<ring.compositionDrawCount;i++){
  NMCloakDraw draw=draws[i];bool duplicate=false;const NMOutlineRowsSubject* prior=f.subjects.bytes;for(NSUInteger j=0;j<f.subjects.length/sizeof(*prior);j++)if(prior[j].control.span[0]==draw.instance){if(prior[j].mesh!=draw.mesh)return nmOutlineRowsFail(r,@"Outline subject uses conflicting meshes");duplicate=true;break;}if(duplicate)continue;if(draw.instance>=n||draw.mesh>=r.meshes.count)return nmOutlineRowsFail(r,@"Outline draw exceeds retained subject span");
  if(visuals[draw.instance*16+2]<=0||visuals[draw.instance*16+11]<=0)continue;
  NMMesh* mesh=r.meshes[draw.mesh];NMOutlineRowsMesh* m=mesh.outlineRowsMesh;if(!m)return nmOutlineRowsFail(r,@"Active construction model lacks immutable outline rings");if(!m.ringCount)continue;
  const float* p=&projections[draw.instance*12];if(!((uint32_t)p[3]&1u)||!isfinite(p[8])||!isfinite(p[10])||p[8]<=0||p[10]<=0)return nmOutlineRowsFail(r,@"Active construction model lacks known native projection");
  uint32_t selector=selectors[draw.instance];if(!selector||selector>ring.compositionSlotCount)return nmOutlineRowsFail(r,@"Outline source slot outside composition atlas");const float* slot=&slots[(selector-1)*12];
  double x0=floor((slot[0]-view.z)/p[10]),y0=floor((slot[1]-view.w)/p[10]);double x1=ceil((slot[0]+slot[2]-view.z)/p[10]),y1=ceil((slot[1]+slot[3]-view.w)/p[10]);
  if(!isfinite(x0)||!isfinite(y0)||!isfinite(x1)||!isfinite(y1)||x0<INT32_MIN||y0<INT32_MIN||x1>INT32_MAX||y1>INT32_MAX||x1-x0>UINT32_MAX||y1-y0>UINT32_MAX)return nmOutlineRowsFail(r,@"Outline native block domain exceeds device range");
  NMOutlineRowsSubject subject={draw.mesh,0,{{draw.instance,(uint32_t)m.ringCount,(uint32_t)corners,(uint32_t)rings},{(int32_t)x0,(int32_t)y0,(int32_t)x1,(int32_t)y1}}};[f.subjects appendBytes:&subject length:sizeof(subject)];corners+=m.cornerCount;rings+=m.ringCount;
  if(corners>UINT32_MAX||rings>UINT32_MAX||corners>r.device.maxBufferLength/16||rings>r.device.maxBufferLength/32)return nmOutlineRowsFail(r,@"Outline prepared device buffer limit");
 }
 f.corners=grow(r,f.corners,MAX((NSUInteger)corners*16,16));f.prepared=grow(r,f.prepared,MAX((NSUInteger)rings*32,32));f.diagnostics=grow(r,f.diagnostics,16);if(!f.corners||!f.prepared||!f.diagnostics)return nmOutlineRowsFail(r,@"Outline prepared allocation failed");memset(f.diagnostics.contents,0,16);f.instanceCount=n;return YES;
}
static BOOL nmOutlineRowsEncode(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u){
 NMOutlineRowsRing* f=ring.outlineRowsRing;if(u.visual.x<=.5||!f||!f.subjects.length)return YES;if(!r.outlineRowsState)return nmOutlineRowsFail(r,@"Outline compute pipeline is not initialized");
 if(r.device.readWriteTextureSupport<MTLReadWriteTextureTier2)return nmOutlineRowsFail(r,@"Outline resolve requires tier-two read/write textures");
 const NMOutlineRowsSubject* subjects=f.subjects.bytes;NSUInteger count=f.subjects.length/sizeof(*subjects);// Subjects write disjoint prepared rows and, through distinct composition
 // slots, disjoint atlas boxes, so their dispatches overlap. A subject that
 // reuses an earlier subject's slot waits for the textures written so far.
 id<MTLComputeCommandEncoder> enc=nmComputeEncoderOf(cb,"outline_rows.nmOutlineRowsEncode#1",MTLDispatchTypeConcurrent);if(!enc)return nmOutlineRowsFail(r,@"Outline compute encoder failed");
 [enc setBuffer:ring.instances offset:0 atIndex:1];[enc setBuffer:poses offset:0 atIndex:2];[enc setBytes:&u length:sizeof(u) atIndex:3];[enc setBuffer:ring.rules offset:0 atIndex:10];[enc setBuffer:ring.compositionSlots offset:0 atIndex:13];[enc setBuffer:ring.compositionSelectors offset:0 atIndex:14];[enc setBuffer:ring.groups.instanceGroups offset:0 atIndex:20];[enc setBuffer:f.corners offset:0 atIndex:21];[enc setBuffer:f.prepared offset:0 atIndex:22];[enc setBuffer:f.diagnostics offset:0 atIndex:24];[enc setBuffer:ring.projections offset:0 atIndex:27];vector_float4 view=ring.projectionView;[enc setBytes:&view length:16 atIndex:29];
 [enc setComputePipelineState:r.outlineRowsState.prepare];
 for(NSUInteger i=0;i<count;i++){NMOutlineRowsSubject s=subjects[i];NMMesh* m=r.meshes[s.mesh];[enc setBuffer:m.vertices offset:0 atIndex:0];[enc setBuffer:m.outlineRowsMesh.rings offset:0 atIndex:4];[enc setBytes:&s.control length:sizeof(s.control) atIndex:23];[enc dispatchThreads:MTLSizeMake(s.control.span[1],1,1) threadsPerThreadgroup:MTLSizeMake(MIN((NSUInteger)32,r.outlineRowsState.prepare.maxTotalThreadsPerThreadgroup),1,1)];}
 [enc memoryBarrierWithScope:MTLBarrierScopeBuffers];[enc setComputePipelineState:r.outlineRowsState.resolve];[enc setTexture:r.modelAtlasColor atIndex:0];[enc setTexture:r.modelAtlasEmission atIndex:1];[enc setTexture:r.modelMetadata atIndex:2];[enc setTexture:r.modelPalette atIndex:4];
 NSUInteger maxThreads=r.outlineRowsState.resolve.maxTotalThreadsPerThreadgroup;NSUInteger tx=MIN((NSUInteger)8,maxThreads),ty=MIN((NSUInteger)8,maxThreads/tx);
 const uint32_t* selectors=ring.compositionSelectors.contents;
 for(NSUInteger i=0;i<count;i++){NMOutlineRowsSubject s=subjects[i];NSUInteger width=(NSUInteger)((int64_t)s.control.domain[2]-s.control.domain[0]),height=(NSUInteger)((int64_t)s.control.domain[3]-s.control.domain[1]);if(!width||!height)continue;
  for(NSUInteger j=0;j<i;j++)if(selectors[subjects[j].control.span[0]]==selectors[s.control.span[0]]){[enc memoryBarrierWithScope:MTLBarrierScopeTextures];break;}
  [enc setBuffer:r.meshes[s.mesh].outlineRowsMesh.rings offset:0 atIndex:4];[enc setBytes:&s.control length:sizeof(s.control) atIndex:23];[enc dispatchThreads:MTLSizeMake(width,height,1) threadsPerThreadgroup:MTLSizeMake(tx,ty,1)];}
 [enc endEncoding];return YES;
}
// Completion owns the diagnostic sample before Prepare can reset this ring.
static void nmOutlineRowsCompleted(NMRenderer* r,NMRing* ring){
 NMOutlineRowsRing* f=ring.outlineRowsRing;uint32_t invalid=f.diagnostics?((const uint32_t*)f.diagnostics.contents)[0]:0;
 @synchronized(r.outlineRowsState){r.outlineRowsState.completedFrames++;r.outlineRowsState.unsupportedRings+=invalid;if(invalid)r.outlineRowsState.unsupportedFrames++;}
}
// Read last-ring values only after drain; totals are completion-protected.
static NSDictionary* nmOutlineRowsInfo(NMRenderer* r,NMRing* ring){NMOutlineRowsRing* f=ring.outlineRowsRing;uint32_t invalid=f.diagnostics?((const uint32_t*)f.diagnostics.contents)[0]:0;
 @synchronized(r.outlineRowsState){return @{@"outline_row_subjects":@(f.subjects.length/sizeof(NMOutlineRowsSubject)),@"outline_row_unsupported_rings_last_frame":@(invalid),@"outline_row_completed_frames":@(r.outlineRowsState.completedFrames),@"outline_row_unsupported_rings_total":@(r.outlineRowsState.unsupportedRings),@"outline_row_unsupported_frames_total":@(r.outlineRowsState.unsupportedFrames),@"outline_row_scratch_bytes":@(f.corners.length+f.prepared.length+f.diagnostics.length)};}
}
