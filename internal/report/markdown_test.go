package report

import (
	"strings"
	"testing"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

// fixtureReport returns a deterministic Report for golden-file rendering
// tests. Timestamps are pinned so the rendered Markdown is stable across runs.
func fixtureReport() checks.Report {
	t0 := time.Date(2026, 6, 1, 14, 22, 5, 0, time.UTC)
	return checks.Report{
		PevVersion:    "1.2.3",
		SchemaVersion: 3,
		Host: discover.HostFacts{
			Hostname: "db-prod-01",
			OS:       "ubuntu-24.04",
			OSPretty: "Ubuntu 24.04.2 LTS",
			OSFamily: "ubuntu",
			Arch:     "amd64",
			CPUs:     8,
			MemMB:    32768,
			DiskGB:   map[string]int{"/": 240},
			Root:     true,
		},
		Run: checks.Run{Products: []string{"workbench"}, Profile: "single-server"},
		Summary: checks.Summary{
			Total: 3, Pass: 1, Fail: 1, Skip: 1,
		},
		StartedAt:  t0,
		FinishedAt: t0.Add(12 * time.Second),
		Results: []checks.Result{
			{
				ID: "net.egress.cdn-rstudio", Title: "Outbound HTTPS to cdn.rstudio.com reachable",
				Status: checks.StatusFail,
				Why:    "Posit installers and software updates are served from cdn.rstudio.com.",
				Reason: "request: dial tcp: i/o timeout",
				Evidence: []checks.Evidence{
					{Note: "GET https://cdn.rstudio.com/ -> timeout"},
				},
				References: []string{"https://docs.posit.co/getting-started/networking.html"},
			},
			{
				ID: "os.supported", Title: "Operating system is supported by Posit professional products",
				Status:   checks.StatusPass,
				Why:      "Workbench, Connect, and PPM support Ubuntu 22.04, 24.04, and RHEL 8/9/10.",
				Evidence: []checks.Evidence{{Command: "true"}},
			},
			{
				ID: "workbench.ssl.cert-key-match", Title: "Workbench SSL certificate and key are paired",
				Status: checks.StatusSkip,
				Reason: "missing or invalid input: template execute: ...",
			},
		},
	}
}

// TestRenderMarkdownDeterministic asserts that the rendered Markdown is
// stable across runs (same input ⇒ identical output). It also pins the
// section ordering and the executive-summary table, which are the parts most
// likely to drift.
func TestRenderMarkdownDeterministic(t *testing.T) {
	a := RenderMarkdown(fixtureReport())
	b := RenderMarkdown(fixtureReport())
	if a != b {
		t.Fatal("RenderMarkdown is non-deterministic")
	}
}

// TestRenderMarkdownStructure pins the high-level structure of the rendered
// report: section order, title format, summary table headers, and that every
// result row lands in the right category section.
func TestRenderMarkdownStructure(t *testing.T) {
	got := RenderMarkdown(fixtureReport())

	for _, want := range []string{
		"# pev report — db-prod-01 — 2026-06-01 14:22:05 UTC",
		"**pev** 1.2.3 · schema 3 · duration 12s",
		"## Summary",
		"| Pass | Warn | Fail | Skip | Unknown |",
		"**1 failure(s)**",
		"## Environment",
		"- OS: Ubuntu 24.04.2 LTS (ubuntu-24.04, family=ubuntu)",
		"- CPUs: 8 · Memory: 32768 MB · Disk(/): 240 GB free",
		"- Running as root: true",
		"## Findings",
		"`net.egress.cdn-rstudio`",
		"`os.supported`",
		"`workbench.ssl.cert-key-match`",
		"Reason: missing or invalid input",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Markdown missing %q\n--- full output ---\n%s", want, got)
		}
	}

	if got != "" {
		envIdx := strings.Index(got, "## Environment")
		findingsIdx := strings.Index(got, "## Findings")
		if envIdx >= findingsIdx {
			t.Errorf("section order wrong: env=%d findings=%d", envIdx, findingsIdx)
		}
	}
}

// TestIconForCoversEveryStatus pins the per-status icon, including the new
// [WARN] tier and the [????] fallback for an unrecognized status.
func TestIconForCoversEveryStatus(t *testing.T) {
	cases := map[checks.Status]string{
		checks.StatusPass:      "[PASS]",
		checks.StatusWarn:      "[WARN]",
		checks.StatusFail:      "[FAIL]",
		checks.StatusSkip:      "[SKIP]",
		checks.StatusUnknown:   "[????]", // unknown isn't a rendered finding icon
		checks.Status("bogus"): "[????]",
	}
	for status, want := range cases {
		if got := iconFor(status); got != want {
			t.Errorf("iconFor(%q) = %q, want %q", status, got, want)
		}
	}
}

// TestRenderMarkdownWarnSummary proves a WARN-bearing, FAIL-free report
// renders the advisory headline (not "All checks passed", not a failure
// banner), shows the Warn column count, and tags the WARN finding [WARN].
func TestRenderMarkdownWarnSummary(t *testing.T) {
	rep := fixtureReport()
	// Flip the failing check to WARN and zero out the failure so the report
	// is WARN-bearing but FAIL-free.
	for i := range rep.Results {
		if rep.Results[i].Status == checks.StatusFail {
			rep.Results[i].Status = checks.StatusWarn
		}
	}
	rep.Summary = checks.Summary{Total: 3, Pass: 1, Warn: 1, Fail: 0, Skip: 1}

	got := RenderMarkdown(rep)
	for _, want := range []string{
		"| Pass | Warn | Fail | Skip | Unknown |",
		"| 1 | 1 | 0 | 1 | 0 |",
		"**1 warning(s)** — review, not blocking.",
		"[WARN] `net.egress.cdn-rstudio`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("WARN report missing %q\n--- full output ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "All checks passed.") {
		t.Errorf("WARN-bearing report must not claim all checks passed:\n%s", got)
	}
	if strings.Contains(got, "failure(s)") {
		t.Errorf("FAIL-free report must not render a failure banner:\n%s", got)
	}
}

// TestRenderMarkdownFailWinsOverWarn proves that when a report has BOTH
// failures and warnings, the headline reports the failures — WARN never
// suppresses or replaces the blocking-failure banner.
func TestRenderMarkdownFailWinsOverWarn(t *testing.T) {
	rep := fixtureReport()
	rep.Summary = checks.Summary{Total: 3, Pass: 1, Warn: 1, Fail: 2, Skip: 0}
	got := RenderMarkdown(rep)
	if !strings.Contains(got, "**2 failure(s)** — investigate before proceeding.") {
		t.Errorf("with failures present, headline must report failures:\n%s", got)
	}
	if strings.Contains(got, "warning(s) — review") {
		t.Errorf("failure headline must not be replaced by the warning headline:\n%s", got)
	}
}
