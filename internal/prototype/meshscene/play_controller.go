package meshscene

// The native match controller is an isolated host for the existing local HUD
// state and typed command boundary (DESIGN_INTERFACE_HUD_INPUT §2.5; I6).
// Its compact layout is prototype presentation; the session decides gameplay.
import (
	"fmt"
	"path"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/hud"
	"github.com/nanolathe-gg/nanolathe/internal/input"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/pool"
	"github.com/nanolathe-gg/nanolathe/internal/session"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

type playRect struct{ x, y, w, h float32 }

func (r playRect) contains(x, y float32) bool {
	return x >= r.x && y >= r.y && x < r.x+r.w && y < r.y+r.h
}

// PlayController is owned exclusively by the simulation worker. All selection
// is allocation-reference-local; every world edit goes through HumanCommand.
type PlayController struct {
	cat                                                            *content.Catalog
	sess                                                           *session.Session
	models                                                         map[string]*model.Model
	local                                                          *hud.LocalInterface
	viewport                                                       [2]int
	scale                                                          float32
	mapW, mapH                                                     float32
	atlas                                                          Texture
	font                                                           playFont
	work                                                           frame.Frame
	facts                                                          []frame.InterfaceFacts
	dropped                                                        uint64
	pointer                                                        NativeEvent
	pointerCamera                                                  [3]float32
	pointerKnown                                                   bool
	drag                                                           bool
	dragStart, dragEnd                                             [3]numeric.Fixed
	pressX, pressY                                                 float32
	pressShift                                                     bool
	latch                                                          input.Latch
	product                                                        string
	page                                                           int
	menuBuilder                                                    pool.Handle
	siteX, siteZ                                                   int32
	site                                                           world.PlacementResult
	siteError                                                      error
	message                                                        string
	messageUntil                                                   uint32
	lastTick                                                       uint32
	events, selectionEvents, orders, builds, errors, minimapClicks uint64
	lastCommand                                                    string
}

func NewPlayController(_ vfs.FSOps, cat *content.Catalog, sess *session.Session, models []*model.Model, viewport [2]int) (*PlayController, error) {
	if cat == nil || sess == nil || viewport[0] <= 0 || viewport[1] <= 0 || sess.World == nil {
		return nil, fmt.Errorf("nanolathe: native match controller unavailable: logical path battle, providers searched [session], expected catalog, world and positive viewport")
	}
	p := &PlayController{cat: cat, sess: sess, viewport: viewport, local: hud.NewLocalInterface(), models: make(map[string]*model.Model), latch: input.LatchNormal}
	p.scale = max(float32(1), min(float32(viewport[0])/1440, float32(viewport[1])/900))
	p.mapW, p.mapH = float32(sess.World.CellW*16), float32(sess.World.CellH*16)
	for _, m := range models {
		if m != nil {
			p.models[playModelKey(m.Name)] = m
		}
	}
	p.atlas, p.font = newPlayFont()
	if sess.Snapshot != nil {
		p.local.AdoptStatusWords(sess.Snapshot.Current())
		p.dropped, _ = sess.Snapshot.InterfaceFactsDropped()
	}
	sess.SetLocalInterfaceFacts(true)
	return p, nil
}
func playModelKey(name string) string {
	return strings.ToLower(strings.TrimSuffix(path.Base(name), path.Ext(name)))
}
func (p *PlayController) HullModel(name string) *model.Model { return p.models[playModelKey(name)] }
func (p *PlayController) OverlayAtlas() Texture              { return p.atlas }
func (p *PlayController) Report() map[string]any {
	return map[string]any{"input_events": p.events, "selection_events": p.selectionEvents, "selected_units": p.local.SelectionCount(), "order_commands": p.orders, "build_commands": p.builds, "command_errors": p.errors, "minimap_recenter_clicks": p.minimapClicks, "last_command": p.lastCommand, "armed_product": p.product, "armed_order": p.latch.String(), "status": p.message}
}

// compose detaches every presentation section LocalInterface writes. The
// borrowed committed frame and all of its slice backing arrays remain intact.
func (p *PlayController) compose(f *frame.Frame) *frame.Frame {
	if f == nil {
		return nil
	}
	p.lastTick = f.Tick
	live := func(r pool.UnitRef) bool { v, ok := playUnit(f, r.Handle); return ok && v.AllocationSerial == r.Serial }
	if p.sess.Snapshot != nil {
		p.facts = p.sess.Snapshot.DrainInterfaceFacts(p.facts[:0])
		for _, fact := range p.facts {
			if fact.Tick <= f.Tick {
				p.local.Advance(fact)
			}
		}
		if dropped, _ := p.sess.Snapshot.InterfaceFactsDropped(); dropped != p.dropped {
			p.dropped = dropped
			p.local.ResyncAfterGap(func(r pool.UnitRef) bool {
				v, ok := playUnit(f, r.Handle)
				return ok && v.AllocationSerial == r.Serial && p.ready(f, v)
			})
		}
	}
	p.local.ObserveTick(f.Tick)
	p.local.Prune(live)
	savedUnits := p.work.Units
	savedContacts := p.work.Radar.Contacts
	p.work = *f
	p.work.Units = append(savedUnits[:0], f.Units...)
	p.work.Radar.Contacts = append(savedContacts[:0], f.Radar.Contacts...)
	p.work.Selection = frame.SelectionView{LocalPlayer: p.sess.LocalOwner}
	p.work.CommandPage = frame.CommandPageView{}
	p.local.ComposeSelection(&p.work)
	hud.ComposeCommandPage(&p.work.CommandPage, &p.work, p.cat, p.sess.CommandPageProducts)
	if p.menuBuilder != p.work.CommandPage.Builder {
		p.page = 0
		p.menuBuilder = p.work.CommandPage.Builder
	}
	return &p.work
}
func playUnit(f *frame.Frame, h pool.Handle) (frame.UnitView, bool) {
	if f != nil {
		for _, v := range f.Units {
			if v.Slot == h {
				return v, true
			}
		}
	}
	return frame.UnitView{}, false
}
func (p *PlayController) notify(s string) { p.message = s; p.messageUntil = p.lastTick + 150 }
func (p *PlayController) enqueue(c session.HumanCommand, label string) bool {
	if err := p.sess.EnqueueHumanCommand(c); err != nil {
		p.errors++
		p.notify(err.Error())
		return false
	}
	p.lastCommand = label
	p.notify(label)
	return true
}
func (p *PlayController) selected(f *frame.Frame) []pool.Handle {
	return append([]pool.Handle(nil), f.Selection.Handles...)
}
func (p *PlayController) products(f *frame.Frame) []string { return hud.AllowedBuildProducts(p.cat, f) }
func (p *PlayController) panel() playRect {
	return playRect{0, 56 * p.scale, 300 * p.scale, float32(p.viewport[1]) - 92*p.scale}
}
func (p *PlayController) minimap() playRect {
	s := p.scale
	maxW, maxH := float32(268)*s, float32(268)*s
	factor := min(maxW/p.mapW, maxH/p.mapH)
	return playRect{16*s + (maxW-p.mapW*factor)/2, 76*s + (maxH-p.mapH*factor)/2, p.mapW * factor, p.mapH * factor}
}
func (p *PlayController) overHUD(x, y float32) bool {
	return y < 56*p.scale || y >= float32(p.viewport[1])-36*p.scale || p.panel().contains(x, y)
}
