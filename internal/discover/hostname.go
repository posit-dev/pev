package discover

import (
	"context"
	"net"
	"strings"
	"time"
)

// resolveFQDN tries to fully qualify hostname via DNS reverse lookup of the
// primary outbound IP, then falls back to net.LookupCNAME(hostname). Returns
// empty if both probes fail; never errors out.
func resolveFQDN(ctx context.Context, hostname string) string {
	if hostname == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if ip := primaryOutboundIP(cctx); ip != "" {
		names, err := net.DefaultResolver.LookupAddr(cctx, ip)
		if err == nil && len(names) > 0 {
			return strings.TrimSuffix(names[0], ".")
		}
	}
	if cname, err := net.DefaultResolver.LookupCNAME(cctx, hostname); err == nil && cname != "" {
		return strings.TrimSuffix(cname, ".")
	}
	return ""
}

// primaryOutboundIP opens a UDP "connection" to a public address — no packets
// are sent — to learn which interface the kernel would route traffic through.
func primaryOutboundIP(ctx context.Context) string {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", "1.1.1.1:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}
