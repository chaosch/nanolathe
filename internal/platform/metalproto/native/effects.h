#ifndef NANOMETAL_EFFECTS_H
#define NANOMETAL_EFFECTS_H
#include <stdint.h>

// All pointers are copied synchronously into the acquired renderer ring.
typedef struct { uint32_t kind,first,count,reserved; } NMEffectsOp;
typedef struct { uint32_t first,count,reserved[2]; } NMEffectsLayer;
typedef struct { float rect[4],source[4],key[4]; } NMEffectsSample;
typedef struct { uint32_t first,count;float read[4];uint32_t reserved[2]; } NMEffectsLens;
typedef struct { float position_uv[4],color[4],bounds[4]; } NMEffectsVertex;
typedef struct {
 const void *quads,*vertices,*samples,*lenses,*ops,*layers,*atlas;
 uint64_t version;
 uint32_t quad_count,vertex_count,sample_count,lens_count,op_count,layer_count,width,height;
 uint32_t dirty[4];
 uint32_t model_submitted,model_suppressed,projectile_models,source_count,quad_span_faces,supersample_models;
} NMEffectsUpload;

#ifdef __OBJC__
@class NMRenderer,NMRing,NMEffectsState,NMEffectsRing;
// The callback owns its render encoders and closes them before returning.
// Uniforms points to the bridge's current NMUniforms. flags bit 0 is ShadowOnly;
// shadows never advance the frame-global projectile BODY slot.
// Return YES only when the marker actually submitted a body/shadow, so an
// intentional ShadowOnly skip remains an explicit suppressed-marker count.
typedef BOOL (*NMEffectsRetainedDraw)(NMRenderer*,NMRing*,id<MTLCommandBuffer>,const void* uniforms,uint32_t slot,uint32_t flags);
typedef BOOL (*NMEffectsGroundDraw)(NMRenderer*,NMRing*,id<MTLCommandBuffer>,const void* uniforms,uint32_t first,uint32_t count);
// The bridge needs this declaration before nm_create; include the implementation
// after NMRenderer/NMRing, target/grow/modelBindings and before encode.
static BOOL nmEffectsInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static void nmEffectsSetRetainedDrawer(NMRenderer*,NMEffectsRetainedDraw);
static void nmEffectsSetGroundDrawer(NMRenderer*,NMEffectsGroundDraw);
#endif
#endif
