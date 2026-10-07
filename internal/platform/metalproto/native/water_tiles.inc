@implementation NMWaterTilesState
- (instancetype)init {if((self=[super init]))_enabled=YES;return self;}
@end
@implementation NMWaterTilesFrame
@end
static BOOL nmWaterTilesFail(NSError** error,NSString* reason){
 if(error)*error=[NSError errorWithDomain:@"nanometal.water-tiles" code:1 userInfo:@{NSLocalizedDescriptionKey:reason}];return NO;
}
static NMWaterTilesState* nmWaterTilesInitialize(id<MTLDevice> device,id<MTLLibrary> library,NSError** error){
 if(!device||!library){nmWaterTilesFail(error,@"Missing reflection tile device or library");return nil;}
 id<MTLFunction> function=[library newFunctionWithName:@"nm_water_tiles_classify"];
 if(!function){nmWaterTilesFail(error,@"Missing reflection tile classification function");return nil;}
 NMWaterTilesState* state=[NMWaterTilesState new];state.device=device;state.classify=[device newComputePipelineStateWithFunction:function error:error];
 if(!state.classify)return nil;
 if(state.classify.maxTotalThreadsPerThreadgroup<NMWaterTileThreads){nmWaterTilesFail(error,@"Reflection tile classification needs 128 threads per group");return nil;}return state;
}
static BOOL nmWaterTilesEncode(NMWaterTilesState* state,NMWaterTilesFrame* frame,id<MTLCommandBuffer> cb,id<MTLTexture> color,id<MTLTexture> soft,NSError** error){
 if(frame)frame.encoded=NO;
 if(!state||!frame||!cb||!color||!soft||color.textureType!=MTLTextureType2D||soft.textureType!=MTLTextureType2D||color.pixelFormat!=MTLPixelFormatRGBA8Unorm||soft.pixelFormat!=MTLPixelFormatRGBA8Unorm||!color.width||!color.height||color.width!=soft.width||color.height!=soft.height||color.device!=state.device||soft.device!=state.device||cb.commandQueue.device!=state.device)return nmWaterTilesFail(error,@"Invalid reflection tile source planes");
 NSUInteger columns=(color.width+NMWaterTileSide-1)/NMWaterTileSide,rows=(color.height+NMWaterTileSide-1)/NMWaterTileSide;
 if(rows>state.device.maxBufferLength/(sizeof(uint32_t)*columns))return nmWaterTilesFail(error,@"Reflection tile buffer exceeds device limit");
 NSUInteger bytes=columns*rows*sizeof(uint32_t);
 if(!frame.flags||frame.flags.length<bytes)frame.flags=[state.device newBufferWithLength:bytes options:MTLResourceStorageModeShared];
 if(!frame.flags)return nmWaterTilesFail(error,@"Reflection tile allocation failed");
 if(state.enabled){
  id<MTLComputeCommandEncoder> encoder=nmComputeEncoder(cb,"water_tiles.nmWaterTilesEncode#1");if(!encoder)return nmWaterTilesFail(error,@"Reflection tile compute encoder failed");
  [encoder setComputePipelineState:state.classify];[encoder setTexture:color atIndex:0];[encoder setTexture:soft atIndex:1];[encoder setBuffer:frame.flags offset:0 atIndex:0];
  [encoder dispatchThreadgroups:MTLSizeMake(columns,rows,1) threadsPerThreadgroup:MTLSizeMake(NMWaterTileThreads,1,1)];[encoder endEncoding];state.classifiedTiles+=columns*rows;
 }else{
  // The ring is acquired, so no preceding GPU reader/writer still owns it.
  // Write WORD value 3, not byte-fill 3, to preserve the shader's bit contract.
  uint32_t* flags=frame.flags.contents;for(NSUInteger i=0;i<columns*rows;i++)flags[i]=3;state.bypassFrames++;
 }
 frame.width=color.width;frame.height=color.height;frame.columns=columns;frame.rows=rows;frame.encoded=YES;state.encodedFrames++;return YES;
}
static BOOL nmWaterTilesBind(NMWaterTilesFrame* frame,id<MTLRenderCommandEncoder> encoder){
 if(!frame||!frame.encoded||!frame.flags||!encoder)return NO;[encoder setFragmentBuffer:frame.flags offset:0 atIndex:23];return YES;
}
static NSDictionary* nmWaterTilesInfo(NMWaterTilesState* state,NMWaterTilesFrame* frame){
 if(!state)return @{@"water_reflection_tiles_enabled":@NO};
 return @{@"water_reflection_tiles_enabled":@(state.enabled),@"water_reflection_full_filter_frames":@(state.bypassFrames),@"water_reflection_tile_side_physical":@(NMWaterTileSide),@"water_reflection_tile_frames":@(state.encodedFrames),@"water_reflection_tiles_classified":@(state.classifiedTiles),@"water_reflection_tile_columns":@(frame.columns),@"water_reflection_tile_rows":@(frame.rows),@"water_reflection_tile_buffer_bytes":@(frame.flags.length),@"water_reflection_tile_content_rule":@"any RGBA channel > 0",@"water_reflection_tile_soft_rule":@"soft.r > 0"};
}
