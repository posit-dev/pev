package checks

import (
	"fmt"
	"strings"
)

// canonicalOSIDs is the closed set of OS identifiers the engine emits
// (internal/operatingsystem/detect.go normalize()). Any applies_to.os
// value outside this set is a typo or an unsupported distro the catalog
// quietly skips on every host — both worth catching at lint time.
//
// Ubuntu 26.04 is included anticipatorily: the LTS exists upstream but
// has not yet been added to Posit's supported list. A check that gates
// `applies_to.os: [ubuntu-26.04]` will SKIP on every host today; we
// accept the value so it lints clean once the matrix is updated.
var canonicalOSIDs = map[string]struct{}{
	"ubuntu-22.04": {},
	"ubuntu-24.04": {},
	"ubuntu-26.04": {},
	"rhel-8":       {},
	"rhel-9":       {},
	"rhel-10":      {},
}

// Lint validates a list of Checks for catalog correctness: required fields,
// known primitive, allowed `with:` keys per primitive, no duplicate IDs,
// and applies_to.os values that match the canonical OS-ID set.
// Returns one error per violation; callers may wrap them.
func Lint(checks []Check) []error {
	var errs []error
	seen := map[string]string{}

	for _, c := range checks {
		prefix := fmt.Sprintf("[%s in %s]", c.ID, c.Source)

		if c.ID == "" {
			errs = append(errs, fmt.Errorf("%s missing id", prefix))
		}
		if c.Title == "" {
			errs = append(errs, fmt.Errorf("%s missing title", prefix))
		}
		if c.Why == "" {
			errs = append(errs, fmt.Errorf("%s missing why (rationale shown to users)", prefix))
		}
		if prev, dup := seen[c.ID]; dup {
			errs = append(errs, fmt.Errorf("%s duplicate id (also in %s)", prefix, prev))
		} else if c.ID != "" {
			seen[c.ID] = c.Source
		}
		for _, os := range c.AppliesTo.OS {
			if _, ok := canonicalOSIDs[os]; !ok {
				errs = append(errs, fmt.Errorf("%s applies_to.os value %q is not a canonical OS ID (allowed: %s)",
					prefix, os, sortedKeys(canonicalOSIDs)))
			}
		}
		if c.Primitive == "" {
			errs = append(errs, fmt.Errorf("%s missing primitive", prefix))
			continue
		}
		if _, err := Lookup(c.Primitive); err != nil {
			errs = append(errs, fmt.Errorf("%s %w", prefix, err))
			continue
		}
		if allowed, ok := AllowedKeys(c.Primitive); ok && len(allowed) > 0 {
			for k := range c.With {
				if _, ok := allowed[k]; !ok {
					errs = append(errs, fmt.Errorf("%s primitive %q does not accept key %q (allowed: %s)",
						prefix, c.Primitive, k, sortedKeys(allowed)))
				}
			}
		}
	}
	return errs
}

func sortedKeys(m map[string]struct{}) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Cheap deterministic sort without importing sort just for a hot loop.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return strings.Join(out, ",")
}
