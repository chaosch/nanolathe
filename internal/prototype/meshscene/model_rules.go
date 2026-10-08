package meshscene

// ModelRules is seven sequential float4 groups (112 bytes) for retained model
// verdicts. Reveal is line, floor, enabled, key-plane admission. Below/Band/Above
// are physical RGB plus verdict (-2 erase, -1 keep, nonnegative palette index).
// Clip is waterline kind/key, Digger admission and the key base. Shadow is
// structure quarter projection (1), mobile/Digger silhouette (2), or disabled
// (0), then ground Y, model origin Y and physical outline index. Outline is RGBA.
// This packet carries decisions; cached/live/cargo staging, the shadow punch
// and the aircraft soft-shadow filter remain executor responsibilities.
type ModelRules struct {
	Reveal, Below, Band, Above, Clip, Shadow, Outline [4]float32
}

// ApplyModelRules resolves the current publication's subject identities in the
// supplied instance order, reusing dst's storage. Call after Frame, before
// another Frame replaces its private identity mapping. The callback never
// receives a mutable session.
func (r *RetainedBattle) ApplyModelRules(dst []ModelRules, f *LiveFrame, rules func(kind uint8, id uint64) ModelRules) []ModelRules {
	if f == nil {
		return nil
	}
	out := resizeComposition(dst, len(f.Instances))
	clear(out)
	if rules == nil {
		return out
	}
	r.VisitSubjects(f, func(i int, kind uint8, id uint64, _, _ int) { out[i] = rules(kind, id) })
	return out
}
