package meshscene

// ModelGroup keeps the source rasters immutable for reflection and solo child
// shadows. Only FinalSlot is painted; slots are one-based, instance indices are
// zero-based. Members follow the production cargo/yard walk (§22.4).
type ModelGroup struct {
	ParentInstance, ParentSlot, FinalSlot uint32
	Members                               []ModelGroupMember
	Keyed                                 bool // explicit unit carrier/factory staging; implicit pairs are painter ordered
	PreserveHoles                         bool // ordinary group; construction children admit visible coverage only
}

type ModelGroupMember struct {
	Instance, Slot uint32
	KeyDelta       int32 // separately truncated integer child Y minus integer parent Y
}

type modelCompositionMemberWork struct {
	instance int
	rect     [4]int
	delta    int32
	reveal   bool
}
type modelCompositionGroupWork struct {
	parent  int
	keyed   bool
	members []modelCompositionMemberWork
}

// TODO(question): reuse the production raw/resized cached-key seed and member
// cached/live chronology when the host exports that lifecycle. The isolated
// source raster remains the existing retained mesh approximation (§22.4).
func (c *ModelComposer) prepareGroups(live *LiveFrame) {
	out := &c.composition
	// Existing work entries are the final union slots and retain paint order.
	// Source entries appended below are never painted directly.
	for final := range c.groupWork {
		work := &c.groupWork[final]
		if len(work.members) == 1 && work.members[0].instance == work.parent {
			continue
		}
		if len(work.members) == 0 {
			continue
		}
		group := ModelGroup{ParentInstance: uint32(work.parent), FinalSlot: uint32(final + 1), Keyed: work.keyed, PreserveHoles: work.keyed}
		index := uint32(len(out.Groups) + 1)
		if cap(out.Groups) > len(out.Groups) {
			group.Members = out.Groups[:len(out.Groups)+1][len(out.Groups)].Members[:0]
		}
		opacity := live.Instances[work.parent].Visual.State[1]
		if !work.keyed || live.Instances[work.parent].Visual.State == [4]float32{} {
			opacity = 1
		}
		c.work[final].opacity = opacity
		for _, member := range work.members {
			slot := uint32(len(c.work) + 1)
			r := member.rect
			c.work = append(c.work, modelSlotWork{x0: r[0], y0: r[1], x1: r[2], y1: r[3], opacity: 1})
			out.InstanceSlots[member.instance] = slot
			out.MemberGroup[member.instance] = index
			group.Members = append(group.Members, ModelGroupMember{Instance: uint32(member.instance), Slot: slot, KeyDelta: member.delta})
			if member.instance == work.parent {
				group.ParentSlot = slot
			}
			if member.reveal {
				group.PreserveHoles = false
			}
		}
		out.Groups = append(out.Groups, group)
	}
}

// FinalSlotForInstance maps a source instance to its single painter/shadow
// commit. Reflection geometry continues to sample InstanceSlots independently.
func (c ModelComposition) FinalSlotForInstance(instance int) uint32 {
	if instance < 0 || instance >= len(c.InstanceSlots) {
		return 0
	}
	if instance < len(c.MemberGroup) {
		group := c.MemberGroup[instance]
		if group > 0 && int(group) <= len(c.Groups) {
			return c.Groups[group-1].FinalSlot
		}
	}
	return c.InstanceSlots[instance]
}
