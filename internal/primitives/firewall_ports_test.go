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

// The firewall port-audit checks (sec.firewall.{workbench,connect,packagemanager}-port)
// are product-gated and backend-agnostic: each asks "can users reach THIS
// product". PASS when the front door (80/443) is open, WARN when only the
// product's own default port is open (direct-to-app, no local TLS), FAIL when
// neither is open. The logic lives in embedded shell that shells out to
// firewall-cmd / iptables / nft / systemctl, so these tests run the REAL
// check scripts against stub binaries on a controlled PATH — exercising the
// catalog as shipped, not a reimplementation.

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

// The three product checks share one resolver body and differ only in PORT/
// PRODUCT, so we exercise all of them against the same firewall fixtures.
var firewallPortChecks = []struct {
	id       string
	product  string
	openPort string // the product's own default port
}{
	{"sec.firewall.workbench-port", "Workbench", "8787"},
	{"sec.firewall.connect-port", "Connect", "3939"},
	{"sec.firewall.packagemanager-port", "Package Manager", "4242"},
}

// --- firewalld -------------------------------------------------------------

func TestFirewalldPortsClassification(t *testing.T) {
	// mkFirewalld builds a PATH dir where firewalld is active and reports the
	// given --list-ports and --list-services output. svcPorts maps a service
	// name to the "ports:" line that `firewall-cmd --info-service=NAME` prints,
	// letting us exercise the service-expansion (masking) path.
	mkFirewalld := func(ports, services string, svcPorts map[string]string) string {
		dir := t.TempDir()
		stubBin(t, dir, "systemctl", `case "$*" in *"is-active"*"firewalld"*) exit 0;; *"is-active"*) exit 1;; esac; exit 0`)
		var info strings.Builder
		for svc, p := range svcPorts {
			// `firewall-cmd --info-service=NAME` prints an indented "ports:" line.
			info.WriteString(`  --info-service=` + svc + `) echo "  ports: ` + p + `" ;;` + "\n")
		}
		stubBin(t, dir, "firewall-cmd", `
case "$1" in
  --list-ports)    echo "`+ports+`" ;;
  --list-services) echo "`+services+`" ;;
esac
case "$1" in
`+info.String()+`
esac
exit 0`)
		return dir
	}

	for _, pc := range firewallPortChecks {
		t.Run(pc.product, func(t *testing.T) {
			c := loadEmbeddedCheck(t, "40-security.yaml", pc.id)

			t.Run("front door open -> PASS", func(t *testing.T) {
				dir := mkFirewalld("80/tcp 443/tcp", "", nil)
				if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
					t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("front door via named services -> PASS", func(t *testing.T) {
				// The builtin http/https services carry 80/443 inside their
				// definitions; --list-ports is empty. Service expansion must
				// recover them.
				dir := mkFirewalld("", "http https", map[string]string{"http": "80/tcp", "https": "443/tcp"})
				if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
					t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("only product port open -> WARN", func(t *testing.T) {
				dir := mkFirewalld(pc.openPort+"/tcp", "", nil)
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusWarn {
					t.Fatalf("want WARN, got %s/%s", r.Status, r.Reason)
				}
				if !strings.Contains(r.Reason, pc.openPort) || !strings.Contains(r.Reason, "TLS") {
					t.Fatalf("WARN reason should name the port and the TLS caveat, got %q", r.Reason)
				}
			})

			t.Run("product port open via masking service -> WARN", func(t *testing.T) {
				// The product port is opened ONLY through a custom named service,
				// so it never appears in --list-ports. Service expansion must
				// find it — otherwise this would false-FAIL.
				dir := mkFirewalld("", "posit", map[string]string{"posit": pc.openPort + "/tcp"})
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusWarn {
					t.Fatalf("want WARN (port open via service), got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("nothing open -> FAIL", func(t *testing.T) {
				dir := mkFirewalld("", "", nil)
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusFail {
					t.Fatalf("want FAIL, got %s/%s", r.Status, r.Reason)
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
		})
	}
}

// --- iptables --------------------------------------------------------------

func TestIptablesPortsClassification(t *testing.T) {
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
		stubBin(t, dir, "systemctl", `case "$*" in *"is-active"*"iptables"*) exit 0;; *"is-active"*) exit 1;; esac; exit 0`)
		stubBin(t, dir, "iptables", `printf '%b' '`+ruleset+`'`)
		return dir
	}

	for _, pc := range firewallPortChecks {
		t.Run(pc.product, func(t *testing.T) {
			c := loadEmbeddedCheck(t, "40-security.yaml", pc.id)

			t.Run("front door open -> PASS", func(t *testing.T) {
				dir := mk(rules("80", "443"))
				if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
					t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("only product port open -> WARN", func(t *testing.T) {
				dir := mk(rules(pc.openPort))
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusWarn {
					t.Fatalf("want WARN, got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("nothing open -> FAIL", func(t *testing.T) {
				dir := mk(rules())
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusFail {
					t.Fatalf("want FAIL, got %s/%s", r.Status, r.Reason)
				}
			})
		})
	}
}

// --- nftables --------------------------------------------------------------

func TestNftablesPortsClassification(t *testing.T) {
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
		stubBin(t, dir, "systemctl", `case "$*" in *"is-active"*"nftables"*) exit 0;; *"is-active"*) exit 1;; esac; exit 0`)
		stubBin(t, dir, "nft", `printf '%b' '`+rs+`'`)
		return dir
	}

	for _, pc := range firewallPortChecks {
		t.Run(pc.product, func(t *testing.T) {
			c := loadEmbeddedCheck(t, "40-security.yaml", pc.id)

			t.Run("front door open -> PASS", func(t *testing.T) {
				dir := mk(ruleset("80", "443"))
				if r := runFirewallCheck(t, c, dir); r.Status != checks.StatusPass {
					t.Fatalf("want PASS, got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("only product port open -> WARN", func(t *testing.T) {
				dir := mk(ruleset(pc.openPort))
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusWarn {
					t.Fatalf("want WARN, got %s/%s", r.Status, r.Reason)
				}
			})

			t.Run("nothing open -> FAIL", func(t *testing.T) {
				dir := mk(ruleset())
				r := runFirewallCheck(t, c, dir)
				if r.Status != checks.StatusFail {
					t.Fatalf("want FAIL, got %s/%s", r.Status, r.Reason)
				}
			})
		})
	}
}
