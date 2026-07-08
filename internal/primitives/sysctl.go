package primitives

import (
	"os"
	"strconv"
	"strings"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("sysctl", runSysctl, []string{"key", "expect_int_min", "expect_equals"})
}

// runSysctl reads /proc/sys/<dotted.key> and compares its value.
func runSysctl(rc checks.RunCtx) checks.Result {
	key, ok := getString(rc.Check.With, "key")
	if !ok || key == "" {
		return unknownf(rc.Check, "missing required `key` field")
	}
	path := "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
	data, err := os.ReadFile(path)
	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{Path: path}},
	}
	if err != nil {
		r.Status = checks.StatusUnknown
		r.Reason = "read: " + err.Error()
		return r
	}
	got := strings.TrimSpace(string(data))

	if want, ok := getString(rc.Check.With, "expect_equals"); ok && want != "" {
		if got != want {
			r.Status = checks.StatusFail
			r.Reason = key + "=" + got + ", want " + want
			return r
		}
	}
	if min, ok := getInt(rc.Check.With, "expect_int_min"); ok {
		n, err := strconv.Atoi(got)
		if err != nil {
			return unknownf(rc.Check, "value %q is not an int", got)
		}
		if n < min {
			r.Status = checks.StatusFail
			r.Reason = key + "=" + got + ", want >= " + strconv.Itoa(min)
			return r
		}
	}

	r.Status = checks.StatusPass
	return r
}
