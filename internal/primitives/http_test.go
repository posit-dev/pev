package primitives

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/posit-dev/pev/internal/checks"
	"github.com/posit-dev/pev/internal/discover"
)

func TestHTTPPrimitive(t *testing.T) {
	srv200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv200.Close()
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv500.Close()

	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{"url": srv200.URL, "timeout_seconds": 2},
	}
	if r := runRC(t, pass, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("got %s/%s", r.Status, r.Reason)
	}

	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{"url": srv500.URL, "timeout_seconds": 2},
	}
	if r := runRC(t, fail, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("expected fail, got %s/%s", r.Status, r.Reason)
	}

	// Connection refused — no listener.
	dead := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{"url": "http://127.0.0.1:1", "timeout_seconds": 2},
	}
	if r := runRC(t, dead, discover.HostFacts{}); r.Status != checks.StatusFail {
		t.Fatalf("expected fail, got %s", r.Status)
	}
}

func TestHTTPAcceptStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv.Close()
	c := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{
			"url":             srv.URL,
			"timeout_seconds": 2,
			"accept_status":   []interface{}{404},
		},
	}
	if r := runRC(t, c, discover.HostFacts{}); r.Status != checks.StatusPass {
		t.Fatalf("expected pass, got %s/%s", r.Status, r.Reason)
	}
}
