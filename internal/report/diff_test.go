package report

import (
	"testing"
	"time"

	"github.com/posit-dev/pev/internal/checks"
)

func TestComputeRegressionAndImprovement(t *testing.T) {
	a := checks.Report{SchemaVersion: 1, Results: []checks.Result{
		{ID: "a", Status: checks.StatusPass},
		{ID: "b", Status: checks.StatusFail},
		{ID: "c", Status: checks.StatusPass},
	}}
	b := checks.Report{SchemaVersion: 1, Results: []checks.Result{
		{ID: "a", Status: checks.StatusFail}, // regression
		{ID: "b", Status: checks.StatusPass}, // improvement
		{ID: "c", Status: checks.StatusPass}, // unchanged
		{ID: "d", Status: checks.StatusPass}, // added
	}}
	d, err := Compute(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasRegressions() {
		t.Fatal("expected regression flag set")
	}
	if len(d.Regressions) != 1 || d.Regressions[0].ID != "a" {
		t.Fatalf("regressions: %+v", d.Regressions)
	}
	if len(d.Improvements) != 1 || d.Improvements[0].ID != "b" {
		t.Fatalf("improvements: %+v", d.Improvements)
	}
	if len(d.Added) != 1 || d.Added[0].ID != "d" {
		t.Fatalf("added: %+v", d.Added)
	}
}

func TestComputeRejectsSchemaMismatch(t *testing.T) {
	a := checks.Report{SchemaVersion: 1}
	b := checks.Report{SchemaVersion: 2}
	if _, err := Compute(a, b); err == nil {
		t.Fatal("expected schema mismatch error")
	}
}

// bucket names the diff classification a single ID landed in.
type bucket int

const (
	bucketRegression bucket = iota
	bucketImprovement
	bucketStatusChanged
	bucketEvidenceOnly
	bucketNone
)

// classify runs a one-ID was→now transition through Compute and reports
// which bucket it landed in. Evidence is held constant so the only signal is
// the status change.
func classify(t *testing.T, was, now checks.Status) bucket {
	t.Helper()
	a := checks.Report{SchemaVersion: 3, Results: []checks.Result{{ID: "x", Status: was}}}
	b := checks.Report{SchemaVersion: 3, Results: []checks.Result{{ID: "x", Status: now}}}
	d, err := Compute(a, b)
	if err != nil {
		t.Fatalf("compute(%s→%s): %v", was, now, err)
	}
	switch {
	case len(d.Regressions) == 1:
		return bucketRegression
	case len(d.Improvements) == 1:
		return bucketImprovement
	case len(d.StatusChanged) == 1:
		return bucketStatusChanged
	case len(d.EvidenceOnly) == 1:
		return bucketEvidenceOnly
	default:
		return bucketNone
	}
}

// TestComputeWarnTransitions exhaustively pins the §5 transition table for
// the WARN tier — WARN sits strictly between PASS and FAIL/UNKNOWN on the
// severity ladder — AND re-confirms the pre-WARN transitions are unchanged.
func TestComputeWarnTransitions(t *testing.T) {
	const (
		P = checks.StatusPass
		W = checks.StatusWarn
		F = checks.StatusFail
		U = checks.StatusUnknown
		S = checks.StatusSkip
	)
	cases := []struct {
		was, now checks.Status
		want     bucket
		why      string
	}{
		// --- WARN as the middle rung (the new behavior, plan §5) ---
		{P, W, bucketRegression, "PASS→WARN: got worse, even if not blocking"},
		{W, P, bucketImprovement, "WARN→PASS: resolved"},
		{W, F, bucketRegression, "WARN→FAIL: got worse"},
		{W, U, bucketRegression, "WARN→UNKNOWN: got worse"},
		{F, W, bucketImprovement, "FAIL→WARN: got better (no longer blocking)"},
		{U, W, bucketImprovement, "UNKNOWN→WARN: got better"},
		{W, S, bucketStatusChanged, "WARN→SKIP: off-ladder, neither better nor worse"},
		{S, W, bucketStatusChanged, "SKIP→WARN: off-ladder"},

		// --- pre-WARN transitions must be byte-for-byte unchanged ---
		{P, F, bucketRegression, "PASS→FAIL unchanged"},
		{P, U, bucketRegression, "PASS→UNKNOWN unchanged"},
		{F, P, bucketImprovement, "FAIL→PASS unchanged"},
		{U, P, bucketImprovement, "UNKNOWN→PASS unchanged"},
		{F, U, bucketStatusChanged, "FAIL↔UNKNOWN equal rank → other status change"},
		{U, F, bucketStatusChanged, "UNKNOWN↔FAIL equal rank → other status change"},
		{P, S, bucketStatusChanged, "PASS→SKIP unchanged"},
		{S, P, bucketStatusChanged, "SKIP→PASS unchanged"},
		{F, S, bucketStatusChanged, "FAIL→SKIP unchanged"},

		// --- no-op transitions land nowhere (evidence held constant) ---
		{P, P, bucketNone, "PASS→PASS no change"},
		{W, W, bucketNone, "WARN→WARN no change"},
		{F, F, bucketNone, "FAIL→FAIL no change"},
	}
	for _, tc := range cases {
		t.Run(string(tc.was)+"_to_"+string(tc.now), func(t *testing.T) {
			if got := classify(t, tc.was, tc.now); got != tc.want {
				t.Fatalf("%s→%s landed in bucket %d, want %d (%s)", tc.was, tc.now, got, tc.want, tc.why)
			}
		})
	}
}

// TestComputeWarnRegressionFlag confirms a PASS→WARN move flips
// HasRegressions, so `pev diff` exits 1 on it (a warning appearing where a
// pass used to be is a real regression for CI gating purposes).
func TestComputeWarnRegressionFlag(t *testing.T) {
	a := checks.Report{SchemaVersion: 3, Results: []checks.Result{{ID: "x", Status: checks.StatusPass}}}
	b := checks.Report{SchemaVersion: 3, Results: []checks.Result{{ID: "x", Status: checks.StatusWarn}}}
	d, err := Compute(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasRegressions() {
		t.Fatal("PASS→WARN must register as a regression")
	}
}

// TestOrderByRecencyIsInputOrderIndependent proves the newer report (by
// started_at) is always returned as current and the older as baseline, so
// `pev diff old.json new.json` and `pev diff new.json old.json` produce the
// same regression/improvement classification.
func TestOrderByRecencyIsInputOrderIndependent(t *testing.T) {
	older := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	old := checks.Report{
		SchemaVersion: 3,
		StartedAt:     older,
		Results:       []checks.Result{{ID: "x", Status: checks.StatusPass}},
	}
	recent := checks.Report{
		SchemaVersion: 3,
		StartedAt:     newer,
		Results:       []checks.Result{{ID: "x", Status: checks.StatusFail}}, // regressed since old
	}

	// Regardless of argument order, baseline must be the older run and current
	// the newer, so the PASS→FAIL move always reads as a regression.
	for _, tc := range []struct {
		name string
		x, y checks.Report
	}{
		{"old_then_new", old, recent},
		{"new_then_old", recent, old},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline, current := OrderByRecency(tc.x, tc.y)
			if !baseline.StartedAt.Equal(older) {
				t.Fatalf("baseline started_at = %s, want %s", baseline.StartedAt, older)
			}
			if !current.StartedAt.Equal(newer) {
				t.Fatalf("current started_at = %s, want %s", current.StartedAt, newer)
			}
			d, err := Compute(baseline, current)
			if err != nil {
				t.Fatal(err)
			}
			if !d.HasRegressions() {
				t.Fatalf("expected PASS→FAIL regression regardless of input order")
			}
			if len(d.Regressions) != 1 || d.Regressions[0].ID != "x" {
				t.Fatalf("regressions: %+v", d.Regressions)
			}
		})
	}
}

// TestOrderByRecencyTiePreservesInputOrder pins the tie-break: identical
// started_at leaves the given order untouched, so output stays stable.
func TestOrderByRecencyTiePreservesInputOrder(t *testing.T) {
	ts := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	// SchemaVersion is used purely as an identity marker to tell the two
	// same-timestamp reports apart.
	x := checks.Report{StartedAt: ts, SchemaVersion: 1}
	y := checks.Report{StartedAt: ts, SchemaVersion: 2}

	baseline, current := OrderByRecency(x, y)
	if baseline.SchemaVersion != 1 || current.SchemaVersion != 2 {
		t.Fatalf("tie must preserve input order: baseline=%d current=%d", baseline.SchemaVersion, current.SchemaVersion)
	}
}

// TestComputeWarnSchemaV3RoundTrips proves two v3 reports diff cleanly (no
// spurious schema mismatch) now that the embedded catalog is schema 3.
func TestComputeWarnSchemaV3RoundTrips(t *testing.T) {
	a := checks.Report{SchemaVersion: 3, Results: []checks.Result{{ID: "x", Status: checks.StatusWarn}}}
	b := checks.Report{SchemaVersion: 3, Results: []checks.Result{{ID: "x", Status: checks.StatusWarn}}}
	if _, err := Compute(a, b); err != nil {
		t.Fatalf("two v3 reports must diff without error: %v", err)
	}
}
