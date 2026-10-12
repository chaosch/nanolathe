package battlegpu

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// This pass-local state records only explicit prototype Set calls. It never
// reorders draws. All pipelines sharing a group use the same group-0 layout;
// changing groups rebinds every resource after selecting the new pipeline.
// Only the index buffer bound through selectIndex is cached; no vertex or
// dynamic-offset state is. Reset at each world/final pass.
type battleRenderState struct {
	pass     *wgpu.RenderPassEncoder
	pipeline *wgpu.RenderPipeline
	group    *wgpu.BindGroup
	index    *wgpu.Buffer
	disabled bool
}

func (s *battleRenderState) reset(pass *wgpu.RenderPassEncoder) {
	s.pass, s.pipeline, s.group, s.index = pass, nil, nil, nil
}

// selectIndex binds a uint32 index buffer unless this pass already holds it.
func (s *battleRenderState) selectIndex(pass *wgpu.RenderPassEncoder, buffer *wgpu.Buffer) {
	if s != nil && !s.disabled && s.pass == pass && s.index == buffer {
		return
	}
	pass.SetIndexBuffer(buffer, gputypes.IndexFormatUint32, 0)
	if s != nil {
		s.index = buffer
	}
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
