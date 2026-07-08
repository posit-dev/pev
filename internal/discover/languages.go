package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/posit-dev/pev/internal/system"
)

// versionRoots are the canonical roots for versioned R/Python/Quarto installs
// that Posit products expect to find. Mirrors wbi/internal/languages/*.go.
var (
	rRoots      = []string{"/opt/R", "/opt/local/R", "/usr/local/lib/R"}
	pythonRoots = []string{"/opt/python", "/opt/Python", "/usr/local/lib/python"}
	quartoRoots = []string{"/opt/quarto"}
	quartoExtra = []string{
		"/usr/lib/rstudio-server/bin/quarto/bin/quarto",
		"/usr/local/bin/quarto",
	}
)

// ScanRVersions returns every directory under known R roots whose <dir>/bin/R exists.
func ScanRVersions() []string { return scanVersioned(rRoots, "bin/R") }

// ScanPythonVersions returns one entry per versioned Python install under
// the known roots. If a directory contains both `bin/python` and
// `bin/python3`, only the bare `python` path is reported (the sibling is
// almost always a symlink to it). Avoids the "duplicate Python" entry the
// previous version emitted on /opt/python images.
func ScanPythonVersions() []string {
	withPlain := map[string]struct{}{}
	for _, p := range scanVersioned(pythonRoots, "bin/python") {
		withPlain[filepath.Dir(p)] = struct{}{}
	}
	out := append([]string{}, keysOf(withPlain)...)
	for _, p := range scanVersioned(pythonRoots, "bin/python3") {
		if _, ok := withPlain[filepath.Dir(p)]; ok {
			continue
		}
		out = append(out, p)
	}
	// keysOf returned bare bin/ dirs; rehydrate with /python suffix.
	for i, p := range out {
		if filepath.Base(p) == "bin" {
			out[i] = filepath.Join(p, "python")
		}
	}
	return dedupeSorted(out)
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// LatestVersionedPath returns the entry whose <root>/<version>/ portion
// sorts highest by semver-ish version. Inputs like
// /opt/R/4.5.2/bin/R and /opt/python/3.14.2-linux-x86_64-gnu/bin/python
// are split at /<version>/, the version is parsed leniently (numbers are
// compared numerically; non-numeric suffixes break ties). Returns "" when
// no input contains a parseable version segment.
func LatestVersionedPath(paths []string) string {
	type cand struct {
		path string
		key  []int
		raw  string
	}
	var cands []cand
	for _, p := range paths {
		ver := versionSegmentOf(p)
		if ver == "" {
			continue
		}
		cands = append(cands, cand{path: p, key: parseVersion(ver), raw: ver})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		return compareVersionKeys(cands[i].key, cands[j].key) > 0
	})
	return cands[0].path
}

// versionSegmentOf returns the path component immediately after one of the
// known versioned roots (/opt/R, /opt/python, etc.). Returns "" if no such
// root prefixes p. Mirrors the layout pev's discovery uses, so adding a
// new root requires touching only this list.
func versionSegmentOf(p string) string {
	cleaned := filepath.Clean(p)
	for _, root := range append(append([]string{}, rRoots...), pythonRoots...) {
		prefix := filepath.Clean(root) + string(filepath.Separator)
		if !strings.HasPrefix(cleaned, prefix) {
			continue
		}
		rest := strings.TrimPrefix(cleaned, prefix)
		if i := strings.IndexByte(rest, filepath.Separator); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	return ""
}

// parseVersion lifts a version string into a comparable []int key. Numeric
// runs become integers; the first non-numeric segment is dropped (so
// "3.14.2-linux-x86_64-gnu" sorts as 3.14.2). Designed to be permissive,
// not strict — pev only uses this to pick "latest" for user-install
// checks, not to gate semver constraints.
func parseVersion(s string) []int {
	var out []int
	cur := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if cur < 0 {
				cur = 0
			}
			cur = cur*10 + int(c-'0')
			continue
		}
		if cur >= 0 {
			out = append(out, cur)
			cur = -1
		}
		if c != '.' {
			break
		}
	}
	if cur >= 0 {
		out = append(out, cur)
	}
	return out
}

// compareVersionKeys returns >0 when a is newer, <0 when older, 0 equal.
func compareVersionKeys(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(a) {
			ai = a[i]
		}
		if i < len(b) {
			bi = b[i]
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}

// ScanQuartoVersions returns versioned roots plus the bundled+symlink fallbacks.
func ScanQuartoVersions() []string {
	out := scanVersioned(quartoRoots, "bin/quarto")
	for _, p := range quartoExtra {
		if system.FileExists(p) {
			out = append(out, p)
		}
	}
	return dedupeSorted(out)
}

func scanVersioned(roots []string, suffix string) []string {
	out := []string{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cand := filepath.Join(root, e.Name(), suffix)
			if system.FileExists(cand) {
				out = append(out, cand)
			}
		}
	}
	return dedupeSorted(out)
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return in
	}
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
