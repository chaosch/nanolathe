#import <Cocoa/Cocoa.h>
#import <Metal/Metal.h>
#import <QuartzCore/CAMetalLayer.h>
#import <simd/simd.h>
#include "bridge.h"
#include <stdatomic.h>

@interface NMMesh : NSObject
@property id<MTLBuffer> vertices, indices, instances;
@property NSUInteger indexCount, instanceCount;
@end
@implementation NMMesh
@end

typedef struct { vector_float4 camera,rect,viewport,timing; } NMUniforms;
@interface NMRenderer : NSObject {
@public
 NMUniforms uniforms, lastUniforms;
 NMRow* rows;
 NSUInteger rowCount, rowCapacity, poseCount, frameNumber;
 dispatch_semaphore_t available;
 dispatch_group_t pending, presentations;
 BOOL presentationDrainTimedOut;
 NSUInteger presentationDrainRowCount;
 BOOL offscreen,effects;
}
@property id<MTLDevice> device;
@property id<MTLCommandQueue> queue;
@property id<MTLRenderPipelineState> model,terrain,composite;
@property id<MTLComputePipelineState> blur;
@property id<MTLDepthStencilState> depth;
@property NSMutableArray<NMMesh*>* meshes;
@property NSMutableArray<id<MTLBuffer>>* poses;
@property id<MTLTexture> atlas,terrainImage,color,emission,blurA,blurB,depthImage,output;
@property CAMetalLayer* layer;
@property NSWindow* window;
@property NSString* lastError;
@end
@implementation NMRenderer
- (void)dealloc { free(rows); }
@end

static id<MTLTexture> target(NMRenderer* r,NSUInteger w,NSUInteger h,MTLPixelFormat format,MTLTextureUsage usage) {
 MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:format width:w height:h mipmapped:NO];d.storageMode=MTLStorageModePrivate;d.usage=usage;return [r.device newTextureWithDescriptor:d];
}
static id<MTLRenderPipelineState> pipeline(NMRenderer* r,id<MTLLibrary> l,NSString* vertex,NSString* fragment,BOOL mrt,NSError** error) {
 MTLRenderPipelineDescriptor* d=[MTLRenderPipelineDescriptor new];d.vertexFunction=[l newFunctionWithName:vertex];d.fragmentFunction=[l newFunctionWithName:fragment];d.colorAttachments[0].pixelFormat=mrt?MTLPixelFormatRGBA16Float:MTLPixelFormatBGRA8Unorm;
 if(mrt){d.colorAttachments[1].pixelFormat=MTLPixelFormatRGBA16Float;d.depthAttachmentPixelFormat=MTLPixelFormatDepth32Float;}
 return [r.device newRenderPipelineStateWithDescriptor:d error:error];
}
static void copyError(char* out,int size,NSString* message) { if(out&&size>0)snprintf(out,size,"%s",message.UTF8String?:"unknown native failure"); }
void* nm_create(int width,int height,int isOffscreen,int vsync,int effects,int capacity,const char* shader,char* error,int errorSize) {
 @autoreleasepool {
 if(![NSThread isMainThread]){copyError(error,errorSize,@"Metal window must be initialized on the process main thread");return NULL;}
 NMRenderer* r=[NMRenderer new];r.device=MTLCreateSystemDefaultDevice();if(!r.device){copyError(error,errorSize,@"No Metal device");return NULL;}
 r.queue=[r.device newCommandQueue];r.meshes=[NSMutableArray new];r.poses=[NSMutableArray new];r->offscreen=isOffscreen;r->effects=effects;r->available=dispatch_semaphore_create(3);r->pending=dispatch_group_create();r->presentations=dispatch_group_create();r->rowCapacity=capacity;r->rows=calloc(capacity,sizeof(NMRow));
 r->uniforms.viewport=(vector_float4){width,height,0,0};
 NSError* e=nil;id<MTLLibrary> lib=[r.device newLibraryWithSource:[NSString stringWithUTF8String:shader] options:nil error:&e];
 if(!lib){copyError(error,errorSize,e.localizedDescription);return NULL;}
 r.model=pipeline(r,lib,@"model_vertex",@"model_fragment",YES,&e);r.terrain=pipeline(r,lib,@"terrain_vertex",@"terrain_fragment",YES,&e);r.composite=pipeline(r,lib,@"screen_vertex",@"composite_fragment",NO,&e);
 r.blur=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"blur"] error:&e];
 if(!r.model||!r.terrain||!r.composite||!r.blur){copyError(error,errorSize,e.localizedDescription);return NULL;}
 MTLDepthStencilDescriptor* depth=[MTLDepthStencilDescriptor new];depth.depthCompareFunction=MTLCompareFunctionLessEqual;depth.depthWriteEnabled=YES;r.depth=[r.device newDepthStencilStateWithDescriptor:depth];
 r.color=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.emission=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.blurA=target(r,MAX(width/2,1),MAX(height/2,1),MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
 r.blurB=target(r,MAX(width/2,1),MAX(height/2,1),MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
 r.depthImage=target(r,width,height,MTLPixelFormatDepth32Float,MTLTextureUsageRenderTarget);
 r.output=target(r,width,height,MTLPixelFormatBGRA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 if(!isOffscreen){
 [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
 CGFloat scale=NSScreen.mainScreen.backingScaleFactor;
 r.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,width/scale,height/scale) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];r.window.releasedWhenClosed=NO;r.window.title=@"Nanolathe retained Metal prototype";
 r.layer=[CAMetalLayer layer];r.layer.device=r.device;r.layer.pixelFormat=MTLPixelFormatBGRA8Unorm;r.layer.drawableSize=CGSizeMake(width,height);r.layer.contentsScale=scale;r.layer.framebufferOnly=YES;r.layer.maximumDrawableCount=3;r.layer.displaySyncEnabled=vsync;
 NSView* view=r.window.contentView;view.wantsLayer=YES;view.layer=r.layer;[r.window center];[r.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
 }
 return (__bridge_retained void*)r;
 }
}
int nm_mesh(void* handle,const void* vertices,int vertexCount,const uint32_t* indices,int indexCount,const uint32_t* offsets,int instances) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;NMMesh* m=[NMMesh new];m.vertices=[r.device newBufferWithBytes:vertices length:vertexCount*48 options:MTLResourceStorageModeShared];m.indices=[r.device newBufferWithBytes:indices length:indexCount*4 options:MTLResourceStorageModeShared];m.instances=[r.device newBufferWithBytes:offsets length:instances*4 options:MTLResourceStorageModeShared];m.indexCount=indexCount;m.instanceCount=instances;if(!m.vertices||!m.indices||!m.instances)return 0;[r.meshes addObject:m];return 1;}
}
int nm_texture(void* handle,int slot,int width,int height,const void* rgba) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:width height:height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;id<MTLTexture> t=[r.device newTextureWithDescriptor:d];if(!t)return 0;[t replaceRegion:MTLRegionMake2D(0,0,width,height) mipmapLevel:0 withBytes:rgba bytesPerRow:width*4];if(slot==0)r.atlas=t;else r.terrainImage=t;return 1;}
}
int nm_configure(void* handle,const float* camera,const float* rect,int count) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;r->uniforms.camera=(vector_float4){camera[0],camera[1],camera[2],0};r->uniforms.rect=(vector_float4){rect[0],rect[1],rect[2],rect[3]};r->poseCount=count;
 for(int i=0;i<3;i++){id<MTLBuffer> b=[r.device newBufferWithLength:count*128 options:MTLResourceStorageModeShared];if(!b)return 0;[r.poses addObject:b];}return 1;}
}
static void pump(NMRenderer* r) {
 if(r->offscreen)return;
 NSEvent* event;while((event=[NSApp nextEventMatchingMask:NSEventMaskAny untilDate:[NSDate distantPast] inMode:NSDefaultRunLoopMode dequeue:YES]))[NSApp sendEvent:event];[NSApp updateWindows];
}
static void encode(NMRenderer* r,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMUniforms u,id<MTLTexture> destination) {
 MTLRenderPassDescriptor* pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.color;pass.colorAttachments[0].loadAction=MTLLoadActionClear;pass.colorAttachments[0].storeAction=MTLStoreActionStore;pass.colorAttachments[0].clearColor=MTLClearColorMake(0.08,0.09,0.1,1);
 pass.colorAttachments[1].texture=r.emission;pass.colorAttachments[1].loadAction=MTLLoadActionClear;pass.colorAttachments[1].storeAction=MTLStoreActionStore;
 pass.depthAttachment.texture=r.depthImage;pass.depthAttachment.loadAction=MTLLoadActionClear;pass.depthAttachment.storeAction=MTLStoreActionDontCare;pass.depthAttachment.clearDepth=1;
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setDepthStencilState:r.depth];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setCullMode:MTLCullModeNone];
 [enc setRenderPipelineState:r.terrain];[enc setFragmentTexture:r.terrainImage atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];
 [enc setRenderPipelineState:r.model];[enc setVertexBuffer:poses offset:0 atIndex:2];[enc setFragmentTexture:r.atlas atIndex:0];
 for(NMMesh* m in r.meshes){[enc setVertexBuffer:m.vertices offset:0 atIndex:0];[enc setVertexBuffer:m.instances offset:0 atIndex:1];[enc drawIndexedPrimitives:MTLPrimitiveTypeTriangle indexCount:m.indexCount indexType:MTLIndexTypeUInt32 indexBuffer:m.indices indexBufferOffset:0 instanceCount:m.instanceCount];}[enc endEncoding];
 if(r->effects){
 id<MTLComputeCommandEncoder> compute=[cb computeCommandEncoder];[compute setComputePipelineState:r.blur];vector_float2 d={1,0};[compute setBytes:&d length:sizeof(d) atIndex:0];[compute setTexture:r.emission atIndex:0];[compute setTexture:r.blurA atIndex:1];[compute dispatchThreads:MTLSizeMake(r.blurA.width,r.blurA.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[compute endEncoding];
 compute=[cb computeCommandEncoder];[compute setComputePipelineState:r.blur];d=(vector_float2){0,1};[compute setBytes:&d length:sizeof(d) atIndex:0];[compute setTexture:r.blurA atIndex:0];[compute setTexture:r.blurB atIndex:1];[compute dispatchThreads:MTLSizeMake(r.blurB.width,r.blurB.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[compute endEncoding];
 }
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=destination;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 enc=[cb renderCommandEncoderWithDescriptor:pass];[enc setRenderPipelineState:r.composite];[enc setFragmentTexture:r.color atIndex:0];[enc setFragmentTexture:r.blurB atIndex:1];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];
}
int nm_frame(void* handle,const void* previous,const void* next,float alpha,double seconds) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;if(r->rowCount>=r->rowCapacity)return 0;pump(r);if(!r->offscreen&&!r.window.visible)return -1;
 double start=CACurrentMediaTime();dispatch_semaphore_wait(r->available,DISPATCH_TIME_FOREVER);double acquired=CACurrentMediaTime();
 id<CAMetalDrawable> drawable=nil;if(!r->offscreen){drawable=[r.layer nextDrawable];if(!drawable){dispatch_semaphore_signal(r->available);return 0;}}
 double ready=CACurrentMediaTime();NSUInteger rowIndex=r->rowCount++;NMRow* row=&r->rows[rowIndex];row->wait=(acquired-start)*1000;row->drawable_wait=(ready-acquired)*1000;
 id<MTLBuffer> poses=r.poses[r->frameNumber%3];memcpy(poses.contents,previous,r->poseCount*64);memcpy((char*)poses.contents+r->poseCount*64,next,r->poseCount*64);
 NMUniforms u=r->uniforms;u.timing=(vector_float4){alpha,(float)seconds,(float)r->poseCount,r->effects?1:0};r->lastUniforms=u;id<MTLCommandBuffer> cb=[r.queue commandBuffer];encode(r,cb,poses,u,drawable?drawable.texture:r.output);
 if(drawable){dispatch_group_enter(r->presentations);[drawable addPresentedHandler:^(id<MTLDrawable> d){@synchronized(r){r->rows[rowIndex].presented=d.presentedTime;}dispatch_group_leave(r->presentations);}];[cb presentDrawable:drawable];}
 row->encode=(CACurrentMediaTime()-ready)*1000;row->submit=CACurrentMediaTime();row->memory=r.device.currentAllocatedSize;dispatch_group_enter(r->pending);
 [cb addCompletedHandler:^(id<MTLCommandBuffer> b){NMRow* result=&r->rows[rowIndex];result->gpu_start=b.GPUStartTime;result->gpu_end=b.GPUEndTime;result->completed=CACurrentMediaTime();result->status=(int)b.status;if(b.error)r.lastError=b.error.localizedDescription;dispatch_semaphore_signal(r->available);dispatch_group_leave(r->pending);}];[cb commit];r->frameNumber++;return 1;}
}
void nm_drain(void* handle) {NMRenderer* r=(__bridge NMRenderer*)handle;dispatch_group_wait(r->pending,DISPATCH_TIME_FOREVER);if(!r->offscreen&&r->presentationDrainRowCount!=r->rowCount){r->presentationDrainRowCount=r->rowCount;r->presentationDrainTimedOut=dispatch_group_wait(r->presentations,dispatch_time(DISPATCH_TIME_NOW,250*NSEC_PER_MSEC))!=0;}}
int nm_rows(void* handle,void* output,int capacity) {NMRenderer* r=(__bridge NMRenderer*)handle;NSUInteger n=MIN((NSUInteger)capacity,r->rowCount);@synchronized(r){memcpy(output,r->rows,n*sizeof(NMRow));}return (int)n;}
int nm_capture(void* handle,const char* path) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;nm_drain(handle);NSUInteger last=(r->frameNumber-1)%3;NMUniforms u=r->lastUniforms;
 id<MTLCommandBuffer> cb=[r.queue commandBuffer];encode(r,cb,r.poses[last],u,r.output);NSUInteger width=r.output.width,height=r.output.height,bytesPerRow=(width*4+255)&~255;
 id<MTLBuffer> read=[r.device newBufferWithLength:bytesPerRow*height options:MTLResourceStorageModeShared];id<MTLBlitCommandEncoder> blit=[cb blitCommandEncoder];[blit copyFromTexture:r.output sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(width,height,1) toBuffer:read destinationOffset:0 destinationBytesPerRow:bytesPerRow destinationBytesPerImage:bytesPerRow*height];[blit endEncoding];[cb commit];[cb waitUntilCompleted];if(cb.status==MTLCommandBufferStatusError)return 0;
 NSBitmapImageRep* image=[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:width pixelsHigh:height bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:width*4 bitsPerPixel:32];uint8_t* src=read.contents;uint8_t* dst=image.bitmapData;
 for(NSUInteger y=0;y<height;y++)for(NSUInteger x=0;x<width;x++){NSUInteger a=y*bytesPerRow+x*4,b=(y*width+x)*4;dst[b]=src[a+2];dst[b+1]=src[a+1];dst[b+2]=src[a];dst[b+3]=255;}
 NSData* png=[image representationUsingType:NSBitmapImageFileTypePNG properties:@{}];return [png writeToFile:[NSString stringWithUTF8String:path] atomically:YES]?1:0;}
}
void nm_info(void* handle,char* output,int capacity) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;NSDictionary* info=@{@"device":r.device.name,@"current_allocated_bytes":@(r.device.currentAllocatedSize),@"recommended_working_set_bytes":@(r.device.recommendedMaxWorkingSetSize),@"unified_memory":@(r.device.hasUnifiedMemory),@"max_buffer_length":@(r.device.maxBufferLength),@"native_error":r.lastError?:@"",@"presentation_drain_timed_out":@(r->presentationDrainTimedOut),@"presentation_drain_timeout_ms":@250,@"drawable_width":@(r.output.width),@"drawable_height":@(r.output.height),@"view_width_points":@(r.window.contentView.bounds.size.width),@"view_height_points":@(r.window.contentView.bounds.size.height),@"layer_contents_scale":@(r.layer.contentsScale),@"screen_maximum_fps":@(NSScreen.mainScreen.maximumFramesPerSecond)};NSData* d=[NSJSONSerialization dataWithJSONObject:info options:0 error:nil];NSString* s=[[NSString alloc]initWithData:d encoding:NSUTF8StringEncoding];copyError(output,capacity,s);}
}
void nm_destroy(void* handle) { @autoreleasepool {NMRenderer* r=(__bridge_transfer NMRenderer*)handle;nm_drain((__bridge void*)r);[r.window orderOut:nil];[r.window close];} }
