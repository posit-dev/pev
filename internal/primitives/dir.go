package primitives

import (
	"os"
	"path/filepath"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("dir", runDir, []string{
		"path", "must_exist", "glob", "glob_min_matches",
	})
}

// runDir checks a directory for existence and an optional glob match count.
func runDir(rc checks.RunCtx) checks.Result {
	path, ok := getString(rc.Check.With, "path")
	if !ok || path == "" {
		return unknownf(rc.Check, "missing required `path` field")
	}
	mustExist := true
	if v, ok := getBool(rc.Check.With, "must_exist"); ok {
		mustExist = v
	}

	st, err := os.Stat(path)
	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{Path: path}},
	}
	if err != nil {
		if os.IsNotExist(err) {
			if mustExist {
				r.Status = checks.StatusFail
				r.Reason = "directory does not exist"
				return r
			}
			r.Status = checks.StatusPass
			r.Reason = "directory absent (must_exist=false)"
			return r
		}
		r.Status = checks.StatusUnknown
		r.Reason = "stat: " + err.Error()
		return r
	}
	if !st.IsDir() {
		r.Status = checks.StatusFail
		r.Reason = "path exists but is not a directory"
		return r
	}

	if g, ok := getString(rc.Check.With, "glob"); ok && g != "" {
		matches, err := filepath.Glob(filepath.Join(path, g))
		if err != nil {
			return unknownf(rc.Check, "invalid glob %q: %v", g, err)
		}
		min := 1
		if n, ok := getInt(rc.Check.With, "glob_min_matches"); ok {
			min = n
		}
		if len(matches) < min {
			r.Status = checks.StatusFail
			r.Reason = "glob matched fewer files than required"
			return r
		}
	}

	r.Status = checks.StatusPass
	return r
}
