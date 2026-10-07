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
// supplied instance order. Call after Frame, before another Frame replaces its
// private identity mapping. The callback never receives a mutable session.
func (r *RetainedBattle) ApplyModelRules(f *LiveFrame, rules func(kind uint8, id uint64) ModelRules) []ModelRules {
	if f == nil {
		return nil
	}
	out := make([]ModelRules, len(f.Instances))
	if r == nil || r.previous == nil || rules == nil {
		return out
	}
	type poseIdentity struct{ mesh, offset int }
	byPose := make(map[poseIdentity]battleEntityKey, len(r.previous.byID))
	// Each drawable subject owns a disjoint pose span. Map iteration only
	// builds a lookup; callback/draw order always follows the instance slice.
	for key, span := range r.previous.byID {
		if span.count > 0 {
			byPose[poseIdentity{span.mesh, span.offset}] = key
		}
	}
	for i, instance := range f.Instances {
		if key, ok := byPose[poseIdentity{instance.Mesh, instance.PoseOffset}]; ok {
			out[i] = rules(key.kind, key.id)
		}
	}
	return out
}
