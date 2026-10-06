package content

// Unit restrictions (docs/DESIGN_MODS_MUTATORS.md §15). A restriction is
// retail's multiplayer per-definition count, offered in skirmish and Survival
// in every gameplay mode: 0 removes the definition from the battle catalog,
// as retail's battle-entry compile removes a record whose creatable bit the
// lobby cleared, and 1..100 caps each player's records of it at the
// allocator's existing per-definition test [05 R-SHARE-01 §8]
// [05 R-SHARE-01 §9] [08 R-SKIR-01 §10]. Like a mutator it transforms the
// per-battle catalog clone once at battle entry; it is never consulted
// inside a tick except through the limit field the allocator already reads.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// RestrictionMaxCount is the largest per-player count: retail's slider
// stores 0..100 and shows No Limit above it [08 R-SKIR-01 §10].
const RestrictionMaxCount = 100

// Restriction is one entry. Count 0 removes every retained record carrying
// the name from the battle; 1..RestrictionMaxCount caps each player's
// records of it (docs/DESIGN_MODS_MUTATORS.md §15.1).
type Restriction struct {
	Unit  string // CanonicalKey of the unit name
	Count uint8
}

// Restrictions is one battle's set: at most one entry per key, kept in
// ascending key order. A unit without an entry has No limit. The zero value
// restricts nothing.
//
// The set is a value: every method that changes it builds a new entry slice,
// so a copy never shares an edit with the value it was copied from.
type Restrictions struct{ entries []Restriction }

// restrictionsIdentityTag versions the meaning of the transform. It is
// hashed into Digest and so into every restricted catalog identity, and it
// must change whenever ApplyRestrictions changes meaning (§15.4 step 3).
const restrictionsIdentityTag = "restrictions/1"

// restrictionKeyError names a key that is not a usable stored spelling.
func restrictionKeyError(key string) error {
	return fmt.Errorf("content: unit restriction key %q is not a unit name key: expected a non-empty canonical key (lower-case ASCII, no surrounding whitespace) without NUL, ',' or '='", key)
}

// validRestrictionKey reports whether key is a stored spelling: already its
// own CanonicalKey, as battle-configuration field 12 requires, and non-empty.
// NUL, ',' and '=' are refused as well, because String joins entries with
// ',' and '=' and the identity hashes String: a key carrying either would let
// two different sets spell one string.
func validRestrictionKey(key string) bool {
	return key != "" && CanonicalKey(key) == key && !strings.ContainsAny(key, "\x00,=")
}

// ParseRestrictions reads the settings and sidecar spelling, canonical unit
// key to count (§15.9, §15.5). Keys are walked in sorted order, so the first
// error reported is deterministic (I1). A key that is not already its own
// CanonicalKey, or a count outside 0..RestrictionMaxCount, is an error
// naming it. Whether a key names a unit is the catalog's question
// (CheckRestrictions).
func ParseRestrictions(values map[string]int) (Restrictions, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]Restriction, 0, len(keys))
	for _, key := range keys {
		if !validRestrictionKey(key) {
			return Restrictions{}, restrictionKeyError(key)
		}
		count := values[key]
		if count < 0 || count > RestrictionMaxCount {
			return Restrictions{}, fmt.Errorf("content: unit restriction %s count %d is outside 0..%d", key, count, RestrictionMaxCount)
		}
		entries = append(entries, Restriction{Unit: key, Count: uint8(count)})
	}
	if len(entries) == 0 {
		return Restrictions{}, nil
	}
	return Restrictions{entries: entries}, nil
}

// ParseRestriction reads the flag spelling, "armpw=20" (§15.5): a canonical
// unit key, '=', and a count of 0..RestrictionMaxCount written as plain
// decimal digits — no sign, fraction or leading zero. Refusing a key given
// twice is the caller's, since one entry cannot repeat itself.
func ParseRestriction(text string) (Restriction, error) {
	key, value, ok := strings.Cut(text, "=")
	if !ok {
		return Restriction{}, fmt.Errorf("content: unit restriction %q: expected unit=count", text)
	}
	if !validRestrictionKey(key) {
		return Restriction{}, restrictionKeyError(key)
	}
	if value == "" || (len(value) > 1 && value[0] == '0') || strings.TrimLeft(value, "0123456789") != "" {
		return Restriction{}, fmt.Errorf("content: unit restriction %q: count %q is not a plain decimal 0..%d", text, value, RestrictionMaxCount)
	}
	count, err := strconv.Atoi(value)
	if err != nil || count > RestrictionMaxCount {
		return Restriction{}, fmt.Errorf("content: unit restriction %q: count %q is outside 0..%d", text, value, RestrictionMaxCount)
	}
	return Restriction{Unit: key, Count: uint8(count)}, nil
}

// IsZero reports whether the set restricts nothing, so applying it would
// change nothing.
func (r Restrictions) IsZero() bool { return len(r.entries) == 0 }

// Entries returns a copy of the entries in key order.
func (r Restrictions) Entries() []Restriction {
	if len(r.entries) == 0 {
		return nil
	}
	return append([]Restriction(nil), r.entries...)
}

// find returns the index of key's entry, or where it would be inserted.
func (r Restrictions) find(key string) (int, bool) {
	i := sort.Search(len(r.entries), func(i int) bool { return r.entries[i].Unit >= key })
	return i, i < len(r.entries) && r.entries[i].Unit == key
}

// Count returns the entry for unit, looked up by its CanonicalKey, and
// whether it has one; a unit without an entry has No limit.
func (r Restrictions) Count(unit string) (count uint8, restricted bool) {
	i, ok := r.find(CanonicalKey(unit))
	if !ok {
		return 0, false
	}
	return r.entries[i].Count, true
}

// Set stores count for unit, keyed by its CanonicalKey. It refuses a count
// above RestrictionMaxCount and a name whose key is not a usable stored
// spelling (empty, or carrying NUL, ',' or '=').
func (r *Restrictions) Set(unit string, count uint8) error {
	key := CanonicalKey(unit)
	if !validRestrictionKey(key) {
		return restrictionKeyError(key)
	}
	if count > RestrictionMaxCount {
		return fmt.Errorf("content: unit restriction %s count %d is outside 0..%d", key, count, RestrictionMaxCount)
	}
	i, ok := r.find(key)
	entries := make([]Restriction, 0, len(r.entries)+1)
	entries = append(entries, r.entries[:i]...)
	entries = append(entries, Restriction{Unit: key, Count: count})
	if ok {
		i++
	}
	entries = append(entries, r.entries[i:]...)
	r.entries = entries
	return nil
}

// Clear returns unit, looked up by its CanonicalKey, to No limit.
func (r *Restrictions) Clear(unit string) {
	i, ok := r.find(CanonicalKey(unit))
	if !ok {
		return
	}
	if len(r.entries) == 1 {
		r.entries = nil
		return
	}
	entries := make([]Restriction, 0, len(r.entries)-1)
	entries = append(entries, r.entries[:i]...)
	r.entries = append(entries, r.entries[i+1:]...)
}

// Equal reports whether two sets hold the same entries.
func (r Restrictions) Equal(o Restrictions) bool {
	if len(r.entries) != len(o.entries) {
		return false
	}
	for i := range r.entries {
		if r.entries[i] != o.entries[i] {
			return false
		}
	}
	return true
}

// Map is the inverse of ParseRestrictions, the settings and sidecar
// spelling. The zero value maps to an empty map.
func (r Restrictions) Map() map[string]int {
	out := make(map[string]int, len(r.entries))
	for _, e := range r.entries {
		out[e.Unit] = int(e.Count)
	}
	return out
}

// String is the canonical form, "key=count" pairs in ascending key order
// joined by commas, e.g. "armkrog=0,armpw=20"; "" when zero. It is the value
// reports print and the catalog identity hashes.
func (r Restrictions) String() string {
	parts := make([]string, len(r.entries))
	for i, e := range r.entries {
		parts[i] = e.Unit + "=" + strconv.Itoa(int(e.Count))
	}
	return strings.Join(parts, ",")
}

// Digest is a stable identity of the set: a hash of the identity tag and the
// canonical String, so input order cannot move it.
func (r Restrictions) Digest() string {
	return HashDefinition([]byte(restrictionsIdentityTag + "\n" + r.String() + "\n"))
}

// RestrictionReason says why one entry cannot apply to a catalog (§15.3).
type RestrictionReason uint8

const (
	// RestrictionUnknownUnit: no retained record carries the name.
	RestrictionUnknownUnit RestrictionReason = iota + 1
	// RestrictionNoRestrict: a record carrying the name authors norestrict,
	// which retail's restriction screen never offers [05 R-SHARE-01 §9]
	// [08 R-SKIR-01 §10].
	RestrictionNoRestrict
	// RestrictionRemovesCommander: the count is 0 and a side record names the
	// unit as its commander. Skirmish entry places a commander for every
	// player and refuses a side whose commander the catalog lacks, so such a
	// set would stop every battle on that content. This is a Nanolathe check
	// on the selection, not a claim about retail's start spawn; a positive
	// count is accepted.
	RestrictionRemovesCommander
)

// String names the reason in the words the diagnostics use.
func (r RestrictionReason) String() string {
	switch r {
	case RestrictionUnknownUnit:
		return "no unit of the running content has this name"
	case RestrictionNoRestrict:
		return "the unit is marked norestrict"
	case RestrictionRemovesCommander:
		return "the unit is a side's commander, which can be capped but not removed"
	}
	return fmt.Sprintf("restriction reason %d", uint8(r))
}

// RestrictionIssue says why one entry cannot apply to a catalog (§15.3).
type RestrictionIssue struct {
	Unit   string
	Reason RestrictionReason // RestrictionUnknownUnit, RestrictionNoRestrict, RestrictionRemovesCommander
}

// RestrictionsError is ApplyRestrictions' refusal of a set CheckRestrictions
// does not wholly accept: the set and every issue, in key order. A host that
// names the source of the set (a flag, a save) reads it to say which entry
// the content cannot take.
type RestrictionsError struct {
	Restrictions Restrictions
	Issues       []RestrictionIssue
}

func (e *RestrictionsError) Error() string {
	parts := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		count, _ := e.Restrictions.Count(issue.Unit)
		parts[i] = fmt.Sprintf("%s=%d (%s)", issue.Unit, count, issue.Reason)
	}
	return "content: unit restrictions the catalog cannot take: " + strings.Join(parts, ", ")
}

// restrictionRecords returns every retained record whose unit name has key,
// in catalog-index order. A restriction names the unit as the player sees
// it, so it covers every record of the name and not only the one the name
// lookup returns, which hides later records of an equal name
// [02 R-CAT-01 §5] (§15.3, proposal R-P1).
func (c *Catalog) restrictionRecords(key string) []*UnitDef {
	var out []*UnitDef
	for _, u := range c.unitRecordView() {
		if u != nil && CanonicalKey(u.UnitName) == key {
			out = append(out, u)
		}
	}
	return out
}

// CheckRestrictions splits r into the entries this catalog accepts and an
// issue for every other entry, both in key order. It reads only immutable
// definition data and writes nothing.
//
// A name counts as norestrict when any record carrying it authors
// norestrict. The reasons are tested in the order of §15.3's table, so a
// commander that is also norestrict — as both stock commanders are — reports
// norestrict.
func (c *Catalog) CheckRestrictions(r Restrictions) (accepted Restrictions, issues []RestrictionIssue) {
	var entries []Restriction
	for _, e := range r.entries {
		reason := c.restrictionReason(e)
		if reason != 0 {
			issues = append(issues, RestrictionIssue{Unit: e.Unit, Reason: reason})
			continue
		}
		entries = append(entries, e)
	}
	return Restrictions{entries: entries}, issues
}

func (c *Catalog) restrictionReason(e Restriction) RestrictionReason {
	if c == nil {
		return RestrictionUnknownUnit
	}
	records := c.restrictionRecords(e.Unit)
	if len(records) == 0 {
		return RestrictionUnknownUnit
	}
	for _, u := range records {
		if u.NoRestrict {
			return RestrictionNoRestrict
		}
	}
	if e.Count == 0 {
		for _, side := range c.Sides {
			if side != nil && CanonicalKey(side.Commander) == e.Unit {
				return RestrictionRemovesCommander
			}
		}
	}
	return 0
}

// ApplyRestrictions transforms a per-battle clone in place (§15.4). Like
// RestrictToCreatable and ApplyMutators, it is never called on a shared
// compiled catalog. It writes nothing and returns a *RestrictionsError when
// CheckRestrictions would report any issue, so an unchecked set can never
// half-apply. With a zero set nothing changes, Hash included.
//
// Otherwise, in three steps:
//
//  1. Removal. Every retained record carrying a name whose count is 0 leaves
//     the table, as retail's battle-entry compile compacts out a record whose
//     creatable bit the lobby's apply cleared [05 R-SHARE-01 §8]
//     [05 R-SHARE-01 §9]. The survivors keep their relative order and are
//     renumbered from 1; the first-name index, the category registry with
//     every definition's masks, and each survivor's per-definition Hash are
//     rebuilt as RestrictToCreatable rebuilds them. An authored Survival
//     roster drops its entries for removed units.
//  2. Caps. Every retained record carrying a name whose count is 1..100 gets
//     that count as its per-definition limit, which the allocator's third
//     test reads [05 R-SHARE-01 §8]. The limit is excluded from definition
//     identity, so no per-definition Hash moves.
//  3. Identity. Hash becomes a hash of the base Hash and Digest, as the
//     mutators restamp it; the mutators applied afterwards hash over this
//     value.
//
// Build menus, weapons, features, movement classes, sides and AI profiles are
// left alone: every consumer resolves a product's name through the catalog,
// so a removed product resolves to nothing (§15.4 "What it leaves alone").
func (c *Catalog) ApplyRestrictions(r Restrictions) error {
	if c == nil || r.IsZero() {
		return nil
	}
	if _, issues := c.CheckRestrictions(r); len(issues) > 0 {
		return &RestrictionsError{Restrictions: r, Issues: issues}
	}
	removed := make(map[string]bool)
	for _, e := range r.entries {
		if e.Count == 0 {
			removed[e.Unit] = true
		}
	}
	base := c.Hash
	if len(removed) > 0 {
		records := c.unitRecordView()
		survivors := make([]*UnitDef, 0, len(records))
		for _, u := range records {
			if u != nil && removed[CanonicalKey(u.UnitName)] {
				continue
			}
			survivors = append(survivors, u)
		}
		// Records taken out of a table sorted by name leave it sorted, so for
		// survivors with distinct names this is the order retail's battle-entry
		// sort gives the compacted table [02 R-CAT-01 §5]. It is deliberately
		// not re-sorted: the retained sort does not keep equal names in their
		// input order, and running it over the survivors would permute a
		// same-name group by the partitioning of a smaller table.
		// TODO(question): whether retail's sort of the compacted table gives a
		// group of same-name survivors the order they had in the full table is
		// not established. Settle it by keeping each record's discovery
		// position and discovery-time name at compile and re-running the
		// retained sort over the survivors, or by a trace of the compile over
		// a lobby-restricted duplicate group; until then the group keeps its
		// compiled order (docs/DESIGN_MODS_MUTATORS.md §15.4, proposal R-P2).
		if err := c.replaceUnitRecords(survivors); err != nil {
			return err
		}
		if c.SurvivalRoster != nil {
			kept := c.SurvivalRoster.Units[:0:0]
			for _, e := range c.SurvivalRoster.Units {
				if !removed[CanonicalKey(e.Unit)] {
					kept = append(kept, e)
				}
			}
			c.SurvivalRoster.Units = kept
		}
	}
	for _, e := range r.entries {
		if e.Count == 0 {
			continue
		}
		for _, u := range c.restrictionRecords(e.Unit) {
			u.Limit, u.LimitEnabled = int32(e.Count), true
		}
	}
	c.Hash = HashDefinition([]byte("catalog+restrictions\n" + base + "\n" + r.Digest() + "\n"))
	return nil
}
