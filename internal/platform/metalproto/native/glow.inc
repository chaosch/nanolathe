@implementation NMGlow
@end
static id<MTLRenderPipelineState> nmGlowPipeline(id<MTLDevice> d,id<MTLLibrary> l,NSString *v,NSString *f,MTLPixelFormat format,BOOL screen,NSError **e) {
    MTLRenderPipelineDescriptor *p=[MTLRenderPipelineDescriptor new];p.vertexFunction=[l newFunctionWithName:v];p.fragmentFunction=[l newFunctionWithName:f];
    MTLRenderPipelineColorAttachmentDescriptor *a=p.colorAttachments[0];a.pixelFormat=format;a.blendingEnabled=YES;
    a.sourceRGBBlendFactor=MTLBlendFactorOne;a.destinationRGBBlendFactor=screen?MTLBlendFactorOneMinusSourceColor:MTLBlendFactorOne;
    a.sourceAlphaBlendFactor=screen?MTLBlendFactorZero:MTLBlendFactorOne;a.destinationAlphaBlendFactor=MTLBlendFactorOne;
    return [d newRenderPipelineStateWithDescriptor:p error:e];
}
static NMGlow *nmGlowInitialize(id<MTLDevice> d,id<MTLLibrary> l,MTLPixelFormat format,NSError **e) {
    NMGlow *g=[NMGlow new];g.device=d;g.destinationFormat=format;
    g.source=nmGlowPipeline(d,l,@"nm_glow_source_vertex",@"nm_glow_source_fragment",MTLPixelFormatRGBA16Float,NO,e);
    g.resolve=nmGlowPipeline(d,l,@"nm_glow_resolve_vertex",@"nm_glow_resolve_fragment",format,YES,e);
    g.shrink=[d newComputePipelineStateWithFunction:[l newFunctionWithName:@"nm_glow_shrink"] error:e];
    g.across=[d newComputePipelineStateWithFunction:[l newFunctionWithName:@"nm_glow_across"] error:e];
    g.down=[d newComputePipelineStateWithFunction:[l newFunctionWithName:@"nm_glow_down"] error:e];
    return g.source&&g.resolve&&g.shrink&&g.across&&g.down?g:nil;
}
static id<MTLTexture> nmGlowTarget(NMGlow *g,NSUInteger w,NSUInteger h) {
    MTLTextureDescriptor *d=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA16Float width:w height:h mipmapped:NO];d.storageMode=MTLStorageModePrivate;d.usage=MTLTextureUsageShaderRead|MTLTextureUsageShaderWrite;return [g.device newTextureWithDescriptor:d];
}
static BOOL nmGlowPrepare(NMGlow *g,NSUInteger w,NSUInteger h) {
    if(!g||!w||!h)return NO;if(g.width==w&&g.height==h&&g.octaves)return YES;
    NSUInteger qw=(w+3)/4,qh=(h+3)/4,ew=(w+7)/8;
    g.quarter=nmGlowTarget(g,qw,qh);g.horizontal=nmGlowTarget(g,qw+ew,qh);g.octaves=nmGlowTarget(g,qw+ew+3,qh+2);
    if(!g.quarter||!g.horizontal||!g.octaves)return NO;g.width=w;g.height=h;return YES;
}
static void nmGlowDispatch(id<MTLComputeCommandEncoder> e,id<MTLTexture> destination) {
    [e dispatchThreads:MTLSizeMake(destination.width,destination.height,1) threadsPerThreadgroup:MTLSizeMake(16,16,1)];[e endEncoding];
}
// Set when the final drawable pass screens the octaves itself.
static BOOL nmGlowDeferResolve;
static BOOL nmGlowEncode(NMGlow *g,id<MTLCommandBuffer> cb,id<MTLTexture> emission,id<MTLTexture> completed,id<MTLTexture> atlas,id<MTLTexture> destination,id<MTLBuffer> vertices,NSUInteger n,NMGlowParameters u) {
    if(!n)return YES;
    if(!g||!cb||!emission||!completed||!atlas||!destination||!vertices||n%6||n>vertices.length/sizeof(NMGlowVertex)||emission.pixelFormat!=MTLPixelFormatRGBA16Float||destination.pixelFormat!=g.destinationFormat||completed.width!=emission.width||completed.height!=emission.height||destination.width!=emission.width||destination.height!=emission.height)return NO;
    if(!nmGlowPrepare(g,emission.width,emission.height))return NO;
    vector_float2 size={g.width,g.height};vector_uint4 sizes={(g.width+3)/4,(g.height+3)/4,(g.width+7)/8,(g.height+7)/8};
    MTLRenderPassDescriptor *p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=emission;p.colorAttachments[0].loadAction=MTLLoadActionClear;p.colorAttachments[0].storeAction=MTLStoreActionStore;
    id<MTLRenderCommandEncoder> r=nmRenderEncoder(cb,p,"glow.nmGlowEncode#1");[r setRenderPipelineState:g.source];[r setVertexBuffer:vertices offset:0 atIndex:0];[r setVertexBytes:&size length:sizeof(size) atIndex:1];[r setFragmentTexture:atlas atIndex:0];[r setFragmentTexture:completed atIndex:1];[r drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:n];[r endEncoding];
    id<MTLComputeCommandEncoder> e=nmComputeEncoder(cb,"glow.nmGlowEncode#2");[e setComputePipelineState:g.shrink];[e setTexture:emission atIndex:0];[e setTexture:g.quarter atIndex:1];nmGlowDispatch(e,g.quarter);
    e=nmComputeEncoder(cb,"glow.nmGlowEncode#3");[e setComputePipelineState:g.across];[e setTexture:g.quarter atIndex:0];[e setTexture:g.horizontal atIndex:1];[e setBytes:&u length:sizeof(u) atIndex:0];nmGlowDispatch(e,g.horizontal);
    e=nmComputeEncoder(cb,"glow.nmGlowEncode#4");[e setComputePipelineState:g.down];[e setTexture:g.horizontal atIndex:0];[e setTexture:g.octaves atIndex:1];[e setBytes:&u length:sizeof(u) atIndex:0];[e setBytes:&sizes length:sizeof(sizes) atIndex:1];nmGlowDispatch(e,g.octaves);
    if(nmGlowDeferResolve)return YES;
    p=[MTLRenderPassDescriptor renderPassDescriptor];p.colorAttachments[0].texture=destination;p.colorAttachments[0].loadAction=MTLLoadActionLoad;p.colorAttachments[0].storeAction=MTLStoreActionStore;
    r=nmRenderEncoder(cb,p,"glow.nmGlowEncode#5");[r setRenderPipelineState:g.resolve];[r setFragmentTexture:g.octaves atIndex:0];[r setFragmentBytes:&u length:sizeof(u) atIndex:0];[r setFragmentBytes:&sizes length:sizeof(sizes) atIndex:1];[r drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];[r endEncoding];return YES;
}
