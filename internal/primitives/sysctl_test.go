package primitives

import (
	"runtime"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func TestSysctlPrimitive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sysctl primitive requires /proc/sys; linux-only")
	}
	// kernel.osrelease always exists on Linux and is a non-empty string;
	// we don't assert a numeric range, just that the read succeeds.
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "sysctl",
		With: map[string]interface{}{"key": "kernel.osrelease"},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusPass {
		t.Fatalf("expected pass on kernel.osrelease, got %s/%s", r.Status, r.Reason)
	}

	missing := checks.Check{
		ID: "x", Title: "x", Primitive: "sysctl",
		With: map[string]interface{}{"key": "kernel.this_does_not_exist"},
	}
	r = runRC(t, missing, discover.HostFacts{})
	if r.Status != checks.StatusUnknown {
		t.Fatalf("expected unknown on missing key, got %s", r.Status)
	}
}

// TestSysctlExpectIntMin covers the numeric-min gate. We use kernel.pid_max,
// which is invariably ≥32k on any modern Linux — pegging the threshold at
// 1 PASSes; pegging it absurdly high (10^9) FAILs.
func TestSysctlExpectIntMin(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sysctl primitive requires /proc/sys; linux-only")
	}
	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "sysctl",
		With: map[string]interface{}{"key": "kernel.pid_max", "expect_int_min": 1},
	}
	if r := runRC(t, pass, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("kernel.pid_max >= 1 should PASS, got %s/%s", r.Status, r.Reason)
	}
	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "sysctl",
		With: map[string]interface{}{"key": "kernel.pid_max", "expect_int_min": 1_000_000_000},
	}
	if r := runRC(t, fail, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("kernel.pid_max >= 1B should FAIL, got %s/%s", r.Status, r.Reason)
	}
}

// TestSysctlExpectEqualsMismatch verifies the equality gate fires on any
// mismatch. We compare kernel.osrelease (a string) against a value that
// can't possibly match.
func TestSysctlExpectEqualsMismatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sysctl primitive requires /proc/sys; linux-only")
	}
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "sysctl",
		With: map[string]interface{}{"key": "kernel.osrelease", "expect_equals": "0.0.0-impossible"},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusFail {
		t.Fatalf("expect_equals mismatch should FAIL, got %s/%s", r.Status, r.Reason)
	}
}
