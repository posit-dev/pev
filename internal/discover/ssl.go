package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SSLCandidates collects plausible cert/key/pem files for SE-confirmation
// prompts. Mirrors the dropdown UX wbi uses for R/Python: always present a
// list of likely paths, let the user pick one (or type a custom path).
type SSLCandidates struct {
	Certs []string
	Keys  []string
}

// canonicalSSLPaths is the preferred match for a given product. Entries
// listed first take priority in the dropdown.
var canonicalSSLPaths = map[string]struct {
	Cert string
	Key  string
}{
	"workbench": {Cert: "/etc/rstudio/workbench.crt", Key: "/etc/rstudio/workbench.key"},
	"connect":   {Cert: "/etc/rstudio-connect/connect.crt", Key: "/etc/rstudio-connect/connect.key"},
	"ppm":       {Cert: "/etc/rstudio-pm/pm.crt", Key: "/etc/rstudio-pm/pm.key"},
}

// productSSLRoots names which directories pev walks per product when
// hunting for cert/key candidates. Intentionally **not** including
// /etc/ssl, /etc/pki/tls, or /etc/letsencrypt — those host trust-store
// bundles and unrelated service certs that would clutter the dropdown
// and confuse SEs. We stay inside the product config directories so the
// candidates are always relevant; if nothing is found, the caller falls
// back to a free-form path prompt.
var productSSLRoots = map[string][]string{
	"workbench": {"/etc/rstudio"},
	"connect":   {"/etc/rstudio-connect"},
	"ppm":       {"/etc/rstudio-pm"},
}

// ScanSSLCandidates walks the canonical product cert/key paths first,
// then the product config directory for any other plausible cert/key
// files. Returns separate cert and key lists, deterministically sorted
// with the canonical product paths first. An empty return signals the
// caller to prompt for a path instead of offering a dropdown.
func ScanSSLCandidates(product string) SSLCandidates {
	var c SSLCandidates
	if can, ok := canonicalSSLPaths[product]; ok {
		if exists(can.Cert) {
			c.Certs = append(c.Certs, can.Cert)
		}
		if exists(can.Key) {
			c.Keys = append(c.Keys, can.Key)
		}
	}
	for _, root := range productSSLRoots[product] {
		walkSSL(root, &c)
	}
	c.Certs = uniqueSorted(c.Certs)
	c.Keys = uniqueSorted(c.Keys)
	return c
}

func walkSSL(root string, c *SSLCandidates) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".crt", ".pem", ".cer":
			c.Certs = append(c.Certs, p)
		case ".key":
			c.Keys = append(c.Keys, p)
		}
		return nil
	})
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func uniqueSorted(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
