#pragma once
#import <Metal/Metal.h>

// Independent state, owned by the renderer and by each acquired frame ring.
// Include water_tiles.inc once after this header. No renderer properties are
// accessed by the module. Tiles are 64 PHYSICAL source-plane pixels per side.
enum { NMWaterTileSide=64, NMWaterTileThreads=128 };
@interface NMWaterTilesState : NSObject
@property id<MTLDevice> device;
@property id<MTLComputePipelineState> classify;
@property uint64_t encodedFrames,classifiedTiles;
@end
@interface NMWaterTilesFrame : NSObject
@property id<MTLBuffer> flags;
@property NSUInteger width,height,columns,rows;
@property BOOL encoded;
@end
static NMWaterTilesState* nmWaterTilesInitialize(id<MTLDevice> device,id<MTLLibrary> library,NSError** error);
// Call only after acquiring the ring and ending the RGBA8 color/soft source
// encoder. This dispatch overwrites EVERY tile; no CPU/GPU clear is required.
// Buffer storage remains ring-owned until command-buffer completion. Ordinary
// tracked-resource hazards order source render -> compute -> fragment reads.
static BOOL nmWaterTilesEncode(NMWaterTilesState* state,NMWaterTilesFrame* frame,id<MTLCommandBuffer> cb,id<MTLTexture> color,id<MTLTexture> soft,NSError** error);
// Bind after Encode, on the same command buffer, before reflection resolve.
// The fragment signature uses device const uint* tileFlags [[buffer(23)]].
static BOOL nmWaterTilesBind(NMWaterTilesFrame* frame,id<MTLRenderCommandEncoder> encoder);
static NSDictionary* nmWaterTilesInfo(NMWaterTilesState* state,NMWaterTilesFrame* frame);
