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
	defer func() { c.retainedForeground = false }()
	return c.recordPausedLayer(pausedForegroundLayer)
}

func (c *Client) drawRetainedAnnotations(cur *frame.Frame) {
	c.refreshRecordExtent()
	if cur == nil || c.cam == nil {
		return
	}
	c.frameTick = cur.Tick
	c.emitWorldBegin()
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
			c.drawSelectionQuad(*u)
		}
	}
	c.drawUnitLabels(cur, true)
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
