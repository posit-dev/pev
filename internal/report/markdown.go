package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/posit-dev/pev/internal/checks"
)

// WriteMarkdown writes the report as Markdown to outDir/<base>.md.
func WriteMarkdown(outDir, base string, rep checks.Report) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, base+".md")
	// Reports are intentionally world-readable so SEs and customers can hand
	// them off via email/PR/Slack without chmod-ing first. Secret-shaped
	// inputs (see cmd/assess.secretInputKeys) are redacted before they
	// reach the report; pev does not collect license keys or credentials.
	if err := os.WriteFile(path, []byte(RenderMarkdown(rep)), 0o644); err != nil { //nolint:gosec // G306: see comment above
		return "", err
	}
	return path, nil
}

// RenderMarkdown produces the Markdown body for a report. Deterministic:
// results sorted by ID, sections in fixed order, no timestamps in section
// titles to keep diffs clean.
func RenderMarkdown(rep checks.Report) string {
	var b strings.Builder
	renderHeader(&b, rep)
	renderSummary(&b, rep)
	renderEnvironment(&b, rep)
	renderFindings(&b, rep)
	return b.String()
}

func renderHeader(b *strings.Builder, rep checks.Report) {
	fmt.Fprintf(b, "# pev report — %s — %s\n\n", rep.Host.Hostname, rep.StartedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(b, "**pev** %s · schema %d · duration %s\n\n",
		rep.PevVersion, rep.SchemaVersion, rep.FinishedAt.Sub(rep.StartedAt).Round(1e9))
}

func renderSummary(b *strings.Builder, rep checks.Report) {
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(b, "| Pass | Warn | Fail | Skip | Unknown |\n")
	fmt.Fprintf(b, "|---:|---:|---:|---:|---:|\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d |\n\n",
		rep.Summary.Pass, rep.Summary.Warn, rep.Summary.Fail, rep.Summary.Skip, rep.Summary.Unknown)
	switch {
	case rep.Summary.Fail > 0:
		fmt.Fprintf(b, "**%d failure(s)** — investigate before proceeding.\n\n", rep.Summary.Fail)
	case rep.Summary.Warn > 0:
		// Advisory only: a WARN-clean run is still installable. Make the
		// distinction explicit so a reader doesn't conflate it with FAIL.
		fmt.Fprintf(b, "**%d warning(s)** — review, not blocking.\n\n", rep.Summary.Warn)
	default:
		b.WriteString("All checks passed.\n\n")
	}
}

func renderEnvironment(b *strings.Builder, rep checks.Report) {
	b.WriteString("## Environment\n\n")
	fmt.Fprintf(b, "- OS: %s (%s, family=%s)\n", rep.Host.OSPretty, rep.Host.OS, rep.Host.OSFamily)
	fmt.Fprintf(b, "- Architecture: %s\n", rep.Host.Arch)
	fmt.Fprintf(b, "- CPUs: %d · Memory: %d MB · Disk(/): %d GB free\n", rep.Host.CPUs, rep.Host.MemMB, rep.Host.DiskGB["/"])
	fmt.Fprintf(b, "- Hostname: %s", rep.Host.Hostname)
	if rep.Host.FQDN != "" && rep.Host.FQDN != rep.Host.Hostname {
		fmt.Fprintf(b, " (FQDN: %s)", rep.Host.FQDN)
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "- Running as root: %v\n", rep.Host.Root)
	fmt.Fprintf(b, "- Detected products: workbench=%v connect=%v packagemanager=%v\n",
		rep.Host.Products.Workbench, rep.Host.Products.Connect, rep.Host.Products.PackageManager)
	if len(rep.Host.R) > 0 {
		fmt.Fprintf(b, "- R installs: %s\n", strings.Join(rep.Host.R, ", "))
	}
	if len(rep.Host.Python) > 0 {
		fmt.Fprintf(b, "- Python installs: %s\n", strings.Join(rep.Host.Python, ", "))
	}
	if len(rep.Host.Quarto) > 0 {
		fmt.Fprintf(b, "- Quarto installs: %s\n", strings.Join(rep.Host.Quarto, ", "))
	}
	if len(rep.Run.Products) > 0 {
		fmt.Fprintf(b, "- Selected products: %s\n", strings.Join(rep.Run.Products, ", "))
	}
	if rep.Run.Profile != "" {
		fmt.Fprintf(b, "- Profile: %s\n", rep.Run.Profile)
	}
	b.WriteString("\n")
}

// renderFindings groups results by Category. The on-disk file shows every
// check (including PASS/SKIP); the terminal renderer hides non-failures.
func renderFindings(b *strings.Builder, rep checks.Report) {
	b.WriteString("## Findings\n\n")
	byCat := groupByCategory(rep.Results)
	for _, cat := range categoryOrder(byCat) {
		rs := byCat[cat]
		fmt.Fprintf(b, "### %s (%d)\n\n", cat, len(rs))
		for _, r := range rs {
			renderResult(b, r)
		}
		b.WriteString("\n")
	}
}

func renderResult(b *strings.Builder, r checks.Result) {
	fmt.Fprintf(b, "- %s `%s` — %s\n", iconFor(r.Status), r.ID, r.Title)
	if r.Reason != "" {
		fmt.Fprintf(b, "  - Reason: %s\n", r.Reason)
	}
	if r.Why != "" {
		fmt.Fprintf(b, "  - Why: %s\n", oneLine(r.Why))
	}
	if r.Remediation != "" {
		fmt.Fprintf(b, "  - Fix: %s\n", oneLine(r.Remediation))
	}
	for _, ev := range r.Evidence {
		if ev.Command != "" {
			fmt.Fprintf(b, "  - Command: `%s`\n", ev.Command)
		}
		if ev.Path != "" {
			fmt.Fprintf(b, "  - Path: `%s`\n", ev.Path)
		}
		if ev.Note != "" {
			fmt.Fprintf(b, "  - Note: %s\n", ev.Note)
		}
	}
	for _, ref := range r.References {
		fmt.Fprintf(b, "  - Reference: %s\n", ref)
	}
}

func groupByCategory(in []checks.Result) map[string][]checks.Result {
	out := map[string][]checks.Result{}
	for _, r := range in {
		out[categoryOf(r)] = append(out[categoryOf(r)], r)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].ID < out[k][j].ID })
	}
	return out
}

func iconFor(s checks.Status) string {
	switch s {
	case checks.StatusPass:
		return "[PASS]"
	case checks.StatusWarn:
		return "[WARN]"
	case checks.StatusFail:
		return "[FAIL]"
	case checks.StatusSkip:
		return "[SKIP]"
	default:
		return "[????]"
	}
}

func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}
