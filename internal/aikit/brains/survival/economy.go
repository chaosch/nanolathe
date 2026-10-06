package survival

import (
	"github.com/nanolathe-gg/nanolathe/internal/aikit"
	"github.com/nanolathe-gg/nanolathe/internal/aikit/core"
)

// Economy wraps the utility economy: the survival layer first decides its
// own jobs and takes their builders, the utility economy then decides the
// builders it is shown, and the survival orders are issued last, from the
// action budget the economy left.
type Economy struct {
	st      *state
	inner   core.Policy
	cleanup cleanup
	// view borrows observation slices only during inner.Plan and is then
	// cleared; reusing its header avoids allocating an observation per think.
	view aikit.Obs
}

// Init implements core.Policy.
func (e *Economy) Init(b *core.Board) {
	if e.inner != nil {
		e.inner.Init(b)
	}
}

// Plan implements core.Policy.
func (e *Economy) Plan(b *core.Board) {
	st := e.st
	st.kit = b.K
	if !st.ready {
		st.setup(b)
	}
	st.refreshJobs(b)
	e.refreshCleanup(b)
	all := b.Builders
	e.hideCleanup(b)
	st.plan(b)
	b.Builders = all
	e.planCleanup(b)
	st.hideClaimed(b)
	e.hideCleanup(b)
	if e.inner != nil {
		original := b.O
		e.view = *original
		e.view.Features = e.cleanupFeatures(original)
		b.O = &e.view
		e.inner.Plan(b)
		b.O = original
		e.view = aikit.Obs{}
	}
	b.Builders = all
	st.emit(b)
	e.emitCleanup(b)
}

// Explain implements core.Explaining.
func (e *Economy) Explain(b *core.Board, x *aikit.Explain) {
	if ex, ok := e.inner.(core.Explaining); ok {
		ex.Explain(b, x)
	}
	e.explainCleanup(x)
}

// Report implements aikit.Reporter; cleanup counters are observational.
func (e *Economy) Report(add func(name string, value int64)) {
	if r, ok := e.inner.(aikit.Reporter); ok {
		r.Report(add)
	}
	add("sv_clears", e.cleanup.orders)
	add("sv_lane_clears", e.cleanup.lanes)
	add("sv_corridor_clears", e.cleanup.corridors)
	add("sv_reclaim_metal", e.cleanup.metal)
	add("sv_cleanup_stops", e.cleanup.stops)
}
