package client

import (
	"sort"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
)

// RetainedMaterialRequest reads a loaded primitive without registering or
// advancing a texture player. Current selects the live/standalone cursor;
// otherwise an active ordinary animation selects frame zero [03 R-REN-03A §5].
// Exactly one subject pointer is supplied by the presentation owner.
type RetainedMaterialRequest struct {
	Piece, Primitive int
	Current          bool
	Frame            *frame.Frame
	Unit             *frame.UnitView
	Feature          *frame.FeatureView
	Projectile       *frame.ProjectileView
	Debris           *frame.DebrisView
}

// RetainedMaterialSelection contains immutable selected pixels, including a
// prepared unit skin. Nil Frame with Skip false selects the physical flat Color.
type RetainedMaterialSelection struct {
	Frame           *formats.GAFFrame
	Color           uint8
	Skip            bool
	Material, Glint uint8
}

// RetainedMaterialResolver borrows one subject's loaded model, selected skin
// and owner selector. Use only during the pinned presentation that prepared it;
// prepare again for each subject draw so ownership/skin/registry changes take
// effect on the next draw. No animation frame or cursor position is cached.
type RetainedMaterialResolver struct {
	client     *Client
	model      *unitModel
	refs       *modelTexRefs
	skin       *ModelSkin
	selector   teamColor
	projectile bool
}

// PrepareRetainedMaterial resolves subject-wide inputs once. The immutable
// texture-reference table is the production generation-checked table; each
// primitive still reads its selected cached/live cursor [I6, GPU design §38].
func (c *Client) PrepareRetainedMaterial(q RetainedMaterialRequest) RetainedMaterialResolver {
	if c == nil || c.modelTextures == nil || c.modelTextures.standalone {
		return RetainedMaterialResolver{}
	}
	r := RetainedMaterialResolver{client: c}
	switch {
	case q.Unit != nil:
		r.model = c.modelForUnit(*q.Unit)
		r.skin = c.selectedModelSkin(q.Unit.InstanceID, q.Unit.Owner)
		r.selector = unitTeamColor(*q.Unit)
	case q.Feature != nil:
		r.model = c.modelForFeature(*q.Feature)
		if q.Frame != nil && q.Frame.Players[0].Present {
			r.selector = teamColor{index: q.Frame.Players[0].Logo, known: true}
		}
	case q.Projectile != nil:
		r.model, r.projectile = c.modelForProjectile(*q.Projectile), true
	case q.Debris != nil:
		r.model = c.modelForDebris(*q.Debris)
		r.selector = teamColor{index: q.Debris.OwnerColor, known: q.Debris.OwnerColorKnown}
	default:
		return RetainedMaterialResolver{}
	}
	if r.model != nil {
		r.refs = c.modelTexRefs(r.model.compiled)
	}
	return r
}

// ResolveRetainedMaterial retains the one-primitive compatibility API, using
// the same prepared descriptor and material selection as the subject path.
func (c *Client) ResolveRetainedMaterial(q RetainedMaterialRequest) (RetainedMaterialSelection, bool) {
	return c.PrepareRetainedMaterial(q).Resolve(q.Piece, q.Primitive, q.Current)
}

// Resolve selects one primitive through the existing phase-7 cursor and
// post-selection skin. It never advances or registers a texture player.
func (r RetainedMaterialResolver) Resolve(pieceIndex, primitiveIndex int, current bool) (RetainedMaterialSelection, bool) {
	c, m, skin, selector, projectile := r.client, r.model, r.skin, r.selector, r.projectile
	if m == nil || m.compiled == nil || pieceIndex < 0 || pieceIndex >= len(m.compiled.Pieces) {
		return RetainedMaterialSelection{}, false
	}
	piece := &m.compiled.Pieces[pieceIndex]
	if primitiveIndex < 0 || primitiveIndex >= len(piece.Primitives) {
		return RetainedMaterialSelection{}, false
	}
	pr := &piece.Primitives[primitiveIndex]
	out := RetainedMaterialSelection{Color: uint8(pr.ColorIndex)}
	ref, resolved := c.texRefAt(r.refs, pieceIndex, primitiveIndex, pr.TextureName)
	if pr.IsColored&1 == 0 {
		if len(pr.VertexIndices) != 4 || pr.TextureName == "" {
			out.Skip = true
			return out, true
		}
		if !resolved {
			out.Color = 0xd1 // Established unresolved-texture placeholder [03 §2.4.1].
			return out, true
		}
		switch ref.kind {
		case texAnimated:
			out.Frame = c.modelTextures.animatedFrameSelected(m.compiled, pieceIndex, primitiveIndex, ref, !current)
		case texTeam:
			if projectile {
				out.Frame = ref.frame // Standalone projectiles have no colour branch [03 R-COMP-02 §6].
			} else {
				out.Frame = teamTextureFrame(ref, selector)
			}
		default:
			out.Frame = ref.frame
		}
		if out.Frame == nil {
			out.Skip = true
			return out, true
		}
		if skin != nil && ref.kind != texTeam {
			key := c.modelNameKey(pr.TextureName)
			if replacement := skin.frames[key]; ref.kind == texStatic && replacement != nil {
				out.Frame = replacement
			} else if replacement := skin.generated[key][out.Frame]; replacement != nil {
				out.Frame = replacement
			}
		}
		if projectile {
			var ok bool
			out.Frame, ok = out.Frame.DirectRaster()
			out.Skip = !ok
		}
		out.Material, out.Glint = ref.materialAnnotation(pr.TextureName), ref.glintAnnotation(pr.TextureName)
	} else if pr.TextureName != "" && !resolved {
		out.Color = 0xd1
	} else if skin != nil && skin.flat != nil {
		out.Color = skin.flat[out.Color]
	}
	return out, true
}

// RetainedMaterialFrames inventories potential immutable source and installed
// skin frames before atlas upload. Stable traversal preserves source order; the
// read does not bind a primitive or change the phase-7 registry [03 R-CRD-005 §1].
func (c *Client) RetainedMaterialFrames() []*formats.GAFFrame {
	if c == nil || c.modelTextures == nil {
		return nil
	}
	var out []*formats.GAFFrame
	seen := make(map[*formats.GAFFrame]bool)
	appendFrame := func(f *formats.GAFFrame) {
		if f != nil && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	refs := make(map[string]texRef)
	for _, bank := range []map[string]texRef{c.modelTextures.primary, c.modelTextures.logos} {
		for name, ref := range bank {
			refs[ref.key+"|"+name] = ref
		}
	}
	keys := make([]string, 0, len(refs))
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ref := refs[key]
		appendFrame(ref.frame)
		if ref.entry != nil {
			for _, f := range ref.entry.Frames {
				appendFrame(f.Frame)
			}
		}
	}
	base := append([]*formats.GAFFrame(nil), out...)
	skins := make([]string, 0, len(c.modelSkins))
	for name := range c.modelSkins {
		skins = append(skins, name)
	}
	sort.Strings(skins)
	for _, name := range skins {
		skin := c.modelSkins[name]
		if skin == nil {
			continue
		}
		keys = keys[:0]
		for key := range skin.frames {
			keys = append(keys, key)
		}
		for key := range skin.generated {
			if skin.frames[key] == nil {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			appendFrame(skin.frames[key])
			for _, source := range base {
				appendFrame(skin.generated[key][source])
			}
		}
	}
	// A standalone projectile may use the direct view of an otherwise shared
	// immutable frame. Include that pointer so lookup never synthesizes pixels.
	base = append(base[:0], out...)
	for _, f := range base {
		if direct, ok := f.DirectRaster(); ok {
			appendFrame(direct)
		}
	}
	return out
}

// RetainedMaterialKey names every subject-wide input Resolve reads. Two
// resolvers with equal keys select identical primitives for every piece,
// primitive and cursor lane, unless the model has an animated texture.
type RetainedMaterialKey struct {
	model      *unitModel
	refs       *modelTexRefs
	skin       *ModelSkin
	selector   teamColor
	projectile bool
}

// CacheKey reports the resolver's key, and false when a selection can change
// without the key changing: an animated texture's cursor advances between
// presentations, and an unprepared resolver has no table to key. The texture
// table is per index generation, so a reload yields a new key.
func (r RetainedMaterialResolver) CacheKey() (RetainedMaterialKey, bool) {
	if r.client == nil || r.model == nil || r.model.compiled == nil || r.refs == nil || r.refs.animated {
		return RetainedMaterialKey{}, false
	}
	return RetainedMaterialKey{model: r.model, refs: r.refs, skin: r.skin, selector: r.selector, projectile: r.projectile}, true
}
