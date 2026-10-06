package survival

import "github.com/nanolathe-gg/nanolathe/internal/aikit"

// Unit restrictions (docs/DESIGN_SESSIONS_AI_SAVE.md "Modern AI restriction
// caps"): the survival layer does not plan a tower or a wall piece once the
// survivor's own records of it, with its requests that have no record yet,
// have reached the definition's cap. The kit's allowance counts the records,
// the queued requests and what this think has emitted; the layer adds the
// jobs it has planned and not yet issued.

// allowance is how many more of p this survivor may still plan: the kit's
// allowance less the pieces of its tower and wall jobs not yet issued.
// aikit.Uncapped for a product without a cap, or without a kit.
func (st *state) allowance(k *aikit.Kit, p *aikit.UnitInfo) int32 {
	left := k.Allowance(p)
	if left == aikit.Uncapped {
		return left
	}
	for i := range st.jobs.list {
		if j := &st.jobs.list[i]; !j.issued && j.prod == p {
			left -= jobPieces(j)
		}
	}
	return left
}

// jobPieces is how many creations a job's orders ask for.
func jobPieces(j *job) int32 {
	switch j.kind {
	case jobTower:
		return 1
	case jobWall:
		return int32(j.npts)
	}
	return 0
}

// dropCapped runs before the jobs are issued: the economy, issued in between,
// may have used the allowance a planned job counted on. An unissued tower or
// wall job whose product has no allowance left after the jobs ahead of it
// ends without an order, so its builder returns to the economy; a wall
// segment keeps only the pieces that remain. Nothing is drawn or spent.
func (st *state) dropCapped(k *aikit.Kit) {
	js := &st.jobs
	kept := js.list[:0]
	for _, j := range js.list {
		if !j.issued && j.prod != nil && (j.kind == jobTower || j.kind == jobWall) {
			if left := k.Allowance(j.prod); left != aikit.Uncapped {
				for i := range kept {
					if q := &kept[i]; !q.issued && q.prod == j.prod {
						left -= jobPieces(q)
					}
				}
				if left <= 0 {
					continue
				}
				if j.kind == jobWall && int32(j.npts) > left {
					j.npts = int(left)
				}
			}
		}
		kept = append(kept, j)
	}
	js.list = kept
}
