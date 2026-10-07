#pragma once
#import <Metal/Metal.h>
#import <simd/simd.h>

// Include after Metal headers; implementation in glow.inc is renderer-independent.
typedef struct { vector_float4 positionUV,color,params; } NMGlowVertex;
typedef struct { float weights[8]; vector_float4 blur; } NMGlowParameters;
_Static_assert(sizeof(NMGlowVertex)==48,"glow vertex transfer");
_Static_assert(sizeof(NMGlowParameters)==48,"glow parameters transfer");
@interface NMGlow : NSObject
@property id<MTLDevice> device;
@property id<MTLRenderPipelineState> source,resolve;
@property id<MTLComputePipelineState> shrink,across,down;
@property id<MTLTexture> quarter,horizontal,octaves;
@property NSUInteger width,height;
@property MTLPixelFormat destinationFormat;
@end
static NMGlow *nmGlowInitialize(id<MTLDevice> device,id<MTLLibrary> library,MTLPixelFormat destinationFormat,NSError **error);
static BOOL nmGlowPrepare(NMGlow *g,NSUInteger width,NSUInteger height);
// Vertices is caller-owned ring-buffer storage, consumed through completion.
// emission is full-size RGBA16F (can reuse r.emission); completed is the current
// world composite. destination can equal completed: resolve samples only octaves
// and screens by blending. Run after all world effects, before fog/chrome; replace
// prototype blur/composite so synthetic emission cannot be counted twice.
static BOOL nmGlowEncode(NMGlow *g,id<MTLCommandBuffer> cb,id<MTLTexture> emission,id<MTLTexture> completed,id<MTLTexture> atlas,id<MTLTexture> destination,id<MTLBuffer> vertices,NSUInteger vertexCount,NMGlowParameters parameters);
