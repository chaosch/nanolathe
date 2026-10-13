package battlegpu

import (
	_ "embed"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

//go:embed modelair.wgsl
var modelAirShader string

// modelAirStride is one AirSlot record: slot placement, then source origin,
// texel size and dry radius. Records align with the shadow subjects.
const modelAirStride = 32

// modelAirMaxScale caps a slot texel at 16 x 16 source texels.
const modelAirMaxScale = 16

// modelAirWork pre-blurs soft (aircraft) shadow sources once per frame: a
// horizontal pass into across and a vertical pass into blurred, which the
// world commit samples once per pixel (modelair.wgsl).
type modelAirWork struct {
	across, blurred              *wgpu.Texture
	acrossView, blurredView      *wgpu.TextureView
	width, height, shrink        int
	needW, needH                 int
	count                        int
	bytes                        []byte
	items                        []modelAirItem // per-frame scratch, reused
	records                      modelBuffer
	uniform                      *wgpu.Buffer
	layout                       *wgpu.BindGroupLayout
	pipelineLayout               *wgpu.PipelineLayout
	module                       *wgpu.ShaderModule
	acrossPipeline, downPipeline *wgpu.RenderPipeline
	capacities                   [2]int
	acrossGroup, downGroup       *wgpu.BindGroup
	groupViews                   [3]*wgpu.TextureView
}

func (a *modelAirWork) releaseGroups() {
	for _, g := range []*wgpu.BindGroup{a.acrossGroup, a.downGroup} {
		if g != nil {
			g.Release()
		}
	}
	a.acrossGroup, a.downGroup, a.groupViews = nil, nil, [3]*wgpu.TextureView{}
}

func (a *modelAirWork) releaseTextures() {
	for _, v := range []*wgpu.TextureView{a.acrossView, a.blurredView} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{a.across, a.blurred} {
		if t != nil {
			t.Release()
		}
	}
	a.across, a.blurred, a.acrossView, a.blurredView = nil, nil, nil, nil
}

func (a *modelAirWork) releasePipelines() {
	for _, p := range []*wgpu.RenderPipeline{a.acrossPipeline, a.downPipeline} {
		if p != nil {
			p.Release()
		}
	}
	if a.module != nil {
		a.module.Release()
	}
	a.acrossPipeline, a.downPipeline, a.module = nil, nil, nil
}

func (a *modelAirWork) modelClose() {
	a.releaseGroups()
	a.releaseTextures()
	a.releasePipelines()
	*a = modelAirWork{}
}

func modelAirLayoutEntries() []gputypes.BindGroupLayoutEntry {
	vf := wgpu.ShaderStageVertex | wgpu.ShaderStageFragment
	return []gputypes.BindGroupLayoutEntry{
		modelBufferEntry(0, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(1, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(2, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(3, vf, gputypes.BufferBindingTypeUniform, 16),
		modelTextureEntry(4), modelTextureEntry(5), modelTextureEntry(6),
		{Binding: 7, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
	}
}

func modelAirShaderSource(capacities [2]int) string {
	return strings.NewReplacer("__SLOTS__", strconv.Itoa(capacities[0]), "__SUBJECTS__", strconv.Itoa(capacities[1])).Replace(modelAirShader)
}

// modelAirScale is the slot texel size in source texels: the largest power of
// two not above the radius, so the blur spans one to two slot texels.
func modelAirScale(radius float64) float64 {
	d := 1.0
	for d*2 <= radius && d < modelAirMaxScale {
		d *= 2
	}
	return d
}

// modelAirItem is one soft subject's air slot before shelf placement.
type modelAirItem struct {
	subject   int
	w, h      int
	x0, y0, d float64
	radius    float64
}

// modelPrepareAir places every soft subject's blurred source in the air atlas
// and records its mapping. Run after the subjects and model slots are final.
func (m *Models) modelPrepareAir(c meshscene.ShadowComposition) error {
	a := &m.air
	a.count, a.needW, a.needH = 0, 0, 0
	a.bytes = modelBytes(a.bytes, max(modelAirStride, len(c.Subjects)*modelAirStride))
	items := a.items[:0]
	defer func() { a.items = items[:0] }()
	for si, s := range c.Subjects {
		if s.Params[0] < 2.5 {
			continue
		}
		rect, raster := s.Body, max(c.Tint[3], 1)
		if s.Body == [4]float32{} {
			slot := m.packedSlots[s.Order[0]-1]
			rect, raster = slot.Atlas, slot.Atlas[2]/max(slot.Screen[2], 1)
		}
		radius := float64(s.Params[2]) * 0.5 * float64(raster)
		d := modelAirScale(radius)
		// The binomial kernel reaches two radii; one more texel keeps the
		// slot's border at zero coverage.
		reach := 2*radius + 1
		x0, y0 := math.Floor(float64(rect[0])-reach), math.Floor(float64(rect[1])-reach)
		x1, y1 := math.Ceil(float64(rect[0]+rect[2])+reach), math.Ceil(float64(rect[1]+rect[3])+reach)
		w, h := int(math.Ceil((x1-x0)/d)), int(math.Ceil((y1-y0)/d))
		if w < 1 || h < 1 || w > 8190 || h > 8190 {
			return modelError("soft shadow sources within the 8192-texel air atlas")
		}
		items = append(items, modelAirItem{si, w, h, x0, y0, d, radius})
	}
	if len(items) == 0 {
		return nil
	}
	// Shelves with a one-texel cleared gap around every slot, so bilinear
	// samples at a slot's edge never read a neighbour.
	width := 1024
	for _, it := range items {
		for it.w+2 > width {
			width *= 2
		}
	}
	x, y, row := 1, 1, 0
	for _, it := range items {
		if x+it.w+1 > width {
			x, y, row = 1, y+row+1, 0
		}
		at := it.subject * modelAirStride
		modelVec(a.bytes, at, [4]float32{float32(x), float32(y), float32(it.w), float32(it.h)})
		modelVec(a.bytes, at+16, [4]float32{float32(it.x0), float32(it.y0), float32(it.d), float32(it.radius)})
		x += it.w + 1
		row = max(row, it.h)
	}
	a.count, a.needW, a.needH = len(items), width, y+row+1
	m.counters.AirSubjects = len(items)
	for _, it := range items {
		m.counters.AirTexels += it.w * it.h
	}
	if a.needH > 8192 {
		return modelError("soft shadow sources within the 8192-texel air atlas")
	}
	return nil
}

// modelPrepareAirGPU sizes the records, atlas, pipelines and groups. The
// records cover the subject capacity the fixed shader arrays declare.
func (m *Models) modelPrepareAirGPU(f meshscene.LiveFrame, changed bool) error {
	a := &m.air
	subjects := max(1, int(m.shadowRecords.size/modelShadowStride))
	grown, err := m.modelGrow(&a.records, "battle-shadow-air-records", max(len(a.bytes), subjects*modelAirStride), wgpu.BufferUsageStorage)
	if err != nil {
		return err
	}
	if grown || changed {
		a.releaseGroups()
		m.modelReleaseCommitGroup()
	}
	if err = m.writeMutableBuffer(a.records.buffer, a.bytes); err != nil {
		return err
	}
	m.counters.UploadBytes += len(a.bytes)
	if a.count == 0 {
		return nil
	}
	nw, nh, change := modelAtlasFit(a.needW, a.needH, a.width, a.height, a.blurredView != nil, &a.shrink)
	if change {
		a.releaseGroups()
		m.modelReleaseCommitGroup()
		a.releaseTextures()
		usage := wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment
		if a.across, a.acrossView, err = m.modelCreateTexture("battle-shadow-air-across", nw, nh, gputypes.TextureFormatR8Unorm, usage); err != nil {
			return err
		}
		if a.blurred, a.blurredView, err = m.modelCreateTexture("battle-shadow-air-blurred", nw, nh, gputypes.TextureFormatR8Unorm, usage); err != nil {
			return err
		}
		a.width, a.height = nw, nh
	}
	if a.uniform == nil {
		if a.uniform, err = m.modelBuffer("battle-shadow-air-uniforms", 16, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst, nil); err != nil {
			return err
		}
	}
	var u [16]byte
	modelVec(u[:], 0, [4]float32{float32(a.width), float32(a.height), max(f.Shadows.Tint[3], 1), 0})
	if err = m.q.WriteBuffer(a.uniform, 0, u[:]); err != nil {
		return err
	}
	if a.layout == nil {
		if a.layout, a.pipelineLayout, err = m.modelLayout(modelAirLayoutEntries()); err != nil {
			return err
		}
	}
	capacities := [2]int{modelResolveCapacity(m.slots.size), subjects}
	if a.module == nil || capacities != a.capacities {
		a.releaseGroups()
		a.releasePipelines()
		if a.module, err = m.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: modelAirShaderSource(capacities)}); err != nil {
			return err
		}
		if a.acrossPipeline, err = m.modelCreatePipeline(a.module, a.pipelineLayout, "air_vertex", "air_across", gputypes.TextureFormatR8Unorm, 0, false, gputypes.PrimitiveTopologyTriangleList); err != nil {
			return err
		}
		if a.downPipeline, err = m.modelCreatePipeline(a.module, a.pipelineLayout, "air_vertex", "air_down", gputypes.TextureFormatR8Unorm, 0, false, gputypes.PrimitiveTopologyTriangleList); err != nil {
			return err
		}
		a.capacities = capacities
	}
	body := m.shadowWork.dummy.bodyView
	if m.shadowWork.atlas.bodyView != nil {
		body = m.shadowWork.atlas.bodyView
	}
	views := [3]*wgpu.TextureView{m.modelBodyView(), body, a.blurredView}
	if views != a.groupViews {
		a.releaseGroups()
	}
	if a.acrossGroup == nil {
		// The horizontal pass renders into across, so its third texture is the
		// (unread) blurred plane; the vertical pass reads across.
		for i, mid := range []*wgpu.TextureView{a.blurredView, a.acrossView} {
			g, e := m.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: a.layout, Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: m.slots.buffer}, {Binding: 1, Buffer: m.shadowRecords.buffer}, {Binding: 2, Buffer: a.records.buffer},
				{Binding: 3, Buffer: a.uniform, Size: 16}, {Binding: 4, TextureView: views[0]}, {Binding: 5, TextureView: views[1]},
				{Binding: 6, TextureView: mid}, {Binding: 7, Sampler: m.shadowSampler},
			}})
			if e != nil {
				return e
			}
			if i == 0 {
				a.acrossGroup = g
			} else {
				a.downGroup = g
			}
		}
		a.groupViews = views
	}
	return nil
}

// modelEncodeAir blurs this frame's soft sources: across, then down.
func (m *Models) modelEncodeAir(enc *wgpu.CommandEncoder) error {
	a := &m.air
	if a.count == 0 || os.Getenv("NANOLATHE_WEBGPU_SKIP_AIR_PASSES") == "1" {
		return nil
	}
	for _, step := range []struct {
		view     *wgpu.TextureView
		pipeline *wgpu.RenderPipeline
		group    *wgpu.BindGroup
	}{{a.acrossView, a.acrossPipeline, a.acrossGroup}, {a.blurredView, a.downPipeline, a.downGroup}} {
		pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: step.view, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
		if err != nil {
			return err
		}
		pass.SetPipeline(step.pipeline)
		pass.SetBindGroup(0, step.group, nil)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(m.shadowCount)})
		m.counters.Draws++
		if err = pass.End(); err != nil {
			return err
		}
	}
	return nil
}
