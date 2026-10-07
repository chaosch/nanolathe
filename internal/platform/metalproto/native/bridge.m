#import <Cocoa/Cocoa.h>
#import <CoreFoundation/CoreFoundation.h>
#import <Metal/Metal.h>
#import <QuartzCore/CAMetalLayer.h>
#import <simd/simd.h>
#include "bridge.h"
#include "timing.inc"
#include "glow.h"
#include "glow.inc"
#include "water_tiles.h"
#include "water_tiles.inc"
#include "terrain_tiles.h"
#include "terrain_tiles.inc"
#include <stdatomic.h>

@class NMFaceMesh,NMFaceState,NMFaceRing,NMOutlineRowsMesh;
@interface NMMesh : NSObject
@property NMFaceMesh* faceMesh;
@property NMOutlineRowsMesh* outlineRowsMesh;
@property id<MTLBuffer> vertices, indices, instances, edges, visuals, primitiveCenters;
@property NSUInteger indexCount, instanceCount, edgeCount;
@end
@implementation NMMesh
@end

typedef struct { vector_float4 camera,rect,viewport,timing,mode,heightRect,fogRect,visual,composition,lightControl; } NMUniforms;
#include "water_objects.h"
#include "groups.h"
#include "faces.h"
#include "face_reflections.h"
#include "outline_rows.h"
@interface NMRing : NSObject
@property NMGroupFrame* groups;
@property NMFaceRing* faceRing;
@property NMOutlineRowsRing* outlineRowsRing;
@property NMFaceReflectionFrame* faceReflections;
@property NMEffectsRing* effectRing;
@property NMGroundRing* groundRing;
@property NMShadowFrame* shadows;
@property NMWaterObjectsRing* waterObjectsRing;
@property id<MTLBuffer> glowVertices;
@property NSUInteger glowCount;
@property BOOL glowEnabled;
@property NMGlowParameters glowParams;
@property id<MTLBuffer> poses,interpolatedPoses,instances,sprites,lights,visuals,distortions,selectors,overlay,overlayUpload,rules,materialOffsets,materialOverrides,retainedLights,subjectLights;
@property NSMutableData *spans,*cloaks,*compositionDraws;
@property id<MTLBuffer> compositionSlots,compositionSelectors,compositionPaint;
@property NSMutableData* slotRanks; // per slot: paint index, or UINT32_MAX
@property NSUInteger directSlotCount;
@property NSUInteger compositionPageWidth,compositionPageHeight; // this frame's raster-scaled page
@property NSUInteger compositionSlotCount,compositionDrawCount,compositionPaintCount;
@property BOOL composed;
@property uint32_t terrainFlags;
@property id<MTLBuffer> projections,projectionFlags;
@property vector_float4 projectionView;
@property NSUInteger spriteCount,groundSpriteCount,distortionCount,overlayCount,cloakCount,airCount;
@property id<MTLTexture> fog,overlayAtlas;
@end
@implementation NMRing
@end
_Static_assert(sizeof(NMLiveUpload)==656,"live upload layout");
_Static_assert(sizeof(NMAssets)==56,"retained assets layout");
_Static_assert(sizeof(NMInput)==16,"input layout");
_Static_assert(sizeof(NMEvent)==32,"event layout");
#define NM_EVENT_CAPACITY 8192
#define NM_SPAN_PASSES 9
_Static_assert(sizeof(NMCloakDraw)==16,"cloak draw layout");
@interface NMRenderer : NSObject {
@public
 NMUniforms uniforms, lastUniforms;
 float water[20];
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
 BOOL interactive,playable,wasKey,cursorHidden,pointerCaptured;
 NSPoint pointerRestore;
 double pointerWarpX,pointerWarpY;
 uint32_t pointerModifiers;
 uint64_t overlayVersion,overlayTextureUploads,overlayTextureBytes,runLoopPumps,runLoopSources;
 BOOL offscreen,effects,visualPasses,held[128],pressed[128],pauseToggle;
 float wheel;
}
@property NMTerrainTiles* terrainTiles;
@property NMEffectsState* effectState;
@property NMGroundState* groundState;
@property NMShadowState* shadows;
@property NMWaterObjectsState* waterObjectsState;
@property NMGlow* productionGlow;
@property NMGroupState* groups;
@property NMFaceState* faceState;
@property NMOutlineRowsState* outlineRowsState;
@property NMFaceReflectionState* faceReflections;
@property id<MTLDevice> device;
@property id<MTLCommandQueue> queue;
@property id<MTLRenderPipelineState> directModel,finalPass,distortionFinal,shadowBody;
@property id<MTLRenderPipelineState> model,subjectModel,subjectResolve,atlasModel,atlasModelMetadata,atlasOutline,shadowOutline,atlasCommit,groundLight,clearDepth,terrain,composite,sprite,shadow,outline,world,distortion,fog,overlay,overlayMultiply;
@property id<MTLComputePipelineState> blur,interpolatePoses,selectLights;
@property id<MTLDepthStencilState> depth,spriteDepth,alwaysDepth;
@property NSMutableArray<NMMesh*>* meshes;
@property NSMutableArray<id<MTLBuffer>> *poses,*interpolatedPoses;
@property NSMutableArray<NMRing*>* rings;
@property NMRing* lastRing;
@property id<MTLBuffer> emptyLights,materials,textureFrames;
@property id<MTLTexture> spriteAtlas,overlayAtlas,fogAtlas,waterMask,modelPalette;
@property id<MTLTexture> atlas,terrainImage,color,emission,blurA,blurB,depthImage,output,heights,shadowMask,worldSnapshot,postColor,emptyFog,subjectColor,subjectEmission,subjectDepth;
@property id<MTLTexture> modelMetadata,modelAtlasColor,modelAtlasEmission,modelAtlasDepth,groundField;
@property CAMetalLayer* layer;
@property NSWindow* window;
@property NSString* lastError;
@end
@implementation NMRenderer
- (void)dealloc { free(rows);free(rowGenerations); }
@end

int nm_terrain_tiles(void* handle,const void* upload){@autoreleasepool{NMRenderer* r=(__bridge NMRenderer*)handle;NSError* error=nil;if(!nmTerrainTilesPrepare(r.terrainTiles,upload,&error)){r.lastError=error.localizedDescription;return 0;}return 1;}}

static id<MTLTexture> target(NMRenderer* r,NSUInteger w,NSUInteger h,MTLPixelFormat format,MTLTextureUsage usage) {
 MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:format width:w height:h mipmapped:NO];d.storageMode=MTLStorageModePrivate;d.usage=usage;return [r.device newTextureWithDescriptor:d];
}
static id<MTLRenderPipelineState> pipeline(NMRenderer* r,id<MTLLibrary> l,NSString* vertex,NSString* fragment,BOOL mrt,NSError** error) {
 MTLRenderPipelineDescriptor* d=[MTLRenderPipelineDescriptor new];if([vertex isEqualToString:@"model_vertex"]){MTLFunctionConstantValues* values=[MTLFunctionConstantValues new];bool on=[fragment isEqualToString:@"model_direct_fragment"];[values setConstantValue:&on type:MTLDataTypeBool atIndex:0];d.vertexFunction=[l newFunctionWithName:vertex constantValues:values error:error];if(!d.vertexFunction)return nil;}else d.vertexFunction=[l newFunctionWithName:vertex];d.fragmentFunction=[l newFunctionWithName:[fragment isEqualToString:@"shadow_outline"]?@"outline_fragment":([fragment isEqualToString:@"subject_model"]||[fragment isEqualToString:@"atlas_model"])?@"model_fragment":fragment];d.colorAttachments[0].pixelFormat=(mrt||[fragment isEqualToString:@"world_fragment"]||[fragment isEqualToString:@"distortion_fragment"]||[fragment isEqualToString:@"shadow_fragment"])?MTLPixelFormatRGBA16Float:MTLPixelFormatBGRA8Unorm;
 if([vertex isEqualToString:@"overlay_vertex"]){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=MTLBlendFactorOneMinusSourceAlpha;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;}
 if([fragment isEqualToString:@"overlay_multiply_fragment"]){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.sourceRGBBlendFactor=MTLBlendFactorZero;a.destinationRGBBlendFactor=MTLBlendFactorSourceColor;a.sourceAlphaBlendFactor=MTLBlendFactorZero;a.destinationAlphaBlendFactor=MTLBlendFactorOne;}
 if([vertex isEqualToString:@"sprite_vertex"]||[fragment isEqualToString:@"model_fragment"]||[fragment isEqualToString:@"outline_fragment"]||[fragment isEqualToString:@"subject_model"]||[fragment isEqualToString:@"subject_resolve_fragment"]||[fragment isEqualToString:@"model_commit_fragment"]||[fragment isEqualToString:@"model_direct_fragment"]){
 for(int i=0;i<2;i++){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[i];a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=(i==0||[fragment isEqualToString:@"subject_model"]||[fragment isEqualToString:@"model_direct_fragment"])?MTLBlendFactorOneMinusSourceAlpha:MTLBlendFactorOne;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOneMinusSourceAlpha;}
 }
 if([fragment isEqualToString:@"shadow_fragment"]){d.colorAttachments[0].pixelFormat=MTLPixelFormatR8Unorm;MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.blendingEnabled=YES;a.rgbBlendOperation=MTLBlendOperationMax;a.alphaBlendOperation=MTLBlendOperationMax;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=MTLBlendFactorOne;a.sourceAlphaBlendFactor=MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOne;}
 if([fragment isEqualToString:@"ground_light_fragment"]){MTLRenderPipelineColorAttachmentDescriptor* a=d.colorAttachments[0];a.pixelFormat=MTLPixelFormatRGBA8Unorm;a.blendingEnabled=YES;a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=MTLBlendFactorOneMinusSourceColor;}
 if([fragment isEqualToString:@"model_atlas_fragment"]||[fragment isEqualToString:@"outline_atlas_fragment"])d.colorAttachments[2].pixelFormat=MTLPixelFormatRGBA16Float;
 if(mrt){d.colorAttachments[1].pixelFormat=MTLPixelFormatRGBA16Float;d.depthAttachmentPixelFormat=MTLPixelFormatDepth32Float;}
 if([fragment isEqualToString:@"model_atlas_fragment"]||[fragment isEqualToString:@"outline_atlas_fragment"]||[fragment isEqualToString:@"atlas_model"]||[fragment isEqualToString:@"shadow_outline"]||[fragment isEqualToString:@"shadow_body_fragment"]){d.colorAttachments[0].pixelFormat=MTLPixelFormatRGBA8Unorm;d.colorAttachments[1].pixelFormat=MTLPixelFormatRGBA8Unorm;}
 if([fragment isEqualToString:@"shadow_body_fragment"])d.colorAttachments[1].writeMask=MTLColorWriteMaskNone;
 if([fragment isEqualToString:@"clear_depth_fragment"]){d.colorAttachments[0].writeMask=MTLColorWriteMaskNone;d.colorAttachments[1].writeMask=MTLColorWriteMaskNone;}
 return [r.device newRenderPipelineStateWithDescriptor:d error:error];
}
static void copyError(char* out,int size,NSString* message) { if(out&&size>0)snprintf(out,size,"%s",message.UTF8String?:"unknown native failure"); }
static void serviceRunLoop(NMRenderer* r) {
 // Dequeuing NSEvents with an expired deadline need not service the main
 // thread's Mach-port sources, timers or observers. Give AppleEvents and
 // accessibility requests an explicit nonblocking pass (Apple CFRunLoop API).
 // Bound ready-source draining so presentation keeps control under a flood.
 r->runLoopPumps++;
 for(int i=0;i<8;i++){CFRunLoopRunResult result=CFRunLoopRunInMode(kCFRunLoopDefaultMode,0,true);if(result!=kCFRunLoopRunHandledSource)break;r->runLoopSources++;}
}
static void nmProjectionBind(NMRing*,id<MTLRenderCommandEncoder>,NSUInteger);
static BOOL nmRetainedProjectile(NMRenderer*,NMRing*,id<MTLCommandBuffer>,const void*,uint32_t,uint32_t);
void* nm_create(int width,int height,int isOffscreen,int vsync,int effects,int visualPasses,int capacity,const char* shader,char* error,int errorSize) {
 @autoreleasepool {
 if(![NSThread isMainThread]){copyError(error,errorSize,@"Metal window must be initialized on the process main thread");return NULL;}
 NMRenderer* r=[NMRenderer new];r.device=MTLCreateSystemDefaultDevice();nmTimingInitialize(r.device);if(!r.device){copyError(error,errorSize,@"No Metal device");return NULL;}
 r.queue=[r.device newCommandQueue];r.rings=[NSMutableArray new];for(int i=0;i<3;i++)[r.rings addObject:[NMRing new]];r.emptyLights=[r.device newBufferWithLength:64 options:MTLResourceStorageModeShared];r.meshes=[NSMutableArray new];r.poses=[NSMutableArray new];r.interpolatedPoses=[NSMutableArray new];r->offscreen=isOffscreen;r->effects=effects;r->visualPasses=visualPasses;r->available=dispatch_semaphore_create(3);r->pending=dispatch_group_create();r->presentations=dispatch_group_create();r->interactive=capacity<0;r->rowCapacity=capacity<0?-(int64_t)capacity:capacity;if(!r->rowCapacity){copyError(error,errorSize,@"Timing capacity must be positive");return NULL;}r->rows=calloc(r->rowCapacity,sizeof(NMRow));r->rowGenerations=calloc(r->rowCapacity,sizeof(uint64_t));if(!r->rows||!r->rowGenerations){copyError(error,errorSize,@"Timing storage allocation failed");return NULL;}
 r->uniforms.viewport=(vector_float4){width,height,0,0};
 NSError* e=nil;id<MTLLibrary> lib=[r.device newLibraryWithSource:[NSString stringWithUTF8String:shader] options:nil error:&e];
 if(!lib){copyError(error,errorSize,e.localizedDescription);return NULL;}
 r.terrainTiles=nmTerrainTilesInitialize(r.device,&e);if(!r.terrainTiles){copyError(error,errorSize,e.localizedDescription);return NULL;}
 if(!nmEffectsInitialize(r,lib,&e)){copyError(error,errorSize,e.localizedDescription);return NULL;}if(!nmGroundInitialize(r,lib,&e)){copyError(error,errorSize,e.localizedDescription);return NULL;}
 if(!nmOutlineRowsInitialize(r,lib,&e)||!nmGroupsInitialize(r,lib,&e)||!nmFacesInitialize(r,lib,&e)||!nmFaceReflectionsInitialize(r,lib,&e)){copyError(error,errorSize,e.localizedDescription?:r.lastError);return NULL;}
 if(!nmWaterObjectsInitialize(r,lib,&e)){copyError(error,errorSize,e.localizedDescription);return NULL;}
 r.shadows=nmShadowsCreate(r,lib,&e);if(!r.shadows){copyError(error,errorSize,e.localizedDescription);return NULL;}
 nmEffectsSetRetainedDrawer(r,nmRetainedProjectile);r.productionGlow=nmGlowInitialize(r.device,lib,MTLPixelFormatRGBA16Float,&e);if(!r.productionGlow){copyError(error,errorSize,e.localizedDescription);return NULL;}
 r.groundLight=pipeline(r,lib,@"ground_light_vertex",@"ground_light_fragment",NO,&e);
 r.shadowOutline=pipeline(r,lib,@"model_vertex",@"shadow_outline",YES,&e);r.atlasModelMetadata=pipeline(r,lib,@"model_vertex",@"model_atlas_fragment",YES,&e);r.directModel=pipeline(r,lib,@"model_vertex",@"model_direct_fragment",YES,&e);r.atlasOutline=pipeline(r,lib,@"model_vertex",@"outline_atlas_fragment",YES,&e);
 r.atlasModel=pipeline(r,lib,@"model_vertex",@"atlas_model",YES,&e);r.shadowBody=pipeline(r,lib,@"model_vertex",@"shadow_body_fragment",YES,&e);r.atlasCommit=pipeline(r,lib,@"model_commit_vertex",@"model_commit_fragment",YES,&e);
 r.model=pipeline(r,lib,@"model_vertex",@"model_fragment",YES,&e);r.subjectModel=pipeline(r,lib,@"model_vertex",@"subject_model",YES,&e);r.subjectResolve=pipeline(r,lib,@"screen_vertex",@"subject_resolve_fragment",YES,&e);r.clearDepth=pipeline(r,lib,@"screen_vertex",@"clear_depth_fragment",YES,&e);r.terrain=pipeline(r,lib,@"terrain_vertex",@"terrain_fragment",YES,&e);r.composite=pipeline(r,lib,@"screen_vertex",@"composite_fragment",NO,&e);
 r.shadow=pipeline(r,lib,@"shadow_vertex",@"shadow_fragment",NO,&e);r.outline=pipeline(r,lib,@"model_vertex",@"outline_fragment",YES,&e);r.world=pipeline(r,lib,@"screen_vertex",@"world_fragment",NO,&e);r.distortion=pipeline(r,lib,@"distortion_vertex",@"distortion_fragment",NO,&e);r.fog=pipeline(r,lib,@"screen_vertex",@"fog_fragment",NO,&e);r.finalPass=pipeline(r,lib,@"screen_vertex",@"final_fragment",NO,&e);r.distortionFinal=pipeline(r,lib,@"distortion_vertex",@"distortion_final_fragment",NO,&e);r.overlay=pipeline(r,lib,@"overlay_vertex",@"overlay_fragment",NO,&e);r.overlayMultiply=pipeline(r,lib,@"overlay_vertex",@"overlay_multiply_fragment",NO,&e);
 r.materials=[r.device newBufferWithLength:16 options:MTLResourceStorageModeShared];r.textureFrames=[r.device newBufferWithLength:16 options:MTLResourceStorageModeShared];
 MTLTextureDescriptor* hd=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatR32Float width:1 height:1 mipmapped:NO];hd.storageMode=MTLStorageModeShared;hd.usage=MTLTextureUsageShaderRead;r.heights=[r.device newTextureWithDescriptor:hd];float zero=0;[r.heights replaceRegion:MTLRegionMake2D(0,0,1,1) mipmapLevel:0 withBytes:&zero bytesPerRow:4];
 hd.pixelFormat=MTLPixelFormatRGBA8Unorm;r.emptyFog=[r.device newTextureWithDescriptor:hd];uint32_t white=0xffffffff;[r.emptyFog replaceRegion:MTLRegionMake2D(0,0,1,1) mipmapLevel:0 withBytes:&white bytesPerRow:4];
 r.sprite=pipeline(r,lib,@"sprite_vertex",@"sprite_fragment",YES,&e);
 r.blur=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"blur"] error:&e];
 r.selectLights=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"select_subject_lights"] error:&e];
 r.interpolatePoses=[r.device newComputePipelineStateWithFunction:[lib newFunctionWithName:@"interpolate_poses"] error:&e];
 if(!r.shadowOutline||!r.atlasModelMetadata||!r.directModel||!r.shadowBody||!r.finalPass||!r.distortionFinal||!r.atlasOutline||!r.selectLights||!r.groundLight||!r.atlasModel||!r.atlasCommit||!r.model||!r.clearDepth||!r.subjectModel||!r.subjectResolve||!r.terrain||!r.composite||!r.sprite||!r.blur||!r.interpolatePoses||!r.shadow||!r.outline||!r.world||!r.distortion||!r.fog||!r.overlay||!r.overlayMultiply){copyError(error,errorSize,e.localizedDescription);return NULL;}
 MTLDepthStencilDescriptor* depth=[MTLDepthStencilDescriptor new];depth.depthCompareFunction=MTLCompareFunctionLessEqual;depth.depthWriteEnabled=YES;r.depth=[r.device newDepthStencilStateWithDescriptor:depth];depth.depthWriteEnabled=NO;r.spriteDepth=[r.device newDepthStencilStateWithDescriptor:depth];depth.depthWriteEnabled=YES;depth.depthCompareFunction=MTLCompareFunctionAlways;r.alwaysDepth=[r.device newDepthStencilStateWithDescriptor:depth];
 r.groundField=target(r,(width+1)/2,(height+1)/2,MTLPixelFormatRGBA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.color=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.emission=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.blurA=target(r,MAX(width/2,1),MAX(height/2,1),MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
 r.blurB=target(r,MAX(width/2,1),MAX(height/2,1),MTLPixelFormatRGBA16Float,MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite);
 if(visualPasses){r.shadowMask=target(r,width,height,MTLPixelFormatR8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.worldSnapshot=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.postColor=target(r,width,height,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 }else{r.shadowMask=r.emptyFog;}
 r.depthImage=target(r,width,height,MTLPixelFormatDepth32Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 r.output=target(r,width,height,MTLPixelFormatBGRA8Unorm,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);
 if(!isOffscreen){
 [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
 // Our loop pumps events instead of calling NSApplication.run. Complete launch
 // explicitly so Launch Services and accessibility can attach to the app.
 [NSApp finishLaunching];
 CGFloat scale=NSScreen.mainScreen.backingScaleFactor;
 r.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,width/scale,height/scale) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];r.window.releasedWhenClosed=NO;r.window.title=@"Nanolathe retained Metal prototype";
 r.layer=[CAMetalLayer layer];r.layer.device=r.device;r.layer.pixelFormat=MTLPixelFormatBGRA8Unorm;r.layer.drawableSize=CGSizeMake(width,height);r.layer.contentsScale=scale;r.layer.framebufferOnly=YES;r.layer.maximumDrawableCount=3;r.layer.displaySyncEnabled=vsync;
 NSView* view=r.window.contentView;view.wantsLayer=YES;view.layer=r.layer;r.window.acceptsMouseMovedEvents=YES;[r.window center];[r.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];serviceRunLoop(r);[NSApp updateWindows];
 }
 return (__bridge_retained void*)r;
 }
}
int nm_mesh(void* handle,const void* vertices,int vertexCount,const uint32_t* indices,int indexCount,const uint32_t* offsets,int instances,const uint32_t* edges,int edgeCount,const void* visuals,const void* primitiveCenters,int primitiveCount) {
 @autoreleasepool {
 NMRenderer* r=(__bridge NMRenderer*)handle;NMMesh* m=[NMMesh new];
 m.vertices=[r.device newBufferWithLength:MAX((NSUInteger)vertexCount*64,64) options:MTLResourceStorageModeShared];
 m.indices=[r.device newBufferWithLength:MAX((NSUInteger)indexCount*4,4) options:MTLResourceStorageModeShared];
 m.instances=[r.device newBufferWithLength:MAX((NSUInteger)instances*4,4) options:MTLResourceStorageModeShared];
 m.edges=[r.device newBufferWithLength:MAX((NSUInteger)edgeCount*4,4) options:MTLResourceStorageModeShared];m.visuals=[r.device newBufferWithLength:MAX((NSUInteger)instances*64,64) options:MTLResourceStorageModeShared];
 m.primitiveCenters=[r.device newBufferWithLength:MAX((NSUInteger)primitiveCount*16,16) options:MTLResourceStorageModeShared];if(primitiveCount)memcpy(m.primitiveCenters.contents,primitiveCenters,primitiveCount*16);
 if(!m.primitiveCenters||!m.vertices||!m.indices||!m.instances||!m.edges||!m.visuals)return 0;
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
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:width height:height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;id<MTLTexture> t=[r.device newTextureWithDescriptor:d];if(!t)return 0;[t replaceRegion:MTLRegionMake2D(0,0,width,height) mipmapLevel:0 withBytes:rgba bytesPerRow:width*4];if(slot==0)r.atlas=t;else if(slot==1)r.terrainImage=t;else if(slot==2)r.spriteAtlas=t;else if(slot==3)r.overlayAtlas=t;else if(slot==4)r.fogAtlas=t;else if(slot==5)r.waterMask=t;else if(slot==6)r.modelPalette=t;else return 0;return 1;}
}
int nm_configure(void* handle,const float* camera,const float* rect,int count) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;r->uniforms.camera=(vector_float4){camera[0],camera[1],camera[2],0};r->uniforms.rect=(vector_float4){rect[0],rect[1],rect[2],rect[3]};r->poseCount=count;
 for(int i=0;i<3;i++){id<MTLBuffer> b=[r.device newBufferWithLength:MAX((NSUInteger)count*128,128) options:MTLResourceStorageModeShared];id<MTLBuffer> out=[r.device newBufferWithLength:MAX((NSUInteger)count*64,64) options:MTLResourceStorageModePrivate];if(!b||!out)return 0;[r.poses addObject:b];[r.interpolatedPoses addObject:out];}return 1;}
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
static CGPoint quartzPoint(NSPoint point) {
 NSScreen* primary=NSScreen.screens.firstObject;return CGPointMake(point.x,NSMaxY(primary.frame)-point.y);
}
static NSPoint viewToScreen(NMRenderer* r,NSPoint point) {
 return [r.window convertPointToScreen:[r.window.contentView convertPoint:point toView:nil]];
}
static BOOL restorePointer(NMRenderer* r) {
 if(!r->pointerCaptured)return YES;
 r->pointerCaptured=NO;r->pointerWarpX=r->pointerWarpY=0;
 CGError warp=CGWarpMouseCursorPosition(quartzPoint(viewToScreen(r,r->pointerRestore)));
 // Reassociation follows the warp, matching the production Cocoa host path's
 // ordering so the restored cursor resumes immediate ordinary movement.
 CGError associate=CGAssociateMouseAndMouseCursorPosition(true);
 if(warp!=kCGErrorSuccess||associate!=kCGErrorSuccess)r.lastError=@"Native pointer restoration failed";
 NSView* view=r.window.contentView;NSRect bounds=view.bounds;
 r->pointerX=(r->pointerRestore.x-NSMinX(bounds))*r.layer.drawableSize.width/MAX(bounds.size.width,1);
 r->pointerY=(view.flipped?r->pointerRestore.y-NSMinY(bounds):NSMaxY(bounds)-r->pointerRestore.y)*r.layer.drawableSize.height/MAX(bounds.size.height,1);
 pushEvent(r,(NMEvent){.kind=1,.modifiers=r->pointerModifiers,.x=r->pointerX,.y=r->pointerY});
 return warp==kCGErrorSuccess&&associate==kCGErrorSuccess;
}
int nm_pointer_capture(void* handle,int captured) {
 @autoreleasepool {
 NMRenderer* r=(__bridge NMRenderer*)handle;
 if(!captured)return restorePointer(r)?1:0;
 if(r->offscreen||!r->playable||!r.window.keyWindow)return 1;
 if(r->pointerCaptured)return 1;
 NSView* view=r.window.contentView;NSRect bounds=view.bounds;NSPoint screen=NSEvent.mouseLocation;
 r->pointerRestore=[view convertPoint:[r.window convertPointFromScreen:screen] fromView:nil];
 NSPoint center=NSMakePoint(NSMidX(bounds),NSMidY(bounds));NSPoint centerScreen=viewToScreen(r,center);
 CGError warp=CGWarpMouseCursorPosition(quartzPoint(centerScreen));
 if(warp!=kCGErrorSuccess){r.lastError=@"Native pointer centering failed";return 0;}
 CGError associate=CGAssociateMouseAndMouseCursorPosition(false);
 if(associate!=kCGErrorSuccess){CGWarpMouseCursorPosition(quartzPoint(screen));CGAssociateMouseAndMouseCursorPosition(true);r.lastError=@"Native relative pointer capture failed";return 0;}
 r->pointerWarpX=centerScreen.x-screen.x;r->pointerWarpY=screen.y-centerScreen.y;
 r->pointerCaptured=YES;return 1;
 }
}
static void clearFocus(NMRenderer* r) {
 // Clear the native poll state and publish releases for the production held
 // state. Modifier flags are their own event, including a complete chord tap.
 if(r->playable)for(uint32_t key=0;key<128;key++)if(r->held[key])pushEvent(r,(NMEvent){.kind=5,.button=key});
 memset(r->held,0,sizeof(r->held));memset(r->pressed,0,sizeof(r->pressed));r->pauseToggle=NO;r->wheel=0;
 if(r->playable)pushEvent(r,(NMEvent){.kind=6});
 for(uint32_t button=0;button<3;button++)if(r->mouseHeld&(1u<<button))pushEvent(r,(NMEvent){.kind=3,.button=button,.x=r->pointerX,.y=r->pointerY});
 r->mouseHeld=0;r->pointerModifiers=0;restorePointer(r);
}
static void observeFocus(NMRenderer* r) {
 BOOL focused=r.window.keyWindow;
 if(r->wasKey==focused)return;
 if(!focused)clearFocus(r);
 if(r->playable)pushEvent(r,(NMEvent){.kind=8,.key=focused?1:0});
 r->wasKey=focused;
}
static void appendText(NMRenderer* r,NSEvent* event) {
 // The production key translator owns Ctrl/Alt/Command tokens. Ordinary
 // translated characters alone go through the text stream, preserving case.
 uint32_t flags=modifiers(event);if(flags&(2|4|8))return;
 NSString* chars=event.characters;
 for(NSUInteger i=0;i<chars.length;i++){
  uint32_t value=[chars characterAtIndex:i];
  if(value>=0xd800&&value<=0xdbff&&i+1<chars.length){uint32_t low=[chars characterAtIndex:i+1];if(low>=0xdc00&&low<=0xdfff){value=0x10000+((value-0xd800)<<10)+(low-0xdc00);i++;}}
  // Editing controls and Cocoa's function-key characters already have a key
  // token. They must not acquire a companion printable command.
  if(value<32||value==127||(value>=0xf700&&value<=0xf8ff))continue;
  pushEvent(r,(NMEvent){.kind=7,.key=value,.modifiers=flags});
 }
}
static void pump(NMRenderer* r) {
 if(r->offscreen)return;serviceRunLoop(r);observeFocus(r);
 NSEvent* event;while((event=[NSApp nextEventMatchingMask:NSEventMaskAny untilDate:[NSDate distantPast] inMode:NSDefaultRunLoopMode dequeue:YES])){
  BOOL consumed=NO;BOOL ours=event.window==r.window;
  if(ours)r->pointerModifiers=modifiers(event);
  if(ours&&event.type==NSEventTypeFlagsChanged){if(r->playable){pushEvent(r,(NMEvent){.kind=6,.modifiers=modifiers(event)});consumed=YES;}}
  if(ours&&(event.type==NSEventTypeKeyDown||event.type==NSEventTypeKeyUp)){
   unsigned short k=event.keyCode;BOOL down=event.type==NSEventTypeKeyDown;
   if(k<128){r->held[k]=down;if(down)r->pressed[k]=YES;if(k==49&&down&&!event.isARepeat&&!r->playable)r->pauseToggle=YES;}
   uint32_t flags=modifiers(event);
   // Close through the ordinary window lifetime, so Run drains and writes the
   // final records instead of NSApplication terminating the process directly.
   if(k==12&&down&&(flags&8)){[r.window close];consumed=YES;}
   else if(r->playable){NSString* chars=event.charactersIgnoringModifiers;uint32_t key=chars.length?[chars characterAtIndex:0]:0;
    pushEvent(r,(NMEvent){.kind=down?4:5,.key=key,.button=k,.modifiers=flags});if(down)appendText(r,event);consumed=YES;
   }else consumed=k==0||k==1||k==2||k==13||k==49||k>=123;
  }
  BOOL down=event.type==NSEventTypeLeftMouseDown||event.type==NSEventTypeRightMouseDown||event.type==NSEventTypeOtherMouseDown;
  BOOL up=event.type==NSEventTypeLeftMouseUp||event.type==NSEventTypeRightMouseUp||event.type==NSEventTypeOtherMouseUp;
  BOOL move=event.type==NSEventTypeMouseMoved||event.type==NSEventTypeLeftMouseDragged||event.type==NSEventTypeRightMouseDragged||event.type==NSEventTypeOtherMouseDragged;
  BOOL scroll=event.type==NSEventTypeScrollWheel;BOOL pinch=event.type==NSEventTypeMagnify;
  if(ours&&(down||up||move||scroll||pinch)){
   NSView* view=r.window.contentView;NSPoint point=[view convertPoint:event.locationInWindow fromView:nil];NSRect bounds=view.bounds;
   float x=(point.x-NSMinX(bounds))*r.layer.drawableSize.width/MAX(bounds.size.width,1);
   float y=(view.flipped?point.y-NSMinY(bounds):NSMaxY(bounds)-point.y)*r.layer.drawableSize.height/MAX(bounds.size.height,1);
   BOOL inside=NSPointInRect(point,bounds);uint32_t button=event.buttonNumber==1?1:event.buttonNumber==2?2:0;
   if(r->pointerCaptured){
    // Relative deltas remain device-independent Cocoa points. Scale into the
    // same drawable-pixel lane as ordinary absolute positions; the production
    // adapter divides once by its device scale. A center warp is host motion,
    // never camera movement [07 R-CAM-01 §11].
    if(move){r->pointerX+=((double)event.deltaX-r->pointerWarpX)*r.layer.drawableSize.width/MAX(bounds.size.width,1);r->pointerY+=((double)event.deltaY-r->pointerWarpY)*r.layer.drawableSize.height/MAX(bounds.size.height,1);r->pointerWarpX=r->pointerWarpY=0;}
    x=r->pointerX;y=r->pointerY;
   }
   if(inside||r->mouseHeld||r->pointerCaptured){r->pointerX=x;r->pointerY=y;
    if(scroll){r->wheel+=(float)event.scrollingDeltaY;consumed=YES;}
    if(r->playable){if(down){[r.window makeKeyWindow];r->mouseHeld|=1u<<button;}if(up)r->mouseHeld&=~(1u<<button);
     uint32_t key=down?(uint32_t)event.clickCount:0;
     if(scroll){BOOL precise=event.hasPreciseScrollingDeltas,momentum=event.momentumPhase!=NSEventPhaseNone;key=4|(precise?1:0)|(momentum?2:0);int64_t notch=0;
      if(!precise&&!momentum){CGEventRef cg=event.CGEvent;if(cg)notch=CGEventGetIntegerValueField(cg,kCGScrollWheelEventDeltaAxis1);double y=event.scrollingDeltaY;if((y>0&&notch<0)||(y<0&&notch>0))notch=-notch;if(notch==0)notch=y>0?1:y<0?-1:0;}
      button=(uint32_t)(int32_t)MAX(INT32_MIN,MIN(INT32_MAX,notch));
     }
     if(pinch)key=((event.phase&NSEventPhaseBegan)?1:0)|((event.phase&NSEventPhaseEnded)?2:0)|((event.phase&NSEventPhaseCancelled)?4:0);
     pushEvent(r,(NMEvent){.kind=pinch?9:down?2:up?3:1,.key=key,.button=button,.modifiers=modifiers(event),.x=x,.y=y,.wheel_x=scroll?(float)event.scrollingDeltaX:0,.wheel_y=pinch?(float)event.magnification:scroll?(float)event.scrollingDeltaY:0});consumed=YES;}
   }
  }
  if(!consumed)[NSApp sendEvent:event];observeFocus(r);
 }
 serviceRunLoop(r);observeFocus(r);[NSApp updateWindows];
}
void nm_playable(void* handle,int playable) {NMRenderer* r=(__bridge NMRenderer*)handle;r->playable=playable!=0;if(r->playable&&!r->cursorHidden&&!r->offscreen){[NSCursor hide];r->cursorHidden=YES;}else if(!r->playable&&r->cursorHidden){[NSCursor unhide];r->cursorHidden=NO;}}
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
 for(NMMesh* m in r.meshes){NSUInteger index=meshIndex++;NSUInteger at=(visualPass*r.meshes.count+index)*2;NSUInteger count=ring?spans[at+1]:m.instanceCount;NSUInteger start=ring?spans[index*2]:0;BOOL outline=visualPass==2||visualPass>=6;NSUInteger indices=outline?m.edgeCount:m.indexCount;if(count==0||indices==0)continue;
 [enc setVertexBuffer:m.primitiveCenters offset:0 atIndex:15];[enc setVertexBuffer:m.vertices offset:0 atIndex:0];[enc setVertexBuffer:ring?ring.instances:m.instances offset:start*4 atIndex:1];[enc setVertexBuffer:ring?ring.visuals:m.visuals offset:start*64 atIndex:5];[enc setVertexBuffer:ring?ring.rules:r.emptyLights offset:ring?start*112:0 atIndex:10];[enc setVertexBuffer:ring?ring.materialOffsets:r.emptyLights offset:ring?start*4:0 atIndex:12];[enc setVertexBuffer:visualPass?ring.selectors:r.emptyLights offset:visualPass?spans[at]*4:0 atIndex:8];
 nmProjectionBind(ring,enc,start);nmFacesBind(ring,enc,start);if(ring.groups)[enc setVertexBuffer:ring.groups.instanceGroups offset:start*16 atIndex:20];
 [enc drawIndexedPrimitives:outline?MTLPrimitiveTypeLine:MTLPrimitiveTypeTriangle indexCount:indices indexType:MTLIndexTypeUInt32 indexBuffer:outline?m.edges:m.indices indexBufferOffset:0 instanceCount:count];}
}
// Draws the HUD into the pass that already owns the drawable, so the finished
// world is not stored and reloaded for the overlay alone.
static void encodeOverlayInto(NMRenderer* r,id<MTLRenderCommandEncoder> enc,NMUniforms u,NMRing* ring) {
 if(!ring.overlayCount)return;
 [enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:ring.overlayAtlas?:r.emptyFog atIndex:0];[enc setFragmentTexture:r.terrainImage atIndex:1];[enc setFragmentTexture:u.visual.z>0.5?ring.fog:r.emptyFog atIndex:2];
 // Split only consecutive blend runs. Reordering them would change shaded
 // gadgets, text and modal surfaces that overlap earlier HUD commands.
 const float* quads=ring.overlay.contents;NSUInteger start=0;
 while(start<ring.overlayCount){BOOL multiply=quads[start*16+13]>0.5;NSUInteger end=start+1;while(end<ring.overlayCount&&(quads[end*16+13]>0.5)==multiply)end++;
  [enc setRenderPipelineState:multiply?r.overlayMultiply:r.overlay];[enc setVertexBuffer:ring.overlay offset:start*64 atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:end-start];start=end;
 }
 
}
// World passes keep colour/emission; the airborne boundary starts a new depth
// plane so earlier terrain/effects cannot clip pass B [03 R-RAST-01 §7].
static MTLRenderPassDescriptor* worldPass(NMRenderer* r,BOOL clearDepth) {
 MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];
 p.colorAttachments[0].texture=r.color;p.colorAttachments[0].loadAction=MTLLoadActionLoad;p.colorAttachments[0].storeAction=MTLStoreActionStore;
 p.colorAttachments[1].texture=r.emission;p.colorAttachments[1].loadAction=MTLLoadActionLoad;p.colorAttachments[1].storeAction=MTLStoreActionStore;
 p.depthAttachment.texture=r.depthImage;p.depthAttachment.loadAction=clearDepth?MTLLoadActionClear:MTLLoadActionLoad;p.depthAttachment.storeAction=MTLStoreActionStore;p.depthAttachment.clearDepth=1;return p;
}
static void modelBindings(NMRenderer* r,id<MTLRenderCommandEncoder> enc,id<MTLBuffer> poses,NMRing* ring,NMUniforms u) {
 nmProjectionBind(ring,enc,0);nmGroupsBind(ring,enc);nmFacesBind(ring,enc,0);[enc setFragmentBuffer:ring?ring.rules:r.emptyLights offset:0 atIndex:24];
 [enc setVertexBuffer:ring.groups?ring.groups.instanceGroups:r.emptyLights offset:0 atIndex:20];
 if(!ring){[enc setVertexBuffer:r.emptyLights offset:0 atIndex:27];[enc setVertexBuffer:r.emptyLights offset:0 atIndex:28];vector_float4 projectionView={0,0,0,0};[enc setVertexBytes:&projectionView length:16 atIndex:29];[enc setVertexBuffer:r.emptyLights offset:0 atIndex:21];[enc setFragmentBuffer:r.emptyLights offset:0 atIndex:21];[enc setVertexBuffer:r.emptyLights offset:0 atIndex:22];}
 [enc setVertexBuffer:ring?ring.subjectLights:r.emptyLights offset:0 atIndex:17];[enc setVertexBuffer:ring?ring.retainedLights:r.emptyLights offset:0 atIndex:16];[enc setFragmentBuffer:ring?ring.retainedLights:r.emptyLights offset:0 atIndex:16];
 [enc setVertexBuffer:ring?ring.compositionSlots:r.emptyLights offset:0 atIndex:13];[enc setVertexBuffer:ring?ring.compositionSelectors:r.emptyLights offset:0 atIndex:14];
 [enc setCullMode:MTLCullModeNone];[enc setVertexBuffer:r.emptyLights offset:0 atIndex:8];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setVertexBuffer:poses offset:0 atIndex:2];[enc setFragmentTexture:r.atlas atIndex:0];[enc setFragmentBuffer:ring?ring.lights:r.emptyLights offset:0 atIndex:4];[enc setVertexBuffer:r.materials offset:0 atIndex:6];[enc setVertexBuffer:r.textureFrames offset:0 atIndex:7];[enc setVertexBuffer:ring?ring.materialOverrides:r.emptyLights offset:0 atIndex:11];[enc setFragmentTexture:r.modelPalette?:r.emptyFog atIndex:4];
}
static id<MTLBuffer> grow(NMRenderer* r,id<MTLBuffer> old,NSUInteger bytes);
static void drawSubjectMember(NMRenderer* r,id<MTLRenderCommandEncoder> enc,NMRing* ring,NMCloakDraw d,BOOL outline);
#include "groups.inc"
#include "projection.inc"
#include "faces.inc"
#include "face_reflections.inc"
#include "outline_rows.inc"
static BOOL nmSlabCommitActive; // shadow commits test the slab depth while set
#include "shadows.inc"
#include "composition.inc"
#include "effects.inc"
#include "ground_effects.inc"
#include "water_objects.inc"
static void drawPhase(NMRenderer* r,id<MTLRenderCommandEncoder> enc,id<MTLBuffer> poses,NMRing* ring,NMUniforms u,NSUInteger phase) {
 u.mode.z=1;modelBindings(r,enc,poses,ring,u);[enc setRenderPipelineState:r.model];[enc setDepthStencilState:r.depth];drawMeshes(r,enc,ring,3+(int)phase);
 if(u.visual.x>0.5){[enc setRenderPipelineState:r.outline];[enc setDepthStencilState:r.spriteDepth];drawMeshes(r,enc,ring,6+(int)phase);}
}
static void drawSubjectMember(NMRenderer* r,id<MTLRenderCommandEncoder> enc,NMRing* ring,NMCloakDraw d,BOOL outline) {
 nmProjectionBind(ring,enc,d.instance);nmFacesBind(ring,enc,d.instance);if(ring.groups)[enc setVertexBuffer:ring.groups.instanceGroups offset:d.instance*16 atIndex:20];
 [enc setVertexBuffer:ring.compositionSelectors offset:d.instance*4 atIndex:14];
 NMMesh* mesh=r.meshes[d.mesh];NSUInteger count=outline?mesh.edgeCount:mesh.indexCount;if(!count)return;
 [enc setVertexBuffer:mesh.primitiveCenters offset:0 atIndex:15];[enc setVertexBuffer:mesh.vertices offset:0 atIndex:0];[enc setVertexBuffer:ring.instances offset:d.instance*4 atIndex:1];[enc setVertexBuffer:ring.visuals offset:d.instance*64 atIndex:5];[enc setVertexBuffer:ring.rules offset:d.instance*112 atIndex:10];[enc setVertexBuffer:ring.materialOffsets offset:d.instance*4 atIndex:12];
 [enc drawIndexedPrimitives:outline?MTLPrimitiveTypeLine:MTLPrimitiveTypeTriangle indexCount:count indexType:MTLIndexTypeUInt32 indexBuffer:outline?mesh.edges:mesh.indices indexBufferOffset:0 instanceCount:1];
}
static void encodeCloaks(NMRenderer* r,id<MTLCommandBuffer> cb,id<MTLBuffer> poses,NMRing* ring,NMUniforms u,NSUInteger phase) {
 const NMCloakDraw* draws=ring.cloaks.bytes;const float* visuals=ring.visuals.contents;
 for(NSUInteger begin=0;begin<ring.cloakCount;){
  NSUInteger end=begin+1;while(end<ring.cloakCount&&draws[end].group==draws[begin].group)end++;
  if(draws[begin].phase!=phase){begin=end;continue;}
  MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];
  p.colorAttachments[0].texture=r.subjectColor;p.colorAttachments[1].texture=r.subjectEmission;
  for(int i=0;i<2;i++){p.colorAttachments[i].loadAction=MTLLoadActionClear;p.colorAttachments[i].storeAction=MTLStoreActionStore;p.colorAttachments[i].clearColor=MTLClearColorMake(0,0,0,0);}
  p.depthAttachment.texture=r.subjectDepth;p.depthAttachment.loadAction=MTLLoadActionClear;p.depthAttachment.storeAction=MTLStoreActionStore;p.depthAttachment.clearDepth=1;
  id<MTLRenderCommandEncoder> enc=nmRenderEncoder(cb,p,"cloak.raster");NMUniforms subject=u;subject.mode.z=0;subject.mode.w=1;
  modelBindings(r,enc,poses,ring,subject);[enc setRenderPipelineState:r.subjectModel];[enc setDepthStencilState:r.depth];
  for(NSUInteger i=begin;i<end;i++)drawSubjectMember(r,enc,ring,draws[i],NO);
  if(u.visual.x>0.5){[enc setRenderPipelineState:r.outline];[enc setDepthStencilState:r.depth];for(NSUInteger i=begin;i<end;i++)if(visuals[draws[i].instance*16+2]>0&&visuals[draws[i].instance*16+11]>0)drawSubjectMember(r,enc,ring,draws[i],YES);}
  [enc endEncoding];
  // Resolve each visible subject pixel once. Native scene depth remains an
  // approximation; opacity is applied after self-occlusion [GPU design §22.1].
  enc=nmRenderEncoder(cb,worldPass(r,NO),"cloak.resolve");[enc setRenderPipelineState:r.subjectResolve];[enc setDepthStencilState:r.depth];
  [enc setFragmentTexture:r.subjectColor atIndex:0];[enc setFragmentTexture:r.subjectEmission atIndex:1];[enc setFragmentTexture:r.subjectDepth atIndex:2];float opacity=visuals[draws[begin].instance*16+1];[enc setFragmentBytes:&opacity length:sizeof(opacity) atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];begin=end;
 }
}
static BOOL nmRetainedProjectile(NMRenderer* r,NMRing* ring,id<MTLCommandBuffer> cb,const void* uniforms,uint32_t slot,uint32_t flags) {
 if(flags&1)return NO;const uint32_t* paint=ring.compositionPaint.contents;NSUInteger at=0,body=0;
 for(;at<ring.compositionPaintCount;at++)if(paint[at*4+3]==2){if(body++==slot)break;}
 if(at==ring.compositionPaintCount||!paint[at*4])return NO;if(flags&2)return YES;NMUniforms u=*(const NMUniforms*)uniforms;
 id<MTLRenderCommandEncoder> enc=nmFxShared?:nmRenderEncoder(cb,worldPass(r,NO),"effects.projectileCommit");[enc setDepthStencilState:nil];[enc setRenderPipelineState:r.atlasCommit];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setVertexBuffer:ring.compositionSlots offset:0 atIndex:0];[enc setVertexBuffer:ring.compositionPaint offset:at*16 atIndex:1];[enc setFragmentTexture:r.modelAtlasColor atIndex:0];[enc setFragmentTexture:r.modelAtlasEmission atIndex:1];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];if(enc!=nmFxShared)[enc endEncoding];return YES;
}
static BOOL encode(NMRenderer* r,id<MTLCommandBuffer> cb,id<MTLBuffer> endpoints,id<MTLBuffer> poses,NMUniforms u,id<MTLTexture> destination,NMRing* ring) {
 // Compute once per submitted piece in this acquired slot. Body, outline and
 // shadow passes share the result; capture reruns the same path after drain.
 NSUInteger count=(NSUInteger)u.timing.z;
 if(count){id<MTLComputeCommandEncoder> compute=nmComputeEncoder(cb,"pose.interpolate");[compute setComputePipelineState:r.interpolatePoses];[compute setBuffer:endpoints offset:0 atIndex:0];[compute setBuffer:poses offset:0 atIndex:1];[compute setBytes:&u length:sizeof(u) atIndex:3];NSUInteger group=MIN((NSUInteger)256,r.interpolatePoses.maxTotalThreadsPerThreadgroup);[compute dispatchThreads:MTLSizeMake(count,1,1) threadsPerThreadgroup:MTLSizeMake(group,1,1)];[compute endEncoding];}
 if(u.lightControl.y>0.5&&!nmAblate("groundlight")){MTLRenderPassDescriptor* p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=r.groundField;p.colorAttachments[0].loadAction=MTLLoadActionClear;p.colorAttachments[0].storeAction=MTLStoreActionStore;p.colorAttachments[0].clearColor=MTLClearColorMake(0,0,0,0);id<MTLRenderCommandEncoder> e=nmRenderEncoder(cb,p,"light.groundField");[e setRenderPipelineState:r.groundLight];[e setVertexBytes:&u length:sizeof(u) atIndex:3];[e setVertexBuffer:ring.retainedLights offset:0 atIndex:16];[e setFragmentBuffer:ring.retainedLights offset:0 atIndex:16];if(u.lightControl.x>0)[e drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:(NSUInteger)u.lightControl.x];[e endEncoding];}
 if(ring.composed&&ring.compositionSlotCount&&u.lightControl.y>0.5){id<MTLComputeCommandEncoder> e=nmComputeEncoder(cb,"light.selectSubjects");[e setComputePipelineState:r.selectLights];[e setBytes:&u length:sizeof(u) atIndex:3];[e setBuffer:ring.compositionSlots offset:0 atIndex:13];[e setBuffer:ring.retainedLights offset:0 atIndex:16];[e setBuffer:ring.subjectLights offset:0 atIndex:17];[e dispatchThreads:MTLSizeMake(ring.compositionSlotCount,1,1) threadsPerThreadgroup:MTLSizeMake(MIN((NSUInteger)64,r.selectLights.maxTotalThreadsPerThreadgroup),1,1)];[e endEncoding];}
 if(ring.composed){if(!nmFacesEncode(r,ring,cb,poses,ring.waterObjectsRing.objects,u)||!nmFaceReflectionsEncode(r,ring,cb,ring.waterObjectsRing.objects,(uint32_t)ring.waterObjectsRing.objectCount))return NO;nmCompositionRaster(r,ring,cb,poses,u);if(!nmOutlineRowsEncode(r,ring,cb,poses,u)||!nmGroupsEncode(r,ring,cb,u))return NO;if(!nmAblate("shadows")&&!nmAblate("shadowraster"))nmShadowsRaster(r,r.shadows,ring.shadows,ring,cb,poses,u);}
 MTLRenderPassDescriptor* pass;
 if(u.visual.x>0.5){
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.shadowMask;pass.colorAttachments[0].loadAction=MTLLoadActionClear;pass.colorAttachments[0].storeAction=MTLStoreActionStore;pass.colorAttachments[0].clearColor=MTLClearColorMake(0,0,0,0);
 id<MTLRenderCommandEncoder> shadow=nmRenderEncoder(cb,pass,"shadow.mask");[shadow setRenderPipelineState:r.shadow];[shadow setVertexBytes:&u length:sizeof(u) atIndex:3];[shadow setVertexBuffer:poses offset:0 atIndex:2];[shadow setVertexTexture:r.heights atIndex:1];[shadow setVertexBuffer:r.materials offset:0 atIndex:6];[shadow setVertexBuffer:r.textureFrames offset:0 atIndex:7];[shadow setVertexBuffer:ring?ring.materialOverrides:r.emptyLights offset:0 atIndex:11];[shadow setFragmentTexture:r.atlas atIndex:0];[shadow setFragmentTexture:r.modelPalette?:r.emptyFog atIndex:4];[shadow setFragmentBytes:&u length:sizeof(u) atIndex:3];if(!ring.composed)drawMeshes(r,shadow,ring,1);[shadow endEncoding];}
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.color;pass.colorAttachments[0].loadAction=MTLLoadActionClear;pass.colorAttachments[0].storeAction=MTLStoreActionStore;pass.colorAttachments[0].clearColor=MTLClearColorMake(0.08,0.09,0.1,1);
 pass.colorAttachments[1].texture=r.emission;pass.colorAttachments[1].loadAction=MTLLoadActionClear;pass.colorAttachments[1].storeAction=MTLStoreActionStore;
 pass.depthAttachment.texture=r.depthImage;pass.depthAttachment.loadAction=MTLLoadActionClear;pass.depthAttachment.storeAction=MTLStoreActionStore;pass.depthAttachment.clearDepth=1;
 id<MTLRenderCommandEncoder> enc=nmRenderEncoder(cb,pass,"world.terrain");[enc setDepthStencilState:r.depth];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setCullMode:MTLCullModeNone];[enc setFragmentBuffer:ring?ring.lights:r.emptyLights offset:0 atIndex:4];
 BOOL deferredWater=ring.composed&&ring.waterObjectsRing&&r->water[7]>.5;
 if(deferredWater){NMUniforms base=u;base.lightControl.y=0;base.visual.x=0;float water[20];memcpy(water,r->water,sizeof(water));water[7]=0;[enc setFragmentBytes:&base length:sizeof(base) atIndex:3];[enc setFragmentBytes:water length:sizeof(water) atIndex:9];}
 if(!nmTerrainTilesBind(r.terrainTiles,enc,(ring.terrainFlags&1)!=0,(ring.terrainFlags&2)!=0)){[enc endEncoding];r.lastError=@"Terrain tile bind failed";return NO;}[enc setRenderPipelineState:r.terrain];[enc setDepthStencilState:r.spriteDepth];[enc setFragmentTexture:r.groundField atIndex:5];[enc setFragmentTexture:r.terrainImage atIndex:0];[enc setFragmentTexture:r.heights atIndex:1];[enc setFragmentTexture:r.shadowMask atIndex:2];[enc setFragmentTexture:r.waterMask?:r.emptyFog atIndex:3];if(!deferredWater)[enc setFragmentBytes:r->water length:sizeof(r->water) atIndex:9];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];
 [enc setFragmentBytes:&u length:sizeof(u) atIndex:3];
 // Short features are painted after TNT and before any model [03 R-RAST-01 §6].
 if(ring.groundSpriteCount&&!ring.composed){[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:r.spriteDepth];[enc setVertexBuffer:ring.sprites offset:0 atIndex:0];[enc setFragmentTexture:r.spriteAtlas atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.groundSpriteCount];}
 if(!ring){
  modelBindings(r,enc,poses,nil,u);[enc setRenderPipelineState:r.model];[enc setDepthStencilState:r.depth];drawMeshes(r,enc,nil,0);[enc endEncoding];
 }else if(ring.composed&&!getenv("NANOLATHE_METAL_SPLIT_WORLD")&&!ring.effectRing.lenses.length){
  // One world encoder from the first effect layer to the last: effect layers
  // and commit phases alternate in painter order without storing and
  // reloading the world between them. Lens snapshots need the split path.
  nmWaterObjectsSeabed(r,ring,enc,u);[enc endEncoding];if(!nmAblate("water")){if(deferredWater&&!nmWaterObjectsSurface(r,ring,cb,u))return NO;if(!nmWaterObjectsReflections(r,ring,cb,poses,u))return NO;}
  BOOL fx=!nmAblate("effects");
  enc=nmRenderEncoder(cb,worldPass(r,NO),"world.merged");nmFxExternal=enc;
  if(fx)nmEffectsDraw(r,ring,cb,0,u);
  nmDirectPhase(r,ring,enc,poses,u,0);nmCompositionCommit(r,ring,enc,u,0);
  if(fx)nmEffectsDraw(r,ring,cb,1,u);
  nmDirectPhase(r,ring,enc,poses,u,1);nmCompositionCommit(r,ring,enc,u,1);
  if(ring.spriteCount>ring.groundSpriteCount){[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:nil];[enc setVertexBuffer:ring.sprites offset:ring.groundSpriteCount*96 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:r.spriteAtlas atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.spriteCount-ring.groundSpriteCount];}
  if(!ring.effectRing.layerCount)nmCompositionCommit(r,ring,enc,u,2);
  if(fx)nmEffectsDraw(r,ring,cb,2,u);
  nmDirectPhase(r,ring,enc,poses,u,3);nmCompositionCommit(r,ring,enc,u,3);
  if(fx){nmEffectsDraw(r,ring,cb,3,u);nmEffectsDraw(r,ring,cb,4,u);}
  nmFxExternal=nil;[enc endEncoding];
 }else if(ring.composed){
  nmWaterObjectsSeabed(r,ring,enc,u);[enc endEncoding];if(!nmAblate("water")){if(deferredWater&&!nmWaterObjectsSurface(r,ring,cb,u))return NO;if(!nmWaterObjectsReflections(r,ring,cb,poses,u))return NO;}if(!nmAblate("effects"))nmEffectsDraw(r,ring,cb,0,u);
  enc=nmRenderEncoder(cb,worldPass(r,NO),"world.commitShortFeatures");nmDirectPhase(r,ring,enc,poses,u,0);nmCompositionCommit(r,ring,enc,u,0);[enc endEncoding];if(!nmAblate("effects"))nmEffectsDraw(r,ring,cb,1,u);
  enc=nmRenderEncoder(cb,worldPass(r,NO),"world.commitGround");nmDirectPhase(r,ring,enc,poses,u,1);nmCompositionCommit(r,ring,enc,u,1);
  if(ring.spriteCount>ring.groundSpriteCount){[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:nil];[enc setVertexBuffer:ring.sprites offset:ring.groundSpriteCount*96 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:r.spriteAtlas atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.spriteCount-ring.groundSpriteCount];}
  if(!ring.effectRing.layerCount)nmCompositionCommit(r,ring,enc,u,2);[enc endEncoding];if(!nmAblate("effects"))nmEffectsDraw(r,ring,cb,2,u);
  enc=nmRenderEncoder(cb,worldPass(r,NO),"world.commitAir");nmDirectPhase(r,ring,enc,poses,u,3);nmCompositionCommit(r,ring,enc,u,3);[enc endEncoding];if(!nmAblate("effects"))nmEffectsDraw(r,ring,cb,3,u);if(!nmAblate("effects"))nmEffectsDraw(r,ring,cb,4,u);
 }else if(!ring.cloakCount){
  // Keep opaque phases in one tile render pass. A depth-only fullscreen draw
  // resets pass B's ownership without storing/reloading both HDR attachments.
  drawPhase(r,enc,poses,ring,u,0);drawPhase(r,enc,poses,ring,u,1);
  if(ring.spriteCount>ring.groundSpriteCount){[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:r.spriteDepth];[enc setVertexBuffer:ring.sprites offset:ring.groundSpriteCount*96 atIndex:0];[enc setFragmentTexture:r.spriteAtlas atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.spriteCount-ring.groundSpriteCount];}
  if(ring.airCount){[enc setRenderPipelineState:r.clearDepth];[enc setDepthStencilState:r.alwaysDepth];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];drawPhase(r,enc,poses,ring,u,2);}
  [enc endEncoding];
 }else{
  drawPhase(r,enc,poses,ring,u,0);[enc endEncoding];encodeCloaks(r,cb,poses,ring,u,0);
  enc=nmRenderEncoder(cb,worldPass(r,NO),"world.depthPhaseGround");drawPhase(r,enc,poses,ring,u,1);
  if(ring.spriteCount>ring.groundSpriteCount){[enc setRenderPipelineState:r.sprite];[enc setDepthStencilState:r.spriteDepth];[enc setVertexBuffer:ring.sprites offset:ring.groundSpriteCount*96 atIndex:0];[enc setFragmentTexture:r.spriteAtlas atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.spriteCount-ring.groundSpriteCount];}
  [enc endEncoding];encodeCloaks(r,cb,poses,ring,u,1);
  if(ring.airCount){enc=nmRenderEncoder(cb,worldPass(r,YES),"world.depthPhaseAir");drawPhase(r,enc,poses,ring,u,2);[enc endEncoding];encodeCloaks(r,cb,poses,ring,u,2);}
 }
 nmGlowDeferResolve=u.visual.x>0.5&&!getenv("NANOLATHE_METAL_LEGACY_POST");if(ring.glowEnabled&&!nmAblate("glow")){if(!nmGlowEncode(r.productionGlow,cb,r.emission,r.color,ring.effectRing.atlas,r.color,ring.glowVertices,ring.glowCount,ring.glowParams))r.lastError=@"Production glow submission failed";}
 if(r->effects&&!ring.glowEnabled){
 id<MTLComputeCommandEncoder> compute=nmComputeEncoder(cb,"bloom.blurX");[compute setComputePipelineState:r.blur];vector_float2 d={1,0};[compute setBytes:&d length:sizeof(d) atIndex:0];[compute setTexture:r.emission atIndex:0];[compute setTexture:r.blurA atIndex:1];[compute dispatchThreads:MTLSizeMake(r.blurA.width,r.blurA.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[compute endEncoding];
 compute=nmComputeEncoder(cb,"bloom.blurY");[compute setComputePipelineState:r.blur];d=(vector_float2){0,1};[compute setBytes:&d length:sizeof(d) atIndex:0];[compute setTexture:r.blurA atIndex:0];[compute setTexture:r.blurB atIndex:1];[compute dispatchThreads:MTLSizeMake(r.blurB.width,r.blurB.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[compute endEncoding];
 }
 // Resolve, slab silhouettes, distortion and fog in the one pass that owns the
// drawable; the HUD follows in the same encoder (NANOLATHE_METAL_LEGACY_POST=1
// restores the stored resolve, snapshot copy and separate fog pass).
 if(u.visual.x>0.5&&!getenv("NANOLATHE_METAL_LEGACY_POST")){
  struct {float bands,bloom,pad0,pad1;} f={ring.directSlotCount?(float)ring.compositionPaintCount+1:0,(u.timing.w>0.5&&!ring.glowEnabled)?1.f:0.f,0,0};
  MTLRenderPassDescriptor* last=[MTLRenderPassDescriptor renderPassDescriptor];last.colorAttachments[0].texture=destination;last.colorAttachments[0].loadAction=MTLLoadActionDontCare;last.colorAttachments[0].storeAction=MTLStoreActionStore;
  enc=nmRenderEncoder(cb,last,"post.final");
  [enc setRenderPipelineState:r.finalPass];
  [enc setFragmentTexture:r.color atIndex:0];[enc setFragmentTexture:r.blurB atIndex:1];[enc setFragmentTexture:ring.fog?:r.emptyFog atIndex:2];[enc setFragmentTexture:r.fogAtlas?:r.emptyFog atIndex:3];[enc setFragmentTexture:r.depthImage atIndex:4];
  [enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&f length:sizeof(f) atIndex:4];
  {NMGlow* glow=r.productionGlow;BOOL on=ring.glowEnabled&&ring.glowCount>0&&glow.octaves&&!nmAblate("glow");
   struct {vector_float4 blur;vector_uint4 sizes;vector_float4 flags;} gf={ring.glowParams.blur,{(uint32_t)(glow.width+3)/4,(uint32_t)(glow.height+3)/4,(uint32_t)(glow.width+7)/8,(uint32_t)(glow.height+7)/8},{on?1.f:0.f,0,0,0}};
   [enc setFragmentTexture:on?glow.octaves:r.emptyFog atIndex:5];[enc setFragmentBytes:&gf length:sizeof(gf) atIndex:5];}
  [enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];
  if(r->effects&&ring.distortionCount>0){[enc setRenderPipelineState:r.distortionFinal];[enc setVertexBuffer:ring.distortions offset:0 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.distortionCount];}
  encodeOverlayInto(r,enc,u,ring);[enc endEncoding];
  return r.lastError==nil;
 }
 if(u.visual.x>0.5){
 if(ring.glowEnabled)u.timing.w=0;
 BOOL distort=r->effects&&ring.distortionCount>0;id<MTLTexture> world=distort?r.worldSnapshot:r.postColor;
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=world;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 enc=nmRenderEncoder(cb,pass,"post.worldResolve");[enc setRenderPipelineState:r.world];[enc setFragmentTexture:r.color atIndex:0];[enc setFragmentTexture:r.depthImage atIndex:2];{float bands=ring.directSlotCount?(float)ring.compositionPaintCount+1:0;[enc setFragmentBytes:&bands length:sizeof(bands) atIndex:4];}[enc setFragmentTexture:r.blurB atIndex:1];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[enc endEncoding];
 if(distort){u.timing.w=r->effects?1:0;id<MTLBlitCommandEncoder> copy=nmBlitEncoder(cb,"post.distortionCopy");[copy copyFromTexture:r.worldSnapshot sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(r.postColor.width,r.postColor.height,1) toTexture:r.postColor destinationSlice:0 destinationLevel:0 destinationOrigin:MTLOriginMake(0,0,0)];[copy endEncoding];
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=r.postColor;pass.colorAttachments[0].loadAction=MTLLoadActionLoad;pass.colorAttachments[0].storeAction=MTLStoreActionStore;enc=nmRenderEncoder(cb,pass,"post.distortion");[enc setRenderPipelineState:r.distortion];[enc setVertexBuffer:ring.distortions offset:0 atIndex:0];[enc setVertexBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc setFragmentTexture:r.worldSnapshot atIndex:0];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6 instanceCount:ring.distortionCount];[enc endEncoding];}
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=destination;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;enc=nmRenderEncoder(cb,pass,"post.fog");[enc setRenderPipelineState:r.fog];[enc setFragmentTexture:r.postColor atIndex:0];[enc setFragmentTexture:ring.fog?:r.emptyFog atIndex:1];[enc setFragmentTexture:r.fogAtlas?:r.emptyFog atIndex:2];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];encodeOverlayInto(r,enc,u,ring);[enc endEncoding];return r.lastError==nil;
 }
 pass=[MTLRenderPassDescriptor renderPassDescriptor];pass.colorAttachments[0].texture=destination;pass.colorAttachments[0].loadAction=MTLLoadActionDontCare;pass.colorAttachments[0].storeAction=MTLStoreActionStore;
 enc=nmRenderEncoder(cb,pass,"post.composite");[enc setRenderPipelineState:r.composite];[enc setFragmentTexture:r.color atIndex:0];[enc setFragmentTexture:r.blurB atIndex:1];[enc setFragmentBytes:&u length:sizeof(u) atIndex:3];[enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];encodeOverlayInto(r,enc,u,ring);[enc endEncoding];return r.lastError==nil;
}
int nm_frame(void* handle,const void* previous,const void* next,float alpha,double seconds) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;if(!r->interactive&&r->rowCount>=r->rowCapacity)return 0;pump(r);if(!r->offscreen&&!r.window.visible)return -1;
 double start=CACurrentMediaTime();dispatch_semaphore_wait(r->available,DISPATCH_TIME_FOREVER);double acquired=CACurrentMediaTime();
 id<CAMetalDrawable> drawable=nil;if(!r->offscreen){drawable=[r.layer nextDrawable];if(!drawable){dispatch_semaphore_signal(r->available);return 0;}}
 double ready=CACurrentMediaTime();NSUInteger generation=++r->rowCount;NSUInteger rowIndex=(generation-1)%r->rowCapacity;NMRow* row=&r->rows[rowIndex];@synchronized(r){memset(row,0,sizeof(*row));r->rowGenerations[rowIndex]=generation;}row->wait=(acquired-start)*1000;row->drawable_wait=(ready-acquired)*1000;
 id<MTLBuffer> poses=r.poses[r->frameNumber%3];memcpy(poses.contents,previous,r->poseCount*64);memcpy((char*)poses.contents+r->poseCount*64,next,r->poseCount*64);
 NMUniforms u=r->uniforms;u.timing=(vector_float4){alpha,(float)seconds,(float)r->poseCount,r->effects?1:0};r->lastUniforms=u;id<MTLCommandBuffer> cb=[r.queue commandBuffer];if(!encode(r,cb,poses,r.interpolatedPoses[r->frameNumber%3],u,drawable?drawable.texture:r.output,nil)){dispatch_semaphore_signal(r->available);return 0;}
 if(drawable){dispatch_group_enter(r->presentations);[drawable addPresentedHandler:^(id<MTLDrawable> d){@synchronized(r){if(r->rowGenerations[rowIndex]==generation)r->rows[rowIndex].presented=d.presentedTime;}dispatch_group_leave(r->presentations);}];[cb presentDrawable:drawable];}
 row->encode=(CACurrentMediaTime()-ready)*1000;row->submit=CACurrentMediaTime();row->memory=r.device.currentAllocatedSize;dispatch_group_enter(r->pending);
 [cb addCompletedHandler:^(id<MTLCommandBuffer> b){@synchronized(r){if(r->rowGenerations[rowIndex]==generation){NMRow* result=&r->rows[rowIndex];result->gpu_start=b.GPUStartTime;result->gpu_end=b.GPUEndTime;result->completed=CACurrentMediaTime();result->status=(int)b.status;}}if(b.error)r.lastError=b.error.localizedDescription;dispatch_semaphore_signal(r->available);dispatch_group_leave(r->pending);}];[cb commit];r->frameNumber++;return 1;}
}
static id<MTLBuffer> grow(NMRenderer* r,id<MTLBuffer> old,NSUInteger bytes) {
 if(old.length>=MAX(bytes,32))return old;NSUInteger capacity=MAX(old.length,4096);while(capacity<bytes)capacity*=2;return [r.device newBufferWithLength:capacity options:MTLResourceStorageModeShared];
}
static id<MTLBuffer> growPoseOutput(NMRenderer* r,id<MTLBuffer> old,NSUInteger bytes) {
 if(old.length>=MAX(bytes,64))return old;NSUInteger capacity=MAX(old.length,4096);while(capacity<bytes)capacity*=2;return [r.device newBufferWithLength:capacity options:MTLResourceStorageModePrivate];
}
int nm_live_frame(void* handle,const NMLiveUpload* upload) {
 @autoreleasepool {
 NMRenderer* r=(__bridge NMRenderer*)handle;if((!r->interactive&&r->rowCount>=r->rowCapacity)||upload->light_count>32||upload->overlay_count>INT32_MAX||upload->ground_sprite_count>upload->sprite_count||upload->cloak_count>upload->instance_count||upload->air_count>upload->instance_count)return 0;pump(r);if(!r->offscreen&&!r.window.visible)return -1;
 double start=CACurrentMediaTime();dispatch_semaphore_wait(r->available,DISPATCH_TIME_FOREVER);double acquired=CACurrentMediaTime();id<CAMetalDrawable> drawable=nil;
 if(!r->offscreen){drawable=[r.layer nextDrawable];if(!drawable){dispatch_semaphore_signal(r->available);return 0;}}
 double ready=CACurrentMediaTime();NMRing* ring=r.rings[r->frameNumber%3];
 if(!nmEffectsPrepare(r,ring,upload->effects)||!nmGroundPrepare(r,ring,upload->ground_effects)){dispatch_semaphore_signal(r->available);return 0;}
 if(!ring.shadows)ring.shadows=[NMShadowFrame new];if(!nmShadowsPrepare(r,r.shadows,ring.shadows,&upload->shadows)){r.lastError=@"Shadow preparation failed";dispatch_semaphore_signal(r->available);return 0;}
 ring.glowVertices=grow(r,ring.glowVertices,(NSUInteger)upload->glow_count*48);if(!ring.glowVertices){dispatch_semaphore_signal(r->available);return 0;}if(upload->glow_count)memcpy(ring.glowVertices.contents,upload->glow_vertices,upload->glow_count*48);ring.glowCount=upload->glow_count;ring.glowEnabled=upload->glow_enabled;NMGlowParameters glow;memcpy(&glow,upload->glow_parameters,sizeof(glow));ring.glowParams=glow;
 if(!nmCompositionPrepare(r,ring,upload)){dispatch_semaphore_signal(r->available);return 0;}
 BOOL uploadOverlay=upload->overlay_version&&upload->overlay_version!=r->overlayVersion;
 NSUInteger overlayX=0,overlayY=0,overlayW=0,overlayH=0,overlayRowBytes=0;
 if(uploadOverlay){
  if(!upload->overlay_texture||!upload->overlay_width||!upload->overlay_height||upload->overlay_width>INT32_MAX||upload->overlay_height>INT32_MAX){dispatch_semaphore_signal(r->available);return 0;}
  BOOL full=r->overlayVersion==0||r.overlayAtlas.width!=upload->overlay_width||r.overlayAtlas.height!=upload->overlay_height;
  if(r.overlayAtlas.width!=upload->overlay_width||r.overlayAtlas.height!=upload->overlay_height){MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:upload->overlay_width height:upload->overlay_height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;r.overlayAtlas=[r.device newTextureWithDescriptor:d];if(!r.overlayAtlas){dispatch_semaphore_signal(r->available);return 0;}}
  overlayX=upload->overlay_dirty[0];overlayY=upload->overlay_dirty[1];overlayW=upload->overlay_dirty[2];overlayH=upload->overlay_dirty[3];
  if(full||!overlayW||!overlayH){overlayX=overlayY=0;overlayW=upload->overlay_width;overlayH=upload->overlay_height;}
  if(overlayX+overlayW>upload->overlay_width||overlayY+overlayH>upload->overlay_height){dispatch_semaphore_signal(r->available);return 0;}
  overlayRowBytes=(overlayW*4+255)&~(NSUInteger)255;ring.overlayUpload=grow(r,ring.overlayUpload,overlayRowBytes*overlayH);if(!ring.overlayUpload){dispatch_semaphore_signal(r->available);return 0;}
  const uint8_t* source=upload->overlay_texture;uint8_t* staging=ring.overlayUpload.contents;
  for(NSUInteger y=0;y<overlayH;y++)memcpy(staging+y*overlayRowBytes,source+((overlayY+y)*upload->overlay_width+overlayX)*4,overlayW*4);
  r->overlayVersion=upload->overlay_version;r->overlayTextureUploads++;r->overlayTextureBytes+=(uint64_t)overlayW*overlayH*4;
 }
 if(upload->cloak_count&&!upload->cloaks){dispatch_semaphore_signal(r->available);return 0;}
 for(NSUInteger i=0;i<upload->cloak_count;i++){NMCloakDraw d=upload->cloaks[i];if(d.mesh>=r.meshes.count||d.instance>=upload->instance_count||d.phase>2){dispatch_semaphore_signal(r->available);return 0;}}
 if(!ring.cloaks)ring.cloaks=[NSMutableData new];ring.cloaks.length=upload->cloak_count*sizeof(NMCloakDraw);if(upload->cloak_count)memcpy(ring.cloaks.mutableBytes,upload->cloaks,ring.cloaks.length);ring.cloakCount=upload->cloak_count;ring.airCount=upload->air_count;
 if(upload->cloak_count&&!r.subjectColor){NSUInteger w=r.color.width,h=r.color.height;r.subjectColor=target(r,w,h,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);r.subjectEmission=target(r,w,h,MTLPixelFormatRGBA16Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);r.subjectDepth=target(r,w,h,MTLPixelFormatDepth32Float,MTLTextureUsageRenderTarget|MTLTextureUsageShaderRead);if(!r.subjectColor||!r.subjectEmission||!r.subjectDepth){dispatch_semaphore_signal(r->available);return 0;}}
 ring.overlayAtlas=r.overlayAtlas;
 // Reuse/grow only the now-free slot. Existing command buffers retain their own
 // resources; no other in-flight slot is modified or resized here.
 memcpy(r->water,upload->water,sizeof(r->water));
 ring.poses=grow(r,ring.poses,(NSUInteger)upload->pose_count*128);ring.instances=grow(r,ring.instances,(NSUInteger)upload->instance_count*4);ring.sprites=grow(r,ring.sprites,(NSUInteger)upload->sprite_count*96);ring.lights=grow(r,ring.lights,(NSUInteger)upload->light_count*32);ring.visuals=grow(r,ring.visuals,(NSUInteger)upload->instance_count*64);ring.distortions=grow(r,ring.distortions,(NSUInteger)upload->distortion_count*64);ring.selectors=grow(r,ring.selectors,(NSUInteger)upload->instance_count*12);ring.overlay=grow(r,ring.overlay,(NSUInteger)upload->overlay_count*64);
 ring.retainedLights=grow(r,ring.retainedLights,MAX((NSUInteger)upload->retained_light_count*64,64));if(!ring.retainedLights){dispatch_semaphore_signal(r->available);return 0;}if(upload->retained_light_count)memcpy(ring.retainedLights.contents,upload->retained_lights,upload->retained_light_count*64);
 ring.rules=grow(r,ring.rules,MAX((NSUInteger)upload->instance_count*112,112));ring.materialOffsets=grow(r,ring.materialOffsets,MAX((NSUInteger)upload->instance_count*4,4));ring.materialOverrides=grow(r,ring.materialOverrides,MAX((NSUInteger)upload->material_count*48,48));
 ring.interpolatedPoses=growPoseOutput(r,ring.interpolatedPoses,(NSUInteger)upload->pose_count*64);
 if(!ring.rules||!ring.materialOffsets||!ring.materialOverrides||!ring.poses||!ring.interpolatedPoses||!ring.instances||!ring.sprites||!ring.lights||!ring.visuals||!ring.distortions||!ring.selectors||!ring.overlay){dispatch_semaphore_signal(r->available);return 0;}
 if(upload->pose_count){memcpy(ring.poses.contents,upload->previous,upload->pose_count*64);memcpy((char*)ring.poses.contents+upload->pose_count*64,upload->current,upload->pose_count*64);}
 if(upload->instance_count){memcpy(ring.rules.contents,upload->rules,upload->instance_count*112);memcpy(ring.materialOffsets.contents,upload->material_offsets,upload->instance_count*4);memcpy(ring.instances.contents,upload->offsets,upload->instance_count*4);memcpy(ring.visuals.contents,upload->visuals,upload->instance_count*64);}
 if(upload->material_count)memcpy(ring.materialOverrides.contents,upload->material_overrides,upload->material_count*48);
 if(upload->distortion_count)memcpy(ring.distortions.contents,upload->distortions,upload->distortion_count*64);ring.distortionCount=upload->distortion_count;
 if(upload->fog_width&&upload->fog_height){if(ring.fog.width!=upload->fog_width||ring.fog.height!=upload->fog_height){MTLTextureDescriptor* d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:upload->fog_width height:upload->fog_height mipmapped:NO];d.storageMode=MTLStorageModeShared;d.usage=MTLTextureUsageShaderRead;ring.fog=[r.device newTextureWithDescriptor:d];}if(!ring.fog){dispatch_semaphore_signal(r->available);return 0;}[ring.fog replaceRegion:MTLRegionMake2D(0,0,upload->fog_width,upload->fog_height) mipmapLevel:0 withBytes:upload->fog bytesPerRow:upload->fog_width*4];}else ring.fog=nil;
 if(upload->overlay_count)memcpy(ring.overlay.contents,upload->overlay,upload->overlay_count*64);ring.overlayCount=upload->overlay_count;
 if(upload->sprite_count)memcpy(ring.sprites.contents,upload->sprites,upload->sprite_count*96);
 if(upload->light_count)memcpy(ring.lights.contents,upload->lights,upload->light_count*32);
 if(!ring.spans)ring.spans=[NSMutableData dataWithLength:r.meshes.count*NM_SPAN_PASSES*8];if(r.meshes.count)memcpy(ring.spans.mutableBytes,upload->spans,r.meshes.count*NM_SPAN_PASSES*8);ring.spriteCount=upload->sprite_count;ring.groundSpriteCount=upload->ground_sprite_count;const uint32_t* copiedSpans=ring.spans.bytes;NSUInteger selectorCount=0;for(NSUInteger i=r.meshes.count;i<r.meshes.count*NM_SPAN_PASSES;i++)selectorCount+=copiedSpans[i*2+1];if(selectorCount)memcpy(ring.selectors.contents,upload->selectors,selectorCount*4);
 ring.terrainFlags=upload->terrain_flags;
 if(!nmProjectionPrepare(r,ring,upload->projections,upload->instance_count,upload->pose_count)){dispatch_semaphore_signal(r->available);return 0;}
 if(!nmGroupsPrepare(r,ring,upload->groups)||!nmFacesPrepare(r,ring,upload->instance_count)||!nmFaceReflectionsPrepare(r,ring)||!nmOutlineRowsPrepare(r,ring)){dispatch_semaphore_signal(r->available);return 0;}
 if(!nmWaterObjectsPrepare(r,ring,upload->water_objects)||!nmWaterObjectsPrepareSources(r,ring,upload->water_sources)){dispatch_semaphore_signal(r->available);return 0;}
 NMUniforms u=r->uniforms;u.camera=(vector_float4){upload->camera[0],upload->camera[1],upload->camera[2],0};u.timing=(vector_float4){upload->alpha,(float)upload->seconds,(float)upload->pose_count,r->effects?1:0};u.mode=(vector_float4){1,upload->light_count,0,0};u.lightControl=(vector_float4){upload->retained_light_count,(upload->light_controls&1)?1:0,(upload->light_controls&2)?1:0,(upload->light_controls&4)?1:0};u.composition=(vector_float4){r.modelAtlasColor.width,r.modelAtlasColor.height,(upload->light_controls&8)?2:1,upload->composition_slot_count};u.visual.x=r->visualPasses?1:0;u.visual.w=upload->rules_count?1:0;u.visual.z=upload->fog_width&&upload->fog_height?1:0;u.fogRect=(vector_float4){upload->fog_rect[0],upload->fog_rect[1],upload->fog_rect[2],upload->fog_rect[3]};r->lastUniforms=u;r.lastRing=ring;
 NSUInteger generation=++r->rowCount;NSUInteger rowIndex=(generation-1)%r->rowCapacity;NMRow* row=&r->rows[rowIndex];@synchronized(r){memset(row,0,sizeof(*row));r->rowGenerations[rowIndex]=generation;}row->wait=(acquired-start)*1000;row->drawable_wait=(ready-acquired)*1000;
 id<MTLCommandBuffer> cb=[r.queue commandBuffer];nmTimingBegin(r.device,r->frameNumber);
 // GPU writes are ordered after previous readers on this serial queue. Only
 // the acquired ring's CPU staging bytes are modified; the atlas is never
 // replaceRegion-written while an earlier command buffer can sample it.
 if(uploadOverlay){id<MTLBlitCommandEncoder> blit=nmBlitEncoder(cb,"hud.atlasUpload");[blit copyFromBuffer:ring.overlayUpload sourceOffset:0 sourceBytesPerRow:overlayRowBytes sourceBytesPerImage:overlayRowBytes*overlayH sourceSize:MTLSizeMake(overlayW,overlayH,1) toTexture:ring.overlayAtlas destinationSlice:0 destinationLevel:0 destinationOrigin:MTLOriginMake(overlayX,overlayY,0)];[blit endEncoding];}
 nmEffectsSetLighting(ring,ring.retainedLights,upload->retained_light_count);
 nmEffectsBegin(r,ring,cb);
 if(!encode(r,cb,ring.poses,ring.interpolatedPoses,u,drawable?drawable.texture:r.output,ring)){dispatch_semaphore_signal(r->available);return 0;}
 if(drawable){dispatch_group_enter(r->presentations);[drawable addPresentedHandler:^(id<MTLDrawable> d){@synchronized(r){if(r->rowGenerations[rowIndex]==generation)r->rows[rowIndex].presented=d.presentedTime;}dispatch_group_leave(r->presentations);}];[cb presentDrawable:drawable];}
 row->encode=(CACurrentMediaTime()-ready)*1000;row->submit=CACurrentMediaTime();row->memory=r.device.currentAllocatedSize;dispatch_group_enter(r->pending);
 {void (^timing)(void)=nmTimingCompletion(r->frameNumber);if(timing)[cb addCompletedHandler:^(id<MTLCommandBuffer> b){(void)b;timing();}];}[cb addCompletedHandler:^(id<MTLCommandBuffer> b){@synchronized(r){if(r->rowGenerations[rowIndex]==generation){NMRow* result=&r->rows[rowIndex];result->gpu_start=b.GPUStartTime;result->gpu_end=b.GPUEndTime;result->completed=CACurrentMediaTime();result->status=(int)b.status;}}if(b.error)r.lastError=b.error.localizedDescription;nmOutlineRowsCompleted(r,ring);dispatch_semaphore_signal(r->available);dispatch_group_leave(r->pending);}];[cb commit];r->frameNumber++;return 1;
 }
}
void nm_drain(void* handle) {NMRenderer* r=(__bridge NMRenderer*)handle;dispatch_group_wait(r->pending,DISPATCH_TIME_FOREVER);if(!r->offscreen&&r->presentationDrainRowCount!=r->rowCount){r->presentationDrainRowCount=r->rowCount;r->presentationDrainTimedOut=dispatch_group_wait(r->presentations,dispatch_time(DISPATCH_TIME_NOW,250*NSEC_PER_MSEC))!=0;}}
int nm_rows(void* handle,void* output,int capacity) {NMRenderer* r=(__bridge NMRenderer*)handle;NSUInteger n=MIN((NSUInteger)MAX(capacity,0),MIN(r->rowCount,r->rowCapacity));@synchronized(r){NMRow* out=output;NSUInteger first=r->rowCount-n;for(NSUInteger i=0;i<n;i++)out[i]=r->rows[(first+i)%r->rowCapacity];}return (int)n;}
int nm_capture(void* handle,const char* path) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;nm_drain(handle);NSUInteger last=(r->frameNumber-1)%3;NMUniforms u=r->lastUniforms;
 id<MTLCommandBuffer> cb=[r.queue commandBuffer];if(!encode(r,cb,r.lastRing?r.lastRing.poses:r.poses[last],r.lastRing?r.lastRing.interpolatedPoses:r.interpolatedPoses[last],u,r.output,r.lastRing))return 0;NSUInteger width=r.output.width,height=r.output.height,bytesPerRow=(width*4+255)&~255;
 id<MTLBuffer> read=[r.device newBufferWithLength:bytesPerRow*height options:MTLResourceStorageModeShared];id<MTLBlitCommandEncoder> blit=nmBlitEncoder(cb,"capture.readback");[blit copyFromTexture:r.output sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(width,height,1) toBuffer:read destinationOffset:0 destinationBytesPerRow:bytesPerRow destinationBytesPerImage:bytesPerRow*height];[blit endEncoding];[cb commit];[cb waitUntilCompleted];if(cb.status==MTLCommandBufferStatusError)return 0;
 NSBitmapImageRep* image=[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:width pixelsHigh:height bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:width*4 bitsPerPixel:32];uint8_t* src=read.contents;uint8_t* dst=image.bitmapData;
 for(NSUInteger y=0;y<height;y++)for(NSUInteger x=0;x<width;x++){NSUInteger a=y*bytesPerRow+x*4,b=(y*width+x)*4;dst[b]=src[a+2];dst[b+1]=src[a+1];dst[b+2]=src[a];dst[b+3]=255;}
 NSData* png=[image representationUsingType:NSBitmapImageFileTypePNG properties:@{}];return [png writeToFile:[NSString stringWithUTF8String:path] atomically:YES]?1:0;}
}
void nm_info(void* handle,char* output,int capacity) {
 @autoreleasepool {NMRenderer* r=(__bridge NMRenderer*)handle;nmTimingWrite();NSUInteger poseOutputBytes=0;for(id<MTLBuffer> poses in r.interpolatedPoses)poseOutputBytes+=poses.length;NSUInteger poseBytes=0,instanceBytes=0,spriteBytes=0,lightBytes=0,visualBytes=0,distortionBytes=0,fogBytes=0,selectorBytes=0,overlayBytes=0,overlayUploadBytes=0;for(NMRing* ring in r.rings){poseOutputBytes+=ring.interpolatedPoses.length;overlayUploadBytes+=ring.overlayUpload.length;overlayBytes+=ring.overlay.length;poseBytes+=ring.poses.length;instanceBytes+=ring.instances.length;spriteBytes+=ring.sprites.length;lightBytes+=ring.lights.length;visualBytes+=ring.visuals.length;distortionBytes+=ring.distortions.length;fogBytes+=ring.fog.width*ring.fog.height*4;selectorBytes+=ring.selectors.length;}NSDictionary* info=@{@"cloak_scratch_bytes":@(r.subjectColor.width*r.subjectColor.height*20),@"model_atlas_bytes":@(r.modelAtlasColor.width*r.modelAtlasColor.height*20),@"model_atlas_dimensions":@[@(r.modelAtlasColor.width),@(r.modelAtlasColor.height)],@"shadow_atlas_bytes":@(r.shadows.body.width*r.shadows.body.height*13),@"shadow_atlas_dimensions":@[@(r.shadows.body.width),@(r.shadows.body.height)],@"model_composition":@"ordered subjects with isolated atlas byte-key depth",@"pose_interpolation_method":@"per-piece shortest-path quaternion slerp",@"pose_interpolation_output_buffer_capacity_bytes":@(poseOutputBytes),@"live_overlay_upload_buffer_capacity_bytes":@(overlayUploadBytes),@"run_loop_service_calls":@(r->runLoopPumps),@"run_loop_handled_sources":@(r->runLoopSources),@"pointer_captured":@(r->pointerCaptured),@"overlay_texture_version":@(r->overlayVersion),@"overlay_texture_uploads":@(r->overlayTextureUploads),@"overlay_texture_uploaded_bytes":@(r->overlayTextureBytes),@"interactive":@(r->interactive),@"total_frames_submitted":@(r->rowCount),@"timing_capacity_frames":@(r->rowCapacity),@"input_queue_capacity":@NM_EVENT_CAPACITY,@"input_queue_overflow":@(r->eventOverflow),@"live_overlay_buffer_capacity_bytes":@(overlayBytes),@"retained_overlay_atlas_bytes":@(r.overlayAtlas.width*r.overlayAtlas.height*4),@"live_selector_buffer_capacity_bytes":@(selectorBytes),@"live_visual_buffer_capacity_bytes":@(visualBytes),@"live_distortion_buffer_capacity_bytes":@(distortionBytes),@"live_fog_texture_capacity_bytes":@(fogBytes),@"visual_passes":@(r->visualPasses),@"visual_hdr_target_bytes":@(r.postColor.width*r.postColor.height*8*2),@"visual_shadow_mask_bytes":@(r->visualPasses?r.shadowMask.width*r.shadowMask.height:0),@"visual_target_bytes":@(r.postColor.width*r.postColor.height*17),@"retained_height_bytes":@(r.heights.width*r.heights.height*4),@"retained_material_bytes":@(r.materials.length+r.textureFrames.length),@"live_pose_buffer_capacity_bytes":@(poseBytes),@"live_instance_buffer_capacity_bytes":@(instanceBytes),@"live_sprite_buffer_capacity_bytes":@(spriteBytes),@"live_light_buffer_capacity_bytes":@(lightBytes),@"device":r.device.name,@"current_allocated_bytes":@(r.device.currentAllocatedSize),@"recommended_working_set_bytes":@(r.device.recommendedMaxWorkingSetSize),@"unified_memory":@(r.device.hasUnifiedMemory),@"max_buffer_length":@(r.device.maxBufferLength),@"native_error":r.lastError?:@"",@"presentation_drain_timed_out":@(r->presentationDrainTimedOut),@"presentation_drain_timeout_ms":@250,@"drawable_width":@(r.output.width),@"drawable_height":@(r.output.height),@"view_width_points":@(r.window.contentView.bounds.size.width),@"view_height_points":@(r.window.contentView.bounds.size.height),@"layer_contents_scale":@(r.layer.contentsScale),@"screen_maximum_fps":@(NSScreen.mainScreen.maximumFramesPerSecond)};NSMutableDictionary* combined=[info mutableCopy];[combined addEntriesFromDictionary:nmEffectsInfo(r)];[combined addEntriesFromDictionary:nmGroundInfo(r)];[combined addEntriesFromDictionary:nmWaterObjectsInfo(r)];[combined addEntriesFromDictionary:nmGroupsInfo(r)];[combined addEntriesFromDictionary:nmTerrainTilesInfo(r.terrainTiles)];[combined addEntriesFromDictionary:nmFaceReflectionsInfo(r.lastRing)];[combined addEntriesFromDictionary:nmOutlineRowsInfo(r,r.lastRing)];[combined addEntriesFromDictionary:nmWaterTilesInfo(r.waterObjectsState.tiles,r.lastRing.waterObjectsRing.tiles)];combined[@"stock_effects_bloom_submitted"]=@(r.lastRing.glowEnabled);NSData* d=[NSJSONSerialization dataWithJSONObject:combined options:0 error:nil];NSString* s=[[NSString alloc]initWithData:d encoding:NSUTF8StringEncoding];copyError(output,capacity,s);}
}
void nm_destroy(void* handle) { @autoreleasepool {NMRenderer* r=(__bridge_transfer NMRenderer*)handle;nm_drain((__bridge void*)r);restorePointer(r);if(r->cursorHidden){[NSCursor unhide];r->cursorHidden=NO;}[r.window orderOut:nil];[r.window close];} }
