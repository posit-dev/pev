package report

import (
	"testing"

	"github.com/posit-dev/pev/internal/checks"
)

// TestSummarizeCountsEveryTier locks in that Summarize tallies all five
// statuses into the right buckets — and in particular that the WARN tier
// (schema v3) lands in Summary.Warn rather than being silently dropped or
// folded into Fail. Total must equal the input length regardless of status
// mix, including any status the switch doesn't recognize.
func TestSummarizeCountsEveryTier(t *testing.T) {
	results := []checks.Result{
		{ID: "p1", Status: checks.StatusPass},
		{ID: "p2", Status: checks.StatusPass},
		{ID: "w1", Status: checks.StatusWarn},
		{ID: "f1", Status: checks.StatusFail},
		{ID: "f2", Status: checks.StatusFail},
		{ID: "f3", Status: checks.StatusFail},
		{ID: "s1", Status: checks.StatusSkip},
		{ID: "u1", Status: checks.StatusUnknown},
	}
	s := Summarize(results)
	if s.Total != len(results) {
		t.Fatalf("total = %d, want %d", s.Total, len(results))
	}
	if s.Pass != 2 {
		t.Errorf("pass = %d, want 2", s.Pass)
	}
	if s.Warn != 1 {
		t.Errorf("warn = %d, want 1 (WARN must not be dropped or folded into Fail)", s.Warn)
	}
	if s.Fail != 3 {
		t.Errorf("fail = %d, want 3", s.Fail)
	}
	if s.Skip != 1 {
		t.Errorf("skip = %d, want 1", s.Skip)
	}
	if s.Unknown != 1 {
		t.Errorf("unknown = %d, want 1", s.Unknown)
	}
	// Sanity: the five tier counts must reconcile with the total.
	if s.Pass+s.Warn+s.Fail+s.Skip+s.Unknown != s.Total {
		t.Fatalf("tier counts %+v do not sum to total %d", s, s.Total)
	}
}

// TestSummarizeWarnOnly proves a run whose only non-pass results are WARN
// produces Fail==0 — the property the assess exit code relies on.
func TestSummarizeWarnOnly(t *testing.T) {
	s := Summarize([]checks.Result{
		{ID: "a", Status: checks.StatusPass},
		{ID: "b", Status: checks.StatusWarn},
		{ID: "c", Status: checks.StatusWarn},
	})
	if s.Warn != 2 || s.Fail != 0 {
		t.Fatalf("want warn=2 fail=0, got warn=%d fail=%d", s.Warn, s.Fail)
	}
}
