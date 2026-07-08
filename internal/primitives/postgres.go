package primitives

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("postgres", runPostgres, []string{"host", "port", "timeout_seconds"})
}

// runPostgres probes a PostgreSQL host with a plain TCP connect. We do
// NOT send a SQL query — that would require a postgres driver in the
// binary and the actual auth credentials in the catalog. The
// network-level probe is enough to catch the "Posit can't reach the DB
// server at all" scenario; the install itself produces the more
// detailed auth/role/database error if the path works but the creds
// don't. The check uses host:port from `with:` so the YAML can wire
// {{ .Inputs.postgres_host }} / {{ .Inputs.postgres_port }} from the
// assess prompt.
func runPostgres(rc checks.RunCtx) checks.Result {
	host, _ := getString(rc.Check.With, "host")
	if host == "" {
		return checks.Result{
			ID: rc.Check.ID, Title: rc.Check.Title,
			Status: checks.StatusSkip, Reason: "no postgres host configured",
		}
	}
	port, _ := getInt(rc.Check.With, "port")
	if port == 0 {
		port = 5432
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
	r.Reason = fmt.Sprintf("reachable on %s", addr)
	return r
}
