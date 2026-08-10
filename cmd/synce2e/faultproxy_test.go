package main

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// newSelectiveFaultProxy returns a loopback reverse proxy in front of upstream.
// Session, enrollment, and pull requests always forward. The first pushFailures
// POSTs to /v1/sync/push return 503 with an empty body; later pushes forward.
func newSelectiveFaultProxy(t *testing.T, upstream string, pushFailures int) string {
	t.Helper()
	if pushFailures < 0 {
		t.Fatalf("pushFailures = %d, want >= 0", pushFailures)
	}
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse upstream %q: %v", upstream, err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(upstreamURL)
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		t.Errorf("fault proxy upstream error: %v", proxyErr)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}

	var mu sync.Mutex
	remaining := pushFailures
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/sync/push") {
			mu.Lock()
			fail := remaining > 0
			if fail {
				remaining--
			}
			mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		reverseProxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}
