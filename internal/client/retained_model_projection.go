package client

import (
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// RetainedModelProjection is presentation metadata, not projected geometry.
// NativeAnchor and DoubledAnchor exclude the camera's viewport constants, as
// modelAnchor/modelDirectVertex do. Feature IDs are one-based frame indices,
// matching the retained battle's feature pose identities, not InstanceID.
type RetainedModelProjection struct {
	Kind                           uint8
	ID                             uint64
	Origin                         [3]numeric.Fixed
	NativeAnchor, DoubledAnchor    [2]int32
	KeyPlane, ModeKnown, DirectAll bool
	// PieceDirect is indexed by model piece. It describes the direct live
	// branch only when ModeKnown is true; construction overrides this filter.
	PieceDirect []bool
}

type RetainedModelProjectionFrame struct {
	World       drawlist.WorldSpace
	Camera      [2]int32
	RecordScale float32
	Doubled     bool
	Subjects    []RetainedModelProjection
	pieces      []bool // backs the subjects' PieceDirect
}

// RecordRetainedModelProjections samples the same pinned, blended and
// arrival-adjusted inputs as production recording (§2.2.1, §17.2–§17.5).
// It reads no posed vertices and changes no cached body/orientation state.
// Call on the presentation owner after BeginPresentationFrame. It records
// into out, reusing its storage, so the metadata is valid until the next
// record into out; no committed frame is mutated [I6].
func (c *Client) RecordRetainedModelProjections(out *RetainedModelProjectionFrame) {
	*out = RetainedModelProjectionFrame{Subjects: out.Subjects[:0], pieces: out.pieces[:0]}
	if c == nil || c.cam == nil {
		return
	}
	cur := c.presentationFrame()
	if cur == nil {
		return
	}
	if cur != c.committedFrame() && c.beginCameraBlend() {
		defer c.endCameraBlend(true)
	}
	out.World = c.worldSpace(true)
	out.Camera = [2]int32{c.cam.X, c.cam.Z}
	out.RecordScale = float32(c.modelScale().Norm() / 2)
	out.Doubled = c.supersampleGeometry()
	anchors := func(p *RetainedModelProjection) {
		x, y, z := p.Origin[0], p.Origin[1], p.Origin[2]
		sx, sy := c.cam.WorldToScreen(x, y, z)
		sx2, sy2 := c.cam.WorldToScreenDoubled(x, y, z)
		p.NativeAnchor = [2]int32{sx - camera.OriginX, sy - camera.OriginY}
		p.DoubledAnchor = [2]int32{sx2 - 2*camera.OriginX, sy2 - 2*camera.OriginY}
	}
	for _, original := range cur.Units {
		if c.arrivalHidesUnit(original) {
			continue
		}
		v := c.arrivalUnit(original)
		p := RetainedModelProjection{Kind: 1, ID: v.InstanceID, Origin: [3]numeric.Fixed{v.X, v.Y, v.Z}, KeyPlane: v.ZBuffer || v.BuildRemaining > 0 || v.Digger}
		// A keyed body's cached and live lanes both use modelLocalVertex.
		// TODO(retained-direct-staging): a keyless subject needs the actual
		// retained body's rebuild/discard decision to distinguish cached plus
		// direct Live from direct All [03 R-REN-03A §4]. A metadata read must
		// not create that body or advance its orientation reference. Until the
		// renderer exports that decision, preserve its existing projection.
		p.ModeKnown = c.enhanced && p.KeyPlane
		if !p.KeyPlane {
			// Preserve the published live-piece selector for a later renderer
			// cache decision. This alone cannot establish whether DirectAll
			// applies. Reading the compiled piece names does not compose poses.
			if m := c.modelForUnit(v); m != nil && m.compiled != nil {
				p.PieceDirect = out.pieceStorage(len(m.compiled.Pieces))
				for _, piece := range v.Pieces {
					index := piece.Index
					if piece.Name != "" {
						var found bool
						index, found = m.pieceByName[strings.ToLower(piece.Name)]
						if !found {
							continue
						}
					}
					if index >= 0 && index < len(p.PieceDirect) {
						p.PieceDirect[index] = piece.DontCache
					}
				}
			}
		}
		anchors(&p)
		out.Subjects = append(out.Subjects, p)
	}
	for i, v := range cur.Features {
		if v.Model == "" || v.Filename != "" {
			continue
		}
		// The feature pseudo-unit is always keyed; its retained and uncached
		// geometry producers both use the local projection [03 R-RAST-01 §6].
		p := RetainedModelProjection{Kind: 2, ID: uint64(i) + 1, Origin: [3]numeric.Fixed{v.X, v.Y, v.Z}, KeyPlane: true, ModeKnown: c.enhanced}
		anchors(&p)
		out.Subjects = append(out.Subjects, p)
	}
}

// pieceStorage returns n cleared flags. Growing leaves earlier subjects on
// the old array, which stays valid; the next record reuses the larger one.
func (out *RetainedModelProjectionFrame) pieceStorage(n int) []bool {
	if cap(out.pieces)-len(out.pieces) < n {
		out.pieces = make([]bool, 0, max(2*cap(out.pieces), n, 1024))
	}
	start := len(out.pieces)
	out.pieces = out.pieces[:start+n]
	flags := out.pieces[start : start+n : start+n]
	clear(flags)
	return flags
}
