package checks

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/discover"
)

// fakePrimitive registers a primitive that echoes back its `with.expect` value
// as the Result.Status — handy for engine unit tests.
func init() {
	Register("test-fake", func(rc RunCtx) Result {
		want, _ := rc.Check.With["expect"].(string)
		return Result{Status: Status(want)}
	}, []string{"expect", "name"})
}

func TestEngineSkipsOnAppliesTo(t *testing.T) {
	e := Engine{Facts: discover.HostFacts{OS: "rhel-9"}}
	c := Check{
		ID: "x", Title: "x", Why: "x", Primitive: "test-fake",
		AppliesTo: AppliesTo{OS: []string{"ubuntu-22.04"}}, With: map[string]interface{}{"expect": "pass"},
	}
	r := e.runOne(context.Background(), c)
	if r.Status != StatusSkip {
		t.Fatalf("want skip, got %s", r.Status)
	}
	// The reason must name both the host's actual OS and the targets so
	// --review-skipped is self-explanatory.
	if !strings.Contains(r.Reason, "rhel-9") || !strings.Contains(r.Reason, "ubuntu-22.04") {
		t.Fatalf("skip reason should name host OS and targets, got %q", r.Reason)
	}
}

// TestAppliesTo covers the OS/arch gate's reason strings directly,
// including the empty-fact ("unknown") fallback.
func TestAppliesTo(t *testing.T) {
	cases := []struct {
		name       string
		a          AppliesTo
		hf         discover.HostFacts
		wantApply  bool   // true = applies (reason "")
		wantSubstr string // substring required in the skip reason
	}{
		{"no gate applies", AppliesTo{}, discover.HostFacts{OS: "rhel-9"}, true, ""},
		{"os match applies", AppliesTo{OS: []string{"rhel-9"}}, discover.HostFacts{OS: "rhel-9"}, true, ""},
		{"os mismatch skips", AppliesTo{OS: []string{"rhel-9", "rhel-10"}}, discover.HostFacts{OS: "ubuntu-26.04"}, false, "ubuntu-26.04"},
		{"arch mismatch skips", AppliesTo{Arch: []string{"amd64"}}, discover.HostFacts{Arch: "arm64"}, false, "arm64"},
		{"empty os fact labelled unknown", AppliesTo{OS: []string{"rhel-9"}}, discover.HostFacts{}, false, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := appliesTo(tc.a, tc.hf)
			if tc.wantApply && got != "" {
				t.Fatalf("want applies (empty reason), got %q", got)
			}
			if !tc.wantApply {
				if got == "" {
					t.Fatalf("want skip reason, got empty")
				}
				if !strings.Contains(got, tc.wantSubstr) {
					t.Fatalf("reason %q should contain %q", got, tc.wantSubstr)
				}
			}
		})
	}
}

// TestRunProgressMarksActualSkips proves the progress stream tags
// "(skipped)" from each check's REAL outcome, so the on-screen markers
// reconcile with the report's SKIP tally. The regression it guards: the
// previous printer predicted skips from host facts alone and so missed
// input-gated skips — a check whose templated input expands empty (a
// declined opt-in prompt) ran clean per the gates but SKIPs at runtime,
// and printed as if it had run.
func TestRunProgressMarksActualSkips(t *testing.T) {
	var buf bytes.Buffer
	e := Engine{
		Facts:    discover.HostFacts{OS: "rhel-9"},
		Progress: &buf,
	}
	checks := []Check{
		// Runs to completion — no "(skipped)" suffix.
		{
			ID: "a.ran", Title: "ran", Primitive: "test-fake",
			With: map[string]interface{}{"expect": "pass"},
		},
		// Static applies_to gate — skips.
		{
			ID: "b.os-gated", Title: "os", Primitive: "test-fake",
			AppliesTo: AppliesTo{OS: []string{"ubuntu-22.04"}},
			With:      map[string]interface{}{"expect": "pass"},
		},
		// Input-gated skip: the template references an input the SE never
		// supplied, so expandWith fails and runOne SKIPs. The old
		// fact-only predictor printed this as if it ran.
		{
			ID: "c.input-gated", Title: "input", Primitive: "test-fake",
			With: map[string]interface{}{"name": "{{ .Inputs.declined }}", "expect": "pass"},
		},
	}
	results := e.Run(context.Background(), checks)

	lines := map[string]string{}
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		for _, id := range []string{"a.ran", "b.os-gated", "c.input-gated"} {
			if strings.Contains(ln, id) {
				lines[id] = ln
			}
		}
	}
	if strings.Contains(lines["a.ran"], "(skipped)") {
		t.Errorf("a.ran should not be marked skipped: %q", lines["a.ran"])
	}
	if !strings.Contains(lines["b.os-gated"], "(skipped)") {
		t.Errorf("b.os-gated should be marked skipped: %q", lines["b.os-gated"])
	}
	if !strings.Contains(lines["c.input-gated"], "(skipped)") {
		t.Errorf("c.input-gated should be marked skipped: %q", lines["c.input-gated"])
	}

	// Every printed "(skipped)" marker must correspond to a real SKIP
	// result — the count on screen equals the count in the tally.
	printed := strings.Count(buf.String(), "(skipped)")
	actual := 0
	for _, r := range results {
		if r.Status == StatusSkip {
			actual++
		}
	}
	if printed != actual {
		t.Fatalf("printed %d (skipped) markers but %d checks actually skipped", printed, actual)
	}
}

// TestRunProgressColorWrapsSkipSuffix proves ProgressColor wraps only the
// "(skipped)" suffix in yellow (and resets), leaving lines for checks that
// ran uncolored. With color off the suffix stays a bare string so piped /
// non-TTY runs and the report tally remain plain-text greppable.
func TestRunProgressColorWrapsSkipSuffix(t *testing.T) {
	run := func(color bool) string {
		var buf bytes.Buffer
		e := Engine{
			Facts:         discover.HostFacts{OS: "rhel-9"},
			Progress:      &buf,
			ProgressColor: color,
		}
		e.Run(context.Background(), []Check{
			{
				ID: "a.ran", Title: "ran", Primitive: "test-fake",
				With: map[string]interface{}{"expect": "pass"},
			},
			{
				ID: "b.os-gated", Title: "os", Primitive: "test-fake",
				AppliesTo: AppliesTo{OS: []string{"ubuntu-22.04"}},
				With:      map[string]interface{}{"expect": "pass"},
			},
		})
		return buf.String()
	}

	colored := run(true)
	if !strings.Contains(colored, progressYellow+"(skipped)"+progressReset) {
		t.Errorf("colored run should wrap the skip suffix in yellow:\n%q", colored)
	}
	// The line for the check that ran carries no color codes.
	for _, ln := range strings.Split(colored, "\n") {
		if strings.Contains(ln, "a.ran") && strings.Contains(ln, progressYellow) {
			t.Errorf("non-skip line must not be colored: %q", ln)
		}
	}

	plain := run(false)
	if strings.Contains(plain, progressYellow) || strings.Contains(plain, progressReset) {
		t.Errorf("color-off run must emit no ANSI codes:\n%q", plain)
	}
	if !strings.Contains(plain, " (skipped)") {
		t.Errorf("color-off run must still mark skips in plain text:\n%q", plain)
	}
}

func TestEngineExpandsTemplate(t *testing.T) {
	e := Engine{
		Facts:  discover.HostFacts{Hostname: "h1"},
		Inputs: map[string]string{"workbench_hostname": "wb.example"},
	}
	c := Check{
		ID: "x", Title: "x", Why: "x", Primitive: "test-fake",
		With: map[string]interface{}{"name": "{{ .Inputs.workbench_hostname }}", "expect": "pass"},
	}
	r := e.runOne(context.Background(), c)
	if r.Status != StatusPass {
		t.Fatalf("want pass, got %s reason=%s", r.Status, r.Reason)
	}
}

// TestEngineRoundTripsWarn proves the engine passes a primitive's StatusWarn
// through unchanged — the WARN tier is a first-class status, not silently
// coerced to PASS or FAIL on the way out of runOne.
func TestEngineRoundTripsWarn(t *testing.T) {
	e := Engine{Facts: discover.HostFacts{}}
	c := Check{
		ID: "x", Title: "x", Why: "x", Primitive: "test-fake",
		Remediation: "advisory fix",
		With:        map[string]interface{}{"expect": "warn"},
	}
	r := e.runOne(context.Background(), c)
	if r.Status != StatusWarn {
		t.Fatalf("want warn, got %s", r.Status)
	}
	// Remediation is copied onto FAIL/UNKNOWN only — a WARN carries no fix
	// banner today (the report renders WARN advisories, not remediations).
	if r.Remediation != "" {
		t.Fatalf("WARN should not carry remediation, got %q", r.Remediation)
	}
}

func TestEngineSkipsOnMissingInput(t *testing.T) {
	e := Engine{Facts: discover.HostFacts{}}
	c := Check{
		ID: "x", Title: "x", Why: "x", Primitive: "test-fake",
		With: map[string]interface{}{"name": "{{ .Inputs.missing }}", "expect": "pass"},
	}
	r := e.runOne(context.Background(), c)
	if r.Status != StatusSkip {
		t.Fatalf("want skip, got %s", r.Status)
	}
}

// TestMissingRequires covers the seven tokens the requires gate
// understands plus the "unknown token defaults to satisfied" contract.
func TestMissingRequires(t *testing.T) {
	cases := []struct {
		name     string
		requires []string
		hf       discover.HostFacts
		want     string // empty = all requirements satisfied
	}{
		{"empty list", nil, discover.HostFacts{}, ""},

		{"r missing", []string{"r"}, discover.HostFacts{}, "r"},
		{"r present", []string{"r"}, discover.HostFacts{R: []string{"4.4.1"}}, ""},

		{"python missing", []string{"python"}, discover.HostFacts{}, "python"},
		{"python present", []string{"python"}, discover.HostFacts{Python: []string{"3.12"}}, ""},

		{"quarto missing", []string{"quarto"}, discover.HostFacts{}, "quarto"},
		{"quarto present", []string{"quarto"}, discover.HostFacts{Quarto: []string{"1.5.0"}}, ""},

		{"uv missing", []string{"uv"}, discover.HostFacts{}, "uv"},
		{"uv present", []string{"uv"}, discover.HostFacts{HasUV: true}, ""},

		{"pip missing", []string{"pip"}, discover.HostFacts{}, "pip"},
		{"pip present", []string{"pip"}, discover.HostFacts{HasPip: true}, ""},

		{"apt missing", []string{"apt"}, discover.HostFacts{}, "apt"},
		{"apt present", []string{"apt"}, discover.HostFacts{HasApt: true}, ""},

		{"dnf missing", []string{"dnf"}, discover.HostFacts{}, "dnf"},
		{"dnf present", []string{"dnf"}, discover.HostFacts{HasDNF: true}, ""},

		// First-missing-wins: r missing AND python missing → "r"
		// surfaces because it appears first in the requires slice.
		// Stable order makes the SKIP reason deterministic.
		{
			"first missing wins",
			[]string{"r", "python"},
			discover.HostFacts{},
			"r",
		},
		{
			"unknown token defaults to satisfied",
			[]string{"future-token-that-does-not-exist"},
			discover.HostFacts{},
			"",
		},
		{
			"all satisfied",
			[]string{"r", "python", "uv", "apt"},
			discover.HostFacts{
				R: []string{"4.4"}, Python: []string{"3.12"},
				HasUV: true, HasApt: true,
			},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingRequires(tc.requires, tc.hf); got != tc.want {
				t.Fatalf("missingRequires(%v) = %q, want %q", tc.requires, got, tc.want)
			}
		})
	}
}

// TestMissingRequiresReason proves the command-gated tokens (detected via a
// bare $PATH lookup) get a skip reason that tells the SE the fix is to put
// the tool on PATH — a versioned interpreter under /opt is not enough — while
// the /opt-scanned tokens keep the generic "missing required tooling" wording.
func TestMissingRequiresReason(t *testing.T) {
	for _, tc := range []struct {
		token   string
		wantSub string
	}{
		{"pip", "not on PATH"},
		{"uv", "not on PATH"},
		{"apt", "not on PATH"},
		{"dnf", "not on PATH"},
		{"r", "missing required tooling: r"},
		{"python", "missing required tooling: python"},
		{"quarto", "missing required tooling: quarto"},
	} {
		t.Run(tc.token, func(t *testing.T) {
			got := missingRequiresReason(tc.token)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("missingRequiresReason(%q) = %q, want substring %q", tc.token, got, tc.wantSub)
			}
		})
	}
}
