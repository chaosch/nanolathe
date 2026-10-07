package meshscene

import (
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/palette"
)

// ModelMaterialSource borrows the production presentation owner's immutable
// art and selected primitive materials. Resolve never advances/registers a
// texture player. Frames must include every possible selected skin/animation
// frame before native atlas upload [03 R-CRD-005 §1][DESIGN_GPU_RENDERER §38].
type ModelMaterialSource struct {
	Frames  []*formats.GAFFrame
	Resolve func(ModelMaterialRequest) (ModelMaterialSelection, bool)
}

type ModelMaterialRequest struct {
	Model            string
	Piece, Primitive int
	Current          bool
	Frame            *frame.Frame
	Unit             *frame.UnitView
	Feature          *frame.FeatureView
	Projectile       *frame.ProjectileView
	Debris           *frame.DebrisView
}

type ModelMaterialSelection struct {
	Frame           *formats.GAFFrame
	Color           uint8
	Skip            bool
	Material, Glint uint8
}

// ModelMaterialSlot maps one retained primitive to its native material and
// mesh-local selector. Vertex.Material identifies Scene.Materials; that
// material's Reserved word is the local selector index, preserving both ABIs.
type ModelMaterialSlot struct {
	Piece, Primitive int
	Material         uint32
}

// NativeModelMaterial is 48 bytes. Params selects skip=0, texture=1, flat=2,
// followed by curated finish and glint scale. UV contains atlas corner centres;
// Color is straight RGBA. These are detached presentation selections [I6].
type NativeModelMaterial struct {
	UV, Color, Params [4]float32
}

func modelFrameRGBA(f *formats.GAFFrame, pal *palette.Tables) []byte {
	w, h := int(f.Width), int(f.Height)
	rgba := make([]byte, w*h*4)
	for i, index := range f.Pixels {
		if i >= w*h {
			break
		}
		// Retain the physical index, so BLUE resolves the authored table before shade.
		rgba[4*i] = index
		if i >= len(f.Transparent) || !f.Transparent[i] {
			rgba[4*i+3] = 255
		}
	}
	return rgba
}

func (s *battleSource) applyModelMaterials(instance *Instance, q ModelMaterialRequest) {
	if s.materialAtlas == nil || instance.Mesh < 0 || instance.Mesh >= len(s.scene.Meshes) {
		return
	}
	mesh := &s.scene.Meshes[instance.Mesh]
	m := s.models[instance.Mesh]
	q.Model = m.Name
	instance.Materials = make([]NativeModelMaterial, len(mesh.MaterialSlots))
	for i, slot := range mesh.MaterialSlots {
		q.Piece, q.Primitive = slot.Piece, slot.Primitive
		q.Current = q.Projectile != nil || q.Debris != nil
		if q.Unit != nil {
			// The retained path combines cached and live pieces. Completed live
			// pieces read their existing cursor; construction keeps frame zero
			// [03 R-REN-03A §5]. Historical cached/direct lane staging remains
			// an explicit prototype limitation, not a second cursor lifecycle.
			for _, p := range q.Unit.Pieces {
				if p.Index == slot.Piece || p.Name != "" && canonicalTexture(p.Name) == canonicalTexture(m.Pieces[slot.Piece].Name) {
					q.Current = p.DontCache && q.Unit.BuildRemaining <= 0
				}
			}
		}
		selection := s.baseModelMaterial(q)
		if s.materialSource != nil && s.materialSource.Resolve != nil {
			if selected, ok := s.materialSource.Resolve(q); ok {
				selection = selected
			}
		}
		out := &instance.Materials[i]
		if selection.Skip {
			continue
		}
		out.Color = [4]float32{1, 1, 1, 1}
		out.Params = [4]float32{2, float32(selection.Material), 1, float32(selection.Color)}
		if selection.Glint != 0 {
			out.Params[2] = float32(selection.Glint-1) / 100
		}
		region := s.materialAtlas.white
		if selection.Frame != nil {
			var ok bool
			region, ok = s.materialAtlas.byFrame[selection.Frame]
			if !ok {
				// TODO(question): support reloaded presentation skin atlases; a
				// frame not prepared before upload is omitted, never substituted.
				*out = NativeModelMaterial{}
				s.materialFramesMissing++
				continue
			}
			out.Params[0] = 1
		} else {
			c := s.palette.Base[selection.Color]
			out.Color = [4]float32{float32(c[0]) / 255, float32(c[1]) / 255, float32(c[2]) / 255, 1}
		}
		a, b := region.uv([2]float32{}, s.materialAtlas.uvWidth, s.materialAtlas.uvHeight), region.uv([2]float32{1, 1}, s.materialAtlas.uvWidth, s.materialAtlas.uvHeight)
		out.UV = [4]float32{a[0], a[1], b[0], b[1]}
	}
}

func (s *battleSource) baseModelMaterial(q ModelMaterialRequest) ModelMaterialSelection {
	pr := s.models[s.modelIndices[battleModelKey(q.Model)]].Pieces[q.Piece].Primitives[q.Primitive]
	out := ModelMaterialSelection{Color: uint8(pr.ColorIndex)}
	if pr.IsColored&1 != 0 {
		if pr.TextureName != "" && len(s.materialAtlas.framesByName[canonicalTexture(pr.TextureName)]) == 0 {
			out.Color = 0xd1
		}
		return out
	}
	frames := s.materialAtlas.framesByName[canonicalTexture(pr.TextureName)]
	if len(frames) == 0 {
		out.Color = 0xd1
		return out
	}
	index := 0
	material := s.materialAtlas.materials[s.materialAtlas.materialByName[canonicalTexture(pr.TextureName)]]
	if material.Kind == 1 && q.Projectile == nil {
		known := false
		switch {
		case q.Unit != nil:
			index, known = int(q.Unit.OwnerColor), q.Unit.OwnerColorKnown
		case q.Feature != nil && q.Frame != nil:
			index, known = int(q.Frame.Players[0].Logo), q.Frame.Players[0].Present
		case q.Debris != nil:
			index, known = int(q.Debris.OwnerColor), q.Debris.OwnerColorKnown
		}
		if !known || index >= len(frames) {
			out.Skip = true
			return out
		}
	}
	// No detached source can answer the production primitive cursor. Retain
	// frame zero only as the standalone prototype's documented fallback.
	out.Frame = frames[index]
	return out
}

func (s *battleSource) applyStandaloneMaterials(pub *battlePublication, current *frame.Frame) {
	byOffset := make(map[int]*Instance, len(pub.frame.Instances))
	for i := range pub.frame.Instances {
		instance := &pub.frame.Instances[i]
		byOffset[instance.PoseOffset] = instance
	}
	for i := range current.Projectiles {
		p := &current.Projectiles[i]
		if span, ok := pub.byID[battleEntityKey{kind: 3, id: p.PresentationID}]; ok {
			if instance := byOffset[span.offset]; instance != nil {
				s.applyModelMaterials(instance, ModelMaterialRequest{Frame: current, Projectile: p})
			}
		}
	}
	for i := range current.Debris {
		d := &current.Debris[i]
		if span, ok := pub.byID[battleEntityKey{kind: 4, id: uint64(d.Slot)}]; ok {
			if instance := byOffset[span.offset]; instance != nil {
				s.applyModelMaterials(instance, ModelMaterialRequest{Frame: current, Debris: d})
			}
		}
	}
}

// ApplyMaterials supplies the same material selection for native projectile
// and detached-debris producers. Call before publishing their immutable frame.
func (r *RetainedBattle) ApplyMaterials(instance *Instance, q ModelMaterialRequest) {
	if r != nil && r.source != nil && instance != nil {
		r.source.applyModelMaterials(instance, q)
	}
}
