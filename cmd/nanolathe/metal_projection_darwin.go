//go:build darwin

package main

import (
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

func prepareMetalProjection(cl *client.Client, retained *meshscene.RetainedBattle, live *meshscene.LiveFrame, outputScale float32) error {
	source := cl.RecordRetainedModelProjections()
	control, view, err := meshscene.ModelProjectionView(source.World, source.Camera, source.RecordScale, source.Doubled, outputScale)
	if err != nil {
		return err
	}
	type identity struct {
		kind uint8
		id   uint64
	}
	subjects := make(map[identity]client.RetainedModelProjection, len(source.Subjects))
	for _, s := range source.Subjects {
		subjects[identity{s.Kind, s.ID}] = s
	}
	live.ModelProjections, err = retained.ApplyModelProjections(live, view, func(kind uint8, id uint64) meshscene.ModelProjectionSubject {
		s, ok := subjects[identity{kind, id}]
		if !ok {
			return meshscene.ModelProjectionSubject{}
		}
		var flags uint32
		if s.ModeKnown {
			flags |= meshscene.ModelProjectionKnown
		}
		if s.DirectAll {
			flags |= meshscene.ModelProjectionDirectAll
		}
		var pieces []uint32
		if len(s.PieceDirect) > 0 {
			pieces = make([]uint32, len(s.PieceDirect))
			for i, direct := range s.PieceDirect {
				if direct {
					pieces[i] = meshscene.ModelProjectionPieceDirect
				}
			}
		}
		return meshscene.ModelProjectionSubject{Projection: meshscene.NewModelProjection(s.Origin, s.NativeAnchor, s.DoubledAnchor, flags, control), PieceFlags: pieces}
	})
	return err
}
