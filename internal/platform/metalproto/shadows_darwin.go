//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"

	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// Append after shaderSource: it reuses model structs/project and nmWaterMask.
//
//go:embed native/shadows.metal
var shadowShaderSource string

// nativeShadowUpload is 136 bytes on 64-bit hosts, matching NMShadowUpload.
// Root embeds it in the live upload; acquired ring slots copy every operand.
type nativeShadowUpload struct {
	Topology, Slots, Subjects, BodyDraws, ProjectedDraws, BodyRules, PieceFlags                  uintptr
	TopologyCount, SlotCount, SubjectCount, BodyCount, ProjectedCount, PieceCount, Width, Height uint32
	Tint, Water                                                                                  [4]float32
	BodyRuleSlots                                                                                uintptr
	BodyRuleCount, Reserved                                                                      uint32
}
type nativeShadowMesh struct {
	Vertices, Indices       uintptr
	VertexCount, IndexCount uint32
}

// ShadowPacking is reusable scratch owned by livePacking, not a global map.
// Prepare remaps original instance indices only; body and pose identities stay
// intact. Topology is uploaded once by the renderer's NMShadowState.
type ShadowPacking struct {
	topology        []nativeShadowMesh
	body, projected []meshscene.ShadowDraw
}

func (p *ShadowPacking) Prepare(c meshscene.ShadowComposition, sourcePacked []uint32) (nativeShadowUpload, error) {
	if c.AtlasWidth > 8192 || c.AtlasHeight > 8192 {
		return nativeShadowUpload{}, shadowUploadError("shadow atlas within 8192 pixels")
	}
	if len(c.BodyRuleSlots) != len(c.BodyDraws) {
		return nativeShadowUpload{}, shadowUploadError("one body rule slot per source draw")
	}
	for _, slot := range c.BodyRuleSlots {
		if int(slot) > len(c.BodyRules) {
			return nativeShadowUpload{}, shadowUploadError("body rule slots within the uploaded rules")
		}
	}
	p.topology = p.topology[:0]
	for _, mesh := range c.Topology {
		p.topology = append(p.topology, nativeShadowMesh{pointer(mesh.Vertices), pointer(mesh.Indices), uint32(len(mesh.Vertices)), uint32(len(mesh.Indices))})
	}
	remap := func(dst []meshscene.ShadowDraw, src []meshscene.ShadowDraw) ([]meshscene.ShadowDraw, error) {
		dst = append(dst[:0], src...)
		for i := range dst {
			d := &dst[i]
			if int(d.Instance) >= len(sourcePacked) || int(d.Mesh) >= len(c.Topology) || d.Slot == 0 || int(d.Slot) > len(c.Slots) || int(d.Subject) >= len(c.Subjects) {
				return nil, shadowUploadError("valid shadow source, slot and subject")
			}
			d.Instance = sourcePacked[d.Instance]
		}
		return dst, nil
	}
	var err error
	p.body, err = remap(p.body, c.BodyDraws)
	if err != nil {
		return nativeShadowUpload{}, err
	}
	p.projected, err = remap(p.projected, c.ProjectedDraws)
	if err != nil {
		return nativeShadowUpload{}, err
	}
	return nativeShadowUpload{Topology: pointer(p.topology), Slots: pointer(c.Slots), Subjects: pointer(c.Subjects), BodyDraws: pointer(p.body), ProjectedDraws: pointer(p.projected), BodyRules: pointer(c.BodyRules), PieceFlags: pointer(c.PieceFlags), TopologyCount: uint32(len(p.topology)), SlotCount: uint32(len(c.Slots)), SubjectCount: uint32(len(c.Subjects)), BodyCount: uint32(len(p.body)), ProjectedCount: uint32(len(p.projected)), PieceCount: uint32(len(c.PieceFlags)), Width: uint32(c.AtlasWidth), Height: uint32(c.AtlasHeight), Tint: c.Tint, Water: c.Water, BodyRuleSlots: pointer(c.BodyRuleSlots), BodyRuleCount: uint32(len(c.BodyRules))}, nil
}

func shadowUploadError(expected string) error {
	return fmt.Errorf("nanolathe: shadow upload failed: logical path retained subjects, providers searched [prepared shadow atlas native instance packing], expected %s", expected)
}
