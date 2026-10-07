// Included by bridge.m after renderer/ring interfaces and resource helpers.
// No global CPU/GPU storage: the renderer retains immutable atlas resources;
// only an acquired ring's staging, geometry and lens snapshot are rewritten.
static id<MTLBuffer> grow(NMRenderer*,id<MTLBuffer>,NSUInteger);
_Static_assert(sizeof(NMEffectsUpload)==152,"effects upload layout");
_Static_assert(sizeof(NMEffectsSmoke)==96,"smoke receiver layout");
_Static_assert(sizeof(NMEffectsOp)==16,"effects op layout");
_Static_assert(sizeof(NMEffectsLayer)==16,"effects layer layout");
_Static_assert(sizeof(NMEffectsLens)==32,"effects lens layout");
_Static_assert(sizeof(NMEffectsSample)==48,"effects sample layout");
_Static_assert(sizeof(NMEffectsVertex)==48,"effects vertex layout");

@interface NMEffectsState : NSObject
@property id<MTLRenderPipelineState> color,multiply,additive,triangles,lens;
@property id<MTLRenderPipelineState> smoke;
@property id<MTLComputePipelineState> selectSmoke;
@property id<MTLBuffer> emptyLights;
@property id<MTLTexture> atlas;
@property uint64_t version,atlasUploads,atlasBytes,preparedFrames,drawnLayers,emptyLayers,drawnLenses,emptyLenses,drawnModels;
@property uint32_t modelSubmitted,modelSuppressed,projectileModels,sourceCount,quadSpanFaces,supersampleModels;
@property BOOL atlasNeedsFullUpload;
@property (nonatomic) NMEffectsRetainedDraw retainedDrawer;
@property (nonatomic) NMEffectsGroundDraw groundDrawer;
@property uint64_t retainedSubmitted,retainedSuppressed;
@property uint64_t groundMarkerSuppressed;
@property uint32_t retainedBodyMarkers,retainedShadowMarkers;
@property uint64_t smokeReceiversDrawn;
@end
@implementation NMEffectsState
@end

@interface NMEffectsRing : NSObject
@property id<MTLBuffer> quads,vertices,samples,atlasUpload;
@property id<MTLBuffer> smoke,smokeSubjects,lights;
@property uint32_t smokeCount,lightCount;
@property NSMutableData *lenses,*ops,*layers;
@property id<MTLTexture> atlas,snapshot;
@property NSUInteger quadCount,vertexCount,sampleCount,lensCount,layerCount;
@property NSUInteger uploadX,uploadY,uploadW,uploadH,uploadRowBytes;
@property uint64_t version;
@property BOOL uploadPending;
@end
@implementation NMEffectsRing
@end

static id<MTLRenderPipelineState> nmEffectsPipeline(NMRenderer* r,id<MTLLibrary> lib,NSString* vertex,NSString* fragment,int blend,NSError** error) {
 MTLRenderPipelineDescriptor* d=[MTLRenderPipelineDescriptor new];d.vertexFunction=[lib newFunctionWithName:vertex];d.fragmentFunction=[lib newFunctionWithName:fragment];
 MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.pixelFormat=MTLPixelFormatRGBA16Float;
 if(blend>=0){a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=blend==2?MTLBlendFactorOne:MTLBlendFactorOneMinusSourceAlpha;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;
  if(blend==1){a.sourceRGBBlendFactor=MTLBlendFactorZero;a.destinationRGBBlendFactor=MTLBlendFactorSourceColor;a.sourceAlphaBlendFactor=MTLBlendFactorZero;a.destinationAlphaBlendFactor=MTLBlendFactorOne;}
 }
 return [r.device newRenderPipelineStateWithDescriptor:d error:error];
}

static BOOL nmEffectsInitialize(NMRenderer* r,id<MTLLibrary> lib,NSError** error) {
 NMEffectsState* s=[NMEffectsState new];
 s.color=nmEffectsPipeline(r,lib,@"nm_fx_quad_vertex",@"nm_fx_color_fragment",0,error);
 s.multiply=nmEffectsPipeline(r,lib,@"nm_fx_quad_vertex",@"nm_fx_multiply_fragment",1,error);
 s.additive=nmEffectsPipeline(r,lib,@"nm_fx_quad_vertex",@"nm_fx_color_fragment",2,error);
 s.triangles=nmEffectsPipeline(r,lib,@"nm_fx_triangle_vertex",@"nm_fx_color_fragment",0,error);
 s.lens=nmEffectsPipeline(r,lib,@"nm_fx_lens_vertex",@"nm_fx_lens_fragment",-1,error);
 s.smoke=nmEffectsPipeline(r,lib,@"nm_fx_smoke_vertex",@"nm_fx_smoke_fragment",0,error);
 s.selectSmoke=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"nm_fx_select_smoke_lights"] error:error];
 s.emptyLights=[r.device newBufferWithLength:64 options:MTLResourceStorageModeShared];
 if(!s.color||!s.multiply||!s.additive||!s.triangles||!s.lens||!s.smoke||!s.selectSmoke||!s.emptyLights)return NO;
 r.effectState=s;return YES;
}
static void nmEffectsSetRetainedDrawer(NMRenderer* r,NMEffectsRetainedDraw draw){r.effectState.retainedDrawer=draw;}
static void nmEffectsSetGroundDrawer(NMRenderer* r,NMEffectsGroundDraw draw){r.effectState.groundDrawer=draw;}
static void nmEffectsSetLighting(NMRing* ring,id<MTLBuffer> lights,uint32_t count){NMEffectsRing* f=ring.effectRing;if(!f)return;f.lights=lights;f.lightCount=lights?MIN(count,64u):0;}

static BOOL nmEffectsFail(NMRenderer* r,NSString* reason){r.lastError=reason;return NO;}
static BOOL nmEffectsPrepare(NMRenderer* r,NMRing* ring,const NMEffectsUpload* u) {
 NMEffectsRing* f=ring.effectRing;
 if(!u){if(f){f.layerCount=0;f.uploadPending=NO;f.smokeCount=f.lightCount=0;f.lights=nil;}return YES;}
 if(!r.effectState)return nmEffectsFail(r,@"Stock-effects pipelines are not initialized");
 if(!f){f=[NMEffectsRing new];ring.effectRing=f;}
 f.layerCount=0;f.uploadPending=NO;f.lights=nil;f.lightCount=0;f.smokeCount=0;
 const uint32_t counts[]={u->quad_count,u->vertex_count,u->sample_count,u->lens_count,u->op_count,u->layer_count};
 const void* pointers[]={u->quads,u->vertices,u->samples,u->lenses,u->ops,u->layers};
 for(int i=0;i<6;i++)if(counts[i]>INT32_MAX||(counts[i]&&!pointers[i]))return nmEffectsFail(r,@"Invalid stock-effects upload span");
 if(u->smoke_count>INT32_MAX||(u->smoke_count&&!u->smoke))return nmEffectsFail(r,@"Invalid smoke receiver upload span");
 if(!u->atlas||!u->width||!u->height||u->width>INT32_MAX||u->height>INT32_MAX)return nmEffectsFail(r,@"Invalid stock-effects atlas");
 const NMEffectsLayer* layers=u->layers;const NMEffectsOp* ops=u->ops;const NMEffectsLens* lenses=u->lenses;
 for(NSUInteger i=0;i<u->layer_count;i++)if((uint64_t)layers[i].first+layers[i].count>u->op_count)return nmEffectsFail(r,@"Stock-effects layer exceeds op storage");
 for(NSUInteger i=0;i<u->op_count;i++){NMEffectsOp op=ops[i];uint32_t bound=op.kind==0?u->quad_count:op.kind==1?u->lens_count:op.kind==2?u->vertex_count:op.kind==4?INT32_MAX:op.kind==5?u->smoke_count:u->projectile_models;
  if(op.kind>5||(uint64_t)op.first+op.count>bound||(op.kind==2&&op.count%3)||(op.kind==3&&(op.reserved>2||op.count!=1)))return nmEffectsFail(r,@"Invalid ordered stock-effects command");
 }
 for(NSUInteger i=0;i<u->lens_count;i++)if((uint64_t)lenses[i].first+lenses[i].count>u->sample_count)return nmEffectsFail(r,@"Stock-effects lens exceeds sample storage");
 NSUInteger qb=(NSUInteger)u->quad_count*64,vb=(NSUInteger)u->vertex_count*sizeof(NMEffectsVertex),sb=(NSUInteger)u->sample_count*sizeof(NMEffectsSample);
 if(qb>r.device.maxBufferLength||vb>r.device.maxBufferLength||sb>r.device.maxBufferLength)return nmEffectsFail(r,@"Stock-effects device buffer limit");
 f.quads=grow(r,f.quads,qb);f.vertices=grow(r,f.vertices,vb);f.samples=grow(r,f.samples,sb);
 if(!f.quads||!f.vertices||!f.samples)return nmEffectsFail(r,@"Stock-effects buffer allocation failed");
 if(qb)memcpy(f.quads.contents,u->quads,qb);if(vb)memcpy(f.vertices.contents,u->vertices,vb);if(sb)memcpy(f.samples.contents,u->samples,sb);
 NSUInteger smokeBytes=(NSUInteger)u->smoke_count*96,subjectBytes=(NSUInteger)u->smoke_count*36;
 if(smokeBytes>r.device.maxBufferLength||subjectBytes>r.device.maxBufferLength)return nmEffectsFail(r,@"Smoke receiver device buffer limit");
 f.smoke=grow(r,f.smoke,MAX(smokeBytes,96));f.smokeSubjects=grow(r,f.smokeSubjects,MAX(subjectBytes,36));if(!f.smoke||!f.smokeSubjects)return nmEffectsFail(r,@"Smoke receiver allocation failed");
 if(u->smoke_count)memcpy(f.smoke.contents,u->smoke,u->smoke_count*96);f.smokeCount=u->smoke_count;
 if(!f.lenses)f.lenses=[NSMutableData new];if(!f.ops)f.ops=[NSMutableData new];if(!f.layers)f.layers=[NSMutableData new];
 f.lenses.length=(NSUInteger)u->lens_count*sizeof(NMEffectsLens);f.ops.length=(NSUInteger)u->op_count*sizeof(NMEffectsOp);f.layers.length=(NSUInteger)u->layer_count*sizeof(NMEffectsLayer);
 if(f.lenses.length)memcpy(f.lenses.mutableBytes,u->lenses,f.lenses.length);if(f.ops.length)memcpy(f.ops.mutableBytes,u->ops,f.ops.length);if(f.layers.length)memcpy(f.layers.mutableBytes,u->layers,f.layers.length);
 NMEffectsState* s=r.effectState;BOOL resize=s.atlas.width!=u->width||s.atlas.height!=u->height;
 if(resize){s.atlas=target(r,u->width,u->height,MTLPixelFormatRGBA8Unorm,MTLTextureUsageShaderRead);if(!s.atlas)return nmEffectsFail(r,@"Stock-effects atlas allocation failed");s.atlasNeedsFullUpload=YES;}
 f.atlas=s.atlas;f.version=u->version;
 if(s.atlasNeedsFullUpload||s.version!=u->version){
  NSUInteger x=u->dirty[0],y=u->dirty[1],w=u->dirty[2],h=u->dirty[3];
  if(s.atlasNeedsFullUpload||!w||!h){x=y=0;w=u->width;h=u->height;}
  if((uint64_t)x+w>u->width||(uint64_t)y+h>u->height)return nmEffectsFail(r,@"Stock-effects atlas dirty rectangle out of bounds");
  NSUInteger rowBytes=(w*4+255)&~(NSUInteger)255;if(rowBytes*h>r.device.maxBufferLength)return nmEffectsFail(r,@"Stock-effects upload buffer limit");
  f.atlasUpload=grow(r,f.atlasUpload,rowBytes*h);if(!f.atlasUpload)return nmEffectsFail(r,@"Stock-effects staging allocation failed");
  const uint8_t* source=u->atlas;uint8_t* staging=f.atlasUpload.contents;
  for(NSUInteger row=0;row<h;row++)memcpy(staging+row*rowBytes,source+((y+row)*u->width+x)*4,w*4);
  f.uploadX=x;f.uploadY=y;f.uploadW=w;f.uploadH=h;f.uploadRowBytes=rowBytes;f.uploadPending=YES;
 }
 if(u->lens_count&&(f.snapshot.width!=r.color.width||f.snapshot.height!=r.color.height)){
  f.snapshot=target(r,r.color.width,r.color.height,MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead);
  if(!f.snapshot)return nmEffectsFail(r,@"Stock-effects lens snapshot allocation failed");
 }
 f.quadCount=u->quad_count;f.vertexCount=u->vertex_count;f.sampleCount=u->sample_count;f.lensCount=u->lens_count;f.layerCount=u->layer_count;
 s.modelSubmitted=u->model_submitted;s.modelSuppressed=u->model_suppressed;s.projectileModels=u->projectile_models;s.sourceCount=u->source_count;s.quadSpanFaces=u->quad_span_faces;s.supersampleModels=u->supersample_models;s.preparedFrames++;
 s.retainedBodyMarkers=s.retainedShadowMarkers=0;for(NSUInteger i=0;i<u->op_count;i++)if(ops[i].kind==3){if(ops[i].reserved&1)s.retainedShadowMarkers++;else s.retainedBodyMarkers++;}
 return YES;
}

static void nmEffectsBegin(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb) {
 NMEffectsRing* f=ring.effectRing;if(!f)return;
 if(f.smokeCount){uint32_t lightCount=f.lightCount;id<MTLComputeCommandEncoder> e=[cb computeCommandEncoder];[e setComputePipelineState:r.effectState.selectSmoke];[e setBuffer:f.smoke offset:0 atIndex:0];[e setBuffer:f.lights?:r.effectState.emptyLights offset:0 atIndex:16];[e setBuffer:f.smokeSubjects offset:0 atIndex:17];[e setBytes:&lightCount length:sizeof(lightCount) atIndex:18];NSUInteger group=MIN((NSUInteger)64,r.effectState.selectSmoke.maxTotalThreadsPerThreadgroup);[e dispatchThreads:MTLSizeMake(f.smokeCount,1,1) threadsPerThreadgroup:MTLSizeMake(group,1,1)];[e endEncoding];}
 if(!f.uploadPending)return;
 id<MTLBlitCommandEncoder> blit=[cb blitCommandEncoder];
 [blit copyFromBuffer:f.atlasUpload sourceOffset:0 sourceBytesPerRow:f.uploadRowBytes sourceBytesPerImage:f.uploadRowBytes*f.uploadH sourceSize:MTLSizeMake(f.uploadW,f.uploadH,1) toTexture:f.atlas destinationSlice:0 destinationLevel:0 destinationOrigin:MTLOriginMake(f.uploadX,f.uploadY,0)];
 [blit endEncoding];f.uploadPending=NO;r.effectState.version=f.version;r.effectState.atlasNeedsFullUpload=NO;r.effectState.atlasUploads++;r.effectState.atlasBytes+=(uint64_t)f.uploadW*f.uploadH*4;
}

static id<MTLRenderCommandEncoder> nmEffectsEncoder(NMRenderer* r,id<MTLCommandBuffer> cb,NMUniforms u) {
 MTLRenderPassDescriptor* pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.color;pass.colorAttachments[0].loadAction=MTLLoadActionLoad;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setCullMode:MTLCullModeNone];[enc setVertexBytes:&u.viewport length:sizeof(u.viewport) atIndex:3];return enc;
}

static void nmEffectsDraw(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,NSUInteger layerIndex,NMUniforms u) {
 NMEffectsRing* f=ring.effectRing;NMEffectsState* s=r.effectState;if(!f||!s||layerIndex>=f.layerCount)return;
 const NMEffectsLayer* layers=f.layers.bytes;NMEffectsLayer layer=layers[layerIndex];if(!layer.count){s.emptyLayers++;return;}s.drawnLayers++;
 const NMEffectsOp* ops=f.ops.bytes;const NMEffectsLens* lenses=f.lenses.bytes;const float* quads=f.quads.contents;
 id<MTLRenderCommandEncoder> enc=nil;
 for(NSUInteger i=layer.first;i<(NSUInteger)layer.first+layer.count;i++){
  NMEffectsOp op=ops[i];if(!op.count)continue;
  if(op.kind==4){if(enc){[enc endEncoding];enc=nil;}if(!s.groundDrawer||!s.groundDrawer(r,ring,cb,&u,op.first,op.count))s.groundMarkerSuppressed+=op.count;continue;}
  if(op.kind==3){
   if(enc){[enc endEncoding];enc=nil;}
   if(s.retainedDrawer&&s.retainedDrawer(r,ring,cb,&u,op.first,op.reserved)){s.retainedSubmitted++;}else{s.retainedSuppressed++;}
   continue;
  }
  if(op.kind==1){
   if(enc){[enc endEncoding];enc=nil;}
   for(NSUInteger j=op.first;j<(NSUInteger)op.first+op.count;j++){
    NMEffectsLens l=lenses[j];
    NSInteger x=MAX(0,(NSInteger)floorf(l.read[0])),y=MAX(0,(NSInteger)floorf(l.read[1]));
    NSInteger ex=MIN((NSInteger)r.color.width,(NSInteger)ceilf(l.read[0]+l.read[2])),ey=MIN((NSInteger)r.color.height,(NSInteger)ceilf(l.read[1]+l.read[3]));
    if(!l.count||x>=ex||y>=ey){s.emptyLenses++;continue;}
    // Each lens reads its own earlier composite. The snapshot is ring-owned;
    // the same command buffer orders the copy before this lens's keyed writes.
    id<MTLBlitCommandEncoder> blit=[cb blitCommandEncoder];[blit copyFromTexture:r.color sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(x,y,0) sourceSize:MTLSizeMake(ex-x,ey-y,1) toTexture:f.snapshot destinationSlice:0 destinationLevel:0 destinationOrigin:MTLOriginMake(x,y,0)];[blit endEncoding];
    id<MTLRenderCommandEncoder> lens=nmEffectsEncoder(r,cb,u);[lens setRenderPipelineState:s.lens];[lens setVertexBuffer:f.samples offset:0 atIndex:0];[lens setFragmentTexture:f.snapshot atIndex:0];[lens drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:l.count baseInstance:l.first];[lens endEncoding];s.drawnLenses++;
   }
   continue;
  }
  if(!enc)enc=nmEffectsEncoder(r,cb,u);
  [enc setFragmentTexture:f.atlas atIndex:0];
  if(op.kind==5){[enc setRenderPipelineState:s.smoke];[enc setVertexBuffer:f.smoke offset:0 atIndex:0];[enc setVertexBuffer:f.lights?:s.emptyLights offset:0 atIndex:16];[enc setVertexBuffer:f.smokeSubjects offset:0 atIndex:17];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:op.count baseInstance:op.first];s.smokeReceiversDrawn+=op.count;continue;}
  if(op.kind==2){[enc setRenderPipelineState:s.triangles];[enc setVertexBuffer:f.vertices offset:0 atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:op.first vertexCount:op.count];s.drawnModels++;continue;}
  [enc setVertexBuffer:f.quads offset:0 atIndex:0];
  for(NSUInteger j=op.first;j<(NSUInteger)op.first+op.count;){
   float mode=quads[j*16+13];NSUInteger end=j+1;while(end<(NSUInteger)op.first+op.count&&quads[end*16+13]==mode)end++;
   [enc setRenderPipelineState:mode==1?s.multiply:mode==2?s.additive:s.color];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:end-j baseInstance:j];j=end;
  }
 }
 if(enc)[enc endEncoding];
}

// The bridge may merge this dictionary into nm_info's ordinary renderer report.
static NSDictionary* nmEffectsInfo(NMRenderer* r) {
 NMEffectsState* s=r.effectState;if(!s)return @{@"stock_effects_enabled":@NO};
 return @{@"stock_effects_enabled":@(s.preparedFrames!=0),@"stock_effects_prepared_frames":@(s.preparedFrames),@"stock_effects_drawn_layers":@(s.drawnLayers),@"stock_effects_empty_layers":@(s.emptyLayers),@"stock_effects_drawn_lenses":@(s.drawnLenses),@"stock_effects_empty_lenses":@(s.emptyLenses),@"stock_effects_drawn_models":@(s.drawnModels),@"stock_effects_prepared_models":@(s.modelSubmitted),@"stock_effects_suppressed_models":@(s.modelSuppressed),@"stock_effects_delegated_projectile_models":@(s.projectileModels),@"stock_effects_retained_body_markers":@(s.retainedBodyMarkers),@"stock_effects_retained_shadow_markers":@(s.retainedShadowMarkers),@"stock_effects_retained_markers_submitted":@(s.retainedSubmitted),@"stock_effects_retained_markers_suppressed":@(s.retainedSuppressed),@"stock_effects_source_count":@(s.sourceCount),@"stock_effects_fan_quad_faces":@(s.quadSpanFaces),@"stock_effects_unused_supersample_models":@(s.supersampleModels),@"stock_effects_atlas_version":@(s.version),@"stock_effects_atlas_uploads":@(s.atlasUploads),@"stock_effects_atlas_uploaded_bytes":@(s.atlasBytes),@"stock_effects_ground_markers_suppressed":@(s.groundMarkerSuppressed),@"stock_effects_smoke_receivers_drawn":@(s.smokeReceiversDrawn),@"stock_effects_bloom_submitted":@NO};
}
