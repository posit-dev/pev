package primitives

import (
	"fmt"
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/system"
)

func init() {
	checks.Register("systemd", runSystemd, []string{
		"unit", "expect", "timeout_seconds",
	})
}

// runSystemd reports whether a systemd unit is present and active. `expect`
// values:
//
//	"installed"  — unit file is present (active or inactive both PASS)
//	"active"     — unit is running (default)
//	"inactive"   — unit is loaded but not running (rare; mostly used to assert
//	              firewalls are NOT active)
//	"absent"     — unit file is not present
func runSystemd(rc checks.RunCtx) checks.Result {
	unit, ok := getString(rc.Check.With, "unit")
	if !ok || unit == "" {
		return unknownf(rc.Check, "missing required `unit` field")
	}
	expect := "active"
	if e, ok := getString(rc.Check.With, "expect"); ok && e != "" {
		expect = e
	}
	timeout := getTimeout(rc.Check.With, 5*time.Second)

	r := checks.Result{ID: rc.Check.ID, Title: rc.Check.Title}

	listCmd := fmt.Sprintf("systemctl list-unit-files %q --no-legend --no-pager", unit+".service")
	listed, err := system.RunCaptured(rc.Ctx, listCmd, timeout)
	if err != nil {
		return unknownf(rc.Check, "systemctl not found: %v", err)
	}
	installed := strings.Contains(listed.Stdout, unit+".service")

	stateCmd := fmt.Sprintf("systemctl is-active %s", unit)
	state, _ := system.RunCaptured(rc.Ctx, stateCmd, timeout)
	active := strings.TrimSpace(state.Stdout) == "active"

	r.Evidence = []checks.Evidence{{
		Command: stateCmd,
		Stdout:  truncate(state.Stdout, 256),
		Note:    fmt.Sprintf("installed=%v active=%v", installed, active),
	}}

	switch expect {
	case "installed":
		if installed {
			r.Status = checks.StatusPass
			r.Reason = "unit present"
		} else {
			r.Status = checks.StatusFail
			r.Reason = "unit not found by systemctl"
		}
	case "active":
		switch {
		case !installed:
			r.Status = checks.StatusFail
			r.Reason = "unit not installed"
		case active:
			r.Status = checks.StatusPass
			r.Reason = "active"
		default:
			r.Status = checks.StatusFail
			r.Reason = "installed but not active"
		}
	case "inactive":
		switch {
		case !installed:
			r.Status = checks.StatusPass
			r.Reason = "unit not installed (counted as not active)"
		case !active:
			r.Status = checks.StatusPass
			r.Reason = "installed but inactive"
		default:
			r.Status = checks.StatusFail
			r.Reason = "unit is active"
		}
	case "absent":
		if installed {
			r.Status = checks.StatusFail
			r.Reason = "unit unexpectedly installed"
		} else {
			r.Status = checks.StatusPass
			r.Reason = "absent"
		}
	default:
		return unknownf(rc.Check, "unknown expect %q (want installed|active|inactive|absent)", expect)
	}
	return r
}
