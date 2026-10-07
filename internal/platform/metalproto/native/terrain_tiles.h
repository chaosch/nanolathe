#pragma once
#import <Metal/Metal.h>
#import <simd/simd.h>
#include <stdint.h>

typedef struct {
 const uint8_t* base;
 const uint8_t* detail;
 const uint32_t* lookup;
 uint32_t base_width,base_height,detail_width,detail_height,map_width,map_height;
} NMTerrainTilesUpload;
_Static_assert(sizeof(NMTerrainTilesUpload)==48,"terrain tile upload transfer");
typedef struct {vector_uint4 layout;vector_float4 controls;} NMTerrainTilesParameters;
_Static_assert(sizeof(NMTerrainTilesParameters)==32,"terrain tile parameters transfer");
@interface NMTerrainTiles : NSObject
@property id<MTLDevice> device;
@property id<MTLTexture> base,detail,lookup;
@property NMTerrainTilesParameters parameters;
@property BOOL hasDetail;
@property uint64_t uploadedBytes,uploads;
@end
// Include terrain_tiles.inc once. The state owns all texture bytes; Prepare is
// a loading/palette replacement operation, before rendering can use the state.
// Initialize creates typed one-texel fallbacks, so disabled bindings are valid.
static NMTerrainTiles* nmTerrainTilesInitialize(id<MTLDevice> device,NSError** error);
static BOOL nmTerrainTilesPrepare(NMTerrainTiles* state,const NMTerrainTilesUpload* upload,NSError** error);
// Slots6/7/8 are RGBA8 native atlas, R32Uint lookup and RGBA8 detail atlas.
// Buffer25 contains layout+selection. Detail is selected only if supplied;
// filter matches production's world-transform filter gate, not water filtering.
static BOOL nmTerrainTilesBind(NMTerrainTiles* state,id<MTLRenderCommandEncoder> encoder,BOOL filter,BOOL detail);
static NSDictionary* nmTerrainTilesInfo(NMTerrainTiles* state);
