@implementation NMTerrainTiles
@end
static BOOL nmTerrainTilesFail(NSError** error,NSString* reason){if(error)*error=[NSError errorWithDomain:@"nanometal.terrain-tiles" code:1 userInfo:@{NSLocalizedDescriptionKey:reason}];return NO;}
static id<MTLTexture> nmTerrainTilesTexture(NMTerrainTiles* state,NSUInteger w,NSUInteger h,MTLPixelFormat format,const void* pixels){
 MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:format width:w height:h mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;
 id<MTLTexture> texture=[state.device newTextureWithDescriptor:d];if(texture)[texture replaceRegion:MTLRegionMake2D(0,0,w,h) mipmapLevel:0 withBytes:pixels bytesPerRow:w*4];return texture;
}
static NMTerrainTiles* nmTerrainTilesInitialize(id<MTLDevice> device,NSError** error){
 if(!device){nmTerrainTilesFail(error,@"Missing terrain tile device");return nil;}NMTerrainTiles* state=[NMTerrainTiles new];state.device=device;
 uint32_t blank=0;state.base=nmTerrainTilesTexture(state,1,1,MTLPixelFormatRGBA8Unorm,&blank);state.detail=state.base;state.lookup=nmTerrainTilesTexture(state,1,1,MTLPixelFormatR32Uint,&blank);
 if(!state.base||!state.lookup){nmTerrainTilesFail(error,@"Terrain tile fallback allocation failed");return nil;}return state;
}
static BOOL nmTerrainTilesPrepare(NMTerrainTiles* state,const NMTerrainTilesUpload* u,NSError** error){
 if(!state)return nmTerrainTilesFail(error,@"Missing terrain tile state");
 if(!u){NMTerrainTilesParameters p=state.parameters;p.controls.x=0;state.parameters=p;return YES;}
 if(!u->base||!u->lookup||!u->map_width||!u->map_height||u->map_width>4096||u->map_height>4096||!u->base_width||!u->base_height||u->base_width%32||u->base_height%32||u->base_width>8192||u->base_height>8192)return nmTerrainTilesFail(error,@"Invalid native terrain tile upload");
 BOOL detail=u->detail!=NULL;
 if(detail&&(!u->detail_width||!u->detail_height||u->detail_width!=u->base_width*2||u->detail_height!=u->base_height*2))return nmTerrainTilesFail(error,@"Invalid detail terrain tile upload");
 if(!detail&&(u->detail_width||u->detail_height))return nmTerrainTilesFail(error,@"Detail terrain dimensions have no pixels");
 uint32_t cells=(u->base_width/32)*(u->base_height/32);
 for(NSUInteger i=0;i<(NSUInteger)u->map_width*u->map_height;i++)if(u->lookup[i]>=cells)return nmTerrainTilesFail(error,@"Terrain lookup exceeds atlas cells");
 id<MTLTexture> base=nmTerrainTilesTexture(state,u->base_width,u->base_height,MTLPixelFormatRGBA8Unorm,u->base),lookup=nmTerrainTilesTexture(state,u->map_width,u->map_height,MTLPixelFormatR32Uint,u->lookup),detailAtlas=detail?nmTerrainTilesTexture(state,u->detail_width,u->detail_height,MTLPixelFormatRGBA8Unorm,u->detail):base;
 if(!base||!lookup||!detailAtlas)return nmTerrainTilesFail(error,@"Terrain tile allocation failed");
 state.base=base;state.lookup=lookup;state.detail=detailAtlas;state.hasDetail=detail;
 state.parameters=(NMTerrainTilesParameters){(vector_uint4){u->map_width,u->map_height,u->base_width/32,detail?u->detail_width/64:0},(vector_float4){1,0,0,0}};
 state.uploadedBytes=(uint64_t)u->base_width*u->base_height*4+(uint64_t)u->map_width*u->map_height*4+(detail?(uint64_t)u->detail_width*u->detail_height*4:0);state.uploads++;return YES;
}
static BOOL nmTerrainTilesBind(NMTerrainTiles* state,id<MTLRenderCommandEncoder> encoder,BOOL filter,BOOL detail){
 if(!state||!encoder||!state.base||!state.lookup||!state.detail)return NO;NMTerrainTilesParameters p=state.parameters;p.controls.y=filter?1:0;p.controls.z=detail&&state.hasDetail?1:0;
 [encoder setFragmentTexture:state.base atIndex:6];[encoder setFragmentTexture:state.lookup atIndex:7];[encoder setFragmentTexture:state.detail atIndex:8];[encoder setFragmentBytes:&p length:sizeof(p) atIndex:25];return YES;
}
static NSDictionary* nmTerrainTilesInfo(NMTerrainTiles* state){if(!state)return @{@"terrain_tiles_enabled":@NO};return @{@"terrain_tiles_enabled":@(state.parameters.controls.x>.5),@"terrain_tiles_native_side":@32,@"terrain_tiles_detail_side":@(state.hasDetail?64:0),@"terrain_tiles_map_dimensions":@[@(state.parameters.layout.x),@(state.parameters.layout.y)],@"terrain_tiles_native_atlas_dimensions":@[@(state.base.width),@(state.base.height)],@"terrain_tiles_detail_atlas_dimensions":state.hasDetail?@[@(state.detail.width),@(state.detail.height)]:@[],@"terrain_tiles_retained_bytes":@(state.uploadedBytes),@"terrain_tiles_uploads":@(state.uploads),@"terrain_tiles_sampling":@"nearest at identity; bilinear clamped within each authored tile under fractional world transform"};}
