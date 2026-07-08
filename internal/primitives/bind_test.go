package primitives

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func TestBindPassWhenFree(t *testing.T) {
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "bind",
		With: map[string]interface{}{"host": "127.0.0.1", "port": pickFreePort(t), "timeout_seconds": 1},
	}
	if r := runRC(t, c, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("expected pass, got %s/%s", r.Status, r.Reason)
	}
}

// TestBindPassWhenOwnerMatches confirms that an in-use port owned by a
// process whose cmd name is in `owned_by` flips FAIL → PASS. We bind a
// listener from the test process itself; its argv[0] is the Go test
// binary name (e.g. "primitives.test"), so we feed that into owned_by.
func TestBindPassWhenOwnerMatches(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ss"); err != nil {
		t.Skip("ss not present; portOwner cannot resolve")
	}
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)

	owner := filepath.Base(os.Args[0])
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "bind",
		With: map[string]interface{}{
			"host": "127.0.0.1", "port": port, "timeout_seconds": 1,
			"owned_by": []interface{}{owner},
		},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusPass {
		t.Fatalf("expected pass (owner match), got %s/%s", r.Status, r.Reason)
	}
	if !strings.Contains(r.Reason, "already held by") {
		t.Fatalf("expected reason to mention owner, got %q", r.Reason)
	}
}

func TestBindFailWhenOwnerMismatches(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ss"); err != nil {
		t.Skip("ss not present; portOwner cannot resolve")
	}
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)

	c := checks.Check{
		ID: "x", Title: "x", Primitive: "bind",
		With: map[string]interface{}{
			"host": "127.0.0.1", "port": port, "timeout_seconds": 1,
			"owned_by": []interface{}{"definitely-not-this-process"},
		},
	}
	r := runRC(t, c, discover.HostFacts{})
	if r.Status != checks.StatusFail {
		t.Fatalf("expected fail (owner mismatch), got %s/%s", r.Status, r.Reason)
	}
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, p, _ := net.SplitHostPort(l.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}
