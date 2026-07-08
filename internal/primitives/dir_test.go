package primitives

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func TestDirPrimitive(t *testing.T) {
	root := t.TempDir()
	// Drop a couple of "*.lic" files so the glob check passes.
	for _, n := range []string{"a.lic", "b.lic"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "dir",
		With: map[string]interface{}{"path": root, "must_exist": true, "glob": "*.lic", "glob_min_matches": 1},
	}
	if r := runRC(t, pass, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("got %s/%s", r.Status, r.Reason)
	}

	missing := checks.Check{
		ID: "x", Title: "x", Primitive: "dir",
		With: map[string]interface{}{"path": "/nonexistent/dir/path"},
	}
	if r := runRC(t, missing, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("expected fail, got %s", r.Status)
	}

	// Glob with too few matches.
	tooFew := checks.Check{
		ID: "x", Title: "x", Primitive: "dir",
		With: map[string]interface{}{"path": root, "glob": "*.lic", "glob_min_matches": 5},
	}
	if r := runRC(t, tooFew, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("expected glob count fail, got %s", r.Status)
	}
}
