package pathlab

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/community"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/content/profiles"
	"github.com/nanolathe-gg/nanolathe/internal/install"
	"github.com/nanolathe-gg/nanolathe/internal/modlibrary"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// Content is a mounted and compiled content set.
type Content struct {
	FS      *vfs.FS
	View    vfs.FSOps
	Profile profiles.Profile
	// Features is the mod config's Community declaration, the session's
	// content source; nil for the base install or a mod without a config.
	Features []community.Overrides
	Catalog  *content.Catalog
	Roots    []string
	Mod      string // "<id>@<version>", empty for the base install
}

// Close releases the mounted overlay.
func (c *Content) Close() {
	if c != nil && c.FS != nil {
		c.FS.Close()
	}
}

// LoadContent mounts the base install found from roots (or discovered when
// roots is empty), then the installed mod named by mod ("" or "none" for
// none) as the last root, applies the mod's own config — its content section
// and Community table, or the base game's profile for a mod without one —
// and compiles the catalog. The settings file is never read, so a run reproduces from its
// arguments.
func LoadContent(roots []string, mod string) (*Content, error) {
	base, err := install.Resolve(roots)
	if err != nil {
		return nil, err
	}
	c := &Content{Roots: append([]string(nil), base...), Profile: profiles.Retail()}
	if strings.TrimSpace(mod) == "" {
		mod = "none"
	}
	if id, _, err := modlibrary.ParseSelector(mod); err != nil {
		return nil, err
	} else if id != "" {
		libRoot, err := modlibrary.DefaultRoot()
		if err != nil {
			return nil, err
		}
		lib, err := modlibrary.Open(libRoot)
		if err != nil {
			return nil, err
		}
		m, ok, err := lib.Select(mod, base)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("nanolathe: mod is not installed: logical path %s, providers searched [%s], expected an installed mod", mod, libRoot)
		}
		c.Roots = append(c.Roots, m.Dir)
		c.Mod = m.ID + "@" + m.Version
		c.Profile, c.Features = m.Content(), m.CommunitySources()
	}
	c.FS = vfs.New()
	if err := c.FS.MountGameDirectories(c.Roots); err != nil {
		c.FS.Close()
		return nil, fmt.Errorf("nanolathe: mounting content failed: logical path <content roots>, providers searched [%s], expected readable Total Annihilation content directories: %w", strings.Join(c.Roots, ", "), err)
	}
	c.View = c.Profile.Layout().Apply(c.FS)
	c.Catalog, err = content.CompileWithOptions(c.View, content.Options{Limits: content.LimitsFromProfile(c.Profile.Limits)})
	if err != nil {
		c.FS.Close()
		return nil, err
	}
	return c, nil
}

// UnitRow is one unit definition as the offline laboratory reads it. Speeds
// are world units per tick and rates are the authored fixed-point values
// converted to floating point for reading; nothing in the simulation reads
// this table.
type UnitRow struct {
	Name    string  `json:"unitname"`
	Air     bool    `json:"air"`
	Mobile  bool    `json:"mobile"`
	FootX   int32   `json:"foot_x"`
	FootZ   int32   `json:"foot_z"`
	VMax    float64 `json:"vmax"`
	Accel   float64 `json:"accel"`
	Brake   float64 `json:"brake"`
	Turn    int32   `json:"turn"`
	Class   string  `json:"class"`
	Kind    string  `json:"kind"`
	Builder bool    `json:"builder"`
	ClassX  int32   `json:"class_foot_x"`
	ClassZ  int32   `json:"class_foot_z"`
	// Roles keeps the one role tools/tad-extract reads to choose the air
	// grammar of a unit-sync entry [fmt tad §7].
	Roles []string `json:"roles"`
}

// UnitRows lists every unit definition of the catalog, by name.
func UnitRows(cat *content.Catalog) []UnitRow {
	var rows []UnitRow
	for _, d := range cat.Units {
		if d == nil || d.UnitName == "" {
			continue
		}
		r := UnitRow{
			Name: strings.ToUpper(d.UnitName), Air: d.CanFly, Mobile: d.BMCode != 0,
			FootX: d.FootprintX, FootZ: d.FootprintZ,
			VMax: float64(d.MaxVelocity) / 65536, Accel: float64(d.Acceleration) / 65536, Brake: float64(d.BrakeRate) / 65536,
			Turn: d.TurnRate, Class: strings.ToUpper(d.MovementClass), Builder: d.Builder, Roles: []string{},
		}
		if mc := cat.Movement[content.CanonicalKey(d.MovementClass)]; mc != nil && d.MovementClass != "" {
			r.ClassX, r.ClassZ = mc.FootprintX, mc.FootprintZ
		}
		r.Kind = unitKind(d, cat)
		if d.CanFly {
			r.Roles = append(r.Roles, "air")
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// unitKind is a coarse family for reporting, read from the definition's own
// capability flags and the depths of its movement class.
func unitKind(d *content.UnitDef, cat *content.Catalog) string {
	switch {
	case d.CanFly:
		return "air"
	case d.BMCode == 0:
		return "structure"
	case d.CanHover:
		return "hover"
	}
	minDepth, maxDepth := d.MinWaterDepth, d.MaxWaterDepth
	if mc := cat.Movement[content.CanonicalKey(d.MovementClass)]; mc != nil && d.MovementClass != "" {
		minDepth, maxDepth = mc.MinWaterDepth, mc.MaxWaterDepth
	}
	switch {
	case minDepth > 0 && !d.Floater:
		return "sub"
	case minDepth > 0 || d.Floater:
		return "ship"
	case maxDepth >= 255:
		return "amphibious"
	case d.Upright || strings.Contains(strings.ToUpper(d.MovementClass), "KBOT"):
		// Most stock kbots share the light vehicle class; upright is what
		// tells a walker from a tracked or wheeled chassis.
		return "kbot"
	}
	return "vehicle"
}

// MapNames lists the catalog's maps by display name.
func MapNames(cat *content.Catalog) []string {
	var out []string
	for _, m := range cat.Maps {
		if m != nil {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}
