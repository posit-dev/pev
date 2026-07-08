package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/posit-dev/pev/internal/checks"
)

// Diff compares two reports and classifies every check ID into one of:
// regression (a move up the severity ladder PASS < WARN < FAIL/UNKNOWN),
// improvement (a move down it), other status change (any transition that
// touches an off-ladder status like SKIP, or swaps between equal-rank
// FAIL/UNKNOWN), added, removed, or unchanged-evidence-changed.
type Diff struct {
	Baseline      Header      `json:"baseline"`
	Current       Header      `json:"current"`
	Regressions   []DiffEntry `json:"regressions"`
	Improvements  []DiffEntry `json:"improvements"`
	StatusChanged []DiffEntry `json:"status_changed_other"` // e.g. PASS->SKIP
	Added         []DiffEntry `json:"added"`
	Removed       []DiffEntry `json:"removed"`
	EvidenceOnly  []DiffEntry `json:"evidence_only_changes"`
}

type Header struct {
	Hostname string `json:"hostname"`
	Started  string `json:"started_at"`
}

// DiffEntry is one row in any of the diff classifications.
type DiffEntry struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	WasStatus   checks.Status `json:"was_status,omitempty"`
	NowStatus   checks.Status `json:"now_status,omitempty"`
	WasReason   string        `json:"was_reason,omitempty"`
	NowReason   string        `json:"now_reason,omitempty"`
	WasEvidence string        `json:"was_evidence,omitempty"`
	NowEvidence string        `json:"now_evidence,omitempty"`
}

// Compute returns a Diff comparing baseline (a) against current (b).
// It errors if schema_versions differ.
func Compute(a, b checks.Report) (Diff, error) {
	if a.SchemaVersion != b.SchemaVersion {
		return Diff{}, fmt.Errorf("schema_version mismatch: baseline=%d current=%d", a.SchemaVersion, b.SchemaVersion)
	}
	mapA := indexBy(a.Results)
	mapB := indexBy(b.Results)

	d := Diff{
		Baseline: Header{Hostname: a.Host.Hostname, Started: a.StartedAt.UTC().Format("2006-01-02T15:04:05Z")},
		Current:  Header{Hostname: b.Host.Hostname, Started: b.StartedAt.UTC().Format("2006-01-02T15:04:05Z")},
	}

	for id, br := range mapB {
		ar, inA := mapA[id]
		if !inA {
			d.Added = append(d.Added, entryNow(br))
			continue
		}
		// Classify on the severity ladder PASS < WARN < FAIL/UNKNOWN. A
		// move up is a regression, a move down an improvement. Transitions
		// touching an off-ladder status (SKIP, or any equal-rank FAIL<->UNKNOWN
		// swap) are neither and fall through to "other status change". This
		// reproduces the pre-WARN behavior exactly for the four old statuses
		// and slots WARN in as the middle rung per the plan's §5 table.
		ra, rb := severityRank(ar.Status), severityRank(br.Status)
		switch {
		case ra >= 0 && rb >= 0 && ra < rb:
			d.Regressions = append(d.Regressions, entryDelta(ar, br))
		case ra >= 0 && rb >= 0 && ra > rb:
			d.Improvements = append(d.Improvements, entryDelta(ar, br))
		case ar.Status != br.Status:
			d.StatusChanged = append(d.StatusChanged, entryDelta(ar, br))
		default:
			if evidenceDiffers(ar, br) {
				d.EvidenceOnly = append(d.EvidenceOnly, entryDelta(ar, br))
			}
		}
	}
	for id, ar := range mapA {
		if _, inB := mapB[id]; !inB {
			d.Removed = append(d.Removed, entryWas(ar))
		}
	}

	for _, slice := range [][]DiffEntry{d.Regressions, d.Improvements, d.StatusChanged, d.Added, d.Removed, d.EvidenceOnly} {
		sort.Slice(slice, func(i, j int) bool { return slice[i].ID < slice[j].ID })
	}
	return d, nil
}

// severityRank places a status on the diff severity ladder so that a higher
// rank is unambiguously "worse". PASS(0) < WARN(1) < FAIL/UNKNOWN(2). FAIL
// and UNKNOWN share rank 2: a swap between them is neither better nor worse,
// so it falls through to "other status change" exactly as it did before WARN
// existed. SKIP — and any status not on the ladder — returns -1, which the
// caller treats as off-ladder (never an improvement or regression). Keeping
// this as a pure ranking function means the §5 transition table is enforced
// by ordering alone, not by enumerating every pair.
func severityRank(s checks.Status) int {
	switch s {
	case checks.StatusPass:
		return 0
	case checks.StatusWarn:
		return 1
	case checks.StatusFail, checks.StatusUnknown:
		return 2
	default:
		return -1
	}
}

func indexBy(rs []checks.Result) map[string]checks.Result {
	m := make(map[string]checks.Result, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

func entryDelta(a, b checks.Result) DiffEntry {
	return DiffEntry{
		ID: b.ID, Title: b.Title,
		WasStatus: a.Status, NowStatus: b.Status,
		WasReason: a.Reason, NowReason: b.Reason,
		WasEvidence: evidenceSummary(a), NowEvidence: evidenceSummary(b),
	}
}

func entryNow(b checks.Result) DiffEntry {
	return DiffEntry{ID: b.ID, Title: b.Title, NowStatus: b.Status, NowReason: b.Reason, NowEvidence: evidenceSummary(b)}
}

func entryWas(a checks.Result) DiffEntry {
	return DiffEntry{ID: a.ID, Title: a.Title, WasStatus: a.Status, WasReason: a.Reason, WasEvidence: evidenceSummary(a)}
}

func evidenceDiffers(a, b checks.Result) bool {
	return evidenceSummary(a) != evidenceSummary(b) || a.Reason != b.Reason
}

func evidenceSummary(r checks.Result) string {
	parts := make([]string, 0, len(r.Evidence))
	for _, e := range r.Evidence {
		s := strings.TrimSpace(strings.Join([]string{e.Command, e.Path, e.Note, e.Stdout}, " "))
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}

// HasRegressions returns true if any check moved up the severity ladder
// (PASS < WARN < FAIL/UNKNOWN) — e.g. PASS->WARN, PASS->FAIL, WARN->FAIL,
// WARN->UNKNOWN. Any such move flips the flag so `pev diff` can gate CI on it.
func (d Diff) HasRegressions() bool { return len(d.Regressions) > 0 }

// RenderDiffMarkdown formats a Diff for human consumption.
func RenderDiffMarkdown(d Diff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pev diff — %s\n\n", d.Current.Hostname)
	fmt.Fprintf(&b, "- Baseline: %s @ %s\n", d.Baseline.Hostname, d.Baseline.Started)
	fmt.Fprintf(&b, "- Current : %s @ %s\n\n", d.Current.Hostname, d.Current.Started)

	section := func(title string, rows []DiffEntry, icon string) {
		fmt.Fprintf(&b, "## %s (%d)\n\n", title, len(rows))
		if len(rows) == 0 {
			b.WriteString("_(none)_\n\n")
			return
		}
		for _, r := range rows {
			arrow := ""
			if r.WasStatus != "" && r.NowStatus != "" {
				arrow = fmt.Sprintf(" — was %s, now %s", strings.ToUpper(string(r.WasStatus)), strings.ToUpper(string(r.NowStatus)))
			}
			fmt.Fprintf(&b, "- %s `%s`%s\n", icon, r.ID, arrow)
			fmt.Fprintf(&b, "  - %s\n", r.Title)
			if r.WasReason != r.NowReason {
				if r.WasReason != "" {
					fmt.Fprintf(&b, "  - Was: %s\n", r.WasReason)
				}
				if r.NowReason != "" {
					fmt.Fprintf(&b, "  - Now: %s\n", r.NowReason)
				}
			}
		}
		b.WriteString("\n")
	}

	section("Regressions", d.Regressions, "[REGRESS]")
	section("Improvements", d.Improvements, "[IMPROVE]")
	section("Other status changes", d.StatusChanged, "[CHANGED]")
	section("Added checks", d.Added, "[ADDED]")
	section("Removed checks", d.Removed, "[REMOVED]")
	section("Evidence-only changes", d.EvidenceOnly, "[EVIDENCE]")
	return b.String()
}

// RenderDiffJSON is a stable, indented JSON form.
func RenderDiffJSON(d Diff) (string, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
