#ifndef NANOLATHE_SHADOWS_H
#define NANOLATHE_SHADOWS_H
#include <stdint.h>
// This upload can be appended to NMLiveUpload; no existing GPU ABI changes.
typedef struct {
 uintptr_t topology,slots,subjects,body_draws,projected_draws,body_rules,piece_flags;
 uint32_t topology_count,slot_count,subject_count,body_count,projected_count,piece_count,width,height;
 float tint[4],water[4];
 // Per body draw: 0 binds the instance's resident model rules, n binds
 // body_rules[n-1]; body_rules holds only rules that differ.
 uintptr_t body_rule_slots;uint32_t body_rule_count,reserved;
} NMShadowUpload;
typedef struct {uintptr_t vertices,indices;uint32_t vertex_count,index_count;} NMShadowMeshUpload;
#ifdef __OBJC__
// Include after Foundation/Metal imports, before renderer declarations. Add
// NMShadowState* shadows to NMRenderer, NMShadowFrame* shadows to every NMRing.
@class NMRenderer;
@interface NMShadowMesh : NSObject
@property id<MTLBuffer> vertices,indices;
@property NSUInteger indexCount;
@end
@interface NMShadowState : NSObject
@property id<MTLRenderPipelineState> projected,commit;
@property NSMutableArray<NMShadowMesh*>* meshes;
@property id<MTLTexture> body,emission,depth,mask;
@end
@interface NMShadowFrame : NSObject
@property id<MTLBuffer> slots,subjects,bodyRules,pieceFlags;
@property NSMutableData* bodyDraws;
@property NSMutableData* bodyRuleSlots;
@property NSMutableData* projectedDraws;
@property NSUInteger subjectCount,bodyCount,projectedCount;
@property uint32_t width,height;
@property(nonatomic) vector_float4 tint,water;
@end
// Creation is called before the .inc definition, from nm_create.
static NMShadowState* nmShadowsCreate(NMRenderer*,id<MTLLibrary>,NSError**);
#endif
#endif
