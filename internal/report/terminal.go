package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/posit-dev/pev/internal/checks"
)

// ANSI colour helpers. Disabled when the writer doesn't support a TTY (we
// don't sniff the terminal here — assess writes to os.Stdout and pipelines
// can flip NO_COLOR or pass --no-color in a future flag iteration).
const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
)

// RenderTerminal writes the human-facing summary view to w. Shows the
// totals table, then per-category sections that include only failing or
// unknown checks (PASS/SKIP are still in the on-disk Markdown). Output
// is parseable: each finding line is `[STATUS] TITLE (ID)` — the title
// reads first, with the dotted id in parens, mirroring the progress
// stream's "<short_description> (<id>)" convention.
func RenderTerminal(w io.Writer, rep checks.Report, color bool) {
	red := func(s string) string { return wrap(s, ansiRed, color) }
	yellow := func(s string) string { return wrap(s, ansiYellow, color) }
	bold := func(s string) string { return wrap(s, ansiBold, color) }
	dim := func(s string) string { return wrap(s, ansiDim, color) }

	fmt.Fprintf(w, "%s\n", bold(fmt.Sprintf("pev report — %s — %s",
		rep.Host.Hostname, rep.StartedAt.UTC().Format("2006-01-02 15:04:05 UTC"))))
	fmt.Fprintf(w, "pev %s · schema %d · %d checks · %s\n\n",
		rep.PevVersion, rep.SchemaVersion, rep.Summary.Total,
		rep.FinishedAt.Sub(rep.StartedAt).Round(1e9))

	// Totals — one line, pipe-separated, easy to scan and to grep.
	totals := fmt.Sprintf("Pass %d  |  Warn %d  |  Fail %d  |  Skip %d  |  Unknown %d",
		rep.Summary.Pass, rep.Summary.Warn, rep.Summary.Fail, rep.Summary.Skip, rep.Summary.Unknown)
	fmt.Fprintln(w, totals)
	// Headline keys on Fail first (blocking), then Warn (advisory, not
	// exit-fatal — see cmd/assess.go). WARN never suppresses or replaces a
	// FAIL headline; failures always take the floor.
	switch {
	case rep.Summary.Fail > 0:
		fmt.Fprintf(w, "%s investigate before proceeding.\n",
			red(fmt.Sprintf("%d failure(s) —", rep.Summary.Fail)))
	case rep.Summary.Warn > 0:
		fmt.Fprintf(w, "%s\n",
			yellow(fmt.Sprintf("%d warning(s) — review, not blocking.", rep.Summary.Warn)))
	default:
		fmt.Fprintf(w, "%s\n", dim("All checks passed."))
	}
	fmt.Fprintln(w)

	renderTerminalEnvironment(w, rep, bold)

	// Group failing/unknown/warning results by category. Pass and Skip
	// stay out of the terminal — they live in the on-disk Markdown for the
	// full audit trail. UNKNOWN counts as a failure here because it means a
	// primitive could not decide. WARN is advisory: it is shown (yellow,
	// the point of the tier is that the SE sees it) but it is not a
	// failure, so it never trips the exit code (see cmd/assess.go).
	byCat := map[string][]checks.Result{}
	for _, r := range rep.Results {
		if r.Status != checks.StatusFail && r.Status != checks.StatusUnknown && r.Status != checks.StatusWarn {
			continue
		}
		byCat[categoryOf(r)] = append(byCat[categoryOf(r)], r)
	}
	if len(byCat) == 0 {
		fmt.Fprintln(w, dim("All checks passed (or were skipped)."))
		return
	}

	for _, cat := range categoryOrder(byCat) {
		rs := byCat[cat]
		sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
		fmt.Fprintf(w, "%s (%s)\n", bold(cat), categoryCounts(rs))
		for _, r := range rs {
			tag, colour := "[FAIL]", red
			switch r.Status {
			case checks.StatusUnknown:
				tag = "[UNKN]"
			case checks.StatusWarn:
				tag, colour = "[WARN]", yellow
			}
			line := fmt.Sprintf("  %s %s (%s)", tag, r.Title, r.ID)
			fmt.Fprintln(w, colour(line))
			if r.Reason != "" {
				fmt.Fprintf(w, "    %s %s\n", yellow("reason:"), r.Reason)
			}
			for _, ev := range r.Evidence {
				if ev.Command != "" {
					fmt.Fprintf(w, "    %s %s\n", dim("command:"), indentMultiline(ev.Command, "      "))
				}
				if ev.Note != "" {
					fmt.Fprintf(w, "    %s %s\n", dim("note:"), indentMultiline(ev.Note, "      "))
				}
			}
			if r.Why != "" {
				fmt.Fprintf(w, "    %s %s\n", dim("why:"), oneLine(r.Why))
			}
			if r.Remediation != "" {
				fmt.Fprintf(w, "    %s %s\n", yellow("fix:"), oneLine(r.Remediation))
			}
		}
		fmt.Fprintln(w)
	}
}

// RenderSkipped writes a skip-review view to w: the same per-category
// shape as the failure summary, but listing SKIPPED checks with their
// skip reason. Used by `pev assess --review-skipped` so an SE can audit
// what the run *didn't* test — a declined prompt, an inapplicable OS, a
// missing language runtime — without opening the on-disk Markdown. The
// totals/environment header is owned by RenderTerminal; this function
// renders only the skip sections so it can be appended after it.
func RenderSkipped(w io.Writer, rep checks.Report, color bool) {
	yellow := func(s string) string { return wrap(s, ansiYellow, color) }
	bold := func(s string) string { return wrap(s, ansiBold, color) }
	dim := func(s string) string { return wrap(s, ansiDim, color) }

	byCat := map[string][]checks.Result{}
	for _, r := range rep.Results {
		if r.Status != checks.StatusSkip {
			continue
		}
		byCat[categoryOf(r)] = append(byCat[categoryOf(r)], r)
	}
	if len(byCat) == 0 {
		fmt.Fprintln(w, dim("No checks were skipped."))
		return
	}

	fmt.Fprintf(w, "%s\n\n", bold(fmt.Sprintf("Skipped checks (%d)", rep.Summary.Skip)))
	for _, cat := range categoryOrder(byCat) {
		rs := byCat[cat]
		sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
		fmt.Fprintf(w, "%s (%d skipped)\n", bold(cat), len(rs))
		for _, r := range rs {
			line := fmt.Sprintf("  %s %s (%s)", "[SKIP]", r.Title, r.ID)
			fmt.Fprintln(w, yellow(line))
			reason := r.Reason
			if reason == "" {
				reason = "no reason recorded"
			}
			fmt.Fprintf(w, "    %s %s\n", yellow("reason:"), reason)
			if r.Why != "" {
				fmt.Fprintf(w, "    %s %s\n", dim("why:"), oneLine(r.Why))
			}
		}
		fmt.Fprintln(w)
	}
}

// renderTerminalEnvironment prints the Environment block on stdout, the
// same shape as `pev discover`. SEs use it as a quick one-line "what is
// this host" reference before scanning failures. Mirrors the on-disk
// Markdown's Environment section so screenshots and the saved report
// agree.
func renderTerminalEnvironment(w io.Writer, rep checks.Report, bold func(string) string) {
	fmt.Fprintln(w, bold("Environment"))
	fmt.Fprintf(w, "  OS:           %s (%s, family=%s)\n", rep.Host.OSPretty, rep.Host.OS, rep.Host.OSFamily)
	fmt.Fprintf(w, "  Architecture: %s\n", rep.Host.Arch)
	fmt.Fprintf(w, "  CPUs:         %d  ·  Memory: %d MB  ·  Disk(/): %d GB free\n",
		rep.Host.CPUs, rep.Host.MemMB, rep.Host.DiskGB["/"])
	if rep.Host.FQDN != "" && rep.Host.FQDN != rep.Host.Hostname {
		fmt.Fprintf(w, "  Hostname:     %s (FQDN: %s)\n", rep.Host.Hostname, rep.Host.FQDN)
	} else {
		fmt.Fprintf(w, "  Hostname:     %s\n", rep.Host.Hostname)
	}
	fmt.Fprintf(w, "  Running root: %v\n", rep.Host.Root)
	fmt.Fprintf(w, "  Detected:     workbench=%v connect=%v packagemanager=%v\n",
		rep.Host.Products.Workbench, rep.Host.Products.Connect, rep.Host.Products.PackageManager)
	if len(rep.Host.R) > 0 {
		fmt.Fprintf(w, "  R:            %s\n", strings.Join(rep.Host.R, ", "))
	}
	if len(rep.Host.Python) > 0 {
		fmt.Fprintf(w, "  Python:       %s\n", strings.Join(rep.Host.Python, ", "))
	}
	if len(rep.Host.Quarto) > 0 {
		fmt.Fprintf(w, "  Quarto:       %s\n", strings.Join(rep.Host.Quarto, ", "))
	}
	if len(rep.Run.Products) > 0 {
		fmt.Fprintf(w, "  Selected:     %s\n", strings.Join(rep.Run.Products, ", "))
	}
	fmt.Fprintln(w)
}

// categoryCounts renders the per-category header tally. WARN must not be
// folded into "failing" — a category with only advisories reads "1 warning",
// not "1 failing". FAIL and UNKNOWN are both counted as "failing" (UNKNOWN is
// treated as a failure for display; see the loop above). Examples:
// "3 failing", "2 warning(s)", "3 failing, 2 warning(s)".
func categoryCounts(rs []checks.Result) string {
	failing, warning := 0, 0
	for _, r := range rs {
		if r.Status == checks.StatusWarn {
			warning++
		} else {
			failing++
		}
	}
	switch {
	case failing > 0 && warning > 0:
		return fmt.Sprintf("%d failing, %d warning(s)", failing, warning)
	case warning > 0:
		return fmt.Sprintf("%d warning(s)", warning)
	default:
		return fmt.Sprintf("%d failing", failing)
	}
}

func wrap(s, code string, on bool) string {
	if !on {
		return s
	}
	return code + s + ansiReset
}

// indentMultiline trims a trailing newline from s and re-indents every line
// after the first by prefix. The first line is returned unprefixed because
// the caller has already written the field label and a single space; the
// goal is to keep continuation lines column-aligned beneath that label.
func indentMultiline(s, prefix string) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	var b strings.Builder
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(prefix)
		b.WriteString(line)
	}
	return b.String()
}
