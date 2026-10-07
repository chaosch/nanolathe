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
 // Depth slabs: a direct slot's band is its index in the phase-major paint walk.
 if(!ring.slotRanks)ring.slotRanks=[NSMutableData new];
 ring.slotRanks.length=MAX((NSUInteger)u->composition_slot_count,1)*4;memset(ring.slotRanks.mutableBytes,0xff,ring.slotRanks.length);
 ring.directSlotCount=0;
 {uint32_t* ranks=ring.slotRanks.mutableBytes;const uint32_t* paint=u->composition_paint;const float* slots=u->composition_slots;
  for(uint32_t i=0;i<u->composition_paint_count;i++)if(paint[i*4+3]==0&&paint[i*4]>0&&paint[i*4]<=u->composition_slot_count)ranks[paint[i*4]-1]=i;
  for(uint32_t i=0;i<u->composition_slot_count;i++)if(slots[i*12+10]>0.5f&&ranks[i]!=UINT32_MAX)ring.directSlotCount++;}
 if(!ring.compositionDraws)ring.compositionDraws=[NSMutableData new];ring.compositionDraws.length=u->composition_draw_count*sizeof(NMCloakDraw);
 if(u->composition_draw_count)memcpy(ring.compositionDraws.mutableBytes,u->composition_draws,ring.compositionDraws.length);
 // Production always rasterizes the body at 2x; Supersample chooses the resolve
 // sample pattern, and underwater refraction keeps its weighted gather.
 float rasterScale=2;float* slots=ring.compositionSlots.contents;for(NSUInteger i=0;i<u->composition_slot_count;i++)for(int j=4;j<8;j++)slots[i*12+j]*=rasterScale;
 ring.compositionPageWidth=u->composition_width*rasterScale;ring.compositionPageHeight=u->composition_height*rasterScale;
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
static BOOL nmSlotDirect(NMRing* ring,uint32_t slot){
 if(!slot||slot>ring.compositionSlotCount||!ring.directSlotCount)return NO;
 return ((const float*)ring.compositionSlots.contents)[(slot-1)*12+10]>0.5f&&((const uint32_t*)ring.slotRanks.bytes)[slot-1]!=UINT32_MAX;
}
// Depth for one paint band. Later ranks are nearer; local 0..1 orders inside it.
static double nmSlabDepth(NMRing* ring,NSUInteger rank,double local){
 double bands=(double)ring.compositionPaintCount+1;
 return (bands-(double)rank-1+local)/bands;
}
// Commit, shadow and sprite quads carry no depth of their own: a zero-height
// depth range places every fragment of the draw inside its paint band.
static void nmSlabViewport(NMRenderer* r,id<MTLRenderCommandEncoder> enc,double depth){
 [enc setViewport:(MTLViewport){0,0,(double)r.color.width,(double)r.color.height,depth,depth}];
}
static void nmSlabViewportReset(NMRenderer* r,id<MTLRenderCommandEncoder> enc){
 [enc setViewport:(MTLViewport){0,0,(double)r.color.width,(double)r.color.height,0,1}];
}
// Draws one phase's slab subjects straight into the world with the staged
// mapping, before that phase's ordered commits test against their depth.
static void nmDirectPhase(NMRenderer* r,NMRing* ring,id<MTLRenderCommandEncoder> enc,id<MTLBuffer> poses,NMUniforms u,uint32_t phase) {
 if(!ring.directSlotCount)return;
 const NMCloakDraw* draws=ring.compositionDraws.bytes;const float* slots=ring.compositionSlots.contents;const uint32_t* ranks=ring.slotRanks.bytes;
 u.mode.z=0;u.mode.w=2;
 BOOL bound=NO;
 for(NSUInteger i=0;i<ring.compositionDrawCount;i++){
  uint32_t slot=draws[i].group;
  if(!nmSlotDirect(ring,slot)||(uint32_t)slots[(slot-1)*12+9]!=phase)continue;
  if(!bound){modelBindings(r,enc,poses,ring,u);[enc setFrontFacingWinding:MTLWindingClockwise];[enc setCullMode:MTLCullModeNone];[enc setRenderPipelineState:r.directModel];[enc setDepthStencilState:r.depth];bound=YES;}
  const float* s=&slots[(slot-1)*12];
  struct {vector_float4 slab,screen,atlas;} direct={{(float)ring.compositionPaintCount+1,(float)ranks[slot-1],0,0},{s[0],s[1],s[2],s[3]},{s[4],s[5],s[6],s[7]}};
  [enc setFragmentBytes:&direct length:sizeof(direct) atIndex:30];
  drawSubjectMember(r,enc,ring,draws[i],NO);
 }
}
static void nmCompositionRaster(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u) {
 if(!ring.compositionSlotCount)return;
 MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=r.modelAtlasColor;p.colorAttachments[1].texture=r.modelAtlasEmission;p.colorAttachments[2].texture=r.modelMetadata;
 for(int i=0;i<3;i++){p.colorAttachments[i].loadAction=MTLLoadActionClear;p.colorAttachments[i].storeAction=MTLStoreActionStore;p.colorAttachments[i].clearColor=MTLClearColorMake(0,0,0,0);}
 p.depthAttachment.texture=r.modelAtlasDepth;p.depthAttachment.loadAction=MTLLoadActionClear;p.depthAttachment.storeAction=MTLStoreActionDontCare;p.depthAttachment.clearDepth=1;
 // The textures only grow; clear, raster and store just this frame's page.
 NSUInteger pageW=MIN(ring.compositionPageWidth,r.modelAtlasColor.width),pageH=MIN(ring.compositionPageHeight,r.modelAtlasColor.height);
 if(pageW&&pageH){p.renderTargetWidth=pageW;p.renderTargetHeight=pageH;u.composition.x=pageW;u.composition.y=pageH;}
 id<MTLRenderCommandEncoder> enc=nmRenderEncoder(cb,p,"composition.nmCompositionRaster#1");u.mode.z=0;u.mode.w=2;
 modelBindings(r,enc,poses,ring,u);[enc setFrontFacingWinding:MTLWindingClockwise];[enc setCullMode:MTLCullModeNone];[enc setRenderPipelineState:r.atlasModelMetadata];[enc setDepthStencilState:r.depth];
 const NMCloakDraw* draws=ring.compositionDraws.bytes;
 for(NSUInteger i=0;i<ring.compositionDrawCount;i++)if(!nmSlotDirect(ring,draws[i].group))drawSubjectMember(r,enc,ring,draws[i],NO);
 // Construction row endpoints resolve complete native pixel blocks after this pass.
 [enc endEncoding];
}
static void nmCompositionCommit(NMRenderer* r,NMRing* ring,id<MTLRenderCommandEncoder> enc,NMUniforms u,uint32_t phase) {
 const uint32_t* paint=ring.compositionPaint.contents;NSUInteger begin=0;while(begin<ring.compositionPaintCount&&paint[begin*4+1]<phase)begin++;
 NSUInteger end=begin;while(end<ring.compositionPaintCount&&paint[end*4+1]==phase)end++;if(begin==end)return;
 // With slabs, every quad is depth-tested (never written) inside its own paint
 // band, so a later-ranked direct body still covers it [DESIGN_GPU_RENDERER C-G3].
 BOOL slabs=ring.directSlotCount>0;nmSlabCommitActive=slabs;
 [enc setDepthStencilState:slabs?r.spriteDepth:nil];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];
 while(begin<end){
  BOOL sprite=paint[begin*4+3]==1;NSUInteger stop=begin+1;
  while(sprite&&stop<end&&paint[stop*4+3]==1&&paint[stop*4]==paint[stop*4-4]+1)stop++;
  // A shadow sits at the far edge of its band: behind its own body, in front of earlier ranks.
  if(slabs)nmSlabViewport(r,enc,nmSlabDepth(ring,begin,0.9999));
  if(!sprite)nmShadowsCommitSlot(r,r.shadows,ring.shadows,enc,u,paint[begin*4]);
  if(!sprite&&paint[begin*4+3]==0&&nmSlotDirect(ring,paint[begin*4])){begin=stop;continue;}
  if(slabs){nmSlabViewport(r,enc,nmSlabDepth(ring,begin,0.5));[enc setDepthStencilState:r.spriteDepth];}
  if(sprite){[enc setRenderPipelineState:r.sprite];[enc setVertexBuffer:ring.sprites offset:(paint[begin*4]-1)*96 atIndex:0];[enc setFragmentTexture:r.spriteAtlas atIndex:0];}
  else{[enc setRenderPipelineState:r.atlasCommit];[enc setVertexBuffer:ring.compositionSlots offset:0 atIndex:0];[enc setVertexBuffer:ring.compositionPaint offset:begin*16 atIndex:1];[enc setFragmentTexture:r.modelAtlasColor atIndex:0];[enc setFragmentTexture:r.modelAtlasEmission atIndex:1];nmWaterObjectsBindCommit(r,ring,enc,u);}
  [enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:stop-begin];begin=stop;
 }
 if(slabs){nmSlabViewportReset(r,enc);[enc setDepthStencilState:nil];}
 nmSlabCommitActive=NO;
}
