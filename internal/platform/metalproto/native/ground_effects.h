#ifndef NANOMETAL_GROUND_EFFECTS_H
#define NANOMETAL_GROUND_EFFECTS_H
#include <stdint.h>
typedef struct {float centre_axis[4],cross_kind[4],values[4],mapping[4],clip[4];} NMGroundMark;
typedef struct {const NMGroundMark* marks;uint32_t count,reserved;float ages[4];} NMGroundUpload;
#ifdef __OBJC__
@class NMGroundState,NMGroundRing;
static BOOL nmGroundInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static BOOL nmGroundDraw(NMRenderer*,NMRing*,id<MTLCommandBuffer>,const void*,uint32_t,uint32_t);
#endif
#endif
