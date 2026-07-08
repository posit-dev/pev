package primitives

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func TestPortPrimitive(t *testing.T) {
	// Bind a TCP listener on a random port to use as the "open" target.
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)

	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "port",
		With: map[string]interface{}{"host": "127.0.0.1", "port": port, "timeout_seconds": 2},
	}
	if r := runRC(t, pass, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("got %s/%s", r.Status, r.Reason)
	}

	// Closed port (1 is reserved and never listening).
	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "port",
		With: map[string]interface{}{"host": "127.0.0.1", "port": 1, "timeout_seconds": 1},
	}
	r := runRC(t, fail, discover.HostFacts{})
	if r.Status != checks.StatusFail || !strings.Contains(r.Reason, "tcp dial") {
		t.Fatalf("expected dial failure, got %s/%s", r.Status, r.Reason)
	}
}
