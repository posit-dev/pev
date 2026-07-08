package primitives

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/system"
)

func init() {
	checks.Register("bind", runBind, []string{
		"host", "port", "timeout_seconds", "owned_by",
	})
}

// runBind tries to open a TCP listener on host:port. PASS means the host
// can bind there; FAIL means another process is already listening, the
// port is below 1024 and we don't have CAP_NET_BIND_SERVICE, or selinux
// is denying the bind. host defaults to 0.0.0.0 (matches what the Posit
// products do at install). timeout_seconds is currently advisory — the
// listener is opened and immediately closed, so a hang is impossible —
// but kept for future symmetry with the port primitive.
//
// owned_by is an optional list of process command names (e.g.
// ["rstudio-pm", "rstudio-server"]). When the bind fails with EADDRINUSE
// and the listener on that port is one of those processes, the result is
// PASSed: the matching Posit product is already running on its default
// port, which is exactly what we want post-install.
func runBind(rc checks.RunCtx) checks.Result {
	port, ok := getInt(rc.Check.With, "port")
	if !ok || port == 0 {
		return unknownf(rc.Check, "missing required `port` field")
	}
	host := "0.0.0.0"
	if h, ok := getString(rc.Check.With, "host"); ok && h != "" {
		host = h
	}
	timeout := getTimeout(rc.Check.With, 2*time.Second)
	ownedBy, _ := getStringSlice(rc.Check.With, "owned_by")

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{
			Note: fmt.Sprintf("attempt: net.Listen(\"tcp\", %q) timeout=%s", addr, timeout),
		}},
	}

	var lc net.ListenConfig
	deadline, cancel := context.WithTimeout(rc.Ctx, timeout)
	defer cancel()
	l, err := lc.Listen(deadline, "tcp", addr)
	if err != nil {
		// If the port is in use AND we know which Posit binary should own
		// it, accept that as PASS. The product being already installed is
		// the only legitimate reason for the port to be busy on a
		// pre-install host.
		if len(ownedBy) > 0 && isAddrInUse(err) {
			owner, lookupErr := portOwner(rc.Ctx, port)
			if lookupErr == nil && owner != "" && containsFold(ownedBy, owner) {
				r.Status = checks.StatusPass
				r.Reason = fmt.Sprintf("port %d already held by %s (matches expected owner)", port, owner)
				r.Evidence[0].Note += fmt.Sprintf("; ss reports owner=%q", owner)
				return r
			}
			r.Status = checks.StatusFail
			if owner != "" {
				r.Reason = fmt.Sprintf("port %d held by %q (expected one of %s)", port, owner, strings.Join(ownedBy, ", "))
			} else {
				r.Reason = "bind failed: " + err.Error()
			}
			return r
		}
		r.Status = checks.StatusFail
		r.Reason = "bind failed: " + err.Error()
		return r
	}
	_ = l.Close()
	r.Status = checks.StatusPass
	r.Reason = "bound and released cleanly"
	return r
}

// isAddrInUse returns true when err is a "bind: address already in use"
// failure. We match on the textual suffix because the syscall error type
// is wrapped twice through net and varies across Go versions.
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "EADDRINUSE")
}

// portOwner asks ss who is listening on a given TCP port. Returns the
// process command name (the bit ss prints inside `users:(("name",pid=…))`)
// or "" if the port is unowned or ss isn't installed. Best-effort — a
// failure here just means we fall back to the original FAIL.
func portOwner(ctx context.Context, port int) (string, error) {
	cmd := fmt.Sprintf("ss -ltnpH 'sport = :%d' 2>/dev/null", port)
	res, err := system.RunCaptured(ctx, cmd, 3*time.Second)
	if err != nil {
		return "", err
	}
	// ss output line ends with: users:(("rstudio-pm",pid=687,fd=12))
	for _, line := range strings.Split(res.Stdout, "\n") {
		i := strings.Index(line, `users:(("`)
		if i < 0 {
			continue
		}
		rest := line[i+len(`users:(("`):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			continue
		}
		return rest[:j], nil
	}
	return "", nil
}

func containsFold(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}
