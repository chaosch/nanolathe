package host

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/render"
	"image"
	"image/png"
	"math"
)

//go:embed points.wgsl
var pointShader string

// Capture owns untimed staging only. MapAsync readiness is actual map completion on all paths.
type Capture struct {
	d                     *wgpu.Device
	points                *wgpu.Buffer
	pointPipeline         *wgpu.ComputePipeline
	pointLayout           *wgpu.BindGroupLayout
	pointGroup            *wgpu.BindGroup
	pointReleases         []func()
	pointSize             uint64
	buffer                *wgpu.Buffer
	pending               *wgpu.MapPending
	final                 *wgpu.Texture
	finalView             *wgpu.TextureView
	format                gputypes.TextureFormat
	size, poseSize, plane uint64
	row, width, height    int
}

func NewCapture(d *wgpu.Device, count, width, height int, format gputypes.TextureFormat) (*Capture, error) {
	c := &Capture{d: d, poseSize: uint64(count * 3 * 64), width: width, height: height, row: (width*4 + 255) / 256 * 256}
	c.plane = uint64(c.row * height)
	c.format = format
	c.pointSize = uint64(count * 3 * 16)
	c.size = c.poseSize + c.plane*4 + c.pointSize
	b, e := d.CreateBuffer(&wgpu.BufferDescriptor{Label: "untimed-diagnostic", Size: c.size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	c.buffer = b
	if e != nil {
		return c, e
	}
	c.final, e = d.CreateTexture(&wgpu.TextureDescriptor{Label: "untimed-final", Size: wgpu.Extent3D{Width: uint32(width), Height: uint32(height), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc})
	if e != nil {
		return c, e
	}
	c.finalView, e = d.CreateTextureView(c.final, nil)
	if e != nil {
		return c, e
	}
	c.points, e = d.CreateBuffer(&wgpu.BufferDescriptor{Label: "untimed-points", Size: c.pointSize, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	if e != nil {
		return c, e
	}
	c.pointReleases = append(c.pointReleases, c.points.Release)
	c.pointLayout, e = d.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage, MinBindingSize: 64}}, {Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage, MinBindingSize: 16}}}})
	if e != nil {
		return c, e
	}
	c.pointReleases = append(c.pointReleases, c.pointLayout.Release)
	layout, e := d.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{c.pointLayout}})
	if e != nil {
		return c, e
	}
	c.pointReleases = append(c.pointReleases, layout.Release)
	shader, e := d.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: pointShader})
	if e != nil {
		return c, e
	}
	c.pointReleases = append(c.pointReleases, shader.Release)
	c.pointPipeline, e = d.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: layout, Module: shader, EntryPoint: "transform"})
	if e == nil {
		c.pointReleases = append(c.pointReleases, c.pointPipeline.Release)
	}
	return c, e
}
func (c *Capture) FinalView() *wgpu.TextureView { return c.finalView }
func (c *Capture) FinalRGBA(data []byte) []byte {
	out := make([]byte, c.width*c.height*4)
	for y := 0; y < c.height; y++ {
		copy(out[y*c.width*4:], data[int(c.poseSize+3*c.plane)+y*c.row:int(c.poseSize+3*c.plane)+y*c.row+c.width*4])
	}
	if c.format == wgpu.TextureFormatBGRA8Unorm {
		for at := 0; at < len(out); at += 4 {
			out[at], out[at+2] = out[at+2], out[at]
		}
	}
	return out
}
func (c *Capture) PNG(data []byte) ([]byte, error) {
	img := &image.RGBA{Pix: c.FinalRGBA(data), Stride: c.width * 4, Rect: image.Rect(0, 0, c.width, c.height)}
	var out bytes.Buffer
	e := png.Encode(&out, img)
	return out.Bytes(), e
}

func (c *Capture) Encode(e *wgpu.CommandEncoder, r *render.Renderer) error {
	poses, color, ids, depth := r.DiagnosticResources()
	if c.pointGroup == nil {
		g, err := c.d.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: c.pointLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: poses, Size: c.poseSize}, {Binding: 1, Buffer: c.points, Size: c.pointSize}}})
		if err != nil {
			return err
		}
		c.pointGroup = g
		c.pointReleases = append(c.pointReleases, g.Release)
	}
	pass, err := e.BeginComputePass(nil)
	if err != nil {
		return err
	}
	pass.SetPipeline(c.pointPipeline)
	pass.SetBindGroup(0, c.pointGroup, nil)
	pass.Dispatch(uint32((c.pointSize/16+63)/64), 1, 1)
	if err := pass.End(); err != nil {
		return err
	}
	e.CopyBufferToBuffer(c.points, 0, c.buffer, c.poseSize+c.plane*4, c.pointSize)
	e.CopyBufferToBuffer(poses, 0, c.buffer, 0, c.poseSize)
	for i, t := range []*wgpu.Texture{color, ids, depth, c.final} {
		aspect := gputypes.TextureAspectAll
		if i == 2 {
			aspect = gputypes.TextureAspectDepthOnly
		}
		old := wgpu.TextureUsageTextureBinding
		if i >= 2 {
			old = wgpu.TextureUsageRenderAttachment
		}
		e.TransitionTextures([]wgpu.TextureBarrier{{Texture: t, Range: wgpu.TextureRange{Aspect: aspect, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: old, NewUsage: wgpu.TextureUsageCopySrc}}})
		e.CopyTextureToBuffer(t, c.buffer, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{Offset: c.poseSize + uint64(i)*c.plane, BytesPerRow: uint32(c.row), RowsPerImage: uint32(c.height)}, TextureBase: wgpu.ImageCopyTexture{Texture: t, Aspect: aspect}, Size: wgpu.Extent3D{Width: uint32(c.width), Height: uint32(c.height), DepthOrArrayLayers: 1}}})
	}
	return nil
}
func (c *Capture) Poll() ([]byte, bool, error) {
	c.d.Poll(wgpu.PollPoll)
	if c.pending == nil {
		p, e := c.buffer.MapAsync(wgpu.MapModeRead, 0, c.size)
		if e != nil {
			return nil, true, e
		}
		c.pending = p
	}
	ready, e := c.pending.Status()
	if !ready || e != nil {
		return nil, ready, e
	}
	view, e := c.buffer.MappedRange(0, c.size)
	if e != nil {
		return nil, true, e
	}
	data := append([]byte(nil), view.Bytes()...)
	view.Release()
	c.buffer.Unmap()
	c.pending.Release()
	c.pending = nil
	return data, true, nil
}
func (c *Capture) Close() {
	for i := len(c.pointReleases) - 1; i >= 0; i-- {
		c.pointReleases[i]()
	}
	c.pointReleases = nil
	if c.finalView != nil {
		c.finalView.Release()
		c.finalView = nil
	}
	if c.final != nil {
		c.final.Release()
		c.final = nil
	}
	if c.pending != nil {
		c.pending.Release()
		c.pending = nil
	}
	if c.buffer != nil {
		c.buffer.Release()
		c.buffer = nil
	}
}

type Comparison struct {
	Alpha                                       float32
	MaxPoseError, MaxPointError                 float64
	MinimumIDPixels, ColorSamples, DepthSamples int
	Resources                                   render.Resources
}

func (c *Capture) Compare(data []byte, w *fixture.Workload, s fixture.Snapshot, alpha float32) (result Comparison, err error) {
	result.Alpha = alpha
	for i := range s.Previous {
		want := fixture.Blend(s.Previous[i], s.Current[i], alpha)
		for row := 0; row < 3; row++ {
			expected := float64(want[row])*.25 + float64(want[4+row])*(-.13) + float64(want[8+row])*.17 + float64(want[12+row])
			offset := int(c.poseSize+c.plane*4) + i*16 + row*4
			got := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[offset:])))
			delta := math.Abs(got - expected)
			result.MaxPointError = max(result.MaxPointError, delta)
			if math.IsNaN(got) || delta > 1e-4 {
				return result, fmt.Errorf("GPU transformed point %d component %d error %.9g", i, row, delta)
			}
		}
		for lane, x := range want {
			got := math.Float32frombits(binary.LittleEndian.Uint32(data[i*64+lane*4:]))
			delta := math.Abs(float64(got) - float64(x))
			result.MaxPoseError = max(result.MaxPoseError, delta)
			if math.IsNaN(float64(got)) || delta > 1e-4 {
				return result, fmt.Errorf("pose %d lane %d got %.9g expected %.9g error %.9g", i, lane, got, x, delta)
			}
		}
	}
	census := make([]int, w.Count+1)
	for y := 0; y < c.height; y++ {
		for x := 0; x < c.width; x++ {
			at := int(c.poseSize+c.plane) + y*c.row + x*4
			id := int(data[at]) | int(data[at+1])<<8 | int(data[at+2])<<16
			if id > w.Count {
				return result, fmt.Errorf("unknown identity %d", id)
			}
			if id > 0 {
				census[id]++
			}
		}
	}
	result.MinimumIDPixels = int(^uint(0) >> 1)
	for id := 1; id <= w.Count; id++ {
		if w.Count == 64 && id == 2 {
			continue
		}
		result.MinimumIDPixels = min(result.MinimumIDPixels, census[id])
	}
	if w.Count != 64 && result.MinimumIDPixels < 16 {
		return result, fmt.Errorf("minimum visible identity pixels %d < 16", result.MinimumIDPixels)
	}
	// Exact nearest color and depth checks at predetermined screen locations in the authored 64-instance diagnostic.
	final := c.FinalRGBA(data)
	for _, p := range []struct {
		x, y int
		rgba [4]byte
	}{{16, 16, [4]byte{0, 255, 255, 255}}, {0, 0, [4]byte{5, 8, 10, 255}}} {
		at := (p.y*c.width + p.x) * 4
		if [4]byte(final[at:at+4]) != p.rgba {
			return result, fmt.Errorf("composite/overlay %d,%d got %v want %v", p.x, p.y, final[at:at+4], p.rgba)
		}
	}
	if w.Count == 64 {
		oracle := fixture.Raster(w, s, alpha)
		var points [][2]int
		for y := 70; y < 1080; y += 83 {
			for x := 70; x < 1920; x += 107 {
				points = append(points, [2]int{x, y})
			}
		}
		nonBackground := 0
		for _, p := range points {
			x, y := p[0]*c.width/1920, p[1]*c.height/1080
			sample := oracle.Sample(x, y)
			if !sample.Interior || sample.DepthAmbiguous {
				continue
			}
			at := int(c.poseSize) + y*c.row + x*4
			got := [4]byte(data[at : at+4])
			if got != sample.RGBA {
				return result, fmt.Errorf("pixel %d,%d got %v expected %v id %d", x, y, got, sample.RGBA, sample.ID)
			}
			fat := (y*c.width + x) * 4
			wantFinal := sample.RGBA
			if sample.ID == 0 {
				wantFinal = [4]byte{5, 8, 10, 255}
			}
			if [4]byte(final[fat:fat+4]) != wantFinal {
				return result, fmt.Errorf("final composite %d,%d got %v want %v", x, y, final[fat:fat+4], wantFinal)
			}
			result.ColorSamples++
			if sample.ID > 0 {
				nonBackground++
			}
			at = int(c.poseSize+c.plane*2) + y*c.row + x*4
			depth := math.Float32frombits(binary.LittleEndian.Uint32(data[at:]))
			if math.Abs(float64(depth)-sample.Depth) > 1e-6 {
				return result, fmt.Errorf("depth %d,%d got %.9g expected %.9g", x, y, depth, sample.Depth)
			}
			result.DepthSamples++
		}
		if nonBackground < 3 {
			return result, fmt.Errorf("too few diagnostic interior samples: %d", result.ColorSamples)
		}
	}
	return result, nil
}
