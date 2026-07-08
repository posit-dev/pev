package prompt

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns what fn
// wrote to stderr. Used to assert the TTY-downgrade notice surfaces on the
// terminal (not just the logrus file logger).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestNonInteractiveTakesDefault(t *testing.T) {
	d := New(ModeNonInteractive)
	got, err := d.Input("workbench hostname:", "wb.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wb.example.com" {
		t.Fatalf("want default, got %q", got)
	}
}

func TestYesModeTakesDefault(t *testing.T) {
	d := New(ModeYes)
	got, err := d.Confirm("Use SSL?", true)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("want true, got false")
	}
}

func TestInteractiveDowngradesWithoutTTY(t *testing.T) {
	// `go test` always runs without a TTY for stdin/stdout, so the
	// interactive driver must downgrade to yes-mode silently.
	d := New(ModeInteractive)
	got, err := d.Input("license file path:", "/var/lib/rstudio-server/x.lic")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/var/lib/rstudio-server/x.lic" {
		t.Fatalf("want default, got %q", got)
	}
}

// TestInteractiveDowngradeNotifiesOnStderr proves the implicit TTY-less
// downgrade is announced on the terminal, not buried in the log file. `go
// test` runs without a TTY, so ModeInteractive always downgrades here. This
// is the fix for Ralf's "piped pev asked no questions and I couldn't tell
// why" — the notice must reach stderr so an SE sees it.
func TestInteractiveDowngradeNotifiesOnStderr(t *testing.T) {
	out := captureStderr(t, func() { New(ModeInteractive) })
	if !strings.Contains(out, "--yes mode") {
		t.Fatalf("interactive TTY-less downgrade should print a stderr notice, got %q", out)
	}
}

// TestExplicitModesStaySilent guards the other half: a user who explicitly
// asked for --yes or --non-interactive already knows defaults will be taken,
// so New must NOT print the downgrade notice for those modes — keeps
// intentional CI pipelines quiet.
func TestExplicitModesStaySilent(t *testing.T) {
	for _, mode := range []Mode{ModeYes, ModeNonInteractive} {
		out := captureStderr(t, func() { New(mode) })
		if out != "" {
			t.Fatalf("mode=%v should emit no stderr notice, got %q", mode, out)
		}
	}
}

// TestPasswordReturnsEmptyInNonInteractive locks in the secret-handling
// contract called out in CLAUDE.md §8 / cmd/assess.secretInputKeys: a
// password has no sensible auto-default, so callers must treat the
// empty return as "skip the dependent check" rather than fall through
// with a fake secret. ModeYes shares the same return so CI runs and
// piped sessions never end up holding a phony password.
func TestPasswordReturnsEmptyInNonInteractive(t *testing.T) {
	for _, mode := range []Mode{ModeYes, ModeNonInteractive} {
		d := New(mode)
		got, err := d.Password("PostgreSQL password:")
		if err != nil {
			t.Fatalf("mode=%v: %v", mode, err)
		}
		if got != "" {
			t.Fatalf("mode=%v: password should be empty, got %q (would leak as a real secret)", mode, got)
		}
	}
}

// TestInteractiveTTY-less downgrades to ModeYes (covered above) so
// Password() in interactive-mode-without-a-TTY MUST also return "".
// Belt-and-braces for the secret path: this is the path real CI
// pipelines exercise when they run pev without --non-interactive but
// without a controlling tty.
func TestPasswordReturnsEmptyInInteractiveWithoutTTY(t *testing.T) {
	d := New(ModeInteractive)
	got, err := d.Password("PostgreSQL password:")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("interactive-without-tty Password should return empty, got %q", got)
	}
}

// TestMultiSelectDoesNotDuplicateDefaults guards the "connect, connect"
// regression. survey/v2 writes a MultiSelect result by APPENDING each
// chosen option to the destination slice, so seeding the destination with
// a copy of defaultValues comes back with every pre-selected default
// duplicated. The interactive path (var out []string) is the real fix;
// this test exercises the yes/non-interactive contract — the selection is
// returned verbatim, never doubled — which is what the assess command's
// product echo renders.
func TestMultiSelectDoesNotDuplicateDefaults(t *testing.T) {
	const sentinel = "system configuration checks - product independent"
	for _, mode := range []Mode{ModeYes, ModeNonInteractive} {
		d := New(mode)
		got, err := d.MultiSelect(
			"Which Posit products will run on this host?",
			[]string{sentinel, "workbench", "connect", "packagemanager"},
			[]string{sentinel, "connect"},
		)
		if err != nil {
			t.Fatalf("mode=%v: %v", mode, err)
		}
		seen := map[string]int{}
		for _, v := range got {
			seen[v]++
			if seen[v] > 1 {
				t.Fatalf("mode=%v: %q appears %d times in %v (default duplicated)", mode, v, seen[v], got)
			}
		}
	}
}
