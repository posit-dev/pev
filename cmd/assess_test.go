package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

// recordingDriver is a prompt.Driver test double: it answers No to every
// Confirm and records the question text so tests can assert the wording a
// given product selection produces. Input/Select/Password return their
// default so the driver is usable for any prompt, but the home-share tests
// only need the Confirm question.
type recordingDriver struct{ confirms []string }

func (r *recordingDriver) Input(q, def string) (string, error) { return def, nil }
func (r *recordingDriver) Confirm(q string, def bool) (bool, error) {
	r.confirms = append(r.confirms, q)
	return false, nil
}

func (r *recordingDriver) Select(q string, opts []string, def string) (string, error) {
	return def, nil
}

func (r *recordingDriver) MultiSelect(q string, opts, def []string) ([]string, error) {
	return def, nil
}
func (r *recordingDriver) Password(q string) (string, error) { return "", nil }

// TestPromptHomeShareWording locks in the NFS prompt's product-aware
// wording: Workbench keeps per-user home directories on the share, so the
// question is about the home directory; Connect/PM have no home requirement,
// so the question is about the application's DataDir instead.
func TestPromptHomeShareWording(t *testing.T) {
	cases := []struct {
		name       string
		wanted     map[string]bool
		wantSubstr string
	}{
		{"workbench asks about home directory", map[string]bool{"workbench": true}, "home directory"},
		{"workbench + connect still asks about home directory", map[string]bool{"workbench": true, "connect": true}, "home directory"},
		{"connect-only asks about DataDir", map[string]bool{"connect": true}, "DataDir"},
		{"ppm-only asks about DataDir", map[string]bool{"ppm": true}, "DataDir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &recordingDriver{}
			in := map[string]string{}
			promptHomeShare(d, in, tc.wanted)
			if len(d.confirms) != 1 {
				t.Fatalf("expected exactly one Confirm, got %d: %v", len(d.confirms), d.confirms)
			}
			if !strings.Contains(d.confirms[0], tc.wantSubstr) {
				t.Fatalf("question %q should mention %q", d.confirms[0], tc.wantSubstr)
			}
		})
	}
}

// TestAssessExitErrorOnlyFailIsFatal locks in the plan §4.3 exit contract:
// `pev assess` returns a non-zero (error) exit ONLY when Summary.Fail > 0. A
// WARN-only run — and even an UNKNOWN-bearing run — must exit 0. This is the
// guardrail against a future refactor accidentally making WARN exit-fatal,
// which would defeat the entire point of the advisory tier.
func TestAssessExitErrorOnlyFailIsFatal(t *testing.T) {
	cases := []struct {
		name    string
		summary checks.Summary
		wantErr bool
	}{
		{"all pass", checks.Summary{Total: 5, Pass: 5}, false},
		{"warn only is not fatal", checks.Summary{Total: 5, Pass: 3, Warn: 2}, false},
		{"unknown only is not fatal", checks.Summary{Total: 5, Pass: 3, Unknown: 2}, false},
		{"skip only is not fatal", checks.Summary{Total: 5, Pass: 3, Skip: 2}, false},
		{"warn + unknown still not fatal", checks.Summary{Total: 6, Pass: 2, Warn: 2, Unknown: 2}, false},
		{"single failure is fatal", checks.Summary{Total: 5, Pass: 4, Fail: 1}, true},
		{"failure wins over warn", checks.Summary{Total: 6, Pass: 2, Warn: 2, Fail: 2}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assessExitError(tc.summary)
			if tc.wantErr && err == nil {
				t.Fatalf("summary %+v: want error, got nil", tc.summary)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("summary %+v: want nil, got %v", tc.summary, err)
			}
		})
	}
}

// TestRedactSecretsScrubsKnownSecretKeys locks in the on-disk-redaction
// contract (CLAUDE.md §8): every secret-shaped input key must be
// replaced with "(redacted)" before the inputs map lands in the JSON
// report — but the original map kept by the engine retains the raw
// secret so primitives can still template against it.
func TestRedactSecretsScrubsKnownSecretKeys(t *testing.T) {
	in := map[string]string{
		"postgres_host":     "db.example.com",
		"postgres_port":     "5432",
		"postgres_user":     "rstudio",
		"postgres_password": "supersecret",
		"connect_smtp_host": "smtp.example.com",
	}
	out := redactSecrets(in)
	if out["postgres_password"] != "(redacted)" {
		t.Fatalf("password was not redacted: %q", out["postgres_password"])
	}
	if out["postgres_host"] != "db.example.com" {
		t.Fatalf("non-secret host was rewritten: %q", out["postgres_host"])
	}
	// Original map still holds the raw secret.
	if in["postgres_password"] != "supersecret" {
		t.Fatalf("original input map was mutated; the engine needs the raw secret")
	}
}

// TestRedactSecretsLeavesEmptySecretAsIs guards against accidentally
// emitting "(redacted)" for an unset secret — that would mislead the
// reader into thinking a password WAS supplied. Empty stays empty.
func TestRedactSecretsLeavesEmptySecretAsIs(t *testing.T) {
	in := map[string]string{"postgres_password": ""}
	out := redactSecrets(in)
	if out["postgres_password"] != "" {
		t.Fatalf("empty secret should stay empty, got %q", out["postgres_password"])
	}
}

// TestFilterNoneOption locks in the multi-select tiebreak: real products
// always beat the "no products" sentinel because the menu pre-selects
// the sentinel and most SEs add a product without un-ticking it first.
// Sentinel-alone produces an empty slice; the caller maps that to the
// "none" string via len(selected) == 0.
func TestFilterNoneOption(t *testing.T) {
	const sentinel = "system configuration checks - product independent"
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"sentinel alone leaves no real picks", []string{sentinel}, []string{}},
		{
			"real products beat sentinel",
			[]string{sentinel, "workbench"},
			[]string{"workbench"},
		},
		{
			"sentinel mixed with multiple products",
			[]string{"workbench", sentinel, "connect"},
			[]string{"workbench", "connect"},
		},
		{
			"no sentinel, all real",
			[]string{"workbench", "connect"},
			[]string{"workbench", "connect"},
		},
		{"empty input", []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterNoneOption(tc.in, sentinel)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), tc.want, len(tc.want))
			}
			if len(tc.want) > 0 && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNormalizeProducts locks in the --products contract: the ubiquitous
// `ppm` alias maps to the catalog's `packagemanager`, values are case-folded
// and trimmed, empty tokens are dropped (cobra emits them for `--products=`),
// and any unsupported value is rejected with an error rather than silently
// degrading to a common-only run.
func TestNormalizeProducts(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"ppm alias maps to packagemanager", []string{"ppm"}, []string{"packagemanager"}, false},
		{"canonical names pass through", []string{"workbench", "connect", "packagemanager"}, []string{"workbench", "connect", "packagemanager"}, false},
		{"case-folded", []string{"Workbench", "PPM"}, []string{"workbench", "packagemanager"}, false},
		{"whitespace trimmed", []string{" connect "}, []string{"connect"}, false},
		{"empty token dropped", []string{""}, []string{}, false},
		{"nil is empty (auto-detect downstream)", nil, []string{}, false},
		{"unknown rejected", []string{"foobar"}, nil, true},
		{"typo rejected", []string{"packagemnager"}, nil, true},
		{"valid mixed with bogus rejected", []string{"workbench", "foobar"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeProducts(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeProducts(%v) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeProducts(%v) unexpected error: %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeProducts(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizeIdP locks in the --idp contract: the empty string (flag unset)
// is preserved so the interactive opt-in still runs; supported kinds pass
// case-folded; anything else is rejected.
func TestNormalizeIdP(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"saml", "saml", false},
		{"OIDC", "oidc", false},
		{" ldap ", "ldap", false},
		{"none", "none", false},
		{"WHATEVER", "", true},
		{"sam", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := normalizeIdP(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeIdP(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeIdP(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeIdP(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestValidateOutputs proves an unrecognized --output token is rejected
// (silent zero-file writes were a data-loss footgun), while empty tokens —
// which cobra emits for `--output=` and which wantOutputs folds to "both" —
// are accepted.
func TestValidateOutputs(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		wantErr bool
	}{
		{"md+json ok", []string{"md", "json"}, false},
		{"markdown alias ok", []string{"markdown"}, false},
		{"empty token ok (defaults to both)", []string{""}, false},
		{"whitespace trimmed ok", []string{" json "}, false},
		{"typo rejected", []string{"jsn"}, true},
		{"unsupported format rejected", []string{"pdf"}, true},
		{"valid mixed with junk rejected", []string{"json", "garbage"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOutputs(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("validateOutputs(%v): want error, got nil", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateOutputs(%v): unexpected error %v", tc.in, err)
			}
		})
	}
}

// TestComputePreselect locks in the documented precedence chain
// flag > profile > detect. The bug it guards against: a bare
// SelectedFromFlag consults detection before profile, so `--profile connect`
// on a Workbench-detected host would silently run Workbench checks.
func TestComputePreselect(t *testing.T) {
	wbDetected := discover.Products{Workbench: true}
	cases := []struct {
		name     string
		products []string
		profile  string
		detected discover.Products
		want     []string
	}{
		{"flag wins over everything", []string{"connect"}, "workbench", wbDetected, []string{"connect"}},
		{"profile beats detection", nil, "connect", wbDetected, []string{"connect"}},
		{"profile ppm beats detection", nil, "ppm", wbDetected, []string{"packagemanager"}},
		{"detect is the fallback", nil, "", wbDetected, []string{"workbench"}},
		{"nothing set, nothing detected", nil, "", discover.Products{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computePreselect(tc.products, tc.profile, tc.detected)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("computePreselect(%v, %q, %+v) = %v, want %v", tc.products, tc.profile, tc.detected, got, tc.want)
			}
		})
	}
}

// TestWantOutputs covers the small flag-parsing helper that decides
// which report formats to emit. No --output flag → both; explicit list
// → just those.
func TestWantOutputs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		md   bool
		j    bool
	}{
		{"empty defaults to both", nil, true, true},
		{"md only", []string{"md"}, true, false},
		{"json only", []string{"json"}, false, true},
		{"markdown alias", []string{"markdown"}, true, false},
		{"both explicit", []string{"md", "json"}, true, true},
		{"trims whitespace", []string{" md ", "  json"}, true, true},
		{"unknown silently dropped", []string{"pdf"}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			md, j := wantOutputs(tc.in)
			if md != tc.md || j != tc.j {
				t.Fatalf("got md=%v json=%v, want md=%v json=%v", md, j, tc.md, tc.j)
			}
		})
	}
}
