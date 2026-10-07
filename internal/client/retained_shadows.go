package client

import (
	"github.com/nanolathe-gg/nanolathe/internal/frame"
)

// RetainedShadowPolicy carries only production presentation decisions. It
// neither composes corners nor advances texture/effect cursors [I6].
type RetainedShadowPolicy struct {
	Rules               RetainedModelRules
	Clearance, Softness float32 // world units and the executor's radius multiplier
	ClipKey             uint8
	Supersample         bool
	Tint                [3]float32
}

func (c *Client) RetainedUnitShadow(v frame.UnitView, tick uint32) RetainedShadowPolicy {
	p := c.retainedShadowPolicy(c.RetainedUnitRules(v, tick))
	if c == nil {
		return p
	}
	v = c.arrivalUnit(v)
	if v.Digger {
		p.ClipKey = uint8(diggerEraseThreshold)
	} else if key, submerged := waterlineThreshold(c.seaLevel(), v.Y, false); submerged {
		p.ClipKey = key
	}
	if c.enhanced && v.MoverMode == 2 && c.effects.SoftShadows {
		receiver := max(p.Rules.GroundY, c.seaLevel())
		p.Clearance = max(0, float32(v.Y.Sub(receiver).Raw())/65536)
		// Same clamp as gpurender.effectStrengthOffset; zero selects hard shadows.
		p.Softness = float32(max(0, min(c.effects.ShadowSoftness, 200))) / 100
	}
	return p
}
func (c *Client) RetainedFeatureShadow(v frame.FeatureView) RetainedShadowPolicy {
	return c.retainedShadowPolicy(c.RetainedFeatureRules(v))
}
func (c *Client) retainedShadowPolicy(r RetainedModelRules) RetainedShadowPolicy {
	p := RetainedShadowPolicy{Rules: r}
	if c != nil {
		p.Supersample = c.supersampleGeometry()
		p.Tint = c.retainedVerdictColor(int16(shadowColorIndex))
	}
	return p
}
