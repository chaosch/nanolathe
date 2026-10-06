package session

import (
	"fmt"

	"github.com/nanolathe-gg/nanolathe/internal/content"
)

// applyEntryRestrictions applies the battle's unit restrictions to the
// catalog one skirmish or Survival battle entry runs on
// (docs/DESIGN_MODS_MUTATORS.md §15.5). It runs straight after the catalog
// compile, before the Community weapon preparation and the mutators, the
// position mission entry gives its UseOnlyUnits list, so the mutators see the
// restricted catalog (§15.1 "Order with the other transforms").
//
// A zero set returns cat itself, so a battle without restrictions runs on
// exactly the catalog it always did and every fingerprint lock is unchanged.
// Otherwise it returns a restricted clone; the catalog handed in may be
// shared between battles and is never written. An entry the catalog cannot
// take is an entry error wrapping *content.RestrictionsError: the host has
// already resolved its preference against the running content (§15.3), so
// whatever reaches entry was asked for explicitly.
//
// Restrictions transform content, not a gameplay rule, so there is no
// gameplay-mode test here: they apply in Strict 3.1 as in every other mode
// (INVARIANTS I11).
func applyEntryRestrictions(cat *content.Catalog, r content.Restrictions) (*content.Catalog, error) {
	if cat == nil || r.IsZero() {
		return cat, nil
	}
	out := cat.Clone()
	if err := out.ApplyRestrictions(r); err != nil {
		return nil, fmt.Errorf("session: battle-entry unit restrictions %q: %w", r.String(), err)
	}
	return out, nil
}
