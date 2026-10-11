module github.com/nanolathe-gg/nanolathe/experiments/webgpu

go 1.27.1

require (
	github.com/gogpu/gogpu v0.54.1-0.20260901095536-12620c9d5007
	github.com/gogpu/gpucontext v0.31.3
	github.com/gogpu/gputypes v0.8.0
	github.com/gogpu/wgpu v0.34.2
	github.com/nanolathe-gg/nanolathe v0.0.0
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/gogpu/naga v0.19.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/nanolathe-gg/nanolathe => ../..
