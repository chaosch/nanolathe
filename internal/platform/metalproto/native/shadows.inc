// Include after modelBindings/drawSubjectMember declarations. Root owns the
// renderer/ring hooks; this module keeps no process globals or pointer maps.
@implementation NMShadowMesh
@end
@implementation NMShadowState
@end
@implementation NMShadowFrame
@end
_Static_assert(sizeof(NMShadowUpload)==120,"shadow upload ABI");
_Static_assert(sizeof(NMShadowMeshUpload)==24,"shadow topology ABI");
typedef struct {vector_float4 screen,body,mask,params,placement;vector_uint4 order;} NMShadowSubject;
typedef struct {uint32_t mesh,instance,slot,subject;} NMShadowDraw;
_Static_assert(sizeof(NMShadowSubject)==96,"shadow subject ABI");
_Static_assert(sizeof(NMShadowDraw)==16,"shadow draw ABI");

static NMShadowState* nmShadowsCreate(NMRenderer* r,id<MTLLibrary> lib,NSError** error) {
 NMShadowState* s=[NMShadowState new];s.meshes=[NSMutableArray new];
 MTLRenderPipelineDescriptor* d=[MTLRenderPipelineDescriptor new];
 d.vertexFunction=[lib newFunctionWithName:@"nm_shadow_project_vertex"];d.fragmentFunction=[lib newFunctionWithName:@"nm_shadow_project_fragment"];d.colorAttachments[0].pixelFormat=MTLPixelFormatR8Unorm;
 // All faces in this local mask paint the same palette index; max is local
 // coverage only. Distinct subjects are composited separately in world order.
 s.projected=[r.device newRenderPipelineStateWithDescriptor:d error:error];
 d=[MTLRenderPipelineDescriptor new];d.vertexFunction=[lib newFunctionWithName:@"nm_shadow_commit_vertex"];d.fragmentFunction=[lib newFunctionWithName:@"nm_shadow_commit_fragment"];
 for(int i=0;i<2;i++){
  MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[i];a.pixelFormat=MTLPixelFormatRGBA16Float;a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=i?MTLBlendFactorOne:MTLBlendFactorOneMinusSourceAlpha;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;
 }
 d.depthAttachmentPixelFormat=MTLPixelFormatDepth32Float;
 s.commit=[r.device newRenderPipelineStateWithDescriptor:d error:error];
 return s.projected&&s.commit?s:nil;
}
static BOOL nmShadowsPrepare(NMRenderer* r,NMShadowState* s,NMShadowFrame* f,const NMShadowUpload* u) {
 if(!s||!f||u->width>8192||u->height>8192)return NO;
 if(!u->topology_count&&!u->subject_count){f.subjectCount=0;f.bodyCount=0;f.projectedCount=0;return YES;}
 if((u->subject_count&&(!u->subjects||!u->slots||!u->width||!u->height))||(u->body_count&&(!u->body_draws||!u->body_rules))||(u->projected_count&&!u->projected_draws)||(u->piece_count&&!u->piece_flags))return NO;
 if(s.meshes.count==0&&u->topology_count){
  if(!u->topology)return NO;const NMShadowMeshUpload* meshes=(const NMShadowMeshUpload*)u->topology;
  for(uint32_t i=0;i<u->topology_count;i++){
   NMShadowMesh* mesh=[NMShadowMesh new];const NMShadowMeshUpload* src=&meshes[i];mesh.indexCount=src->index_count;
   if((src->vertex_count&&!src->vertices)||(src->index_count&&!src->indices))return NO;
   mesh.vertices=[r.device newBufferWithLength:MAX((NSUInteger)src->vertex_count*64,32) options:MTLResourceStorageModeShared];mesh.indices=[r.device newBufferWithLength:MAX((NSUInteger)src->index_count*4,32) options:MTLResourceStorageModeShared];
   if(!mesh.vertices||!mesh.indices)return NO;
   if(src->vertex_count)memcpy(mesh.vertices.contents,(const void*)src->vertices,src->vertex_count*64);
   if(src->index_count)memcpy(mesh.indices.contents,(const void*)src->indices,src->index_count*4);
   [s.meshes addObject:mesh];
  }
 }
 if(s.meshes.count!=u->topology_count)return NO;
 f.subjectCount=u->subject_count;f.bodyCount=u->body_count;f.projectedCount=u->projected_count;f.width=u->width;f.height=u->height;vector_float4 tint,water;memcpy(&tint,u->tint,16);memcpy(&water,u->water,16);f.tint=tint;f.water=water;
 f.slots=grow(r,f.slots,(NSUInteger)u->slot_count*48);f.subjects=grow(r,f.subjects,(NSUInteger)u->subject_count*96);f.bodyRules=grow(r,f.bodyRules,(NSUInteger)u->body_count*112);f.pieceFlags=grow(r,f.pieceFlags,(NSUInteger)u->piece_count*4);
 if(!f.slots||!f.subjects||!f.bodyRules||!f.pieceFlags)return NO;
 if(u->slot_count)memcpy(f.slots.contents,(const void*)u->slots,u->slot_count*48);
 if(u->subject_count)memcpy(f.subjects.contents,(const void*)u->subjects,u->subject_count*96);
 if(u->body_count)memcpy(f.bodyRules.contents,(const void*)u->body_rules,u->body_count*112);
 if(u->piece_count)memcpy(f.pieceFlags.contents,(const void*)u->piece_flags,u->piece_count*4);
 if(!f.bodyDraws)f.bodyDraws=[NSMutableData new];if(!f.projectedDraws)f.projectedDraws=[NSMutableData new];
 f.bodyDraws.length=u->body_count*16;f.projectedDraws.length=u->projected_count*16;
 if(u->body_count)memcpy(f.bodyDraws.mutableBytes,(const void*)u->body_draws,f.bodyDraws.length);
 if(u->projected_count)memcpy(f.projectedDraws.mutableBytes,(const void*)u->projected_draws,f.projectedDraws.length);
 NSUInteger w=MAX(s.body.width,u->width),h=MAX(s.body.height,u->height);
 if(w&&h&&(w!=s.body.width||h!=s.body.height)){
  s.body=target(r,w,h,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);s.emission=target(r,w,h,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget);
  s.depth=target(r,w,h,MTLPixelFormatDepth32Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);s.mask=target(r,w,h,MTLPixelFormatR8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
  if(!s.body||!s.emission||!s.depth||!s.mask)return NO;
 }
 return YES;
}
static void nmShadowBodyDraw(NMRenderer* r,NMShadowFrame* f,NMRing* ring,id<MTLRenderCommandEncoder> enc,NMShadowDraw d,NSUInteger ruleIndex,BOOL outline) {
 NMMesh* mesh=r.meshes[d.mesh];NSUInteger count=outline?mesh.edgeCount:mesh.indexCount;if(!count)return;
 [enc setVertexBuffer:mesh.vertices offset:0 atIndex:0];[enc setVertexBuffer:ring.instances offset:d.instance*4 atIndex:1];[enc setVertexBuffer:ring.visuals offset:d.instance*64 atIndex:5];[enc setVertexBuffer:f.bodyRules offset:ruleIndex*112 atIndex:10];[enc setVertexBuffer:ring.materialOffsets offset:d.instance*4 atIndex:12];
 [enc setVertexBuffer:f.slots offset:0 atIndex:13];[enc setVertexBytes:&d.slot length:4 atIndex:14];[enc setVertexBuffer:mesh.primitiveCenters offset:0 atIndex:15];
 [enc drawIndexedPrimitives:outline?MTLPrimitiveTypeLine:MTLPrimitiveTypeTriangle indexCount:count indexType:MTLIndexTypeUInt32 indexBuffer:outline?mesh.edges:mesh.indices indexBufferOffset:0 instanceCount:1];
}
static void nmShadowsRaster(NMRenderer* r,NMShadowState* s,NMShadowFrame* f,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u) {
 if(!f.subjectCount)return;
 // The target can grow after another ring was prepared. Use actual texture
 // dimensions for clip-space, retaining this frame's own slot coordinates.
 u.composition.x=s.body.width;u.composition.y=s.body.height;u.composition.z=f.tint.w;u.lightControl.y=0;u.mode.z=0;u.mode.w=3;
 MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=s.body;p.colorAttachments[1].texture=s.emission;
 for(int i=0;i<2;i++){p.colorAttachments[i].loadAction=MTLLoadActionClear;p.colorAttachments[i].storeAction=MTLStoreActionStore;p.colorAttachments[i].clearColor=MTLClearColorMake(0,0,0,0);}
 p.depthAttachment.texture=s.depth;p.depthAttachment.loadAction=MTLLoadActionClear;p.depthAttachment.storeAction=MTLStoreActionStore;p.depthAttachment.clearDepth=1;
 id<MTLRenderCommandEncoder> enc=nmRenderEncoder(cb,p,"shadows.nmShadowsRaster#1");modelBindings(r,enc,poses,ring,u);[enc setFrontFacingWinding:MTLWindingClockwise];[enc setCullMode:MTLCullModeBack];[enc setVertexBuffer:ring.subjectLights offset:0 atIndex:17];[enc setRenderPipelineState:r.atlasModel];[enc setDepthStencilState:r.depth];
 const NMShadowDraw* draws=f.bodyDraws.bytes;const float* visuals=ring.visuals.contents;
 for(NSUInteger i=0;i<f.bodyCount;i++)nmShadowBodyDraw(r,f,ring,enc,draws[i],i,NO);
 // Outline coverage is part of the finished silhouette, before the shadow.
 if(u.visual.x>0.5){[enc setRenderPipelineState:r.shadowOutline];[enc setDepthStencilState:r.depth];for(NSUInteger i=0;i<f.bodyCount;i++)if(visuals[draws[i].instance*16+2]>0&&visuals[draws[i].instance*16+11]>0)nmShadowBodyDraw(r,f,ring,enc,draws[i],i,YES);}
 [enc endEncoding];
 p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=s.mask;p.colorAttachments[0].loadAction=MTLLoadActionClear;p.colorAttachments[0].storeAction=MTLStoreActionStore;p.colorAttachments[0].clearColor=MTLClearColorMake(0,0,0,0);
 enc=nmRenderEncoder(cb,p,"shadows.nmShadowsRaster#2");[enc setRenderPipelineState:s.projected];[enc setDepthStencilState:nil];[enc setFrontFacingWinding:MTLWindingClockwise];[enc setCullMode:MTLCullModeBack];[enc setVertexBuffer:poses offset:0 atIndex:2];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setVertexBuffer:f.pieceFlags offset:0 atIndex:17];vector_float4 tint=f.tint;[enc setVertexBytes:&tint length:16 atIndex:18];
 draws=f.projectedDraws.bytes;
 for(NSUInteger i=0;i<f.projectedCount;i++){
  NMShadowDraw d=draws[i];NMShadowMesh* mesh=s.meshes[d.mesh];if(!mesh.indexCount)continue;
  [enc setVertexBuffer:mesh.vertices offset:0 atIndex:0];[enc setVertexBuffer:ring.instances offset:d.instance*4 atIndex:1];[enc setVertexBuffer:ring.rules offset:d.instance*112 atIndex:10];[enc setVertexBuffer:f.slots offset:(d.slot-1)*48 atIndex:13];
  [enc drawIndexedPrimitives:MTLPrimitiveTypeTriangle indexCount:mesh.indexCount indexType:MTLIndexTypeUInt32 indexBuffer:mesh.indices indexBufferOffset:0 instanceCount:1];
 }
 [enc endEncoding];
}
// Root calls this immediately before a model-slot body commit, after earlier
// sprite/model writes. A staged child's solo shadow precedes the parent's.
static void nmShadowsCommitSlot(NMRenderer* r,NMShadowState* s,NMShadowFrame* f,id<MTLRenderCommandEncoder> enc,NMUniforms u,uint32_t slot) {
 if(!f.subjectCount)return;
 const NMShadowSubject* subjects=f.subjects.contents;
 [enc setRenderPipelineState:s.commit];[enc setDepthStencilState:nmSlabCommitActive?r.spriteDepth:nil];[enc setCullMode:MTLCullModeNone];[enc setVertexBuffer:f.subjects offset:0 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBuffer:f.subjects offset:0 atIndex:0];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];
 vector_float4 tint=f.tint,water=f.water;[enc setFragmentBytes:&tint length:16 atIndex:5];[enc setFragmentBytes:&water length:16 atIndex:6];[enc setFragmentTexture:s.body atIndex:0];[enc setFragmentTexture:s.depth atIndex:1];[enc setFragmentTexture:s.mask atIndex:2];[enc setFragmentTexture:r.waterMask?:r.emptyFog atIndex:3];
 for(uint32_t i=0;i<f.subjectCount;i++)if(subjects[i].order.x==slot){[enc setVertexBytes:&i length:4 atIndex:1];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];}
}
