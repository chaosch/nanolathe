//go:build darwin

package main

import (
	"fmt"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/prototype/meshscene"
)

// metalProjection keeps the client's projection metadata, its subject lookup
// and the result between frames, so a frame allocates nothing. The result is
// valid until the next Prepare; native upload copies it before then.
type metalProjection struct {
	source   client.RetainedModelProjectionFrame
	subjects map[metalSubjectKey]int
	out      meshscene.ModelProjectionFrame
}

type metalSubjectKey struct {
	kind uint8
	id   uint64
}

func (m *metalProjection) Prepare(cl *client.Client, retained *meshscene.RetainedBattle, live *meshscene.LiveFrame, outputScale float32) error {
	cl.RecordRetainedModelProjections(&m.source)
	source := &m.source
	control, view, err := meshscene.ModelProjectionView(source.World, source.Camera, source.RecordScale, source.Doubled, outputScale)
	if err != nil {
		return err
	}
	if m.subjects == nil {
		m.subjects = make(map[metalSubjectKey]int, len(source.Subjects))
	}
	clear(m.subjects)
	for i := range source.Subjects {
		s := &source.Subjects[i]
		m.subjects[metalSubjectKey{s.Kind, s.ID}] = i
	}
	err = retained.ApplyModelProjections(&m.out, live, view, func(kind uint8, id uint64, pieces []uint32) (meshscene.ModelProjection, error) {
		at, ok := m.subjects[metalSubjectKey{kind, id}]
		if !ok {
			return meshscene.ModelProjection{}, nil
		}
		s := &source.Subjects[at]
		if len(s.PieceDirect) != 0 && len(s.PieceDirect) != len(pieces) {
			return meshscene.ModelProjection{}, fmt.Errorf("nanolathe: retained projection failed: logical path model projection metadata, providers searched [production client retained subject identities], expected flags aligned to subject pose span")
		}
		var flags uint32
		if s.ModeKnown {
			flags |= meshscene.ModelProjectionKnown
		}
		if s.DirectAll {
			flags |= meshscene.ModelProjectionDirectAll
		}
		for i, direct := range s.PieceDirect {
			if direct {
				pieces[i] = meshscene.ModelProjectionPieceDirect
			}
		}
		return meshscene.NewModelProjection(s.Origin, s.NativeAnchor, s.DoubledAnchor, flags, control), nil
	})
	live.ModelProjections = m.out
	return err
}
