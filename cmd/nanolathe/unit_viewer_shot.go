package main

import (
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

func unitViewerShotSyntax() error {
	return fmt.Errorf("nanolathe: unit viewer capture: logical path <command line>, providers searched [--shot-unit-viewer], expected <unit ID>[/stats|/weapons|/build][/action=<idle|move|fly|land|aim|fire|build|stop|hit|death|wreck|on|off>][/ticks=N][/severity=N][/weapon=N][/speed=1|4|16][/only][/card][/query=<text>]")
}

// unitViewerShotPlan is a capture's scripted action: the buttons a user
// would press, then a fixed number of preview ticks run without a clock.
type unitViewerShotPlan struct {
	action           string
	ticks            int
	severity, weapon int
	speed            int // index into unitViewerSpeeds, plus one
}

func (p *unitViewerShotPlan) set(key, value string) bool {
	n, err := strconv.Atoi(value)
	switch key {
	case "action":
		switch value {
		case "idle", "move", "fly", "land", "aim", "fire", "build", "stop", "hit", "death", "wreck", "on", "off":
			p.action = value
			return true
		}
	case "ticks":
		p.ticks = n
		return err == nil && n >= 0 && n <= 3600
	case "severity":
		p.severity = n
		return err == nil && n >= 1 && n <= len(unitViewerSeverities)
	case "weapon":
		p.weapon = n
		return err == nil && n >= 1 && n <= 3
	case "speed":
		for i, speed := range unitViewerSpeeds {
			if err == nil && n == speed {
				p.speed = i + 1
				return true
			}
		}
	}
	return false
}

// apply loads the selected model, presses the plan's buttons and advances the
// preview. A land or stop capture first flies or builds for the default time.
// Death and Wreck stage their field synchronously and run it for the plan's
// field ticks; by default a death is caught two-thirds of a second after it
// happens and a wreck just after the turntable takes its corpse over.
func (p unitViewerShotPlan) apply(s *toolsScreen) error {
	if !s.model.ensureLoaded(s.cs, s.selected) {
		return s.model.err
	}
	if p.severity > 0 {
		s.severity = p.severity - 1
	}
	if p.weapon > 0 {
		s.weapon = p.weapon
	}
	if p.speed > 0 {
		s.speed = p.speed - 1
		s.model.setSpeed(unitViewerSpeeds[s.speed])
	}
	ticks := p.ticks
	if ticks == 0 {
		ticks = map[string]int{"idle": 90, "on": 90, "off": 90, "move": 60, "fly": 180, "land": 240, "aim": 60, "fire": 60, "build": 120, "stop": 90, "hit": 12,
			"death": unitViewerFieldSettle + 20, "wreck": unitViewerFieldSettle + unitViewerFieldHold}[p.action]
	}
	press := map[string]string{"idle": "IDLE", "move": "MOVE", "fly": "MOVE", "land": "MOVE", "aim": "FIRE", "fire": "FIRE", "build": "BUILD", "stop": "BUILD", "hit": "HIT", "death": "DEATH", "wreck": "WRECK", "on": "IDLE", "off": "IDLE"}[p.action]
	s.activateTool(press)
	if p.action == "death" || p.action == "wreck" {
		s.stageFieldNow(ticks)
		s.refreshControls()
		return nil
	}
	switch p.action {
	case "land", "stop":
		s.model.advance(180)
		s.activateTool(press)
	case "on", "off":
		// Idle presents the creation state; the toggle then drives the edge
		// machine only when the bit differs from what the capture asks for.
		if s.model.anim.activated() != (p.action == "on") {
			s.activateTool("POWER")
		}
	}
	s.model.advance(ticks)
	s.refreshControls()
	return nil
}

// This uses the shipped screen and geometry path, including clipping at the
// requested window size. It creates no session and writes no preferences.
func runUnitViewerShot(opts Options, cs *contentSet) error {
	size := opts.ShotSize
	if size == "" {
		size = "1440x900"
	}
	w, h, err := parseNLShotSize(size)
	if err != nil || w > 4096 || h > 4096 {
		return fmt.Errorf("nanolathe: unit viewer capture size: logical path <command line>, providers searched [--shot-size], expected WxH within 320x240..4096x4096")
	}
	s := &toolsScreen{}
	s.show(&gameShell{cs: cs, opts: opts})
	defer s.release()
	// A capture draws the field with the default presentation, never the
	// player's saved choices, and lays the stage out at the capture's size
	// before a field is staged at it.
	s.fieldRender = unitViewerFieldRender(settings.DefaultPresentation(), settings.DefaultDisplay())
	s.layout(float64(w), float64(h))
	// A capture never reads the settings key (DESIGN_MODS_MUTATORS §15.5);
	// --restrict gives the editor's draft, which the menu route compares
	// with an empty saved set, so Apply shows its count.
	s.restrict.draft = opts.Restrictions
	var plan unitViewerShotPlan
	if opts.ShotUnitViewer != "@tools" {
		// "<unit ID>[/<tab>][/action=<name>][/ticks=N][/severity=N][/weapon=N]"
		// captures a tab and, optionally, an action after N preview ticks;
		// /only turns Restricted only on, /card opens the editor as the
		// Nanolathe screen's card does, and /query= types a search.
		parts := strings.Split(opts.ShotUnitViewer, "/")
		unit, t := parts[0], unitViewerTabStats
		tabs := map[string]int{"": unitViewerTabStats, "stats": unitViewerTabStats, "weapons": unitViewerTabWeapons, "build": unitViewerTabBuild}
		for _, part := range parts[1:] {
			key, value, isOption := strings.Cut(strings.ToLower(part), "=")
			switch {
			case !isOption && key == "only":
				s.restrict.only = true
				continue
			case !isOption && key == "card":
				s.restrict.route, s.restrict.saved = unitViewerRestrictCard, s.restrict.draft
				continue
			case isOption && key == "query":
				s.query = value
				continue
			}
			if !isOption {
				tab, ok := tabs[key]
				if !ok {
					return unitViewerShotSyntax()
				}
				t = tab
				continue
			}
			if !plan.set(key, value) {
				return unitViewerShotSyntax()
			}
		}
		cat, err := cs.nlPreviewCatalog()
		if err != nil {
			return err
		}
		s.viewer, s.entries, s.infoTab = true, unitViewerEntries(cat), t
		s.tree = unitViewerBuildTree(cat, s.entries)
		s.features = cat.Features
		s.restrict.names, s.restrict.keys = unitViewerRestrictNames(cat, s.entries)
		s.buildPanel()
		s.panel.SetText("SEARCH", s.query)
		s.filter()
		found := false
		for _, entry := range s.entries {
			if strings.EqualFold(entry.Key, unit) || strings.EqualFold(entry.Def.UnitName, unit) {
				s.selectUnit(entry.Def)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("nanolathe: unit viewer capture: logical path units/%s, providers searched [compiled catalog], expected unit ID", unit)
		}
	}
	if plan.action != "" {
		if err := plan.apply(s); err != nil {
			return err
		}
	}
	g := &unitViewerShotGame{s: s, w: w, h: h, out: opts.Shot}
	ebiten.SetWindowVisible(false)
	ebiten.SetWindowSize(640, 480)
	if err := ebiten.RunGame(g); err != nil {
		return err
	}
	return g.err
}

type unitViewerShotGame struct {
	s      *toolsScreen
	w, h   int
	out    string
	done   bool
	err    error
	frames int
}

// unitViewerShotPictureFrames bounds how long a capture waits for the
// picture worker; a still-decoding picture is captured as its placeholder.
const unitViewerShotPictureFrames = 600

func (g *unitViewerShotGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *unitViewerShotGame) Layout(int, int) (int, int) { return g.w, g.h }
func (g *unitViewerShotGame) Draw(dst *ebiten.Image) {
	if g.done {
		return
	}
	g.s.Draw(dst)
	// The first draw measures how many rows fit. Reveal the subject using
	// those measured rows, exactly as a keyboard selection does.
	g.s.revealSelection()
	g.s.Draw(dst)
	// Pictures decode on the loader's worker; later frames upload them.
	if g.frames++; g.s.picsPending > 0 && g.frames < unitViewerShotPictureFrames {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, g.w, g.h))
	dst.ReadPixels(img.Pix)
	g.err = encodeShotPNG(g.out, img)
	if g.err == nil {
		g.err = g.s.model.err
	}
	g.done = true
}
