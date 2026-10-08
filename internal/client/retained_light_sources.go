package client

import (
	"image"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	presentationrender "github.com/nanolathe-gg/nanolathe/internal/render"
)

// RetainedLightSources owns source-only feature inputs in production order.
// World and Wrecks use projected RECORD pixels; Art retains whole emitters,
// while Leaves supplies their ordinary replayed art to glow/heat consumers.
// Short/Ground retain the sprite-source barriers. recordMetalEffects already
// inserts that Art into its layer Sources; append only Wrecks separately to
// RetainedLighting, once. This never records unit model geometry.
type RetainedLightSources struct {
	World         drawlist.WorldSpace
	Art, Leaves   []drawlist.Sprite
	Wrecks        []drawlist.WreckSource
	Short, Ground drawlist.List
}

// RecordRetainedLightSources is called by RecordRetainedEffects while its
// pinned camera and frame scratch are active. It records sprite features through
// drawFeature (the original cursor/LOS/heat/lighting decisions), but measures
// only cooling 3DO wreck packets through the retained feature cache. It does
// not rasterize models or rebuild unit meshes; packet admission remains exact.
// It changes no observer history and consumes no presentation or session RNG.
func (c *Client) RecordRetainedLightSources() RetainedLightSources {
	return c.recordRetainedLightSources(nil)
}

// recordRetainedLightSources copies into storage's reusable lists and slices
// when storage is non-nil; the result is then valid until the next call.
func (c *Client) recordRetainedLightSources(storage *RetainedEffectStorage) RetainedLightSources {
	var out RetainedLightSources
	if storage != nil {
		out.Art, out.Leaves, out.Wrecks = storage.art[:0], storage.leaves[:0], storage.wrecks[:0]
		defer func() { storage.art, storage.leaves, storage.wrecks = out.Art, out.Leaves, out.Wrecks }()
	}
	if c == nil || c.cam == nil {
		return out
	}
	cur := c.presentationFrame()
	if cur == nil {
		return out
	}
	win := c.worldWindow()
	c.list.Reset()
	c.emitWorldBegin()
	spaces := c.list.WorldSpaces()
	if len(spaces) > 0 {
		out.World = spaces[0]
	}
	visit := func(f *frame.FeatureView) {
		is3DO := f.Model != "" && f.Filename == "" || f.Filename == "" && f.SeqName == ""
		if is3DO {
			if source, ok := c.retainedWreckSource(*f); ok {
				out.Wrecks = append(out.Wrecks, source)
			}
		} else if f.IsBurning {
			// Ordinary feature art is neither a classified emitter nor a heat
			// source; leave its body/shadow to the retained world producer.
			// drawFeature receives a copy, as it did before.
			view := *f
			c.drawFeature(&view)
		}
	}
	// Index rather than range: the window rejects most of the map's features
	// before any of their views is copied.
	for i := range cur.Features {
		f := &cur.Features[i]
		if f.Height < 10 && win.admitsCell(f.CX, f.CZ) && featureVisibleForFrame(cur, *f) {
			visit(f)
		}
	}
	c.emitWorldEnd()
	out.Short = c.cloneRetained(storage, func(s *RetainedEffectStorage) *drawlist.List { return &s.short })
	c.list.Reset()
	c.emitWorldBegin()
	// The feature tail within each pass-A row retains source enumeration order;
	// unit ordering in that row does not change feature-to-feature ordering.
	buckets := &c.retainedLightBuckets
	buckets.reset()
	for i := range cur.Features {
		f := &cur.Features[i]
		if f.Height >= 10 && win.admitsCell(f.CX, f.CZ) && tallFeatureVisibleForFrame(cur, *f) {
			buckets.add(worldDrawable{row: featureBucketRow(f.CZ, c.cam.Z), feature: f})
		}
	}
	for _, d := range buckets.ordered() {
		if win.admitsPassARow(d.row) {
			visit(d.feature)
		}
	}
	c.emitWorldEnd()
	out.Ground = c.cloneRetained(storage, func(s *RetainedEffectStorage) *drawlist.List { return &s.ground })
	for _, list := range []*drawlist.List{&out.Short, &out.Ground} {
		list.VisitLightSources(func(s drawlist.Sprite) { out.Art = append(out.Art, s) })
		list.VisitSprites(func(s drawlist.Sprite) { out.Leaves = append(out.Leaves, s) })
	}
	return out
}

func (c *Client) retainedWreckSource(f frame.FeatureView) (drawlist.WreckSource, bool) {
	var g drawlist.ModelGeometry
	c.applyWreckHeat(&g, f)
	if g.WreckEmission == [3]float32{} {
		return drawlist.WreckSource{}, false
	}
	m := c.modelForFeature(f)
	if m == nil || m.compiled == nil {
		return drawlist.WreckSource{}, false
	}
	draw := presentationrender.BuildUnitDrawInto(m.compiled, nil, f.Heading, f.Pitch, f.Bank, frame.UnitView{X: f.X, Y: f.Y, Z: f.Z}, nil, c.borrowDrawScratch())
	if draw == nil {
		return drawlist.WreckSource{}, false
	}
	// Use the same pseudo-unit and packet admission as drawFeatureModel. Merely
	// measuring vertices could lend light to a model whose material walk emits
	// no faces. The feature cache shares that material decision and exact box.
	draw.Structure, draw.KeyPlane = true, true
	packet := c.prepareModelGeometry(draw, 0, c.featureTeamColor(), featurePresentationID(f), modelCursorFeature, nil, 0)
	if packet == nil {
		return drawlist.WreckSource{}, false
	}
	x, y := int(packet.AnchorX-packet.OriginX), int(packet.AnchorY-packet.OriginY)
	return drawlist.WreckSource{Bounds: image.Rect(x, y, x+int(packet.Width), y+int(packet.Height)), Emission: g.WreckEmission, WorldHeight: packet.WorldHeight, Scale: g.WreckHeatScale}, true
}
