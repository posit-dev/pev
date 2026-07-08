package primitives

import (
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func TestDNSPrimitiveLocalhost(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "dns",
		With: map[string]interface{}{"name": "localhost", "must_resolve": true, "timeout_seconds": 2},
	}
	if r := runRC(t, c, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("expected localhost to resolve, got %s/%s", r.Status, r.Reason)
	}
}

func TestDNSPrimitiveBogusName(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "dns",
		With: map[string]interface{}{
			"name":            "this-name-should-never-resolve.invalid.",
			"must_resolve":    true,
			"timeout_seconds": 2,
		},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusFail || !strings.Contains(r.Reason, "lookup") {
		t.Fatalf("expected lookup failure, got %s/%s", r.Status, r.Reason)
	}
}
