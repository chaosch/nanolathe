// Shared source types are declared by the base shader. No source slot is ever
// written here: final rectangles are reserved and disjoint from all sources.
struct NMGroupGPU {uint4 info,counts;};
struct NMGroupMemberGPU {uint instance,slot;int delta;uint reserved;};

bool nm_group_source(ModelSlot slot,float2 screen,thread uint2& pixel) {
 float2 local=(screen-slot.screen.xy)*slot.atlas.zw/slot.screen.zw;
 if(any(local<0)||any(local>=slot.atlas.zw))return false;
 pixel=uint2(slot.atlas.xy+floor(local));return true;
}
// Keyed-unit source hook: a child forces key-plane admission and every member
// defers water/Digger erase to its carrier. Policy 2 is a keyless painter pair.
ModelRules nm_group_source_rule(ModelRules own,int4 member) {
 if(member.x==0||member.y==2)return own;
 if(member.y==1)own.reveal.w=1;
 own.clip.x=0;own.clip.z=0;return own;
}
// Palette BLUE is selected before lighting/finish, as clipIndex does in the
// production direct shader. It cannot be approximated by tinting finished RGB.
bool nm_group_blue(float ownKey,int4 member,device const ModelRules* rules) {
 if(member.x==0||member.y==2)return false;
 float key=member.z==0?ownKey:clamp(ownKey+float(member.z),0.0,255.0);
 float4 clip=rules[member.w].clip;
 return clip.x>1.5&&key<=clip.y;
}
kernel void nm_group_merge(device const NMGroupGPU* groups [[buffer(0)]],device const NMGroupMemberGPU* members [[buffer(1)]],device const ModelSlot* slots [[buffer(2)]],constant Uniforms& u [[buffer(3)]],device const ModelRules* rules [[buffer(4)]],constant uint& groupIndex [[buffer(5)]],texture2d<float,access::read_write> colors [[texture(0)]],texture2d<float,access::read_write> emissions [[texture(1)]],texture2d<float,access::read_write> metadata [[texture(2)]],uint2 at [[thread_position_in_grid]]) {
 NMGroupGPU group=groups[groupIndex];ModelSlot final=slots[group.info.z-1];
 if(any(at>=uint2(final.atlas.zw)))return;
 float2 screen=final.screen.xy+(float2(at)+.5)*final.screen.zw/final.atlas.zw;
 float4 color=0,emission=0,key=0;
 for(uint i=0;i<group.counts.x;i++){
  NMGroupMemberGPU member=members[group.info.w+i];uint2 source;
  if(!nm_group_source(slots[member.slot-1],screen,source))continue;
  float4 nextColor=colors.read(source),nextEmission=emissions.read(source),nextKey=metadata.read(source);
  if(group.counts.z!=0&&member.delta!=0)nextKey.y=clamp(round(nextKey.y)+float(member.delta),0.0,255.0);
  // Copy the parent's erased keys too. The production initial union retains
  // its key plane even where its completed colour stage has no coverage.
  bool parent=member.instance==group.info.x;
  bool hole=group.counts.z!=0&&group.counts.y!=0&&nextKey.w>0&&key.y<nextKey.y;
  // Standalone projectile calls are keyless: the producer draws the root
  // then its admitted header child, so transparent pixels preserve prior art.
  // They never acquire unit key tests or the carrier's final clip.
  bool visible=nextColor.a>0&&(group.counts.z==0||key.y<=nextKey.y);
  if(parent||hole||visible){color=nextColor;emission=nextEmission;key=nextKey;}
 }
 if(group.counts.z!=0&&u.visual.w>.5){
  float4 clip=rules[group.info.x].clip;
  bool blue=clip.x>1.5&&key.y<=clip.y;
  key.z=blue?1:0;
  if((clip.x>.5&&clip.x<1.5&&key.y<=clip.y)||(clip.z>.5&&key.y<=clip.w)){color=0;emission=0;}
 }
 uint2 destination=uint2(final.atlas.xy)+at;
 colors.write(color,destination);emissions.write(emission,destination);metadata.write(key,destination);
}
