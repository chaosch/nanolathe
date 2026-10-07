#ifndef NANOMETAL_WATER_OBJECTS_H
#define NANOMETAL_WATER_OBJECTS_H
#include <stdint.h>
typedef struct {float params[4];} NMWaterObject;
typedef struct {const NMWaterObject* objects;const uint32_t* seabed;uint32_t object_count,seabed_count;float controls[4];} NMWaterObjectsUpload;
typedef struct {float position_height[4],uv[4],color[4],bounds[4];} NMWaterSourceVertex;
typedef struct {const NMWaterSourceVertex* sources;uint32_t vertex_count,reserved;} NMWaterSourcesUpload;
#ifdef __OBJC__
@class NMRenderer,NMRing,NMWaterObjectsState,NMWaterObjectsRing;
static BOOL nmWaterObjectsInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static BOOL nmWaterObjectsPrepare(NMRenderer*,NMRing*,const NMWaterObjectsUpload*);
static BOOL nmWaterObjectsPrepareSources(NMRenderer*,NMRing*,const NMWaterSourcesUpload*);
static BOOL nmWaterObjectsReflections(NMRenderer*,NMRing*,id<MTLCommandBuffer>,id<MTLBuffer>,NMUniforms);
static BOOL nmWaterObjectsSurface(NMRenderer*,NMRing*,id<MTLCommandBuffer>,NMUniforms);
static void nmWaterObjectsBindCommit(NMRenderer*,NMRing*,id<MTLRenderCommandEncoder>,NMUniforms);
static void nmWaterObjectsSeabed(NMRenderer*,NMRing*,id<MTLRenderCommandEncoder>,NMUniforms);
#endif
#endif
