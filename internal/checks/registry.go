package checks

import (
	"context"
	"fmt"
	"sync"

	"github.com/posit-dev/pev/internal/discover"
)

// RunCtx is everything a Runner needs to make a check decision.
type RunCtx struct {
	Ctx    context.Context
	Check  Check
	Facts  discover.HostFacts
	Inputs map[string]string
}

// Runner evaluates one check and returns a populated Result.
// It MUST NOT panic. Set Status to StatusUnknown when the primitive
// genuinely cannot decide (e.g. `with:` is malformed at runtime).
type Runner func(rc RunCtx) Result

var (
	regMu     sync.RWMutex
	registry  = map[string]Runner{}
	validKeys = map[string]map[string]struct{}{}
)

// Register adds a Runner under a primitive name. Panics on duplicate names —
// this only fails at init() time, never at runtime, so a panic is acceptable.
func Register(name string, r Runner, allowedKeys []string) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[name]; dup {
		panic("checks: duplicate primitive registered: " + name)
	}
	registry[name] = r
	keys := map[string]struct{}{}
	for _, k := range allowedKeys {
		keys[k] = struct{}{}
	}
	validKeys[name] = keys
}

// Lookup returns the Runner for primitive `name`, or an error if unregistered.
func Lookup(name string) (Runner, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	r, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown primitive %q", name)
	}
	return r, nil
}

// AllowedKeys returns the set of valid `with:` keys for a primitive, used by
// `pev lint-checks` to surface typos before runtime. Empty map means "no
// restriction" — primitives with dynamic schemas (none today) opt out by not
// declaring keys.
func AllowedKeys(name string) (map[string]struct{}, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	k, ok := validKeys[name]
	return k, ok
}
