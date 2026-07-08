package primitives

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("port", runPort, []string{
		"host", "port", "timeout_seconds",
	})
}

// runPort opens a TCP connection to host:port and reports reachability.
// Equivalent to `nc -vz host port` but built-in.
//
// The host guard mirrors x509/postgres: a missing `host` key is a YAML
// authoring bug (UNKNOWN), but a key that is present yet empty means the SE
// declined the input the host templates against (e.g. the SMTP confirm), so
// the check SKIPs rather than firing a misleading UNKNOWN/investigation.
func runPort(rc checks.RunCtx) checks.Result {
	host, present := getString(rc.Check.With, "host")
	if !present {
		return unknownf(rc.Check, "missing required `host` field")
	}
	if host == "" {
		return checks.Result{
			ID: rc.Check.ID, Title: rc.Check.Title,
			Status: checks.StatusSkip, Reason: "host input is empty (no value supplied)",
		}
	}
	port, ok := getInt(rc.Check.With, "port")
	if !ok || port == 0 {
		return unknownf(rc.Check, "missing required `port` field")
	}
	timeout := getTimeout(rc.Check.With, 5*time.Second)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
		Evidence: []checks.Evidence{{
			Note: fmt.Sprintf("nc -vz %s %d (timeout %s)", host, port, timeout),
		}},
	}
	if err := tcpReachable(rc.Ctx, addr, timeout); err != nil {
		r.Status = checks.StatusFail
		r.Reason = "tcp dial: " + err.Error()
		return r
	}
	r.Status = checks.StatusPass
	return r
}

// tcpReachable opens and immediately closes a TCP connection to addr,
// returning nil on success and the dial error otherwise. Centralizes the
// "is anything listening on host:port" probe shared by the port and
// postgres primitives — keeps timeout handling and connection cleanup
// in one place.
func tcpReachable(ctx context.Context, addr string, timeout time.Duration) error {
	d := net.Dialer{Timeout: timeout}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}
