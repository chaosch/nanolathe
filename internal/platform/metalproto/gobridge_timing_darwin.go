//go:build darwin && arm64

package metalproto

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/platform/metalproto/mtl"
)

// gbTiming is the per-pass GPU timing diagnostic (timing.inc). Disabled
// unless NANOLATHE_METAL_PASS_TIMING names an output CSV. Render passes
// sample the start and end of their vertex and fragment stages, compute and
// blit passes the start and end of the encoder. Apple GPUs sample only stage
// boundaries; adjacent passes may overlap on a tile GPU, so spans are
// attribution, not exclusive busy time.
const gbTimingSlots, gbTimingPasses = 3, 512

type gbPassRecord struct {
	frame       uint32
	index, kind uint16
	label       string
	t           [4]uint64
}

type gbTimingSelectors struct {
	attachments, sampleBuffer, startVertex, endVertex, startFragment, endFragment       mtl.SEL
	startEncoder, endEncoder, dispatchType, computeWithDescriptor, blitWithDescriptor   mtl.SEL
	resolve, bytes, length, sampleTimestamps, computePassDescriptor, blitPassDescriptor mtl.SEL
}

type gbTiming struct {
	enabled, active        bool
	path                   string
	buffers                [gbTimingSlots]mtl.ID
	labels                 [gbTimingSlots][gbTimingPasses]string
	kinds                  [gbTimingSlots][gbTimingPasses]uint16
	used                   [gbTimingSlots]int
	slot                   int
	records                []gbPassRecord
	cpu0, gpu0, cpu1, gpu1 uint64
	wall0, wall1           float64
	sel                    gbTimingSelectors
	computePass, blitPass  mtl.ID
}

// gbTimingFrame names one frame's slot, resolved on completion.
type gbTimingFrame struct {
	frame uint32
	slot  int
}

func newGBTiming(device mtl.Device) *gbTiming {
	t := &gbTiming{}
	path := os.Getenv("NANOLATHE_METAL_PASS_TIMING")
	if path == "" {
		return t
	}
	s := mtl.Sel
	if mtl.ID(device).Send(s("supportsCounterSampling:"), 0)&0xff == 0 {
		fmt.Fprintln(os.Stderr, "nanolathe: Metal pass timing: device lacks stage-boundary counter sampling")
		return t
	}
	var timestamps mtl.ID
	sets := mtl.ID(mtl.ID(device).Get(s("counterSets")))
	for i := 0; i < sets.Count(); i++ {
		set := mtl.ID(sets.Send(s("objectAtIndex:"), uintptr(i)))
		if mtl.ID(set.Get(s("name"))).Send(s("isEqualToString:"), uintptr(mtl.CommonCounterSetTimestamp()))&0xff != 0 {
			timestamps = set
		}
	}
	if timestamps == 0 {
		fmt.Fprintln(os.Stderr, "nanolathe: Metal pass timing: no timestamp counter set")
		return t
	}
	desc := mtl.New("MTLCounterSampleBufferDescriptor")
	defer mtl.Release(desc)
	desc.Send(s("setCounterSet:"), uintptr(timestamps))
	desc.Send(s("setStorageMode:"), mtl.StorageModeShared)
	desc.Send(s("setSampleCount:"), gbTimingPasses*4)
	for i := range t.buffers {
		desc.Send(s("setLabel:"), uintptr(mtl.String(fmt.Sprintf("pass timing %d", i))))
		var err mtl.ID
		t.buffers[i] = mtl.ID(mtl.ID(device).Send(s("newCounterSampleBufferWithDescriptor:error:"), uintptr(desc), uintptr(unsafe.Pointer(&err))))
		if t.buffers[i] == 0 {
			fmt.Fprintf(os.Stderr, "nanolathe: Metal pass timing: sample buffer: %s\n", mtl.ErrorText(err))
			return t
		}
	}
	t.sel = gbTimingSelectors{
		attachments: s("sampleBufferAttachments"), sampleBuffer: s("setSampleBuffer:"),
		startVertex: s("setStartOfVertexSampleIndex:"), endVertex: s("setEndOfVertexSampleIndex:"),
		startFragment: s("setStartOfFragmentSampleIndex:"), endFragment: s("setEndOfFragmentSampleIndex:"),
		startEncoder: s("setStartOfEncoderSampleIndex:"), endEncoder: s("setEndOfEncoderSampleIndex:"),
		dispatchType: s("setDispatchType:"), computeWithDescriptor: s("computeCommandEncoderWithDescriptor:"),
		blitWithDescriptor: s("blitCommandEncoderWithDescriptor:"), resolve: s("resolveCounterRange:"),
		bytes: s("bytes"), length: s("length"), sampleTimestamps: s("sampleTimestamps:gpuTimestamp:"),
		computePassDescriptor: s("computePassDescriptor"), blitPassDescriptor: s("blitPassDescriptor"),
	}
	t.computePass, t.blitPass = mtl.GetClass("MTLComputePassDescriptor"), mtl.GetClass("MTLBlitPassDescriptor")
	t.path = path
	mtl.ID(device).Send(t.sel.sampleTimestamps, uintptr(unsafe.Pointer(&t.cpu0)), uintptr(unsafe.Pointer(&t.gpu0)))
	t.wall0 = mtl.MediaTime()
	t.enabled = true
	return t
}

// begin starts a frame after its command buffer exists. The bounded queue
// guarantees the slot's previous command buffer has completed.
func (t *gbTiming) begin(device mtl.Device, frame int) *gbTimingFrame {
	if !t.enabled {
		return nil
	}
	t.slot = frame % gbTimingSlots
	t.used[t.slot] = 0
	t.active = true
	mtl.ID(device).Send(t.sel.sampleTimestamps, uintptr(unsafe.Pointer(&t.cpu1)), uintptr(unsafe.Pointer(&t.gpu1)))
	t.wall1 = mtl.MediaTime()
	return &gbTimingFrame{frame: uint32(frame), slot: t.slot}
}

func (t *gbTiming) claim(kind uint16, label string) int {
	if !t.active {
		return -1
	}
	i := t.used[t.slot]
	if i >= gbTimingPasses {
		return -1
	}
	t.labels[t.slot][i], t.kinds[t.slot][i] = label, kind
	t.used[t.slot] = i + 1
	return i
}

func (t *gbTiming) attachment(pass mtl.ID) mtl.ID {
	return mtl.ID(pass.Get(t.sel.attachments)).Index(0)
}

func (t *gbTiming) attachRender(pass mtl.PassDescriptor, label string) {
	if !t.enabled {
		return
	}
	i := t.claim(1, label)
	if i < 0 {
		return
	}
	a := t.attachment(mtl.ID(pass))
	a.Send(t.sel.sampleBuffer, uintptr(t.buffers[t.slot]))
	a.Send(t.sel.startVertex, uintptr(i*4))
	a.Send(t.sel.endVertex, uintptr(i*4+1))
	a.Send(t.sel.startFragment, uintptr(i*4+2))
	a.Send(t.sel.endFragment, uintptr(i*4+3))
}

func (t *gbTiming) compute(cb mtl.CommandBuffer, label string, dispatch uint) (mtl.ComputeEncoder, bool) {
	if !t.enabled {
		return 0, false
	}
	i := t.claim(2, label)
	if i < 0 {
		return 0, false
	}
	p := mtl.ID(t.computePass.Get(t.sel.computePassDescriptor))
	p.Send(t.sel.dispatchType, uintptr(dispatch))
	a := t.attachment(p)
	a.Send(t.sel.sampleBuffer, uintptr(t.buffers[t.slot]))
	a.Send(t.sel.startEncoder, uintptr(i*4))
	a.Send(t.sel.endEncoder, uintptr(i*4+1))
	return mtl.ComputeEncoder(mtl.ID(cb).Send(t.sel.computeWithDescriptor, uintptr(p))), true
}

func (t *gbTiming) blit(cb mtl.CommandBuffer, label string) (mtl.BlitEncoder, bool) {
	if !t.enabled {
		return 0, false
	}
	i := t.claim(3, label)
	if i < 0 {
		return 0, false
	}
	p := mtl.ID(t.blitPass.Get(t.sel.blitPassDescriptor))
	a := t.attachment(p)
	a.Send(t.sel.sampleBuffer, uintptr(t.buffers[t.slot]))
	a.Send(t.sel.startEncoder, uintptr(i*4))
	a.Send(t.sel.endEncoder, uintptr(i*4+1))
	return mtl.BlitEncoder(mtl.ID(cb).Send(t.sel.blitWithDescriptor, uintptr(p))), true
}

// complete resolves a finished frame's samples into records.
func (t *gbTiming) complete(f *gbTimingFrame) {
	if f == nil {
		return
	}
	n := t.used[f.slot]
	if n == 0 {
		return
	}
	// resolveCounterRange: takes an NSRange, passed in two registers.
	data := mtl.ID(t.buffers[f.slot].Send(t.sel.resolve, 0, uintptr(n*4)))
	var samples []uint64
	if data != 0 && int(data.Get(t.sel.length)) >= n*4*8 {
		samples = unsafe.Slice((*uint64)(mtl.CPtr(data.Get(t.sel.bytes))), n*4)
	}
	for i := 0; i < n; i++ {
		r := gbPassRecord{frame: f.frame, index: uint16(i), kind: t.kinds[f.slot][i], label: t.labels[f.slot][i]}
		if samples != nil {
			copy(r.t[:], samples[i*4:i*4+4])
		}
		t.records = append(t.records, r)
	}
}

// write saves every resolved record; unsampled lanes hold the counter error
// value.
func (t *gbTiming) write() {
	if !t.enabled || t.path == "" {
		return
	}
	var out strings.Builder
	out.WriteString("frame,index,kind,label,t0,t1,t2,t3\n")
	for _, r := range t.records {
		fmt.Fprintf(&out, "%d,%d,%d,%s,%d,%d,%d,%d\n", r.frame, r.index, r.kind, r.label, r.t[0], r.t[1], r.t[2], r.t[3])
	}
	if err := os.WriteFile(t.path, []byte(out.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nanolathe: Metal pass timing: %v\n", err)
		return
	}
	seconds := t.wall1 - t.wall0
	calibration := map[string]any{"gpu_ticks_per_second": 0.0, "cpu_ticks_per_second": 0.0, "calibration_seconds": seconds, "error_value": uint64(1<<64 - 1)}
	if seconds > 0 {
		calibration["gpu_ticks_per_second"] = float64(t.gpu1-t.gpu0) / seconds
		calibration["cpu_ticks_per_second"] = float64(t.cpu1-t.cpu0) / seconds
	}
	data, _ := json.MarshalIndent(calibration, "", "  ")
	_ = os.WriteFile(t.path+".json", data, 0o644)
}

func lookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
