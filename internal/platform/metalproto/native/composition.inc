// Included after renderer declarations and shared binding helpers. The atlas
// owns model-local depth; the world receives subjects in production paint order.
static BOOL nmCompositionPrepare(NMRenderer* r,NMRing* ring,const NMLiveUpload* u) {
 ring.composed=u->composed;ring.compositionSlotCount=u->composition_slot_count;ring.compositionDrawCount=u->composition_draw_count;ring.compositionPaintCount=u->composition_paint_count;
 ring.subjectLights=grow(r,ring.subjectLights,MAX((NSUInteger)u->composition_slot_count*36,36));if(!ring.subjectLights)return NO;
 ring.compositionSlots=grow(r,ring.compositionSlots,MAX((NSUInteger)u->composition_slot_count*48,48));
 ring.compositionSelectors=grow(r,ring.compositionSelectors,(NSUInteger)u->instance_count*4);
 ring.compositionPaint=grow(r,ring.compositionPaint,(NSUInteger)u->composition_paint_count*16);
 if(!ring.compositionSlots||!ring.compositionSelectors||!ring.compositionPaint)return NO;
 if(!u->composed)return YES;
 if(u->composition_slot_count&&(!u->composition_slots||!u->composition_paint||!u->composition_width||!u->composition_height))return NO;
 if(u->composition_width>8192||u->composition_height>8192)return NO;
 if(u->instance_count)memcpy(ring.compositionSelectors.contents,u->composition_selectors,u->instance_count*4);
 if(u->composition_slot_count)memcpy(ring.compositionSlots.contents,u->composition_slots,u->composition_slot_count*48);
 if(u->composition_paint_count)memcpy(ring.compositionPaint.contents,u->composition_paint,u->composition_paint_count*16);
 if(!ring.compositionDraws)ring.compositionDraws=[NSMutableData new];ring.compositionDraws.length=u->composition_draw_count*sizeof(NMCloakDraw);
 if(u->composition_draw_count)memcpy(ring.compositionDraws.mutableBytes,u->composition_draws,ring.compositionDraws.length);
 // Production always rasterizes the body at 2x; Supersample chooses the resolve
 // sample pattern, and underwater refraction keeps its weighted gather.
 float rasterScale=2;float* slots=ring.compositionSlots.contents;for(NSUInteger i=0;i<u->composition_slot_count;i++)for(int j=4;j<8;j++)slots[i*12+j]*=rasterScale;
 NSUInteger w=MAX(r.modelAtlasColor.width,u->composition_width*rasterScale),h=MAX(r.modelAtlasColor.height,u->composition_height*rasterScale);
 if(w&&h&&(w!=r.modelAtlasColor.width||h!=r.modelAtlasColor.height)){
  r.modelMetadata=target(r,w,h,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
  r.modelAtlasColor=target(r,w,h,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
  r.modelAtlasEmission=target(r,w,h,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
  r.modelAtlasDepth=target(r,w,h,MTLPixelFormatDepth32Float,MTLTextureUsageRenderTarget);
  if(!r.modelMetadata||!r.modelAtlasColor||!r.modelAtlasEmission||!r.modelAtlasDepth)return NO;
 }
 return YES;
}
static void nmCompositionRaster(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u) {
 if(!ring.compositionSlotCount)return;
 MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=r.modelAtlasColor;p.colorAttachments[1].texture=r.modelAtlasEmission;p.colorAttachments[2].texture=r.modelMetadata;
 for(int i=0;i<3;i++){p.colorAttachments[i].loadAction=MTLLoadActionClear;p.colorAttachments[i].storeAction=MTLStoreActionStore;p.colorAttachments[i].clearColor=MTLClearColorMake(0,0,0,0);}
 p.depthAttachment.texture=r.modelAtlasDepth;p.depthAttachment.loadAction=MTLLoadActionClear;p.depthAttachment.storeAction=MTLStoreActionDontCare;p.depthAttachment.clearDepth=1;
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:p];u.mode.z=0;u.mode.w=2;
 modelBindings(r,enc,poses,ring,u);[enc setFrontFacingWinding:MTLWindingClockwise];[enc setCullMode:MTLCullModeNone];[enc setRenderPipelineState:r.atlasModelMetadata];[enc setDepthStencilState:r.depth];
 const NMCloakDraw* draws=ring.compositionDraws.bytes;
 for(NSUInteger i=0;i<ring.compositionDrawCount;i++)drawSubjectMember(r,enc,ring,draws[i],NO);
 // Construction row endpoints resolve complete native pixel blocks after this pass.
 [enc endEncoding];
}
static void nmCompositionCommit(NMRenderer* r,NMRing* ring,id<MTLRenderCommandEncoder> enc,NMUniforms u,uint32_t phase) {
 const uint32_t* paint=ring.compositionPaint.contents;NSUInteger begin=0;while(begin<ring.compositionPaintCount&&paint[begin*4+1]<phase)begin++;
 NSUInteger end=begin;while(end<ring.compositionPaintCount&&paint[end*4+1]==phase)end++;if(begin==end)return;
 [enc setDepthStencilState:nil];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];
 while(begin<end){
  BOOL sprite=paint[begin*4+3]==1;NSUInteger stop=begin+1;
  while(sprite&&stop<end&&paint[stop*4+3]==1&&paint[stop*4]==paint[stop*4-4]+1)stop++;
  if(!sprite)nmShadowsCommitSlot(r,r.shadows,ring.shadows,enc,u,paint[begin*4]);
  if(sprite){[enc setRenderPipelineState:r.sprite];[enc setVertexBuffer:ring.sprites offset:(paint[begin*4]-1)*96 atIndex:0];[enc setFragmentTexture:r.spriteAtlas atIndex:0];}
  else{[enc setRenderPipelineState:r.atlasCommit];[enc setVertexBuffer:ring.compositionSlots offset:0 atIndex:0];[enc setVertexBuffer:ring.compositionPaint offset:begin*16 atIndex:1];[enc setFragmentTexture:r.modelAtlasColor atIndex:0];[enc setFragmentTexture:r.modelAtlasEmission atIndex:1];nmWaterObjectsBindCommit(r,ring,enc,u);}
  [enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:stop-begin];begin=stop;
 }
}
