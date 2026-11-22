package main

import (
	"cmp"
	"slices"
	"strings"

	"github.com/nanolathe-gg/nanolathe/internal/content"
)

type unitViewerEntry struct {
	Key string
	Def *content.UnitDef
}

// unitViewerEntries reads the immutable content catalog, without build-menu or
// battle admission filters (DESIGN_DEVELOPER_TOOLS §7). UnitRecords preserves
// definitions hidden by duplicate or empty lookup names [02 R-CAT-01 §§4–5].
func unitViewerEntries(c *content.Catalog) []unitViewerEntry {
	type orderedEntry struct {
		entry                  unitViewerEntry
		name, identity         string
		foldName, foldIdentity string
		ordinal                int
	}
	content.ApplyUnitText(c)
	records := c.UnitRecords()
	ordered := make([]orderedEntry, 0, len(records))
	for ordinal, def := range records {
		if def == nil {
			continue
		}
		key := def.CanonicalKey
		if key == "" {
			key = content.CanonicalKey(def.UnitName)
		}
		identity := def.UnitName
		if identity == "" {
			identity = key
		}
		ordered = append(ordered, orderedEntry{
			entry: unitViewerEntry{Key: key, Def: def},
			name:  def.Name, identity: identity,
			foldName: strings.ToLower(def.Name), foldIdentity: strings.ToLower(identity),
			ordinal: ordinal,
		})
	}
	// Name then internal ID is the viewer's presentation order. The catalog
	// ordinal makes even identical names and IDs a total order without writing
	// a sort index into the immutable definitions [I1][I6].
	slices.SortFunc(ordered, func(a, b orderedEntry) int {
		return cmp.Or(
			cmp.Compare(a.foldName, b.foldName),
			cmp.Compare(a.foldIdentity, b.foldIdentity),
			cmp.Compare(a.entry.Def.UnitDefID, b.entry.Def.UnitDefID),
			cmp.Compare(a.name, b.name),
			cmp.Compare(a.identity, b.identity),
			cmp.Compare(a.entry.Key, b.entry.Key),
			cmp.Compare(a.ordinal, b.ordinal),
		)
	})
	entries := make([]unitViewerEntry, len(ordered))
	for i, item := range ordered {
		entries[i] = item.entry
	}
	return entries
}

// Each query token may match any identity field. Copying the entry slice keeps
// filtering and later list selection independent of the complete catalog list.
func filterUnitViewerEntries(entries []unitViewerEntry, query string) []unitViewerEntry {
	tokens := strings.Fields(strings.ToLower(query))
	filtered := make([]unitViewerEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Def == nil {
			continue
		}
		name := strings.ToLower(entry.Def.Name)
		identity := strings.ToLower(entry.Def.UnitName)
		key := strings.ToLower(entry.Key)
		matches := true
		for _, token := range tokens {
			if !strings.Contains(name, token) && !strings.Contains(identity, token) && !strings.Contains(key, token) {
				matches = false
				break
			}
		}
		if matches {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
