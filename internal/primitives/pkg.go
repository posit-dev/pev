package primitives

import (
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/system"
)

func init() {
	checks.Register("pkg", runPkg, []string{"manager", "any_of", "all_of"})
}

// runPkg checks for installed distro packages via dpkg or rpm. `manager: auto`
// (default) probes whichever is on PATH first; `any_of` PASSes if any package
// is installed; `all_of` PASSes only if every named package is installed.
func runPkg(rc checks.RunCtx) checks.Result {
	mgr, _ := getString(rc.Check.With, "manager")
	if mgr == "" || mgr == "auto" {
		switch {
		case system.CommandExists("dpkg-query"):
			mgr = "dpkg"
		case system.CommandExists("rpm"):
			mgr = "rpm"
		default:
			return unknownf(rc.Check, "no package manager (dpkg/rpm) on PATH")
		}
	}

	any, _ := getStringSlice(rc.Check.With, "any_of")
	all, _ := getStringSlice(rc.Check.With, "all_of")
	if len(any) == 0 && len(all) == 0 {
		return unknownf(rc.Check, "must set any_of or all_of")
	}

	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
	}

	check := func(name string) bool {
		var cmd string
		switch mgr {
		case "dpkg":
			cmd = "dpkg-query -W -f='${Status}\n' " + name + " 2>/dev/null"
		case "rpm":
			cmd = "rpm -q " + name + " >/dev/null 2>&1 && echo installed"
		}
		out, err := system.RunCaptured(rc.Ctx, cmd, 10*time.Second)
		if err != nil {
			return false
		}
		s := strings.TrimSpace(out.Stdout)
		switch mgr {
		case "dpkg":
			return strings.Contains(s, "install ok installed")
		case "rpm":
			return s == "installed"
		}
		return false
	}

	if len(any) > 0 {
		for _, p := range any {
			if check(p) {
				r.Status = checks.StatusPass
				r.Reason = p + " is installed"
				return r
			}
		}
		r.Status = checks.StatusFail
		switch len(any) {
		case 1:
			r.Reason = any[0] + " is not installed"
		default:
			// Multi-package any_of (cross-distro fallback like
			// libssl-dev|openssl-devel): name every candidate and
			// surface that none of them are installed.
			r.Reason = "none of " + strings.Join(any, ", ") + " are installed"
		}
		return r
	}
	missing := []string{}
	for _, p := range all {
		if !check(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		r.Status = checks.StatusFail
		r.Reason = "missing: " + strings.Join(missing, ",")
		return r
	}
	r.Status = checks.StatusPass
	return r
}
