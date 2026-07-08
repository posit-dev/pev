package checks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/posit-dev/pev/internal/discover"
	"github.com/posit-dev/pev/internal/system"
)

// Engine runs a list of Checks against a HostFacts snapshot and a set of inputs.
type Engine struct {
	Facts  discover.HostFacts
	Inputs map[string]string

	// Progress, when non-nil, receives one line per check as it runs.
	// Format: "[i/N] <short_description> (<id>)" — or "[i/N] <id>" when
	// the catalog entry hasn't supplied a short_description. Useful for
	// stderr output during long-running primitives (apt update, uv venv,
	// renv install) so the SE knows pev hasn't hung. Final per-check
	// status is NOT emitted — that lives in the report.
	Progress io.Writer

	// ProgressColor enables ANSI color on the progress stream — set by the
	// caller from a TTY/NO_COLOR check on the Progress writer. When true the
	// "(skipped)" suffix is rendered yellow so skips stand out from the
	// checks that actually ran.
	ProgressColor bool
}

// ANSI codes for the progress stream. Kept local to this package because
// internal/report imports internal/checks (not the reverse), so we cannot
// reach for report's color helpers without an import cycle.
const (
	progressYellow = "\033[33m"
	progressReset  = "\033[0m"
)

// Run filters checks by AppliesTo, gates root-only checks, expands templates
// in `with:`, dispatches to the registered primitive, and returns Results
// sorted by ID.
func (e *Engine) Run(ctx context.Context, all []Check) []Result {
	out := make([]Result, 0, len(all))
	for i, c := range all {
		// Emit the "[i/N] label" prefix BEFORE running so the SE sees
		// something happening during long-running primitives (apt
		// update, dnf makecache, uv venv, renv install) and knows pev
		// hasn't hung. The line is left open (no newline) and closed
		// after the check resolves.
		if e.Progress != nil {
			label := c.ID
			if c.ShortDescription != "" {
				label = fmt.Sprintf("%s (%s)", c.ShortDescription, c.ID)
			}
			fmt.Fprintf(e.Progress, "[%d/%d] %s", i+1, len(all), label)
		}
		res := e.runOne(ctx, c)
		// Close the line from the check's ACTUAL outcome — the only
		// source of truth. A "(skipped)" suffix here therefore always
		// matches the report's SKIP tally, whether the skip came from a
		// static gate (applies_to/requires/requires_root), a declined
		// prompt (an input templated empty), or a runtime self-skip
		// (a cmd script's skip_exit). The previous design predicted the
		// skip from host facts alone, which silently disagreed with the
		// summary for input- and runtime-gated skips.
		if e.Progress != nil {
			if res.Status == StatusSkip {
				suffix := " (skipped)"
				if e.ProgressColor {
					suffix = " " + progressYellow + "(skipped)" + progressReset
				}
				fmt.Fprint(e.Progress, suffix+"\n")
			} else {
				fmt.Fprint(e.Progress, "\n")
			}
		}
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (e *Engine) runOne(ctx context.Context, c Check) Result {
	start := time.Now()
	base := Result{
		ID: c.ID, Title: c.Title,
		Category: CategoryFor(c),
		Why:      c.Why, References: c.References,
	}
	finish := func(r Result) Result {
		r.DurationMS = time.Since(start).Milliseconds()
		return r
	}

	if reason := appliesTo(c.AppliesTo, e.Facts); reason != "" {
		base.Status = StatusSkip
		base.Reason = reason
		return finish(base)
	}
	if missing := missingRequires(c.AppliesTo.Requires, e.Facts); missing != "" {
		base.Status = StatusSkip
		base.Reason = missingRequiresReason(missing)
		return finish(base)
	}
	if c.RequiresRoot && !system.IsRoot() {
		base.Status = StatusSkip
		base.Reason = "requires root; rerun with sudo to evaluate"
		return finish(base)
	}

	expanded, err := expandWith(c.With, e.Facts, e.Inputs)
	if err != nil {
		base.Status = StatusSkip
		base.Reason = "missing or invalid input: " + err.Error()
		return finish(base)
	}
	c.With = expanded

	runner, err := Lookup(c.Primitive)
	if err != nil {
		base.Status = StatusUnknown
		base.Reason = err.Error()
		return finish(base)
	}

	r := runner(RunCtx{
		Ctx: ctx, Check: c, Facts: e.Facts, Inputs: e.Inputs,
	})
	r.ID = c.ID
	r.Title = c.Title
	r.Category = CategoryFor(c)
	r.Why = c.Why
	r.References = c.References
	if r.Status == StatusFail || r.Status == StatusUnknown {
		r.Remediation = c.Remediation
	}
	return finish(r)
}

// appliesTo matches a Check's AppliesTo gate against host facts. An empty
// list for a dimension is "any" — only non-empty lists are restrictive.
// Returns "" when the check applies, or a specific, SE-facing skip reason
// naming the dimension, the host's actual value, and what the check
// requires — so `--review-skipped` shows "this host is ubuntu-26.04; check
// targets rhel-8, rhel-9, rhel-10" rather than a bare "does not apply".
//
// Product gating is intentionally absent here: it lives in Filter.Apply
// (internal/checks/filter.go), which the assess command runs before
// handing the surviving checks to the engine.
func appliesTo(a AppliesTo, hf discover.HostFacts) string {
	if len(a.OS) > 0 && !contains(a.OS, hf.OS) {
		return fmt.Sprintf("this host is %s; check targets %s",
			orUnknown(hf.OS), strings.Join(a.OS, ", "))
	}
	if len(a.Arch) > 0 && !contains(a.Arch, hf.Arch) {
		return fmt.Sprintf("this host is %s; check targets %s",
			orUnknown(hf.Arch), strings.Join(a.Arch, ", "))
	}
	return ""
}

// orUnknown labels an empty fact value so a skip reason never reads
// "this host is ; check targets ...".
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func contains(set []string, s string) bool {
	for _, e := range set {
		if e == s {
			return true
		}
	}
	return false
}

// missingRequiresReason renders the SE-facing skip reason for an unmet
// `requires` token. The command-gated tokens (uv, pip, apt, dnf) are
// detected with a bare $PATH lookup (exec.LookPath), so a tool that exists
// only under a versioned prefix like /opt/python/<ver>/bin is invisible to
// the gate — and to the user who types `pip`. Spell that out so the
// --review-skipped reader knows the fix is "put it on PATH", not "it isn't
// installed anywhere". The scanned tokens (r, python, quarto) come from an
// /opt/<tool> scan, not PATH, so they keep the generic wording.
func missingRequiresReason(token string) string {
	switch token {
	case "uv", "pip", "apt", "dnf":
		return fmt.Sprintf("%s is not on PATH; pev probes for it with a bare command lookup, "+
			"so it must be on PATH (an interpreter under /opt/python/<ver>/bin is not enough) for pev to test it", token)
	default:
		return "missing required tooling: " + token
	}
}

// missingRequires returns the first required-fact token that the host
// doesn't satisfy, or "" when every requirement is met. Unknown tokens
// are treated as a satisfied fact so adding a new token to a YAML pack
// doesn't blanket-SKIP the catalog on older pev binaries.
func missingRequires(requires []string, hf discover.HostFacts) string {
	for _, req := range requires {
		switch req {
		case "r":
			if len(hf.R) == 0 {
				return req
			}
		case "python":
			if len(hf.Python) == 0 {
				return req
			}
		case "quarto":
			if len(hf.Quarto) == 0 {
				return req
			}
		case "uv":
			if !hf.HasUV {
				return req
			}
		case "pip":
			if !hf.HasPip {
				return req
			}
		case "apt":
			if !hf.HasApt {
				return req
			}
		case "dnf":
			if !hf.HasDNF {
				return req
			}
		}
	}
	return ""
}

// expandWith renders any string-typed value in the `with:` payload through
// text/template against {{ .Facts }} and {{ .Inputs.X }}. Missing keys cause
// an error so the engine can surface them as a SKIP with a clear reason.
func expandWith(with map[string]interface{}, facts discover.HostFacts, inputs map[string]string) (map[string]interface{}, error) {
	if len(with) == 0 {
		return with, nil
	}
	out := make(map[string]interface{}, len(with))
	data := struct {
		Facts  discover.HostFacts
		Inputs map[string]string
	}{Facts: facts, Inputs: inputs}

	var walk func(v interface{}) (interface{}, error)
	walk = func(v interface{}) (interface{}, error) {
		switch x := v.(type) {
		case string:
			if !looksLikeTemplate(x) {
				return x, nil
			}
			t, err := template.New("with").Option("missingkey=error").Parse(x)
			if err != nil {
				return nil, fmt.Errorf("template parse: %w", err)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return nil, fmt.Errorf("template execute: %w", err)
			}
			return buf.String(), nil
		case map[string]interface{}:
			m := make(map[string]interface{}, len(x))
			for k, vv := range x {
				nv, err := walk(vv)
				if err != nil {
					return nil, err
				}
				m[k] = nv
			}
			return m, nil
		case []interface{}:
			s := make([]interface{}, len(x))
			for i, vv := range x {
				nv, err := walk(vv)
				if err != nil {
					return nil, err
				}
				s[i] = nv
			}
			return s, nil
		}
		return v, nil
	}

	for k, v := range with {
		nv, err := walk(v)
		if err != nil {
			return nil, err
		}
		out[k] = nv
	}
	return out, nil
}

func looksLikeTemplate(s string) bool {
	return bytes.Contains([]byte(s), []byte("{{"))
}
