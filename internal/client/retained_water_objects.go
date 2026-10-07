package client

import (
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

// RetainedWaterObject carries production admission only, in unscaled world
// units. The retained executor clips individual physical corners at SeaY;
// a submerged origin does not suppress its above-water pieces (§26.6).
type RetainedWaterObject struct {
	Reflect, Underwater bool
	SeaY                float32
}

func (c *Client) retainedWaterObject(x, z numeric.Fixed, blue bool, emission [3]float32) RetainedWaterObject {
	if c == nil {
		return RetainedWaterObject{}
	}
	r := RetainedWaterObject{SeaY: float32(c.seaLevel().Raw()) / 65536}
	surface := c.waterSurfaceMetadata()
	r.Reflect = c.enhanced && surface.Enabled && c.reflectionWaterAt(x, z)
	// Mask/source identity and validity are checked again by the executor. The
	// production underwater commit excludes cooling emission independently of
	// the shimmer switch, and requires motion even when surface shading is on.
	r.Underwater = blue && emission == [3]float32{} && c.effects.WaterMotion && surface.Enabled && c.terrain != nil && !c.terrain.LavaWorld
	return r
}
func (c *Client) RetainedUnitWaterObject(v frame.UnitView, tick uint32) RetainedWaterObject {
	if c == nil {
		return RetainedWaterObject{}
	}
	rules := c.RetainedUnitRules(v, tick)
	var g drawlist.ModelGeometry
	c.applyArrivalHeat(&g, v)
	v = c.arrivalUnit(v)
	return c.retainedWaterObject(v.X, v.Z, rules.Waterline == drawlist.ModelWaterlineBlue, g.WreckEmission)
}
func (c *Client) RetainedFeatureWaterObject(v frame.FeatureView) RetainedWaterObject {
	if c == nil {
		return RetainedWaterObject{}
	}
	rules := c.RetainedFeatureRules(v)
	var g drawlist.ModelGeometry
	c.applyWreckHeat(&g, v)
	return c.retainedWaterObject(v.X, v.Z, rules.Waterline == drawlist.ModelWaterlineBlue, g.WreckEmission)
}
func (c *Client) RetainedProjectileWaterObject(v frame.ProjectileView) RetainedWaterObject {
	return c.retainedWaterObject(v.X, v.Z, false, [3]float32{})
}
