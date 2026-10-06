package content

import (
	"errors"
	"reflect"
	"testing"
)

// restrictionFixture is an authored catalog shaped for the unit-restriction
// contracts (docs/DESIGN_MODS_MUTATORS.md §15.2–§15.4): a side commander
// that is not norestrict (the stock ones are), a second commander named with
// padding and capitals in its side record, a name carried by two records
// whose second authors norestrict, a name carried by two ordinary records,
// a wacky definition and an authored Survival roster. Records are in the
// retained name order with their indices, masks and hashes compiled, and
// Hash is stamped as Compile stamps it.
func restrictionFixture(t *testing.T) *Catalog {
	t.Helper()
	mk := func(name, category string, f func(*UnitDef)) *UnitDef {
		u := &UnitDef{UnitName: name, Category: category, Limit: -1, LimitEnabled: true}
		u.CanonicalKey = CanonicalKey(name)
		if f != nil {
			f(u)
		}
		return u
	}
	records := []*UnitDef{
		mk("ARMCOM", "COMMANDER ARM", func(u *UnitDef) { u.Commander = true }),
		mk("ARMDUP", "DUPFIRST", nil),
		mk("armdup", "DUPSECOND", nil),
		mk("ARMPW", "KBOT ARM", nil),
		mk("ARMSHIELD", "SHIELD", nil),
		mk("armshield", "SHIELD", func(u *UnitDef) { u.NoRestrict = true }),
		mk("ARMWACKY", "KBOT", func(u *UnitDef) { u.Wacky = true }),
		mk("CORCOM", "COMMANDER CORE", func(u *UnitDef) { u.Commander = true }),
	}
	sortUnitRecords(records)
	c := &Catalog{
		unitRecords: records,
		Units:       firstUnitNames(records),
		Sides:       []*SideDef{{Index: 0, Commander: "armcom"}, {Index: 1, Commander: " CORCOM "}},
		SurvivalRoster: &SurvivalRoster{Units: []SurvivalRosterEntry{
			{Unit: "armpw", Tier: 1}, {Unit: "armwacky", Tier: 2},
		}},
	}
	var err error
	if c.Categories, err = compileCategoryRecords(records, c.Units, RetailLimits()); err != nil {
		t.Fatal(err)
	}
	c.Hash = catalogHash(c)
	return c
}

func mustRestrictions(t *testing.T, values map[string]int) Restrictions {
	t.Helper()
	r, err := ParseRestrictions(values)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Parsing checks spelling only (§15.2): canonical keys, counts 0..100, the
// flag's plain decimal, and one canonical String/Map/Digest whatever order
// the set was built in.
func TestRestrictionsSpelling(t *testing.T) {
	r := mustRestrictions(t, map[string]int{"armpw": 20, "armkrog": 0})
	if got := r.String(); got != "armkrog=0,armpw=20" {
		t.Fatalf("String = %q", got)
	}
	back, err := ParseRestrictions(r.Map())
	if err != nil || !back.Equal(r) || back.String() != r.String() {
		t.Fatalf("Map round trip = %q, %v", back.String(), err)
	}
	if c, ok := r.Count("ARMPW"); !ok || c != 20 {
		t.Fatalf("Count(ARMPW) = %d, %v", c, ok)
	}
	if _, ok := r.Count("armflash"); ok {
		t.Fatal("a unit without an entry has No limit")
	}
	for _, values := range []map[string]int{
		{"ArmPW": 1}, {" armpw": 1}, {"": 1}, {"arm,pw": 1}, {"arm=pw": 1}, {"armpw": -1}, {"armpw": 101},
	} {
		if _, err := ParseRestrictions(values); err == nil {
			t.Errorf("ParseRestrictions(%v) accepted", values)
		}
	}

	for text, want := range map[string]Restriction{
		"armpw=20": {"armpw", 20}, "armkrog=0": {"armkrog", 0}, "armpw=100": {"armpw", 100},
	} {
		if got, err := ParseRestriction(text); err != nil || got != want {
			t.Errorf("ParseRestriction(%q) = %+v, %v", text, got, err)
		}
	}
	for _, text := range []string{
		"armpw", "armpw=", "armpw=+5", "armpw=-1", "armpw=05", "armpw=00", "armpw=1.5", "armpw=101",
		"armpw=99999999999999999999", "ARMPW=5", " armpw=5", "=5", "armpw= 5",
	} {
		if _, err := ParseRestriction(text); err == nil {
			t.Errorf("ParseRestriction(%q) accepted", text)
		}
	}

	var built Restrictions
	if !built.IsZero() || built.String() != "" || len(built.Map()) != 0 || built.Entries() != nil {
		t.Fatal("the zero value restricts nothing")
	}
	for _, e := range []Restriction{{"armpw", 3}, {"ARMKROG", 0}, {"armpw", 20}} {
		if err := built.Set(e.Unit, e.Count); err != nil {
			t.Fatal(err)
		}
	}
	if !built.Equal(r) || built.Digest() != r.Digest() {
		t.Fatalf("a set built in another order is %q, want %q with the same digest", built.String(), r.String())
	}
	if err := built.Set("armpw", 101); err == nil {
		t.Fatal("Set accepted a count above 100")
	}
	copied := built
	copied.Clear("armkrog")
	if err := copied.Set("armflash", 1); err != nil {
		t.Fatal(err)
	}
	if built.String() != "armkrog=0,armpw=20" || copied.String() != "armflash=1,armpw=20" {
		t.Fatalf("a copy shared an edit: %q and %q", built.String(), copied.String())
	}
	copied.Clear("armflash")
	copied.Clear("armpw")
	if !copied.IsZero() || !copied.Equal(Restrictions{}) || copied.Digest() != (Restrictions{}).Digest() {
		t.Fatal("clearing every entry must give the zero set")
	}
}

// An empty set leaves a clone deep-equal to an untouched clone, Hash
// included, on content with a wacky definition: nothing is seeded (§15.1).
func TestEmptyRestrictionsLeaveTheCloneIdentical(t *testing.T) {
	base := restrictionFixture(t)
	want, got := base.Clone(), base.Clone()
	if err := got.ApplyRestrictions(Restrictions{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || got.Hash != base.Hash {
		t.Fatal("an empty restriction set changed the catalog")
	}
	if u, _ := got.Unit("armwacky"); u.Limit != -1 || u.UnitDefID == 0 {
		t.Fatalf("a wacky definition was restricted without an entry: %+v", u)
	}
}

// Removal (§15.4 step 1): every record of a removed name leaves the table,
// survivors keep their order and are renumbered with masks and hashes
// rebuilt, the roster loses the entry, and the source is untouched.
func TestApplyRestrictionsRemovesEveryRecordOfTheName(t *testing.T) {
	base := restrictionFixture(t)
	baseHash := base.Hash
	baseRecords := base.UnitRecords()
	baseIDs := make([]uint32, len(baseRecords))
	baseHashes := make([]string, len(baseRecords))
	for i, u := range baseRecords {
		baseIDs[i], baseHashes[i] = u.UnitDefID, u.Hash
	}
	r := mustRestrictions(t, map[string]int{"armdup": 0, "armpw": 0})
	c := base.Clone()
	if err := c.ApplyRestrictions(r); err != nil {
		t.Fatal(err)
	}

	var wantNames []string
	for _, u := range baseRecords {
		if key := CanonicalKey(u.UnitName); key != "armdup" && key != "armpw" {
			wantNames = append(wantNames, u.UnitName)
		}
	}
	got := c.UnitRecords()
	if len(got) != len(wantNames) {
		t.Fatalf("%d records survive, want %d", len(got), len(wantNames))
	}
	all, _ := c.Category("ALL")
	for i, u := range got {
		id := uint32(i + 1)
		if u.UnitName != wantNames[i] {
			t.Fatalf("survivor %d is %s, want %s: survivors keep their compiled order", i, u.UnitName, wantNames[i])
		}
		if u.UnitDefID != id || !u.UnitMask.Contains(id) || !all.Contains(id) {
			t.Fatalf("survivor %s: index %d, own mask or ALL mask not rebuilt for %d", u.UnitName, u.UnitDefID, id)
		}
		if u.Hash != HashDefinition(writeUnitCanonical(u)) {
			t.Fatalf("survivor %s per-definition hash not rebuilt", u.UnitName)
		}
		if def, ok := c.UnitDefByIndex(id); !ok || def != u {
			t.Fatalf("index %d does not resolve to its survivor", id)
		}
	}
	if all.Contains(uint32(len(got) + 1)) {
		t.Fatal("ALL keeps a removed index")
	}
	for _, name := range []string{"armdup", "ARMDUP", "armpw"} {
		if _, ok := c.Unit(name); ok {
			t.Fatalf("%s is still in the name index", name)
		}
	}
	if _, ok := c.Category("DUPSECOND"); ok {
		t.Fatal("a category only removed records carried survives")
	}
	if shield, _ := c.Unit("armshield"); shield.UnitDefID == baseIDs[4] || shield.Hash == baseHashes[4] {
		t.Fatal("a renumbered survivor kept its old index or hash")
	}
	if roster := c.SurvivalRoster.Units; len(roster) != 1 || roster[0].Unit != "armwacky" {
		t.Fatalf("roster = %+v, want only armwacky", roster)
	}
	if want := HashDefinition([]byte("catalog+restrictions\n" + baseHash + "\n" + r.Digest() + "\n")); c.Hash != want {
		t.Fatalf("Hash = %s, want catalog+restrictions over the base", c.Hash)
	}

	if base.Hash != baseHash || len(base.UnitRecords()) != len(baseRecords) || len(base.SurvivalRoster.Units) != 2 {
		t.Fatal("the source catalog was written")
	}
	for i, u := range base.UnitRecords() {
		if u != baseRecords[i] || u.UnitDefID != baseIDs[i] || u.Hash != baseHashes[i] {
			t.Fatalf("source record %d was written", i)
		}
	}
	if _, ok := base.Unit("armpw"); !ok {
		t.Fatal("the source lost a definition")
	}
}

// Caps (§15.4 step 2) set the limit on every record of the name and move no
// per-definition identity; Catalog.Hash follows the set.
func TestApplyRestrictionsCapsEveryRecordOfTheName(t *testing.T) {
	base := restrictionFixture(t)
	r := mustRestrictions(t, map[string]int{"armdup": 3, "armcom": 1, "armwacky": 100})
	c := base.Clone()
	if err := c.ApplyRestrictions(r); err != nil {
		t.Fatal(err)
	}
	src := base.UnitRecords()
	for i, u := range c.UnitRecords() {
		want, capped := r.Count(u.UnitName)
		switch {
		case u.UnitDefID != src[i].UnitDefID || u.Hash != src[i].Hash || !reflect.DeepEqual(u.UnitMask, src[i].UnitMask):
			t.Fatalf("%s: a cap moved definition identity", u.UnitName)
		case capped && (u.Limit != int32(want) || !u.LimitEnabled):
			t.Fatalf("%s: limit %d enabled %v, want %d", u.UnitName, u.Limit, u.LimitEnabled, want)
		case !capped && u.Limit != -1:
			t.Fatalf("%s: an unrestricted definition was capped at %d", u.UnitName, u.Limit)
		}
		if src[i].Limit != -1 {
			t.Fatalf("%s: the source was capped", u.UnitName)
		}
	}
	if want := HashDefinition([]byte("catalog+restrictions\n" + base.Hash + "\n" + r.Digest() + "\n")); c.Hash != want {
		t.Fatal("Hash does not follow catalog+restrictions over the base and the set")
	}
	other := base.Clone()
	if err := other.ApplyRestrictions(mustRestrictions(t, map[string]int{"armdup": 4, "armcom": 1, "armwacky": 100})); err != nil {
		t.Fatal(err)
	}
	if other.Hash == c.Hash {
		t.Fatal("two different sets gave one identity")
	}
}

// CheckRestrictions refuses an unknown name, a norestrict name (any record
// of it) and a commander's removal, and accepts a commander's cap;
// ApplyRestrictions with any issue writes nothing (§15.3).
func TestCheckRestrictionsRefusals(t *testing.T) {
	base := restrictionFixture(t)
	r := mustRestrictions(t, map[string]int{
		"armcom": 0, "armfoo": 2, "armpw": 0, "armshield": 5, "corcom": 1, "armwacky": 0,
	})
	accepted, issues := base.CheckRestrictions(r)
	if accepted.String() != "armpw=0,armwacky=0,corcom=1" {
		t.Fatalf("accepted %q", accepted.String())
	}
	want := []RestrictionIssue{
		{"armcom", RestrictionRemovesCommander},
		{"armfoo", RestrictionUnknownUnit},
		{"armshield", RestrictionNoRestrict},
	}
	if !reflect.DeepEqual(issues, want) {
		t.Fatalf("issues = %+v, want %+v", issues, want)
	}
	// A commander named with padding and capitals in its side record is
	// still that side's commander.
	if _, issues := base.CheckRestrictions(mustRestrictions(t, map[string]int{"corcom": 0})); len(issues) != 1 || issues[0].Reason != RestrictionRemovesCommander {
		t.Fatalf("CORCOM removal issues = %+v", issues)
	}

	c, untouched := base.Clone(), base.Clone()
	err := c.ApplyRestrictions(r)
	var refusal *RestrictionsError
	if !errors.As(err, &refusal) || !reflect.DeepEqual(refusal.Issues, want) || !refusal.Restrictions.Equal(r) {
		t.Fatalf("ApplyRestrictions = %v, want the three issues", err)
	}
	if !reflect.DeepEqual(c, untouched) {
		t.Fatal("ApplyRestrictions wrote a catalog it refused")
	}
}
