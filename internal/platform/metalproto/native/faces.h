#ifndef NANOMETAL_FACES_H
#define NANOMETAL_FACES_H
#include <stdint.h>
typedef struct {uint32_t corner_start,corner_count,piece,material,index_start,index_count,primitive,flags;} NMFace;
typedef struct {const NMFace* faces;const void* corners;const uint32_t* indices;uint32_t face_count,corner_count,index_count,reserved;} NMFaceUpload;
// Prepared records are 128 bytes: x/y/key/SHD int4s, slope int4, UV float4,
// pixel-transform float4, and control uint4. See faces.metal for field meanings.
int nm_mesh_faces(void* renderer,uint32_t mesh_index,const NMFaceUpload* upload);
#ifdef __OBJC__
@class NMRenderer,NMRing,NMFaceState,NMFaceMesh,NMFaceRing;
static BOOL nmFacesInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static BOOL nmFacesPrepare(NMRenderer*,NMRing*,uint32_t instance_count);
static BOOL nmFacesEncode(NMRenderer*,NMRing*,id<MTLCommandBuffer>,id<MTLBuffer> poses,NMUniforms);
// Bind bases with the same instance offset as offsets/visuals on EVERY model
// draw, including selectors and per-subject draws. Slot 21 is frame-wide.
static void nmFacesBind(NMRing*,id<MTLRenderCommandEncoder>,NSUInteger instance_start);
#endif
#endif
