//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Append after the base shader source: ModelSlot, ModelRules and Uniforms are
// shared. Root binds the per-instance int4 operand at vertex buffer 20.
//
//go:embed native/groups.metal
var groupShaderSource string

type nativeGroup struct{ Info, Counts [4]uint32 }
type nativeGroupMember struct {
	Instance, Slot uint32
	KeyDelta       int32
	Reserved       uint32
}
type nativeGroupUpload struct {
	Groups, Members, InstanceGroups                  unsafe.Pointer
	GroupCount, MemberCount, InstanceCount, Reserved uint32
}

// GroupPacking owns borrowed upload storage until its next Prepare. It remaps
// only identities; source/final slots and ordered key deltas remain unchanged.
// Counts.z admits keyed unit staging. Instance policy is parent=0, child=1,
// or keyless painter member=2; policy 2 never forces a plane or carrier clip.
type GroupPacking struct {
	groups    []nativeGroup
	members   []nativeGroupMember
	instances [][4]int32
	slotRoles []uint8
}

func (p *GroupPacking) Prepare(c meshscene.ModelComposition, sourcePacked []uint32) (nativeGroupUpload, error) {
	var out nativeGroupUpload
	fail := func(why string) (nativeGroupUpload, error) {
		return out, fmt.Errorf("nanolathe: group upload failed: logical path retained groups, providers searched [production order native instance packing], expected %s", why)
	}
	if unsafe.Sizeof(out) != 40 || unsafe.Sizeof(nativeGroup{}) != 32 || unsafe.Sizeof(nativeGroupMember{}) != 16 {
		return fail("matching native group ABI")
	}
	if len(sourcePacked) != len(c.MemberGroup) || len(c.Groups) > math.MaxInt32 || len(sourcePacked) > math.MaxInt32 {
		return fail("bounded group and instance counts")
	}
	for _, at := range sourcePacked {
		if int(at) >= len(sourcePacked) {
			return fail("packed indices within instance span")
		}
	}
	p.groups, p.members = p.groups[:0], p.members[:0]
	p.instances = resizeGroup(p.instances, len(sourcePacked))
	clear(p.instances)
	p.slotRoles = resizeGroup(p.slotRoles, len(c.Slots))
	clear(p.slotRoles)
	validSlot := func(slot uint32) bool { return slot > 0 && int(slot) <= len(c.Slots) }
	for gi, g := range c.Groups {
		if int(g.ParentInstance) >= len(sourcePacked) || !validSlot(g.FinalSlot) || (g.ParentSlot != 0 && !validSlot(g.ParentSlot)) || len(g.Members) == 0 {
			return fail("valid parent, final slot and ordered members")
		}
		if p.slotRoles[g.FinalSlot-1] != 0 {
			return fail("unique reserved final slots")
		}
		p.slotRoles[g.FinalSlot-1] = 2
		parent := sourcePacked[g.ParentInstance]
		if int(parent) >= len(sourcePacked) {
			return fail("packed parent within instance span")
		}
		start := len(p.members)
		for mi, m := range g.Members {
			if int(m.Instance) >= len(sourcePacked) || !validSlot(m.Slot) || m.Slot == g.FinalSlot || c.MemberGroup[m.Instance] != uint32(gi+1) {
				return fail("isolated source slots and matching group membership")
			}
			if p.slotRoles[m.Slot-1] != 0 {
				return fail("unique immutable source slots")
			}
			p.slotRoles[m.Slot-1] = 1
			if m.Instance == g.ParentInstance && (mi != 0 || m.KeyDelta != 0 || m.Slot != g.ParentSlot) {
				return fail("parent seed first with unchanged key")
			}
			at := sourcePacked[m.Instance]
			if int(at) >= len(sourcePacked) || p.instances[at][0] != 0 {
				return fail("unique packed group members")
			}
			child := int32(0)
			if !g.Keyed {
				child = 2
			} else if m.Instance != g.ParentInstance {
				child = 1
			}
			p.instances[at] = [4]int32{int32(gi + 1), child, m.KeyDelta, int32(parent)}
			p.members = append(p.members, nativeGroupMember{Instance: at, Slot: m.Slot, KeyDelta: m.KeyDelta})
		}
		if len(p.members) > math.MaxInt32 {
			return fail("bounded member count")
		}
		keyed := uint32(0)
		if g.Keyed {
			keyed = 1
		}
		holes := uint32(0)
		if g.PreserveHoles {
			holes = 1
		}
		p.groups = append(p.groups, nativeGroup{Info: [4]uint32{parent, g.ParentSlot, g.FinalSlot, uint32(start)}, Counts: [4]uint32{uint32(len(g.Members)), holes, keyed}})
	}
	for i, gi := range c.MemberGroup {
		if gi > uint32(len(c.Groups)) || (gi != 0 && p.instances[sourcePacked[i]][0] != int32(gi)) {
			return fail("complete group membership sidecar")
		}
	}
	out = nativeGroupUpload{Groups: pointer(p.groups), Members: pointer(p.members), InstanceGroups: pointer(p.instances), GroupCount: uint32(len(p.groups)), MemberCount: uint32(len(p.members)), InstanceCount: uint32(len(p.instances))}
	return out, nil
}
func resizeGroup[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
