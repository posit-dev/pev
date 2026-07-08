package primitives

import (
	"os"
	"regexp"
	"strconv"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("file", runFile, []string{
		"path", "must_exist", "mode_max", "contains_regex",
	})
}

// runFile checks a single file's existence, mode (no more permissive than
// `mode_max`), and optionally that its contents match a regex.
func runFile(rc checks.RunCtx) checks.Result {
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
				r.Reason = "file does not exist"
				return r
			}
			r.Status = checks.StatusPass
			r.Reason = "file absent (must_exist=false)"
			return r
		}
		r.Status = checks.StatusUnknown
		r.Reason = "stat: " + err.Error()
		return r
	}

	if maxStr, ok := getString(rc.Check.With, "mode_max"); ok && maxStr != "" {
		maxMode, err := strconv.ParseUint(maxStr, 8, 32)
		if err != nil {
			return unknownf(rc.Check, "invalid mode_max %q: %v", maxStr, err)
		}
		actual := uint32(st.Mode().Perm())
		if actual&^uint32(maxMode) != 0 {
			r.Status = checks.StatusFail
			r.Reason = "mode " + os.FileMode(actual).String() + " is more permissive than allowed"
			return r
		}
	}

	if pat, ok := getString(rc.Check.With, "contains_regex"); ok && pat != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			r.Status = checks.StatusUnknown
			r.Reason = "read: " + err.Error()
			return r
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return unknownf(rc.Check, "invalid contains_regex: %v", err)
		}
		if !re.Match(data) {
			r.Status = checks.StatusFail
			r.Reason = "file contents did not match contains_regex"
			return r
		}
	}

	r.Status = checks.StatusPass
	return r
}
