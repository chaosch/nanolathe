package battlegpu

import (
	_ "embed"
	"strconv"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed modelcommit.wgsl
var modelCommitShader string

// The standalone shader files remain the canonical equations for each commit.
// Change only their entry signatures and uniform names when making them callable
// by the shared dispatcher. This keeps ordinary coverage (§17/§22) and shadow
// filtering (§34) independent of the pipeline allocation change.
func modelCommitBodyHelpers() string {
	_, tail, _ := strings.Cut(modelResolveShader, "const corners=")
	return strings.NewReplacer(
		"@vertex fn resolve_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)", "fn modelCommitBodyVertex(vid:u32,iid:u32)",
		"@fragment fn resolve_fragment(input:Out)->@location(0) vec4<f32>", "fn modelCommitBodyFragment(input:Out)->vec4<f32>",
		"u.", "mainU.",
	).Replace("const corners=" + tail)
}

func modelCommitShadowHelpers() string {
	_, tail, _ := strings.Cut(modelShadowCommitShader, "const modelShadowCorners=")
	return strings.NewReplacer(
		"@vertex fn model_shadow_commit_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)", "fn modelCommitShadowVertex(vid:u32,iid:u32)",
		"@fragment fn model_shadow_commit_fragment(input:ModelShadowCommitOut)->@location(0) vec4<f32>", "fn modelCommitShadowFragment(input:ModelShadowCommitOut)->vec4<f32>",
		"u.", "shadowU.",
	).Replace("const modelShadowCorners=" + tail)
}

func modelCommitShaderSource(capacities [2]int, runtime ...bool) string {
	source := strings.NewReplacer(
		"__SLOTS__", strconv.Itoa(capacities[0]), "__SUBJECTS__", strconv.Itoa(capacities[1]),
		"__BODY_HELPERS__", modelCommitBodyHelpers(), "__SHADOW_HELPERS__", modelCommitShadowHelpers(),
	).Replace(modelCommitShader)
	if len(runtime) > 0 && runtime[0] {
		source = runtimeStorageArrays(source)
		source = strings.Replace(source, "const modelCommitSlotCapacity:u32="+strconv.Itoa(capacities[0])+"u;", "__RUNTIME_CAPACITY_FUNCTION__", 1)
		source = strings.ReplaceAll(source, "modelCommitSlotCapacity", "modelCommitSlotCapacity()")
		source = strings.Replace(source, "__RUNTIME_CAPACITY_FUNCTION__", "fn modelCommitSlotCapacity()->u32{return arrayLength(&slots);}", 1)
	}
	return source
}

func modelCommitCapacities(slotBytes, subjectBytes uint64) [2]int {
	return [2]int{modelResolveCapacity(slotBytes), max(1, int(subjectBytes/modelShadowStride))}
}

func modelCommitShadowIndex(slotCapacity int, subject uint32) uint32 {
	return uint32(slotCapacity) + subject
}

// One current module/pipeline pair covers both world commits. Prepare runs only
// after completion, so successful replacement releases the obsolete pair now.
type modelCommitPipeline struct {
	module     *wgpu.ShaderModule
	pipeline   *wgpu.RenderPipeline
	capacities [2]int
	releases   [2]modelRelease // module, then its dependent pipeline
}

func (p *modelCommitPipeline) modelClose() {
	for i := len(p.releases) - 1; i >= 0; i-- {
		if p.releases[i] != nil {
			p.releases[i]()
		}
	}
	*p = modelCommitPipeline{}
}

func (p *modelCommitPipeline) modelReplace(build func(*modelCommitPipeline) error) error {
	var next modelCommitPipeline
	if err := build(&next); err != nil {
		next.modelClose()
		return err
	}
	old := *p
	*p = next
	old.modelClose()
	return nil
}

func (m *Models) modelPrepareCommitPipeline() error {
	capacities := modelCommitCapacities(m.slots.size, m.shadowRecords.size)
	if m.commitWork.module != nil && (m.runtimeArrays || capacities == m.commitWork.capacities) {
		m.commitWork.capacities = capacities
		return nil
	}
	if err := m.commitWork.modelReplace(func(next *modelCommitPipeline) error {
		next.capacities = capacities
		var err error
		next.module, err = m.d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: modelCommitShaderSource(capacities, m.runtimeArrays)})
		if next.module != nil {
			next.releases[0] = next.module.Release
		}
		if err != nil {
			return err
		}
		next.pipeline, err = m.modelCreatePipeline(next.module, m.resolvePipelineLayout, "model_commit_vertex", "model_commit_fragment", m.format, 2, true, gputypes.PrimitiveTopologyTriangleList)
		if next.pipeline != nil {
			next.releases[1] = next.pipeline.Release
		}
		return err
	}); err != nil {
		return err
	}
	m.resolvePipeline = m.commitWork.pipeline
	return nil
}
