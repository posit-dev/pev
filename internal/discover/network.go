package discover

import "net"

// LocalIPs returns every non-loopback IPv4/IPv6 address bound to one of
// this host's interfaces, formatted as plain strings ("10.0.0.5", not
// "10.0.0.5/24"). Used by the DNS forward-resolve check to confirm that
// the customer's hostname resolves to *this* host. Returns empty when
// the interface enumeration fails — callers should treat that as
// "skip" rather than "fail".
func LocalIPs() []string {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := []string{}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}
