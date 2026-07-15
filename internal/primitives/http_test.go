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

func TestHTTPFallbackPaths(t *testing.T) {
	// Only the well-known discovery path answers 200; the issuer root 404s.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	// Primary is the bare issuer URL; fallback appends the discovery suffix.
	pass := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{
			"url":             srv.URL,
			"timeout_seconds": 2,
			"fallback_paths":  []interface{}{".well-known/openid-configuration"},
		},
	}
	r := runRC(t, pass, discover.HostFacts{})
	if r.Status != checks.StatusPass {
		t.Fatalf("expected pass via fallback, got %s/%s", r.Status, r.Reason)
	}
	if len(r.Evidence) != 2 {
		t.Fatalf("expected evidence for both primary and fallback, got %d: %+v", len(r.Evidence), r.Evidence)
	}

	// Primary already carries the suffix: passes on the first attempt and the
	// fallback is skipped (single evidence line).
	direct := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{
			"url":             srv.URL + "/.well-known/openid-configuration",
			"timeout_seconds": 2,
			"fallback_paths":  []interface{}{".well-known/openid-configuration"},
		},
	}
	if r := runRC(t, direct, discover.HostFacts{}); r.Status != checks.StatusPass || len(r.Evidence) != 1 {
		t.Fatalf("expected direct pass with one evidence line, got %s/%d", r.Status, len(r.Evidence))
	}

	// Nothing answers on any path: still FAIL, with evidence for every probe.
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv404.Close()
	fail := checks.Check{
		ID: "x", Title: "x", Primitive: "http",
		With: map[string]interface{}{
			"url":             srv404.URL,
			"timeout_seconds": 2,
			"fallback_paths":  []interface{}{".well-known/openid-configuration"},
		},
	}
	if r := runRC(t, fail, discover.HostFacts{}); r.Status != checks.StatusFail || len(r.Evidence) != 2 {
		t.Fatalf("expected fail with two evidence lines, got %s/%d", r.Status, len(r.Evidence))
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
