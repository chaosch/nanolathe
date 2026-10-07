#ifndef NANOMETAL_GROUPS_H
#define NANOMETAL_GROUPS_H
#include <stdint.h>
typedef struct {uint32_t info[4],counts[4];} NMGroup;
typedef struct {uint32_t instance,slot;int32_t key_delta;uint32_t reserved;} NMGroupMember;
typedef struct {const NMGroup* groups;const NMGroupMember* members;const int32_t* instance_groups;uint32_t group_count,member_count,instance_count,reserved;} NMGroupUpload;
#ifdef __OBJC__
@class NMRenderer,NMRing;
@interface NMGroupState : NSObject
@property id<MTLComputePipelineState> merge;
@property uint64_t frames,groups,members,pixels;
@end
@interface NMGroupFrame : NSObject
@property id<MTLBuffer> descriptors,members,instanceGroups;
@property NSUInteger groupCount,memberCount,instanceCount;
@end
static BOOL nmGroupsInitialize(NMRenderer*,id<MTLLibrary>,NSError**);
static BOOL nmGroupsPrepare(NMRenderer*,NMRing*,const NMGroupUpload*);
static BOOL nmGroupsEncode(NMRenderer*,NMRing*,id<MTLCommandBuffer>,NMUniforms);
static void nmGroupsBind(NMRing*,id<MTLRenderCommandEncoder>);
static uint32_t nmGroupFinalSlot(NMRing*,NSUInteger,uint32_t);
static NSDictionary* nmGroupsInfo(NMRenderer*);
#endif
#endif
