//go:build darwin && arm64

package mtl

import "unsafe"

// Selectors, registered once by Load.
var (
	selAlloc, selNew, selInit, selRelease, selRetain, selAutorelease                           SEL
	selStringWithUTF8String, selUTF8String, selLength, selCharacterAtIndex, selIsEqualToString SEL
	selLocalizedDescription, selCount, selObjectAtIndex, selObjectAtIndexedSubscript           SEL
	selName, selFirstObject, selSetLabel                                                       SEL

	// Device and resources.
	selNewCommandQueue, selNewBufferWithLength, selNewTextureWithDescriptor, selNewLibraryWithSource SEL
	selNewRenderPipelineState, selNewComputePipelineState, selNewDepthStencilState                   SEL
	selMaxBufferLength, selCurrentAllocatedSize, selRecommendedMaxWorkingSetSize                     SEL
	selHasUnifiedMemory, selReadWriteTextureSupport, selContents                                     SEL
	selNewFunctionWithName, selNewFunctionWithNameConstants, selSetConstantValue                     SEL
	selSetVertexFunction, selSetFragmentFunction, selColorAttachments, selSetPixelFormat             SEL
	selSetBlendingEnabled, selSetSourceRGBBlendFactor, selSetDestinationRGBBlendFactor               SEL
	selSetSourceAlphaBlendFactor, selSetDestinationAlphaBlendFactor, selSetRgbBlendOperation         SEL
	selSetAlphaBlendOperation, selSetWriteMask, selSetDepthAttachmentPixelFormat                     SEL
	selMaxTotalThreadsPerThreadgroup, selSetDepthCompareFunction, selSetDepthWriteEnabled            SEL
	selTexture2DDescriptor, selSetStorageMode, selSetUsage, selReplaceRegion                         SEL

	// Command submission.
	selCommandBuffer, selRenderCommandEncoderWithDescriptor, selComputeCommandEncoderWithDispatchType SEL
	selBlitCommandEncoder, selPresentDrawable, selCommit, selWaitUntilCompleted, selStatus, selError  SEL
	selGPUStartTime, selGPUEndTime, selRenderPassDescriptor, selSetTexture, selSetLoadAction          SEL
	selSetStoreAction, selSetClearColor, selDepthAttachment, selSetClearDepth                         SEL
	selSetRenderTargetWidth, selSetRenderTargetHeight                                                 SEL

	// Encoders.
	selSetRenderPipelineState, selSetDepthStencilState, selSetCullMode, selSetFrontFacingWinding   SEL
	selSetVertexBuffer, selSetFragmentBuffer, selSetVertexBufferOffset, selSetFragmentBufferOffset SEL
	selSetVertexBytes, selSetFragmentBytes, selSetVertexTexture, selSetFragmentTexture             SEL
	selSetViewport, selDrawPrimitives, selDrawPrimitivesInstanced, selDrawPrimitivesBase           SEL
	selDrawIndexed, selDrawIndexedInstanced, selEndEncoding                                        SEL
	selSetComputePipelineState, selSetBuffer, selSetBytes, selSetComputeTexture                    SEL
	selDispatchThreads, selDispatchThreadgroups, selMemoryBarrierWithScope                         SEL
	selCopyBufferToTexture, selCopyTextureToTexture, selCopyTextureToBuffer, selFillBuffer         SEL

	// Layer, window, application and events.
	selLayer, selSetDevice, selSetDrawableSize, selSetContentsScale, selSetFramebufferOnly      SEL
	selSetMaximumDrawableCount, selSetDisplaySyncEnabled, selNextDrawable, selDrawableTexture   SEL
	selPresentedTime, selDrawableSize, selContentsScale                                         SEL
	selSharedApplication, selSetActivationPolicy, selFinishLaunching, selMainScreen, selScreens SEL
	selBackingScaleFactor, selFrame, selMaximumFramesPerSecond                                  SEL
	selInitWithContentRect, selSetReleasedWhenClosed, selSetTitle, selContentView               SEL
	selSetWantsLayer, selSetLayer, selSetAcceptsMouseMovedEvents, selCenter                     SEL
	selMakeKeyAndOrderFront, selActivateIgnoringOtherApps, selUpdateWindows, selIsVisible       SEL
	selIsKeyWindow, selIsMainWindow, selMakeKeyWindow, selClose, selOrderOut, selIsActive       SEL
	selKeyWindow, selConvertPointToScreen, selConvertPointFromScreen, selConvertPointToView     SEL
	selConvertPointFromView, selBounds, selIsFlipped, selNextEventMatchingMask, selSendEvent    SEL
	selDistantPast, selType, selWindow, selModifierFlags, selKeyCode, selIsARepeat              SEL
	selCharacters, selCharactersIgnoringModifiers, selLocationInWindow, selButtonNumber         SEL
	selClickCount, selScrollingDeltaX, selScrollingDeltaY, selHasPreciseScrollingDeltas         SEL
	selMomentumPhase, selPhase, selMagnification, selDeltaX, selDeltaY, selCGEvent              SEL
	selMouseLocation, selHide, selUnhide, selIsMainThread                                       SEL
)

func initSelectors() {
	for _, s := range []struct {
		p    *SEL
		name string
	}{
		{&selAlloc, "alloc"}, {&selNew, "new"}, {&selInit, "init"}, {&selRelease, "release"}, {&selRetain, "retain"}, {&selAutorelease, "autorelease"},
		{&selStringWithUTF8String, "stringWithUTF8String:"}, {&selUTF8String, "UTF8String"}, {&selLength, "length"}, {&selCharacterAtIndex, "characterAtIndex:"}, {&selIsEqualToString, "isEqualToString:"},
		{&selLocalizedDescription, "localizedDescription"}, {&selCount, "count"}, {&selObjectAtIndex, "objectAtIndex:"}, {&selObjectAtIndexedSubscript, "objectAtIndexedSubscript:"},
		{&selName, "name"}, {&selFirstObject, "firstObject"}, {&selSetLabel, "setLabel:"},
		{&selNewCommandQueue, "newCommandQueue"}, {&selNewBufferWithLength, "newBufferWithLength:options:"}, {&selNewTextureWithDescriptor, "newTextureWithDescriptor:"}, {&selNewLibraryWithSource, "newLibraryWithSource:options:error:"},
		{&selNewRenderPipelineState, "newRenderPipelineStateWithDescriptor:error:"}, {&selNewComputePipelineState, "newComputePipelineStateWithFunction:error:"}, {&selNewDepthStencilState, "newDepthStencilStateWithDescriptor:"},
		{&selMaxBufferLength, "maxBufferLength"}, {&selCurrentAllocatedSize, "currentAllocatedSize"}, {&selRecommendedMaxWorkingSetSize, "recommendedMaxWorkingSetSize"},
		{&selHasUnifiedMemory, "hasUnifiedMemory"}, {&selReadWriteTextureSupport, "readWriteTextureSupport"}, {&selContents, "contents"},
		{&selNewFunctionWithName, "newFunctionWithName:"}, {&selNewFunctionWithNameConstants, "newFunctionWithName:constantValues:error:"}, {&selSetConstantValue, "setConstantValue:type:atIndex:"},
		{&selSetVertexFunction, "setVertexFunction:"}, {&selSetFragmentFunction, "setFragmentFunction:"}, {&selColorAttachments, "colorAttachments"}, {&selSetPixelFormat, "setPixelFormat:"},
		{&selSetBlendingEnabled, "setBlendingEnabled:"}, {&selSetSourceRGBBlendFactor, "setSourceRGBBlendFactor:"}, {&selSetDestinationRGBBlendFactor, "setDestinationRGBBlendFactor:"},
		{&selSetSourceAlphaBlendFactor, "setSourceAlphaBlendFactor:"}, {&selSetDestinationAlphaBlendFactor, "setDestinationAlphaBlendFactor:"}, {&selSetRgbBlendOperation, "setRgbBlendOperation:"},
		{&selSetAlphaBlendOperation, "setAlphaBlendOperation:"}, {&selSetWriteMask, "setWriteMask:"}, {&selSetDepthAttachmentPixelFormat, "setDepthAttachmentPixelFormat:"},
		{&selMaxTotalThreadsPerThreadgroup, "maxTotalThreadsPerThreadgroup"}, {&selSetDepthCompareFunction, "setDepthCompareFunction:"}, {&selSetDepthWriteEnabled, "setDepthWriteEnabled:"},
		{&selTexture2DDescriptor, "texture2DDescriptorWithPixelFormat:width:height:mipmapped:"}, {&selSetStorageMode, "setStorageMode:"}, {&selSetUsage, "setUsage:"}, {&selReplaceRegion, "replaceRegion:mipmapLevel:withBytes:bytesPerRow:"},
		{&selCommandBuffer, "commandBuffer"}, {&selRenderCommandEncoderWithDescriptor, "renderCommandEncoderWithDescriptor:"}, {&selComputeCommandEncoderWithDispatchType, "computeCommandEncoderWithDispatchType:"},
		{&selBlitCommandEncoder, "blitCommandEncoder"}, {&selPresentDrawable, "presentDrawable:"}, {&selCommit, "commit"}, {&selWaitUntilCompleted, "waitUntilCompleted"}, {&selStatus, "status"}, {&selError, "error"},
		{&selGPUStartTime, "GPUStartTime"}, {&selGPUEndTime, "GPUEndTime"}, {&selRenderPassDescriptor, "renderPassDescriptor"}, {&selSetTexture, "setTexture:"}, {&selSetLoadAction, "setLoadAction:"},
		{&selSetStoreAction, "setStoreAction:"}, {&selSetClearColor, "setClearColor:"}, {&selDepthAttachment, "depthAttachment"}, {&selSetClearDepth, "setClearDepth:"},
		{&selSetRenderTargetWidth, "setRenderTargetWidth:"}, {&selSetRenderTargetHeight, "setRenderTargetHeight:"},
		{&selSetRenderPipelineState, "setRenderPipelineState:"}, {&selSetDepthStencilState, "setDepthStencilState:"}, {&selSetCullMode, "setCullMode:"}, {&selSetFrontFacingWinding, "setFrontFacingWinding:"},
		{&selSetVertexBuffer, "setVertexBuffer:offset:atIndex:"}, {&selSetFragmentBuffer, "setFragmentBuffer:offset:atIndex:"}, {&selSetVertexBufferOffset, "setVertexBufferOffset:atIndex:"}, {&selSetFragmentBufferOffset, "setFragmentBufferOffset:atIndex:"},
		{&selSetVertexBytes, "setVertexBytes:length:atIndex:"}, {&selSetFragmentBytes, "setFragmentBytes:length:atIndex:"}, {&selSetVertexTexture, "setVertexTexture:atIndex:"}, {&selSetFragmentTexture, "setFragmentTexture:atIndex:"},
		{&selSetViewport, "setViewport:"}, {&selDrawPrimitives, "drawPrimitives:vertexStart:vertexCount:"}, {&selDrawPrimitivesInstanced, "drawPrimitives:vertexStart:vertexCount:instanceCount:"}, {&selDrawPrimitivesBase, "drawPrimitives:vertexStart:vertexCount:instanceCount:baseInstance:"},
		{&selDrawIndexed, "drawIndexedPrimitives:indexCount:indexType:indexBuffer:indexBufferOffset:"}, {&selDrawIndexedInstanced, "drawIndexedPrimitives:indexCount:indexType:indexBuffer:indexBufferOffset:instanceCount:"}, {&selEndEncoding, "endEncoding"},
		{&selSetComputePipelineState, "setComputePipelineState:"}, {&selSetBuffer, "setBuffer:offset:atIndex:"}, {&selSetBytes, "setBytes:length:atIndex:"}, {&selSetComputeTexture, "setTexture:atIndex:"},
		{&selDispatchThreads, "dispatchThreads:threadsPerThreadgroup:"}, {&selDispatchThreadgroups, "dispatchThreadgroups:threadsPerThreadgroup:"}, {&selMemoryBarrierWithScope, "memoryBarrierWithScope:"},
		{&selCopyBufferToTexture, "copyFromBuffer:sourceOffset:sourceBytesPerRow:sourceBytesPerImage:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"},
		{&selCopyTextureToTexture, "copyFromTexture:sourceSlice:sourceLevel:sourceOrigin:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"},
		{&selCopyTextureToBuffer, "copyFromTexture:sourceSlice:sourceLevel:sourceOrigin:sourceSize:toBuffer:destinationOffset:destinationBytesPerRow:destinationBytesPerImage:"},
		{&selFillBuffer, "fillBuffer:range:value:"},
		{&selLayer, "layer"}, {&selSetDevice, "setDevice:"}, {&selSetDrawableSize, "setDrawableSize:"}, {&selSetContentsScale, "setContentsScale:"}, {&selSetFramebufferOnly, "setFramebufferOnly:"},
		{&selSetMaximumDrawableCount, "setMaximumDrawableCount:"}, {&selSetDisplaySyncEnabled, "setDisplaySyncEnabled:"}, {&selNextDrawable, "nextDrawable"}, {&selDrawableTexture, "texture"},
		{&selPresentedTime, "presentedTime"}, {&selDrawableSize, "drawableSize"}, {&selContentsScale, "contentsScale"},
		{&selSharedApplication, "sharedApplication"}, {&selSetActivationPolicy, "setActivationPolicy:"}, {&selFinishLaunching, "finishLaunching"}, {&selMainScreen, "mainScreen"}, {&selScreens, "screens"},
		{&selBackingScaleFactor, "backingScaleFactor"}, {&selFrame, "frame"}, {&selMaximumFramesPerSecond, "maximumFramesPerSecond"},
		{&selInitWithContentRect, "initWithContentRect:styleMask:backing:defer:"}, {&selSetReleasedWhenClosed, "setReleasedWhenClosed:"}, {&selSetTitle, "setTitle:"}, {&selContentView, "contentView"},
		{&selSetWantsLayer, "setWantsLayer:"}, {&selSetLayer, "setLayer:"}, {&selSetAcceptsMouseMovedEvents, "setAcceptsMouseMovedEvents:"}, {&selCenter, "center"},
		{&selMakeKeyAndOrderFront, "makeKeyAndOrderFront:"}, {&selActivateIgnoringOtherApps, "activateIgnoringOtherApps:"}, {&selUpdateWindows, "updateWindows"}, {&selIsVisible, "isVisible"},
		{&selIsKeyWindow, "isKeyWindow"}, {&selIsMainWindow, "isMainWindow"}, {&selMakeKeyWindow, "makeKeyWindow"}, {&selClose, "close"}, {&selOrderOut, "orderOut:"}, {&selIsActive, "isActive"},
		{&selKeyWindow, "keyWindow"}, {&selConvertPointToScreen, "convertPointToScreen:"}, {&selConvertPointFromScreen, "convertPointFromScreen:"}, {&selConvertPointToView, "convertPoint:toView:"},
		{&selConvertPointFromView, "convertPoint:fromView:"}, {&selBounds, "bounds"}, {&selIsFlipped, "isFlipped"}, {&selNextEventMatchingMask, "nextEventMatchingMask:untilDate:inMode:dequeue:"}, {&selSendEvent, "sendEvent:"},
		{&selDistantPast, "distantPast"}, {&selType, "type"}, {&selWindow, "window"}, {&selModifierFlags, "modifierFlags"}, {&selKeyCode, "keyCode"}, {&selIsARepeat, "isARepeat"},
		{&selCharacters, "characters"}, {&selCharactersIgnoringModifiers, "charactersIgnoringModifiers"}, {&selLocationInWindow, "locationInWindow"}, {&selButtonNumber, "buttonNumber"},
		{&selClickCount, "clickCount"}, {&selScrollingDeltaX, "scrollingDeltaX"}, {&selScrollingDeltaY, "scrollingDeltaY"}, {&selHasPreciseScrollingDeltas, "hasPreciseScrollingDeltas"},
		{&selMomentumPhase, "momentumPhase"}, {&selPhase, "phase"}, {&selMagnification, "magnification"}, {&selDeltaX, "deltaX"}, {&selDeltaY, "deltaY"}, {&selCGEvent, "CGEvent"},
		{&selMouseLocation, "mouseLocation"}, {&selHide, "hide"}, {&selUnhide, "unhide"}, {&selIsMainThread, "isMainThread"},
	} {
		*s.p = Sel(s.name)
	}
}

// Release and Retain manage a +1 reference; nil is ignored.
func Release(id ID) {
	if id != 0 {
		send0(id, selRelease)
	}
}
func Retain(id ID) ID {
	if id != 0 {
		send0(id, selRetain)
	}
	return id
}

// New is [Class new]: a +1 instance.
func New(class string) ID { return ID(send0(GetClass(class), selNew)) }

// String returns an autoreleased NSString.
func String(s string) ID {
	b := append([]byte(s), 0)
	r := send1(GetClass("NSString"), selStringWithUTF8String, uintptr(unsafe.Pointer(&b[0])))
	keep(b)
	return ID(r)
}

// GoString copies an NSString.
func GoString(s ID) string {
	if s == 0 {
		return ""
	}
	p := send0(s, selUTF8String)
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(CPtr(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(CPtr(p)), n))
}

// ErrorText is an NSError's localized description.
func ErrorText(err ID) string {
	if err == 0 {
		return ""
	}
	return GoString(ID(send0(err, selLocalizedDescription)))
}

// StringCharacters returns an NSString's UTF-16 code units.
func StringCharacters(s ID, dst []uint16) []uint16 {
	dst = dst[:0]
	if s == 0 {
		return dst
	}
	n := int(send0(s, selLength))
	for i := 0; i < n; i++ {
		dst = append(dst, uint16(send1(s, selCharacterAtIndex, uintptr(i))))
	}
	return dst
}

// IsMainThread is [NSThread isMainThread].
func IsMainThread() bool { return send0(GetClass("NSThread"), selIsMainThread)&0xff != 0 }

// Label sets a debug label from a cached NSString.
func Label(obj ID, label ID) { send1(obj, selSetLabel, uintptr(label)) }

// Generic property helpers for set-up code.
func (id ID) Send(sel SEL, args ...uintptr) uintptr { return Send(id, sel, args...) }
func (id ID) Get(sel SEL) uintptr                   { return send0(id, sel) }
func (id ID) Bool(sel SEL) bool                     { return send0(id, sel)&0xff != 0 }
func (id ID) Index(i int) ID                        { return ID(send1(id, selObjectAtIndexedSubscript, uintptr(i))) }
func (id ID) Count() int                            { return int(send0(id, selCount)) }
