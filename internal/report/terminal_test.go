package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
)

// TestRenderSkippedListsSkipsWithReason proves the --review-skipped view
// surfaces every SKIPPED check (and only those) with its skip reason,
// grouped under its category header, and never leaks PASS/FAIL rows.
func TestRenderSkippedListsSkipsWithReason(t *testing.T) {
	var buf bytes.Buffer
	RenderSkipped(&buf, fixtureReport(), false)
	out := buf.String()

	if !strings.Contains(out, "Skipped checks (1)") {
		t.Fatalf("missing skip header; got:\n%s", out)
	}
	if !strings.Contains(out, "[SKIP]") || !strings.Contains(out, "workbench.ssl.cert-key-match") {
		t.Fatalf("skipped check not listed; got:\n%s", out)
	}
	if !strings.Contains(out, "reason:") || !strings.Contains(out, "missing or invalid input") {
		t.Fatalf("skip reason not shown; got:\n%s", out)
	}
	// The failing and passing checks from the fixture must not appear.
	if strings.Contains(out, "net.egress.cdn-rstudio") || strings.Contains(out, "[FAIL]") {
		t.Fatalf("non-skip rows leaked into skip view; got:\n%s", out)
	}
	if strings.Contains(out, "[PASS]") {
		t.Fatalf("pass rows leaked into skip view; got:\n%s", out)
	}
}

// warnFixture returns a report with a single WARN result (flipped from the
// fixture's failing check) and a FAIL-free summary, so terminal-render tests
// exercise the advisory path in isolation.
func warnFixture() checks.Report {
	rep := fixtureReport()
	for i := range rep.Results {
		if rep.Results[i].Status == checks.StatusFail {
			rep.Results[i].Status = checks.StatusWarn
		}
	}
	rep.Summary = checks.Summary{Total: 3, Pass: 1, Warn: 1, Fail: 0, Skip: 1}
	return rep
}

// TestRenderTerminalShowsWarn is the core BfR requirement: a WARN must be
// VISIBLE on the console (the whole point of the tier), tagged [WARN], and
// the totals/headline must reflect it — without a failure banner and without
// claiming "all checks passed".
func TestRenderTerminalShowsWarn(t *testing.T) {
	var buf bytes.Buffer
	RenderTerminal(&buf, warnFixture(), false)
	out := buf.String()

	for _, want := range []string{
		"Pass 1  |  Warn 1  |  Fail 0  |  Skip 1  |  Unknown 0",
		"1 warning(s) — review, not blocking.",
		"[WARN]",
		"net.egress.cdn-rstudio",
		"1 warning(s)", // per-category header tally
	} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal WARN view missing %q\n--- full output ---\n%s", want, out)
		}
	}
	// A WARN-only run must not be reported as a failure or as all-clear.
	if strings.Contains(out, "[FAIL]") || strings.Contains(out, "investigate before proceeding") {
		t.Errorf("WARN-only run leaked a failure render:\n%s", out)
	}
	if strings.Contains(out, "All checks passed") {
		t.Errorf("WARN-bearing run must not claim all checks passed:\n%s", out)
	}
	// The category header must NOT call a warning "failing".
	if strings.Contains(out, "1 failing") {
		t.Errorf("WARN miscounted as failing in category header:\n%s", out)
	}
}

// TestRenderTerminalWarnColored proves the WARN line is wrapped in yellow
// (not red) when color is on, distinguishing it from a FAIL line.
func TestRenderTerminalWarnColored(t *testing.T) {
	var buf bytes.Buffer
	RenderTerminal(&buf, warnFixture(), true)
	out := buf.String()
	// The WARN finding line must carry the yellow code and not the red one.
	wantYellowWarn := ansiYellow + "  [WARN]"
	if !strings.Contains(out, wantYellowWarn) {
		t.Errorf("WARN line not wrapped in yellow; want substring %q\n%s", wantYellowWarn, out)
	}
	if strings.Contains(out, ansiRed+"  [WARN]") {
		t.Errorf("WARN line was colored red; it must be yellow:\n%s", out)
	}
}

// TestRenderTerminalMixedFailAndWarn proves that when failures and warnings
// coexist, both render (each with its own tag and color), the headline keys
// on the failure, and a mixed-category header counts them separately.
func TestRenderTerminalMixedFailAndWarn(t *testing.T) {
	rep := fixtureReport()
	// fixture: net.egress.cdn-rstudio FAIL (keep), add a WARN in the same
	// (Other) category, and an UNKNOWN to confirm it still renders as a
	// failure alongside.
	rep.Results = append(rep.Results,
		checks.Result{ID: "z.advisory.note", Title: "Advisory note", Status: checks.StatusWarn, Reason: "non-standard but valid"},
		checks.Result{ID: "z.undecided", Title: "Undecided", Status: checks.StatusUnknown, Reason: "primitive could not decide"},
	)
	rep.Summary = checks.Summary{Total: 5, Pass: 1, Warn: 1, Fail: 1, Skip: 1, Unknown: 1}

	var buf bytes.Buffer
	RenderTerminal(&buf, rep, false)
	out := buf.String()

	// The totals line carries every tier.
	if !strings.Contains(out, "Pass 1  |  Warn 1  |  Fail 1  |  Skip 1  |  Unknown 1") {
		t.Errorf("totals line wrong:\n%s", out)
	}
	// Failure headline wins whenever any failure is present.
	if !strings.Contains(out, "1 failure(s) —") {
		t.Errorf("failure headline must win when failures present:\n%s", out)
	}
	// The "Other" category holds FAIL + WARN + UNKNOWN. UNKNOWN counts as
	// failing, WARN does not, so the header reads "2 failing, 1 warning(s)".
	if !strings.Contains(out, "2 failing, 1 warning(s)") {
		t.Errorf("mixed category header tally wrong; want '2 failing, 1 warning(s)':\n%s", out)
	}
	// Each status renders with its own distinct tag and the id in parens.
	for _, tag := range []string{"[FAIL]", "(net.egress.cdn-rstudio)", "[WARN]", "(z.advisory.note)", "[UNKN]", "(z.undecided)"} {
		if !strings.Contains(out, tag) {
			t.Errorf("missing rendered row %q:\n%s", tag, out)
		}
	}
}

// TestRenderTerminalEmptyCategoryFoldsToOther proves a result with an empty
// Category is bucketed under "Other" by the terminal renderer — the same
// fold groupByCategory applies for the on-disk Markdown — so an empty
// Category never produces a blank-named, visually-orphaned section header.
// (Regression guard for the terminal-vs-Markdown bucketing divergence.)
func TestRenderTerminalEmptyCategoryFoldsToOther(t *testing.T) {
	rep := checks.Report{
		SchemaVersion: 3,
		Summary:       checks.Summary{Total: 2, Warn: 1, Fail: 1},
		Results: []checks.Result{
			{ID: "e.warn", Title: "T-e.warn", Category: "", Status: checks.StatusWarn},
			{ID: "e.fail", Title: "T-e.fail", Category: checks.CategoryOther, Status: checks.StatusFail},
		},
	}

	var buf bytes.Buffer
	RenderTerminal(&buf, rep, false)
	term := buf.String()
	md := RenderMarkdown(rep)

	// Both results must live under a single "Other" section in each renderer.
	// The terminal header is "Other (1 failing, 1 warning(s))"; the Markdown
	// header is "### Other (2)". Neither may emit a blank-named header.
	if !strings.Contains(term, "Other (1 failing, 1 warning(s))") {
		t.Errorf("terminal did not fold empty Category into Other:\n%s", term)
	}
	if !strings.Contains(md, "### Other (2)") {
		t.Errorf("markdown did not fold empty Category into Other:\n%s", md)
	}
	// A blank-named header manifests as a line that is "(" preceded only by
	// whitespace — i.e. an empty category name before the tally parens.
	for _, line := range strings.Split(term, "\n") {
		if strings.HasPrefix(line, " (") || strings.HasPrefix(line, "(") {
			if strings.Contains(line, "failing") || strings.Contains(line, "warning") {
				t.Errorf("terminal emitted a blank-named category header: %q", line)
			}
		}
	}
}

// TestRenderSkippedEmpty proves the no-skips path prints a clear sentinel
// rather than an empty header.
func TestRenderSkippedEmpty(t *testing.T) {
	rep := fixtureReport()
	// Flip the lone skip to pass so nothing is skipped.
	for i := range rep.Results {
		if rep.Results[i].Status == checks.StatusSkip {
			rep.Results[i].Status = checks.StatusPass
		}
	}
	var buf bytes.Buffer
	RenderSkipped(&buf, rep, false)
	if got := buf.String(); !strings.Contains(got, "No checks were skipped.") {
		t.Fatalf("expected empty-skip sentinel; got:\n%s", got)
	}
}
