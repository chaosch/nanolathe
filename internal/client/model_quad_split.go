package client

// Quad-split measurement prototype: NANOLATHE_QUAD_SPLIT=1 splits every
// four-corner face of every presentation model into two quads that share their
// new edge-midpoint vertices, and gives each textured half its half of the
// texture through per-corner UVs. It stands in for a remastered model with
// about twice retail's quad count, so the ring lane can be measured at that
// density. Only the client's own compiled copy changes; the simulation loads
// its models separately.

import (
	"os"

	compiledmodel "github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

var quadSplitPrototype = os.Getenv("NANOLATHE_QUAD_SPLIT") == "1"

// splitModelQuads returns a copy of m whose four-corner faces are each split
// into two quads through the midpoints of edges 0–1 and 3–2. Halves keep the
// face's place in the load-fixed order, so draw order and key ties stay as the
// source had them. Selection plates stay whole at primitive zero.
func splitModelQuads(m *compiledmodel.Model) *compiledmodel.Model {
	out := *m
	out.Pieces = make([]compiledmodel.Piece, len(m.Pieces))
	for pi := range m.Pieces {
		src := &m.Pieces[pi]
		dst := &out.Pieces[pi]
		*dst = *src
		verts := append([][3]numeric.Fixed(nil), src.Vertices...)
		mids := map[[2]uint16]uint16{}
		mid := func(a, b uint16) uint16 {
			k := [2]uint16{min(a, b), max(a, b)}
			if i, ok := mids[k]; ok {
				return i
			}
			va, vb := verts[a], verts[b]
			verts = append(verts, [3]numeric.Fixed{(va[0] + vb[0]) / 2, (va[1] + vb[1]) / 2, (va[2] + vb[2]) / 2})
			i := uint16(len(verts) - 1)
			mids[k] = i
			return i
		}
		prims := make([]compiledmodel.Primitive, 0, 2*len(src.Primitives))
		for pri, pr := range src.Primitives {
			vi := pr.VertexIndices
			if len(vi) != 4 || (src.Selection && pri == 0) || len(verts) > 65000 {
				prims = append(prims, pr)
				continue
			}
			valid := true
			for _, v := range vi {
				if int(v) >= len(src.Vertices) {
					valid = false
				}
			}
			if !valid {
				prims = append(prims, pr)
				continue
			}
			m01, m32 := mid(vi[0], vi[1]), mid(vi[3], vi[2])
			left, right := pr, pr
			left.VertexIndices = []uint16{vi[0], m01, m32, vi[3]}
			right.VertexIndices = []uint16{m01, vi[1], vi[2], m32}
			if pr.TextureName != "" {
				const half = 32768
				left.UV = &[4][2]uint16{{0, 0}, {half, 0}, {half, 65535}, {0, 65535}}
				right.UV = &[4][2]uint16{{half, 0}, {65535, 0}, {65535, 65535}, {half, 65535}}
			}
			prims = append(prims, left, right)
		}
		dst.Vertices, dst.Primitives = verts, prims
	}
	return &out
}

// quadUV resolves one corner of a split face at a w×h texture: the fraction
// of the default corner extent, rounded to the nearest whole texel.
func quadUV(frac uint16, extent int32) int32 {
	return int32((int64(frac)*int64(extent) + 32767) / 65535)
}
