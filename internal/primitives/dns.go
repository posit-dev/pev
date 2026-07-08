package primitives

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func init() {
	checks.Register("dns", runDNS, []string{
		"name", "type", "must_resolve", "reverse_match_hostname",
		"match_local_ip", "timeout_seconds",
	})
}

// runDNS resolves `name` (default A) and optionally checks that a reverse
// lookup of one of its IPs round-trips to `name`.
func runDNS(rc checks.RunCtx) checks.Result {
	name, ok := getString(rc.Check.With, "name")
	if !ok || name == "" {
		return unknownf(rc.Check, "missing required `name` field")
	}
	mustResolve := true
	if v, ok := getBool(rc.Check.With, "must_resolve"); ok {
		mustResolve = v
	}
	timeout := getTimeout(rc.Check.With, 5*time.Second)

	ctx, cancel := context.WithTimeout(rc.Ctx, timeout)
	defer cancel()

	r := checks.Result{
		ID: rc.Check.ID, Title: rc.Check.Title,
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, name)
	if err != nil {
		if !mustResolve {
			r.Status = checks.StatusPass
			r.Reason = "name does not resolve (must_resolve=false)"
			return r
		}
		r.Status = checks.StatusFail
		r.Reason = "lookup: " + err.Error()
		return r
	}
	r.Evidence = []checks.Evidence{{Note: fmt.Sprintf("%s -> %s", name, strings.Join(addrs, ","))}}

	if v, ok := getBool(rc.Check.With, "match_local_ip"); ok && v {
		if reason := matchLocalIP(addrs); reason != "" {
			r.Status = checks.StatusFail
			if reason == "no-local-ips" {
				r.Status = checks.StatusUnknown
				r.Reason = "could not enumerate local interface IPs"
			} else {
				r.Reason = reason
			}
			return r
		}
	}

	if v, ok := getBool(rc.Check.With, "reverse_match_hostname"); ok && v {
		if !reverseHostnameMatches(ctx, addrs, name) {
			r.Status = checks.StatusFail
			r.Reason = "no reverse-DNS record for " + name + " matched its forward IP"
			return r
		}
	}

	r.Status = checks.StatusPass
	return r
}

func matchLocalIP(forward []string) string {
	local := discover.LocalIPs()
	if len(local) == 0 {
		return "no-local-ips"
	}
	for _, ip := range forward {
		for _, l := range local {
			if ip == l {
				return ""
			}
		}
	}
	return fmt.Sprintf("name resolves to %s but no IP matches a local interface (%s)",
		strings.Join(forward, ","), strings.Join(local, ","))
}

func reverseHostnameMatches(ctx context.Context, forward []string, want string) bool {
	for _, ip := range forward {
		names, err := net.DefaultResolver.LookupAddr(ctx, ip)
		if err != nil {
			continue
		}
		for _, n := range names {
			if strings.EqualFold(strings.TrimSuffix(n, "."), want) {
				return true
			}
		}
	}
	return false
}
