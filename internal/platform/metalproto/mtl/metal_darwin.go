//go:build darwin && arm64

package mtl

import "unsafe"

// Metal enumerations used by the renderer.
const (
	PixelFormatR8Unorm      = 10
	PixelFormatRGBA8Unorm   = 70
	PixelFormatBGRA8Unorm   = 80
	PixelFormatRGBA16Float  = 115
	PixelFormatR32Uint      = 53
	PixelFormatR32Float     = 55
	PixelFormatDepth32Float = 252

	StorageModeShared  = 0
	StorageModePrivate = 2
	ResourceShared     = StorageModeShared << 4
	ResourcePrivate    = StorageModePrivate << 4

	UsageShaderRead   = 1
	UsageShaderWrite  = 2
	UsageRenderTarget = 4

	LoadDontCare  = 0
	LoadLoad      = 1
	LoadClear     = 2
	StoreDontCare = 0
	StoreStore    = 1

	BlendZero                   = 0
	BlendOne                    = 1
	BlendSourceColor            = 2
	BlendOneMinusSourceColor    = 3
	BlendOneMinusSourceAlpha    = 5
	BlendOperationMax           = 4
	ColorWriteNone              = 0
	CompareAlways               = 7
	CompareLessEqual            = 3
	CullNone                    = 0
	CullBack                    = 2
	WindingClockwise            = 0
	PrimitiveLine               = 1
	PrimitiveTriangle           = 3
	IndexUInt32                 = 1
	DispatchSerial              = 0
	DispatchConcurrent          = 1
	BarrierBuffers              = 1
	BarrierTextures             = 2
	DataTypeBool                = 53
	CommandBufferStatusComplete = 4
	CommandBufferStatusError    = 5
	ReadWriteTextureTier2       = 2
)

// Buffer is an MTLBuffer with its contents pointer and length cached.
type Buffer struct {
	ID   ID
	Ptr  unsafe.Pointer
	Size int
}

// Bytes views the first n bytes of the buffer.
func (b *Buffer) Bytes(n int) []byte { return unsafe.Slice((*byte)(b.Ptr), n) }

// Texture is an MTLTexture with the properties the renderer reads cached.
type Texture struct {
	ID            ID
	Width, Height int
	Format, Usage uint
}

// Device is an MTLDevice.
type Device ID

func (d Device) NewCommandQueue() ID { return ID(send0(ID(d), selNewCommandQueue)) }

// NewBuffer returns a +1 shared or private buffer, or nil.
func (d Device) NewBuffer(length int, options uint) *Buffer {
	id := ID(send2(ID(d), selNewBufferWithLength, uintptr(length), uintptr(options)))
	if id == 0 {
		return nil
	}
	b := &Buffer{ID: id, Size: length}
	if options&(3<<4) != ResourcePrivate {
		b.Ptr = CPtr(send0(id, selContents))
	}
	return b
}

// NewTexture2D returns a +1 two-dimensional texture, or nil.
func (d Device) NewTexture2D(format uint, width, height int, storage, usage uint) *Texture {
	desc := ID(Send(GetClass("MTLTextureDescriptor"), selTexture2DDescriptor, uintptr(format), uintptr(width), uintptr(height), 0))
	send1(desc, selSetStorageMode, uintptr(storage))
	send1(desc, selSetUsage, uintptr(usage))
	id := ID(send1(ID(d), selNewTextureWithDescriptor, uintptr(desc)))
	if id == 0 {
		return nil
	}
	return &Texture{ID: id, Width: width, Height: height, Format: format, Usage: usage}
}

// Region is MTLRegion.
type Region struct{ X, Y, Z, W, H, D uintptr }

// Replace copies bytes into a shared texture region.
func (t *Texture) Replace(x, y, w, h int, bytes unsafe.Pointer, rowBytes int) {
	r := Region{uintptr(x), uintptr(y), 0, uintptr(w), uintptr(h), 1}
	Send(t.ID, selReplaceRegion, uintptr(unsafe.Pointer(&r)), 0, uintptr(bytes), uintptr(rowBytes))
}

// NewLibrary compiles Metal source at run time.
func (d Device) NewLibrary(source string) (ID, string) {
	var err ID
	src := String(source)
	lib := ID(SendBlocking(ID(d), selNewLibraryWithSource, uintptr(src), 0, uintptr(unsafe.Pointer(&err))))
	if lib == 0 {
		return 0, ErrorText(err)
	}
	return lib, ""
}

// Function returns a +1 library function, or zero.
func Function(lib ID, name string) ID {
	return ID(send1(lib, selNewFunctionWithName, uintptr(String(name))))
}

// FunctionBoolConstant specializes a function on bool constant 0.
func FunctionBoolConstant(lib ID, name string, value bool) (ID, string) {
	values := New("MTLFunctionConstantValues")
	defer Release(values)
	v := uint8(0)
	if value {
		v = 1
	}
	send3(values, selSetConstantValue, uintptr(unsafe.Pointer(&v)), DataTypeBool, 0)
	var err ID
	fn := ID(SendBlocking(lib, selNewFunctionWithNameConstants, uintptr(String(name)), uintptr(values), uintptr(unsafe.Pointer(&err))))
	return fn, ErrorText(err)
}

// RenderPipeline compiles a render pipeline descriptor.
func (d Device) RenderPipeline(desc ID) (ID, string) {
	var err ID
	p := ID(SendBlocking(ID(d), selNewRenderPipelineState, uintptr(desc), uintptr(unsafe.Pointer(&err))))
	return p, ErrorText(err)
}

// ComputePipeline compiles a compute function; fn may be zero.
func (d Device) ComputePipeline(fn ID) (ID, string) {
	if fn == 0 {
		return 0, "missing compute function"
	}
	var err ID
	p := ID(SendBlocking(ID(d), selNewComputePipelineState, uintptr(fn), uintptr(unsafe.Pointer(&err))))
	return p, ErrorText(err)
}

// DepthState creates a depth-stencil state.
func (d Device) DepthState(compare uint, write bool) ID {
	desc := New("MTLDepthStencilDescriptor")
	defer Release(desc)
	send1(desc, selSetDepthCompareFunction, uintptr(compare))
	send1(desc, selSetDepthWriteEnabled, boolArg(write))
	return ID(send1(ID(d), selNewDepthStencilState, uintptr(desc)))
}

func (d Device) MaxBufferLength() int         { return int(send0(ID(d), selMaxBufferLength)) }
func (d Device) CurrentAllocatedSize() uint64 { return uint64(send0(ID(d), selCurrentAllocatedSize)) }
func (d Device) RecommendedWorkingSet() uint64 {
	return uint64(send0(ID(d), selRecommendedMaxWorkingSetSize))
}
func (d Device) UnifiedMemory() bool       { return send0(ID(d), selHasUnifiedMemory)&0xff != 0 }
func (d Device) ReadWriteTextureTier() int { return int(send0(ID(d), selReadWriteTextureSupport)) }
func (d Device) Name() string              { return GoString(ID(send0(ID(d), selName))) }

// MaxThreads is a compute pipeline's maxTotalThreadsPerThreadgroup.
func MaxThreads(pipeline ID) int { return int(send0(pipeline, selMaxTotalThreadsPerThreadgroup)) }

// PipelineDescriptor is an MTLRenderPipelineDescriptor under construction.
type PipelineDescriptor ID

func NewPipelineDescriptor() PipelineDescriptor {
	return PipelineDescriptor(New("MTLRenderPipelineDescriptor"))
}
func (p PipelineDescriptor) Release()          { Release(ID(p)) }
func (p PipelineDescriptor) SetVertex(fn ID)   { send1(ID(p), selSetVertexFunction, uintptr(fn)) }
func (p PipelineDescriptor) SetFragment(fn ID) { send1(ID(p), selSetFragmentFunction, uintptr(fn)) }
func (p PipelineDescriptor) SetDepthFormat(f uint) {
	send1(ID(p), selSetDepthAttachmentPixelFormat, uintptr(f))
}

// Attachment returns colour attachment i's descriptor.
func (p PipelineDescriptor) Attachment(i int) Attachment {
	return Attachment(send1(ID(send0(ID(p), selColorAttachments)), selObjectAtIndexedSubscript, uintptr(i)))
}

// Attachment is an MTLRenderPipelineColorAttachmentDescriptor.
type Attachment ID

func (a Attachment) Format(f uint)         { send1(ID(a), selSetPixelFormat, uintptr(f)) }
func (a Attachment) Blending(on bool)      { send1(ID(a), selSetBlendingEnabled, boolArg(on)) }
func (a Attachment) SourceRGB(f uint)      { send1(ID(a), selSetSourceRGBBlendFactor, uintptr(f)) }
func (a Attachment) DestRGB(f uint)        { send1(ID(a), selSetDestinationRGBBlendFactor, uintptr(f)) }
func (a Attachment) SourceAlpha(f uint)    { send1(ID(a), selSetSourceAlphaBlendFactor, uintptr(f)) }
func (a Attachment) DestAlpha(f uint)      { send1(ID(a), selSetDestinationAlphaBlendFactor, uintptr(f)) }
func (a Attachment) RGBOperation(o uint)   { send1(ID(a), selSetRgbBlendOperation, uintptr(o)) }
func (a Attachment) AlphaOperation(o uint) { send1(ID(a), selSetAlphaBlendOperation, uintptr(o)) }
func (a Attachment) WriteMask(m uint)      { send1(ID(a), selSetWriteMask, uintptr(m)) }

// Queue is an MTLCommandQueue.
type Queue ID

// CommandBuffer returns an autoreleased command buffer.
func (q Queue) CommandBuffer() CommandBuffer { return CommandBuffer(send0(ID(q), selCommandBuffer)) }

// CommandBuffer is an MTLCommandBuffer.
type CommandBuffer ID

func (c CommandBuffer) Render(pass PassDescriptor) RenderEncoder {
	return RenderEncoder(send1(ID(c), selRenderCommandEncoderWithDescriptor, uintptr(pass)))
}
func (c CommandBuffer) Compute(dispatch uint) ComputeEncoder {
	return ComputeEncoder(send1(ID(c), selComputeCommandEncoderWithDispatchType, uintptr(dispatch)))
}
func (c CommandBuffer) Blit() BlitEncoder     { return BlitEncoder(send0(ID(c), selBlitCommandEncoder)) }
func (c CommandBuffer) Present(drawable ID)   { send1(ID(c), selPresentDrawable, uintptr(drawable)) }
func (c CommandBuffer) Commit()               { SendBlocking(ID(c), selCommit) }
func (c CommandBuffer) WaitUntilCompleted()   { SendBlocking(ID(c), selWaitUntilCompleted) }
func (c CommandBuffer) Status() int           { return int(send0(ID(c), selStatus)) }
func (c CommandBuffer) ErrorText() string     { return ErrorText(ID(send0(ID(c), selError))) }
func (c CommandBuffer) GPUStartTime() float64 { return SendDouble(ID(c), selGPUStartTime) }
func (c CommandBuffer) GPUEndTime() float64   { return SendDouble(ID(c), selGPUEndTime) }

// PassDescriptor is an MTLRenderPassDescriptor.
type PassDescriptor ID

var classRenderPassDescriptor ID

// NewPass returns an autoreleased empty render pass descriptor.
func NewPass() PassDescriptor {
	if classRenderPassDescriptor == 0 {
		classRenderPassDescriptor = GetClass("MTLRenderPassDescriptor")
	}
	return PassDescriptor(send0(classRenderPassDescriptor, selRenderPassDescriptor))
}

// Color configures colour attachment i.
func (p PassDescriptor) Color(i int, texture *Texture, load, store uint) PassAttachment {
	a := PassAttachment(send1(ID(send0(ID(p), selColorAttachments)), selObjectAtIndexedSubscript, uintptr(i)))
	a.set(texture, load, store)
	return a
}

// Depth configures the depth attachment.
func (p PassDescriptor) Depth(texture *Texture, load, store uint, clear float64) {
	a := PassAttachment(send0(ID(p), selDepthAttachment))
	a.set(texture, load, store)
	c := SendCall(ID(a), selSetClearDepth)
	c.SetDouble(0, clear)
	c.Do()
}
func (p PassDescriptor) TargetSize(w, h int) {
	send1(ID(p), selSetRenderTargetWidth, uintptr(w))
	send1(ID(p), selSetRenderTargetHeight, uintptr(h))
}

// PassAttachment is an MTLRenderPassAttachmentDescriptor.
type PassAttachment ID

func (a PassAttachment) set(texture *Texture, load, store uint) {
	var t ID
	if texture != nil {
		t = texture.ID
	}
	send1(ID(a), selSetTexture, uintptr(t))
	send1(ID(a), selSetLoadAction, uintptr(load))
	send1(ID(a), selSetStoreAction, uintptr(store))
}

// Clear sets the clear colour.
func (a PassAttachment) Clear(r, g, b, alpha float64) {
	c := SendCall(ID(a), selSetClearColor)
	c.SetDouble(0, r)
	c.SetDouble(1, g)
	c.SetDouble(2, b)
	c.SetDouble(3, alpha)
	c.Do()
}

// Viewport is MTLViewport.
type Viewport struct{ X, Y, W, H, Near, Far float64 }

// RenderEncoder is an MTLRenderCommandEncoder.
type RenderEncoder ID

func (e RenderEncoder) Pipeline(p ID)   { send1(ID(e), selSetRenderPipelineState, uintptr(p)) }
func (e RenderEncoder) DepthState(s ID) { send1(ID(e), selSetDepthStencilState, uintptr(s)) }
func (e RenderEncoder) Cull(mode uint)  { send1(ID(e), selSetCullMode, uintptr(mode)) }
func (e RenderEncoder) Winding(w uint)  { send1(ID(e), selSetFrontFacingWinding, uintptr(w)) }
func (e RenderEncoder) End()            { send0(ID(e), selEndEncoding) }
func (e RenderEncoder) Viewport(v Viewport) {
	send1(ID(e), selSetViewport, uintptr(unsafe.Pointer(&v)))
}
func (e RenderEncoder) VertexBuffer(b *Buffer, offset, index int) {
	send3(ID(e), selSetVertexBuffer, uintptr(bufferID(b)), uintptr(offset), uintptr(index))
}
func (e RenderEncoder) FragmentBuffer(b *Buffer, offset, index int) {
	send3(ID(e), selSetFragmentBuffer, uintptr(bufferID(b)), uintptr(offset), uintptr(index))
}
func (e RenderEncoder) VertexBytes(p unsafe.Pointer, n, index int) {
	send3(ID(e), selSetVertexBytes, uintptr(p), uintptr(n), uintptr(index))
}
func (e RenderEncoder) FragmentBytes(p unsafe.Pointer, n, index int) {
	send3(ID(e), selSetFragmentBytes, uintptr(p), uintptr(n), uintptr(index))
}
func (e RenderEncoder) VertexTexture(t *Texture, index int) {
	send2(ID(e), selSetVertexTexture, uintptr(textureID(t)), uintptr(index))
}
func (e RenderEncoder) FragmentTexture(t *Texture, index int) {
	send2(ID(e), selSetFragmentTexture, uintptr(textureID(t)), uintptr(index))
}
func (e RenderEncoder) Draw(primitive uint, start, count int) {
	Send(ID(e), selDrawPrimitives, uintptr(primitive), uintptr(start), uintptr(count))
}
func (e RenderEncoder) DrawInstanced(primitive uint, start, count, instances int) {
	Send(ID(e), selDrawPrimitivesInstanced, uintptr(primitive), uintptr(start), uintptr(count), uintptr(instances))
}
func (e RenderEncoder) DrawBase(primitive uint, start, count, instances, base int) {
	Send(ID(e), selDrawPrimitivesBase, uintptr(primitive), uintptr(start), uintptr(count), uintptr(instances), uintptr(base))
}
func (e RenderEncoder) DrawIndexed(primitive uint, count int, indices *Buffer, offset int) {
	Send(ID(e), selDrawIndexed, uintptr(primitive), uintptr(count), IndexUInt32, uintptr(bufferID(indices)), uintptr(offset))
}
func (e RenderEncoder) DrawIndexedInstanced(primitive uint, count int, indices *Buffer, offset, instances int) {
	Send(ID(e), selDrawIndexedInstanced, uintptr(primitive), uintptr(count), IndexUInt32, uintptr(bufferID(indices)), uintptr(offset), uintptr(instances))
}

// ComputeEncoder is an MTLComputeCommandEncoder.
type ComputeEncoder ID

func (e ComputeEncoder) Pipeline(p ID) { send1(ID(e), selSetComputePipelineState, uintptr(p)) }
func (e ComputeEncoder) Buffer(b *Buffer, offset, index int) {
	send3(ID(e), selSetBuffer, uintptr(bufferID(b)), uintptr(offset), uintptr(index))
}
func (e ComputeEncoder) Bytes(p unsafe.Pointer, n, index int) {
	send3(ID(e), selSetBytes, uintptr(p), uintptr(n), uintptr(index))
}
func (e ComputeEncoder) Texture(t *Texture, index int) {
	send2(ID(e), selSetComputeTexture, uintptr(textureID(t)), uintptr(index))
}
func (e ComputeEncoder) DispatchThreads(w, h, d, tw, th, td int) {
	threads := [3]uintptr{uintptr(w), uintptr(h), uintptr(d)}
	group := [3]uintptr{uintptr(tw), uintptr(th), uintptr(td)}
	send2(ID(e), selDispatchThreads, uintptr(unsafe.Pointer(&threads)), uintptr(unsafe.Pointer(&group)))
}
func (e ComputeEncoder) DispatchGroups(w, h, d, tw, th, td int) {
	groups := [3]uintptr{uintptr(w), uintptr(h), uintptr(d)}
	threads := [3]uintptr{uintptr(tw), uintptr(th), uintptr(td)}
	send2(ID(e), selDispatchThreadgroups, uintptr(unsafe.Pointer(&groups)), uintptr(unsafe.Pointer(&threads)))
}
func (e ComputeEncoder) Barrier(scope uint) { send1(ID(e), selMemoryBarrierWithScope, uintptr(scope)) }
func (e ComputeEncoder) End()               { send0(ID(e), selEndEncoding) }

// BlitEncoder is an MTLBlitCommandEncoder.
type BlitEncoder ID

func (e BlitEncoder) End() { send0(ID(e), selEndEncoding) }

// Fill sets bytes [offset, offset+n) of b to value.
func (e BlitEncoder) Fill(b *Buffer, offset, n int, value uint8) {
	Send(ID(e), selFillBuffer, uintptr(bufferID(b)), uintptr(offset), uintptr(n), uintptr(value))
}

// CopyBufferToTexture copies rows from a staging buffer into a texture region.
func (e BlitEncoder) CopyBufferToTexture(src *Buffer, offset, rowBytes, imageBytes, w, h int, dst *Texture, x, y int) {
	var a struct {
		c            Call
		size, origin [3]uintptr
	}
	a.size = [3]uintptr{uintptr(w), uintptr(h), 1}
	a.origin = [3]uintptr{uintptr(x), uintptr(y), 0}
	a.c = SendCall(ID(e), selCopyBufferToTexture)
	a.c.X[2], a.c.X[3], a.c.X[4], a.c.X[5] = uintptr(bufferID(src)), uintptr(offset), uintptr(rowBytes), uintptr(imageBytes)
	a.c.X[6], a.c.X[7] = uintptr(unsafe.Pointer(&a.size)), uintptr(textureID(dst))
	a.c.Stack[0], a.c.Stack[1], a.c.Stack[2] = 0, 0, uintptr(unsafe.Pointer(&a.origin))
	a.c.Do()
}

// CopyTexture copies a region between textures at the same origin offsets.
func (e BlitEncoder) CopyTexture(src *Texture, sx, sy, w, h int, dst *Texture, dx, dy int) {
	var a struct {
		c            Call
		so, size, do [3]uintptr
	}
	a.so = [3]uintptr{uintptr(sx), uintptr(sy), 0}
	a.size = [3]uintptr{uintptr(w), uintptr(h), 1}
	a.do = [3]uintptr{uintptr(dx), uintptr(dy), 0}
	a.c = SendCall(ID(e), selCopyTextureToTexture)
	a.c.X[2], a.c.X[3], a.c.X[4], a.c.X[5] = uintptr(textureID(src)), 0, 0, uintptr(unsafe.Pointer(&a.so))
	a.c.X[6], a.c.X[7] = uintptr(unsafe.Pointer(&a.size)), uintptr(textureID(dst))
	a.c.Stack[0], a.c.Stack[1], a.c.Stack[2] = 0, 0, uintptr(unsafe.Pointer(&a.do))
	a.c.Do()
}

// CopyTextureToBuffer reads a texture region back into a buffer.
func (e BlitEncoder) CopyTextureToBuffer(src *Texture, w, h int, dst *Buffer, offset, rowBytes, imageBytes int) {
	var a struct {
		c            Call
		origin, size [3]uintptr
	}
	a.size = [3]uintptr{uintptr(w), uintptr(h), 1}
	a.c = SendCall(ID(e), selCopyTextureToBuffer)
	a.c.X[2], a.c.X[3], a.c.X[4], a.c.X[5] = uintptr(textureID(src)), 0, 0, uintptr(unsafe.Pointer(&a.origin))
	a.c.X[6], a.c.X[7] = uintptr(unsafe.Pointer(&a.size)), uintptr(bufferID(dst))
	a.c.Stack[0], a.c.Stack[1], a.c.Stack[2] = uintptr(offset), uintptr(rowBytes), uintptr(imageBytes)
	a.c.Do()
}

func bufferID(b *Buffer) ID {
	if b == nil {
		return 0
	}
	return b.ID
}
func textureID(t *Texture) ID {
	if t == nil {
		return 0
	}
	return t.ID
}
