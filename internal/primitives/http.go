package primitives

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/posit-dev/pev/internal/checks"
)

func init() {
	checks.Register("http", runHTTP, []string{
		"url", "method", "timeout_seconds", "accept_status", "fallback_paths",
	})
}

// runHTTP issues a GET (or configured method) and accepts the response if its
// status code falls in `accept_status` (default any 2xx). If `url` is unset
// the check is UNKNOWN (a YAML authoring bug); if `url` expands to an empty
// string the check SKIPs (the SE didn't supply the input the URL templates
// against, e.g. an unconfigured IdP metadata URL).
//
// When the primary URL fails and `fallback_paths` is set, each path is appended
// to the primary URL and retried in order. The first attempt to pass wins. This
// exists so an SE who supplies an OIDC issuer URL (e.g. https://idp.example.com)
// instead of the full discovery URL still gets a PASS once the well-known suffix
// is appended — see checks/common/50-languages.yaml (lang.idp.metadata).
func runHTTP(rc checks.RunCtx) checks.Result {
	url, present := getString(rc.Check.With, "url")
	if !present {
		return unknownf(rc.Check, "missing required `url` field")
	}
	if url == "" {
		return checks.Result{
			ID: rc.Check.ID, Title: rc.Check.Title,
			Status: checks.StatusSkip, Reason: "url input is empty (no value supplied)",
		}
	}
	method := "GET"
	if m, ok := getString(rc.Check.With, "method"); ok && m != "" {
		method = m
	}
	timeout := getTimeout(rc.Check.With, 5*time.Second)
	accept, _ := getIntSlice(rc.Check.With, "accept_status")
	fallbacks, _ := getStringSlice(rc.Check.With, "fallback_paths")

	client := &http.Client{Timeout: timeout}

	r := attemptHTTP(rc, client, method, url, accept)
	if r.Status == checks.StatusPass || r.Status == checks.StatusUnknown {
		return r
	}

	// Primary failed. Try each fallback suffix in turn; the first PASS wins.
	// We carry the primary's evidence forward so the report shows every URL
	// we probed, not just the one that ultimately answered.
	for _, path := range fallbacks {
		// Skip suffixes the primary URL already carries — the primary attempt
		// already covered that exact URL, so re-probing it is wasted work.
		if strings.HasSuffix(strings.TrimRight(url, "/"), "/"+strings.TrimLeft(path, "/")) {
			continue
		}
		alt := joinURLPath(url, path)
		fr := attemptHTTP(rc, client, method, alt, accept)
		r.Evidence = append(r.Evidence, fr.Evidence...)
		if fr.Status == checks.StatusPass {
			fr.Evidence = r.Evidence
			fr.Reason = fmt.Sprintf("primary %s failed; passed after appending %s", url, path)
			return fr
		}
	}
	return r
}

// attemptHTTP performs a single request and classifies the response. UNKNOWN is
// reserved for a malformed request (an authoring bug), so callers can bail early
// rather than treating it as a retryable failure.
func attemptHTTP(rc checks.RunCtx, client *http.Client, method, url string, accept []int) checks.Result {
	r := checks.Result{ID: rc.Check.ID, Title: rc.Check.Title}

	req, err := http.NewRequestWithContext(rc.Ctx, method, url, nil)
	if err != nil {
		return unknownf(rc.Check, "build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		r.Status = checks.StatusFail
		r.Reason = "request: " + err.Error()
		r.Evidence = []checks.Evidence{{Note: fmt.Sprintf("%s %s -> error: %v", method, url, err)}}
		return r
	}
	defer resp.Body.Close()
	r.Evidence = []checks.Evidence{{Note: fmt.Sprintf("%s %s -> %d", method, url, resp.StatusCode)}}

	if len(accept) > 0 {
		for _, code := range accept {
			if resp.StatusCode == code {
				r.Status = checks.StatusPass
				return r
			}
		}
		r.Status = checks.StatusFail
		r.Reason = fmt.Sprintf("status %d not in accept_status %v", resp.StatusCode, accept)
		return r
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		r.Status = checks.StatusPass
		return r
	}
	r.Status = checks.StatusFail
	r.Reason = fmt.Sprintf("non-2xx status %d", resp.StatusCode)
	return r
}

// joinURLPath appends suffix to base, collapsing the slash boundary so a base
// with or without a trailing slash and a suffix with or without a leading slash
// join to exactly one separator.
func joinURLPath(base, suffix string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(suffix, "/")
}
