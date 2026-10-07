#ifndef NANOMETAL_FACE_REFLECTIONS_H
#define NANOMETAL_FACE_REFLECTIONS_H
#include <stdint.h>
typedef struct {uint32_t instance,face_base,face_count,reserved;} NMFaceReflectionRecord;
#ifdef __OBJC__
@class NMFaceReflectionState,NMFaceReflectionFrame;
static BOOL nmFaceReflectionsInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static BOOL nmFaceReflectionsPrepare(NMRenderer*,NMRing*);
static BOOL nmFaceReflectionsEncode(NMRenderer*,NMRing*,id<MTLCommandBuffer>,id<MTLBuffer> water_objects,uint32_t object_count);
static void nmFaceReflectionsBind(NMRing*,id<MTLRenderCommandEncoder>);
// Read only after this ring's command buffer completes (normally final drain).
// This is diagnostic observation, never an admission/prepare dependency.
static NSDictionary* nmFaceReflectionsInfo(NMRing*);
#endif
#endif
