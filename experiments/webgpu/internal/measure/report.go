// Package measure keeps observation clocks separate; it owns no GPU or simulation state.
package measure

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

const Warmup, Measured = 240, 3600

// Value encodes missing observations as null, including in CSV. Zero is a real observation.
type Value struct {
	MS    float64
	Valid bool
}

func Number(ms float64) Value { return Value{ms, true} }
func (v Value) MarshalJSON() ([]byte, error) {
	if !v.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(v.MS)
}
func (v Value) text() string {
	if !v.Valid {
		return "null"
	}
	return strconv.FormatFloat(v.MS, 'f', 6, 64)
}

type Frame struct {
	Index                                                                                                              int    `json:"index"`
	Generation                                                                                                         uint64 `json:"generation"`
	Prepare, Pack, Encode, Submit, RingWait, FencePollCPU, GPU, Completion, CallbackInterval, Presented, InputToSubmit Value
	Draws, Dispatches, Instances, Pieces, Triangles, UploadBytes, EndpointBytes, PersistentCreations                   int
	LiveResources                                                                                                      int
	LiveBytes                                                                                                          uint64
}
type Stats struct {
	Available          bool   `json:"available"`
	Reason             string `json:"reason,omitempty"`
	Samples            int    `json:"samples"`
	P50, P95, P99, Max *float64
}

func Statistics(values []float64, reason string) Stats {
	if len(values) == 0 {
		if reason == "" {
			reason = "no observations in this run"
		}
		return Stats{Reason: reason}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	at := func(q float64) *float64 { x := sorted[int(math.Ceil(q*float64(len(sorted))))-1]; return &x }
	return Stats{Available: true, Samples: len(sorted), P50: at(.5), P95: at(.95), P99: at(.99), Max: at(1)}
}

var metrics = []struct {
	name string
	get  func(Frame) Value
}{
	{"cpu_prepare_ms", func(f Frame) Value { return f.Prepare }},
	{"cpu_pack_ms", func(f Frame) Value { return f.Pack }},
	{"cpu_encode_ms", func(f Frame) Value { return f.Encode }},
	{"cpu_submit_ms", func(f Frame) Value { return f.Submit }},
	{"ring_wait_ms", func(f Frame) Value { return f.RingWait }},
	{"fence_poll_cpu_ms", func(f Frame) Value { return f.FencePollCPU }},
	{"gpu_ms", func(f Frame) Value { return f.GPU }},
	{"queue_completion_latency_ms", func(f Frame) Value { return f.Completion }},
	{"callback_interval_ms", func(f Frame) Value { return f.CallbackInterval }},
	{"presented_ms", func(f Frame) Value { return f.Presented }},
	{"input_to_submit_ms", func(f Frame) Value { return f.InputToSubmit }},
	{"cpu_work_ms", func(f Frame) Value {
		if !f.Prepare.Valid || !f.Pack.Valid || !f.Encode.Valid || !f.Submit.Valid {
			return Value{}
		}
		return Number(f.Prepare.MS + f.Pack.MS + f.Encode.MS + f.Submit.MS)
	}},
}

func CSV(rows []Frame) (string, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	header := []string{"index", "generation"}
	for _, m := range metrics {
		header = append(header, m.name)
	}
	header = append(header, "draws", "dispatches", "instances", "pieces", "triangles", "upload_bytes", "endpoint_bytes", "persistent_creations", "live_resources", "live_bytes")
	if e := w.Write(header); e != nil {
		return "", e
	}
	for _, f := range rows {
		if f.Index < Warmup {
			continue
		}
		row := []string{strconv.Itoa(f.Index), strconv.FormatUint(f.Generation, 10)}
		for _, m := range metrics {
			v := m.get(f)
			if m.name == "callback_interval_ms" && f.Index == Warmup {
				v = Value{}
			}
			row = append(row, v.text())
		}
		for _, n := range []int{f.Draws, f.Dispatches, f.Instances, f.Pieces, f.Triangles, f.UploadBytes, f.EndpointBytes, f.PersistentCreations, f.LiveResources} {
			row = append(row, strconv.Itoa(n))
		}
		row = append(row, strconv.FormatUint(f.LiveBytes, 10))
		if e := w.Write(row); e != nil {
			return "", e
		}
	}
	w.Flush()
	return b.String(), w.Error()
}
func Summary(rows []Frame, reasons map[string]string, refreshHz float64) map[string]any {
	stats := map[string]Stats{}
	count := 0
	gaps, long := 0, 0
	var peakBytes uint64
	peakCount := 0
	creates, uploads := 0, 0
	for _, m := range metrics {
		var v []float64
		for _, f := range rows {
			if f.Index < Warmup || f.Index == Warmup && m.name == "callback_interval_ms" {
				continue
			}
			x := m.get(f)
			if x.Valid {
				v = append(v, x.MS)
			}
		}
		stats[m.name] = Statistics(v, reasons[m.name])
	}
	for i := range rows {
		f := &rows[i]
		if f.Index < Warmup {
			continue
		}
		count++
		peakBytes = max(peakBytes, f.LiveBytes)
		peakCount = max(peakCount, f.LiveResources)
		creates += f.PersistentCreations
		uploads += f.EndpointBytes
		if f.Index > Warmup && f.CallbackInterval.Valid {
			if refreshHz > 0 && f.CallbackInterval.MS > 1500/refreshHz {
				gaps++
			}
			if f.CallbackInterval.MS > 50 {
				long++
			}
		}
	}
	var elapsed, fps *float64
	// Scheduling elapsed excludes the warmup boundary. No scanout claim follows.
	if count > 1 && stats["callback_interval_ms"].Samples == count-1 {
		sum := 0.0
		for _, f := range rows {
			if f.Index > Warmup {
				sum += f.CallbackInterval.MS
			}
		}
		seconds := sum / 1000
		elapsed = &seconds
		if seconds > 0 {
			x := float64(count-1) / seconds
			fps = &x
		}
	}
	var refreshGaps, longGaps *int
	if stats["callback_interval_ms"].Samples > 0 {
		longGaps = &long
		if refreshHz > 0 {
			refreshGaps = &gaps
		}
	}
	refreshReason := ""
	if refreshHz <= 0 {
		refreshReason = "verified refresh unavailable"
	}
	return map[string]any{"refresh_availability_reason": refreshReason, "measured_draws": count, "required_draws": Measured, "protocol_complete": count == Measured, "metrics": stats, "scheduling_elapsed_seconds": elapsed, "scheduling_frames_per_second": fps, "gaps_over_1_5_refresh_periods": refreshGaps, "gaps_over_50_ms": longGaps, "refresh_hz": refreshHz, "peak_renderer_resource_count": peakCount, "peak_renderer_estimated_bytes": peakBytes, "persistent_creations": creates, "endpoint_upload_bytes": uploads, "actual_display_timing": nil, "actual_display_reason": "not supplied by GoGPU public context at the pinned source", "clock_policy": "CPU and callback clocks are observations; queue completion is not GPU execution or display"}
}
func ValidateDimensions(actualW, actualH, wantW, wantH int) error {
	if actualW != wantW || actualH != wantH {
		return fmt.Errorf("actual physical extent %dx%d differs from required %dx%d", actualW, actualH, wantW, wantH)
	}
	return nil
}
