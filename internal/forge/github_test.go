package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost is a GitHub stand-in: routes by "METHOD path", serves ETags the way
// GitHub does (a validator per body, 304 when it matches), and records every
// request so a test can say what was — and was not — sent.
type fakeHost struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]func(w http.ResponseWriter, r *http.Request, body []byte)
	log    []recorded
}

type recorded struct {
	Method, Path, Query, Auth, IfNoneMatch string
	Body                                   []byte
}

func newFakeHost(t *testing.T) *fakeHost {
	f := &fakeHost{t: t, routes: map[string]func(http.ResponseWriter, *http.Request, []byte){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.log = append(f.log, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("If-None-Match"), body})
		route := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if route == nil {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		route(w, r, body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHost) provider() *GitHub { return NewGitHub(WithAPIBase(f.srv.URL)) }

func (f *fakeHost) handle(route string, fn func(http.ResponseWriter, *http.Request, []byte)) {
	f.mu.Lock()
	f.routes[route] = fn
	f.mu.Unlock()
}

// json serves a body with an ETag derived from it, answering 304 to a match.
func (f *fakeHost) json(route string, v any) {
	raw, _ := json.Marshal(v)
	etag := fmt.Sprintf(`"%x"`, len(raw)*31+int(raw[len(raw)/2]))
	f.handle(route, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	})
}

func (f *fakeHost) status(route string, code int, message string, headers map[string]string) {
	f.handle(route, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"message":%q}`, message)
	})
}

func (f *fakeHost) exact(route string) []recorded {
	var out []recorded
	for _, r := range f.requests(route) {
		if r.Method+" "+r.Path == route {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeHost) requests(prefix string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.log {
		if strings.HasPrefix(r.Method+" "+r.Path, prefix) {
			out = append(out, r)
		}
	}
	return out
}

var (
	repo = Repo{Host: "github.com", Path: "acme/widgets"}
	cred = Credential{Token: "ghp_secret_token"}
	bg   = context.Background()
)

const pullBase = "/repos/acme/widgets/pulls/7"

func samplePull(extra map[string]any) map[string]any {
	pull := map[string]any{
		"number": 7, "title": "Add retries", "html_url": "https://github.com/acme/widgets/pull/7",
		"state": "open", "draft": false, "merged": false, "additions": 120, "deletions": 14, "changed_files": 5,
		"mergeable": true, "mergeable_state": "clean", "updated_at": "2026-10-16T10:00:00Z",
		"user": map[string]any{"login": "syuro"},
		"head": map[string]any{"ref": "feat/retries", "sha": "abc123", "repo": map[string]any{"full_name": "acme/widgets"}},
		"base": map[string]any{"ref": "main"},
	}
	for k, v := range extra {
		pull[k] = v
	}
	return pull
}

func serveChecks(f *fakeHost, runs []map[string]any, statuses []map[string]any) {
	f.json("GET /repos/acme/widgets/commits/abc123/check-runs", map[string]any{"check_runs": runs})
	f.json("GET /repos/acme/widgets/commits/abc123/status", map[string]any{"statuses": statuses})
}

func serveReviews(f *fakeHost, reviews []map[string]any) {
	f.json("GET "+pullBase+"/reviews", reviews)
}

func run(name, status, conclusion string) map[string]any {
	return map[string]any{"name": name, "status": status, "conclusion": conclusion, "html_url": "https://ci/" + name}
}

func TestParseURL(t *testing.T) {
	g := NewGitHub()
	for _, tc := range []struct {
		raw  string
		ok   bool
		path string
		n    int
	}{
		{"https://github.com/acme/widgets/pull/7", true, "acme/widgets", 7},
		{"https://github.com/acme/widgets/pull/7/files", true, "acme/widgets", 7},
		{"  https://github.com/Acme/Widgets/pull/12  ", true, "Acme/Widgets", 12},
		{"https://github.com/acme/widgets/issues/7", false, "", 0},
		{"https://evil-github.com/acme/widgets/pull/7", false, "", 0},
		{"https://github.com.evil.test/acme/widgets/pull/7", false, "", 0},
		{"ftp://github.com/acme/widgets/pull/7", false, "", 0},
		{"https://github.com/acme/widgets/pull/0", false, "", 0},
		{"not a url", false, "", 0},
	} {
		r, n, ok := g.ParseURL(tc.raw)
		if ok != tc.ok || r.Path != tc.path || n != tc.n {
			t.Errorf("ParseURL(%q) = %v %d %v, want %q %d %v", tc.raw, r, n, ok, tc.path, tc.n, tc.ok)
		}
	}
}

func TestFindSendsHeadAndToken(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /repos/acme/widgets/pulls", []map[string]any{
		samplePull(map[string]any{"number": 9, "state": "closed", "merged_at": "2026-10-01T00:00:00Z"}),
		samplePull(nil),
	})
	got, err := f.provider().Find(bg, cred, repo, Head{Owner: "acme", Branch: "feat/retries"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].State != StateMerged || got[1].State != StateOpen || got[1].Number != 7 {
		t.Fatalf("found %+v", got)
	}
	req := f.requests("GET /repos/acme/widgets/pulls")[0]
	if req.Auth != "Bearer ghp_secret_token" {
		t.Errorf("auth %q", req.Auth)
	}
	for _, want := range []string{"head=acme%3Afeat%2Fretries", "state=all"} {
		if !strings.Contains(req.Query, want) {
			t.Errorf("query %q lacks %q", req.Query, want)
		}
	}
	if got[1].ChecksKnown || got[1].UnresolvedThreads != -1 {
		t.Errorf("a list result claims more than it read: %+v", got[1])
	}
}

func TestGetReadsSizeChecksAndDecision(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	serveChecks(f,
		[]map[string]any{
			run("build", "completed", "success"),
			run("lint", "completed", "neutral"),
			run("docs", "completed", "skipped"),
			run("unit", "completed", "failure"),
			run("e2e", "in_progress", ""),
			run("slow", "completed", "timed_out"),
		},
		[]map[string]any{
			{"state": "success", "context": "ci/legacy", "target_url": "https://legacy/1"},
			{"state": "pending", "context": "ci/queue"},
			{"state": "error", "context": "ci/deploy", "description": "deploy broke"},
		})
	serveReviews(f, []map[string]any{
		{"state": "CHANGES_REQUESTED", "user": map[string]any{"login": "a"}},
		{"state": "COMMENTED", "user": map[string]any{"login": "b"}},
		{"state": "APPROVED", "user": map[string]any{"login": "a"}},
	})
	cr, err := f.provider().Get(bg, cred, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Additions != 120 || cr.Deletions != 14 || cr.ChangedFiles != 5 || cr.Head != "feat/retries" || cr.Base != "main" || cr.HeadSHA != "abc123" {
		t.Errorf("pull %+v", cr)
	}
	if cr.Mergeable != MergeClean || cr.State != StateOpen || cr.Terms.Short != "PR" {
		t.Errorf("state %+v", cr)
	}
	want := CheckSummary{Total: 9, Passed: 4, Skipped: 1, Failed: 3, Running: 2}
	got := cr.Checks
	if got.Total != want.Total || got.Passed != want.Passed || got.Skipped != want.Skipped || got.Failed != want.Failed || got.Running != want.Running {
		t.Errorf("checks %+v, want %+v", got, want)
	}
	names := []string{}
	for _, c := range got.Failing {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "unit,slow,ci/deploy" {
		t.Errorf("failing %v", names)
	}
	if !cr.ChecksKnown || len(cr.Notes) != 0 {
		t.Errorf("known=%v notes=%v", cr.ChecksKnown, cr.Notes)
	}
	// a's latest vote is the approval, so nothing is requesting changes.
	if cr.Decision != DecisionApproved {
		t.Errorf("decision %q", cr.Decision)
	}
	if got.Failing[0].Summary != "" {
		t.Errorf("no output was reported, summary %q", got.Failing[0].Summary)
	}
}

func TestMergeableStates(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		mergeable *bool
		state     string
		want      Mergeable
	}{
		{nil, "unknown", MergeUnknown},
		{&yes, "clean", MergeClean},
		{&yes, "unstable", MergeClean},
		{&no, "dirty", MergeConflicting},
		{&yes, "blocked", MergeBlocked},
		{&yes, "behind", MergeBehind},
		{&yes, "draft", MergeBlocked},
		{&no, "", MergeConflicting},
	} {
		if got := mergeableOf(ghPull{Mergeable: tc.mergeable, MergeState: tc.state}); got != tc.want {
			t.Errorf("mergeable=%v state=%q → %q, want %q", tc.mergeable, tc.state, got, tc.want)
		}
	}
}

func TestDecisionChangesRequestedAndDismissed(t *testing.T) {
	f := newFakeHost(t)
	serveReviews(f, []map[string]any{
		{"state": "APPROVED", "user": map[string]any{"login": "a"}},
		{"state": "CHANGES_REQUESTED", "user": map[string]any{"login": "b"}},
	})
	if d, err := f.provider().decision(bg, cred, repo, "7"); err != nil || d != DecisionChangesRequested {
		t.Fatalf("decision %q %v", d, err)
	}
	serveReviews(f, []map[string]any{
		{"state": "CHANGES_REQUESTED", "user": map[string]any{"login": "b"}},
		{"state": "DISMISSED", "user": map[string]any{"login": "b"}},
	})
	if d, _ := f.provider().decision(bg, cred, repo, "7"); d != DecisionNone {
		t.Fatalf("a dismissed review still counts: %q", d)
	}
}

func TestFailedCheckCarriesWhyTheHostSaid(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	failing := run("unit", "completed", "failure")
	failing["output"] = map[string]any{"title": "3 tests failed\nmore", "summary": "ignored"}
	serveChecks(f, []map[string]any{failing}, nil)
	serveReviews(f, nil)
	checks, err := f.provider().Checks(bg, cred, repo, 7)
	if err != nil || len(checks) != 1 {
		t.Fatalf("%v %v", checks, err)
	}
	if checks[0].Summary != "3 tests failed" || checks[0].URL != "https://ci/unit" || checks[0].Conclusion != "failure" {
		t.Errorf("%+v", checks[0])
	}
}

func TestChecksAreFollowedAcrossPages(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	f.handle("GET /repos/acme/widgets/commits/abc123/check-runs", func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`{"check_runs":[{"name":"b","status":"completed","conclusion":"success"}]}`))
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/widgets/commits/abc123/check-runs?per_page=100&page=2>; rel="next"`, f.srv.URL))
		w.Write([]byte(`{"check_runs":[{"name":"a","status":"completed","conclusion":"success"}]}`))
	})
	f.json("GET /repos/acme/widgets/commits/abc123/status", map[string]any{"statuses": []any{}})
	checks, err := f.provider().Checks(bg, cred, repo, 7)
	if err != nil || len(checks) != 2 {
		t.Fatalf("%v %v", checks, err)
	}
}

func TestPollingAnUnchangedPullIsAnsweredWith304(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	serveChecks(f, []map[string]any{run("build", "completed", "success")}, nil)
	serveReviews(f, nil)
	g := f.provider()

	first, err := g.Get(bg, cred, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if g.Stats().NotModified != 0 {
		t.Fatalf("the first read cannot be conditional: %+v", g.Stats())
	}
	second, err := g.Get(bg, cred, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	stats := g.Stats()
	// pull, check-runs, status, reviews: four requests, four validators.
	if stats.Requests != 8 || stats.NotModified != 4 {
		t.Fatalf("stats %+v", stats)
	}
	if second.Title != first.Title || second.Checks.Passed != 1 || second.Additions != 120 {
		t.Errorf("a 304 must serve the cached body: %+v", second)
	}
	for _, r := range f.exact("GET " + pullBase)[1:] {
		if r.IfNoneMatch == "" {
			t.Error("the second request was not conditional")
		}
	}

	// The same URL for another account is not served from the first's cache.
	other := Credential{Token: "ghp_someone_else"}
	if _, err := g.Get(bg, other, repo, 7); err != nil {
		t.Fatal(err)
	}
	last := f.exact("GET " + pullBase)
	if last[len(last)-1].IfNoneMatch != "" {
		t.Error("a validator cached for one token was sent for another")
	}
}

func TestChangedPullIsNotServedFromTheCache(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	serveChecks(f, nil, nil)
	serveReviews(f, nil)
	g := f.provider()
	if _, err := g.Get(bg, cred, repo, 7); err != nil {
		t.Fatal(err)
	}
	f.json("GET "+pullBase, samplePull(map[string]any{"title": "Add retries and backoff", "additions": 200}))
	cr, err := g.Get(bg, cred, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Title != "Add retries and backoff" || cr.Additions != 200 {
		t.Errorf("stale: %+v", cr)
	}
}

func TestGetLosesChecksWithANoteNotTheWholeCall(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	f.status("GET /repos/acme/widgets/commits/abc123/check-runs", 403, "Resource not accessible by personal access token", nil)
	serveReviews(f, nil)
	cr, err := f.provider().Get(bg, cred, repo, 7)
	if err != nil {
		t.Fatalf("a token that cannot read checks must still read the pull request: %v", err)
	}
	if cr.ChecksKnown || len(cr.Notes) != 1 || !strings.Contains(cr.Notes[0], "checks unavailable") {
		t.Errorf("known=%v notes=%v", cr.ChecksKnown, cr.Notes)
	}
	if cr.Number != 7 {
		t.Errorf("%+v", cr)
	}
}

func TestErrorsAreClassifiedAndNeverCarryTheToken(t *testing.T) {
	reset := time.Now().Add(37 * time.Minute).Truncate(time.Second)
	for _, tc := range []struct {
		name    string
		code    int
		headers map[string]string
		message string
		kind    ErrorKind
	}{
		{"bad token", 401, nil, "Bad credentials", ErrUnauthorized},
		{"no scope", 403, nil, "Resource not accessible by personal access token", ErrForbidden},
		{"sso", 403, map[string]string{"X-GitHub-SSO": "required; url=https://x"}, "Resource protected by organization SAML enforcement", ErrForbidden},
		{"primary limit", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())}, "API rate limit exceeded", ErrRateLimited},
		{"secondary limit", 403, map[string]string{"Retry-After": "60"}, "You have exceeded a secondary rate limit", ErrRateLimited},
		{"too many", 429, map[string]string{"Retry-After": "30"}, "slow down", ErrRateLimited},
		{"missing", 404, nil, "Not Found", ErrNotFound},
		{"invalid", 422, nil, "Validation Failed", ErrValidation},
		{"down", 502, nil, "Bad Gateway", ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHost(t)
			f.status("GET "+pullBase, tc.code, tc.message, tc.headers)
			_, err := f.provider().Get(bg, cred, repo, 7)
			if KindOf(err) != tc.kind {
				t.Fatalf("kind %q (%v), want %q", KindOf(err), err, tc.kind)
			}
			if strings.Contains(err.Error(), cred.Token) {
				t.Errorf("the error names the token: %v", err)
			}
			if tc.kind == ErrRateLimited {
				at := RetryAtOf(err)
				if at.IsZero() || !at.After(time.Now()) {
					t.Errorf("retry at %v", at)
				}
				if tc.name == "primary limit" && !at.Equal(reset) {
					t.Errorf("retry at %v, want the reset %v", at, reset)
				}
			}
			if tc.name == "sso" && !strings.Contains(err.Error(), "SAML") {
				t.Errorf("the SSO hint was lost: %v", err)
			}
		})
	}
}

func TestValidationErrorKeepsTheHostsReasons(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /repos/acme/widgets/pulls", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(422)
		w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for acme:feat/retries."}]}`))
	})
	_, err := f.provider().Create(bg, cred, repo, CreateInput{Title: "t", Head: "feat/retries", Base: "main"})
	if KindOf(err) != ErrValidation || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("%v", err)
	}
}

func TestNoTokenIsRefusedBeforeAnythingIsSent(t *testing.T) {
	f := newFakeHost(t)
	_, err := f.provider().Get(bg, Credential{}, repo, 7)
	if KindOf(err) != ErrUnauthorized || len(f.requests("")) != 0 {
		t.Fatalf("%v, %d requests", err, len(f.requests("")))
	}
}

func TestMalformedInputIsRefusedLocally(t *testing.T) {
	f := newFakeHost(t)
	g := f.provider()
	if _, err := g.Get(bg, cred, Repo{Host: "github.com", Path: "nope"}, 7); KindOf(err) != ErrInvalid {
		t.Errorf("repo: %v", err)
	}
	if _, err := g.Get(bg, cred, repo, 0); KindOf(err) != ErrInvalid {
		t.Errorf("number: %v", err)
	}
	if _, err := g.Merge(bg, cred, repo, 7, MergeInput{Method: "fast-forward"}); KindOf(err) != ErrInvalid {
		t.Errorf("method: %v", err)
	}
	if _, err := g.Reply(bg, cred, repo, 7, "../../x", "hi"); KindOf(err) != ErrInvalid {
		t.Errorf("reply id: %v", err)
	}
	if len(f.requests("")) != 0 {
		t.Errorf("something was sent: %v", f.requests(""))
	}
}

func TestCommentsMergeConversationAndThreads(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /graphql", func(w http.ResponseWriter, _ *http.Request, body []byte) {
		if !strings.Contains(string(body), "reviewThreads") {
			t.Errorf("query %s", body)
		}
		w.Write([]byte(`{"data":{"repository":{"pullRequest":{
		 "comments":{"nodes":[{"databaseId":11,"author":{"login":"bob"},"body":"LGTM overall","createdAt":"2026-10-16T09:00:00Z","url":"https://x/11"}]},
		 "reviewThreads":{"nodes":[
		  {"id":"T1","isResolved":false,"isOutdated":false,"path":"a.go","line":42,"originalLine":40,"comments":{"nodes":[
		    {"databaseId":21,"author":{"login":"carol"},"body":"nil check?","createdAt":"2026-10-16T09:01:00Z","url":"https://x/21"},
		    {"databaseId":22,"author":null,"body":"done","createdAt":"2026-10-16T09:02:00Z","url":"https://x/22","replyTo":{"databaseId":21}}]}},
		  {"id":"T2","isResolved":true,"isOutdated":true,"path":"b.go","line":0,"originalLine":7,"comments":{"nodes":[
		    {"databaseId":31,"author":{"login":"dan"},"body":"rename","createdAt":"2026-10-16T09:03:00Z","url":"https://x/31"}]}}]}}}}}`))
	})
	comments, err := f.provider().Comments(bg, cred, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 4 {
		t.Fatalf("%+v", comments)
	}
	conv, first, reply, resolved := comments[0], comments[1], comments[2], comments[3]
	if conv.Kind != CommentConversation || conv.Author != "bob" || conv.ID != "11" {
		t.Errorf("%+v", conv)
	}
	if first.Path != "a.go" || first.Line != 42 || first.Resolved || first.ThreadID != "T1" {
		t.Errorf("%+v", first)
	}
	if reply.ReplyTo != "21" || reply.Author != "ghost" {
		t.Errorf("%+v", reply)
	}
	if !resolved.Resolved || !resolved.Outdated || resolved.Line != 7 {
		t.Errorf("%+v", resolved)
	}
	if got := f.requests("POST /graphql")[0].Auth; got != "Bearer ghp_secret_token" {
		t.Errorf("auth %q", got)
	}
}

func TestUnresolvedThreadsAndGraphQLErrors(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /graphql", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":false},{"isResolved":true},{"isResolved":false}]}}}}}`))
	})
	n, err := f.provider().UnresolvedThreads(bg, cred, repo, 7)
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	f.handle("POST /graphql", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Write([]byte(`{"data":null,"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`))
	})
	if _, err := f.provider().UnresolvedThreads(bg, cred, repo, 7); KindOf(err) != ErrNotFound {
		t.Fatalf("%v", err)
	}
}

func decodeBody(t *testing.T, r recorded) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("body %q: %v", r.Body, err)
	}
	return m
}

func TestCreateSendsTheDraftAndReturnsTheNumber(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /repos/acme/widgets/pulls", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(samplePull(map[string]any{"draft": true}))
	})
	cr, err := f.provider().Create(bg, cred, repo, CreateInput{Title: "Add retries", Body: "why", Head: "feat/retries", Base: "main", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number != 7 || !cr.Draft {
		t.Errorf("%+v", cr)
	}
	body := decodeBody(t, f.requests("POST /repos/acme/widgets/pulls")[0])
	if body["head"] != "feat/retries" || body["base"] != "main" || body["draft"] != true || body["title"] != "Add retries" || body["body"] != "why" {
		t.Errorf("sent %v", body)
	}
	if _, err := f.provider().Create(bg, cred, repo, CreateInput{Head: "x", Base: "y"}); KindOf(err) != ErrInvalid {
		t.Errorf("a title is required: %v", err)
	}
}

func TestUpdateSendsOnlyWhatChanged(t *testing.T) {
	f := newFakeHost(t)
	f.handle("PATCH "+pullBase, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		json.NewEncoder(w).Encode(samplePull(nil))
	})
	title := "New title"
	if _, err := f.provider().Update(bg, cred, repo, 7, UpdateInput{Title: &title}); err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, f.requests("PATCH " + pullBase)[0])
	if _, has := body["body"]; has || body["title"] != "New title" {
		t.Errorf("sent %v", body)
	}
	if _, err := f.provider().Update(bg, cred, repo, 7, UpdateInput{}); KindOf(err) != ErrInvalid {
		t.Errorf("an empty update: %v", err)
	}
}

func TestCommentAndReplyUseTheRightEndpoints(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /repos/acme/widgets/issues/7/comments", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":501,"body":"thanks","html_url":"https://x/501","user":{"login":"syuro"},"created_at":"2026-10-16T10:00:00Z"}`))
	})
	f.handle("POST "+pullBase+"/comments/21/replies", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":502,"body":"fixed","html_url":"https://x/502","user":{"login":"syuro"},"path":"a.go","line":42,"in_reply_to_id":21}`))
	})
	g := f.provider()
	c, err := g.Comment(bg, cred, repo, 7, "thanks")
	if err != nil || c.ID != "501" || c.Kind != CommentConversation {
		t.Fatalf("%+v %v", c, err)
	}
	r, err := g.Reply(bg, cred, repo, 7, "21", "fixed")
	if err != nil || r.ID != "502" || r.ReplyTo != "21" || r.Kind != CommentReview || r.Path != "a.go" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Comment(bg, cred, repo, 7, "  "); KindOf(err) != ErrInvalid {
		t.Errorf("empty comment: %v", err)
	}
}

func TestMergeSendsMethodAndTheShaItWasConfirmedAgainst(t *testing.T) {
	f := newFakeHost(t)
	f.handle("PUT "+pullBase+"/merge", func(w http.ResponseWriter, _ *http.Request, body []byte) {
		w.Write([]byte(`{"merged":true,"sha":"deadbeef","message":"Pull Request successfully merged"}`))
	})
	res, err := f.provider().Merge(bg, cred, repo, 7, MergeInput{Method: MergeMethodSquash, Title: "Add retries (#7)", SHA: "abc123"})
	if err != nil || !res.Merged || res.SHA != "deadbeef" {
		t.Fatalf("%+v %v", res, err)
	}
	body := decodeBody(t, f.requests("PUT " + pullBase + "/merge")[0])
	if body["merge_method"] != "squash" || body["sha"] != "abc123" || body["commit_title"] != "Add retries (#7)" {
		t.Errorf("sent %v", body)
	}
	if _, has := body["commit_message"]; has {
		t.Errorf("an empty body must keep the host's default: %v", body)
	}
}

func TestMergeRefusalsAreValidationErrors(t *testing.T) {
	for code, message := range map[int]string{405: "Pull Request is not mergeable", 409: "Head branch was modified. Review and try the merge again."} {
		f := newFakeHost(t)
		f.status("PUT "+pullBase+"/merge", code, message, nil)
		_, err := f.provider().Merge(bg, cred, repo, 7, MergeInput{Method: MergeMethodMerge, SHA: "abc123"})
		if KindOf(err) != ErrValidation || !strings.Contains(err.Error(), message) {
			t.Errorf("%d: %v", code, err)
		}
	}
}

func TestCloseOnlyClosesAndDeleteBranchEscapesTheName(t *testing.T) {
	f := newFakeHost(t)
	f.handle("PATCH "+pullBase, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		json.NewEncoder(w).Encode(samplePull(map[string]any{"state": "closed"}))
	})
	f.handle("DELETE /repos/acme/widgets/git/refs/heads/feat/re tries", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(204)
	})
	g := f.provider()
	cr, err := g.Close(bg, cred, repo, 7)
	if err != nil || cr.State != StateClosed {
		t.Fatalf("%+v %v", cr, err)
	}
	if body := decodeBody(t, f.requests("PATCH " + pullBase)[0]); len(body) != 1 || body["state"] != "closed" {
		t.Errorf("close sent %v", body)
	}
	if err := g.DeleteBranch(bg, cred, repo, "feat/re tries"); err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteBranch(bg, cred, repo, " "); KindOf(err) != ErrInvalid {
		t.Errorf("no name: %v", err)
	}
}

func TestBranchReportsDefaultAndProtected(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /repos/acme/widgets", map[string]any{"default_branch": "main"})
	f.json("GET /repos/acme/widgets/branches/release/1.x", map[string]any{"protected": true})
	f.json("GET /repos/acme/widgets/branches/main", map[string]any{"protected": false})
	g := f.provider()
	b, err := g.Branch(bg, cred, repo, "release/1.x")
	if err != nil || b.Default || !b.Protected {
		t.Fatalf("%+v %v", b, err)
	}
	b, err = g.Branch(bg, cred, repo, "main")
	if err != nil || !b.Default {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestStatusesWithoutCheckRunsStillCount(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+pullBase, samplePull(nil))
	serveChecks(f, nil, []map[string]any{{"state": "success", "context": "jenkins"}, {"state": "failure", "context": "sonar", "description": "quality gate"}})
	serveReviews(f, nil)
	cr, err := f.provider().Get(bg, cred, repo, 7)
	if err != nil || cr.Checks.Total != 2 || cr.Checks.Failed != 1 || cr.Checks.Failing[0].Summary != "quality gate" {
		t.Fatalf("%+v %v", cr.Checks, err)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(NewGitHub())
	if r.For("github.com") == nil || r.For("gitlab.com") != nil || r.For("github.com.evil.test") != nil {
		t.Error("host routing")
	}
	if p, repo, n, ok := r.ParseURL("https://github.com/a/b/pull/3"); !ok || p.Kind() != "github" || repo.Path != "a/b" || n != 3 {
		t.Error("ParseURL")
	}
	var none *Registry
	if none.For("github.com") != nil {
		t.Error("nil registry")
	}
}

func TestSummarizeClipsFailingList(t *testing.T) {
	var checks []Check
	for i := 0; i < 15; i++ {
		checks = append(checks, Check{Name: fmt.Sprint("job", i), Status: CheckFailed})
	}
	s := summarize(checks)
	if s.Failed != 15 || len(s.Failing) != maxFailingChecks {
		t.Errorf("%+v", s)
	}
	if !(CheckSummary{}).Settled() || (CheckSummary{Running: 1}).Settled() {
		t.Error("Settled")
	}
}
