package checks

import (
	"embed"
	"os"
	"path/filepath"
	"testing"
)

// emptyFS satisfies embed.FS plumbing without bringing in real packs; tests
// load only from the on-disk directory passed via extraDirs.
var emptyFS embed.FS

func TestLoadRejectsBadSchema(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	body := "schema_version: 99\nchecks:\n- id: x\n  title: x\n  primitive: cmd\n  severity: info\n  why: x\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(emptyFS, "checks", []string{p}, nil); err == nil {
		t.Fatal("expected schema_version error")
	}
}

func TestLoadRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	body := `schema_version: 3
checks:
- id: dupe
  title: a
  primitive: cmd
  why: a
- id: dupe
  title: b
  primitive: cmd
  why: b
`
	p := filepath.Join(dir, "dup.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(emptyFS, "checks", []string{p}, nil); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestLoadOK(t *testing.T) {
	dir := t.TempDir()
	body := `schema_version: 3
checks:
- id: ok.one
  title: one
  primitive: cmd
  why: y
  with: { cmd: "true" }
`
	p := filepath.Join(dir, "ok.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, err := Load(emptyFS, "checks", []string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].ID != "ok.one" {
		t.Fatalf("got %+v", cs)
	}
}
