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
	// The retained source discards each resolver after its subject's walk and
	// prepares one subject at a time, so one resolver is reused and its keys
	// are interned: neither allocates per subject.
	resolver := &metalPreparedMaterial{keys: map[client.RetainedMaterialKey]*client.RetainedMaterialKey{}}
	prepare := func(q meshscene.ModelMaterialRequest) meshscene.ModelMaterialResolver {
		resolver.source = cl.PrepareRetainedMaterial(client.RetainedMaterialRequest{
			Frame: q.Frame, Unit: q.Unit, Feature: q.Feature,
			Projectile: q.Projectile, Debris: q.Debris,
		})
		return resolver
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
	keys   map[client.RetainedMaterialKey]*client.RetainedMaterialKey
}

// metalMaterialKeyLimit bounds the interned keys, as the retained cache
// bounds its walks; texture-generation changes would otherwise accumulate.
const metalMaterialKeyLimit = 4096

func (m *metalPreparedMaterial) Resolve(piece, primitive int, current bool) (meshscene.ModelMaterialSelection, bool) {
	s, ok := m.source.Resolve(piece, primitive, current)
	return meshscene.ModelMaterialSelection{Frame: s.Frame, Color: s.Color, Skip: s.Skip, Material: s.Material, Glint: s.Glint}, ok
}

// CacheKey lets the retained source reuse a walk whose selections cannot
// differ: same model, texture generation, skin and owner selector, and no
// animated texture. Equal keys return one interned pointer.
func (m *metalPreparedMaterial) CacheKey() (any, bool) {
	key, ok := m.source.CacheKey()
	if !ok {
		return nil, false
	}
	interned, found := m.keys[key]
	if !found {
		if len(m.keys) >= metalMaterialKeyLimit {
			clear(m.keys)
		}
		interned = &key
		m.keys[key] = interned
	}
	return interned, true
}
