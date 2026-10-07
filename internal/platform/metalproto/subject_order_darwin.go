//go:build darwin

package metalproto

import (
	"cmp"
	"slices"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Only translucent subjects require individual resolves. Opaque geometry keeps
// mesh instancing. Depth order here remains a native presentation approximation.
func (p *livePacking) prepareCloaks(f meshscene.LiveFrame) {
	p.cloaks = p.cloaks[:0]
	p.cloakSubjects = p.cloakSubjects[:0]
	p.cloakMembers = p.cloakMembers[:0]
	if p.cloakIndices == nil {
		p.cloakIndices = make(map[uint64]int)
	}
	clear(p.cloakIndices)
	for i := range p.cursor {
		p.cursor[i] = p.spans[i].Start
	}
	for i, v := range f.Instances {
		at := p.cursor[v.Mesh]
		p.cursor[v.Mesh]++
		if v.Visual.State[1] >= 1 || v.Visual.State == [4]float32{} {
			continue
		}
		id := v.Subject
		if id == 0 {
			id = uint64(i) + (1 << 63)
		}
		if _, ok := p.cloakIndices[id]; !ok {
			depth := float32(0)
			// Empty retained models are valid and submit no geometry.
			if v.PoseOffset < len(f.Current) {
				a, b := f.Previous[v.PoseOffset], f.Current[v.PoseOffset]
				y := a[13] + (b[13]-a[13])*f.Alpha
				z := a[14] + (b[14]-a[14])*f.Alpha
				depth = z + 2*y
			}
			p.cloakIndices[id] = len(p.cloakSubjects)
			p.cloakSubjects = append(p.cloakSubjects, cloakSubject{id: id, depth: depth, phase: v.Phase})
		}
		p.cloakMembers = append(p.cloakMembers, cloakMember{draw: cloakDraw{Mesh: uint32(v.Mesh), Instance: at, Phase: v.Phase}, subject: id, order: v.SubjectOrder})
	}
	slices.SortStableFunc(p.cloakSubjects, func(a, b cloakSubject) int {
		if c := cmp.Compare(a.phase, b.phase); c != 0 {
			return c
		}
		return cmp.Compare(a.depth, b.depth)
	})
	for i, s := range p.cloakSubjects {
		p.cloakIndices[s.id] = i
	}
	slices.SortStableFunc(p.cloakMembers, func(a, b cloakMember) int {
		if c := cmp.Compare(p.cloakIndices[a.subject], p.cloakIndices[b.subject]); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	for _, m := range p.cloakMembers {
		m.draw.Group = uint32(p.cloakIndices[m.subject])
		p.cloaks = append(p.cloaks, m.draw)
	}
}
