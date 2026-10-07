package client

import (
	"github.com/nanolathe-gg/nanolathe/internal/drawlist"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/hud"
)

// RecordRetainedForeground reuses the ordinary HUD, cursor, command previews
// and unit annotations when a separate retained renderer owns the world.
// Prototype compromise: annotations compose after world fog/depth instead of
// inside each model's painter slot. Their geometry and controls are unchanged.
func (c *Client) RecordRetainedForeground() *drawlist.List {
	if c == nil {
		return nil
	}
	c.retainedForeground = true
	c.retainedSelections = c.retainedSelections[:0]
	defer func() { c.retainedForeground = false }()
	return c.recordPausedLayer(pausedForegroundLayer)
}

// RetainedSelection is one selected unit's footprint quad, left out of the
// retained foreground for a host that draws it in the unit's own paint slot,
// before the model, as presentUnit does [03 R-WATER-01 §1] rule 5.
type RetainedSelection struct {
	InstanceID uint64
	Lines      [4]drawlist.Line // record pixels inside the returned world region
}

// SetExternalSelection makes RecordRetainedForeground collect selected-unit
// quads for RetainedSelections, and leave unit labels to RecordRetainedLabels,
// instead of drawing either over the world.
func (c *Client) SetExternalSelection(on bool) {
	if c != nil {
		c.externalSelection = on
	}
}

// RetainedSelections returns the quads the last RecordRetainedForeground
// collected and the world region their lines are recorded in. Both are
// borrowed until the next record.
func (c *Client) RetainedSelections() (drawlist.WorldSpace, []RetainedSelection) {
	if c == nil {
		return drawlist.WorldSpace{}, nil
	}
	return c.retainedSelectionWS, c.retainedSelections
}

// RecordRetainedLabels records the pinned frame's unit labels (health bars,
// Community unit HUD, group digits) into dst in their own world region, for a
// host that composes them where drawCommittedWorld does: after the airborne
// pass and its strip, before strip 9 and the fog [03 §1][03 R-FX-01 §6]. It
// records nothing unless SetExternalSelection is on. Call it after
// BeginPresentationFrame and replay dst before the next client recording,
// which reuses the point arena its batches borrow.
func (c *Client) RecordRetainedLabels(dst *drawlist.List) {
	if dst == nil {
		return
	}
	dst.Reset()
	if c == nil || !c.externalSelection || c.cam == nil {
		return
	}
	cur := c.committedFrame()
	if cur == nil {
		return
	}
	saved := c.list
	c.list = *dst
	c.refreshRecordExtent()
	c.frameTick = cur.Tick
	c.emitWorldBegin()
	c.drawUnitLabels(cur, true)
	c.emitWorldEnd()
	*dst = c.list
	c.list = saved
}

func (c *Client) drawRetainedAnnotations(cur *frame.Frame) {
	c.refreshRecordExtent()
	if cur == nil || c.cam == nil {
		return
	}
	c.frameTick = cur.Tick
	c.emitWorldBegin()
	c.retainedSelectionWS = c.worldSpace(true)
	if !c.StrategicIconsActive() {
		win := c.worldWindow()
		for i := range cur.Units {
			u := &cur.Units[i]
			if u.Flags&hud.SelectionFlag == 0 || !unitVisibleForFrame(cur, *u, cur.ViewingPlayer) {
				continue
			}
			row := unitBucketRow(u.Z, c.cam.Z)
			if row < 0 || row >= win.bucketRows {
				continue
			}
			if c.externalSelection {
				if lines, ok := c.selectionQuadLines(*u); ok {
					c.retainedSelections = append(c.retainedSelections, RetainedSelection{InstanceID: u.InstanceID, Lines: lines})
				}
				continue
			}
			c.drawSelectionQuad(*u)
		}
	}
	if !c.externalSelection {
		c.drawUnitLabels(cur, true)
	}
	c.emitWorldEnd()
}

// PresentedFramePair borrows the exact pair held by PinPresentation. The host
// must finish reading it before the next pin or host step; re-pinning by tick
// could select a newer same-tick paused-input publication.
func (c *Client) PresentedFramePair() (previous, current *frame.Frame) {
	if c == nil {
		return nil, nil
	}
	return c.committedPrevious(), c.committedFrame()
}
