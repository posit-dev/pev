// Package operatingsystem detects the running Linux distribution and
// normalizes it into the canonical IDs used by the check catalog
// (e.g. ubuntu-22.04, rhel-9). RHEL-family distros (Alma, Rocky, Oracle,
// CentOS) collapse onto rhel-<major> so a single check catalog covers real
// RHEL and its rebuilds with one set of applies_to.os gates.
//
// Normalization is deliberately coarse: it answers "which catalog applies",
// not "is this distro supported". Support is a separate judgement made by the
// os.supported check (checks/common/00-os.yaml), which re-reads the raw
// /etc/os-release ID and can be stricter than this mapping — e.g. it rejects
// CentOS Stream 9/10 (upstream of RHEL, not a binary-compatible rebuild) even
// though they normalize to rhel-9/rhel-10 here.
package operatingsystem

import (
	"bufio"
	"os"
	"strings"
)

// OS captures the identification we need for catalog gating and report headers.
type OS struct {
	ID     string // canonical: ubuntu-22.04 | ubuntu-24.04 | rhel-8 | rhel-9 | rhel-10 | unknown
	Pretty string // /etc/os-release PRETTY_NAME, or a synthesized fallback
	Family string // ubuntu | rhel | unknown
}

// Detect inspects /etc/os-release first (the modern, freedesktop-spec source),
// then falls back to /etc/redhat-release and /etc/issue if needed.
func Detect() OS {
	if osr, ok := readOSRelease("/etc/os-release"); ok {
		return normalize(osr)
	}
	if data, err := os.ReadFile("/etc/redhat-release"); err == nil {
		return fromRedhatRelease(string(data))
	}
	if data, err := os.ReadFile("/etc/issue"); err == nil {
		return fromIssue(string(data))
	}
	return OS{ID: "unknown", Family: "unknown", Pretty: "Unknown Linux"}
}

// readOSRelease parses a freedesktop os-release file into a flat map.
func readOSRelease(path string) (map[string]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	out := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		out[strings.TrimSpace(k)] = v
	}
	return out, len(out) > 0
}

func normalize(osr map[string]string) OS {
	id := strings.ToLower(osr["ID"])
	ver := osr["VERSION_ID"]
	pretty := osr["PRETTY_NAME"]
	if pretty == "" {
		pretty = strings.TrimSpace(osr["NAME"] + " " + ver)
	}

	out := OS{Pretty: pretty}

	switch id {
	case "ubuntu":
		out.Family = "ubuntu"
		out.ID = "ubuntu-" + ver // 22.04 / 24.04 used verbatim
	case "rhel", "redhat", "almalinux", "rocky", "centos", "ol", "oracle":
		out.Family = "rhel"
		major := majorOf(ver)
		if major == "" {
			out.ID = "rhel-unknown"
		} else {
			out.ID = "rhel-" + major
		}
	default:
		out.Family = "unknown"
		out.ID = "unknown"
	}
	return out
}

// majorOf returns the leading numeric component of a VERSION_ID like "9.4" or "10.0".
func majorOf(v string) string {
	if v == "" {
		return ""
	}
	for i, r := range v {
		if r < '0' || r > '9' {
			return v[:i]
		}
	}
	return v
}

func fromRedhatRelease(s string) OS {
	out := OS{Family: "rhel", Pretty: strings.TrimSpace(s)}
	for _, m := range []string{"10", "9", "8"} {
		if strings.Contains(s, "release "+m) {
			out.ID = "rhel-" + m
			return out
		}
	}
	out.ID = "rhel-unknown"
	return out
}

func fromIssue(s string) OS {
	low := strings.ToLower(s)
	if strings.Contains(low, "ubuntu") {
		// /etc/issue typically reads "Ubuntu 22.04.5 LTS \n \l"
		fields := strings.Fields(s)
		if len(fields) >= 2 {
			ver := fields[1]
			// Take MAJOR.MINOR
			parts := strings.SplitN(ver, ".", 3)
			if len(parts) >= 2 {
				short := parts[0] + "." + parts[1]
				return OS{
					ID: "ubuntu-" + short, Family: "ubuntu",
					Pretty: strings.TrimSpace(s),
				}
			}
		}
	}
	return OS{ID: "unknown", Family: "unknown", Pretty: strings.TrimSpace(s)}
}
