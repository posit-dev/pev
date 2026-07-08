package primitives

import (
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/system"
)

func init() {
	checks.Register("cmd", runCmd, []string{
		"cmd", "expect_exit", "expect_stdout_regex", "expect_stderr_regex",
		"timeout_seconds", "as_user", "skip_exit", "warn_exit",
	})
}

// asUserWrap rewrites cmd so it runs as user u. Returns "" for the rewritten
// command when the run can't be performed safely (e.g. running as a
// different non-root user), in which case the caller should SKIP.
func asUserWrap(cmd, u string) (string, bool) {
	if u == "" {
		return cmd, true
	}
	current, err := user.Current()
	if err != nil {
		return cmd, true
	}
	if current.Username == u || current.Uid == u {
		return cmd, true
	}
	if os.Geteuid() != 0 {
		// Non-root, asked to impersonate someone else — can't.
		return "", false
	}
	// `runuser -u USER -- sh -c CMD` runs CMD with USER's environment and
	// PAM session. -- prevents argv leakage; sh -c lets the YAML keep
	// shell pipelines without us reinterpreting the body.
	quoted := strings.ReplaceAll(cmd, "'", `'\''`)
	return "runuser -u " + u + " -- sh -c '" + quoted + "'", true
}

// runCmd executes a shell command and matches its output. Useful when an SE
// would naturally type the same command at the terminal.
func runCmd(rc checks.RunCtx) checks.Result {
	cmd, ok := getString(rc.Check.With, "cmd")
	if !ok || cmd == "" {
		return unknownf(rc.Check, "missing required `cmd` field")
	}
	timeout := getTimeout(rc.Check.With, 30*time.Second)

	// `as_user` lets a check assert behavior for an unprivileged user
	// (e.g. `renv::install("renv")` succeeds without sudo). The
	// `unprivileged_user` input is set by the assess command; YAML
	// authors should write `as_user: "{{ .Inputs.unprivileged_user }}"`
	// so the user is consistent across checks.
	asUser, _ := getString(rc.Check.With, "as_user")
	wrapped, canRun := asUserWrap(cmd, asUser)
	if !canRun {
		return checks.Result{
			ID: rc.Check.ID, Title: rc.Check.Title,
			Status: checks.StatusSkip,
			Reason: "cannot impersonate " + asUser + " from a non-root user",
		}
	}
	cmd = wrapped

	res, err := system.RunCaptured(rc.Ctx, cmd, timeout)
	if err != nil {
		return unknownf(rc.Check, "command did not start: %v", err)
	}

	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{
			Command: cmd, Stdout: truncate(res.Stdout, 4096),
			Stderr: truncate(res.Stderr, 1024),
			Note:   noteFor(res),
		}},
	}

	if res.TimedOut {
		r.Status = checks.StatusFail
		r.Reason = "command timed out"
		return r
	}

	// `skip_exit` lets a YAML author signal "this check is not applicable
	// on this host" from inside the script (e.g. "no apt lists present
	// yet"). Matching exit code → SKIP, with the script's stdout becoming
	// the human-readable reason. Checked before expect_exit so a script
	// can use a single exit code to mean either pass or skip.
	if skip, ok := getInt(rc.Check.With, "skip_exit"); ok && res.ExitCode == skip {
		r.Status = checks.StatusSkip
		reason := firstNonBlankLine(res.Stdout)
		if reason == "" {
			reason = firstNonBlankLine(res.Stderr)
		}
		if reason == "" {
			reason = fmt.Sprintf("script signalled skip (exit %d)", skip)
		}
		r.Reason = reason
		return r
	}

	// `warn_exit` is the advisory sibling of skip_exit: a matching exit code
	// resolves the check to WARN (visible, not exit-fatal) rather than FAIL.
	// Lets a YAML author say "the host is installable as-is, but note this"
	// from inside the script — e.g. a recommended tool (uv) is absent so a
	// fallback path will be used. The script's last echoed line becomes the
	// advisory reason, matching the FAIL convention. Checked before
	// expect_exit so a single non-zero code can mean WARN instead of FAIL.
	if warn, ok := getInt(rc.Check.With, "warn_exit"); ok && res.ExitCode == warn {
		r.Status = checks.StatusWarn
		r.Reason = failReason(res, fmt.Sprintf("script signalled warning (exit %d)", warn))
		return r
	}

	if want, ok := getInt(rc.Check.With, "expect_exit"); ok && res.ExitCode != want {
		r.Status = checks.StatusFail
		r.Reason = failReason(res, fmt.Sprintf("command failed (exit %d)", res.ExitCode))
		return r
	}
	if pat, ok := getString(rc.Check.With, "expect_stdout_regex"); ok && pat != "" {
		re, err := compileMultiline(pat)
		if err != nil {
			return unknownf(rc.Check, "invalid expect_stdout_regex: %v", err)
		}
		if !re.MatchString(res.Stdout) {
			r.Status = checks.StatusFail
			r.Reason = failReason(res, "command output did not match the expected pattern")
			return r
		}
	}
	if pat, ok := getString(rc.Check.With, "expect_stderr_regex"); ok && pat != "" {
		re, err := compileMultiline(pat)
		if err != nil {
			return unknownf(rc.Check, "invalid expect_stderr_regex: %v", err)
		}
		if !re.MatchString(res.Stderr) {
			r.Status = checks.StatusFail
			r.Reason = failReason(res, "command stderr did not match the expected pattern")
			return r
		}
	}

	r.Status = checks.StatusPass
	return r
}

// failReason builds a customer-readable reason for a failed cmd-primitive
// run. The script's last echoed line is the most reliable diagnostic — by
// convention every cmd: script in the catalog prints its problem
// description as the final line of stdout (or stderr) before exit 1. When
// the script said something useful, we promote that line as the reason
// and bury the exit-code arithmetic in a parenthetical. When the script
// went silent we fall back to the supplied default (e.g. "command failed
// (exit 1)") so the SE at least sees a non-zero status.
func failReason(res system.CommandResult, fallback string) string {
	out := lastNonBlankLine(res.Stdout)
	if out == "" {
		out = lastNonBlankLine(res.Stderr)
	}
	if out == "" {
		return fallback
	}
	if len(out) > 240 {
		out = out[:240] + "…"
	}
	if res.ExitCode != 0 {
		return fmt.Sprintf("%s (exit %d)", out, res.ExitCode)
	}
	return out
}

// lastNonBlankLine returns the trailing non-blank line of s, trimmed of
// leading/trailing whitespace. Catalog scripts conventionally end with
// the diagnostic line (e.g. "default locale is not UTF-8"); reading from
// the bottom up surfaces that instead of an unrelated info line printed
// earlier in the run.
func lastNonBlankLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

// firstNonBlankLine returns the leading non-blank line of s, trimmed.
// Used by the skip_exit path: scripts that opt into a SKIP exit code
// conventionally print the skip rationale as the first line of stdout.
func firstNonBlankLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…(truncated)"
}

func noteFor(r system.CommandResult) string {
	if r.TimedOut {
		return "timed out"
	}
	return ""
}

// compileMultiline parses pat with the multi-line flag forced on so `^` /
// `$` anchors match line boundaries by default — the behavior YAML authors
// expect (`uname -m` outputs `x86_64\n`, and `^x86_64$` should match).
// Patterns may opt out by passing their own `(?-m)`.
func compileMultiline(pat string) (*regexp.Regexp, error) {
	if !strings.HasPrefix(pat, "(?") {
		pat = "(?m)" + pat
	}
	return regexp.Compile(pat)
}
