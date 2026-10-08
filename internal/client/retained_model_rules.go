package client

import (
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	presentationrender "github.com/nanolathe-gg/nanolathe/internal/render"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// RetainedModelRules is the production composer's resolved per-subject state,
// without projected corners or composition images. RGB values use the client's
// current physical palette; negative reveal verdicts carry no colour.
type RetainedModelRules struct {
	KeyPlane, Digger       bool
	Waterline              drawlist.ModelWaterline
	WaterlineKey, KeyBias  uint8
	Reveal                 drawlist.ModelReveal // valid when Revealed
	Revealed               bool
	Below, Band, Above     [3]float32
	Outline                [4]float32
	OutlineIndex           uint8 // physical palette index before BLUE
	CastsShadow, Structure bool
	GroundY, OriginY       numeric.Fixed
}

// RetainedUnitRules resolves the metadata portion of buildUnitDraw and
// configureModelGeometryFor. The retained renderer already owns model admission;
// this call does not load a model, compose transforms or materialize vertices.
// Like recording, it runs only on the pinned presentation owner [I6].
func (c *Client) RetainedUnitRules(v frame.UnitView, tick uint32) RetainedModelRules {
	r := RetainedModelRules{KeyBias: uint8(modelHeightKey(0, false))}
	if c == nil {
		return r
	}
	v = c.arrivalUnit(v)
	// Preserve the current production buildUnitDraw predicates, including its
	// Digger key-plane override and shadow gate. This exports implementation
	// parity; it does not settle the differing contract in [03 R-REN-03D §1].
	draw := presentationrender.UnitDraw{
		WorldPos:  [3]numeric.Fixed{v.X, v.Y, v.Z},
		Structure: !v.BMCode, KeyPlane: v.ZBuffer || v.BuildRemaining > 0 || v.Digger,
		DiggerClip: v.Digger, SonarContact: v.UnderwaterExempt,
		CastsShadow: c.castsModelShadow(v.NoShadow, v.CanHover, v.Floater, !v.BMCode && !v.Digger),
		GroundY:     c.groundHeightUnder(v.X, v.Z),
	}
	r = c.retainedDrawRules(&draw, v.Owner, modelCursorUnit)
	// unitNanoframeReveal owns both the pulse arithmetic and Community colours.
	// Temporarily select the requested committed tick, restoring the recording
	// state before return; no observer or simulation clock is advanced.
	previousTick := c.frameTick
	c.frameTick = tick
	reveal, outline, revealed := c.unitNanoframeRevealValue(v)
	c.frameTick = previousTick
	if revealed {
		r.Reveal = drawlist.ModelReveal{Line: reveal.Line, Floor: reveal.Floor, Below: reveal.Below, Band: reveal.Band, Above: reveal.Above}
		r.Revealed = true
		r.Below, r.Band, r.Above = c.retainedVerdictColor(reveal.Below), c.retainedVerdictColor(reveal.Band), c.retainedVerdictColor(reveal.Above)
		colour := c.retainedVerdictColor(int16(outline))
		r.Outline = [4]float32{colour[0], colour[1], colour[2], 1}
		r.OutlineIndex = outline
	}
	return r
}

// RetainedFeatureRules is the ordinary feature pseudo-unit's metadata: keyed
// structure, no reveal or Digger, and permanently admitted BLUE waterline
// [03 R-RAST-01 §6]. Its shadow gate retains the underwater feature suppression.
func (c *Client) RetainedFeatureRules(v frame.FeatureView) RetainedModelRules {
	if c == nil {
		return RetainedModelRules{KeyBias: uint8(modelHeightKey(0, false))}
	}
	draw := presentationrender.UnitDraw{
		WorldPos: [3]numeric.Fixed{v.X, v.Y, v.Z}, Structure: true, KeyPlane: true,
		CastsShadow: c.featureCastsModelShadow(v.Y), GroundY: c.groundHeightUnder(v.X, v.Z),
	}
	return c.retainedDrawRules(&draw, 0, modelCursorFeature)
}

func (c *Client) retainedDrawRules(draw *presentationrender.UnitDraw, owner, kind uint8) RetainedModelRules {
	r := RetainedModelRules{
		KeyPlane: draw.KeyPlane, Digger: draw.DiggerClip,
		KeyBias:   uint8(modelHeightKey(0, draw.DiggerClip)),
		Structure: draw.Structure, CastsShadow: draw.CastsShadow,
		GroundY: draw.GroundY, OriginY: draw.WorldPos[1],
	}
	if draw.KeyPlane {
		if threshold, submerged := waterlineThreshold(c.seaLevel(), draw.WorldPos[1], draw.DiggerClip); submerged {
			r.Waterline, r.WaterlineKey = drawlist.ModelWaterlineErase, threshold
			if c.waterlineTints(draw, owner, kind) {
				r.Waterline = drawlist.ModelWaterlineBlue
			}
		}
	}
	return r
}

func (c *Client) retainedVerdictColor(verdict int16) [3]float32 {
	if verdict < 0 || c.pal == nil {
		return [3]float32{}
	}
	colour := c.pal.Base[uint8(verdict)]
	return [3]float32{float32(colour[0]) / 255, float32(colour[1]) / 255, float32(colour[2]) / 255}
}
