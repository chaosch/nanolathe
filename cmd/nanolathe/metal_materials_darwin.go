//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// The host owns the production presentation registry and installed skins.
// Native preparation borrows their immutable frames; frame selection is a
// read of that registry, never a second animation lifecycle [I6].
func metalMaterialSource(cl *client.Client) *meshscene.ModelMaterialSource {
	if cl == nil {
		return nil
	}
	prepare := func(q meshscene.ModelMaterialRequest) meshscene.ModelMaterialResolver {
		return metalPreparedMaterial{cl.PrepareRetainedMaterial(client.RetainedMaterialRequest{
			Frame: q.Frame, Unit: q.Unit, Feature: q.Feature,
			Projectile: q.Projectile, Debris: q.Debris,
		})}
	}
	return &meshscene.ModelMaterialSource{
		Frames:  cl.RetainedMaterialFrames(),
		Prepare: prepare,
		Resolve: func(q meshscene.ModelMaterialRequest) (meshscene.ModelMaterialSelection, bool) {
			return prepare(q).Resolve(q.Piece, q.Primitive, q.Current)
		},
	}
}

type metalPreparedMaterial struct {
	source client.RetainedMaterialResolver
}

func (m metalPreparedMaterial) Resolve(piece, primitive int, current bool) (meshscene.ModelMaterialSelection, bool) {
	s, ok := m.source.Resolve(piece, primitive, current)
	return meshscene.ModelMaterialSelection{Frame: s.Frame, Color: s.Color, Skip: s.Skip, Material: s.Material, Glint: s.Glint}, ok
}

// CacheKey lets the retained source reuse a walk whose selections cannot
// differ: same model, texture generation, skin and owner selector, and no
// animated texture.
func (m metalPreparedMaterial) CacheKey() (any, bool) {
	return m.source.CacheKey()
}
