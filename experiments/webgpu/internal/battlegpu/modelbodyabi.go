package battlegpu

import (
	"strconv"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// The body module also supplies the independent solo-shadow body and outline
// entries. One current set owns all four pipelines and their shared module.
type modelBodyPipelines struct {
	module     *wgpu.ShaderModule
	pipelines  [5]*wgpu.RenderPipeline
	capacities [8]int
	releases   [6]modelRelease // module, then the five dependent pipelines
}

func (p *modelBodyPipelines) modelClose() {
	for i := len(p.releases) - 1; i >= 0; i-- {
		if p.releases[i] != nil {
			p.releases[i]()
		}
	}
	*p = modelBodyPipelines{}
}

func (p *modelBodyPipelines) modelReplace(build func(*modelBodyPipelines) error) error {
	var next modelBodyPipelines
	if err := build(&next); err != nil {
		next.modelClose()
		return err
	}
	// Completion precedes Prepare/Close. Failed creation above preserves every
	// old handle; success releases only the previous current set, in reverse.
	old := *p
	*p = next
	old.modelClose()
	return nil
}

var modelBodyArrays = [8]struct {
	name, element string
	stride        uint64
}{
	{"vertices", "Vertex", modelVertexStride},
	{"poses", "Pose", modelPoseStride},
	{"instances", "Instance", modelInstanceStride},
	{"materials", "Material", 16},
	{"frames", "vec4<f32>", 16},
	{"selected", "Selected", 48},
	{"lights", "Light", 64},
	{"centers", "vec4<f32>", 16},
}

func modelBodyCapacities(bytes [8]uint64) (capacities [8]int) {
	for i, record := range modelBodyArrays {
		// NewModels compiles an initial one-record dynamic array before those
		// buffers exist. Prepare always chooses every full allocated record.
		capacities[i] = max(1, int(bytes[i]/record.stride))
	}
	return
}

func modelBodyShaderSource(capacities [8]int, runtime ...bool) string {
	if len(runtime) > 0 && runtime[0] {
		return runtimeStorageArrays(modelBodyShader)
	}
	var replacements [16]string
	for i, record := range modelBodyArrays {
		prefix := record.name + ":array<" + record.element + ","
		replacements[2*i], replacements[2*i+1] = prefix+"1>", prefix+strconv.Itoa(capacities[i])+">"
	}
	return strings.NewReplacer(replacements[:]...).Replace(modelBodyShader)
}

func modelBodyLayoutEntries() []gputypes.BindGroupLayoutEntry {
	v, vf := wgpu.ShaderStageVertex, wgpu.ShaderStageVertex|wgpu.ShaderStageFragment
	// Zero storage minima defer capacity validation to the actual bound buffer.
	// Declaration order and stage visibility retain the existing native slots.
	return []gputypes.BindGroupLayoutEntry{
		modelBufferEntry(0, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(1, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(2, vf, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(3, vf, gputypes.BufferBindingTypeUniform, 80),
		modelTextureEntry(4), modelTextureEntry(5),
		modelBufferEntry(6, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(7, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(8, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		modelBufferEntry(9, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
		{Binding: 10, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
		modelBufferEntry(11, v, gputypes.BufferBindingTypeReadOnlyStorage, 0),
	}
}

func (m *Models) modelPrepareBodyPipelines() error {
	capacities := modelBodyCapacities([8]uint64{m.vertices.Size(), m.poses.size, m.instances.size, m.materialData.Size(), m.frameData.Size(), m.selected.size, m.lights.size, m.centers.Size()})
	if m.bodyWork.module != nil && (m.runtimeArrays || capacities == m.bodyWork.capacities) {
		return nil
	}
	err := m.bodyWork.modelReplace(func(next *modelBodyPipelines) error {
		next.capacities = capacities
		var err error
		next.module, err = m.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: modelBodyShaderSource(capacities, m.runtimeArrays)})
		if next.module != nil {
			next.releases[0] = next.module.Release
		}
		if err != nil {
			return err
		}
		for i, fragment := range [...]string{"model_fragment", "outline_fragment", "model_shadow_fragment", "model_shadow_outline"} {
			topology := gputypes.PrimitiveTopologyTriangleList
			if i%2 != 0 {
				topology = gputypes.PrimitiveTopologyLineList
			}
			next.pipelines[i], err = m.modelCreatePipeline(next.module, m.bodyPipelineLayout, "model_vertex", fragment, wgpu.TextureFormatRGBA8Unorm, 1, false, topology)
			if next.pipelines[i] != nil {
				next.releases[i+1] = next.pipelines[i].Release
			}
			if err != nil {
				return err
			}
		}
		// Direct subjects draw into the world pass and are its only depth
		// writers: banded by paint rank, so a later subject always covers an
		// earlier one, and each subject's own faces resolve as on the atlas.
		next.pipelines[4], err = m.modelCreatePipeline(next.module, m.bodyPipelineLayout, "model_direct_vertex", "model_direct_fragment", m.format, 1, true, gputypes.PrimitiveTopologyTriangleList)
		if next.pipelines[4] != nil {
			next.releases[5] = next.pipelines[4].Release
		}
		return err
	})
	if err != nil {
		return err
	}
	m.directPipeline = m.bodyWork.pipelines[4]
	m.bodyPipeline, m.outlinePipeline = m.bodyWork.pipelines[0], m.bodyWork.pipelines[1]
	m.shadowWork.bodyPipeline, m.shadowWork.outlinePipeline = m.bodyWork.pipelines[2], m.bodyWork.pipelines[3]
	return nil
}
