// Package report renders pev's checks.Report into the on-disk artifacts
// (Markdown + JSON) and computes diffs between two prior runs.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/posit-dev/pev/internal/checks"
)

// WriteJSON writes the report as pretty-printed JSON to outDir/<base>.json.
// Returns the path written.
func WriteJSON(outDir, base string, rep checks.Report) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	// Stable result ordering for diff-friendly output.
	sort.Slice(rep.Results, func(i, j int) bool { return rep.Results[i].ID < rep.Results[j].ID })
	path := filepath.Join(outDir, base+".json")
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", err
	}
	// Reports are intentionally world-readable; see comment in markdown.go.
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // G306: see comment above
		return "", err
	}
	return path, nil
}

// ReadJSON loads a previously-written JSON report.
func ReadJSON(path string) (checks.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return checks.Report{}, err
	}
	var rep checks.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return checks.Report{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return rep, nil
}

// Summarize tallies a Result list into a Summary.
func Summarize(results []checks.Result) checks.Summary {
	s := checks.Summary{Total: len(results)}
	for _, r := range results {
		switch r.Status {
		case checks.StatusPass:
			s.Pass++
		case checks.StatusWarn:
			s.Warn++
		case checks.StatusFail:
			s.Fail++
		case checks.StatusSkip:
			s.Skip++
		case checks.StatusUnknown:
			s.Unknown++
		}
	}
	return s
}
