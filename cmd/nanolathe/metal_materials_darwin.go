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
	return &meshscene.ModelMaterialSource{
		Frames: cl.RetainedMaterialFrames(),
		Resolve: func(q meshscene.ModelMaterialRequest) (meshscene.ModelMaterialSelection, bool) {
			m, ok := cl.ResolveRetainedMaterial(client.RetainedMaterialRequest{
				Piece: q.Piece, Primitive: q.Primitive, Current: q.Current,
				Frame: q.Frame, Unit: q.Unit, Feature: q.Feature,
				Projectile: q.Projectile, Debris: q.Debris,
			})
			return meshscene.ModelMaterialSelection{Frame: m.Frame, Color: m.Color, Skip: m.Skip, Material: m.Material, Glint: m.Glint}, ok
		},
	}
}
