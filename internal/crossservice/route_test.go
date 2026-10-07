package crossservice

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		call, endpoint string
		ok             bool
		score          int
	}{
		{"/v1/internal/api-access/{org}/api/{api}", "/v1/internal/api-access/{org}/api/{api}", true, 4},
		{"/v1/internal/api-access/{org}/api/{api}", "/v1/internal/api-access/{organizationId}/api/{apiId}", true, 4},
		{"/v1/internal/api-access/acme/api/orders", "/v1/internal/api-access/{org}/api/{api}", true, 4},
		{"/users/me", "/users/{id}", true, 1},
		{"/users/me", "/users/me", true, 2},
		{"/users/{id}", "/users/me", true, 1},
		{"/users", "/users/{id}", false, 0},
		{"/users/1/orders", "/users/{id}", false, 0},
		{"/users/1/orders", "/users/{id}/{*rest}", true, 1},
		{"/users/1", "/users/{id}/**", true, 1},
		{"/a/b/", "/a/b", true, 2},
		{"//a//b", "/a/b", true, 2},
		{"/a/x", "/a/y", false, 0},
		{"/a/:id", "/a/{id}", true, 1},
		{"/", "/", true, 0},
	}
	for _, c := range cases {
		score, ok := match(c.call, c.endpoint)
		if ok != c.ok || (ok && score != c.score) {
			t.Errorf("match(%q, %q) = %d, %v; want %d, %v", c.call, c.endpoint, score, ok, c.score, c.ok)
		}
	}
}

func TestLiteralSegments(t *testing.T) {
	for path, want := range map[string]int{"/health": 1, "/": 0, "/{id}": 0, "/v1/x/{a}": 2, "/v1/{a}/{b}": 1} {
		if got := literalSegments(path); got != want {
			t.Errorf("literalSegments(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestCallPath(t *testing.T) {
	for url, want := range map[string]string{
		"http://billing-service:8080/invoices/{id}?x=1": "/invoices/{id}",
		"https://api.example.com":                       "",
		"/v1/ping":                                      "/v1/ping",
		"{conf.access_url}/v1/internal/x/{org}":         "/v1/internal/x/{org}",
		"${clients.billing.url}/invoices":               "/invoices",
		"{base}":                                        "",
		"{base}suffix/x":                                "",
		"relative/path":                                 "",
		"/a?b#c":                                        "/a",
	} {
		if got := callPath(url); got != want {
			t.Errorf("callPath(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestWithContextPath(t *testing.T) {
	if got := withContextPath("/api", "/v1/x"); got != "/api/v1/x" {
		t.Fatal(got)
	}
	if got := withContextPath("", "/v1/x"); got != "/v1/x" {
		t.Fatal(got)
	}
	if got := withContextPath("/api/", "v1/x"); got != "/api/v1/x" {
		t.Fatal(got)
	}
}
