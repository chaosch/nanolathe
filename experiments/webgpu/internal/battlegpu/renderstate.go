package battlegpu

import "github.com/gogpu/wgpu"

// This pass-local state records only explicit prototype Set calls. It never
// reorders draws. All pipelines sharing a group use the same group-0 layout;
// changing groups rebinds every resource after selecting the new pipeline.
// No index/vertex/dynamic-offset state is cached. Reset at each world/final pass.
type battleRenderState struct {
	pass     *wgpu.RenderPassEncoder
	pipeline *wgpu.RenderPipeline
	group    *wgpu.BindGroup
	disabled bool
}

func (s *battleRenderState) reset(pass *wgpu.RenderPassEncoder) {
	s.pass, s.pipeline, s.group = pass, nil, nil
}
func (s *battleRenderState) selectResources(pass *wgpu.RenderPassEncoder, pipeline *wgpu.RenderPipeline, group *wgpu.BindGroup) {
	if s == nil || s.disabled {
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, group, nil)
		return
	}
	if s.pass != pass {
		s.reset(pass)
	}
	if s.pipeline != pipeline {
		pass.SetPipeline(pipeline)
		s.pipeline = pipeline
	}
	if s.group != group {
		pass.SetBindGroup(0, group, nil)
		s.group = group
	}
}
