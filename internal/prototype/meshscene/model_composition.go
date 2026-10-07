package meshscene

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/nanolathe-gg/nanolathe/internal/frame"
)

// ModelSlot is the native 48-byte placement record. Both rectangles use
// physical pixels and describe the same image at one texel per screen pixel.
// Params contains opacity, production body phase, and two reserved zero lanes.
type ModelSlot struct {
	Screen [4]float32
	Atlas  [4]float32
	Params [4]float32
}

// ModelOrder is the neutral identity bridge from the production painter walk.
// Kind is unit=1, feature=2, projectile=3, debris=4. GroupKind/GroupID identify
// an explicitly staged production subject; zero falls back to Instance.Subject.
// Phase is short features=0, ground rows=1, effects=2, airborne rows=3.
type ModelOrder struct {
	Kind, GroupKind uint8
	ID, GroupID     uint64
	Rank, Phase     uint32
	Row             int32
}

const (
	ModelPhaseShortFeatures uint32 = iota
	ModelPhaseGround
	ModelPhaseEffects
	ModelPhaseAir
)

// ModelPaint commits a slot in production order. Slot is one-based, as in
// InstanceSlots; Kind/ID name the first member (normally its keyed parent).
// Row and Kind retain the unit/feature boundary for later sprite interleaving.
type ModelPaint struct {
	Slot, Rank, Phase uint32
	Row               int32
	Kind              uint8
	ID                uint64
}

type ModelComposition struct {
	InstanceSlots           []uint32 // aligned to live instances; zero means no body draw
	InstanceRanks           []uint32 // production member order, including factory children
	Slots                   []ModelSlot
	AtlasWidth, AtlasHeight int
	Paint                   []ModelPaint
	SubjectPixels           []uint64 // aligned to Slots; bounded rectangle area, not covered texels
	TotalPixels             uint64
	UnorderedInstances      int // absent production rank, including deliberately gated subjects
}

type modelPieceBound struct {
	radius float64
	valid  bool
}
type modelGroupKey struct {
	kind uint8
	id   uint64
	// Independent instances cannot collide with published subject identities.
	instance int
}
type modelSlotWork struct {
	x0, y0, x1, y1 int
	opacity        float32
	paint          ModelPaint
}
type modelInstanceKey struct{ mesh, offset int }

// ModelComposer owns reusable CPU scratch. New caches each retained piece's
// radius about its origin once; Prepare never transforms individual vertices.
// Use one composer on the presentation owner, after RetainedBattle.Frame.
// Its result remains borrowed until the next Prepare [I6][C-G5].
type ModelComposer struct {
	bounds         [][]modelPieceBound
	groups         map[modelGroupKey]int
	instances      map[modelInstanceKey]int
	work           []modelSlotWork
	order          []ModelOrder
	packing        []int
	seen           []bool
	instanceGroups []int
	composition    ModelComposition
}

func NewModelComposer(scene *Scene) *ModelComposer {
	c := &ModelComposer{groups: make(map[modelGroupKey]int), instances: make(map[modelInstanceKey]int)}
	if scene == nil {
		return c
	}
	c.bounds = make([][]modelPieceBound, len(scene.Meshes))
	for mi, mesh := range scene.Meshes {
		pieces := make([]modelPieceBound, mesh.Pieces)
		for _, v := range mesh.Vertices {
			if int(v.Piece) >= len(pieces) {
				continue
			}
			p := &pieces[v.Piece]
			x, y, z := float64(v.Position[0]), float64(v.Position[1]), float64(v.Position[2])
			r := math.Sqrt(x*x + y*y + z*z)
			if math.IsNaN(r) || math.IsInf(r, 0) {
				p.radius = math.Inf(1)
			} else {
				p.radius = max(p.radius, math.Nextafter(r, math.Inf(1)))
			}
			p.valid = true
		}
		c.bounds[mi] = pieces
	}
	return c
}

// Prepare keeps depth within one staged subject, then emits its single image
// in production body order (DESIGN_GPU_RENDERER C-G3/C-G5). Packing changes
// atlas placement only; it never changes painter order. An oversized frame
// returns an error instead of dropping subjects or sharing their key planes.
func (c *ModelComposer) Prepare(retained *RetainedBattle, live *LiveFrame, current *frame.Frame, width, height int, order []ModelOrder) (ModelComposition, error) {
	fail := func(why string) (ModelComposition, error) {
		return ModelComposition{}, fmt.Errorf("nanolathe: model composition failed: logical path retained subjects, providers searched [production order retained meshes], expected %s", why)
	}
	if c == nil || live == nil || width <= 0 || height <= 0 {
		return fail("positive viewport and prepared composer/frame")
	}
	if math.IsNaN(float64(live.Alpha)) || live.Alpha < 0 || live.Alpha > 1 || live.Camera[2] <= 0 {
		return fail("finite interpolation and positive camera scale")
	}
	for _, v := range live.Camera {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fail("finite camera")
		}
	}
	clear(c.groups)
	clear(c.instances)
	c.work = c.work[:0]
	out := &c.composition
	out.InstanceSlots = resizeComposition(out.InstanceSlots, len(live.Instances))
	clear(out.InstanceSlots)
	out.InstanceRanks = resizeComposition(out.InstanceRanks, len(live.Instances))
	clear(out.InstanceRanks)
	out.Slots, out.Paint, out.SubjectPixels = out.Slots[:0], out.Paint[:0], out.SubjectPixels[:0]
	out.AtlasWidth, out.AtlasHeight, out.TotalPixels, out.UnorderedInstances = 0, 0, 0, 0
	c.seen = resizeComposition(c.seen, len(live.Instances))
	clear(c.seen)
	c.instanceGroups = resizeComposition(c.instanceGroups, len(live.Instances))
	for i, instance := range live.Instances {
		c.instanceGroups[i] = -1
		if instance.Mesh < 0 || instance.Mesh >= len(c.bounds) || instance.PoseOffset < 0 || instance.PoseOffset+len(c.bounds[instance.Mesh]) > len(live.Current) || instance.PoseOffset+len(c.bounds[instance.Mesh]) > len(live.Previous) {
			return fail("valid retained instance mesh and pose span")
		}
		c.instances[modelInstanceKey{instance.Mesh, instance.PoseOffset}] = i
	}
	if len(live.Instances) == 0 {
		return *out, nil
	}
	if retained == nil || retained.previous == nil || current == nil || retained.previous.frame.Tick != current.Tick || live.Tick != current.Tick {
		return fail("matching retained publication and committed frame")
	}
	c.order = append(c.order[:0], order...)
	slices.SortStableFunc(c.order, func(a, b ModelOrder) int {
		if n := cmp.Compare(a.Phase, b.Phase); n != 0 {
			return n
		}
		return cmp.Compare(a.Rank, b.Rank)
	})
	for _, entry := range c.order {
		span, ok := retained.previous.byID[battleEntityKey{entry.Kind, entry.ID}]
		if !ok {
			continue // topology/culling must not manufacture a replacement subject
		}
		i, ok := c.instances[modelInstanceKey{span.mesh, span.offset}]
		if !ok || c.seen[i] {
			continue
		}
		c.seen[i] = true
		out.InstanceRanks[i] = entry.Rank
		instance := live.Instances[i]
		rect, visible, err := c.screenBounds(instance, live, width, height)
		if err != nil {
			return fail(err.Error())
		}
		if !visible {
			continue
		}
		key := modelGroupKey{instance: i + 1}
		if entry.GroupKind != 0 && entry.GroupID != 0 {
			key = modelGroupKey{kind: entry.GroupKind, id: entry.GroupID}
		} else if instance.Subject != 0 {
			key = modelGroupKey{kind: 1, id: instance.Subject}
		}
		group, found := c.groups[key]
		if !found {
			group = len(c.work)
			c.groups[key] = group
			opacity := instance.Visual.State[1]
			if instance.Visual.State == [4]float32{} {
				opacity = 1
			}
			c.work = append(c.work, modelSlotWork{rect[0], rect[1], rect[2], rect[3], opacity, ModelPaint{Rank: entry.Rank, Phase: entry.Phase, Row: entry.Row, Kind: entry.Kind, ID: entry.ID}})
		} else {
			w := &c.work[group]
			w.x0, w.y0, w.x1, w.y1 = min(w.x0, rect[0]), min(w.y0, rect[1]), max(w.x1, rect[2]), max(w.y1, rect[3])
		}
		c.instanceGroups[i] = group
	}
	for i := range c.seen {
		if !c.seen[i] {
			out.UnorderedInstances++
		}
	}
	for i, w := range c.work {
		slot := uint32(i + 1)
		pixels := uint64(w.x1-w.x0) * uint64(w.y1-w.y0)
		out.Slots = append(out.Slots, ModelSlot{Screen: [4]float32{float32(w.x0), float32(w.y0), float32(w.x1 - w.x0), float32(w.y1 - w.y0)}, Params: [4]float32{w.opacity, float32(w.paint.Phase)}})
		w.paint.Slot = slot
		out.Paint = append(out.Paint, w.paint)
		out.SubjectPixels = append(out.SubjectPixels, pixels)
		out.TotalPixels += pixels
	}
	if err := c.pack(); err != nil {
		return fail(err.Error())
	}
	for i, group := range c.instanceGroups {
		if group >= 0 {
			out.InstanceSlots[i] = uint32(group + 1)
		}
	}
	return *out, nil
}

func (c *ModelComposer) screenBounds(instance Instance, live *LiveFrame, width, height int) ([4]int, bool, error) {
	var rect [4]int
	loX, loY, hiX, hiY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	alpha, zoom := float64(live.Alpha), float64(live.Camera[2])
	for piece, bound := range c.bounds[instance.Mesh] {
		if !bound.valid {
			continue
		}
		a, b := live.Previous[instance.PoseOffset+piece], live.Current[instance.PoseOffset+piece]
		// Native rigid interpolation suppresses a piece hidden at either end.
		if a[15] < 0.5 || b[15] < 0.5 {
			continue
		}
		origin := [3]float64{}
		for axis := range origin {
			origin[axis] = float64(a[12+axis]) + (float64(b[12+axis])-float64(a[12+axis]))*alpha
		}
		// Rotation preserves the radius. The projection's second row has
		// norm sqrt(1+1/4), bounding z-y/2 for EVERY intermediate rotation.
		x := (origin[0]-float64(live.Camera[0]))*zoom + float64(width)/2
		y := (origin[2]-origin[1]/2-float64(live.Camera[1]))*zoom + float64(height)/2
		rx, ry := bound.radius*zoom, bound.radius*zoom*math.Sqrt(1.25)
		if math.IsNaN(x+y+rx+ry) || math.IsInf(x+y+rx+ry, 0) {
			return rect, false, fmt.Errorf("finite retained piece positions and radius")
		}
		loX, loY, hiX, hiY = min(loX, x-rx), min(loY, y-ry), max(hiX, x+rx), max(hiY, y+ry)
	}
	if math.IsInf(loX, 1) {
		return rect, false, nil
	}
	// One physical pixel guards float rounding, AA/outline edge coverage and
	// viewport borders. Clip before integer conversion, including offscreen
	// models with very large authored extents.
	loX, loY = max(math.Floor(loX)-1, -1), max(math.Floor(loY)-1, -1)
	hiX, hiY = min(math.Ceil(hiX)+1, float64(width)+1), min(math.Ceil(hiY)+1, float64(height)+1)
	if hiX <= loX || hiY <= loY {
		return rect, false, nil
	}
	rect = [4]int{int(loX), int(loY), int(hiX), int(hiY)}
	return rect, true, nil
}

// pack uses a stable descending-height/width shelf order. Every bounded power
// of two width is considered; the smallest resulting power-of-two page wins.
func (c *ModelComposer) pack() error {
	const limit = 8192
	out := &c.composition
	if len(c.work) == 0 {
		return nil
	}
	c.packing = resizeComposition(c.packing, len(c.work))
	minWidth := 1
	for i, w := range c.work {
		c.packing[i] = i
		minWidth = max(minWidth, w.x1-w.x0)
		if w.x1-w.x0 > limit || w.y1-w.y0 > limit {
			return fmt.Errorf("subject rectangles within the 8192-pixel atlas bound")
		}
	}
	slices.SortStableFunc(c.packing, func(a, b int) int {
		wa, wb := c.work[a], c.work[b]
		if n := cmp.Compare(wb.y1-wb.y0, wa.y1-wa.y0); n != 0 {
			return n
		}
		return cmp.Compare(wb.x1-wb.x0, wa.x1-wa.x0)
	})
	bestWidth, bestHeight, bestArea := 0, 0, limit*limit+1
	for width := compositionPowerOfTwo(minWidth); width <= limit; width *= 2 {
		height := c.shelves(width, false)
		if height > limit {
			continue
		}
		height = compositionPowerOfTwo(height)
		if area := width * height; area < bestArea {
			bestWidth, bestHeight, bestArea = width, height, area
		}
	}
	if bestWidth == 0 {
		return fmt.Errorf("all %d subjects (%d rectangle pixels) in one atlas up to 8192 by 8192", len(c.work), out.TotalPixels)
	}
	c.shelves(bestWidth, true)
	out.AtlasWidth, out.AtlasHeight = bestWidth, bestHeight
	return nil
}

func (c *ModelComposer) shelves(width int, assign bool) int {
	x, y, shelf := 0, 0, 0
	for _, i := range c.packing {
		w := c.work[i]
		rw, rh := w.x1-w.x0, w.y1-w.y0
		if x+rw > width {
			x, y, shelf = 0, y+shelf, 0
		}
		if assign {
			c.composition.Slots[i].Atlas = [4]float32{float32(x), float32(y), float32(rw), float32(rh)}
		}
		x, shelf = x+rw, max(shelf, rh)
	}
	return y + shelf
}

func compositionPowerOfTwo(v int) int {
	n := 1
	for n < v {
		n *= 2
	}
	return n
}

func resizeComposition[T any](v []T, n int) []T {
	if cap(v) < n {
		v = slices.Grow(v, n-len(v))
	}
	return v[:n]
}
