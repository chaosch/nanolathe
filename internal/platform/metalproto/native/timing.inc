// Diagnostic per-pass GPU timing. Disabled unless NANOLATHE_METAL_PASS_TIMING
// names an output CSV. Every encoder created through these helpers carries a
// stable source label; render passes sample the start/end of their vertex and
// fragment stages, compute and blit passes the start/end of the encoder.
// Apple GPUs support only stage-boundary sampling, so per-draw costs are not
// available. Adjacent passes may overlap on a tile GPU: spans are attribution,
// not exclusive busy time, and must not be summed as if serial.
#define NM_TIMING_SLOTS 3
#define NM_TIMING_PASSES 512
typedef struct { uint32_t frame; uint16_t index, kind; const char* label; uint64_t t[4]; } NMPassRecord;
static BOOL nmTimingEnabled;
static NSString* nmTimingPath;
static id<MTLCounterSampleBuffer> nmTimingBuffers[NM_TIMING_SLOTS];
static const char* nmTimingLabels[NM_TIMING_SLOTS][NM_TIMING_PASSES];
static uint16_t nmTimingKinds[NM_TIMING_SLOTS][NM_TIMING_PASSES];
static uint32_t nmTimingUsed[NM_TIMING_SLOTS];
static uint32_t nmTimingSlot;
static BOOL nmTimingActive;
static NSMutableData* nmTimingRecords;
static MTLTimestamp nmTimingCPU0, nmTimingGPU0, nmTimingCPU1, nmTimingGPU1;
static double nmTimingWall0, nmTimingWall1;

static void nmTimingInitialize(id<MTLDevice> device) {
 const char* path=getenv("NANOLATHE_METAL_PASS_TIMING");
 if(!path||!*path)return;
 if(![device supportsCounterSampling:MTLCounterSamplingPointAtStageBoundary]){fprintf(stderr,"nanolathe: Metal pass timing: device lacks stage-boundary counter sampling\n");return;}
 id<MTLCounterSet> timestamps=nil;
 for(id<MTLCounterSet> set in device.counterSets)if([set.name isEqualToString:MTLCommonCounterSetTimestamp])timestamps=set;
 if(!timestamps){fprintf(stderr,"nanolathe: Metal pass timing: no timestamp counter set\n");return;}
 MTLCounterSampleBufferDescriptor* d=[MTLCounterSampleBufferDescriptor new];
 d.counterSet=timestamps;d.storageMode=MTLStorageModeShared;d.sampleCount=NM_TIMING_PASSES*4;
 for(int i=0;i<NM_TIMING_SLOTS;i++){NSError* e=nil;d.label=[NSString stringWithFormat:@"pass timing %d",i];
  nmTimingBuffers[i]=[device newCounterSampleBufferWithDescriptor:d error:&e];
  if(!nmTimingBuffers[i]){fprintf(stderr,"nanolathe: Metal pass timing: sample buffer: %s\n",e.localizedDescription.UTF8String?:"unknown");return;}}
 nmTimingPath=[NSString stringWithUTF8String:path];
 nmTimingRecords=[NSMutableData data];
 [device sampleTimestamps:&nmTimingCPU0 gpuTimestamp:&nmTimingGPU0];nmTimingWall0=CACurrentMediaTime();
 nmTimingEnabled=YES;
}
// Call once per frame after creating its command buffer. The bounded queue
// guarantees the slot's previous command buffer has completed.
static void nmTimingBegin(id<MTLDevice> device,NSUInteger frame) {
 if(!nmTimingEnabled)return;
 nmTimingSlot=(uint32_t)(frame%NM_TIMING_SLOTS);nmTimingUsed[nmTimingSlot]=0;nmTimingActive=YES;
 [device sampleTimestamps:&nmTimingCPU1 gpuTimestamp:&nmTimingGPU1];nmTimingWall1=CACurrentMediaTime();
}
static int nmTimingClaim(uint16_t kind,const char* label) {
 if(!nmTimingActive)return -1;
 uint32_t s=nmTimingSlot,i=nmTimingUsed[s];
 if(i>=NM_TIMING_PASSES)return -1;
 nmTimingLabels[s][i]=label;nmTimingKinds[s][i]=kind;nmTimingUsed[s]=i+1;
 return (int)i;
}
static id<MTLRenderCommandEncoder> nmRenderEncoder(id<MTLCommandBuffer> cb,MTLRenderPassDescriptor* p,const char* label) {
 int i=nmTimingClaim(1,label);
 if(i>=0){MTLRenderPassSampleBufferAttachmentDescriptor* a=p.sampleBufferAttachments[0];a.sampleBuffer=nmTimingBuffers[nmTimingSlot];
  a.startOfVertexSampleIndex=i*4;a.endOfVertexSampleIndex=i*4+1;a.startOfFragmentSampleIndex=i*4+2;a.endOfFragmentSampleIndex=i*4+3;}
 id<MTLRenderCommandEncoder> enc=[cb renderCommandEncoderWithDescriptor:p];
 if(i>=0)p.sampleBufferAttachments[0].sampleBuffer=nil;
 enc.label=@(label);
 return enc;
}
static id<MTLComputeCommandEncoder> nmComputeEncoder(id<MTLCommandBuffer> cb,const char* label) {
 int i=nmTimingClaim(2,label);id<MTLComputeCommandEncoder> enc;
 if(i>=0){MTLComputePassDescriptor* p=[MTLComputePassDescriptor computePassDescriptor];MTLComputePassSampleBufferAttachmentDescriptor* a=p.sampleBufferAttachments[0];
  a.sampleBuffer=nmTimingBuffers[nmTimingSlot];a.startOfEncoderSampleIndex=i*4;a.endOfEncoderSampleIndex=i*4+1;enc=[cb computeCommandEncoderWithDescriptor:p];}
 else enc=[cb computeCommandEncoder];
 enc.label=@(label);
 return enc;
}
static id<MTLBlitCommandEncoder> nmBlitEncoder(id<MTLCommandBuffer> cb,const char* label) {
 int i=nmTimingClaim(3,label);id<MTLBlitCommandEncoder> enc;
 if(i>=0){MTLBlitPassDescriptor* p=[MTLBlitPassDescriptor blitPassDescriptor];MTLBlitPassSampleBufferAttachmentDescriptor* a=p.sampleBufferAttachments[0];
  a.sampleBuffer=nmTimingBuffers[nmTimingSlot];a.startOfEncoderSampleIndex=i*4;a.endOfEncoderSampleIndex=i*4+1;enc=[cb blitCommandEncoderWithDescriptor:p];}
 else enc=[cb blitCommandEncoder];
 enc.label=@(label);
 return enc;
}
// Returns the completion work for the frame begun last; run it from the
// command buffer's completed handler.
static void (^nmTimingCompletion(NSUInteger frame))(void) {
 if(!nmTimingEnabled||!nmTimingActive)return nil;
 uint32_t s=nmTimingSlot,n=nmTimingUsed[s];
 if(!n)return nil;
 NMPassRecord* records=calloc(n,sizeof(NMPassRecord));
 for(uint32_t i=0;i<n;i++){records[i].frame=(uint32_t)frame;records[i].index=(uint16_t)i;records[i].kind=nmTimingKinds[s][i];records[i].label=nmTimingLabels[s][i];}
 id<MTLCounterSampleBuffer> buffer=nmTimingBuffers[s];
 return ^{
  NSData* data=[buffer resolveCounterRange:NSMakeRange(0,n*4)];
  const MTLCounterResultTimestamp* t=data.bytes;
  if(t&&data.length>=n*4*sizeof(MTLCounterResultTimestamp))for(uint32_t i=0;i<n;i++)for(int k=0;k<4;k++)records[i].t[k]=t[i*4+k].timestamp;
  @synchronized(nmTimingRecords){[nmTimingRecords appendBytes:records length:n*sizeof(NMPassRecord)];}
  free(records);
 };
}
// Writes every resolved record. Unsampled lanes are MTLCounterErrorValue.
static void nmTimingWrite(void) {
 if(!nmTimingEnabled||!nmTimingPath)return;
 NSMutableString* out=[NSMutableString stringWithString:@"frame,index,kind,label,t0,t1,t2,t3\n"];
 @synchronized(nmTimingRecords){
  const NMPassRecord* r=nmTimingRecords.bytes;NSUInteger n=nmTimingRecords.length/sizeof(NMPassRecord);
  for(NSUInteger i=0;i<n;i++)[out appendFormat:@"%u,%u,%u,%s,%llu,%llu,%llu,%llu\n",r[i].frame,r[i].index,r[i].kind,r[i].label,r[i].t[0],r[i].t[1],r[i].t[2],r[i].t[3]];
 }
 [out writeToFile:nmTimingPath atomically:YES encoding:NSUTF8StringEncoding error:nil];
 double seconds=nmTimingWall1-nmTimingWall0;
 NSDictionary* calibration=@{@"gpu_ticks_per_second":@(seconds>0?(double)(nmTimingGPU1-nmTimingGPU0)/seconds:0),@"cpu_ticks_per_second":@(seconds>0?(double)(nmTimingCPU1-nmTimingCPU0)/seconds:0),@"calibration_seconds":@(seconds),@"error_value":@(MTLCounterErrorValue)};
 NSData* json=[NSJSONSerialization dataWithJSONObject:calibration options:NSJSONWritingPrettyPrinted error:nil];
 [json writeToFile:[nmTimingPath stringByAppendingString:@".json"] atomically:YES];
}

// Diagnostic ablation for whole-frame A/B: NANOLATHE_METAL_ABLATE is a comma
// list of pass families to skip (effects, glow, water). Output is incomplete.
static BOOL nmAblate(const char* family) {
 static NSArray<NSString*>* list;static dispatch_once_t once;
 dispatch_once(&once,^{const char* v=getenv("NANOLATHE_METAL_ABLATE");list=v?[[NSString stringWithUTF8String:v] componentsSeparatedByString:@","]:@[];});
 return [list containsObject:@(family)];
}
