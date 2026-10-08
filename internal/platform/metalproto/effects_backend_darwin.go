//go:build darwin

package metalproto

import (
	_ "embed"
	"fmt"
	"math"
	"unsafe"

	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

//go:embed native/effects.metal
var effectsShaderSource string

type nativeEffectsOp struct{ Kind, First, Count, Reserved uint32 }
type nativeEffectsLayer struct {
	First, Count uint32
	Reserved     [2]uint32
}
type nativeEffectsSample struct{ Rect, Source, Key [4]float32 }
type nativeEffectsLens struct {
	First, Count uint32
	Read         [4]float32
	Reserved     [2]uint32
}

// nativeEffectsUpload matches NMEffectsUpload (152 bytes on the supported
// 64-bit host). Its uintptr values borrow packing storage until the synchronous
// nm_live_upload copy returns; the host must keep effectsPacking alive then.
type nativeEffectsUpload struct {
	Quads, Vertices, Samples, Lenses, Ops, Layers, Atlas                                             unsafe.Pointer
	Version                                                                                          uint64
	QuadCount, VertexCount, SampleCount, LensCount, OpCount, LayerCount, Width, Height               uint32
	Dirty                                                                                            [4]uint32
	ModelSubmitted, ModelSuppressed, ProjectileModels, SourceCount, QuadSpanFaces, SupersampleModels uint32
	Smoke                                                                                            unsafe.Pointer
	SmokeCount, Reserved                                                                             uint32
}

type effectsPacking struct {
	quads          []meshscene.OverlayQuad
	vertices       []meshscene.EffectVertex
	samples        []nativeEffectsSample
	lenses         []nativeEffectsLens
	ops            []nativeEffectsOp
	layers         []nativeEffectsLayer
	atlas          []byte
	sources        []meshscene.EffectSources
	retainedModels []drawlist.Model
	smoke          []meshscene.EffectSmoke
	modelSpans     []nativeEffectsOp // one layer's model spans, reused
}

// Prepare scales the logical client picture exactly once. Native replay reads
// these already-physical positions and never applies its world camera again.
// Sources remain in EffectLayer for shared lighting/glow/distortion preparation;
// the stock body pass does not manufacture Enhanced emission from them.
func (p *effectsPacking) Prepare(layers []meshscene.EffectLayer, atlas meshscene.Texture, version uint64, dirty [4]uint32, scale float32) (nativeEffectsUpload, error) {
	var out nativeEffectsUpload
	if unsafe.Sizeof(out) != 152 || unsafe.Sizeof(meshscene.EffectSmoke{}) != 96 || unsafe.Sizeof(meshscene.OverlayQuad{}) != 64 || unsafe.Sizeof(meshscene.EffectVertex{}) != 48 || unsafe.Sizeof(nativeEffectsSample{}) != 48 || unsafe.Sizeof(nativeEffectsLens{}) != 32 || unsafe.Sizeof(nativeEffectsOp{}) != 16 || unsafe.Sizeof(nativeEffectsLayer{}) != 16 {
		return out, fmt.Errorf("metalproto: effects upload ABI mismatch")
	}
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return out, fmt.Errorf("metalproto: invalid effects presentation scale")
	}
	if atlas.Width <= 0 || atlas.Height <= 0 || atlas.Width > math.MaxInt32 || atlas.Height > math.MaxInt32 || int64(atlas.Width)*int64(atlas.Height)*4 != int64(len(atlas.RGBA)) {
		return out, fmt.Errorf("metalproto: invalid effects atlas")
	}
	if uint64(dirty[0])+uint64(dirty[2]) > uint64(atlas.Width) || uint64(dirty[1])+uint64(dirty[3]) > uint64(atlas.Height) {
		return out, fmt.Errorf("metalproto: effects dirty rectangle outside atlas")
	}
	p.quads, p.vertices, p.samples, p.lenses, p.ops, p.layers = p.quads[:0], p.vertices[:0], p.samples[:0], p.lenses[:0], p.ops[:0], p.layers[:0]
	p.atlas = atlas.RGBA
	p.sources = p.sources[:0]
	p.retainedModels = p.retainedModels[:0]
	p.smoke = p.smoke[:0]
	var bodySlot uint32
	var groundBase uint32
	for _, layer := range layers {
		smokeBase := len(p.smoke)
		for _, smoke := range layer.Smoke {
			for i := range smoke.Quad.Rect {
				smoke.Quad.Rect[i] *= scale
			}
			for i := range smoke.Selection {
				smoke.Selection[i] *= scale
			}
			smoke.Quad.Params[1] *= scale
			smoke.Quad.Params[2] *= scale
			p.smoke = append(p.smoke, smoke)
		}
		qbase, lbase := len(p.quads), len(p.lenses)
		for _, q := range layer.Quads {
			for j := range q.Rect {
				q.Rect[j] *= scale
			}
			p.quads = append(p.quads, q)
		}
		p.modelSpans = append(p.modelSpans[:0], make([]nativeEffectsOp, len(layer.Models))...)
		modelSpans := p.modelSpans
		for i, m := range layer.Models {
			if m.Suppressed != "" {
				out.ModelSuppressed++
				continue
			}
			if len(m.Vertices)%3 != 0 {
				return out, fmt.Errorf("metalproto: effect geometry does not contain whole triangles")
			}
			if len(m.Vertices) > 0 {
				out.ModelSubmitted++
			}
			if m.SupersampleIgnored {
				out.SupersampleModels++
			}
			out.QuadSpanFaces += m.QuadSpanFaces
			modelSpans[i] = nativeEffectsOp{Kind: 2, First: uint32(len(p.vertices)), Count: uint32(len(m.Vertices))}
			for _, v := range m.Vertices {
				v.PositionUV[0] *= scale
				v.PositionUV[1] *= scale
				p.vertices = append(p.vertices, v)
			}
		}
		for _, l := range layer.Lenses {
			lens := nativeEffectsLens{First: uint32(len(p.samples)), Count: uint32(len(l.Samples)), Read: l.ReadRect}
			for j := range lens.Read {
				lens.Read[j] *= scale
			}
			for _, sample := range l.Samples {
				a := nativeEffectsSample{Rect: sample.Rect, Source: sample.Source, Key: [4]float32{float32(l.KeyRGB[0]), float32(l.KeyRGB[1]), float32(l.KeyRGB[2]), 0}}
				for j := range a.Rect {
					a.Rect[j] *= scale
					a.Source[j] *= scale
				}
				p.samples = append(p.samples, a)
			}
			p.lenses = append(p.lenses, lens)
		}
		phase := nativeEffectsLayer{First: uint32(len(p.ops))}
		for _, op := range layer.Ops {
			if op.First < 0 || op.Count < 0 {
				return out, fmt.Errorf("metalproto: invalid effects op span")
			}
			switch op.Kind {
			case meshscene.EffectOpSmoke:
				if op.First > len(layer.Smoke) || op.Count > len(layer.Smoke)-op.First {
					return out, fmt.Errorf("metalproto: smoke receiver span outside layer")
				}
				p.ops = append(p.ops, nativeEffectsOp{Kind: 5, First: uint32(smokeBase + op.First), Count: uint32(op.Count)})
			case meshscene.EffectOpGround:
				if op.First > len(layer.Ground) || op.Count > len(layer.Ground)-op.First {
					return out, fmt.Errorf("metalproto: ground marker outside layer")
				}
				p.ops = append(p.ops, nativeEffectsOp{Kind: 4, First: groundBase + uint32(op.First), Count: uint32(op.Count)})
			case meshscene.EffectOpQuads:
				if op.First > len(layer.Quads) || op.Count > len(layer.Quads)-op.First {
					return out, fmt.Errorf("metalproto: effect quad span outside layer")
				}
				p.ops = append(p.ops, nativeEffectsOp{Kind: 0, First: uint32(qbase + op.First), Count: uint32(op.Count)})
			case meshscene.EffectOpLens:
				if op.First > len(layer.Lenses) || op.Count > len(layer.Lenses)-op.First {
					return out, fmt.Errorf("metalproto: effect lens span outside layer")
				}
				p.ops = append(p.ops, nativeEffectsOp{Kind: 1, First: uint32(lbase + op.First), Count: uint32(op.Count)})
			case meshscene.EffectOpGeometry:
				if op.First > len(modelSpans) || op.Count > len(modelSpans)-op.First {
					return out, fmt.Errorf("metalproto: effect model span outside layer")
				}
				for _, span := range modelSpans[op.First : op.First+op.Count] {
					if span.Count > 0 {
						p.ops = append(p.ops, span)
					}
				}
			case meshscene.EffectOpRetainedModel:
				if len(layer.ProjectileOrder) != len(layer.RetainedModels) {
					return out, fmt.Errorf("metalproto: projectile identities differ from retained model markers")
				}
				if op.First > len(layer.RetainedModels) || op.Count > len(layer.RetainedModels)-op.First {
					return out, fmt.Errorf("metalproto: retained projectile marker outside layer")
				}
				for i, model := range layer.RetainedModels[op.First : op.First+op.Count] {
					identity := layer.ProjectileOrder[op.First+i]
					if identity.ID == 0 || identity.Member > 1 {
						return out, fmt.Errorf("metalproto: invalid retained projectile identity/member")
					}
					marker := nativeEffectsOp{Kind: 3, First: bodySlot, Count: 1}
					if model.ShadowOnly {
						marker.Reserved = 1
					} else {
						if identity.Member == 1 {
							marker.Reserved = 2 // already included in the parent's combined retained slot
						}
						bodySlot++
					}
					p.ops = append(p.ops, marker)
					p.retainedModels = append(p.retainedModels, model)
				}
			default:
				return out, fmt.Errorf("metalproto: unsupported effects op %d", op.Kind)
			}
		}
		phase.Count = uint32(len(p.ops)) - phase.First
		p.layers = append(p.layers, phase)
		if uint64(groundBase)+uint64(len(layer.Ground)) > math.MaxInt32 {
			return out, fmt.Errorf("metalproto: ground marker count exceeds native range")
		}
		groundBase += uint32(len(layer.Ground))
		for _, src := range layer.Sources {
			p.sources = append(p.sources, src)
			out.SourceCount += uint32(len(src.Art) + len(src.Lines) + len(src.Nano) + len(src.Flashes) + len(src.Halos))
		}
	}
	for _, n := range []int{len(p.quads), len(p.vertices), len(p.samples), len(p.lenses), len(p.ops), len(p.layers), len(p.retainedModels), len(p.smoke)} {
		if n > math.MaxInt32 {
			return out, fmt.Errorf("metalproto: effects upload count exceeds native range")
		}
	}
	out.Quads, out.Vertices, out.Samples, out.Lenses, out.Ops, out.Layers, out.Atlas = pointer(p.quads), pointer(p.vertices), pointer(p.samples), pointer(p.lenses), pointer(p.ops), pointer(p.layers), pointer(p.atlas)
	out.QuadCount, out.VertexCount, out.SampleCount, out.LensCount, out.OpCount, out.LayerCount = uint32(len(p.quads)), uint32(len(p.vertices)), uint32(len(p.samples)), uint32(len(p.lenses)), uint32(len(p.ops)), uint32(len(p.layers))
	out.ProjectileModels = uint32(len(p.retainedModels))
	out.Smoke, out.SmokeCount = pointer(p.smoke), uint32(len(p.smoke))
	out.Width, out.Height, out.Version, out.Dirty = uint32(atlas.Width), uint32(atlas.Height), version, dirty
	return out, nil
}
