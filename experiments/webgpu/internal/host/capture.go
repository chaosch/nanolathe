package host

import (
	"encoding/binary"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/fixture"
	"github.com/nanolathe-gg/nanolathe/experiments/webgpu/internal/render"
	"math"
)

// Capture owns untimed staging only. MapAsync readiness is actual map completion on all paths.
type Capture struct {
	d                     *wgpu.Device
	buffer                *wgpu.Buffer
	pending               *wgpu.MapPending
	size, poseSize, plane uint64
	row, width, height    int
}

func NewCapture(d *wgpu.Device, count, width, height int) (*Capture, error) {
	c := &Capture{d: d, poseSize: uint64(count * 3 * 64), width: width, height: height, row: (width*4 + 255) / 256 * 256}
	c.plane = uint64(c.row * height)
	c.size = c.poseSize + c.plane*3
	b, e := d.CreateBuffer(&wgpu.BufferDescriptor{Label: "untimed-diagnostic", Size: c.size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	c.buffer = b
	return c, e
}
func (c *Capture) Encode(e *wgpu.CommandEncoder, r *render.Renderer) {
	poses, color, ids, depth := r.DiagnosticResources()
	e.CopyBufferToBuffer(poses, 0, c.buffer, 0, c.poseSize)
	for i, t := range []*wgpu.Texture{color, ids, depth} {
		aspect := gputypes.TextureAspectAll
		if i == 2 {
			aspect = gputypes.TextureAspectDepthOnly
		}
		old := wgpu.TextureUsageTextureBinding
		if i == 2 {
			old = wgpu.TextureUsageRenderAttachment
		}
		e.TransitionTextures([]wgpu.TextureBarrier{{Texture: t, Range: wgpu.TextureRange{Aspect: aspect, MipLevelCount: 1, ArrayLayerCount: 1}, Usage: wgpu.TextureUsageTransition{OldUsage: old, NewUsage: wgpu.TextureUsageCopySrc}}})
		e.CopyTextureToBuffer(t, c.buffer, []wgpu.BufferTextureCopy{{BufferLayout: wgpu.ImageDataLayout{Offset: c.poseSize + uint64(i)*c.plane, BytesPerRow: uint32(c.row), RowsPerImage: uint32(c.height)}, TextureBase: wgpu.ImageCopyTexture{Texture: t, Aspect: aspect}, Size: wgpu.Extent3D{Width: uint32(c.width), Height: uint32(c.height), DepthOrArrayLayers: 1}}})
	}
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
	MaxPoseError                                float64
	MinimumIDPixels, ColorSamples, DepthSamples int
	Resources                                   render.Resources
}

func (c *Capture) Compare(data []byte, w *fixture.Workload, s fixture.Snapshot, alpha float32) (result Comparison, err error) {
	result.Alpha = alpha
	for i := range s.Previous {
		want := fixture.Blend(s.Previous[i], s.Current[i], alpha)
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
