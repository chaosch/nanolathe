// Include after effects.inc and renderer/ring definitions. Root supplies
// groundState/groundRing properties; all frame data belongs to an acquired ring.
_Static_assert(sizeof(NMGroundMark)==80,"ground mark layout");
_Static_assert(sizeof(NMGroundUpload)==32,"ground upload layout");
@interface NMGroundState : NSObject
@property id<MTLRenderPipelineState> color,multiply;
@property uint64_t prepared,drawn,suppressed;
@end
@implementation NMGroundState
@end
@interface NMGroundRing : NSObject
@property id<MTLBuffer> marks;
@property NSUInteger count;
@property vector_float4 ages;
@end
@implementation NMGroundRing
@end
static BOOL nmGroundInitialize(NMRenderer* r,id<MTLLibrary> lib,NSError** error){
 NMGroundState* s=[NMGroundState new];s.color=nmEffectsPipeline(r,lib,@"nm_ground_vertex",@"nm_ground_color",0,error);s.multiply=nmEffectsPipeline(r,lib,@"nm_ground_vertex",@"nm_ground_trail",1,error);
 if(!s.color||!s.multiply)return NO;r.groundState=s;nmEffectsSetGroundDrawer(r,nmGroundDraw);return YES;
}
static BOOL nmGroundPrepare(NMRenderer* r,NMRing* ring,const NMGroundUpload* u){
 NMGroundRing* f=ring.groundRing;if(!u){if(f)f.count=0;return YES;}
 if(!r.groundState||u->count>INT32_MAX||(u->count&&!u->marks))return nmEffectsFail(r,@"Invalid ground-effects upload");
 if(!f){f=[NMGroundRing new];ring.groundRing=f;}f.count=0;
 NSUInteger bytes=(NSUInteger)u->count*sizeof(NMGroundMark);if(bytes>r.device.maxBufferLength)return nmEffectsFail(r,@"Ground-effects buffer limit");
 f.marks=grow(r,f.marks,bytes);if(!f.marks)return nmEffectsFail(r,@"Ground-effects buffer allocation failed");
 if(bytes)memcpy(f.marks.contents,u->marks,bytes);f.count=u->count;f.ages=(vector_float4){u->ages[0],u->ages[1],u->ages[2],u->ages[3]};r.groundState.prepared++;return YES;
}
static BOOL nmGroundDraw(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,const void* uniforms,uint32_t first,uint32_t count){
 NMGroundRing* f=ring.groundRing;NMGroundState* s=r.groundState;if(!f||!s||(uint64_t)first+count>f.count){s.suppressed+=count;return NO;}
 NMUniforms u=*(const NMUniforms*)uniforms;const NMGroundMark* marks=f.marks.contents;
 id<MTLRenderCommandEncoder> enc=nil;BOOL submitted=NO;
 for(NSUInteger i=first;i<(NSUInteger)first+count;){
  BOOL trail=marks[i].cross_kind[2]<.5;
  NSUInteger end=i+1;while(end<(NSUInteger)first+count&&(marks[end].cross_kind[2]<.5)==trail)end++;
  if(!trail&&(!r.waterMask||r->water[4]<=0)){s.suppressed+=end-i;i=end;continue;}
  if(!enc)enc=nmFxShared?:nmEffectsEncoder(r,cb,u);
  [enc setRenderPipelineState:trail?s.multiply:s.color];[enc setVertexBuffer:f.marks offset:0 atIndex:0];
  if(!trail){vector_float4 control={r->water[4],f.ages.x,f.ages.y,0};[enc setFragmentBytes:&control length:sizeof(control) atIndex:0];[enc setFragmentTexture:r.waterMask atIndex:0];}
  [enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:end-i baseInstance:i];s.drawn+=end-i;submitted=YES;i=end;
 }
 if(enc&&enc!=nmFxShared)[enc endEncoding];return submitted;
}
static NSDictionary* nmGroundInfo(NMRenderer* r){NMGroundState* s=r.groundState;if(!s)return @{@"ground_effects_enabled":@NO};return @{@"ground_effects_enabled":@YES,@"ground_effects_prepared_frames":@(s.prepared),@"ground_effects_drawn_marks":@(s.drawn),@"ground_effects_suppressed_marks":@(s.suppressed)};}
