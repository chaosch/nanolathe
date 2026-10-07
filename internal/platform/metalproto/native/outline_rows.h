#ifndef NANOMETAL_OUTLINE_ROWS_H
#define NANOMETAL_OUTLINE_ROWS_H
#include <stdint.h>
typedef struct {uint32_t start,count,piece,corner_offset;} NMOutlineRing;
typedef struct {const NMOutlineRing* rings;uint32_t ring_count,reserved;} NMOutlineRowsUpload;
int nm_mesh_outline_rows(void*,uint32_t,const NMOutlineRowsUpload*);
#ifdef __OBJC__
@class NMRenderer,NMRing;
@interface NMOutlineRowsMesh : NSObject
@property id<MTLBuffer> rings;
@property NSUInteger ringCount,cornerCount;
@end
@interface NMOutlineRowsState : NSObject
@property id<MTLComputePipelineState> prepare,resolve;
@property uint64_t completedFrames,unsupportedRings,unsupportedFrames;
@end
@interface NMOutlineRowsRing : NSObject
@property id<MTLBuffer> corners,prepared,diagnostics;
@property NSMutableData* subjects;
@property NSUInteger instanceCount;
@end
static BOOL nmOutlineRowsInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
// Call exactly once per submitted command completion, before ring release.
static void nmOutlineRowsCompleted(NMRenderer*,NMRing*);
static NSDictionary* nmOutlineRowsInfo(NMRenderer*,NMRing*);
static BOOL nmOutlineRowsPrepare(NMRenderer*,NMRing*);
// Encode AFTER body atlas raster, BEFORE group merge. Replaces atlas line draws.
static BOOL nmOutlineRowsEncode(NMRenderer*,NMRing*,id<MTLCommandBuffer>,id<MTLBuffer>,NMUniforms);
#endif
#endif
