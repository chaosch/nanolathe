package main

import (
	"fmt"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/cob"
	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/model"
)

// A factory's Build action constructs its real build-tree products one after
// another on the pad its QueryBuildInfo names (DESIGN_DEVELOPER_TOOLS §7).
// Each product is a fresh nanoframe whose remaining fraction falls by the
// shared construction step with the factory's worker quantum; every step is
// admitted. Nothing here allocates a unit, touches a world or draws from a
// random stream.

// unitViewerSpeeds are the product cycle's preview speeds: construction steps
// per preview tick. Scripts, nano queries and the spray keep the 30 Hz preview
// tick; only the fraction falls faster. A viewer preference, not a game speed.
var unitViewerSpeeds = [...]int{1, 4, 16}

// unitViewerProductHold is how long a completed product stays on its pad
// before the next nanoframe replaces it: two seconds of preview ticks. A
// battle product leaves under its own GetBuilt order while the factory's
// exit stays blocked; the viewer has no movement, so this viewer interval
// stands in for that departure [05 "Factory production lifecycle"].
const unitViewerProductHold = 2 * unitViewerTickRate

// unitViewerProducts is one factory Build action's product cycle. list holds
// the buildable entries in Build-tab order; skipped names every entry left
// out and why, in the same order.
type unitViewerProducts struct {
	list    []unitViewerProductEntry
	skipped []string
	load    func(*content.UnitDef) (*model.Model, error)
	next    int    // the list entry the next nanoframe builds
	serial  uint16 // nanoframe identifiers, one per product started
	current *unitViewerProduct
}

type unitViewerProductEntry struct {
	def      *content.UnitDef
	geometry *model.Model
	failed   bool
}

// unitViewerProduct is the nanoframe on the pad, or the completed product
// held there before the next one starts.
type unitViewerProduct struct {
	def       *content.UnitDef
	geometry  *model.Model
	anim      *unitViewerAnimation
	index     int
	id        uint16
	remaining float32
	steps     int // construction steps applied
	total     int // construction.WorkTicks, when ok
	totalOK   bool
	hold      int   // completion hold ticks left; nonzero only once complete
	failed    error // the renderer refused the product; set by Draw
}

// newUnitViewerProducts selects the cycle's products from the Build tab's
// entries: the retail list in library order. A Modern-only entry, a record
// another definition hides and a discovery-only record (whose gameplay
// fields were not parsed) are left out and reported. A product whose model
// later fails to load is reported the same way; nothing is substituted.
func newUnitViewerProducts(builds []unitViewerLink, hidden map[*content.UnitDef]bool, load func(*content.UnitDef) (*model.Model, error)) *unitViewerProducts {
	p := &unitViewerProducts{load: load}
	for _, l := range builds {
		def := l.Def
		switch {
		case def == nil:
		case l.Modern:
			p.skip(def, "Modern only")
		case hidden[def]:
			p.skip(def, "hidden")
		case def.DiscoveryOnly:
			p.skip(def, "no statistics")
		default:
			p.list = append(p.list, unitViewerProductEntry{def: def})
		}
	}
	return p
}

func (p *unitViewerProducts) skip(def *content.UnitDef, reason string) {
	p.skipped = append(p.skipped, strings.ToUpper(def.UnitName)+" ("+reason+")")
}

// start places a fresh nanoframe of the next buildable product, or leaves the
// pad empty when none remains. remaining starts at one [05 "Factory
// production lifecycle"]; the product's own script runs Create without the
// completion-time activation a finished unit receives.
func (p *unitViewerProducts) start(workerTime int32) *unitViewerProduct {
	p.current = nil
	for range len(p.list) {
		i := p.next
		p.next = (p.next + 1) % len(p.list)
		e := &p.list[i]
		if e.failed {
			continue
		}
		if e.geometry == nil {
			var err error
			if p.load == nil {
				err = fmt.Errorf("no content mounted")
			} else {
				e.geometry, err = p.load(e.def)
			}
			if err != nil || e.geometry == nil {
				e.failed = true
				p.skip(e.def, "no model")
				continue
			}
		}
		p.serial++
		if p.serial == 0 {
			p.serial = 1
		}
		product := &unitViewerProduct{def: e.def, geometry: e.geometry, index: i, id: p.serial, remaining: 1}
		product.total, product.totalOK = construction.WorkTicks(workerTime, e.def.BuildTime, unitViewerWorkLimit)
		product.anim = newUnitViewerAnimation(e.def, e.geometry, unitViewerAnimationOptions{action: unitViewerIdle, weapon: 1, nanoframe: &product.remaining})
		p.current = product
		return product
	}
	return nil
}

// fail drops a product the renderer refused, reports it and never retries it.
func (p *unitViewerProducts) fail(product *unitViewerProduct) {
	if product == nil || product.index >= len(p.list) {
		return
	}
	p.list[product.index].failed = true
	p.skip(product.def, "model unavailable")
	if p.current == product {
		p.current = nil
	}
}

// cancel discards an unfinished product; the next start builds it again from
// a fresh nanoframe. A completed product's successor is already next.
func (p *unitViewerProducts) cancel() {
	if c := p.current; c != nil && c.remaining != 0 && len(p.list) != 0 {
		p.next = c.index
	}
	p.current = nil
}

// unitViewerWorkStep is the remaining-fraction half of the shared
// construction step [05 R-WORK-01 §1]: a stored zero or a zero worker quantum
// commits nothing; otherwise the fraction falls by quantum/buildtime at
// working precision, clamps to 0..1 and narrows to the stored single
// precision. It is the same arithmetic construction.WorkTicks repeats, whose
// step count the tests require it to reproduce; resource demand and health
// are not modelled because every preview step is admitted.
func unitViewerWorkStep(old float32, quantum int32, buildTime int32) (float32, bool) {
	if old == 0 || quantum == 0 {
		return old, false
	}
	// buildtime 0 divides to an infinity and a negative one drives the
	// fraction upward; the clamp absorbs both, as the step does [05 R-WORK-01 §11].
	next := float64(old) - float64(float32(quantum))/float64(buildTime)
	switch {
	case next <= 0:
		return 0, true
	case next >= 1:
		return 1, true
	}
	return float32(next), true
}

// work applies up to speed construction steps of the factory's worker
// quantum and reports whether any was committed.
func (c *unitViewerProduct) work(quantum int32, speed int) bool {
	committed := false
	for range max(1, speed) {
		next, ok := unitViewerWorkStep(c.remaining, quantum, c.def.BuildTime)
		if !ok {
			break
		}
		c.remaining, committed = next, true
		c.steps++
	}
	return committed
}

// percent is the built share the product's own BUILD_PERCENT_LEFT port
// implies [04 §4.4]: 0 for a fresh frame, 100 once complete.
func (c *unitViewerProduct) percent() int32 { return 100 - cob.BuildPercentLeft(c.remaining) }

// productStatus is the status line for the product cycle.
func (a *unitViewerAnimation) productStatus() string {
	g := &a.build
	p := g.products
	if p == nil {
		return ""
	}
	var status string
	switch c := p.current; {
	case c == nil:
		status = "Build / no product to build"
	case c.hold > 0:
		status = "Built " + strings.ToUpper(c.def.UnitName)
		if n := p.peek(); n != nil {
			status += " / next " + strings.ToUpper(n.UnitName)
		}
	default:
		status = fmt.Sprintf("Building %s %d%% / %s", strings.ToUpper(c.def.UnitName), c.percent(), c.left(a.def, g.speed))
	}
	if len(p.skipped) != 0 {
		status += " / skipped " + strings.Join(p.skipped, ", ")
	}
	return status
}

// left is the unhindered time left at the preview speed, truncated to the
// Build tab's precision; refusals never become a guessed number.
func (c *unitViewerProduct) left(factory *content.UnitDef, speed int) string {
	speed = max(1, speed)
	switch {
	case construction.WorkerQuantum(factory.WorkerTime) == 0:
		return "no work"
	case c.def.BuildTime <= 0:
		return "work n/a"
	case !c.totalOK:
		return "over 24 h"
	}
	ticks := (max(0, c.total-c.steps) + speed - 1) / speed
	return fmt.Sprintf("%s left at %dx", unitViewerDuration(ticks), speed)
}

// peek is the product the cycle will start next, without loading it.
func (p *unitViewerProducts) peek() *content.UnitDef {
	for k := range len(p.list) {
		if e := p.list[(p.next+k)%len(p.list)]; !e.failed {
			return e.def
		}
	}
	return nil
}
