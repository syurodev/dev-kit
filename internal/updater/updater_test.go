package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, status int, body string) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+DefaultRepo+"/releases" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Checker{APIBase: srv.URL, Client: srv.Client()}
}

func rel(tag string, prerelease bool) string {
	pre := "false"
	if prerelease {
		pre = "true"
	}
	return `{"tag_name":"` + tag + `","prerelease":` + pre +
		`,"html_url":"https://github.com/syurodev/dev-kit/releases/tag/` + tag +
		`","published_at":"2026-10-01T00:00:00Z"}`
}

func list(releases ...string) string { return "[" + strings.Join(releases, ",") + "]" }

func TestCheckReportsNewerStableRelease(t *testing.T) {
	got, err := serve(t, 200, list(rel("v1.4.0", false), rel("v1.3.0", false))).Check(context.Background(), "1.3.2")
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Current: "1.3.2", Latest: "1.4.0", Available: true,
		URL:         "https://github.com/syurodev/dev-kit/releases/tag/v1.4.0",
		PublishedAt: "2026-10-01T00:00:00Z",
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCheckQuietWhenCurrentOrAhead(t *testing.T) {
	for _, current := range []string{"1.4.0", "v1.4.0", "1.4.1", "2.0.0"} {
		got, err := serve(t, 200, list(rel("v1.4.0", false))).Check(context.Background(), current)
		if err != nil {
			t.Fatalf("%s: %v", current, err)
		}
		if got.Available || got.URL != "" {
			t.Errorf("%s: offered %+v", current, got)
		}
	}
}

// A stable build is never pushed onto a beta, however new it is.
func TestCheckStableBuildNeverSeesBetas(t *testing.T) {
	releases := list(rel("v2.0.0-beta", true), rel("v1.4.0", false), rel("v1.5.0-beta", false))
	got, err := serve(t, 200, releases).Check(context.Background(), "1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Latest != "1.4.0" || got.Prerelease || !got.Available {
		t.Fatalf("got %+v", got)
	}

	only := list(rel("v2.0.0-beta", true))
	if _, err := serve(t, 200, only).Check(context.Background(), "1.3.0"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("only betas published: err = %v, want ErrNoRelease", err)
	}
}

// A beta build follows betas and the stable release that supersedes them.
func TestCheckBetaBuildFollowsBetasAndStable(t *testing.T) {
	got, err := serve(t, 200, list(rel("v1.1.0-beta", true), rel("v1.0.0-beta", true))).Check(context.Background(), "1.0.0-beta")
	if err != nil || !got.Available || got.Latest != "1.1.0-beta" || !got.Prerelease {
		t.Fatalf("next beta: %+v, %v", got, err)
	}

	got, err = serve(t, 200, list(rel("v1.0.0", false), rel("v1.0.0-beta", true))).Check(context.Background(), "1.0.0-beta")
	if err != nil || !got.Available || got.Latest != "1.0.0" || got.Prerelease {
		t.Fatalf("stable supersedes its own beta: %+v, %v", got, err)
	}

	got, err = serve(t, 200, list(rel("v1.0.0-beta", true))).Check(context.Background(), "1.0.0-beta")
	if err != nil || got.Available {
		t.Fatalf("already on the newest beta: %+v, %v", got, err)
	}
}

// "Newest" is by version, not by the order GitHub lists them: a patch of an
// older line published last is not the latest.
func TestCheckPicksHighestVersionNotNewestListed(t *testing.T) {
	got, err := serve(t, 200, list(rel("v1.2.9", false), rel("v1.3.0", false), rel("v1.2.10", false))).Check(context.Background(), "1.2.0")
	if err != nil || got.Latest != "1.3.0" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCheckSkipsDraftsAndNonVersionTags(t *testing.T) {
	draft := `{"tag_name":"v9.0.0","draft":true,"html_url":"https://github.com/syurodev/dev-kit/releases/tag/v9.0.0"}`
	got, err := serve(t, 200, list(draft, rel("nightly", false), rel("v1.1.0", false))).Check(context.Background(), "1.0.0")
	if err != nil || got.Latest != "1.1.0" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCheckDevBuildHasNothingToCompare(t *testing.T) {
	for _, current := range []string{"", "dev", " dev "} {
		_, err := (&Checker{}).Check(context.Background(), current)
		if !errors.Is(err, ErrDevBuild) {
			t.Errorf("%q: err = %v", current, err)
		}
	}
}

func TestCheckRefusesReleasePageElsewhere(t *testing.T) {
	for _, page := range []string{
		"https://evil.example/syurodev/dev-kit/releases/tag/v9.0.0",
		"http://github.com/syurodev/dev-kit/releases/tag/v9.0.0",
		"https://github.com/someone-else/dev-kit/releases/tag/v9.0.0",
		"https://github.com.evil.example/syurodev/dev-kit/releases/tag/v9.0.0",
		"https://user@github.com/syurodev/dev-kit/releases/tag/v9.0.0",
		"javascript:alert(1)",
	} {
		body := `[{"tag_name":"v9.0.0","html_url":"` + page + `"}]`
		if _, err := serve(t, 200, body).Check(context.Background(), "1.0.0"); err == nil {
			t.Errorf("accepted release page %q", page)
		}
	}
}

func TestCheckServerAnswers(t *testing.T) {
	if _, err := serve(t, 404, `{}`).Check(context.Background(), "1.0.0"); !errors.Is(err, ErrNoRelease) {
		t.Errorf("404: %v", err)
	}
	if _, err := serve(t, 200, `[]`).Check(context.Background(), "1.0.0"); !errors.Is(err, ErrNoRelease) {
		t.Errorf("empty list: %v", err)
	}
	if _, err := serve(t, 403, `{}`).Check(context.Background(), "1.0.0"); err == nil || !strings.Contains(err.Error(), "rate") {
		t.Errorf("403: %v", err)
	}
	if _, err := serve(t, 500, `{}`).Check(context.Background(), "1.0.0"); err == nil {
		t.Error("500 accepted")
	}
	if _, err := serve(t, 200, `not json`).Check(context.Background(), "1.0.0"); err == nil {
		t.Error("garbage accepted")
	}
}

func TestVersionOrdering(t *testing.T) {
	older := [][2]string{
		{"1.0.0", "1.0.1"}, {"1.9.0", "1.10.0"}, {"1.2.3", "2.0.0"},
		{"1.0.0-rc.1", "1.0.0"}, {"1.0.0-beta", "1.0.0"}, {"1.0.0-alpha", "1.0.0-beta"},
		{"1.0.0-beta", "1.1.0-beta"}, {"1.0.0-beta.2", "1.0.0-beta.10"},
		{"1.0.0-1", "1.0.0-alpha"}, {"1.0.0-rc", "1.0.0-rc.1"},
	}
	for _, p := range older {
		a, aOK := parseVersion(p[0])
		b, bOK := parseVersion(p[1])
		if !aOK || !bOK {
			t.Fatalf("unparsable pair %v", p)
		}
		if !b.newerThan(a) || a.newerThan(b) {
			t.Errorf("%s should sort below %s", p[0], p[1])
		}
	}
	for _, bad := range []string{"", "1", "1.2", "1.2.3.4", "1.2.x", "01.2.3", "1.2.3-", "-1.2.3"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("parsed %q", bad)
		}
	}
	if a, _ := parseVersion("1.0.0+build5"); a.newerThan(version{major: 1}) {
		t.Error("build metadata affected ordering")
	}
}
