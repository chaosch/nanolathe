//go:build darwin

package metalproto

import "time"

type benchmarkHostTiming struct {
	lastStart, lastEnd time.Time
}

func (t *benchmarkHostTiming) begin(frame int, entry, start time.Time) BenchmarkFrame {
	return t.beginPaced(frame, entry, entry, start)
}

// beginPaced matches Ebitengine's benchmark bracket: waitStart immediately
// precedes deadline computation/sleep and start immediately follows it.
// entry is the callback/loop entry after any one-time measurement setup; using
// entry as waitStart would include host bookkeeping in PaceWait.
func (t *benchmarkHostTiming) beginPaced(frame int, entry, waitStart, start time.Time) BenchmarkFrame {
	row := BenchmarkFrame{Frame: frame, CadenceValid: !t.lastStart.IsZero(), PaceWait: float64(start.Sub(waitStart)) / 1e6}
	if row.CadenceValid {
		row.Cadence = float64(start.Sub(t.lastStart)) / 1e6
	}
	if !t.lastEnd.IsZero() {
		row.OutsideDraw = float64(entry.Sub(t.lastEnd)) / 1e6
	}
	t.lastStart = start
	return row
}

func (t *benchmarkHostTiming) end(row *BenchmarkFrame, start time.Time) {
	t.lastEnd = time.Now()
	row.DrawWork = float64(t.lastEnd.Sub(start)) / 1e6
}
