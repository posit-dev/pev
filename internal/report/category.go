package report

import (
	"sort"

	"github.com/posit-dev/pev/internal/checks"
)

// canonicalCategoryOrder pins the display order of known categories. Both
// the Markdown writer and the terminal renderer walk this list first; any
// category outside it (custom YAML packs, etc.) is appended alphabetically
// so new packs surface naturally without forcing a code change.
var canonicalCategoryOrder = []string{
	checks.CategoryNetworking,
	checks.CategoryStorage,
	checks.CategoryOperatingSystem,
	checks.CategorySecurity,
	checks.CategoryIdentity,
	checks.CategorySSL,
	checks.CategoryPackages,
	checks.CategorySizing,
	checks.CategoryProduct,
	checks.CategoryOther,
}

// categoryOf returns the bucket a result belongs to, folding an empty
// Category onto CategoryOther. Both renderers route through this so the
// terminal summary and the on-disk Markdown bucket a check identically —
// an empty Category must never produce a blank-named section header.
func categoryOf(r checks.Result) string {
	if r.Category == "" {
		return checks.CategoryOther
	}
	return r.Category
}

// categoryOrder returns the keys of byCat in display order: known
// categories first (per canonicalCategoryOrder), unknown categories
// appended in alphabetical order.
func categoryOrder(byCat map[string][]checks.Result) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(byCat))
	for _, c := range canonicalCategoryOrder {
		if _, ok := byCat[c]; ok {
			out = append(out, c)
			seen[c] = true
		}
	}
	extra := []string{}
	for c := range byCat {
		if !seen[c] {
			extra = append(extra, c)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}
