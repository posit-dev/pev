package checks

import (
	"strings"
	"testing"
)

// stubRunner is a no-op runner registered under a test-only name so the
// Lint() pass can resolve it without dragging the primitives package in
// (avoids an import cycle: primitives depends on checks).
func init() {
	Register("__lint_test__", func(rc RunCtx) Result { return Result{} }, []string{"foo", "bar"})
}

func TestLint_FlagsMissingFields(t *testing.T) {
	cases := []struct {
		name   string
		check  Check
		expect string // substring of the error
	}{
		{"missing id", Check{Title: "t", Why: "w", Primitive: "__lint_test__"}, "missing id"},
		{"missing title", Check{ID: "x.y", Why: "w", Primitive: "__lint_test__"}, "missing title"},
		{"missing why", Check{ID: "x.y", Title: "t", Primitive: "__lint_test__"}, "missing why"},
		{"missing primitive", Check{ID: "x.y", Title: "t", Why: "w"}, "missing primitive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := Lint([]Check{tc.check})
			if len(errs) == 0 {
				t.Fatalf("want error, got none")
			}
			if !containsErr(errs, tc.expect) {
				t.Fatalf("want error containing %q, got %v", tc.expect, errs)
			}
		})
	}
}

func TestLint_RejectsUnknownPrimitive(t *testing.T) {
	errs := Lint([]Check{{ID: "x", Title: "t", Why: "w", Primitive: "no-such-thing"}})
	if !containsErr(errs, `unknown primitive "no-such-thing"`) {
		t.Fatalf("want unknown primitive, got %v", errs)
	}
}

func TestLint_RejectsUnknownWithKey(t *testing.T) {
	errs := Lint([]Check{{
		ID: "x", Title: "t", Why: "w", Primitive: "__lint_test__",
		With: map[string]interface{}{"foo": 1, "typo": 2},
	}})
	if !containsErr(errs, `does not accept key "typo"`) {
		t.Fatalf("want unknown-key error, got %v", errs)
	}
	// The known key MUST not produce an error.
	if containsErr(errs, `does not accept key "foo"`) {
		t.Fatalf("`foo` is a registered key but lint flagged it: %v", errs)
	}
}

func TestLint_RejectsDuplicateID(t *testing.T) {
	errs := Lint([]Check{
		{ID: "dup", Title: "a", Why: "w", Primitive: "__lint_test__", Source: "p1.yaml"},
		{ID: "dup", Title: "b", Why: "w", Primitive: "__lint_test__", Source: "p2.yaml"},
	})
	if !containsErr(errs, "duplicate id") {
		t.Fatalf("want duplicate-id error, got %v", errs)
	}
	if !containsErr(errs, "p1.yaml") {
		t.Fatalf("want duplicate-id error to reference the prior pack, got %v", errs)
	}
}

func TestLint_RejectsNonCanonicalOSID(t *testing.T) {
	errs := Lint([]Check{{
		ID: "x", Title: "t", Why: "w", Primitive: "__lint_test__",
		AppliesTo: AppliesTo{OS: []string{"freebsd-14", "ubuntu-22.04"}},
	}})
	if !containsErr(errs, `applies_to.os value "freebsd-14"`) {
		t.Fatalf("want non-canonical OS error, got %v", errs)
	}
	// ubuntu-22.04 must NOT be flagged.
	if containsErr(errs, `applies_to.os value "ubuntu-22.04"`) {
		t.Fatalf("ubuntu-22.04 is canonical but lint flagged it: %v", errs)
	}
}

func TestLint_AcceptsAnticipatoryUbuntu2604(t *testing.T) {
	// canonicalOSIDs deliberately includes ubuntu-26.04 ahead of Posit's
	// supported matrix so packs can be authored before the matrix lands.
	// This guards against a future "tighten the set" change that breaks
	// that contract.
	errs := Lint([]Check{{
		ID: "x", Title: "t", Why: "w", Primitive: "__lint_test__",
		AppliesTo: AppliesTo{OS: []string{"ubuntu-26.04"}},
	}})
	for _, e := range errs {
		if strings.Contains(e.Error(), "ubuntu-26.04") {
			t.Fatalf("ubuntu-26.04 should be accepted by lint: %v", e)
		}
	}
}

func TestLint_CleanCheckProducesNoErrors(t *testing.T) {
	errs := Lint([]Check{{
		ID: "x.y.z", Title: "t", Why: "w", Primitive: "__lint_test__",
		With:      map[string]interface{}{"foo": "ok"},
		AppliesTo: AppliesTo{OS: []string{"rhel-9"}},
	}})
	if len(errs) != 0 {
		t.Fatalf("clean check should lint without errors, got %v", errs)
	}
}

func containsErr(errs []error, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), sub) {
			return true
		}
	}
	return false
}
