package modlibrary

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nanolathe-gg/nanolathe/internal/community"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// fullConfig exercises every section of a schema 2 config.
const fullConfig = `{
  "schema": 2,
  "id": "sample", "name": "Sample", "version": "1.0",
  "summary": "One line.", "homepage": "https://example.invalid",
  "requires": ["maps/foo.tnt"],
  "content": {
    "detect": ["unitsS"],
    "layout": {"units": "unitsS"},
    "limits": {"units": 4000, "weapons": 1024},
    "presentation": {"main_menu_version": "9.9", "build_menu_page_size": 10}
  },
  "rules": {
    "minimumGameplay": "community-3.9",
    "gameplay": "modern",
    "communityFeatures": {"airCorpseFall": true, "unitLimit": 900}
  },
  "settings": {
    "presentation": {"overview": 1, "playerDotColors": [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]},
    "switchAlt": 1,
    "audio": {"soundMode": 2}
  },
  "keys": {"profile": "zero", "bindings": {"stop": ["ctrl+q"]}},
  "locks": ["gameplay", "presentation.waterSurface"]
}`

func TestParseConfigDocument(t *testing.T) {
	meta, err := ParseMetadata([]byte(fullConfig))
	if err != nil {
		t.Fatal(err)
	}
	if !meta.HasConfig() || meta.Schema != 2 || meta.ID != "sample" || !reflect.DeepEqual(meta.Requires, []string{"maps/foo.tnt"}) {
		t.Fatalf("identity = %+v", meta)
	}
	c := meta.Config
	if c.Content.Name != "sample" || c.Content.Directories["units"] != "unitsS" || c.Content.Limits.Units != 4000 || c.Content.Presentation.MainMenuVersion != "9.9" {
		t.Fatalf("content = %+v", c.Content)
	}
	if c.Rules.MinimumGameplay != gameplay.Community39 || c.Rules.Gameplay != gameplay.Modern || c.Rules.CommunityFeatures == nil {
		t.Fatalf("rules = %+v", c.Rules)
	}
	mainline, _ := community.Table(community.Mainline)
	want := mainline
	want.AirCorpseFall, want.UnitLimit = true, 900
	if *c.Rules.CommunityFeatures != want {
		t.Fatalf("communityFeatures = %+v, want the mainline table with two fields changed", *c.Rules.CommunityFeatures)
	}
	// The flat fields every reader looks at carry the config's values.
	if meta.MinimumGameplay != "community-3.9" || meta.Controls != "zero" || meta.BuildMenuPageSize != 10 {
		t.Fatalf("flat recommendations = %q, %q, %d", meta.MinimumGameplay, meta.Controls, meta.BuildMenuPageSize)
	}
	if c.Keys == nil || c.Keys.Profile != "zero" || !reflect.DeepEqual(c.Keys.Bindings["stop"], []string{"ctrl+q"}) {
		t.Fatalf("keys = %+v", c.Keys)
	}
	if !reflect.DeepEqual(c.Locks, []string{"gameplay", "presentation.waterSurface"}) {
		t.Fatalf("locks = %v", c.Locks)
	}
	sources := meta.CommunitySources()
	if len(sources) != 1 || sources[0].Base == nil || *sources[0].Base != want {
		t.Fatalf("community sources = %+v", sources)
	}
	resolved, err := community.Resolve(false, sources...)
	if err != nil || resolved != want {
		t.Fatalf("resolved = %+v, %v", resolved, err)
	}
	if strict, _ := community.Resolve(true, sources...); strict != (community.Features{}) {
		t.Fatalf("Strict took the config's table: %+v", strict)
	}

	// A config without sections is still a config: plain retail-shaped
	// content under its own name, no rules, no preferences.
	bare, err := ParseMetadata([]byte(`{"schema":2,"id":"bare","name":"Bare","version":"1"}`))
	if err != nil || !bare.HasConfig() || bare.Content().Name != "bare" || len(bare.Content().Directories) != 0 || bare.CommunitySources() != nil || bare.Controls != "" {
		t.Fatalf("bare config = %+v, %v", bare, err)
	}
}

func TestParseConfigDocumentRefusals(t *testing.T) {
	head := `"schema":2,"id":"a","name":"A","version":"1"`
	for name, tc := range map[string]struct{ doc, want string }{
		"unknown key":             {`{` + head + `,"colour":"red"}`, "unknown field"},
		"schema 1 key":            {`{` + head + `,"contentProfile":"prota"}`, "unknown field"},
		"flat minimum":            {`{` + head + `,"minimumGameplay":"modern"}`, "unknown field"},
		"unknown content key":     {`{` + head + `,"content":{"directories":{}}}`, "content section"},
		"nested layout":           {`{` + head + `,"content":{"layout":{"units":"a/b"}}}`, "not a single directory"},
		"nested marker":           {`{` + head + `,"content":{"detect":["a/b"]}}`, "not a single directory"},
		"negative limit":          {`{` + head + `,"content":{"limits":{"units":-1}}}`, "negative"},
		"unknown rules key":       {`{` + head + `,"rules":{"table":"prota"}}`, "unknown field"},
		"unreserved gameplay":     {`{` + head + `,"rules":{"gameplay":"modern-ai"}}`, "not a reserved gameplay word"},
		"unreserved minimum":      {`{` + head + `,"rules":{"minimumGameplay":"strict"}}`, "not a reserved gameplay word"},
		"gameplay below minimum":  {`{` + head + `,"rules":{"minimumGameplay":"modern","gameplay":"community-3.9"}}`, "below its minimum"},
		"unknown feature":         {`{` + head + `,"rules":{"communityFeatures":{"modernJamRelease":false}}}`, "communityFeatures"},
		"feature out of bounds":   {`{` + head + `,"rules":{"communityFeatures":{"unitLimit":5}}}`, "communityFeatures"},
		"settings not an object":  {`{` + head + `,"settings":[1]}`, "not an object"},
		"unknown setting":         {`{` + head + `,"settings":{"presentation":{"sparkle":1}}}`, "does not read as settings"},
		"setting of another type": {`{` + head + `,"settings":{"switchAlt":"yes"}}`, "does not read as settings"},
		"gameplay setting":        {`{` + head + `,"settings":{"gameplay":"modern"}}`, "rules.gameplay"},
		"feature setting":         {`{` + head + `,"settings":{"gameplayFeatures":{}}}`, "rules.communityFeatures"},
		"unit limit setting":      {`{` + head + `,"settings":{"unitLimit":500}}`, "rules.communityFeatures.unitLimit"},
		"key setting":             {`{` + head + `,"settings":{"keyBindings":{}}}`, "the keys section"},
		"mod setting":             {`{` + head + `,"settings":{"mod":{"id":"x"}}}`, "player's own choice"},
		"restrictions setting":    {`{` + head + `,"settings":{"restrictions":{"armpw":0}}}`, `names "restrictions"`},
		"unknown keys key":        {`{` + head + `,"keys":{"layout":"x"}}`, "keys section"},
		"unknown keyboard":        {`{` + head + `,"keys":{"profile":"dvorak"}}`, "keyboard profile"},
		"unknown action":          {`{` + head + `,"keys":{"bindings":{"teleport":["t"]}}}`, "not a rebindable action"},
		"unreadable chord":        {`{` + head + `,"keys":{"bindings":{"stop":["alt+s"]}}}`, "not a chord"},
		"unknown lock":            {`{` + head + `,"locks":["presentation.sparkle"]}`, "names no setting"},
		"lock past a leaf":        {`{` + head + `,"locks":["switchAlt.x"]}`, "names no setting"},
		"repeated lock":           {`{` + head + `,"locks":["gameplay","gameplay"]}`, "twice"},
		"restrictions lock":       {`{` + head + `,"locks":["restrictions"]}`, "unit restrictions"},
		"trailing document":       {`{` + head + `}{}`, "reading mod"},
	} {
		_, err := ParseMetadata([]byte(tc.doc))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: ParseMetadata = %v, want a refusal containing %q", name, err, tc.want)
			continue
		}
		if !strings.HasPrefix(err.Error(), "nanolathe: ") || !strings.Contains(err.Error(), "providers searched [") {
			t.Errorf("%s: diagnostic %q is not in the standard shape", name, err)
		}
	}
}

// The settings document is a layer, not a replacement: ApplySettings writes
// only what it names and SettingsPaths says what that is.
func TestConfigSettingsLayer(t *testing.T) {
	meta, err := ParseMetadata([]byte(fullConfig))
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Defaults()
	s.Presentation.WaterSurface = 0
	s.Audio.MixingBuffers = 64
	if err := meta.Config.ApplySettings(&s); err != nil {
		t.Fatal(err)
	}
	if s.Presentation.Overview != settings.OverviewMegamap || s.SwitchAlt != 1 || s.Audio.SoundMode != settings.SoundMode3D || s.Presentation.PlayerDotColors != [10]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		t.Fatalf("named settings were not applied: %+v", s)
	}
	if s.Presentation.WaterSurface != 0 || s.Audio.MixingBuffers != 64 {
		t.Fatalf("an unnamed setting changed: water %d, voices %d", s.Presentation.WaterSurface, s.Audio.MixingBuffers)
	}
	want := []string{"audio.soundMode", "presentation.overview", "presentation.playerDotColors", "switchAlt"}
	if got := meta.Config.SettingsPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SettingsPaths = %v, want %v", got, want)
	}
	var none *Config
	if none.SettingsPaths() != nil || none.ApplySettings(&s) != nil || none.CommunitySources() != nil || none.ControlsPreset() != "" {
		t.Fatal("a nil config declared something")
	}
}

func TestSettingsPathExists(t *testing.T) {
	for path, want := range map[string]bool{
		"gameplay": true, "presentation": true, "presentation.waterSurface": true, "audio.soundMode": true,
		"keyBindings.profile": true, "skirmish.numPlayers": true, "switchAlt": true,
		"": false, "presentation.sparkle": false, "Presentation.waterSurface": false, "presentation.water": false, "switchAlt.x": false, "presentation.": false,
	} {
		if got := settingsPathExists(path); got != want {
			t.Errorf("settingsPathExists(%q) = %v, want %v", path, got, want)
		}
	}
}

// --mod-config reads a stand-alone file, which must be a schema 2 config;
// a diagnostic names the file rather than the canonical name.
func TestReadConfigFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	legacy := filepath.Join(dir, "legacy.json")
	broken := filepath.Join(dir, "broken.json")
	for path, body := range map[string]string{
		good:   `{"schema":2,"id":"g","name":"G","version":"1"}`,
		legacy: `{"schema":1,"id":"l","name":"L","version":"1","contentProfile":"prota"}`,
		broken: `{"schema":2,"id":"b","name":"B","version":"1","locks":["nope"]}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if meta, err := ReadConfigFile(good); err != nil || !meta.HasConfig() || meta.ID != "g" {
		t.Fatalf("ReadConfigFile(good) = %+v, %v", meta, err)
	}
	if _, err := ReadConfigFile(legacy); err == nil || !strings.Contains(err.Error(), "carries no Nanolathe config") || !strings.Contains(err.Error(), legacy) {
		t.Fatalf("ReadConfigFile(schema 1) = %v", err)
	}
	if _, err := ReadConfigFile(broken); err == nil || !strings.Contains(err.Error(), "logical path "+broken+" locks") {
		t.Fatalf("ReadConfigFile(broken) = %v, want the file named", err)
	}
	if _, err := ReadConfigFile(filepath.Join(dir, "absent.json")); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("ReadConfigFile(absent) = %v", err)
	}
	for _, name := range []string{"prota", " Zero ", "retail", "ESCALATION", "mayhem"} {
		if !IsRemovedProfile(name) {
			t.Errorf("IsRemovedProfile(%q) = false", name)
		}
	}
	if IsRemovedProfile(good) || IsRemovedProfile("") {
		t.Error("a config path or an empty preference read as a removed profile")
	}
}
