package metalhud

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/camera"
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// GlowAtlas is a read-only view of art already packed by PrepareEffectLayers.
// Lookups never change texture bytes, version or the aggregate dirty rectangle.
// Sprite RGB is unpremultiplied displayed palette RGB and alpha is coverage;
// the producer's half-alpha remains separate in Sprite.Kind. Palette tables are
// immutable for an adapter's lifetime: recreate it when the palette changes.
type GlowAtlas struct{ foreground *Foreground }

func (f *Foreground) GlowAtlas() GlowAtlas { return GlowAtlas{foreground: f} }

// Frame returns atlas TEXEL ENDPOINTS [x0,y0,x1,y1] for a replayed GAF leaf.
// Complete composites belong to lighting measurement; glow follows recorded
// ordered leaves instead [DESIGN_GPU_RENDERER §19, §23].
func (a GlowAtlas) Frame(frame *formats.GAFFrame) ([4]float32, error) {
	if a.foreground != nil && frame != nil && len(frame.Subframes) == 0 {
		if r, ok := a.foreground.images[imageKey{gaf: frame}]; ok && r.w > 0 && r.h > 0 {
			return [4]float32{float32(r.x), float32(r.y), float32(r.x + r.w), float32(r.y + r.h)}, nil
		}
	}
	return [4]float32{}, fmt.Errorf("nanolathe: native glow source has no prepared GAF leaf")
}

// Flash returns texel endpoints for the existing generated row raster. Red is
// row/255, alpha is coverage; its emission high lane is min(1+row/30,2)-1,
// retaining the production single-precision addition and subtraction order.
func (a GlowAtlas) Flash(flash drawlist.Flash) ([4]float32, error) {
	if a.foreground != nil && flash.Side > 0 && len(flash.Rows) > 0 {
		if r, ok := a.foreground.effectArt[effectArtKey{rows: &flash.Rows[0], side: flash.Side}]; ok && r.w > 0 && r.h > 0 {
			return [4]float32{float32(r.x), float32(r.y), float32(r.x + r.w), float32(r.y + r.h)}, nil
		}
	}
	return [4]float32{}, fmt.Errorf("nanolathe: native glow source has no prepared calculated flash")
}

// EffectBatch retains the distinction between projectile models, whose native
// producer already owns their bodies, and direct fragment/debris packets.
type EffectBatch struct {
	List             *drawlist.List
	ProjectileModels bool
	ProjectileOrder  []meshscene.EffectProjectileModel
	SourceOnly       bool
	// Labels marks the host's unit-label recording (health bars, group
	// digits) placed in its production slot between two effect layers: plain
	// interface art, so glyphs are admitted and nothing feeds light sources.
	Labels bool
}

type effectArtKey struct {
	rows         *uint8
	model        *formats.GAFFrame
	side, radius int32
	row          uint8
}

// PrepareEffectLayers uses one effects-only Foreground for every layer in a
// presentation. One call aggregates atlas changes; DirtyRect is valid until the
// next Prepare/PrepareEffectLayers. Layers and their nested slices borrow this
// adapter; model packets still borrow the independently owned recorded batches.
// The presentation owner must finish all source gathers and native packing/copy
// before preparing another frame [DESIGN_GPU_RENDERER §2.1, C-G5].
func (f *Foreground) PrepareEffectLayers(groups [][]EffectBatch) ([]meshscene.EffectLayer, meshscene.Texture, uint64, error) {
	f.beginPrepare()
	for len(f.effectLayers) < len(groups) {
		f.effectLayers = append(f.effectLayers, meshscene.EffectLayer{})
	}
	layers := f.effectLayers[:len(groups)]
	for i, batches := range groups {
		start := len(f.quads)
		s := effectSink{Foreground: f, layer: &layers[i], base: start}
		for _, batch := range batches {
			if batch.List == nil {
				continue
			}
			s.projectileModels = batch.ProjectileModels
			s.labels = batch.Labels
			s.projectileOrder, s.projectileAt = batch.ProjectileOrder, 0
			s.source = meshscene.EffectSources{}
			if at := len(s.layer.Sources); at < cap(s.layer.Sources) {
				s.source = s.layer.Sources[:cap(s.layer.Sources)][at]
			}
			batch.List.VisitLightSources(func(c drawlist.Sprite) { s.source.Art = append(s.source.Art, c) })
			if batch.SourceOnly {
				spaces := batch.List.WorldSpaces()
				if len(spaces) > 0 {
					s.source.World = spaces[0]
				}
				batch.List.VisitSprites(func(c drawlist.Sprite) {
					s.source.Sprites = append(s.source.Sprites, c)
					if c.Frame != nil {
						f.image(imageKey{gaf: c.Frame})
					}
				})
			} else {
				batch.List.Replay(&s)
				if batch.ProjectileModels && s.projectileAt != len(batch.ProjectileOrder) {
					s.fail("projectile identity count differs from recorded model commands")
				}
			}
			s.layer.Sources = append(s.layer.Sources, s.source)
		}
		layers[i].Quads = f.quads[start:len(f.quads)]
	}
	return layers, f.atlas, f.version, f.err
}

// Reset only adapter-owned receivers after the previous consumer has copied
// them. Keep nested receiver capacities, but release every recorded model/art
// reference so a smaller next frame cannot retain an old client's batches.
// Inactive slots were already reset when they last appeared in a layer.
func resetEffectLayer(l *meshscene.EffectLayer) {
	l.Quads = nil
	l.Ops = l.Ops[:0]
	for i := range l.Lenses {
		lens := &l.Lenses[i]
		*lens = meshscene.EffectLens{Samples: lens.Samples[:0]}
	}
	l.Lenses = l.Lenses[:0]
	for i := range l.Models {
		model := &l.Models[i]
		*model = meshscene.EffectGeometry{Vertices: model.Vertices[:0]}
	}
	l.Models = l.Models[:0]
	clear(l.RetainedModels)
	l.RetainedModels = l.RetainedModels[:0]
	l.ProjectileOrder = l.ProjectileOrder[:0]
	for i := range l.Sources {
		source := &l.Sources[i]
		clear(source.Art)
		clear(source.Sprites)
		clear(source.Nano)
		clear(source.Flashes)
		*source = meshscene.EffectSources{Art: source.Art[:0], Sprites: source.Sprites[:0],
			Lines: source.Lines[:0], Nano: source.Nano[:0], Flashes: source.Flashes[:0], Halos: source.Halos[:0]}
	}
	l.Sources = l.Sources[:0]
	l.Counts = meshscene.EffectCounts{}
	l.Ground = l.Ground[:0]
	l.Smoke = l.Smoke[:0]
}

type effectSink struct {
	*Foreground
	layer            *meshscene.EffectLayer
	base             int
	projectileModels bool
	projectileOrder  []meshscene.EffectProjectileModel
	projectileAt     int
	labels           bool
	terrain          drawlist.Terrain
	source           meshscene.EffectSources
}

var _ drawlist.Sink = (*effectSink)(nil)
var _ drawlist.WorldSink = (*effectSink)(nil)
var _ drawlist.LensSink = (*effectSink)(nil)

func (s *effectSink) World(c drawlist.WorldSpace) {
	if c.Begin {
		s.source.World = c
	}
	s.Foreground.World(c)
}

func (s *effectSink) quadsFrom(start int) {
	count := len(s.quads) - start
	if count <= 0 {
		return
	}
	first := start - s.base
	ops := &s.layer.Ops
	if len(*ops) > 0 {
		last := &(*ops)[len(*ops)-1]
		if last.Kind == meshscene.EffectOpQuads && last.First+last.Count == first {
			last.Count += count
			return
		}
	}
	*ops = append(*ops, meshscene.EffectOp{Kind: meshscene.EffectOpQuads, First: first, Count: count})
}

func (s *effectSink) Sprite(c drawlist.Sprite) {
	start := len(s.quads)
	if s.labels {
		s.Foreground.Sprite(c)
		s.quadsFrom(start)
		return
	}
	if c.Kind == drawlist.BlitTinted && c.LightingKind == drawlist.SpriteLightingSmoke && c.Frame != nil && c.PCX == nil {
		s.smoke(c)
	} else {
		s.Foreground.Sprite(c)
	}
	s.quadsFrom(start)
	s.layer.Counts.Sprites++
	s.source.Sprites = append(s.source.Sprites, c)
}

// Keep production's RECORD clip and its four receiving corners, rather than
// evaluating a second set of corners after framebuffer clipping (§23.2).
func (s *effectSink) smoke(c drawlist.Sprite) {
	if s.pal == nil {
		return
	}
	f := c.Frame
	r := s.image(imageKey{gaf: f})
	if r.w <= 0 || r.h <= 0 {
		return
	}
	x, y := int(c.X)-int(f.XOffset), int(c.Y)-int(f.YOffset)
	cx, cy, cw, ch := 0, 0, s.cw, s.ch
	if c.HasClip {
		cx, cy, cw, ch = int(c.Clip.X), int(c.Clip.Y), int(c.Clip.W), int(c.Clip.H)
	}
	x0, y0 := max(x, cx, 0), max(y, cy, 0)
	x1, y1 := min(x+int(f.Width), cx+cw, s.cw), min(y+int(f.Height), cy+ch, s.ch)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	const a = float32(atlasSize)
	p := meshscene.EffectSmoke{Quad: meshscene.OverlayQuad{
		Rect:  [4]float32{float32(x0), float32(y0), float32(x1 - x0), float32(y1 - y0)},
		UV:    [4]float32{float32(r.x+x0-x) / a, float32(r.y+y0-y) / a, float32(r.x+x1-x) / a, float32(r.y+y1-y) / a},
		Color: [4]float32{1, 1, 1, .5}},
		Selection: [4]float32{float32(c.X), float32(c.Y), float32(max(f.Width, f.Height)), c.WorldHeight},
		Bounds:    [4]float32{float32(r.x) / a, float32(r.y) / a, float32(r.x+r.w) / a, float32(r.y+r.h) / a}}
	if s.world {
		p.Quad.Rect[0] = p.Quad.Rect[0]*s.scale + s.ox
		p.Quad.Rect[1] = p.Quad.Rect[1]*s.scale + s.oy
		p.Quad.Rect[2] = max(p.Quad.Rect[2]*s.scale, 1)
		p.Quad.Rect[3] = max(p.Quad.Rect[3]*s.scale, 1)
		p.Selection[0] = p.Selection[0]*s.scale + s.ox
		p.Selection[1] = p.Selection[1]*s.scale + s.oy
		p.Selection[2] *= s.scale
		p.Selection[3] *= s.scale
		if s.source.World.Step.Norm() == camera.ViewScaleDetail && s.scale < 1 {
			p.Quad.Params[0] = 1
		}
	}
	// World replay expands subpixel sprite spans to one pixel, after production
	// has evaluated its original clipped receiving corners. Keep that physical
	// span separate so raster expansion does not move the light sample.
	p.Quad.Params[1], p.Quad.Params[2] = float32(x1-x0), float32(y1-y0)
	if s.world {
		p.Quad.Params[1] *= s.scale
		p.Quad.Params[2] *= s.scale
	}
	i := len(s.layer.Smoke)
	s.layer.Smoke = append(s.layer.Smoke, p)
	s.layer.Ops = append(s.layer.Ops, meshscene.EffectOp{Kind: meshscene.EffectOpSmoke, First: i, Count: 1})
}

func (s *effectSink) Line(c drawlist.Line) {
	start := len(s.quads)
	s.Foreground.Line(c)
	s.quadsFrom(start)
	s.layer.Counts.Lines++
	if c.Emissive {
		s.source.Lines = append(s.source.Lines, c)
	}
}

func (s *effectSink) Fill(c drawlist.Fill) {
	start := len(s.quads)
	s.Foreground.Fill(c)
	s.quadsFrom(start)
	s.layer.Counts.Fills++
	if c.Nano {
		s.source.Nano = append(s.source.Nano, c)
	}
}

func (s *effectSink) Points(c drawlist.Points) {
	start := len(s.quads)
	s.Foreground.Points(c)
	s.quadsFrom(start)
}

func (s *effectSink) Model(c drawlist.Model) {
	if s.projectileModels {
		if s.projectileAt >= len(s.projectileOrder) {
			s.fail("projectile model command has no recorded identity")
			return
		}
		identity := s.projectileOrder[s.projectileAt]
		s.projectileAt++
		if identity.ID == 0 || identity.Member > 1 {
			s.fail("invalid recorded projectile identity/member")
			return
		}
		s.layer.Counts.ProjectileModels++
		i := len(s.layer.RetainedModels)
		s.layer.RetainedModels = append(s.layer.RetainedModels, c)
		s.layer.ProjectileOrder = append(s.layer.ProjectileOrder, identity)
		s.layer.Ops = append(s.layer.Ops, meshscene.EffectOp{Kind: meshscene.EffectOpRetainedModel, First: i, Count: 1})
		return
	}
	if c.Geometry == nil {
		s.layer.Counts.Unsupported++
		s.fail("effect model has no device-neutral geometry")
		return
	}
	i := len(s.layer.Models)
	scale, ox, oy := float32(1), float32(0), float32(0)
	if s.world {
		scale, ox, oy = s.scale, s.ox, s.oy
	}
	packet := meshscene.EffectGeometry{Model: c, Scale: scale, OffsetX: ox, OffsetY: oy}
	if i < cap(s.layer.Models) {
		packet.Vertices = s.layer.Models[:cap(s.layer.Models)][i].Vertices
	}
	s.projectModel(&packet)
	s.layer.Models = append(s.layer.Models, packet)
	s.layer.Ops = append(s.layer.Ops, meshscene.EffectOp{Kind: meshscene.EffectOpGeometry, First: i, Count: 1})
	s.layer.Counts.Geometry++
}

// Direct fragments/debris have no subject key, staging or SHD. This native
// inspection lane retains painter order and the production positive-area gate
// and half-texel sampling grid. Quads use projected fan interpolation here;
// QuadSpanFaces reports the missing production two-chain UV mapping explicitly
// (GPU design §11.2). SupersampleIgnored reports the unused doubled raster.
func (s *effectSink) projectModel(out *meshscene.EffectGeometry) {
	g := out.Model.Geometry
	if !g.Eligible || g.Fallback != drawlist.ModelFallbackNone || out.Model.ShadowOnly ||
		g.KeyPlane || g.Reveal != nil || g.Waterline != drawlist.ModelWaterlineNone ||
		g.Digger || g.Cloaked || g.Shadow != nil || len(g.Children) != 0 || len(g.Outline) != 0 ||
		len(g.LiveFaces) != 0 || g.Scale != 1 {
		out.Suppressed = "direct effects require eligible keyless unstaged native geometry"
		return
	}
	for _, face := range g.Faces {
		if face.Shaded {
			out.Suppressed = "SHD effect geometry requires subject raster composition"
			return
		}
		// A winning composition-background write must erase an earlier face
		// before the finished subject is blitted [03 R-REN-03A §1]. Immediate
		// framebuffer triangles cannot represent that subject-local hole.
		if face.Texture == nil && face.Color == 1 {
			out.Suppressed = "model background winner requires subject-local composition"
			return
		}
		if tex := face.Texture; tex != nil {
			for i, index := range tex.Pixels {
				if i < len(tex.Transparent) && tex.Transparent[i] {
					out.Suppressed = "masked model texture requires subject-local composition"
					return
				}
				if index == 1 {
					out.Suppressed = "model background winner requires subject-local composition"
					return
				}
			}
		}
	}
	out.SupersampleIgnored = g.Supersample != nil
	for _, face := range g.Faces {
		v := face.Vertices
		if len(v) < 3 {
			continue
		}
		var area int64
		prev := v[len(v)-1]
		for _, cur := range v {
			area += int64(prev.X)*int64(cur.Y) - int64(prev.Y)*int64(cur.X)
			prev = cur
		}
		if area <= 0 {
			continue
		}
		if len(v) == 4 {
			out.QuadSpanFaces++
		}
		color := s.color(face.Color)
		u, vv := float32(.5/atlasSize), float32(.5/atlasSize)
		bounds := [4]float32{0, 0, 1, 1}
		var r region
		if face.Texture != nil {
			tex := face.Texture
			w, h := int(tex.Width), int(tex.Height)
			if w <= 0 || h <= 0 || int64(w)*int64(h) > int64(len(tex.Pixels)) || len(tex.Subframes) != 0 {
				out.Suppressed = "direct effect texture is not a resolved raster"
				out.Vertices = nil
				return
			}
			// Model rasters use zero-coverage borders, unlike replicated sprite
			// edges. Explicit bounds in the fragment supply that distinction.
			r = s.rowRaster(effectArtKey{model: tex}, w, h, func(x, y int) [4]byte {
				i := y*w + x
				if i < len(tex.Transparent) && tex.Transparent[i] {
					return [4]byte{}
				}
				a, b, c, d := s.pal.RGBA(tex.Pixels[i])
				return [4]byte{a, b, c, d}
			})
			color = [4]float32{1, 1, 1, 1}
			bounds = [4]float32{float32(r.x) / atlasSize, float32(r.y) / atlasSize, float32(r.x+r.w) / atlasSize, float32(r.y+r.h) / atlasSize}
		}
		corner := func(a drawlist.ModelVertex) meshscene.EffectVertex {
			if face.Texture != nil {
				u, vv = (float32(r.x)+float32(a.U))/atlasSize, (float32(r.y)+float32(a.V))/atlasSize
			}
			// Production modelSamplePos biases the raster by half a recording
			// pixel before the world transform [03 R-RAST-01 §1].
			x := (float32(a.X)+float32(g.AnchorX)-float32(g.OriginX)+.5)*out.Scale + out.OffsetX
			y := (float32(a.Y)+float32(g.AnchorY)-float32(g.OriginY)+.5)*out.Scale + out.OffsetY
			return meshscene.EffectVertex{PositionUV: [4]float32{x, y, u, vv}, Color: color, Bounds: bounds}
		}
		for i := 1; i+1 < len(v); i++ {
			out.Vertices = append(out.Vertices, corner(v[0]), corner(v[i]), corner(v[i+1]))
		}
	}
}

// Generated row rasters use the same normalized nearest sampling as the modern
// executor's Flash/Halo quads (§13.11). Red is an exact row byte; transparent
// alpha yields the identity factor, never blackening the destination.
func (s *effectSink) rowRaster(key effectArtKey, w, h int, pixel func(x, y int) [4]byte) region {
	f := s.Foreground
	if f.effectArt == nil {
		f.effectArt = make(map[effectArtKey]region)
	}
	if r, ok := f.effectArt[key]; ok {
		return r
	}
	r := f.allocate(w, h)
	f.write(r, pixel)
	if f.err == nil {
		f.effectArt[key] = r
	}
	return r
}

func (s *effectSink) disc(r region, x0, y0, x1, y1 int32, clip drawlist.Rect) {
	if r.w <= 0 || r.h <= 0 || x0 >= x1 || y0 >= y1 {
		return
	}
	cx0, cy0 := max(x0, clip.X, 0), max(y0, clip.Y, 0)
	cx1, cy1 := min(x1, clip.X+clip.W, int32(s.cw)), min(y1, clip.Y+clip.H, int32(s.ch))
	if cx0 >= cx1 || cy0 >= cy1 {
		return
	}
	kx, ky := float32(r.w)/float32(x1-x0), float32(r.h)/float32(y1-y0)
	u0 := (float32(r.x) + float32(cx0-x0)*kx) / atlasSize
	v0 := (float32(r.y) + float32(cy0-y0)*ky) / atlasSize
	u1 := (float32(r.x) + float32(cx1-x0)*kx) / atlasSize
	v1 := (float32(r.y) + float32(cy1-y0)*ky) / atlasSize
	start := len(s.quads)
	dx0, dy0, dx1, dy1 := float32(cx0), float32(cy0), float32(cx1), float32(cy1)
	world := s.world
	if world {
		dx0, dx1 = dx0*s.scale+s.ox, dx1*s.scale+s.ox
		dy0, dy1 = dy0*s.scale+s.oy, dy1*s.scale+s.oy
	}
	// Unlike isolated points/sprite marks, the production disc quad has no
	// minimum one-pixel span after world scaling (§13.11, §16.3). Preserve a
	// partially clipped rim's own sampling, including a subpixel thin span.
	s.world = false
	s.emit(dx0, dy0, dx1, dy1, u0, v0, u1, v1, [4]float32{1, 1, 1, 1}, true)
	s.world = world
	for i := start; i < len(s.quads); i++ {
		s.quads[i].Params[2] = 1
	}
	s.quadsFrom(start)
}

func (s *effectSink) Flash(c drawlist.Flash) {
	s.layer.Counts.Flashes++
	s.source.Flashes = append(s.source.Flashes, c)
	if c.Side <= 0 || int64(c.Side)*int64(c.Side) > int64(len(c.Rows)) {
		s.layer.Counts.Unsupported++
		s.fail("invalid generated flash raster %dx%d", c.Side, c.Side)
		return
	}
	r := s.rowRaster(effectArtKey{rows: &c.Rows[0], side: c.Side}, int(c.Side), int(c.Side), func(x, y int) [4]byte {
		row := c.Rows[y*int(c.Side)+x]
		if row == drawlist.FlashTransparentRow {
			return [4]byte{}
		}
		return [4]byte{row & 31, 0, 0, 255}
	})
	s.disc(r, c.X+c.Scale.Project(-c.Offset), c.Y+c.Scale.Project(-c.Offset), c.X+c.Scale.Project(c.Side-c.Offset), c.Y+c.Scale.Project(c.Side-c.Offset), c.Clip)
}

func (s *effectSink) Halo(c drawlist.Halo) {
	s.layer.Counts.Halos++
	s.source.Halos = append(s.source.Halos, c)
	if c.Radius <= 0 {
		return
	}
	side := int64(c.Radius)*2 + 1
	if side > atlasSize-2 {
		s.layer.Counts.Unsupported++
		s.fail("halo radius %d exceeds effects atlas", c.Radius)
		return
	}
	key := effectArtKey{radius: c.Radius, row: c.Row}
	if r, ok := s.effectArt[key]; ok {
		s.disc(r, c.X-c.Radius, c.Y-c.Radius, c.X+c.Radius+1, c.Y+c.Radius+1, c.Clip)
		return
	}
	// Expand is the production circle predicate, including its inclusive rim.
	pixels := make([]byte, int(side*side))
	local := c
	local.X, local.Y = c.Radius, c.Radius
	local.Clip = drawlist.Rect{W: int32(side), H: int32(side)}
	local.Expand(func(x, y int32, row uint8) { pixels[int64(y)*side+int64(x)] = 255 })
	r := s.rowRaster(key, int(side), int(side), func(x, y int) [4]byte {
		return [4]byte{c.Row, 0, 0, pixels[y*int(side)+x]}
	})
	s.disc(r, c.X-c.Radius, c.Y-c.Radius, c.X+c.Radius+1, c.Y+c.Radius+1, c.Clip)
}

func (s *effectSink) Lens(c drawlist.Lens) {
	s.layer.Counts.Lenses++
	var out meshscene.EffectLens
	if i := len(s.layer.Lenses); i < cap(s.layer.Lenses) {
		out.Samples = s.layer.Lenses[:cap(s.layer.Lenses)][i].Samples
	}
	r, g, b, _ := s.pal.RGBA(c.Key)
	out.KeyRGB = [3]uint8{r, g, b}
	bound := c.Bounds()
	transform := func(x, y int32) (float32, float32) {
		if s.world {
			return float32(x)*s.scale + s.ox, float32(y)*s.scale + s.oy
		}
		return float32(x), float32(y)
	}
	var rx0, ry0, rx1, ry1 float32
	for y := bound.Y; y < bound.Y+bound.H; y++ {
		for x := bound.X; x < bound.X+bound.W; x++ {
			sx, sy, ok := c.Source(x, y)
			if !ok || (sx == x && sy == y) {
				continue
			}
			dx, dy := transform(x, y)
			ex, ey := transform(x+1, y+1)
			cx, cy := max(dx, float32(c.Clip.X), 0), max(dy, float32(c.Clip.Y), 0)
			ce, cf := min(ex, float32(c.Clip.X+c.Clip.W), float32(s.w)), min(ey, float32(c.Clip.Y+c.Clip.H), float32(s.h))
			if cx >= ce || cy >= cf {
				continue
			}
			tx, ty := transform(sx, sy)
			tx, ty = tx+cx-dx, ty+cy-dy
			if tx < 0 {
				cx -= tx
				tx = 0
			}
			if ty < 0 {
				cy -= ty
				ty = 0
			}
			ce, cf = min(ce, cx+float32(s.w)-tx), min(cf, cy+float32(s.h)-ty)
			if cx >= ce || cy >= cf {
				continue
			}
			if len(out.Samples) == 0 {
				rx0, ry0, rx1, ry1 = tx, ty, tx+ce-cx, ty+cf-cy
			} else {
				rx0, ry0 = min(rx0, tx), min(ry0, ty)
				rx1, ry1 = max(rx1, tx+ce-cx), max(ry1, ty+cf-cy)
			}
			out.Samples = append(out.Samples, meshscene.EffectLensSample{Rect: [4]float32{cx, cy, ce - cx, cf - cy}, Source: [4]float32{tx, ty, ce - cx, cf - cy}})
		}
	}
	out.ReadRect = [4]float32{rx0, ry0, rx1 - rx0, ry1 - ry0}
	i := len(s.layer.Lenses)
	s.layer.Lenses = append(s.layer.Lenses, out)
	s.layer.Ops = append(s.layer.Ops, meshscene.EffectOp{Kind: meshscene.EffectOpLens, First: i, Count: 1})
}

// Unexpected families are errors, not optional Sink extensions that silently
// vanish. The foreground adapter remains strict for the ordinary HUD route.
func (s *effectSink) reject(family string) {
	s.layer.Counts.Unsupported++
	s.fail("unexpected %s command in stock-effects list", family)
}
func (s *effectSink) Clear()           { s.reject("clear") }
func (s *effectSink) Fog(drawlist.Fog) { s.reject("fog") }
func (s *effectSink) Glyphs(c drawlist.Glyphs) {
	if !s.labels {
		s.reject("glyph")
		return
	}
	start := len(s.quads)
	s.Foreground.Glyphs(c)
	s.quadsFrom(start)
}
func (s *effectSink) Surface(drawlist.Surface) { s.reject("surface") }
func (s *effectSink) Cursor(drawlist.Cursor)   { s.reject("cursor") }
func (s *effectSink) Markers(drawlist.Markers) { s.reject("marker") }
