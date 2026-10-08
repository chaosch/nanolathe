#ifndef NANOMETAL_GROUND_EFFECTS_H
#define NANOMETAL_GROUND_EFFECTS_H
#include <stdint.h>
// cross_kind[3] indexes the frame table: marks of one recording share their
// painted-map mapping and clip, which travel once per frame entry.
typedef struct {float centre_axis[4],cross_kind[4],values[4];} NMGroundMark;
typedef struct {float mapping[4],clip[4];} NMGroundFrame;
typedef struct {const NMGroundMark* marks;uint32_t count,reserved;float ages[4];const NMGroundFrame* frames;uint32_t frame_count,reserved2;} NMGroundUpload;
#ifdef __OBJC__
@class NMGroundState,NMGroundRing;
static BOOL nmGroundInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static BOOL nmGroundDraw(NMRenderer*,NMRing*,id<MTLCommandBuffer>,const void*,uint32_t,uint32_t);
#endif
#endif
