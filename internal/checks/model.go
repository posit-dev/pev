// Package checks defines the data model, registry, loader, and engine for
// pev's check catalog. Checks themselves live in YAML packs under /checks
// (embedded at build time) and may be supplemented by user-supplied YAML.
package checks

import (
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/discover"
)

// SchemaVersion is the major version of the pev report and YAML schemas.
// Bumped only on breaking changes; pev diff rejects mismatched majors.
//
// v2 (2026-06): dropped Severity. Every FAIL is treated as worth
// investigating; the catalog no longer tries to predict which failures
// will block an install.
//
// v3 (2026-06): added the WARN tier (StatusWarn / Summary.Warn). This is a
// deliberate, narrow partial reversal of v2's "no severity tiers" stance —
// not a return to graduated severities. The model is now binary plus one
// advisory rung:
//
//   - FAIL — the install will likely fail, break, or be unsupported if this
//     is not fixed. The default for any prerequisite. Trips a non-zero exit.
//   - WARN — the host can be installed on as-is, but the SE should note this:
//     a non-standard-but-valid layout, a soft recommendation not met, a
//     condition that matters only for some deployment shapes. Visible on
//     screen (yellow), but NOT counted against the run and NOT exit-fatal.
//
// WARN is not "FAIL but I'm not sure" — an undecidable check is UNKNOWN, and
// an inapplicable one is SKIP. Authors: when in doubt, use FAIL. See
// docs/check-authoring.md for the full guidance. The key set changed
// (summary.warn is new), so a v3 binary cannot diff against a v2 report.
const SchemaVersion = 3

// Status is the outcome of running a check. On a severity ladder, WARN sits
// strictly between PASS and FAIL: worth surfacing, not disqualifying.
type Status string

const (
	StatusPass    Status = "pass"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusSkip    Status = "skip"
	StatusUnknown Status = "unknown"
)

// AppliesTo gates a check by host facts.
type AppliesTo struct {
	OS       []string `yaml:"os" json:"os,omitempty"`
	Products []string `yaml:"products" json:"products,omitempty"`
	Arch     []string `yaml:"arch" json:"arch,omitempty"`

	// Requires lists tooling/language facts that must be true before the
	// check runs. Tokens (any of):
	//   r        — at least one /opt/R/<ver>/bin/R found
	//   python   — at least one /opt/python/<ver>/bin/python found
	//   quarto   — at least one quarto binary found
	//   uv       — `uv` on PATH
	//   pip      — `pip` or `pip3` on PATH
	//   apt      — `apt-get` on PATH (Debian-family)
	//   dnf      — `dnf` on PATH (RHEL-family)
	// Missing facts SKIP the check with reason "missing required tooling: X".
	Requires []string `yaml:"requires" json:"requires,omitempty"`
}

// Check is one entry in the catalog.
type Check struct {
	ID    string `yaml:"id" json:"id"`
	Title string `yaml:"title" json:"title"`
	// ShortDescription is the friendly label shown in the engine's
	// per-check progress line (e.g. "[12/65] Open-file limit (os.ulimit.nofile)").
	// Keep it human-readable, present-tense, and a handful of words
	// long; admins watching a hung run should be able to tell at a
	// glance which check is in flight without decoding the dotted ID.
	// When empty the engine falls back to the ID alone.
	ShortDescription string                 `yaml:"short_description" json:"short_description,omitempty"`
	Category         string                 `yaml:"category" json:"category,omitempty"`
	Tags             []string               `yaml:"tags" json:"tags,omitempty"`
	AppliesTo        AppliesTo              `yaml:"applies_to" json:"applies_to,omitempty"`
	RequiresRoot     bool                   `yaml:"requires_root" json:"requires_root,omitempty"`
	Why              string                 `yaml:"why" json:"why,omitempty"`
	Remediation      string                 `yaml:"remediation" json:"remediation,omitempty"`
	References       []string               `yaml:"references" json:"references,omitempty"`
	Primitive        string                 `yaml:"primitive" json:"primitive"`
	With             map[string]interface{} `yaml:"with" json:"with,omitempty"`
	Source           string                 `yaml:"-" json:"-"` // pack origin, for diagnostics
}

// Categories used for grouping in reports. Free-form strings are accepted in
// YAML; these are the canonical set the renderer orders explicitly.
const (
	CategoryNetworking      = "Networking"
	CategoryStorage         = "Storage"
	CategoryOperatingSystem = "Operating System"
	CategorySecurity        = "Security"
	CategoryIdentity        = "Identity"
	CategorySSL             = "SSL/TLS"
	CategoryPackages        = "Packages"
	CategorySizing          = "Sizing"
	CategoryProduct         = "Product"
	CategoryOther           = "Other"
)

// CategoryFor returns c.Category if set; otherwise derives a category from
// tags, then from the id-prefix, falling back to "Other". Pure function so
// tests and the renderer share the same logic.
func CategoryFor(c Check) string {
	if c.Category != "" {
		return c.Category
	}
	for _, t := range c.Tags {
		switch t {
		case "egress", "network", "dns", "port":
			return CategoryNetworking
		case "ssl", "tls":
			return CategorySSL
		case "packages", "package":
			return CategoryPackages
		case "sizing":
			return CategorySizing
		case "idp", "identity", "auth":
			return CategoryIdentity
		case "selinux", "apparmor", "firewall", "security":
			return CategorySecurity
		case "storage", "disk":
			return CategoryStorage
		case "os", "arch":
			return CategoryOperatingSystem
		}
	}
	switch {
	case strings.HasPrefix(c.ID, "net."):
		return CategoryNetworking
	case strings.HasPrefix(c.ID, "os."):
		return CategoryOperatingSystem
	case strings.HasPrefix(c.ID, "pkg."):
		return CategoryPackages
	case strings.HasPrefix(c.ID, "sizing."):
		return CategorySizing
	case strings.HasPrefix(c.ID, "sec."):
		return CategorySecurity
	case strings.HasSuffix(c.ID, ".ssl.cert-key-match"), strings.Contains(c.ID, ".ssl."):
		return CategorySSL
	case strings.HasPrefix(c.ID, "workbench."), strings.HasPrefix(c.ID, "connect."), strings.HasPrefix(c.ID, "ppm."):
		return CategoryProduct
	}
	return CategoryOther
}

// Pack is one YAML file's contents.
type Pack struct {
	SchemaVersion int     `yaml:"schema_version"`
	Checks        []Check `yaml:"checks"`
}

// Evidence is a single piece of supporting output for a Result.
type Evidence struct {
	Command string `json:"command,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
	Path    string `json:"path,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Result is what one check produced.
type Result struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Category    string     `json:"category,omitempty"`
	Status      Status     `json:"status"`
	Why         string     `json:"why,omitempty"`
	Reason      string     `json:"reason,omitempty"`      // short pass/fail/skip explanation
	Remediation string     `json:"remediation,omitempty"` // copied from Check.Remediation when status=fail
	Evidence    []Evidence `json:"evidence,omitempty"`
	References  []string   `json:"references,omitempty"`
	DurationMS  int64      `json:"duration_ms"`
}

// Summary tallies a Report's outcomes.
type Summary struct {
	Total   int `json:"total"`
	Pass    int `json:"pass"`
	Warn    int `json:"warn"`
	Fail    int `json:"fail"`
	Skip    int `json:"skip"`
	Unknown int `json:"unknown"`
}

// Run records the assessment's input parameters.
type Run struct {
	Products []string          `json:"products"`
	Profile  string            `json:"profile,omitempty"`
	Inputs   map[string]string `json:"inputs,omitempty"`
}

// Report is the on-disk artifact emitted by `pev assess` (and rendered to Markdown).
type Report struct {
	PevVersion    string             `json:"pev_version"`
	SchemaVersion int                `json:"schema_version"`
	Host          discover.HostFacts `json:"host"`
	Run           Run                `json:"run"`
	StartedAt     time.Time          `json:"started_at"`
	FinishedAt    time.Time          `json:"finished_at"`
	Summary       Summary            `json:"summary"`
	Results       []Result           `json:"results"`
}
