#import <Cocoa/Cocoa.h>
#import <Metal/Metal.h>
#import <QuartzCore/CAMetalLayer.h>
#import <simd/simd.h>
#include "bridge.h"
#include <stdatomic.h>

@interface NMMesh : NSObject
@property id<MTLBuffer> vertices, indices, instances, edges, visuals;
@property NSUInteger indexCount, instanceCount, edgeCount;
@end
@implementation NMMesh
@end

typedef struct { vector_float4 camera,rect,viewport,timing,mode,heightRect,fogRect,visual; } NMUniforms;
@interface NMRing : NSObject
@property id<MTLBuffer> poses,instances,sprites,lights,visuals,distortions,selectors,overlay;
@property NSMutableData* spans;
@property NSUInteger spriteCount,distortionCount,overlayCount;
@property id<MTLTexture> fog;
@end
@implementation NMRing
@end
_Static_assert(sizeof(NMLiveUpload)==168,"live upload layout");
_Static_assert(sizeof(NMAssets)==56,"retained assets layout");
_Static_assert(sizeof(NMInput)==16,"input layout");
_Static_assert(sizeof(NMEvent)==32,"event layout");
#define NM_EVENT_CAPACITY 8192
@interface NMRenderer : NSObject {
@public
 NMUniforms uniforms, lastUniforms;
 NMRow* rows;
 uint64_t* rowGenerations;
 NMEvent events[NM_EVENT_CAPACITY];
 NSUInteger eventCount,eventOverflow;
 uint32_t mouseHeld;
 float pointerX,pointerY;
 NSUInteger rowCount, rowCapacity, poseCount, frameNumber;
 dispatch_semaphore_t available;
 dispatch_group_t pending, presentations;
 BOOL presentationDrainTimedOut;
 NSUInteger presentationDrainRowCount;
 BOOL interactive,playable,wasKey;
 BOOL offscreen,effects,visualPasses,held[128],pressed[128],pauseToggle;
 float wheel;
}
@property id<MTLDevice> device;
@property id<MTLCommandQueue> queue;
@property id<MTLRenderPipelineState> model,terrain,composite,sprite,shadow,outline,world,distortion,fog,overlay;
@property id<MTLComputePipelineState> blur;
@property id<MTLDepthStencilState> depth,spriteDepth;
@property NSMutableArray<NMMesh*>* meshes;
@property NSMutableArray<id<MTLBuffer>>* poses;
@property NSMutableArray<NMRing*>* rings;
@property NMRing* lastRing;
@property id<MTLBuffer> emptyLights,materials,textureFrames;
@property id<MTLTexture> spriteAtlas,overlayAtlas;
@property id<MTLTexture> atlas,terrainImage,color,emission,blurA,blurB,depthImage,output,heights,shadowMask,worldSnapshot,postColor,emptyFog;
@property CAMetalLayer* layer;
@property NSWindow* window;
@property NSString* lastError;
@end
@implementation NMRenderer
- (void)dealloc { free(rows);free(rowGenerations); }
@end

static id<MTLTexture> target(NMRenderer* r,NSUInteger w,NSUInteger h,MTLPixelFormat format,MTLTextureUsage usage) {
 MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:format width:w height:h mipmapped:NO];d.storageMode=MTLStorageModePrivate;d.usage=usage;return [r.device newTextureWithDescriptor:d];
}
static id<MTLRenderPipelineState> pipeline(NMRenderer* r,id<MTLLibrary> l,NSString* vertex,NSString* fragment,BOOL mrt,NSError** error) {
 MTLRenderPipelineDescriptor* d=[MTLRenderPipelineDescriptor new];d.vertexFunction=[l newFunctionWithName:vertex];d.fragmentFunction=[l newFunctionWithName:fragment];d.colorAttachments[0].pixelFormat=(mrt||[fragment isEqualToString:@"world_fragment"]||[fragment isEqualToString:@"distortion_fragment"]||[fragment isEqualToString:@"shadow_fragment"])?MTLPixelFormatRGBA16Float:MTLPixelFormatBGRA8Unorm;
 if([vertex isEqualToString:@"overlay_vertex"]){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=MTLBlendFactorOneMinusSourceAlpha;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;}
 if([vertex isEqualToString:@"sprite_vertex"]||[fragment isEqualToString:@"model_fragment"]||[fragment isEqualToString:@"outline_fragment"]){
 for(int i=0;i<2;i++){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[i];a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=i==0?MTLBlendFactorOneMinusSourceAlpha:MTLBlendFactorOne;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;}
 }
 if([fragment isEqualToString:@"shadow_fragment"]){d.colorAttachments[0].pixelFormat=MTLPixelFormatR8Unorm;MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.blendingEnabled=YES;a.rgbBlendOperation=MTLBlendOperationMax;a.alphaBlendOperation=MTLBlendOperationMax;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=MTLBlendFactorOne;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOne;}
 if(mrt){d.colorAttachments[1].pixelFormat=MTLPixelFormatRGBA16Float;d.depthAttachmentPixelFormat=MTLPixelFormatDepth32Float;}
 return [r.device newRenderPipelineStateWithDescriptor:d error:error];
}
static void copyError(char* out,int size,NSString* message) { if(out&&size>0)snprintf(out,size,"%s",message.UTF8String?:"unknown native failure"); }
void* nm_create(int width,int height,int isOffscreen,int vsync,int effects,int visualPasses,int capacity,const char* shader,char* error,int errorSize) {
 @autoreleasepool {
 if(![NSThread isMainThread]){copyError(error,errorSize,@"Metal window must be initialized on the process main thread");return NULL;}
 NMRenderer* r=[NMRenderer new];r.device=MTLCreateSystemDefaultDevice();if(!r.device){copyError(error,errorSize,@"No Metal device");return NULL;}
 r.queue=[r.device newCommandQueue];r.rings=[NSMutableArray new];for(int i=0;i<3;i++)[r.rings addObject:[NMRing new]];r.emptyLights=[r.device newBufferWithLength:32 options:MTLResourceStorageModeShared];r.meshes=[NSMutableArray new];r.poses=[NSMutableArray new];r->offscreen=isOffscreen;r->effects=effects;r->visualPasses=visualPasses;r->available=dispatch_semaphore_create(3);r->pending=dispatch_group_create();r->presentations=dispatch_group_create();r->interactive=capacity<0;r->rowCapacity=capacity<0?-(int64_t)capacity:capacity;if(!r->rowCapacity){copyError(error,errorSize,@"Timing capacity must be positive");return NULL;}r->rows=calloc(r->rowCapacity,sizeof(NMRow));r->rowGenerations=calloc(r->rowCapacity,sizeof(uint64_t));if(!r->rows||!r->rowGenerations){copyError(error,errorSize,@"Timing storage allocation failed");return NULL;}
 r->uniforms.viewport=(vector_float4){width,height,0,0};
 NSError* e=nil;id<MTLLibrary> lib=[r.device newLibraryWithSource:[NSString stringWithUTF8String:shader] options:nil error:&e];
 if(!lib){copyError(error,errorSize,e.localizedDescription);return NULL;}
 r.model=pipeline(r,lib,@"model_vertex",@"model_fragment",YES,&e);r.terrain=pipeline(r,lib,@"terrain_vertex",@"terrain_fragment",YES,&e);r.composite=pipeline(r,lib,@"screen_vertex",@"composite_fragment",NO,&e);
 r.shadow=pipeline(r,lib,@"shadow_vertex",@"shadow_fragment",NO,&e);r.outline=pipeline(r,lib,@"model_vertex",@"outline_fragment",YES,&e);r.world=pipeline(r,lib,@"screen_vertex",@"world_fragment",NO,&e);r.distortion=pipeline(r,lib,@"distortion_vertex",@"distortion_fragment",NO,&e);r.fog=pipeline(r,lib,@"screen_vertex",@"fog_fragment",NO,&e);r.overlay=pipeline(r,lib,@"overlay_vertex",@"overlay_fragment",NO,&e);
 r.materials=[r.device newBufferWithLength:16 options:MTLResourceStorageModeShared];r.textureFrames=[r.device newBufferWithLength:16 options:MTLResourceStorageModeShared];
 MTLTextureDescriptor* hd=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatR32Float width:1 height:1 mipmapped:NO];hd.storageMode=MTLStorageModeShared;hd.usage=MTLTextureUsageShaderRead;r.heights=[r.device newTextureWithDescriptor:hd];float zero=0;[r.heights replaceRegion:MTLRegionMake2D(0,0,1,1) mipmapLevel:0 withBytes:&zero bytesPerRow:4];
 hd.pixelFormat=MTLPixelFormatRGBA8Unorm;r.emptyFog=[r.device newTextureWithDescriptor:hd];uint32_t white=0xffffffff;[r.emptyFog replaceRegion:MTLRegionMake2D(0,0,1,1) mipmapLevel:0 withBytes:&white bytesPerRow:4];
 r.sprite=pipeline(r,lib,@"sprite_vertex",@"sprite_fragment",YES,&e);
 r.blur=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"blur"] error:&e];
 if(!r.model||!r.terrain||!r.composite||!r.sprite||!r.blur||!r.shadow||!r.outline||!r.world||!r.distortion||!r.fog||!r.overlay){copyError(error,errorSize,e.localizedDescription);return NULL;}
 MTLDepthStencilDescriptor* depth=[MTLDepthStencilDescriptor new];depth.depthCompareFunction=MTLCompareFunctionLessEqual;depth.depthWriteEnabled=YES;r.depth=[r.device newDepthStencilStateWithDescriptor:depth];depth.depthWriteEnabled=NO;r.spriteDepth=[r.device newDepthStencilStateWithDescriptor:depth];
 r.color=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.emission=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.blurA=target(r,MAX(width/2,1),MAX(height/2,1),MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
 r.blurB=target(r,MAX(width/2,1),MAX(height/2,1),MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
 if(visualPasses){r.shadowMask=target(r,width,height,MTLPixelFormatR8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.worldSnapshot=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.postColor=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 }else{r.shadowMask=r.emptyFog;}
 r.depthImage=target(r,width,height,MTLPixelFormatDepth32Float,MTLTextureUsageRenderTarget);
 r.output=target(r,width,height,MTLPixelFormatBGRA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 if(!isOffscreen){
 [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
 // Our loop pumps events instead of calling NSApplication.run. Complete launch
 // explicitly so Launch Services and accessibility can attach to the app.
 [NSApp finishLaunching];
 CGFloat scale=NSScreen.mainScreen.backingScaleFactor;
 r.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,width/scale,height/scale) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];r.window.releasedWhenClosed=NO;r.window.title=@"Nanolathe retained Metal prototype";
 r.layer=[CAMetalLayer layer];r.layer.device=r.device;r.layer.pixelFormat=MTLPixelFormatBGRA8Unorm;r.layer.drawableSize=CGSizeMake(width,height);r.layer.contentsScale=scale;r.layer.framebufferOnly=YES;r.layer.maximumDrawableCount=3;r.layer.displaySyncEnabled=vsync;
 NSView* view=r.window.contentView;view.wantsLayer=YES;view.layer=r.layer;r.window.acceptsMouseMovedEvents=YES;[r.window center];[r.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
 }
 return (__bridge_retained void*)r;
 }
}
int nm_mesh(void* handle,const void* vertices,int vertexCount,const uint32_t* indices,int indexCount,const uint32_t* offsets,int instances,const uint32_t* edges,int edgeCount,const void* visuals) {
 @autoreleasepool {
 NMRenderer* r=(__bridge NMRenderer*)handle;NMMesh* m=[NMMesh new];
 m.vertices=[r.device newBufferWithLength:MAX((NSUInteger)vertexCount*64,64) options:MTLResourceStorageModeShared];
 m.indices=[r.device newBufferWithLength:MAX((NSUInteger)indexCount*4,4) options:MTLResourceStorageModeShared];
 m.instances=[r.device newBufferWithLength:MAX((NSUInteger)instances*4,4) options:MTLResourceStorageModeShared];
 m.edges=[r.device newBufferWithLength:MAX((NSUInteger)edgeCount*4,4) options:MTLResourceStorageModeShared];m.visuals=[r.device newBufferWithLength:MAX((NSUInteger)instances*64,64) options:MTLResourceStorageModeShared];
 if(!m.vertices||!m.indices||!m.instances||!m.edges||!m.visuals)return 0;
 if(edgeCount)memcpy(m.edges.contents,edges,(NSUInteger)edgeCount*4);if(instances)memcpy(m.visuals.contents,visuals,(NSUInteger)instances*64);m.edgeCount=edgeCount;
 if(vertexCount>0)memcpy(m.vertices.contents,vertices,(NSUInteger)vertexCount*64);
 if(indexCount>0)memcpy(m.indices.contents,indices,(NSUInteger)indexCount*4);
 if(instances>0)memcpy(m.instances.contents,offsets,(NSUInteger)instances*4);
 m.indexCount=indexCount;m.instanceCount=instances;[r.meshes addObject:m];return 1;
 }
}
int nm_assets(void* handle,const NMAssets* a) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;
 r.materials=[r.device newBufferWithLength:MAX((NSUInteger)a->material_count*16,16) options:MTLResourceStorageModeShared];r.textureFrames=[r.device newBufferWithLength:MAX((NSUInteger)a->frame_count*16,16) options:MTLResourceStorageModeShared];if(!r.materials||!r.textureFrames)return 0;
 if(a->material_count)memcpy(r.materials.contents,a->materials,a->material_count*16);if(a->frame_count)memcpy(r.textureFrames.contents,a->frames,a->frame_count*16);
 if(a->width&&a->height){MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatR32Float width:a->width height:a->height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;r.heights=[r.device newTextureWithDescriptor:d];if(!r.heights)return 0;[r.heights replaceRegion:MTLRegionMake2D(0,0,a->width,a->height) mipmapLevel:0 withBytes:a->heights bytesPerRow:a->width*4];r->uniforms.visual.y=1;}
 r->uniforms.heightRect=(vector_float4){a->rect[0],a->rect[1],a->rect[2],a->rect[3]};return 1;}
}
int nm_texture(void* handle,int slot,int width,int height,const void* rgba) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:width height:height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;id<MTLTexture> t=[r.device newTextureWithDescriptor:d];if(!t)return 0;[t replaceRegion:MTLRegionMake2D(0,0,width,height) mipmapLevel:0 withBytes:rgba bytesPerRow:width*4];if(slot==0)r.atlas=t;else if(slot==1)r.terrainImage=t;else if(slot==2)r.spriteAtlas=t;else if(slot==3)r.overlayAtlas=t;else return 0;return 1;}
}
int nm_configure(void* handle,const float* camera,const float* rect,int count) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;r->uniforms.camera=(vector_float4){camera[0],camera[1],camera[2],0};r->uniforms.rect=(vector_float4){rect[0],rect[1],rect[2],rect[3]};r->poseCount=count;
 for(int i=0;i<3;i++){id<MTLBuffer> b=[r.device newBufferWithLength:MAX(count*128,128) options:MTLResourceStorageModeShared];if(!b)return 0;[r.poses addObject:b];}return 1;}
}
static void pushEvent(NMRenderer* r,NMEvent event) {
 // Only adjacent pointer motion with no wheel can collapse. Edges never merge.
 if(event.kind==1&&event.wheel_x==0&&event.wheel_y==0&&r->eventCount){NMEvent* last=&r->events[r->eventCount-1];if(last->kind==1&&last->wheel_x==0&&last->wheel_y==0){*last=event;return;}}
 if(r->eventCount==NM_EVENT_CAPACITY){r->eventOverflow++;return;}
 r->events[r->eventCount++]=event;
}
static uint32_t modifiers(NSEvent* event) {
 NSEventModifierFlags f=event.modifierFlags;return ((f&NSEventModifierFlagShift)?1:0)|((f&NSEventModifierFlagControl)?2:0)|((f&NSEventModifierFlagOption)?4:0)|((f&NSEventModifierFlagCommand)?8:0);
}
static void clearFocus(NMRenderer* r) {
 memset(r->held,0,sizeof(r->held));memset(r->pressed,0,sizeof(r->pressed));r->pauseToggle=NO;r->wheel=0;
 // Release an unfinished drag when the app loses focus; there may be no later
 // mouse-up for a button released while another application is active.
 for(uint32_t button=0;button<3;button++)if(r->mouseHeld&(1u<<button))pushEvent(r,(NMEvent){.kind=3,.button=button,.x=r->pointerX,.y=r->pointerY});
 r->mouseHeld=0;
}
static void pump(NMRenderer* r) {
 if(r->offscreen)return;
 if(r->wasKey&&!r.window.keyWindow)clearFocus(r);
 NSEvent* event;while((event=[NSApp nextEventMatchingMask:NSEventMaskAny untilDate:[NSDate distantPast] inMode:NSDefaultRunLoopMode dequeue:YES])){
  BOOL consumed=NO;BOOL ours=event.window==r.window;
  if(ours&&(event.type==NSEventTypeKeyDown||event.type==NSEventTypeKeyUp)){
   unsigned short k=event.keyCode;if(k<128){r->held[k]=event.type==NSEventTypeKeyDown;if(r->held[k])r->pressed[k]=YES;if(k==49&&event.type==NSEventTypeKeyDown&&!event.isARepeat)r->pauseToggle=YES;}
   if(event.type==NSEventTypeKeyDown){NSString* chars=event.charactersIgnoringModifiers.uppercaseString;uint32_t key=chars.length?[chars characterAtIndex:0]:0;if(k==53)key=27;else if(k==36||k==76)key=13;else if(k==51||k==117)key=8;
    if(r->playable&&((key>=32&&key<=126)||key==27||key==13||key==8))pushEvent(r,(NMEvent){.kind=4,.key=key,.modifiers=modifiers(event),.x=r->pointerX,.y=r->pointerY});}
   // Gameplay command keys are handled by Go, including right-click commands.
   consumed=r->playable||k==0||k==1||k==2||k==13||k==49||k>=123;
  }
  BOOL down=event.type==NSEventTypeLeftMouseDown||event.type==NSEventTypeRightMouseDown||event.type==NSEventTypeOtherMouseDown;
  BOOL up=event.type==NSEventTypeLeftMouseUp||event.type==NSEventTypeRightMouseUp||event.type==NSEventTypeOtherMouseUp;
  BOOL move=event.type==NSEventTypeMouseMoved||event.type==NSEventTypeLeftMouseDragged||event.type==NSEventTypeRightMouseDragged||event.type==NSEventTypeOtherMouseDragged;
  BOOL scroll=event.type==NSEventTypeScrollWheel;
  if(ours&&(down||up||move||scroll)){
   NSView* view=r.window.contentView;NSPoint point=[view convertPoint:event.locationInWindow fromView:nil];NSRect bounds=view.bounds;
   float x=(point.x-NSMinX(bounds))*r.layer.drawableSize.width/MAX(bounds.size.width,1);
   float y=(view.flipped?point.y-NSMinY(bounds):NSMaxY(bounds)-point.y)*r.layer.drawableSize.height/MAX(bounds.size.height,1);
   BOOL inside=NSPointInRect(point,bounds);uint32_t button=event.buttonNumber==1?1:event.buttonNumber==2?2:0;
   if(inside||r->mouseHeld){r->pointerX=x;r->pointerY=y;
    if(scroll){r->wheel+=(float)event.scrollingDeltaY;consumed=YES;}
    if(r->playable){if(down){[r.window makeKeyWindow];r->mouseHeld|=1u<<button;}if(up)r->mouseHeld&=~(1u<<button);
     pushEvent(r,(NMEvent){.kind=down?2:up?3:1,.button=button,.modifiers=modifiers(event),.x=x,.y=y,.wheel_x=scroll?(float)event.scrollingDeltaX:0,.wheel_y=scroll?(float)event.scrollingDeltaY:0});consumed=YES;}
   }
  }
  if(!consumed)[NSApp sendEvent:event];
  if(r->wasKey&&!r.window.keyWindow)clearFocus(r);r->wasKey=r.window.keyWindow;
 }
 if(!r.window.keyWindow)clearFocus(r);r->wasKey=r.window.keyWindow;[NSApp updateWindows];
}
void nm_playable(void* handle,int playable) {NMRenderer* r=(__bridge NMRenderer*)handle;r->playable=playable!=0;}
int nm_events(void* handle,NMEvent* output,int capacity) {
 NMRenderer* r=(__bridge NMRenderer*)handle;if(capacity<0)return -1;NSUInteger n=MIN((NSUInteger)capacity,r->eventCount);if(n)memcpy(output,r->events,n*sizeof(NMEvent));r->eventCount-=n;if(r->eventCount)memmove(r->events,r->events+n,r->eventCount*sizeof(NMEvent));return (int)n;
}
void nm_input(void* handle,NMInput* input) {
 @autoreleasepool {
 NMRenderer* r=(__bridge NMRenderer*)handle;pump(r);
 // Retain a short tap for one sample even when down/up share an event pump.
 BOOL right=r->held[124]||r->pressed[124]||(!r->playable&&(r->held[2]||r->pressed[2]));
 BOOL left=r->held[123]||r->pressed[123]||(!r->playable&&(r->held[0]||r->pressed[0]));
 BOOL down=r->held[125]||r->pressed[125]||(!r->playable&&(r->held[1]||r->pressed[1]));
 BOOL up=r->held[126]||r->pressed[126]||(!r->playable&&(r->held[13]||r->pressed[13]));
 input->pan_x=right-left;input->pan_z=down-up;input->zoom=r->wheel;input->pause_toggle=r->pauseToggle;
 memset(r->pressed,0,sizeof(r->pressed));r->wheel=0;r->pauseToggle=NO;
 }
}
static void drawMeshes(NMRenderer* r,id<MTLRenderCommandEncoder> enc,NMRing* ring,int visualPass) {
 NSUInteger meshIndex=0;const uint32_t* spans=ring.spans.bytes;
 for(NMMesh* m in r.meshes){NSUInteger index=meshIndex++;NSUInteger at=(visualPass*r.meshes.count+index)*2;NSUInteger count=ring?spans[at+1]:m.instanceCount;NSUInteger start=ring?spans[index*2]:0;NSUInteger indices=visualPass==2?m.edgeCount:m.indexCount;if(count==0||indices==0)continue;
 [enc setVertexBuffer:m.vertices offset:0 atIndex:0];[enc setVertexBuffer:ring?ring.instances:m.instances offset:start*4 atIndex:1];[enc setVertexBuffer:ring?ring.visuals:m.visuals offset:start*64 atIndex:5];[enc setVertexBuffer:visualPass?ring.selectors:r.emptyLights offset:visualPass?spans[at]*4:0 atIndex:8];
 [enc drawIndexedPrimitives:visualPass==2?MTLPrimitiveTypeLine:MTLPrimitiveTypeTriangle indexCount:indices indexType:MTLIndexTypeUInt32 indexBuffer:visualPass==2?m.edges:m.indices indexBufferOffset:0 instanceCount:count];}
}
static void encodeOverlay(NMRenderer* r,id<MTLCommandBuffer> cb,NMUniforms u,id<MTLTexture> destination,NMRing* ring) {
 if(!ring.overlayCount)return;
 MTLRenderPassDescriptor* pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=destination;pass.colorAttachments[0].loadAction=MTLLoadActionLoad;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setRenderPipelineState:r.overlay];[enc setVertexBuffer:ring.overlay offset:0 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:r.overlayAtlas?:r.emptyFog atIndex:0];[enc setFragmentTexture:r.terrainImage atIndex:1];[enc setFragmentTexture:u.visual.z>0.5?ring.fog:r.emptyFog atIndex:2];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.overlayCount];[enc endEncoding];
}
static void encode(NMRenderer* r,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u,id<MTLTexture> destination,NMRing* ring) {
 MTLRenderPassDescriptor* pass;
 if(u.visual.x>0.5){
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.shadowMask;pass.colorAttachments[0].loadAction=MTLLoadActionClear;pass.colorAttachments[0].storeAction=MTLStoreActionStore;pass.colorAttachments[0].clearColor=MTLClearColorMake(0,0,0,0);
 id<MTLRenderCommandEncoder> shadow=[cb renderCommandEncoderWithDescriptor:pass];[shadow setRenderPipelineState:r.shadow];[shadow setVertexBytes:&u length:sizeof(u) atIndex:3];[shadow setVertexBuffer:poses offset:0 atIndex:2];[shadow setVertexTexture:r.heights atIndex:1];drawMeshes(r,shadow,ring,1);[shadow endEncoding];}
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.color;pass.colorAttachments[0].loadAction=MTLLoadActionClear;pass.colorAttachments[0].storeAction=MTLStoreActionStore;pass.colorAttachments[0].clearColor=MTLClearColorMake(0.08,0.09,0.1,1);
 pass.colorAttachments[1].texture=r.emission;pass.colorAttachments[1].loadAction=MTLLoadActionClear;pass.colorAttachments[1].storeAction=MTLStoreActionStore;
 pass.depthAttachment.texture=r.depthImage;pass.depthAttachment.loadAction=MTLLoadActionClear;pass.depthAttachment.storeAction=MTLStoreActionDontCare;pass.depthAttachment.clearDepth=1;
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setDepthStencilState:r.depth];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setCullMode:MTLCullModeNone];[enc setFragmentBuffer:ring?ring.lights:r.emptyLights offset:0 atIndex:4];
 [enc setRenderPipelineState:r.terrain];[enc setFragmentTexture:r.terrainImage atIndex:0];[enc setFragmentTexture:r.heights atIndex:1];[enc setFragmentTexture:r.shadowMask atIndex:2];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];
 [enc setRenderPipelineState:r.model];[enc setVertexBuffer:poses offset:0 atIndex:2];[enc setFragmentTexture:r.atlas atIndex:0];
 [enc setVertexBuffer:r.materials offset:0 atIndex:6];[enc setVertexBuffer:r.textureFrames offset:0 atIndex:7];drawMeshes(r,enc,ring,0);
 if(u.visual.x>0.5){[enc setRenderPipelineState:r.outline];[enc setDepthStencilState:r.spriteDepth];NMUniforms outlineU=u;outlineU.mode.z=1;[enc setVertexBytes:&outlineU length:sizeof(outlineU) atIndex:3];drawMeshes(r,enc,ring,2);[enc setVertexBytes:&u length:sizeof(u) atIndex:3];}
 if(ring.spriteCount>0){[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:r.spriteDepth];[enc setVertexBuffer:ring.sprites offset:0 atIndex:0];[enc setFragmentTexture:r.spriteAtlas atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.spriteCount];}
 [enc endEncoding];
 if(r->effects){
 id<MTLComputeCommandEncoder> compute=[cb computeCommandEncoder];[compute setComputePipelineState:r.blur];vector_float2 d={1,0};[compute setBytes:&d length:sizeof(d) atIndex:0];[compute setTexture:r.emission atIndex:0];[compute setTexture:r.blurA atIndex:1];[compute dispatchThreads:MTLSizeMake(r.blurA.width,r.blurA.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[compute endEncoding];
 compute=[cb computeCommandEncoder];[compute setComputePipelineState:r.blur];d=(vector_float2){0,1};[compute setBytes:&d length:sizeof(d) atIndex:0];[compute setTexture:r.blurA atIndex:0];[compute setTexture:r.blurB atIndex:1];[compute dispatchThreads:MTLSizeMake(r.blurB.width,r.blurB.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[compute endEncoding];
 }
 if(u.visual.x>0.5){
 BOOL distort=r->effects&&ring.distortionCount>0;id<MTLTexture> world=distort?r.worldSnapshot:r.postColor;
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=world;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setRenderPipelineState:r.world];[enc setFragmentTexture:r.color atIndex:0];[enc setFragmentTexture:r.blurB atIndex:1];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];
 if(distort){id<MTLBlitCommandEncoder> copy=[cb blitCommandEncoder];[copy copyFromTexture:r.worldSnapshot sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(r.postColor.width,r.postColor.height,1) toTexture:r.postColor destinationSlice:0 destinationLevel:0 destinationOrigin:MTLOriginMake(0,0,0)];[copy endEncoding];
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.postColor;pass.colorAttachments[0].loadAction=MTLLoadActionLoad;pass.colorAttachments[0].storeAction=MTLStoreActionStore;enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setRenderPipelineState:r.distortion];[enc setVertexBuffer:ring.distortions offset:0 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:r.worldSnapshot atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.distortionCount];[enc endEncoding];}
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=destination;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setRenderPipelineState:r.fog];[enc setFragmentTexture:r.postColor atIndex:0];[enc setFragmentTexture:ring.fog?:r.emptyFog atIndex:1];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];encodeOverlay(r,cb,u,destination,ring);return;
 }
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=destination;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setRenderPipelineState:r.composite];[enc setFragmentTexture:r.color atIndex:0];[enc setFragmentTexture:r.blurB atIndex:1];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];encodeOverlay(r,cb,u,destination,ring);
}
int nm_frame(void* handle,const void* previous,const void* next,float alpha,double seconds) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;if(!r->interactive&&r->rowCount>=r->rowCapacity)return 0;pump(r);if(!r->offscreen&&!r.window.visible)return -1;
 double start=CACurrentMediaTime();dispatch_semaphore_wait(r->available,DISPATCH_TIME_FOREVER);double acquired=CACurrentMediaTime();
 id<CAMetalDrawable> drawable=nil;if(!r->offscreen){drawable=[r.layer nextDrawable];if(!drawable){dispatch_semaphore_signal(r->available);return 0;}}
 double ready=CACurrentMediaTime();NSUInteger generation=++r->rowCount;NSUInteger rowIndex=(generation-1)%r->rowCapacity;NMRow* row=&r->rows[rowIndex];@synchronized(r){memset(row,0,sizeof(*row));r->rowGenerations[rowIndex]=generation;}row->wait=(acquired-start)*1000;row->drawable_wait=(ready-acquired)*1000;
 id<MTLBuffer> poses=r.poses[r->frameNumber%3];memcpy(poses.contents,previous,r->poseCount*64);memcpy((char*)poses.contents+r->poseCount*64,next,r->poseCount*64);
 NMUniforms u=r->uniforms;u.timing=(vector_float4){alpha,(float)seconds,(float)r->poseCount,r->effects?1:0};r->lastUniforms=u;id<MTLCommandBuffer> cb=[r.queue commandBuffer];encode(r,cb,poses,u,drawable?drawable.texture:r.output,nil);
 if(drawable){dispatch_group_enter(r->presentations);[drawable addPresentedHandler:^(id<MTLDrawable> d){@synchronized(r){if(r->rowGenerations[rowIndex]==generation)r->rows[rowIndex].presented=d.presentedTime;}dispatch_group_leave(r->presentations);}];[cb presentDrawable:drawable];}
 row->encode=(CACurrentMediaTime()-ready)*1000;row->submit=CACurrentMediaTime();row->memory=r.device.currentAllocatedSize;dispatch_group_enter(r->pending);
 [cb addCompletedHandler:^(id<MTLCommandBuffer> b){@synchronized(r){if(r->rowGenerations[rowIndex]==generation){NMRow* result=&r->rows[rowIndex];result->gpu_start=b.GPUStartTime;result->gpu_end=b.GPUEndTime;result->completed=CACurrentMediaTime();result->status=(int)b.status;}}if(b.error)r.lastError=b.error.localizedDescription;dispatch_semaphore_signal(r->available);dispatch_group_leave(r->pending);}];[cb commit];r->frameNumber++;return 1;}
}
static id<MTLBuffer> grow(NMRenderer* r,id<MTLBuffer> old,NSUInteger bytes) {
 if(old.length>=MAX(bytes,32))return old;NSUInteger capacity=MAX(old.length,4096);while(capacity<bytes)capacity*=2;return [r.device newBufferWithLength:capacity options:MTLResourceStorageModeShared];
}
int nm_live_frame(void* handle,const NMLiveUpload* upload) {
 @autoreleasepool {
 NMRenderer* r=(__bridge NMRenderer*)handle;if((!r->interactive&&r->rowCount>=r->rowCapacity)||upload->light_count>32||upload->overlay_count>INT32_MAX)return 0;pump(r);if(!r->offscreen&&!r.window.visible)return -1;
 double start=CACurrentMediaTime();dispatch_semaphore_wait(r->available,DISPATCH_TIME_FOREVER);double acquired=CACurrentMediaTime();id<CAMetalDrawable> drawable=nil;
 if(!r->offscreen){drawable=[r.layer nextDrawable];if(!drawable){dispatch_semaphore_signal(r->available);return 0;}}
 double ready=CACurrentMediaTime();NMRing* ring=r.rings[r->frameNumber%3];
 // Reuse/grow only the now-free slot. Existing command buffers retain their own
 // resources; no other in-flight slot is modified or resized here.
 ring.poses=grow(r,ring.poses,(NSUInteger)upload->pose_count*128);ring.instances=grow(r,ring.instances,(NSUInteger)upload->instance_count*4);ring.sprites=grow(r,ring.sprites,(NSUInteger)upload->sprite_count*96);ring.lights=grow(r,ring.lights,(NSUInteger)upload->light_count*32);ring.visuals=grow(r,ring.visuals,(NSUInteger)upload->instance_count*64);ring.distortions=grow(r,ring.distortions,(NSUInteger)upload->distortion_count*64);ring.selectors=grow(r,ring.selectors,(NSUInteger)upload->instance_count*8);ring.overlay=grow(r,ring.overlay,(NSUInteger)upload->overlay_count*64);
 if(!ring.poses||!ring.instances||!ring.sprites||!ring.lights||!ring.visuals||!ring.distortions||!ring.selectors||!ring.overlay){dispatch_semaphore_signal(r->available);return 0;}
 if(upload->pose_count){memcpy(ring.poses.contents,upload->previous,upload->pose_count*64);memcpy((char*)ring.poses.contents+upload->pose_count*64,upload->current,upload->pose_count*64);}
 if(upload->instance_count){memcpy(ring.instances.contents,upload->offsets,upload->instance_count*4);memcpy(ring.visuals.contents,upload->visuals,upload->instance_count*64);}
 if(upload->distortion_count)memcpy(ring.distortions.contents,upload->distortions,upload->distortion_count*64);ring.distortionCount=upload->distortion_count;
 if(upload->fog_width&&upload->fog_height){if(ring.fog.width!=upload->fog_width||ring.fog.height!=upload->fog_height){MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:upload->fog_width height:upload->fog_height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;ring.fog=[r.device newTextureWithDescriptor:d];}if(!ring.fog){dispatch_semaphore_signal(r->available);return 0;}[ring.fog replaceRegion:MTLRegionMake2D(0,0,upload->fog_width,upload->fog_height) mipmapLevel:0 withBytes:upload->fog bytesPerRow:upload->fog_width*4];}else ring.fog=nil;
 if(upload->overlay_count)memcpy(ring.overlay.contents,upload->overlay,upload->overlay_count*64);ring.overlayCount=upload->overlay_count;
 if(upload->sprite_count)memcpy(ring.sprites.contents,upload->sprites,upload->sprite_count*96);
 if(upload->light_count)memcpy(ring.lights.contents,upload->lights,upload->light_count*32);
 if(!ring.spans)ring.spans=[NSMutableData dataWithLength:r.meshes.count*24];if(r.meshes.count)memcpy(ring.spans.mutableBytes,upload->spans,r.meshes.count*24);ring.spriteCount=upload->sprite_count;const uint32_t* copiedSpans=ring.spans.bytes;NSUInteger selectorCount=0;for(NSUInteger i=r.meshes.count;i<r.meshes.count*3;i++)selectorCount+=copiedSpans[i*2+1];if(selectorCount)memcpy(ring.selectors.contents,upload->selectors,selectorCount*4);
 NMUniforms u=r->uniforms;u.camera=(vector_float4){upload->camera[0],upload->camera[1],upload->camera[2],0};u.timing=(vector_float4){upload->alpha,(float)upload->seconds,(float)upload->pose_count,r->effects?1:0};u.mode=(vector_float4){1,upload->light_count,0,0};u.visual.x=r->visualPasses?1:0;u.visual.z=upload->fog_width&&upload->fog_height?1:0;u.fogRect=(vector_float4){upload->fog_rect[0],upload->fog_rect[1],upload->fog_rect[2],upload->fog_rect[3]};r->lastUniforms=u;r.lastRing=ring;
 NSUInteger generation=++r->rowCount;NSUInteger rowIndex=(generation-1)%r->rowCapacity;NMRow* row=&r->rows[rowIndex];@synchronized(r){memset(row,0,sizeof(*row));r->rowGenerations[rowIndex]=generation;}row->wait=(acquired-start)*1000;row->drawable_wait=(ready-acquired)*1000;
 id<MTLCommandBuffer> cb=[r.queue commandBuffer];encode(r,cb,ring.poses,u,drawable?drawable.texture:r.output,ring);
 if(drawable){dispatch_group_enter(r->presentations);[drawable addPresentedHandler:^(id<MTLDrawable> d){@synchronized(r){if(r->rowGenerations[rowIndex]==generation)r->rows[rowIndex].presented=d.presentedTime;}dispatch_group_leave(r->presentations);}];[cb presentDrawable:drawable];}
 row->encode=(CACurrentMediaTime()-ready)*1000;row->submit=CACurrentMediaTime();row->memory=r.device.currentAllocatedSize;dispatch_group_enter(r->pending);
 [cb addCompletedHandler:^(id<MTLCommandBuffer> b){@synchronized(r){if(r->rowGenerations[rowIndex]==generation){NMRow* result=&r->rows[rowIndex];result->gpu_start=b.GPUStartTime;result->gpu_end=b.GPUEndTime;result->completed=CACurrentMediaTime();result->status=(int)b.status;}}if(b.error)r.lastError=b.error.localizedDescription;dispatch_semaphore_signal(r->available);dispatch_group_leave(r->pending);}];[cb commit];r->frameNumber++;return 1;
 }
}
void nm_drain(void* handle) {NMRenderer* r=(__bridge NMRenderer*)handle;dispatch_group_wait(r->pending,DISPATCH_TIME_FOREVER);if(!r->offscreen&&r->presentationDrainRowCount!=r->rowCount){r->presentationDrainRowCount=r->rowCount;r->presentationDrainTimedOut=dispatch_group_wait(r->presentations,dispatch_time(DISPATCH_TIME_NOW,250*NSEC_PER_MSEC))!=0;}}
int nm_rows(void* handle,void* output,int capacity) {NMRenderer* r=(__bridge NMRenderer*)handle;NSUInteger n=MIN((NSUInteger)MAX(capacity,0),MIN(r->rowCount,r->rowCapacity));@synchronized(r){NMRow* out=output;NSUInteger first=r->rowCount-n;for(NSUInteger i=0;i<n;i++)out[i]=r->rows[(first+i)%r->rowCapacity];}return (int)n;}
int nm_capture(void* handle,const char* path) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;nm_drain(handle);NSUInteger last=(r->frameNumber-1)%3;NMUniforms u=r->lastUniforms;
 id<MTLCommandBuffer> cb=[r.queue commandBuffer];encode(r,cb,r.lastRing?r.lastRing.poses:r.poses[last],u,r.output,r.lastRing);NSUInteger width=r.output.width,height=r.output.height,bytesPerRow=(width*4+255)&~255;
 id<MTLBuffer> read=[r.device newBufferWithLength:bytesPerRow*height options:MTLResourceStorageModeShared];id<MTLBlitCommandEncoder> blit=[cb blitCommandEncoder];[blit copyFromTexture:r.output sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(width,height,1) toBuffer:read destinationOffset:0 destinationBytesPerRow:bytesPerRow destinationBytesPerImage:bytesPerRow*height];[blit endEncoding];[cb commit];[cb waitUntilCompleted];if(cb.status==MTLCommandBufferStatusError)return 0;
 NSBitmapImageRep* image=[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:width pixelsHigh:height bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:width*4 bitsPerPixel:32];uint8_t* src=read.contents;uint8_t* dst=image.bitmapData;
 for(NSUInteger y=0;y<height;y++)for(NSUInteger x=0;x<width;x++){NSUInteger a=y*bytesPerRow+x*4,b=(y*width+x)*4;dst[b]=src[a+2];dst[b+1]=src[a+1];dst[b+2]=src[a];dst[b+3]=255;}
 NSData* png=[image representationUsingType:NSBitmapImageFileTypePNG properties:@{}];return [png writeToFile:[NSString stringWithUTF8String:path] atomically:YES]?1:0;}
}
void nm_info(void* handle,char* output,int capacity) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;NSUInteger poseBytes=0,instanceBytes=0,spriteBytes=0,lightBytes=0,visualBytes=0,distortionBytes=0,fogBytes=0,selectorBytes=0,overlayBytes=0;for(NMRing* ring in r.rings){overlayBytes+=ring.overlay.length;poseBytes+=ring.poses.length;instanceBytes+=ring.instances.length;spriteBytes+=ring.sprites.length;lightBytes+=ring.lights.length;visualBytes+=ring.visuals.length;distortionBytes+=ring.distortions.length;fogBytes+=ring.fog.width*ring.fog.height*4;selectorBytes+=ring.selectors.length;}NSDictionary* info=@{@"interactive":@(r->interactive),@"total_frames_submitted":@(r->rowCount),@"timing_capacity_frames":@(r->rowCapacity),@"input_queue_capacity":@NM_EVENT_CAPACITY,@"input_queue_overflow":@(r->eventOverflow),@"live_overlay_buffer_capacity_bytes":@(overlayBytes),@"retained_overlay_atlas_bytes":@(r.overlayAtlas.width*r.overlayAtlas.height*4),@"live_selector_buffer_capacity_bytes":@(selectorBytes),@"live_visual_buffer_capacity_bytes":@(visualBytes),@"live_distortion_buffer_capacity_bytes":@(distortionBytes),@"live_fog_texture_capacity_bytes":@(fogBytes),@"visual_passes":@(r->visualPasses),@"visual_hdr_target_bytes":@(r.postColor.width*r.postColor.height*8*2),@"visual_shadow_mask_bytes":@(r->visualPasses?r.shadowMask.width*r.shadowMask.height:0),@"visual_target_bytes":@(r.postColor.width*r.postColor.height*17),@"retained_height_bytes":@(r.heights.width*r.heights.height*4),@"retained_material_bytes":@(r.materials.length+r.textureFrames.length),@"live_pose_buffer_capacity_bytes":@(poseBytes),@"live_instance_buffer_capacity_bytes":@(instanceBytes),@"live_sprite_buffer_capacity_bytes":@(spriteBytes),@"live_light_buffer_capacity_bytes":@(lightBytes),@"device":r.device.name,@"current_allocated_bytes":@(r.device.currentAllocatedSize),@"recommended_working_set_bytes":@(r.device.recommendedMaxWorkingSetSize),@"unified_memory":@(r.device.hasUnifiedMemory),@"max_buffer_length":@(r.device.maxBufferLength),@"native_error":r.lastError?:@"",@"presentation_drain_timed_out":@(r->presentationDrainTimedOut),@"presentation_drain_timeout_ms":@250,@"drawable_width":@(r.output.width),@"drawable_height":@(r.output.height),@"view_width_points":@(r.window.contentView.bounds.size.width),@"view_height_points":@(r.window.contentView.bounds.size.height),@"layer_contents_scale":@(r.layer.contentsScale),@"screen_maximum_fps":@(NSScreen.mainScreen.maximumFramesPerSecond)};NSData* d=[NSJSONSerialization dataWithJSONObject:info options:0 error:nil];NSString* s=[[NSString alloc]initWithData:d encoding:NSUTF8StringEncoding];copyError(output,capacity,s);}
}
void nm_destroy(void* handle) { @autoreleasepool {NMRenderer* r=(__bridge_transfer NMRenderer*)handle;nm_drain((__bridge void*)r);[r.window orderOut:nil];[r.window close];} }
