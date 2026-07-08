package primitives

import (
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/system"
)

func init() {
	checks.Register("selinux", runSELinux, []string{"expect", "timeout_seconds"})
	checks.Register("apparmor", runAppArmor, []string{"expect", "timeout_seconds"})
}

// runSELinux reports SELinux status by parsing `getenforce` (preferred) or
// `sestatus`. `expect`: any|disabled|permissive|enforcing.
func runSELinux(rc checks.RunCtx) checks.Result {
	timeout := getTimeout(rc.Check.With, 5*time.Second)
	expect := "any"
	if e, ok := getString(rc.Check.With, "expect"); ok && e != "" {
		expect = strings.ToLower(e)
	}

	cmd := "getenforce"
	out, err := system.RunCaptured(rc.Ctx, cmd, timeout)
	mode := "absent"
	if err == nil && out.ExitCode == 0 {
		mode = strings.ToLower(strings.TrimSpace(out.Stdout))
	}

	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{Command: cmd, Stdout: truncate(out.Stdout, 256), Note: "selinux mode=" + mode}},
	}

	matches := func(want string) bool {
		switch want {
		case "any":
			return mode == "enforcing" || mode == "permissive" || mode == "disabled"
		case "absent":
			return mode == "absent"
		case "not_enforcing":
			// PASS when SELinux is not actively confining the host.
			// `absent` (no kernel module / non-RHEL) also counts as
			// not_enforcing — applies_to filters keep this check on RHEL only.
			return mode == "permissive" || mode == "disabled" || mode == "absent"
		default:
			return mode == want
		}
	}
	if matches(expect) {
		r.Status = checks.StatusPass
		r.Reason = "selinux mode=" + mode
	} else {
		r.Status = checks.StatusFail
		r.Reason = "selinux mode=" + mode + " (expected " + expect + ")"
	}
	return r
}

// runAppArmor reports AppArmor status. `expect`: any|enabled|disabled|absent|not_enabled.
func runAppArmor(rc checks.RunCtx) checks.Result {
	timeout := getTimeout(rc.Check.With, 5*time.Second)
	expect := "any"
	if e, ok := getString(rc.Check.With, "expect"); ok && e != "" {
		expect = strings.ToLower(e)
	}

	// /sys/module/apparmor/parameters/enabled is "Y" when enabled, "N" otherwise;
	// absent altogether means AppArmor isn't compiled into the running kernel.
	cmd := "cat /sys/module/apparmor/parameters/enabled 2>/dev/null"
	out, _ := system.RunCaptured(rc.Ctx, cmd, timeout)
	mode := "absent"
	switch strings.TrimSpace(out.Stdout) {
	case "Y":
		mode = "enabled"
	case "N":
		mode = "disabled"
	}

	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{Command: cmd, Stdout: truncate(out.Stdout, 64), Note: "apparmor mode=" + mode}},
	}

	matches := func(want string) bool {
		switch want {
		case "any":
			return mode == "enabled" || mode == "disabled" || mode == "absent"
		case "not_enabled":
			return mode == "disabled" || mode == "absent"
		default:
			return mode == want
		}
	}
	if matches(expect) {
		r.Status = checks.StatusPass
		r.Reason = "apparmor " + mode
	} else {
		r.Status = checks.StatusFail
		r.Reason = "apparmor " + mode + " (expected " + expect + ")"
	}
	return r
}
