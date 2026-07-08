package checks

// Filter narrows a Check list by user-supplied product, tag, and ID filters.
// All filters are optional; an empty filter passes everything through.
type Filter struct {
	Products []string // checks must mention at least one
	Tags     []string // checks must have ALL of these tags
	SkipTags []string // checks must have NONE of these tags
	SkipIDs  []string
}

// Apply runs the filter and returns the surviving checks in the input order.
func (f Filter) Apply(in []Check) []Check {
	out := in[:0:0]
	skipID := toSet(f.SkipIDs)
	skipTag := toSet(f.SkipTags)
	mustTag := toSet(f.Tags)
	productSet := toSet(f.Products)

	for _, c := range in {
		if _, drop := skipID[c.ID]; drop {
			continue
		}
		if hasAny(c.Tags, skipTag) {
			continue
		}
		if len(mustTag) > 0 && !hasAll(c.Tags, mustTag) {
			continue
		}
		if len(productSet) > 0 && len(c.AppliesTo.Products) > 0 && !hasAny(c.AppliesTo.Products, productSet) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func toSet(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, v := range s {
		if v != "" {
			m[v] = struct{}{}
		}
	}
	return m
}

func hasAny(list []string, set map[string]struct{}) bool {
	for _, x := range list {
		if _, ok := set[x]; ok {
			return true
		}
	}
	return false
}

func hasAll(list []string, set map[string]struct{}) bool {
	have := toSet(list)
	for k := range set {
		if _, ok := have[k]; !ok {
			return false
		}
	}
	return true
}
