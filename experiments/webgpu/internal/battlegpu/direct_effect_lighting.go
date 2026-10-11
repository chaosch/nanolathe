package battlegpu

import (
	"math"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/meshscene"
)

// The bridge admits a positive-area source ring and emits 0/i/i+1 in its
// original order (metalhud projectModel; GPU design §11.2). Alignment is checked
// before dependent lighting; neither source admission nor fan coverage changes.
func layerAppendDirectGeometry(dst []byte, packet meshscene.EffectGeometry, lights []meshscene.RetainedLight, physicalScale float32, enabled bool) ([]byte, error) {
	if packet.Suppressed != "" || len(packet.Vertices) == 0 {
		return dst, nil
	}
	g := packet.Model.Geometry
	if g == nil || len(packet.Vertices)%3 != 0 {
		return dst, layerError("direct effect lighting requires original admitted faces and whole triangles")
	}
	indices, count := [8]uint32{}, uint32(0)
	if enabled {
		center, extent := layerDirectSubjectBound(packet, physicalScale)
		indices, count = layerSelectSmoke(lights, center, extent)
	}
	first := len(dst)
	dst = append(dst, make([]byte, len(packet.Vertices)*layerDirectVertexStride)...)
	at := 0
	for _, face := range g.Faces {
		if !layerDirectFaceAdmitted(face) {
			continue
		}
		code := layerDirectFaceLight(packet, face, lights, indices, count, physicalScale)
		for i := 1; i+1 < len(face.Vertices); i++ {
			for _, corner := range [3]int{0, i, i + 1} {
				if at >= len(packet.Vertices) {
					return dst[:first], layerError("direct effect triangle count must match admitted original faces")
				}
				v := packet.Vertices[at]
				source := face.Vertices[corner]
				x := (float32(source.X)+float32(g.AnchorX)-float32(g.OriginX)+.5)*packet.Scale + packet.OffsetX
				y := (float32(source.Y)+float32(g.AnchorY)-float32(g.OriginY)+.5)*packet.Scale + packet.OffsetY
				if v.PositionUV[0] != x || v.PositionUV[1] != y {
					return dst[:first], layerError("direct effect corners must match the producer's admitted face fan")
				}
				v.PositionUV[0] *= physicalScale
				v.PositionUV[1] *= physicalScale
				out := first + at*layerDirectVertexStride
				layerVec(dst, out, v.PositionUV)
				layerVec(dst, out+16, v.Color)
				layerVec(dst, out+32, v.Bounds)
				layerVec(dst, out+48, [4]float32{float32(code)})
				at++
			}
		}
	}
	if at != len(packet.Vertices) {
		return dst[:first], layerError("direct effect triangle count must match admitted original faces")
	}
	return dst, nil
}

func layerDirectFaceAdmitted(face drawlist.ModelFace) bool {
	if len(face.Vertices) < 3 {
		return false
	}
	var area int64
	previous := face.Vertices[len(face.Vertices)-1]
	for _, v := range face.Vertices {
		area += int64(previous.X)*int64(v.Y) - int64(previous.Y)*int64(v.X)
		previous = v
	}
	return area > 0
}

// Selection uses the same whole subject box as modelWorldBounds/near, transformed
// once into the physical coordinate system of the retained sources (BL2–BL3).
func layerDirectSubjectBound(packet meshscene.EffectGeometry, physicalScale float32) ([2]float32, float32) {
	g := packet.Model.Geometry
	x0, y0, x1, y1 := int32(0), int32(0), g.Width, g.Height
	if g.Width <= 0 || g.Height <= 0 {
		first := true
		for _, face := range g.Faces {
			for _, v := range face.Vertices {
				if first {
					x0, y0, x1, y1 = v.X, v.Y, v.X+1, v.Y+1
					first = false
				} else {
					x0, y0, x1, y1 = min(x0, v.X), min(y0, v.Y), max(x1, v.X+1), max(y1, v.Y+1)
				}
			}
		}
	}
	ox, oy := g.AnchorX-g.OriginX, g.AnchorY-g.OriginY
	k := packet.Scale * physicalScale
	x := float32(x0+ox+x1+ox)*.5*k + packet.OffsetX*physicalScale
	y := float32(y0+oy+y1+oy)*.5*k + packet.OffsetY*physicalScale
	extent := float32(x1-x0+y1-y0) * .5 * k
	return [2]float32{x, y}, extent
}

// BL2 evaluates the mean original corner, without the raster's half-pixel bias.
// Absolute height is independent of the composition key. BL3 caps via BL4's
// production quantization, after Lambert response and squared radial falloff.
func layerDirectFaceLight(packet meshscene.EffectGeometry, face drawlist.ModelFace, lights []meshscene.RetainedLight, indices [8]uint32, count uint32, physicalScale float32) uint32 {
	if count == 0 || face.Normal == [3]float32{} || len(face.Vertices) == 0 {
		return 0
	}
	var x, y, h float32
	for _, v := range face.Vertices {
		x += float32(v.X)
		y += float32(v.Y)
		h += v.Height
	}
	inv := 1 / float32(len(face.Vertices))
	g := packet.Model.Geometry
	k := packet.Scale * physicalScale
	x = (float32(g.AnchorX-g.OriginX)+x*inv)*k + packet.OffsetX*physicalScale
	y = (float32(g.AnchorY-g.OriginY)+y*inv)*k + packet.OffsetY*physicalScale
	h = (g.WorldHeight + h*inv) * k
	var rgb [3]float32
	for i := uint32(0); i < count; i++ {
		light := lights[indices[i]]
		dx, dy, dh := light.PositionRadius[0]-x, light.PositionRadius[1]-(y+h*.5), light.PositionRadius[2]-h
		d2 := dx*dx + dy*dy + dh*dh
		r2 := light.PositionRadius[3] * light.PositionRadius[3]
		if d2 >= r2 {
			continue
		}
		falloff := 1 - d2/r2
		falloff *= falloff
		response := max((dx*face.Normal[0]+dy*face.Normal[1]+dh*face.Normal[2])/float32(math.Sqrt(float64(max(d2, 1)))), 0)
		gain := falloff * response * 3.25
		for j := range rgb {
			rgb[j] += light.ColorGroundGain[j] * gain
		}
	}
	return layerDirectLightCode(rgb)
}

func layerDirectLightCode(rgb [3]float32) (code uint32) {
	for i, channel := range rgb {
		code |= uint32(min(max(channel, 0), 2)*63.5+.5) << (7 * i)
	}
	return code
}
