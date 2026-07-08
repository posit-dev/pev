package checks

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load returns every Check from the embedded catalog plus any extra packs on
// disk. Files are loaded in deterministic order (sorted by path). Duplicate
// IDs across packs cause an error so the catalog stays unambiguous.
func Load(embedded embed.FS, embeddedRoot string, extraFiles []string, extraDirs []string) ([]Check, error) {
	var packs []packLoad

	// Embedded. A missing root is treated as "no embedded catalog" — useful in
	// tests that exercise only filesystem packs.
	if _, err := fs.Stat(embedded, embeddedRoot); err == nil {
		if err := fs.WalkDir(embedded, embeddedRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !isYAML(p) {
				return nil
			}
			data, err := embedded.ReadFile(p)
			if err != nil {
				return err
			}
			packs = append(packs, packLoad{path: "embedded:" + p, data: data})
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk embedded catalog: %w", err)
		}
	}

	// Explicit files.
	for _, f := range extraFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		packs = append(packs, packLoad{path: f, data: data})
	}

	// Directory globs (e.g. ~/.config/pev/checks.d).
	for _, d := range extraDirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", d, err)
		}
		for _, e := range entries {
			if e.IsDir() || !isYAML(e.Name()) {
				continue
			}
			path := filepath.Join(d, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
			packs = append(packs, packLoad{path: path, data: data})
		}
	}

	sort.Slice(packs, func(i, j int) bool { return packs[i].path < packs[j].path })

	var all []Check
	seen := map[string]string{} // id -> source path
	for _, p := range packs {
		var pack Pack
		if err := yaml.Unmarshal(p.data, &pack); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p.path, err)
		}
		if pack.SchemaVersion == 0 {
			return nil, fmt.Errorf("%s: missing schema_version", p.path)
		}
		if pack.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("%s: schema_version %d unsupported (this pev expects %d)", p.path, pack.SchemaVersion, SchemaVersion)
		}
		for _, c := range pack.Checks {
			c.Source = p.path
			if prev, dup := seen[c.ID]; dup {
				return nil, fmt.Errorf("duplicate check id %q in %s (also in %s)", c.ID, p.path, prev)
			}
			seen[c.ID] = p.path
			all = append(all, c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}

type packLoad struct {
	path string
	data []byte
}

func isYAML(p string) bool {
	return strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
}
