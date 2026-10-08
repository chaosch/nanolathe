package meshscene

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/frame"
)

// ShadowSubject is 96 bytes: five float4 groups and one uint4. Screen is the
// clipped commit rectangle. Body/Mask are atlas rectangles. Placement carries
// body screen origin and the mobile shadow's screen displacement (or the
// projected mask screen origin for structures). Params is
// kind (1 quarter projection, 2 silhouette, 3 aircraft), clip key, radius and
// physical pixels per world unit. Order is model commit slot, phase, rank and
// source instance. Shadows resolve immediately before that model's body.
type ShadowSubject struct {
	Screen, Body, Mask, Params, Placement [4]float32
	Order                                 [4]uint32
}

// ShadowDraw is 16 bytes; instance remains an original LiveFrame index until
// the native packer remaps it. Slot is one-based; Subject is zero-based.
type ShadowDraw struct{ Mesh, Instance, Slot, Subject uint32 }
type ShadowPolicy struct {
	Rules               ModelRules
	Clearance, Softness float32
	ClipKey             uint8
	Supersample         bool
	Tint                [3]float32
}
type ShadowComposition struct {
	Topology                  []Mesh // immutable authored face topology, aligned to Scene.Meshes
	Slots                     []ModelSlot
	Subjects                  []ShadowSubject
	BodyDraws, ProjectedDraws []ShadowDraw
	BodyRules                 []ModelRules // rules differing from the instance's ModelRules: solo child key planes
	BodyRuleSlots             []uint32     // aligned to BodyDraws: 0 is the instance's ModelRules, n is BodyRules[n-1]
	PieceFlags                []uint32     // aligned to poses; bit zero excludes DontCache structure pieces
	AtlasWidth, AtlasHeight   int
	Tint                      [4]float32 // physical palette index zero RGB, sample-block size (1 or 2)
	Water                     [4]float32 // existing production phase, integrated drift X/Z, non-lava mask step
	SubjectPixels             []uint64
	TotalPixels               uint64 // committed screen rectangles
	AtlasPixels               uint64 // body source plus projected-mask rectangle area
}

// ShadowComposer keeps immutable topology and reusable bounded atlas scratch
// on the presentation owner. No mesh vertices are transformed on the CPU.
type ShadowComposer struct {
	bounds     *ModelComposer
	bodyBounds *ModelComposer
	packer     *ModelComposer
	result     ShadowComposition
	order      []ModelOrder
	indices    map[modelInstanceKey]int
	members    map[uint32][]int
}

func NewShadowComposer(retained *RetainedBattle) *ShadowComposer {
	c := &ShadowComposer{indices: make(map[modelInstanceKey]int), members: make(map[uint32][]int), packer: &ModelComposer{}}
	if retained == nil || retained.source == nil {
		return c
	}
	scene := &Scene{Meshes: make([]Mesh, len(retained.source.models))}
	// Shadow dispatch accepts every valid authored polygon independently of
	// body textures. DontCache is dynamic and supplied separately [03 R-REN-03D §2].
	for mi, m := range retained.source.models {
		mesh := &scene.Meshes[mi]
		mesh.Name = m.Name
		mesh.Pieces = len(m.Pieces)
		for pi := len(m.Pieces) - 1; pi >= 0; pi-- {
			piece := m.Pieces[pi]
			for pri, p := range piece.Primitives {
				if piece.Selection && pri == 0 || len(p.VertexIndices) < 3 {
					continue
				}
				valid := true
				for _, v := range p.VertexIndices {
					if int(v) >= len(piece.Vertices) {
						valid = false
						break
					}
				}
				if !valid {
					continue
				}
				base := uint32(len(mesh.Vertices))
				for _, at := range p.VertexIndices {
					v := piece.Vertices[at]
					mesh.Vertices = append(mesh.Vertices, Vertex{Position: [3]float32{float32(v[0]) / 65536, float32(v[1]) / 65536, float32(v[2]) / 65536}, Piece: uint32(pi)})
				}
				for i := 1; i < len(p.VertexIndices)-1; i++ {
					mesh.Indices = append(mesh.Indices, base, base+uint32(i), base+uint32(i+1))
				}
			}
		}
	}
	c.result.Topology = scene.Meshes
	c.bounds = NewModelComposer(scene)
	c.bodyBounds = NewModelComposer(retained.source.scene)
	return c
}

// Prepare uses the same admitted production order as ModelComposer. Keyed
// children cast from their solo sources before their parent's group shadow
// [03 R-REN-03A §4]. Results are borrowed until the next Prepare.
func (c *ShadowComposer) Prepare(r *RetainedBattle, live *LiveFrame, current *frame.Frame, width, height int, order []ModelOrder, policy func(kind uint8, id uint64) ShadowPolicy) (ShadowComposition, error) {
	fail := func(reason string) (ShadowComposition, error) {
		return ShadowComposition{}, fmt.Errorf("nanolathe: shadow composition failed: logical path retained subjects, providers searched [production policy retained topology], expected %s", reason)
	}
	if c == nil || c.bounds == nil || r == nil || r.previous == nil || live == nil || current == nil || policy == nil || width <= 0 || height <= 0 || live.Tick != current.Tick || r.previous.frame.Tick != current.Tick || live.Camera[2] <= 0 || live.Alpha < 0 || live.Alpha > 1 {
		return fail("matching retained publication, camera and shadow policy")
	}
	for _, v := range live.Camera {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fail("finite camera")
		}
	}
	if math.IsNaN(float64(live.Alpha)) {
		return fail("finite interpolation")
	}
	out := &c.result
	out.Slots = out.Slots[:0]
	out.Subjects = out.Subjects[:0]
	out.BodyDraws = out.BodyDraws[:0]
	out.ProjectedDraws = out.ProjectedDraws[:0]
	out.BodyRules = out.BodyRules[:0]
	out.BodyRuleSlots = out.BodyRuleSlots[:0]
	out.SubjectPixels = out.SubjectPixels[:0]
	out.TotalPixels = 0
	out.AtlasPixels = 0
	out.AtlasWidth = 0
	out.AtlasHeight = 0
	out.Tint = [4]float32{}
	out.Water = [4]float32{}
	out.PieceFlags = resizeComposition(out.PieceFlags, len(live.Current))
	clear(out.PieceFlags)
	clear(c.indices)
	for slot, m := range c.members {
		c.members[slot] = m[:0]
	}
	if len(live.ModelRules) != len(live.Instances) || len(live.Composition.InstanceRanks) != len(live.Instances) || len(live.Composition.InstanceSlots) != len(live.Instances) {
		return fail("model composition aligned to instances")
	}
	for i, in := range live.Instances {
		if in.Mesh < 0 || in.Mesh >= len(c.bounds.bounds) || in.PoseOffset < 0 || in.PoseOffset+len(c.bounds.bounds[in.Mesh]) > len(live.Current) || in.PoseOffset+len(c.bounds.bounds[in.Mesh]) > len(live.Previous) {
			return fail("valid retained mesh and pose span")
		}
		c.indices[modelInstanceKey{in.Mesh, in.PoseOffset}] = i
		slot := live.Composition.FinalSlotForInstance(i)
		if slot != 0 {
			c.members[slot] = append(c.members[slot], i)
		}
	}
	// Script lane aliases use the same model-index/name mapping as pose composition.
	for ui := range current.Units {
		u := &current.Units[ui]
		span, ok := r.previous.byID[battleEntityKey{1, u.InstanceID}]
		if !ok {
			continue
		}
		m := r.source.models[span.mesh]
		for _, p := range u.Pieces {
			index := p.Index
			if p.Name != "" {
				index = -1
				for i, piece := range m.Pieces {
					if strings.EqualFold(piece.Name, p.Name) {
						index = i
						break
					}
				}
			}
			if index >= 0 && index < span.count {
				if p.DontCache {
					out.PieceFlags[span.offset+index] = 1
				} else {
					out.PieceFlags[span.offset+index] = 0
				}
			}
		}
	}
	c.packer.work = c.packer.work[:0]
	c.packer.composition.AtlasWidth = 0
	c.packer.composition.AtlasHeight = 0
	for slot := range c.members {
		slices.SortStableFunc(c.members[slot], func(a, b int) int {
			return cmp.Compare(live.Composition.InstanceRanks[a], live.Composition.InstanceRanks[b])
		})
	}
	// Production model ranks already preserve row barriers. A keyed carrier
	// records each child's shadow-only command before the carrier command.
	c.order = append(c.order[:0], order...)
	entries := c.order
	slices.SortStableFunc(entries, func(a, b ModelOrder) int {
		if n := cmp.Compare(a.Phase, b.Phase); n != 0 {
			return n
		}
		return cmp.Compare(a.Rank, b.Rank)
	})
	for at := 0; at < len(entries); at++ {
		e := entries[at]
		if e.GroupID != 0 && e.ID == e.GroupID {
			end := at + 1
			for end < len(entries) && entries[end].GroupKind == e.GroupKind && entries[end].GroupID == e.GroupID {
				end++
			}
			if end > at+1 {
				copy(entries[at:end-1], entries[at+1:end])
				entries[end-1] = e
				e = entries[at]
			}
		}
		if e.Sprite || e.Kind != 1 && e.Kind != 2 {
			continue
		}
		keyID := e.ID
		if e.Kind == 2 {
			keyID = uint64(e.FeatureIndex) + 1
		}
		span, ok := r.previous.byID[battleEntityKey{e.Kind, keyID}]
		if !ok {
			continue
		}
		i, ok := c.indices[modelInstanceKey{span.mesh, span.offset}]
		if !ok {
			continue
		}
		commit := live.Composition.FinalSlotForInstance(i)
		if commit == 0 {
			// TODO(question): support admitted shadow-only subjects whose body
			// is outside the viewport. An ordered shadow-only native paint
			// entry, instead of a model-slot attachment, would settle this.
			continue
		}
		p := policy(e.Kind, keyID)
		if p.Rules.Shadow[0] < 0.5 {
			continue
		}
		out.Tint = [4]float32{p.Tint[0], p.Tint[1], p.Tint[2], 1}
		if p.Supersample {
			out.Tint[3] = 2
		}
		members := []int{i}
		if e.GroupID != 0 && e.ID == e.GroupID {
			members = c.members[commit]
		}
		body := [4]int{}
		bodyValid := false
		for _, member := range members {
			rect, ok := c.rectangle(live.Instances[member], live, width, height, p.Rules, false)
			if ok {
				if !bodyValid {
					body = rect
					bodyValid = true
				} else {
					body = [4]int{min(body[0], rect[0]), min(body[1], rect[1]), max(body[2], rect[2]), max(body[3], rect[3])}
				}
			}
		}
		if !bodyValid {
			continue
		}
		bodySlot := c.addSlot(body)
		subject := uint32(len(out.Subjects))
		kind := p.Rules.Shadow[0]
		clip := float32(p.ClipKey)
		if p.Rules.Clip[2] > 0.5 {
			clip = p.Rules.Clip[3]
		} else if p.Rules.Clip[0] > 0.5 {
			clip = p.Rules.Clip[1]
		}
		origin := p.Rules.Shadow[2]
		for piece := 0; piece < span.count; piece++ {
			a, b := live.Previous[span.offset+piece], live.Current[span.offset+piece]
			if a[11] > .5 && b[11] > .5 {
				origin = a[7] + (b[7]-a[7])*live.Alpha
				break
			}
		}
		scale := live.Camera[2]
		delta := [2]float32{5 * scale, (origin - p.Rules.Shadow[1]) * 0.5 * scale}
		screen := body
		radius := float32(0)
		maskSlot := uint32(0)
		if kind < 1.5 {
			rect, ok := c.rectangle(live.Instances[i], live, width, height, p.Rules, true)
			if !ok {
				continue
			}
			screen = rect
			maskSlot = c.addSlot(rect)
		} else {
			screen = [4]int{int(math.Floor(float64(float32(body[0]) + delta[0]))), int(math.Floor(float64(float32(body[1]) + delta[1]))), int(math.Ceil(float64(float32(body[2]) + delta[0]))), int(math.Ceil(float64(float32(body[3]) + delta[1])))}
			if p.Clearance > 0 && p.Softness > 0 && clip == 0 {
				kind = 3
				t := min(max((p.Clearance-120)/80, 0), 1)
				radius = (min(p.Clearance/60, 3) + 1.5*t*t*(3-2*t)) * scale * p.Softness
				// Convert the production logical-pixel margin to this fixed 2x host.
				margin := int(math.Ceil(float64(radius/2+scale+1))) * 2
				screen = [4]int{screen[0] - margin, screen[1] - margin, screen[2] + margin, screen[3] + margin}
			}
		}
		screen = [4]int{max(screen[0], 0), max(screen[1], 0), min(screen[2], width), min(screen[3], height)}
		if screen[2] <= screen[0] || screen[3] <= screen[1] {
			continue
		}
		s := ShadowSubject{Screen: shadowRect(screen), Params: [4]float32{kind, clip, radius, scale}, Placement: [4]float32{float32(body[0]), float32(body[1]), delta[0], delta[1]}, Order: [4]uint32{commit, e.Phase, e.Rank, uint32(i)}}
		if maskSlot != 0 {
			maskRect := out.Slots[maskSlot-1].Screen
			s.Placement[2], s.Placement[3] = maskRect[0], maskRect[1]
			out.ProjectedDraws = append(out.ProjectedDraws, ShadowDraw{uint32(span.mesh), uint32(i), maskSlot, subject})
		}
		s.Body[0] = float32(bodySlot)
		s.Mask[0] = float32(maskSlot) // patched after packing
		out.Subjects = append(out.Subjects, s)
		pixels := uint64(screen[2]-screen[0]) * uint64(screen[3]-screen[1])
		out.SubjectPixels = append(out.SubjectPixels, pixels)
		out.TotalPixels += pixels
		for _, member := range members {
			in := live.Instances[member]
			rules := p.Rules
			own := member < len(live.ModelRules)
			if own {
				rules = live.ModelRules[member]
			}
			child := e.GroupID != 0 && e.ID != e.GroupID
			if child {
				rules.Reveal[3] = 1
				// Production child's shadow-only source precedes carrier
				// water/Digger final passes (model_staging.deferChildFinalPasses).
				rules.Clip[0], rules.Clip[1], rules.Clip[2] = 0, 0, 0
			}
			// TODO(question): retained grouped bodies still inherit the native
			// cache/direct and signed wrapped child-key approximations. The
			// production staged solo/group lane would settle full parity.
			out.BodyDraws = append(out.BodyDraws, ShadowDraw{uint32(in.Mesh), uint32(member), bodySlot, subject})
			// The native host already holds every instance's ModelRules; only
			// rules that differ travel with the shadows.
			slot := uint32(0)
			if child || !own {
				out.BodyRules = append(out.BodyRules, rules)
				slot = uint32(len(out.BodyRules))
			}
			out.BodyRuleSlots = append(out.BodyRuleSlots, slot)
		}
	}
	c.packer.composition.Slots = out.Slots
	c.packer.composition.TotalPixels = out.AtlasPixels
	if err := c.packer.pack(); err != nil {
		return fail(err.Error())
	}
	out.Slots = c.packer.composition.Slots
	out.AtlasWidth = c.packer.composition.AtlasWidth
	out.AtlasHeight = c.packer.composition.AtlasHeight
	for i := range out.Subjects {
		s := &out.Subjects[i]
		s.Body = out.Slots[uint32(s.Body[0])-1].Atlas
		if s.Mask[0] > 0 {
			s.Mask = out.Slots[uint32(s.Mask[0])-1].Atlas
		}
	}
	if r.source.scene.WaterMaskStep > 0 && !r.source.scene.WaterLava {
		out.Water[3] = float32(r.source.scene.WaterMaskStep)
		if live.Water.Mask[3] > 0.5 && live.Water.Controls[0] > 0.5 {
			copy(out.Water[:3], live.Water.Phase[:3])
		}
	}
	return *out, nil
}
func shadowRect(r [4]int) [4]float32 {
	return [4]float32{float32(r[0]), float32(r[1]), float32(r[2] - r[0]), float32(r[3] - r[1])}
}
func (c *ShadowComposer) addSlot(r [4]int) uint32 {
	raster := int(c.result.Tint[3])
	if raster < 1 {
		raster = 1
	}
	c.result.AtlasPixels += uint64(r[2]-r[0]) * uint64(r[3]-r[1]) * uint64(raster*raster)
	c.result.Slots = append(c.result.Slots, ModelSlot{Screen: shadowRect(r), Params: [4]float32{1}})
	c.packer.work = append(c.packer.work, modelSlotWork{x0: 0, y0: 0, x1: (r[2] - r[0]) * raster, y1: (r[3] - r[1]) * raster})
	return uint32(len(c.result.Slots))
}
func (c *ShadowComposer) rectangle(in Instance, live *LiveFrame, width, height int, rules ModelRules, projected bool) ([4]int, bool) {
	loX, loY, hiX, hiY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	scale, alpha := float64(live.Camera[2]), float64(live.Alpha)
	bounds := c.bounds.bounds[in.Mesh]
	if !projected {
		bounds = c.bodyBounds.bounds[in.Mesh]
	}
	for piece, bound := range bounds {
		if !bound.valid {
			continue
		}
		a, b := live.Previous[in.PoseOffset+piece], live.Current[in.PoseOffset+piece]
		if a[15] < 0.5 || b[15] < 0.5 || projected && c.result.PieceFlags[in.PoseOffset+piece] != 0 {
			continue
		}
		x := float64(a[12]) + (float64(b[12])-float64(a[12]))*alpha
		y := float64(a[13]) + (float64(b[13])-float64(a[13]))*alpha
		z := float64(a[14]) + (float64(b[14])-float64(a[14]))*alpha
		rx, ry := bound.radius, bound.radius*math.Sqrt(1.25)
		if projected {
			origin := float64(rules.Shadow[2])
			if a[11] > .5 && b[11] > .5 {
				origin = float64(a[7]) + (float64(b[7])-float64(a[7]))*alpha
			}
			q := (y - origin) / 4
			x += q + 5
			z -= q
			y = float64(rules.Shadow[1])
			rx = bound.radius*math.Sqrt(1.0625) + 1
			ry = rx
		}
		sx := (x-float64(live.Camera[0]))*scale + float64(width)/2
		sy := (z-y/2-float64(live.Camera[1]))*scale + float64(height)/2
		loX, loY, hiX, hiY = min(loX, sx-rx*scale), min(loY, sy-ry*scale), max(hiX, sx+rx*scale), max(hiY, sy+ry*scale)
	}
	if math.IsInf(loX, 1) || math.IsNaN(loX+loY+hiX+hiY) || math.IsInf(loX+loY+hiX+hiY, 0) {
		return [4]int{}, false
	}
	// Two physical pixels represent one production logical pixel. Round boxes
	// outward to that grid so the four-sample resolves cannot read a neighbour.
	return [4]int{int(math.Floor(loX/2)*2) - 2, int(math.Floor(loY/2)*2) - 2, int(math.Ceil(hiX/2)*2) + 2, int(math.Ceil(hiY/2)*2) + 2}, true
}
