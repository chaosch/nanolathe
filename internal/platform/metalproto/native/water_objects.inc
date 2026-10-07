// Include after renderer/ring interfaces, grow/target, modelBindings and
// NMCloakDraw. Root owns the two state/ring properties and encode barriers.
_Static_assert(sizeof(NMWaterObject)==16,"water object layout");
_Static_assert(sizeof(NMWaterObjectsUpload)==40,"water object upload layout");
_Static_assert(sizeof(NMWaterSourcesUpload)==16,"water source upload layout");
_Static_assert(sizeof(NMWaterSourceVertex)==64,"water source vertex layout");
@interface NMWaterObjectsState : NSObject
@property id<MTLRenderPipelineState> source,stock,resolve,commit,surface;
@property NMWaterTilesState* tiles;
@property id<MTLTexture> reflectionColor,reflectionSoft;
@property uint64_t frames,reflectedModels,reflectionVertices,groupSuppressed,capSuppressed,seabedDraws,underwaterSlots,reflectedSprites,reflectedLines,stockCapSuppressed;
@end
@implementation NMWaterObjectsState
@end
@interface NMWaterObjectsRing : NSObject
@property id<MTLBuffer> objects,slotFlags,seabed,sources;
@property NMWaterTilesFrame* tiles;
@property id<MTLTexture> painted;
@property NSUInteger objectCount,seabedCount,slotCount,sourceVertices;
@property vector_float4 controls;
@end
@implementation NMWaterObjectsRing
@end
static id<MTLRenderPipelineState> nmWaterObjectsPipeline(NMRenderer* r,id<MTLLibrary> lib,NSString* vertex,NSString* fragment,NSUInteger outputs,BOOL source,BOOL blend,NSError** error){
 MTLRenderPipelineDescriptor* d=[MTLRenderPipelineDescriptor new];d.vertexFunction=[lib newFunctionWithName:vertex];d.fragmentFunction=[lib newFunctionWithName:fragment];
 if([fragment isEqualToString:@"nm_wo_commit_fragment"])d.depthAttachmentPixelFormat=MTLPixelFormatDepth32Float;
 for(NSUInteger i=0;i<outputs;i++){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[i];a.pixelFormat=source?MTLPixelFormatRGBA8Unorm:MTLPixelFormatRGBA16Float;
  if(blend){a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=(!source&&i==1)?MTLBlendFactorOne:MTLBlendFactorOneMinusSourceAlpha;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;}
 }
 return [r.device newRenderPipelineStateWithDescriptor:d error:error];
}
static BOOL nmWaterObjectsInitialize(NMRenderer* r,id<MTLLibrary> lib,NSError** error){
 NMWaterObjectsState* s=[NMWaterObjectsState new];
 s.tiles=nmWaterTilesInitialize(r.device,lib,error);if(!s.tiles)return NO;const char* full=getenv("NANOLATHE_METAL_FULL_REFLECTIONS");if(full&&strcmp(full,"1")==0)s.tiles.enabled=NO;
 s.source=nmWaterObjectsPipeline(r,lib,@"nm_wo_reflection_vertex",@"nm_wo_reflection_source",2,YES,YES,error);
 s.stock=nmWaterObjectsPipeline(r,lib,@"nm_wo_stock_vertex",@"nm_wo_stock_source",2,YES,YES,error);
 s.resolve=nmWaterObjectsPipeline(r,lib,@"nm_wo_screen_vertex",@"nm_wo_reflection_resolve",1,NO,YES,error);
 s.commit=nmWaterObjectsPipeline(r,lib,@"nm_wo_commit_vertex",@"nm_wo_commit_fragment",2,NO,YES,error);
 s.surface=nmWaterObjectsPipeline(r,lib,@"nm_wo_screen_vertex",@"nm_wo_surface_fragment",1,NO,NO,error);
 if(!s.source||!s.stock||!s.resolve||!s.commit||!s.surface)return NO;r.waterObjectsState=s;return YES;
}
static BOOL nmWaterObjectsFail(NMRenderer* r,NSString* reason){r.lastError=reason;return NO;}
static BOOL nmWaterObjectsPrepare(NMRenderer* r,NMRing* ring,const NMWaterObjectsUpload* u){
 NMWaterObjectsRing* f=ring.waterObjectsRing;
 if(!u){if(f){f.objectCount=0;f.seabedCount=0;f.slotCount=0;f.sourceVertices=0;}return YES;}
 if(!r.waterObjectsState||u->object_count>INT32_MAX||u->seabed_count>INT32_MAX||(u->object_count&&!u->objects)||(u->seabed_count&&!u->seabed))return nmWaterObjectsFail(r,@"Invalid water-object upload");
 if((NSUInteger)u->object_count*4>ring.instances.length||(NSUInteger)u->object_count*4>ring.compositionSelectors.length)return nmWaterObjectsFail(r,@"Water-object instances exceed retained model span");
 if(!f){f=[NMWaterObjectsRing new];ring.waterObjectsRing=f;}
 f.objectCount=f.seabedCount=f.slotCount=f.sourceVertices=0;
 NSUInteger ob=(NSUInteger)u->object_count*16,sb=(NSUInteger)u->seabed_count*4,fb=ring.compositionSlotCount*16;
 if(ob>r.device.maxBufferLength||sb>r.device.maxBufferLength||fb>r.device.maxBufferLength)return nmWaterObjectsFail(r,@"Water-object device buffer limit");
 f.objects=grow(r,f.objects,ob);f.seabed=grow(r,f.seabed,sb);f.slotFlags=grow(r,f.slotFlags,fb);
 if(!f.objects||!f.seabed||!f.slotFlags)return nmWaterObjectsFail(r,@"Water-object allocation failed");
 if(ob)memcpy(f.objects.contents,u->objects,ob);if(sb)memcpy(f.seabed.contents,u->seabed,sb);
 const uint32_t* seabed=f.seabed.contents;for(NSUInteger i=0;i<u->seabed_count;i++)if(seabed[i]>=ring.spriteCount)return nmWaterObjectsFail(r,@"Seabed sprite exceeds retained sprite span");
 vector_float4* flags=f.slotFlags.contents;if(fb)memset(flags,0,fb);
 const uint32_t* selectors=ring.compositionSelectors.contents;
 NMWaterObject* objects=f.objects.contents;
 // Isolated source pages remain available to reflections. Only the parent's
 // water policy admits the final group to the underwater composition pass.
 for(NSUInteger i=0;i<u->object_count;i++){
  uint32_t slot=selectors[i];if(!slot)continue;if(slot>ring.compositionSlotCount)return nmWaterObjectsFail(r,@"Water-object selector exceeds subject atlas");
  uint32_t final=nmGroupFinalSlot(ring,i,slot);objects[i].params[3]=(float)final;
  if(final==slot)flags[slot-1].x=objects[i].params[2];
 }
 NMGroupFrame* groups=ring.groups;const NMGroup* descriptors=groups.descriptors.contents;
 for(NSUInteger i=0;i<groups.groupCount;i++){NMGroup group=descriptors[i];if(group.info[0]>=u->object_count||!group.info[2]||group.info[2]>ring.compositionSlotCount)return nmWaterObjectsFail(r,@"Water group exceeds parent/slot span");flags[group.info[2]-1].x=objects[group.info[0]].params[2];}
 for(NSUInteger i=0;i<ring.compositionSlotCount;i++)if(flags[i].x>.5)r.waterObjectsState.underwaterSlots++;
 f.objectCount=u->object_count;f.seabedCount=u->seabed_count;f.slotCount=ring.compositionSlotCount;f.controls=(vector_float4){u->controls[0],u->controls[1],u->controls[2],u->controls[3]};r.waterObjectsState.frames++;return YES;
}
static BOOL nmWaterObjectsPrepareSources(NMRenderer* r,NMRing* ring,const NMWaterSourcesUpload* u){
 NMWaterObjectsRing* f=ring.waterObjectsRing;if(!u){if(f)f.sourceVertices=0;return YES;}
 if(!r.waterObjectsState||!f||u->vertex_count>INT32_MAX||u->vertex_count%4||(u->vertex_count&&!u->sources))return nmWaterObjectsFail(r,@"Invalid stock water-source upload");
 f.sourceVertices=0;NSUInteger bytes=(NSUInteger)u->vertex_count*sizeof(NMWaterSourceVertex);if(bytes>r.device.maxBufferLength)return nmWaterObjectsFail(r,@"Stock water-source buffer limit");
 f.sources=grow(r,f.sources,bytes);if(!f.sources)return nmWaterObjectsFail(r,@"Stock water-source allocation failed");if(bytes)memcpy(f.sources.contents,u->sources,bytes);f.sourceVertices=u->vertex_count;return YES;
}
static MTLRenderPassDescriptor* nmWaterObjectsPass(id<MTLTexture> color,BOOL clear){MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=color;p.colorAttachments[0].loadAction=clear?MTLLoadActionClear:MTLLoadActionLoad;p.colorAttachments[0].storeAction=MTLStoreActionStore;p.colorAttachments[0].clearColor=MTLClearColorMake(0,0,0,0);return p;}
static void nmWaterObjectsUniforms(NMRenderer* r,NMWaterObjectsRing* f,id<MTLRenderCommandEncoder> enc,NMUniforms u){[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setVertexBytes:r->water length:sizeof(r->water) atIndex:9];[enc setFragmentBytes:r->water length:sizeof(r->water) atIndex:9];vector_float4 control=f.controls;[enc setVertexBytes:&control length:sizeof(control) atIndex:19];[enc setFragmentBytes:&control length:sizeof(control) atIndex:19];}
static BOOL nmWaterObjectsReflections(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u){
 NMWaterObjectsRing* f=ring.waterObjectsRing;NMWaterObjectsState* s=r.waterObjectsState;if(!f||(!f.objectCount&&!f.sourceVertices)||!s||!r.waterMask||r->water[4]<=0)return YES;
 const NMWaterObject* objects=f.objects.contents;const NMCloakDraw* draws=ring.compositionDraws.bytes;const uint32_t* selectors=ring.compositionSelectors.contents;
 BOOL admitted=f.sourceVertices>0;for(NSUInteger i=0;i<ring.compositionDrawCount;i++)if(r.modelMetadata&&draws[i].instance<f.objectCount&&selectors[draws[i].instance]!=0&&objects[draws[i].instance].params[0]>.5){admitted=YES;break;}if(!admitted)return YES;
 if(!s.reflectionColor||s.reflectionColor.width!=r.color.width||s.reflectionColor.height!=r.color.height){s.reflectionColor=target(r,r.color.width,r.color.height,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);s.reflectionSoft=target(r,r.color.width,r.color.height,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);}
 if(!s.reflectionColor||!s.reflectionSoft)return nmWaterObjectsFail(r,@"Reflection source allocation failed");
 MTLRenderPassDescriptor* pass=nmWaterObjectsPass(s.reflectionColor,YES);pass.colorAttachments[1].texture=s.reflectionSoft;pass.colorAttachments[1].loadAction=MTLLoadActionClear;pass.colorAttachments[1].storeAction=MTLStoreActionStore;pass.colorAttachments[1].clearColor=MTLClearColorMake(0,0,0,0);
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:pass];modelBindings(r,enc,poses,ring,u);nmWaterObjectsUniforms(r,f,enc,u);[enc setRenderPipelineState:s.source];[enc setDepthStencilState:nil];[enc setCullMode:MTLCullModeNone];nmFaceReflectionsBind(ring,enc);[enc setFragmentTexture:r.modelAtlasColor atIndex:0];[enc setFragmentTexture:r.modelMetadata atIndex:1];
 NSUInteger vertices=0;
 for(NSUInteger i=0;i<ring.compositionDrawCount;i++){
  NMCloakDraw d=draws[i];if(!r.modelMetadata||d.instance>=f.objectCount||selectors[d.instance]==0||objects[d.instance].params[0]<.5)continue;NMMesh* mesh=r.meshes[d.mesh];
  NMFaceMesh* faceMesh=mesh.faceMesh;if(!faceMesh.indexCount)continue;
  nmProjectionBind(ring,enc,d.instance);nmFacesBind(ring,enc,d.instance);[enc setVertexBuffer:ring.groups.instanceGroups offset:d.instance*16 atIndex:20];vertices+=faceMesh.indexCount;
  [enc setVertexBuffer:faceMesh.corners offset:0 atIndex:0];[enc setVertexBuffer:ring.instances offset:d.instance*4 atIndex:1];[enc setVertexBuffer:ring.visuals offset:d.instance*64 atIndex:5];[enc setVertexBuffer:ring.rules offset:d.instance*112 atIndex:10];[enc setVertexBuffer:ring.compositionSelectors offset:d.instance*4 atIndex:14];[enc setVertexBuffer:f.objects offset:d.instance*16 atIndex:18];
  [enc drawIndexedPrimitives:MTLPrimitiveTypeTriangle indexCount:faceMesh.indexCount indexType:MTLIndexTypeUInt32 indexBuffer:faceMesh.indices indexBufferOffset:0];s.reflectedModels++;
 }
 // Production appends stock billboards, then strokes, after model faces.
 // Both consume four source corners; the six raster vertices are only the
 // quad fan. They may use all remaining capacity beyond the model reserve.
 NSUInteger stock=f.sourceVertices; // GPU budget rejects excess stock quads.
 const NMWaterSourceVertex* sources=f.sources.contents;
 if(stock){
  BOOL sprite=NO;for(NSUInteger i=0;i<stock;i+=4)if(sources[i].position_height[3]<1.5){sprite=YES;break;}
  id<MTLTexture> atlas=r.effectState.atlas;if(sprite&&!atlas){[enc endEncoding];return nmWaterObjectsFail(r,@"Reflected billboard has no uploaded effects atlas");}
  [enc setRenderPipelineState:s.stock];[enc setVertexBuffer:f.sources offset:0 atIndex:0];[enc setFragmentTexture:atlas?:r.emptyFog atIndex:0];
  [enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:stock/4];
  for(NSUInteger i=0;i<stock;i+=4){if(sources[i].position_height[3]<1.5)s.reflectedSprites++;else s.reflectedLines++;}
 }
 s.reflectionVertices+=vertices+stock;[enc endEncoding];
 if(!f.tiles)f.tiles=[NMWaterTilesFrame new];NSError* tileError=nil;if(!nmWaterTilesEncode(s.tiles,f.tiles,cb,s.reflectionColor,s.reflectionSoft,&tileError))return nmWaterObjectsFail(r,tileError.localizedDescription);
 enc=[cb renderCommandEncoderWithDescriptor:nmWaterObjectsPass(r.color,NO)];nmWaterObjectsUniforms(r,f,enc,u);[enc setRenderPipelineState:s.resolve];if(!nmWaterTilesBind(f.tiles,enc)){[enc endEncoding];return nmWaterObjectsFail(r,@"Reflection tile bind failed");}[enc setFragmentTexture:s.reflectionColor atIndex:0];[enc setFragmentTexture:s.reflectionSoft atIndex:1];[enc setFragmentTexture:r.waterMask atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];return YES;
}
// Use instead of the ordinary atlas-commit pipeline, leaving slots/paint buffer
// bindings and production painter order in nmCompositionCommit unchanged.
static void nmWaterObjectsBindCommit(NMRenderer* r,NMRing* ring,id<MTLRenderCommandEncoder> enc,NMUniforms u){NMWaterObjectsRing* f=ring.waterObjectsRing;if(!f||!f.slotCount||!r.modelMetadata||!r.waterMask)return;[enc setRenderPipelineState:r.waterObjectsState.commit];nmWaterObjectsUniforms(r,f,enc,u);[enc setVertexBuffer:f.slotFlags offset:0 atIndex:18];[enc setFragmentTexture:r.modelMetadata atIndex:2];[enc setFragmentTexture:r.waterMask atIndex:3];}
static void nmWaterObjectsSeabed(NMRenderer* r,NMRing* ring,id<MTLRenderCommandEncoder> enc,NMUniforms u){NMWaterObjectsRing* f=ring.waterObjectsRing;if(!f||!f.seabedCount)return;const uint32_t* indices=f.seabed.contents;[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:nil];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:r.spriteAtlas atIndex:0];for(NSUInteger i=0;i<f.seabedCount;i++){[enc setVertexBuffer:ring.sprites offset:indices[i]*96 atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];r.waterObjectsState.seabedDraws++;}}
// Active surface path: base terrain must defer water, ground-light resolve and
// shadow multiplication (water mask admission zero, lightControl.y=0, visual.x=0
// for that draw only). This call receives the original frame uniforms.
static BOOL nmWaterObjectsSurface(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,NMUniforms u){NMWaterObjectsRing* f=ring.waterObjectsRing;if(!f||!r.waterMask||r->water[7]<.5)return YES;
 if(!f.painted||f.painted.width!=r.color.width||f.painted.height!=r.color.height)f.painted=target(r,r.color.width,r.color.height,MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead);if(!f.painted)return nmWaterObjectsFail(r,@"Painted seabed snapshot allocation failed");
 id<MTLBlitCommandEncoder> blit=[cb blitCommandEncoder];[blit copyFromTexture:r.color sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(r.color.width,r.color.height,1) toTexture:f.painted destinationSlice:0 destinationLevel:0 destinationOrigin:MTLOriginMake(0,0,0)];[blit endEncoding];
 float water[20];memcpy(water,r->water,sizeof(water));water[12]=u.camera.x-u.viewport.x*.5/u.camera.z;water[13]=u.camera.y-u.viewport.y*.5/u.camera.z;water[14]=u.camera.x+u.viewport.x*.5/u.camera.z;water[15]=u.camera.y+u.viewport.y*.5/u.camera.z;
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:nmWaterObjectsPass(r.color,NO)];nmWaterObjectsUniforms(r,f,enc,u);[enc setFragmentBytes:water length:sizeof(water) atIndex:9];[enc setRenderPipelineState:r.waterObjectsState.surface];[enc setFragmentTexture:f.painted atIndex:0];[enc setFragmentTexture:r.waterMask atIndex:3];[enc setFragmentTexture:r.shadowMask?:r.emptyFog atIndex:2];[enc setFragmentTexture:r.groundField?:r.emptyFog atIndex:5];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];return YES;
}
// Device-classified physical tiles reject only mathematically empty filter
// footprints and select the unchanged cheap filter when softness is zero.
static NSDictionary* nmWaterObjectsInfo(NMRenderer* r){NMWaterObjectsState* s=r.waterObjectsState;if(!s)return @{@"water_objects_enabled":@NO};return @{@"water_objects_enabled":@YES,@"water_object_frames":@(s.frames),@"water_reflected_models":@(s.reflectedModels),@"water_reflection_submitted_raster_vertices":@(s.reflectionVertices),@"water_group_suppressed":@(s.groupSuppressed),@"water_cap_suppressed":@(s.capSuppressed),@"water_seabed_draws":@(s.seabedDraws),@"water_underwater_slots":@(s.underwaterSlots),@"water_stock_reflection_sources_supported":@YES,@"water_reflected_sprites":@(s.reflectedSprites),@"water_reflected_lines":@(s.reflectedLines),@"water_stock_cap_suppressed":@(s.stockCapSuppressed)};}
