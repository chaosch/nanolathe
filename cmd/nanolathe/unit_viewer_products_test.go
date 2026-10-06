package main

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/construction"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/model"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/testsupport"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

func TestUnitViewerProductCycleFollowsTheRetailListAndReportsSkips(t *testing.T) {
	unit := func(name string) *content.UnitDef { return &content.UnitDef{UnitName: name, ObjectName: name} }
	a, modern, hidden, discovery, broken, f := unit("a"), unit("modern"), unit("hidden"), unit("disc"), unit("broken"), unit("f")
	discovery.DiscoveryOnly = true
	builds := []unitViewerLink{{Def: a}, {Def: modern, Modern: true}, {Def: hidden}, {Def: discovery}, {Def: broken}, {Def: f}}
	loads := map[*content.UnitDef]int{}
	p := newUnitViewerProducts(builds, map[*content.UnitDef]bool{hidden: true}, func(def *content.UnitDef) (*model.Model, error) {
		loads[def]++
		if def == broken {
			return nil, errors.New("missing")
		}
		_, mdl := unitViewerAnimationFixture()
		return mdl, nil
	})
	var order []*content.UnitDef
	for range 4 {
		c := p.start(300)
		if c == nil || c.remaining != 1 {
			t.Fatal("no fresh nanoframe")
		}
		order = append(order, c.def)
	}
	// Build-tab order, skipping what a battle could not build here and a
	// product whose model fails, which is never retried.
	if !reflect.DeepEqual(order, []*content.UnitDef{a, f, a, f}) || loads[broken] != 1 || loads[a] != 1 {
		t.Fatalf("cycle order %v, loads %v", order, loads)
	}
	want := []string{"MODERN (Modern only)", "HIDDEN (hidden)", "DISC (no statistics)", "BROKEN (no model)"}
	if !reflect.DeepEqual(p.skipped, want) {
		t.Fatalf("skipped %q", p.skipped)
	}
	// A product cancelled before completion is rebuilt from a fresh frame.
	c := p.start(300)
	c.remaining = 0.5
	p.cancel()
	if again := p.start(300); again.def != c.def || again.remaining != 1 || again.id == c.id {
		t.Fatal("a cancelled product did not restart as a fresh nanoframe")
	}
	if none := newUnitViewerProducts([]unitViewerLink{{Def: modern, Modern: true}}, nil, nil); none.start(300) != nil {
		t.Fatal("a cycle with no buildable product placed one")
	}
}

func TestUnitViewerProductStepsMatchConstructionWorkTicks(t *testing.T) {
	for _, worker := range []int32{30, 59, 60, 80, 100, 200, 300, 600, 1000, 65535} {
		for _, buildTime := range []int32{1, 7, 50, 100, 1000, 6760, 33333, 95897, 123457} {
			want, ok := construction.WorkTicks(worker, buildTime, unitViewerWorkLimit)
			if !ok {
				continue
			}
			c := &unitViewerProduct{def: &content.UnitDef{BuildTime: buildTime}, remaining: 1}
			ticks := 0
			for c.remaining != 0 && ticks <= want {
				if !c.work(construction.WorkerQuantum(worker), 1) {
					t.Fatal("an admitted step committed nothing")
				}
				ticks++
			}
			if ticks != want || c.steps != want {
				t.Fatalf("workertime %d buildtime %d: %d steps, construction.WorkTicks %d", worker, buildTime, ticks, want)
			}
			// A speed of k applies k steps per tick and stops at zero.
			c = &unitViewerProduct{def: c.def, remaining: 1}
			for ticks = 0; c.remaining != 0; ticks++ {
				c.work(construction.WorkerQuantum(worker), 16)
			}
			if ticks != (want+15)/16 || c.steps != want {
				t.Fatalf("speed 16 took %d ticks and %d steps for %d", ticks, c.steps, want)
			}
		}
	}
	// The step's refusals and clamps [05 R-WORK-01 §1][05 R-WORK-01 §11].
	if _, ok := unitViewerWorkStep(1, 0, 100); ok {
		t.Fatal("a zero quantum committed work")
	}
	if _, ok := unitViewerWorkStep(0, 10, 100); ok {
		t.Fatal("a stored zero committed work")
	}
	if next, ok := unitViewerWorkStep(1, 10, 0); !ok || next != 0 {
		t.Fatal("buildtime 0 did not complete in one step")
	}
	if next, ok := unitViewerWorkStep(0.95, 10, -100); !ok || next != 1 {
		t.Fatal("a negative buildtime did not clamp at one")
	}
}

// viewerFactoryFixture is a factory whose Activate raises the stance, whose
// pad is piece 1 and whose nano piece is piece 2, with a product list.
func viewerFactoryFixture(t *testing.T, buildTime int32, names ...string) (*unitViewerAnimation, []*content.UnitDef) {
	t.Helper()
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerReturn}},
		viewerScriptFixture{"Activate", viewerTrace(1, viewerPush, 5, viewerPush, 1, viewerSet)},
		viewerScriptFixture{"QueryBuildInfo", viewerTrace(2, viewerPush, 1, viewerPopLocal, 0)},
		viewerScriptFixture{"StartBuilding", viewerTrace(3)},
		viewerScriptFixture{"QueryNanoPiece", viewerTrace(4, viewerPush, 2, viewerPopLocal, 0)},
		viewerScriptFixture{"StopBuilding", viewerTrace(5)},
		viewerScriptFixture{"Deactivate", viewerTrace(6)},
		viewerReport,
		viewerScriptFixture{"Reset", []uint32{viewerPush, 0, viewerPopStatic, 0, viewerPush, 0, viewerReturn}},
	)
	def.Builder, def.BMCode, def.WorkerTime = true, 0, 300
	var products []*content.UnitDef
	var links []unitViewerLink
	for _, name := range names {
		product, _ := unitViewerAnimationFixture(
			viewerScriptFixture{"Create", []uint32{viewerPush, 17, viewerRead, viewerMoveNow, 0, 0, viewerReturn}},
		)
		product.UnitName, product.BuildTime, product.FootprintX, product.FootprintZ, product.ModelTopFixed = name, buildTime, 2, 2, 20<<16
		products = append(products, product)
		links = append(links, unitViewerLink{Def: product})
	}
	cycle := newUnitViewerProducts(links, nil, func(*content.UnitDef) (*model.Model, error) {
		_, m := unitViewerAnimationFixture()
		return m, nil
	})
	a := newUnitViewerAnimation(def, mdl, unitViewerAnimationOptions{action: unitViewerBuilding, weapon: 1, products: cycle, speed: 1})
	return a, products
}

func TestUnitViewerFactoryBuildsEachProductThenHoldsThePad(t *testing.T) {
	a, products := viewerFactoryFixture(t, 50, "first", "second")
	steps, ok := construction.WorkTicks(300, 50, unitViewerWorkLimit)
	if !ok {
		t.Fatal("fixture product has no work estimate")
	}
	unitViewerAdvance(a, 2)
	c := a.product()
	// The pad query, the fresh nanoframe and the building edge come together;
	// the first work step follows at once [05 "Factory production lifecycle"].
	if c == nil || c.def != products[0] || c.id != 1 || c.steps != 1 || viewerTraced(a) != 124 || len(a.spray.records) != 1 {
		t.Fatalf("first product start: trace %d, product %+v", viewerTraced(a), c)
	}
	// The product's own script reads its fraction through BUILD_PERCENT_LEFT.
	if got := c.anim.poses()[0].Tx; got != 100 {
		t.Fatalf("product Create read BUILD_PERCENT_LEFT %d at a fresh frame", got)
	}
	ticks := 1
	for c.remaining != 0 {
		unitViewerAdvance(a, 1)
		ticks++
		if ticks > steps {
			t.Fatal("product outlived its WorkTicks steps")
		}
	}
	if ticks != steps || c.steps != steps || a.build.state != unitViewerBuildHolding || a.building() || c.percent() != 100 {
		t.Fatalf("completion after %d ticks, want %d; state %d", ticks, steps, a.build.state)
	}
	// Completion lowers the building edge; activation stays raised for the
	// next product, so no Deactivate and no second Activate run.
	a.script.bridge.Query("Reset", [4]int32{})
	records := len(a.spray.records)
	unitViewerAdvance(a, unitViewerProductHold-1)
	if next := a.product(); next != c || viewerTraced(a) != 5 || !a.activated() || a.nanoPiece() != -1 || len(a.spray.records) > records {
		t.Fatalf("hold ran work or replaced the product early: trace %d", viewerTraced(a))
	}
	unitViewerAdvance(a, 1)
	next := a.product()
	if next == nil || next.def != products[1] || next.id != 2 || next.remaining == 1 || viewerTraced(a) != 524 {
		t.Fatalf("second product did not start after the hold: trace %d", viewerTraced(a))
	}
	// Stop discards the unfinished product; resuming rebuilds it fresh.
	a.toggleBuild()
	unitViewerAdvance(a, 2)
	if a.product() != nil || a.activated() || a.buildEngaged() {
		t.Fatal("stop left the product cycle running")
	}
	a.toggleBuild()
	unitViewerAdvance(a, 3)
	if again := a.product(); again == nil || again.def != products[1] || again.id != 3 {
		t.Fatal("resumed build did not restart the interrupted product")
	}
}

func TestUnitViewerProductSpeedOnlyMultipliesSteps(t *testing.T) {
	a, _ := viewerFactoryFixture(t, 50, "only")
	a.build.speed = 4
	steps, _ := construction.WorkTicks(300, 50, unitViewerWorkLimit)
	unitViewerAdvance(a, 2)
	c := a.product()
	working := 1
	for c.remaining != 0 {
		unitViewerAdvance(a, 1)
		working++
	}
	// One nano query and one spray record per preview tick at any speed.
	if working != (steps+3)/4 || c.steps != steps || len(a.spray.records) != working {
		t.Fatalf("speed 4: %d ticks, %d steps, %d records; want %d ticks", working, c.steps, len(a.spray.records), (steps+3)/4)
	}
}

func TestUnitViewerSprayParticleRules(t *testing.T) {
	s := newUnitViewerSpray()
	f := func(n int64) numeric.Fixed { return numeric.Fixed(n << 16) }
	// A degenerate target 40 world units away: every particle lives
	// trunc(40/4) = 10 ticks, and five spawn on each of two ticks.
	s.emit(100, [3]numeric.Fixed{}, [3]numeric.Fixed{f(40)}, [3]numeric.Fixed{f(40)})
	r := s.records[0]
	if len(r.particles) != 5 {
		t.Fatalf("%d particles at emission", len(r.particles))
	}
	for i, p := range r.particles {
		if p.color != 0xa0|uint8(1+i) || p.expiry != 110 || p.vel != [3]numeric.Fixed{f(4)} {
			t.Fatalf("particle %d: colour %#x expiry %d velocity %v", i, p.color, p.expiry, p.vel)
		}
	}
	s.update(100)
	s.update(101)
	if n := len(s.records[0].particles); n != 10 {
		t.Fatalf("second spawn tick left %d particles", n)
	}
	for tick := uint32(102); tick <= 112; tick++ {
		s.update(tick)
		for _, p := range s.records[0].particles {
			if n := p.color & 0x0f; p.color&0xf0 != 0xa0 || n < 1 || n > 7 {
				t.Fatalf("colour %#x left the 0xa1..0xa7 shimmer", p.color)
			}
		}
	}
	if n := len(s.records[0].particles); n != 0 {
		t.Fatalf("%d particles outlived their lifetime", n)
	}
	s.update(113)
	if len(s.records) != 0 {
		t.Fatal("an empty record was not removed")
	}
	// Zero-length hops are discarded before they are written.
	s.emit(5, [3]numeric.Fixed{f(1)}, [3]numeric.Fixed{f(1)}, [3]numeric.Fixed{f(1)})
	if len(s.records[0].particles) != 0 {
		t.Fatal("a zero-length particle was written")
	}
	// Six private picks per particle; nothing else draws.
	a, b := newUnitViewerSpray(), newUnitViewerSpray()
	a.emit(0, [3]numeric.Fixed{}, [3]numeric.Fixed{f(40)}, [3]numeric.Fixed{f(80)})
	for range 30 {
		b.pick()
	}
	if a.pick() != b.pick() {
		t.Fatal("an emission did not take exactly thirty private picks")
	}
	// The narrowed box is the 4/11..7/11 span.
	if o, e := unitViewerNarrow([3]numeric.Fixed{}, [3]numeric.Fixed{f(110), f(-110), 0}); o != [3]numeric.Fixed{f(40), f(-40), 0} || e != [3]numeric.Fixed{f(30), f(-30), 0} {
		t.Fatalf("narrowed origin %v extent %v", o, e)
	}
}

func TestUnitViewerSprayOnlyWhileWorking(t *testing.T) {
	def, mdl := unitViewerAnimationFixture(
		viewerScriptFixture{"Create", []uint32{viewerReturn}},
		viewerScriptFixture{"StartBuilding", []uint32{viewerPush, 100, viewerSleep, viewerPush, 5, viewerPush, 1, viewerSet, viewerPush, 0, viewerReturn}},
		viewerScriptFixture{"QueryNanoPiece", []uint32{viewerPush, 2, viewerPopLocal, 0, viewerPush, 0, viewerReturn}},
		viewerScriptFixture{"StopBuilding", []uint32{viewerPush, 0, viewerReturn}},
	)
	def.Builder, def.BMCode, def.FootprintX, def.FootprintZ, def.ModelTopFixed = true, 1, 2, 2, 10<<16
	idle := newUnitViewerAnimation(def, mdl, unitViewerAnimationOptions{action: unitViewerIdle, weapon: 1, reach: 30})
	unitViewerAdvance(idle, 30)
	if len(idle.spray.records) != 0 {
		t.Fatal("Idle sprayed")
	}
	a := newUnitViewerAnimation(def, mdl, unitViewerAnimationOptions{action: unitViewerBuilding, weapon: 1, reach: 30})
	emitted := 0
	for range 20 {
		unitViewerAdvance(a, 1)
		if !a.building() && len(a.spray.records) != 0 {
			t.Fatal("spray before the build stance")
		}
		if a.building() {
			emitted++
			if n := len(a.spray.records); n == 0 || a.spray.records[n-1].windowEnd != uint32(a.ticks)+1 {
				t.Fatal("a work step emitted no record")
			}
		}
	}
	if emitted == 0 {
		t.Fatal("mobile build never reached its stance")
	}
	a.toggleBuild()
	for range 40 {
		unitViewerAdvance(a, 1)
	}
	if len(a.spray.records) != 0 || a.building() {
		t.Fatal("spray continued after Stop")
	}
	// A factory without a product sprays nothing: there is no target.
	f, _ := viewerFactoryFixture(t, 50)
	unitViewerAdvance(f, 10)
	if !f.building() || len(f.spray.records) != 0 || f.product() != nil {
		t.Fatal("a factory with no product sprayed")
	}
}

func TestUnitViewerSprayMatchesPreviewProjectionRetail(t *testing.T) {
	cs, err := openContent(Options{Root: testsupport.RetailRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	cat, err := cs.nlPreviewCatalog()
	if err != nil {
		t.Fatal(err)
	}
	entries := unitViewerEntries(cat)
	tree := unitViewerBuildTree(cat, entries)
	preview, err := client.NewModelPreviewRenderer(cs.unmappedMount, cs.presentation.TeamLogos)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"armlab", "corap"} {
		t.Run(name, func(t *testing.T) {
			def, _ := cat.Unit(name)
			m := unitViewerModel{preview: preview, mount: cs.unmappedMount, builds: tree.builds[def], hidden: tree.hidden, speed: 4}
			if !m.ensureLoaded(cs, def) {
				t.Fatal(m.err)
			}
			m.setAnimation(unitViewerBuilding, 1)
			for range 300 {
				if m.anim.product() != nil && m.anim.nanoPiece() >= 0 {
					break
				}
				m.updateAnimation(1.0 / unitViewerTickRate)
			}
			c, g := m.anim.product(), &m.anim.build
			if c == nil || g.pad < 0 || g.nano < 0 {
				t.Fatalf("no product on a pad: %s", m.anim.note)
			}
			parent := vfs.ResourcePath("objects3d", def.ObjectName, "3do")
			placement, err := preview.PiecePlacement(parent, m.poses, m.geometry.Pieces[g.pad].Name)
			if err != nil {
				t.Fatal(err)
			}
			// The spray's target sits exactly where the attachment is placed.
			if pad, _ := unitViewerRootLocal(m.geometry, m.poses, g.pad); pad != placement.Position {
				t.Fatalf("spray target %v, attachment placement %v", pad, placement.Position)
			}
			src, _ := unitViewerRootLocal(m.geometry, m.poses, g.nano)
			for _, yaw := range []uint16{0, 16384, 40960, 61000} {
				h, p, b := unitViewerOrientation(yaw, 8192)
				if _, _, _, err := m.record(def, h, p, b, 1, 984, 628); err != nil || !m.view.ok {
					t.Fatalf("record: %v", err)
				}
				project, _ := m.rootLocalProjector()
				x, y := project(src)
				opts := client.ModelPreviewOptions{Model: parent, Width: m.view.w, Height: m.view.h, Heading: h, Pitch: p, Bank: b,
					Structure: true, KeyPlane: def.ZBuffer, PiecePoses: m.poses, Attachment: m.attachment(parent)}
				points, err := preview.ProjectedPieces(opts, m.view.projection, []client.ModelPreviewPiece{{Name: m.geometry.Pieces[g.nano].Name}})
				if err != nil {
					t.Fatal(err)
				}
				// The spray leaves the drawn nano piece at every orbit angle,
				// to within the fixed-point rounding of composing below and
				// at the root separately.
				if math.Abs(points[0].X-x) > 0.01 || math.Abs(points[0].Y-y) > 0.01 {
					t.Fatalf("yaw %d: spray source (%.3f, %.3f), drawn nano piece (%.3f, %.3f)", yaw, x, y, points[0].X, points[0].Y)
				}
			}
		})
	}
}
