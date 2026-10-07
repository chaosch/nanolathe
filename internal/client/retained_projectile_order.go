package client

import "github.com/nanolathe-gg/nanolathe/internal/frame"

const (
	RetainedProjectileParent uint32 = iota
	RetainedProjectileChild
)

// RetainedProjectileModel identifies an actual successful model command from
// the projectile recorder. Type 1 may emit the parent followed by its first
// header child; the deadline and per-piece geometry admission belong to the
// ordinary producer [03 §5.4][03 R-COMP-02 §6]. No second dispatch predicts it.
type RetainedProjectileModel struct {
	ID     uint64
	Member uint32
}

func (c *Client) recordRetainedProjectileModel(p frame.ProjectileView, member uint32) {
	if c.retainedProjectileOrder != nil {
		*c.retainedProjectileOrder = append(*c.retainedProjectileOrder, RetainedProjectileModel{ID: projectilePresentationID(p), Member: member})
	}
}
