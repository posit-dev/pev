package primitives

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func runRC(t *testing.T, c checks.Check, facts discover.HostFacts) checks.Result {
	t.Helper()
	rc := checks.RunCtx{Ctx: context.Background(), Check: c, Facts: facts}
	r, err := checks.Lookup(c.Primitive)
	if err != nil {
		t.Fatal(err)
	}
	return r(rc)
}

func TestCmdPrimitivePassFail(t *testing.T) {
	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "cmd",
		With: map[string]interface{}{"cmd": "true", "expect_exit": 0},
	}
	if r := runRC(t, pass, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("expected pass, got %s/%s", r.Status, r.Reason)
	}
	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "cmd",
		With: map[string]interface{}{"cmd": "false", "expect_exit": 0},
	}
	if r := runRC(t, fail, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("expected fail, got %s", r.Status)
	}
}

// TestCmdPrimitiveWarnExit proves warn_exit resolves a matching exit code to
// an advisory WARN (not FAIL), with the script's last line as the reason, and
// that a non-matching exit code still flows through expect_exit. This is the
// sibling of skip_exit, used by lang.python.venv-tooling to flag "no uv/pip on
// PATH" without failing an otherwise-installable host.
func TestCmdPrimitiveWarnExit(t *testing.T) {
	warn := checks.Check{
		ID: "x", Title: "x", Primitive: "cmd",
		With: map[string]interface{}{
			"cmd":         "echo 'neither uv nor pip on PATH'; exit 2",
			"expect_exit": 0, "warn_exit": 2,
		},
	}
	r := runRC(t, warn, discover.HostFacts{})
	if r.Status != checks.StatusWarn {
		t.Fatalf("warn_exit match should WARN, got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "neither uv nor pip") {
		t.Fatalf("WARN reason should carry the script's last line, got %q", r.Reason)
	}

	// exit 0 with warn_exit set still PASSes (the tool was found).
	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "cmd",
		With: map[string]interface{}{
			"cmd": "echo found; exit 0", "expect_exit": 0, "warn_exit": 2,
		},
	}
	if r := runRC(t, pass, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("exit 0 with warn_exit set should PASS, got %s/%s", r.Status, r.Reason)
	}

	// A non-matching, non-zero exit is neither warn nor the expected code →
	// FAIL, so warn_exit never masks a genuine failure.
	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "cmd",
		With: map[string]interface{}{
			"cmd": "echo broke; exit 1", "expect_exit": 0, "warn_exit": 2,
		},
	}
	if r := runRC(t, fail, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("exit 1 (not warn_exit, not expect_exit) should FAIL, got %s/%s", r.Status, r.Reason)
	}
}

func TestFilePrimitiveExistence(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "exists")
	if err := os.WriteFile(tmp, []byte("ssl-enabled=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "file",
		With: map[string]interface{}{"path": tmp, "must_exist": true, "contains_regex": "(?m)^ssl-enabled=1"},
	}
	if r := runRC(t, c, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("got %s/%s", r.Status, r.Reason)
	}

	missing := checks.Check{
		ID: "x", Title: "x", Primitive: "file",
		With: map[string]interface{}{"path": "/nonexistent/path"},
	}
	if r := runRC(t, missing, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("got %s/%s", r.Status, r.Reason)
	}
}

// TestPostgresSkipsWithoutHost locks in the contract that an unconfigured
// postgres input SKIPs (rather than UNKNOWN-ing) — the YAML wires the
// postgres_host input from the assess prompt, so empty host means "the
// SE chose not to point at an external Postgres". The dependent
// postgres.* checks should not trigger an investigation.
func TestPostgresSkipsWithoutHost(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "postgres",
		With: map[string]interface{}{"host": ""},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusSkip {
		t.Fatalf("postgres with empty host should SKIP, got %s/%s", r.Status, r.Reason)
	}
}

// TestPortSkipsWithEmptyHost is the SMTP-decline backstop: the connect SMTP
// check wires host from {{ .Inputs.connect_smtp_host }}, which is pre-seeded
// to "" and left empty when the SE declines the SMTP prompt. An empty host is
// a declined input, not a YAML bug, so the port primitive must SKIP — not
// UNKNOWN — mirroring x509's empty-cert_path and postgres's empty-host paths.
// Regression guard for Ralf's "[UNKN] connect.smtp.reachable" report.
func TestPortSkipsWithEmptyHost(t *testing.T) {
	c := checks.Check{
		ID: "connect.smtp.reachable", Title: "SMTP server reachable from Connect host",
		Primitive: "port",
		With:      map[string]interface{}{"host": "", "port": 587},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusSkip {
		t.Fatalf("port with empty host should SKIP, got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "empty") {
		t.Fatalf("SKIP reason should name the empty input, got %q", r.Reason)
	}
}

// TestPortUnknownWhenHostKeyAbsent keeps the other half of the contract: a
// genuinely missing `host` key is a catalog authoring bug and must stay
// UNKNOWN, not collapse into the declined-input SKIP above.
func TestPortUnknownWhenHostKeyAbsent(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "port",
		With: map[string]interface{}{"port": 587},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusUnknown {
		t.Fatalf("port with absent host key should UNKNOWN, got %s/%s", r.Status, r.Reason)
	}
}

// TestPostgresFailsOnUnreachable points at a deliberately closed port so
// the dial returns ECONNREFUSED quickly. We pick port 1 (rfc-reserved,
// listener-impossible-for-non-root) and a 1s timeout so the test runs
// fast even if the loopback stack momentarily stalls.
func TestPostgresFailsOnUnreachable(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "postgres",
		With: map[string]interface{}{
			"host": "127.0.0.1", "port": 1, "timeout_seconds": 1,
		},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusFail {
		t.Fatalf("postgres on closed port should FAIL, got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "tcp dial:") {
		t.Fatalf("want tcp-dial reason, got %q", r.Reason)
	}
}

// TestPkgRejectsEmptyAnyAndAllOf locks in the catalog-author guardrail
// that a `pkg` check must declare either any_of or all_of; neither
// supplied is a YAML authoring bug, surfaced as UNKNOWN.
func TestPkgRejectsEmptyAnyAndAllOf(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "pkg",
		With: map[string]interface{}{"manager": "dpkg"},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusUnknown {
		t.Fatalf("pkg without any_of/all_of should UNKNOWN, got %s/%s", r.Status, r.Reason)
	}
}

// TestPkgFailsOnUnknownPackage uses any_of with a name that cannot
// possibly exist; the check should FAIL with a "not installed" reason
// rather than UNKNOWN. Skipped on hosts that lack both dpkg and rpm —
// the primitive UNKNOWNs there with a "no package manager on PATH"
// reason, which is correct but not what this test exercises.
func TestPkgFailsOnUnknownPackage(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "pkg",
		With: map[string]interface{}{
			"any_of": []interface{}{"pev-deliberately-not-a-real-package"},
		},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status == checks.StatusUnknown && strings.Contains(r.Reason, "no package manager") {
		t.Skip("no dpkg/rpm on the test host")
	}
	if r.Status != checks.StatusFail {
		t.Fatalf("pkg with bogus package name should FAIL, got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "not installed") {
		t.Fatalf("want 'not installed' in reason, got %q", r.Reason)
	}
}

// TestSELinuxNotEnforcingTreatsAbsentAsPass locks in the contract that
// `expect: not_enforcing` accepts an `absent` host (no kernel module /
// non-RHEL). Without it, every Ubuntu host would fail any selinux
// check — the catalog gates on RHEL via applies_to, but the primitive
// itself must agree on the semantic.
func TestSELinuxNotEnforcingTreatsAbsentAsPass(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "selinux",
		With: map[string]interface{}{"expect": "not_enforcing", "timeout_seconds": 2},
	}
	r := runRC(t, c, discover.HostFacts{})
	// On a host that has SELinux enforcing the test will (correctly) FAIL
	// — that means selinux IS confining the host, which is the opposite
	// of "not_enforcing". We accept either PASS (no kernel module /
	// permissive / disabled) or skip out; we only fail the test if the
	// primitive returned UNKNOWN, which would mean the implementation
	// errored rather than reaching a verdict.
	if r.Status == checks.StatusUnknown {
		t.Fatalf("selinux returned UNKNOWN: %s", r.Reason)
	}
}

// TestAppArmorModePassesAny mirrors TestSELinuxModePassesAny for
// AppArmor's parallel set of expect values.
func TestAppArmorModePassesAny(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "apparmor",
		With: map[string]interface{}{"expect": "any", "timeout_seconds": 2},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusPass {
		t.Fatalf("apparmor with expect=any should PASS, got %s/%s", r.Status, r.Reason)
	}
}

// TestSystemdRejectsUnknownExpect catches the catalog-authoring nit
// where someone writes `expect: enabled` (a real systemctl word but not
// one this primitive understands) — surfaces it as UNKNOWN with a
// human-readable list of accepted values, rather than silently
// FAILing on every host.
func TestSystemdRejectsUnknownExpect(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "systemd",
		With: map[string]interface{}{"unit": "fake-unit", "expect": "enabled", "timeout_seconds": 1},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusUnknown {
		t.Fatalf("systemd with bogus expect should UNKNOWN, got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "installed|active|inactive|absent") {
		t.Fatalf("want allowed-values list in reason, got %q", r.Reason)
	}
}

// TestX509MissingCertField locks in the YAML-author guardrail: an x509
// check without a cert_path is meaningless and surfaces as UNKNOWN.
func TestX509MissingCertField(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "x509",
		With: map[string]interface{}{},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusUnknown {
		t.Fatalf("x509 without cert_path should UNKNOWN, got %s/%s", r.Status, r.Reason)
	}
}

// TestX509EmptyCertPathSkips proves the SE-declined-prompt path: when the
// cert_path key is present but expands to "" (the SE answered No to "Check
// <product> SSL certificate?", leaving {{ .Inputs.<product>_cert }} empty),
// the check SKIPs rather than surfacing a noisy UNKNOWN.
func TestX509EmptyCertPathSkips(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "x509",
		With: map[string]interface{}{"cert_path": ""},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusSkip {
		t.Fatalf("x509 with empty cert_path should SKIP, got %s/%s", r.Status, r.Reason)
	}
}

// TestFileModeMaxExposesPermissive proves the mode_max gate flags an
// over-permissive file. 0o666 is more permissive than 0o644 → fail.
// Conversely, a file at 0o600 must PASS the same gate.
//
// We Chmod after WriteFile because most umasks strip group/other write
// from 0o666 down to 0o644, which would defeat the test on default
// systems.
func TestFileModeMaxExposesPermissive(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "modefile")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, 0o666); err != nil {
		t.Fatal(err)
	}
	tooPermissive := checks.Check{
		ID: "x", Title: "x", Primitive: "file",
		With: map[string]interface{}{"path": tmp, "mode_max": "0644"},
	}
	if r := runRC(t, tooPermissive, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("file at 0o666 vs mode_max=0644 should FAIL, got %s/%s", r.Status, r.Reason)
	}

	tight := filepath.Join(t.TempDir(), "tight")
	if err := os.WriteFile(tight, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tight, 0o600); err != nil {
		t.Fatal(err)
	}
	ok := checks.Check{
		ID: "x", Title: "x", Primitive: "file",
		With: map[string]interface{}{"path": tight, "mode_max": "0644"},
	}
	if r := runRC(t, ok, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("file at 0o600 vs mode_max=0644 should PASS, got %s/%s", r.Status, r.Reason)
	}
}

func TestSizingPrimitive(t *testing.T) {
	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "sizing",
		With: map[string]interface{}{"cpus_min": 1, "mem_gb_min": 1, "disk_gb_min": map[string]interface{}{"/": 0}},
	}
	r := runRC(t, pass, discover.HostFacts{CPUs: 4, MemMB: 8192, DiskGB: map[string]int{"/": 100}})
	if r.Status != checks.StatusPass {
		t.Fatalf("expected pass, got %s/%s", r.Status, r.Reason)
	}
	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "sizing",
		With: map[string]interface{}{"cpus_min": 100},
	}
	r = runRC(t, fail, discover.HostFacts{CPUs: 4, MemMB: 8192, DiskGB: map[string]int{"/": 100}})
	if r.Status != checks.StatusFail || !strings.Contains(r.Reason, "cpus") {
		t.Fatalf("expected cpu failure, got %s/%s", r.Status, r.Reason)
	}

	// Memory shortfall is also blocking → FAIL.
	memFail := checks.Check{
		ID: "x", Title: "x", Primitive: "sizing",
		With: map[string]interface{}{"mem_gb_min": 16},
	}
	r = runRC(t, memFail, discover.HostFacts{CPUs: 4, MemMB: 8192, DiskGB: map[string]int{"/": 100}})
	if r.Status != checks.StatusFail || !strings.Contains(r.Reason, "mem_gb") {
		t.Fatalf("expected memory failure, got %s/%s", r.Status, r.Reason)
	}

	// Disk-only shortfall is advisory → WARN, not FAIL.
	diskShort := checks.Check{
		ID: "x", Title: "x", Primitive: "sizing",
		With: map[string]interface{}{"cpus_min": 1, "mem_gb_min": 1, "disk_gb_min": map[string]interface{}{"/": 500}},
	}
	r = runRC(t, diskShort, discover.HostFacts{CPUs: 4, MemMB: 8192, DiskGB: map[string]int{"/": 100}})
	if r.Status != checks.StatusWarn || !strings.Contains(r.Reason, "disk_gb") {
		t.Fatalf("expected disk WARN, got %s/%s", r.Status, r.Reason)
	}

	// CPU shortfall + disk shortfall together → FAIL (blocking wins), but the
	// disk shortfall is still named in the reason.
	both := checks.Check{
		ID: "x", Title: "x", Primitive: "sizing",
		With: map[string]interface{}{"cpus_min": 100, "disk_gb_min": map[string]interface{}{"/": 500}},
	}
	r = runRC(t, both, discover.HostFacts{CPUs: 4, MemMB: 8192, DiskGB: map[string]int{"/": 100}})
	if r.Status != checks.StatusFail {
		t.Fatalf("cpu+disk shortfall should FAIL, got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "cpus") || !strings.Contains(r.Reason, "disk_gb") {
		t.Fatalf("FAIL reason should name both cpu and disk shortfalls, got %q", r.Reason)
	}
}
