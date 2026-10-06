package modlibrary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/community"
	contentprofiles "github.com/nanolathe-gg/nanolathe/internal/content/profiles"
	"github.com/nanolathe-gg/nanolathe/internal/gameplay"
	"github.com/nanolathe-gg/nanolathe/internal/input"
	"github.com/nanolathe-gg/nanolathe/internal/settings"
)

// Config is the Nanolathe configuration a mod ships in its own
// nanolathe-mod.json, schema 2 (docs/DESIGN_MODS_MUTATORS.md §4.2). It is the
// single source of everything Nanolathe-specific about the mod: the engine
// carries no per-mod data. Its parts are kept apart because they are
// different kinds of fact:
//
//   - Content is load-time content data (directory table, limits, front-end
//     art). It is chosen before a session exists and never reaches a tick
//     (docs/DESIGN_CONTENT_VFS.md §5 "Content profiles").
//   - Rules are gameplay declarations: the minimum and recommended reserved
//     rule set and the complete Community 3.9 feature table the content was
//     authored for. Strict 3.1 ignores the table, as it ignores every
//     Community source (DESIGN_COMMUNITY_PATCH §3.2). A mod chooses a whole
//     reserved set; nothing here can switch one Modern policy off.
//   - Settings, Keys and Locks are host preferences the mod recommends or
//     pins. They are validated here and exposed for the shell to layer; this
//     package applies none of them.
type Config struct {
	Content contentprofiles.Profile
	Rules   Rules
	// Settings is a validated partial settings document in the settings
	// file's own JSON shape, nil when the config names none. ApplySettings
	// overlays it on a settings value and SettingsPaths lists what it sets.
	Settings json.RawMessage
	// Keys is the recommended keyboard profile and rebound actions in the
	// settings file's `keyBindings` shape, nil when the config names none.
	Keys *settings.KeyBindings
	// Locks are dotted paths into the settings document (for example
	// "gameplay" or "presentation.waterSurface") the mod asks the player not to
	// change; every one names a path the settings document has.
	Locks []string
}

// Rules are a config's gameplay declarations.
type Rules struct {
	// MinimumGameplay is the reserved set below which the mod may not run
	// without the player overriding its lock (§4.3), "" for none.
	MinimumGameplay gameplay.Mode
	// Gameplay is the reserved set the mod recommends, "" for none. It is
	// never below MinimumGameplay.
	Gameplay gameplay.Mode
	// CommunityFeatures is the complete Community 3.9 feature table the
	// content was authored for, decoded over the mainline table
	// (community.ParseFeatures); nil keeps the mainline table.
	CommunityFeatures *community.Features
}

// CommunitySources is the config's Community declaration as the session's
// content source (DESIGN_COMMUNITY_PATCH §3.2 source 2): one source whose
// Base replaces the mainline table, or none. A nil config declares none.
func (c *Config) CommunitySources() []community.Overrides {
	if c == nil || c.Rules.CommunityFeatures == nil {
		return nil
	}
	features := *c.Rules.CommunityFeatures
	return []community.Overrides{{Base: &features}}
}

// ControlsPreset is the controls preset the shell offers for the config:
// the keyboard profile's name when it is one of the presets the shell's
// preset rows define (community or zero), else "". The config's Settings and
// Keys are exactly those rows' values for its preset
// (cmd/nanolathe/controls_preset.go); naming the preset lets the existing
// offer run until the shell layers Settings itself.
func (c *Config) ControlsPreset() string {
	if c == nil || c.Keys == nil {
		return ""
	}
	switch profile := c.Keys.Normalized().Profile; profile {
	case contentprofiles.ControlsCommunity, contentprofiles.ControlsZero:
		return profile
	}
	return ""
}

// ApplySettings overlays the config's settings document on dst: a key the
// document names replaces dst's value, and every other value is kept. A
// config without a document leaves dst unchanged. The document was
// validated when the config was read, so an error here means dst's type
// changed under it.
func (c *Config) ApplySettings(dst *settings.Settings) error {
	if c == nil || len(c.Settings) == 0 || dst == nil {
		return nil
	}
	return decodeSettings(c.Settings, dst)
}

// SettingsPaths lists the dotted leaf paths the config's settings document
// sets ("presentation.overview", "audio.soundMode", "switchAlt"), sorted. An
// object is walked; an array or a scalar is one leaf. Nil when the config
// names no document.
func (c *Config) SettingsPaths() []string {
	if c == nil || len(c.Settings) == 0 {
		return nil
	}
	var document map[string]any
	if err := json.Unmarshal(c.Settings, &document); err != nil {
		return nil
	}
	var paths []string
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		object, ok := value.(map[string]any)
		if !ok {
			paths = append(paths, prefix)
			return
		}
		for key, child := range object {
			walk(prefix+"."+key, child)
		}
	}
	for key, value := range document {
		walk(key, value)
	}
	sort.Strings(paths)
	return paths
}

// configDocument is the schema 2 file as written. Sections stay raw until
// their own reader decodes them, each closed against unknown keys.
type configDocument struct {
	Schema   int             `json:"schema"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Summary  string          `json:"summary,omitempty"`
	Homepage string          `json:"homepage,omitempty"`
	Requires []string        `json:"requires,omitempty"`
	Content  json.RawMessage `json:"content,omitempty"`
	Rules    *rulesDocument  `json:"rules,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
	Keys     json.RawMessage `json:"keys,omitempty"`
	Locks    []string        `json:"locks,omitempty"`
}

type rulesDocument struct {
	MinimumGameplay   string          `json:"minimumGameplay,omitempty"`
	Gameplay          string          `json:"gameplay,omitempty"`
	CommunityFeatures json.RawMessage `json:"communityFeatures,omitempty"`
}

// parseConfigDocument reads a schema 2 nanolathe-mod.json into metadata that
// carries its Config. The flat recommendation fields other code reads —
// MinimumGameplay, Controls and BuildMenuPageSize — are filled from the
// config, so the running shell sees one value wherever it looks.
func parseConfigDocument(data []byte) (Metadata, error) {
	fail := func(what, logical, expected string) error {
		return diagnostic(what, logical, nil, expected)
	}
	var doc configDocument
	if err := decodeClosedJSON(data, &doc); err != nil {
		return Metadata{}, fail("reading mod config failed: "+err.Error(), MetadataFile, "a schema 2 mod config document")
	}
	config := &Config{Content: contentprofiles.Profile{Name: doc.ID}}
	if present(doc.Content) {
		content, err := contentprofiles.Parse(doc.Content, doc.ID, MetadataFile+" content")
		if err != nil {
			return Metadata{}, err
		}
		config.Content = content
	}
	if doc.Rules != nil {
		rules, err := parseRules(*doc.Rules)
		if err != nil {
			return Metadata{}, err
		}
		config.Rules = rules
	}
	if present(doc.Settings) {
		if err := validateSettingsDocument(doc.Settings); err != nil {
			return Metadata{}, err
		}
		config.Settings = append(json.RawMessage(nil), doc.Settings...)
	}
	if present(doc.Keys) {
		keys, err := parseKeys(doc.Keys)
		if err != nil {
			return Metadata{}, err
		}
		config.Keys = &keys
	}
	for _, lock := range doc.Locks {
		if slices.Contains(config.Locks, lock) {
			return Metadata{}, fail(fmt.Sprintf("mod config locks %q twice", lock), MetadataFile+" locks", "each settings path once")
		}
		if !settingsPathExists(lock) {
			return Metadata{}, fail(fmt.Sprintf("mod config lock %q names no setting", lock), MetadataFile+" locks", "dotted paths into the settings document, such as gameplay or presentation.waterSurface")
		}
		// A lock would hold the player's unit restrictions at the base set
		// for this mod, which is a recommendation by another route; the set
		// is the player's own choice (docs/DESIGN_MODS_MUTATORS.md §15.9).
		if lock == "restrictions" || strings.HasPrefix(lock, "restrictions.") {
			return Metadata{}, fail(fmt.Sprintf("mod config lock %q names the unit restrictions", lock), MetadataFile+" locks", "a setting a mod may recommend; unit restrictions are the player's own choice")
		}
		config.Locks = append(config.Locks, lock)
	}
	meta := Metadata{
		Schema: doc.Schema, ID: doc.ID, Name: doc.Name, Version: doc.Version,
		Summary: doc.Summary, Homepage: doc.Homepage, Requires: doc.Requires,
		MinimumGameplay:   string(config.Rules.MinimumGameplay),
		Controls:          config.ControlsPreset(),
		BuildMenuPageSize: config.Content.Presentation.BuildMenuPageSize,
		Config:            config,
	}
	if err := meta.Validate(); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

// present reports a section the document actually carries; an explicit
// null is the same as leaving it out.
func present(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null"))
}

// parseRules reads the rules section. Both gameplay words are reserved sets
// only: a mod chooses Strict 3.1, Community 3.9 or Modern as a whole.
func parseRules(doc rulesDocument) (Rules, error) {
	logical := MetadataFile + " rules"
	var rules Rules
	for _, field := range []struct {
		name string
		text string
		dst  *gameplay.Mode
	}{
		{"minimumGameplay", doc.MinimumGameplay, &rules.MinimumGameplay},
		{"gameplay", doc.Gameplay, &rules.Gameplay},
	} {
		switch mode := gameplay.Mode(field.text); mode {
		case "", gameplay.Strict31, gameplay.Community39, gameplay.Modern:
			*field.dst = mode
		default:
			return Rules{}, diagnostic(fmt.Sprintf("mod config rules %s %q is not a reserved gameplay word", field.name, field.text), logical, nil,
				fmt.Sprintf("one of %s, %s, %s or omitted", gameplay.Strict31, gameplay.Community39, gameplay.Modern))
		}
	}
	if rules.Gameplay != "" && rules.MinimumGameplay != "" && gameplayRank(rules.Gameplay) < gameplayRank(rules.MinimumGameplay) {
		return Rules{}, diagnostic(fmt.Sprintf("mod config recommends %s below its minimum %s", rules.Gameplay, rules.MinimumGameplay), logical, nil, "a recommended gameplay at or above minimumGameplay")
	}
	if present(doc.CommunityFeatures) {
		features, err := community.ParseFeatures(doc.CommunityFeatures, logical+".communityFeatures")
		if err != nil {
			return Rules{}, err
		}
		rules.CommunityFeatures = &features
	}
	return rules, nil
}

// gameplayRank orders the reserved sets in their derivation order
// (DESIGN_COMMUNITY_PATCH §2).
func gameplayRank(mode gameplay.Mode) int {
	switch mode {
	case gameplay.Strict31:
		return 0
	case gameplay.Community39:
		return 1
	}
	return 2
}

// reservedSettingsKeys are the settings keys a config's settings document may
// not carry, with where the value belongs instead. They are the match
// selection (§3), which the player and the rules section choose, the
// keyboard block, which is the keys section, and the file's own bookkeeping.
// Unit restrictions are part of the match selection: a config cannot
// recommend them, although the player keeps them per mod (§15.9, proposal
// R-P5).
var reservedSettingsKeys = []struct{ key, belongs string }{
	{"gameplay", "rules.gameplay"},
	{"gameplayFeatures", "rules.communityFeatures"},
	{"unitLimit", "rules.communityFeatures.unitLimit"},
	{"keyBindings", "the keys section"},
	{"mod", "the player's own choice"},
	{"mutators", "the player's own choice"},
	{"restrictions", "the player's own choice"},
	{"contentProfile", "the player's own choice"},
	{"modernAI", "the player's own choice"},
	{"controlsOffered", "the settings file's own bookkeeping"},
	{"modLockOverrides", "the settings file's own bookkeeping"},
	{"version", "the settings file's own bookkeeping"},
}

// validateSettingsDocument checks a config's settings document: an object in
// the settings file's shape, free of reserved keys, whose every key is a
// setting. It decodes over the defaults exactly as the settings file is read.
func validateSettingsDocument(raw json.RawMessage) error {
	logical := MetadataFile + " settings"
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return diagnostic("mod config settings is not an object", logical, nil, "a partial settings document in the settings file's JSON shape")
	}
	for _, reserved := range reservedSettingsKeys {
		if _, ok := keys[reserved.key]; ok {
			return diagnostic(fmt.Sprintf("mod config settings names %q", reserved.key), logical, nil, reserved.key+" in "+reserved.belongs)
		}
	}
	// Every key must be a setting. Checked by walking the settings types, not
	// by the decoder alone, because a block with its own reader (the
	// presentation block reads retired keys) does not refuse unknown ones.
	if unknown := unknownSettingsKey(raw, reflect.TypeOf(settings.Settings{}), ""); unknown != "" {
		return diagnostic("mod config settings does not read as settings: unknown key "+strconv.Quote(unknown), logical, nil, "known settings keys with values of the settings file's types")
	}
	defaults := settings.Defaults()
	if err := decodeSettings(raw, &defaults); err != nil {
		return diagnostic("mod config settings does not read as settings: "+err.Error(), logical, nil, "known settings keys with values of the settings file's types")
	}
	return nil
}

// decodeSettings decodes a settings document over dst, refusing unknown keys.
func decodeSettings(raw json.RawMessage, dst *settings.Settings) error {
	return decodeClosedJSON(raw, dst)
}

// parseKeys reads the keys section: the settings file's `keyBindings` block,
// with a known keyboard profile, catalogued rebindable actions and chords the
// battle's token stream can deliver.
func parseKeys(raw json.RawMessage) (settings.KeyBindings, error) {
	logical := MetadataFile + " keys"
	var keys settings.KeyBindings
	if err := decodeClosedJSON(raw, &keys); err != nil {
		return settings.KeyBindings{}, diagnostic("reading mod config keys failed: "+err.Error(), logical, nil, "a keys section of profile and bindings")
	}
	if profile := strings.ToLower(strings.TrimSpace(keys.Profile)); profile != "" && !slices.Contains(input.Profiles(), profile) {
		return settings.KeyBindings{}, diagnostic(fmt.Sprintf("mod config keyboard profile %q is unknown", keys.Profile), logical, nil, "one of "+strings.Join(input.Profiles(), ", ")+" or omitted")
	}
	actions := make([]string, 0, len(keys.Bindings))
	for action := range keys.Bindings {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	for _, id := range actions {
		action, ok := input.LookupAction(id)
		if !ok || action.Fixed {
			return settings.KeyBindings{}, diagnostic(fmt.Sprintf("mod config binds %q, which is not a rebindable action", id), logical, nil, "rebindable action identifiers from the keyboard catalogue")
		}
		for _, chord := range keys.Bindings[id] {
			if _, err := input.ParseChord(chord); err != nil {
				return settings.KeyBindings{}, diagnostic(fmt.Sprintf("mod config binds %q to %q, which is not a chord: %v", id, chord, err), logical, nil, "chords such as ctrl+a, f5 or q")
			}
		}
	}
	return keys, nil
}

// settingsPathExists reports whether a dotted path names a field of the
// settings document, following the JSON names of settings.Settings.
func settingsPathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	current := reflect.TypeOf(settings.Settings{})
	for _, segment := range strings.Split(path, ".") {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return false
		}
		field, ok := jsonField(current, segment)
		if !ok {
			return false
		}
		current = field.Type
	}
	return true
}

// unknownSettingsKey is the first key of an object document, at any depth,
// that names no field of t; "" when every key does.
func unknownSettingsKey(raw json.RawMessage, t reflect.Type, prefix string) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	names := make([]string, 0, len(doc))
	for k := range doc {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		field, ok := jsonField(t, k)
		if !ok {
			return prefix + k
		}
		if bad := unknownSettingsKey(doc[k], field.Type, prefix+k+"."); bad != "" {
			return bad
		}
	}
	return ""
}

// jsonField finds the struct field whose JSON name is exactly name.
func jsonField(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		if tag == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

// decodeClosedJSON decodes exactly one JSON value into dst, refusing unknown
// object keys at every level the decoder walks.
func decodeClosedJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing data after the document")
	}
	return nil
}

// ReadConfigFile reads a stand-alone nanolathe-mod.json, the file
// `--mod-config` names for a manual root stack or a displayless run
// (§4.3). It must be a schema 2 config; a schema 1 metadata file carries no
// Nanolathe configuration and is refused.
func ReadConfigFile(path string) (Metadata, error) {
	data, err := readLimited(path, maxMetadataBytes)
	if err != nil {
		return Metadata{}, diagnostic("mod config file is not readable", path, nil, fmt.Sprintf("a nanolathe-mod.json of at most %d bytes", maxMetadataBytes))
	}
	meta, err := ParseMetadata(data)
	if err != nil {
		return Metadata{}, withLogical(err, path)
	}
	if meta.Config == nil {
		return Metadata{}, diagnostic(fmt.Sprintf("mod metadata schema %d carries no Nanolathe config", meta.Schema), path, nil, "a schema 2 mod config")
	}
	return meta, nil
}

// NoConfigNotice is the one-line notice for content mounted without a
// Nanolathe config: a mod whose metadata is schema 1 or was generated for a
// package that had none. It mounts as plain content (§4.5).
func NoConfigNotice(name string) string {
	return name + " has no Nanolathe config file; it may not load correctly. Re-download it from Get more mods."
}

// removedProfileNames are the built-in content profiles earlier builds
// carried. A saved `contentProfile` preference naming one is ignored with
// the plain-content notice; the mods now carry their own configs.
var removedProfileNames = []string{"retail", "escalation", "prota", "zero", "mayhem"}

// IsRemovedProfile reports whether a saved content-profile preference names
// one of the removed built-in profiles rather than a config file.
func IsRemovedProfile(name string) bool {
	return slices.Contains(removedProfileNames, strings.ToLower(strings.TrimSpace(name)))
}

// withLogical renames the logical path of a diagnostic raised against the
// canonical metadata file name, for a config read from another path.
func withLogical(err error, logical string) error {
	var diag *diagError
	if errors.As(err, &diag) && strings.HasPrefix(diag.logical, MetadataFile) {
		copied := *diag
		copied.logical = logical + strings.TrimPrefix(diag.logical, MetadataFile)
		return &copied
	}
	return err
}
