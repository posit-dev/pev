package primitives

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

// emptyFS is a zero-value embed.FS; the loader treats its missing "checks"
// root as "no embedded catalog", so Load reads only the on-disk pack we pass.
var emptyFS embed.FS

// The firewall port-audit checks (sec.firewalld/iptables/nftables.posit-ports-allowed)
// classify a blocked front-door port (80/443) as FAIL and a blocked
// per-product port (8787/3939/4242/5559) as advisory WARN. The logic lives in
// embedded shell that shells out to firewall-cmd / iptables / nft / systemctl,
// so these tests run the REAL check scripts against stub binaries on a
// controlled PATH — exercising the catalog as shipped, not a reimplementation.

// loadEmbeddedCheck pulls a single check out of the on-disk catalog pack by id.
// We read the YAML from disk (../../checks/...) rather than the root package's
// unexported embed var, which isn't reachable from this package.
func loadEmbeddedCheck(t *testing.T, pack, id string) checks.Check {
	t.Helper()
	all, err := checks.Load(emptyFS, "checks", []string{filepath.Join("..", "..", "checks", "common", pack)}, nil)
	if err != nil {
		t.Fatalf("load catalog pack %s: %v", pack, err)
	}
	for _, c := range all {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %q not found in %s", id, pack)
	return checks.Check{}
}

// stubBin writes an executable shell stub named `name` into dir. The body is a
// /bin/sh script; it can branch on "$@" to emulate the tool's subcommands.
func stubBin(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
}

// runFirewallCheck runs check c with PATH pointed at binDir (plus the real
// system path so /bin/sh's builtins like grep still resolve).
func runFirewallCheck(t *testing.T, c checks.Check, binDir string) checks.Result {
	t.Helper()
	t.Setenv("PATH", binDir+":/usr/bin:/bin")
	return runRC(t, c, discover.HostFacts{})
}

// --- firewalld -------------------------------------------------------------

func TestFirewalldPortsClassification(t *testing.T) {
	c := loadEmbeddedCheck(t, "40-security.yaml", "sec.firewalld.posit-ports-allowed")

	// firewall-cmd stub: --state -> running; is-active proxied via systemctl;
	// --list-ports / --list-services emit a controllable set.
	mk := func(ports, services string) string {
		dir := t.TempDir()
		stubBin(t, dir, "systemctl", `case "$*" in *"is-active"*"firewalld"*) exit 0;; esac; exit 0`)
		stubBin(t, dir, "firewall-cmd", `
case "$1" in
  --list-ports)    echo "`+ports+`" ;;
  --list-services) echo "`+services+`" ;;
  *) ;;
esac`)
		return dir
	}

	t.Run("all open -> PASS", func(t *testing.T) {
		dir := mk("80/tcp 443/tcp 8787/tcp 3939/tcp 4242/tcp 5559/tcp", "")
		if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
			t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
		}
	})

	t.Run("only product ports blocked -> WARN", func(t *testing.T) {
		// 80/443 open (via http/https services), product ports absent.
		dir := mk("", "http https")
		r := runFirewallCheck(t, c, dir)
		if r.Status != checks.StatusWarn {
			t.Fatalf("want WARN, got %s/%s", r.Status, r.Reason)
		}
		if !strings.Contains(r.Reason, "8787") || !strings.Contains(r.Reason, "advisory") {
			t.Fatalf("WARN reason should name product ports as advisory, got %q", r.Reason)
		}
	})

	t.Run("front-door 443 blocked -> FAIL", func(t *testing.T) {
		// 80 open, 443 closed, product ports open -> blocking FAIL.
		dir := mk("80/tcp 8787/tcp 3939/tcp 4242/tcp 5559/tcp", "")
		r := runFirewallCheck(t, c, dir)
		if r.Status != checks.StatusFail {
			t.Fatalf("want FAIL, got %s/%s", r.Status, r.Reason)
		}
		if !strings.Contains(r.Reason, "443") {
			t.Fatalf("FAIL reason should name 443, got %q", r.Reason)
		}
	})

	t.Run("inactive firewalld -> PASS noop", func(t *testing.T) {
		dir := t.TempDir()
		stubBin(t, dir, "systemctl", `exit 1`) // is-active --quiet -> non-zero
		stubBin(t, dir, "firewall-cmd", `exit 0`)
		if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
			t.Fatalf("inactive firewalld should PASS (noop), got %s/%s", r.Status, r.Reason)
		}
	})
}

// --- iptables --------------------------------------------------------------

func TestIptablesPortsClassification(t *testing.T) {
	c := loadEmbeddedCheck(t, "40-security.yaml", "sec.iptables.posit-ports-allowed")

	// Build an "iptables -S INPUT" ruleset with a DROP policy plus explicit
	// ACCEPTs for the named ports, so the default-permit short-circuit is not
	// taken and per-port matching is exercised.
	rules := func(accept ...string) string {
		var b strings.Builder
		b.WriteString("-P INPUT DROP\\n")
		for _, p := range accept {
			b.WriteString("-A INPUT -p tcp --dport " + p + " -j ACCEPT\\n")
		}
		b.WriteString("-A INPUT -j DROP\\n")
		return b.String()
	}
	mk := func(ruleset string) string {
		dir := t.TempDir()
		stubBin(t, dir, "systemctl", `case "$*" in *"is-active"*"iptables"*) exit 0;; esac; exit 0`)
		stubBin(t, dir, "iptables", `printf '%b' '`+ruleset+`'`)
		return dir
	}

	t.Run("all open -> PASS", func(t *testing.T) {
		dir := mk(rules("80", "443", "8787", "3939", "4242", "5559"))
		if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
			t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
		}
	})

	t.Run("only product ports blocked -> WARN", func(t *testing.T) {
		dir := mk(rules("80", "443"))
		r := runFirewallCheck(t, c, dir)
		if r.Status != checks.StatusWarn {
			t.Fatalf("want WARN, got %s/%s", r.Status, r.Reason)
		}
		if !strings.Contains(r.Reason, "advisory") {
			t.Fatalf("WARN reason should flag advisory, got %q", r.Reason)
		}
	})

	t.Run("front-door 80 blocked -> FAIL", func(t *testing.T) {
		dir := mk(rules("443", "8787", "3939", "4242", "5559"))
		r := runFirewallCheck(t, c, dir)
		if r.Status != checks.StatusFail {
			t.Fatalf("want FAIL, got %s/%s", r.Status, r.Reason)
		}
		if !strings.Contains(r.Reason, "80") {
			t.Fatalf("FAIL reason should name 80, got %q", r.Reason)
		}
	})
}

// --- nftables --------------------------------------------------------------

func TestNftablesPortsClassification(t *testing.T) {
	c := loadEmbeddedCheck(t, "40-security.yaml", "sec.nftables.posit-ports-allowed")

	ruleset := func(ports ...string) string {
		var b strings.Builder
		b.WriteString("table inet filter {\\n chain input {\\n")
		for _, p := range ports {
			b.WriteString("  tcp dport " + p + " accept\\n")
		}
		b.WriteString(" }\\n}\\n")
		return b.String()
	}
	mk := func(rs string) string {
		dir := t.TempDir()
		stubBin(t, dir, "systemctl", `case "$*" in *"is-active"*"nftables"*) exit 0;; esac; exit 0`)
		stubBin(t, dir, "nft", `printf '%b' '`+rs+`'`)
		return dir
	}

	t.Run("all open -> PASS", func(t *testing.T) {
		dir := mk(ruleset("80", "443", "8787", "3939", "4242", "5559"))
		if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
			t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
		}
	})

	t.Run("only product ports blocked -> WARN", func(t *testing.T) {
		dir := mk(ruleset("80", "443"))
		r := runFirewallCheck(t, c, dir)
		if r.Status != checks.StatusWarn {
			t.Fatalf("want WARN, got %s/%s", r.Status, r.Reason)
		}
	})

	t.Run("front-door blocked -> FAIL", func(t *testing.T) {
		dir := mk(ruleset("8787", "3939", "4242", "5559"))
		r := runFirewallCheck(t, c, dir)
		if r.Status != checks.StatusFail {
			t.Fatalf("want FAIL, got %s/%s", r.Status, r.Reason)
		}
	})
}
