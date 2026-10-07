// Include after renderer/ring interfaces and grow(). Root adds NMGroupState*
// groups to the renderer, NMGroupFrame* groups to each acquired frame ring.
_Static_assert(sizeof(NMGroup)==32,"group descriptor layout");
_Static_assert(sizeof(NMGroupMember)==16,"group member layout");
_Static_assert(sizeof(NMGroupUpload)==40,"group upload layout");
@implementation NMGroupState
@end
@implementation NMGroupFrame
@end
static BOOL nmGroupsFail(NMRenderer* r,NSString* why){r.lastError=why;return NO;}
static BOOL nmGroupsInitialize(NMRenderer* r,id<MTLLibrary> library,NSError** error){
 NMGroupState* state=[NMGroupState new];id<MTLFunction> fn=[library newFunctionWithName:@"nm_group_merge"];
 if(!fn){return nmGroupsFail(r,@"Missing group merge shader");}
 state.merge=[r.device newComputePipelineStateWithFunction:fn error:error];if(!state.merge)return NO;r.groups=state;return YES;
}
static BOOL nmGroupsPrepare(NMRenderer* r,NMRing* ring,const NMGroupUpload* upload){
 if(!upload){
  NMGroupFrame* f=ring.groups;if(!f){f=[NMGroupFrame new];ring.groups=f;}
  f.groupCount=f.memberCount=f.instanceCount=0;f.instanceGroups=grow(r,f.instanceGroups,16);
  if(!f.instanceGroups)return nmGroupsFail(r,@"Group dummy buffer allocation failed");memset(f.instanceGroups.contents,0,16);return YES;
 }
 if(!r.groups||upload->group_count>INT32_MAX||upload->member_count>INT32_MAX||upload->instance_count>INT32_MAX||
 (upload->group_count&&!upload->groups)||(upload->member_count&&!upload->members)||(upload->instance_count&&!upload->instance_groups))return nmGroupsFail(r,@"Invalid group upload");
 if((NSUInteger)upload->instance_count*4>ring.instances.length)return nmGroupsFail(r,@"Group instances exceed retained span");
 NMGroupFrame* f=ring.groups;if(!f){f=[NMGroupFrame new];ring.groups=f;}
 f.groupCount=f.memberCount=f.instanceCount=0;
 NSUInteger gb=upload->group_count*32ull,mb=upload->member_count*16ull,ib=upload->instance_count*16ull;
 if(MAX(MAX(gb,mb),ib)>r.device.maxBufferLength)return nmGroupsFail(r,@"Group device buffer limit");
 f.descriptors=grow(r,f.descriptors,MAX(gb,32));f.members=grow(r,f.members,MAX(mb,16));f.instanceGroups=grow(r,f.instanceGroups,MAX(ib,16));
 if(!f.descriptors||!f.members||!f.instanceGroups)return nmGroupsFail(r,@"Group buffer allocation failed");
 memset(f.instanceGroups.contents,0,MAX(ib,16));if(ib)memcpy(f.instanceGroups.contents,upload->instance_groups,ib);
 if(gb)memcpy(f.descriptors.contents,upload->groups,gb);if(mb)memcpy(f.members.contents,upload->members,mb);
 const NMGroup* groups=f.descriptors.contents;const NMGroupMember* members=f.members.contents;const int32_t* identities=f.instanceGroups.contents;
 const uint32_t* selectors=ring.compositionSelectors.contents;
 for(NSUInteger i=0;i<upload->group_count;i++){
  NMGroup group=groups[i];uint32_t parent=group.info[0],source=group.info[1],final=group.info[2],first=group.info[3],count=group.counts[0];
  if(parent>=upload->instance_count||source>ring.compositionSlotCount||!final||final>ring.compositionSlotCount||!count||first>upload->member_count||count>upload->member_count-first||source==final)return nmGroupsFail(r,@"Invalid group source/final span");
  for(NSUInteger j=first;j<first+count;j++){
   NMGroupMember member=members[j];
   if(member.instance>=upload->instance_count||!member.slot||member.slot>ring.compositionSlotCount||member.slot==final||selectors[member.instance]!=member.slot)return nmGroupsFail(r,@"Invalid isolated group member");
   const int32_t* identity=&identities[member.instance*4];
   int32_t policy=group.counts[2]?(member.instance!=parent):2;
   if(identity[0]!=(int32_t)(i+1)||identity[1]!=policy||identity[2]!=member.key_delta||identity[3]!=(int32_t)parent)return nmGroupsFail(r,@"Group identity sidecar mismatch");
  }
  // Every final slot is write-only. It must never alias any group's source.
  for(NSUInteger j=0;j<upload->member_count;j++)if(members[j].slot==final)return nmGroupsFail(r,@"Group final slot aliases immutable source");
  for(NSUInteger j=0;j<i;j++)if(groups[j].info[2]==final)return nmGroupsFail(r,@"Group final slots overlap");
 }
 for(NSUInteger i=0;i<upload->instance_count;i++)if(identities[i*4]<0||(uint32_t)identities[i*4]>upload->group_count)return nmGroupsFail(r,@"Group identity outside descriptor span");
 f.groupCount=upload->group_count;f.memberCount=upload->member_count;f.instanceCount=upload->instance_count;r.groups.frames++;return YES;
}
static void nmGroupsBind(NMRing* ring,id<MTLRenderCommandEncoder> encoder){if(ring.groups)[encoder setVertexBuffer:ring.groups.instanceGroups offset:0 atIndex:20];}
// Original source selector remains the fallback for ungrouped bodies. Water
// reflections use this final selector only for their additional occlusion test.
static uint32_t nmGroupFinalSlot(NMRing* ring,NSUInteger instance,uint32_t source){
 NMGroupFrame* f=ring.groups;if(!f||instance>=f.instanceCount)return source;
 const int32_t* identities=f.instanceGroups.contents;int32_t group=identities[instance*4];if(group<=0||(NSUInteger)group>f.groupCount)return source;
 const NMGroup* descriptors=f.descriptors.contents;return descriptors[group-1].info[2];
}
static BOOL nmGroupsEncode(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,NMUniforms u){
 NMGroupFrame* f=ring.groups;if(!f||!f.groupCount)return YES;
 if(!r.groups.merge||!r.modelAtlasColor||!r.modelAtlasEmission||!r.modelMetadata)return nmGroupsFail(r,@"Missing group atlas or pipeline");
 if(r.device.readWriteTextureSupport<MTLReadWriteTextureTier2)return nmGroupsFail(r,@"Group merge requires tier-two read/write textures");
 for(id<MTLTexture> texture in @[r.modelAtlasColor,r.modelAtlasEmission,r.modelMetadata])if(!(texture.usage&MTLTextureUsageShaderWrite))return nmGroupsFail(r,@"Group atlas lacks shader-write usage");
 if(ring.rules.length<f.instanceCount*112ull)return nmGroupsFail(r,@"Group parent rules exceed retained span");
 const NMGroup* groups=f.descriptors.contents;const float* slots=ring.compositionSlots.contents;
 id<MTLComputeCommandEncoder> enc=nmComputeEncoder(cb,"groups.nmGroupsEncode#1");[enc setComputePipelineState:r.groups.merge];
 [enc setBuffer:f.descriptors offset:0 atIndex:0];[enc setBuffer:f.members offset:0 atIndex:1];[enc setBuffer:ring.compositionSlots offset:0 atIndex:2];[enc setBytes:&u length:sizeof(u) atIndex:3];[enc setBuffer:ring.rules offset:0 atIndex:4];
 [enc setTexture:r.modelAtlasColor atIndex:0];[enc setTexture:r.modelAtlasEmission atIndex:1];[enc setTexture:r.modelMetadata atIndex:2];
 for(uint32_t i=0;i<f.groupCount;i++){
  const float* slot=&slots[(groups[i].info[2]-1)*12];NSUInteger width=(NSUInteger)slot[6],height=(NSUInteger)slot[7];
  if(!width||!height||slot[4]<0||slot[5]<0||slot[4]+width>r.modelAtlasColor.width||slot[5]+height>r.modelAtlasColor.height){[enc endEncoding];return nmGroupsFail(r,@"Group final rectangle outside atlas");}
  [enc setBytes:&i length:sizeof(i) atIndex:5];[enc dispatchThreads:MTLSizeMake(width,height,1) threadsPerThreadgroup:MTLSizeMake(8,8,1)];
  r.groups.groups++;r.groups.members+=groups[i].counts[0];r.groups.pixels+=width*height;
 }
 [enc endEncoding];return YES;
}
static NSDictionary* nmGroupsInfo(NMRenderer* r){NMGroupState* s=r.groups;return @{@"group_frames":@(s.frames),@"group_merges":@(s.groups),@"group_members":@(s.members),@"group_pixels":@(s.pixels)};}
