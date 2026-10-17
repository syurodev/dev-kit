package forge

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// glFake is a GitLab stand-in. Routes are "METHOD escaped-path": the escaped
// path is what proves a nested group went out as group%2Fsub%2Frepo.
type glFake struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]func(w http.ResponseWriter, r *http.Request, body []byte)
	log    []glRecorded
}

type glRecorded struct {
	Method, Path, Query, PrivateToken, Authorization, IfNoneMatch string
	Body                                                          []byte
}

func newGLFake(t *testing.T) *glFake {
	f := &glFake{t: t, routes: map[string]func(http.ResponseWriter, *http.Request, []byte){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.log = append(f.log, glRecorded{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("PRIVATE-TOKEN"), r.Header.Get("Authorization"), r.Header.Get("If-None-Match"), body})
		route := f.routes[r.Method+" "+r.URL.EscapedPath()]
		f.mu.Unlock()
		if route == nil {
			http.Error(w, `{"message":"404 Not found"}`, http.StatusNotFound)
			return
		}
		route(w, r, body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *glFake) provider(opts ...GitLabOption) *GitLab {
	return NewGitLab(append([]GitLabOption{WithGitLabRoot(f.srv.URL)}, opts...)...)
}

func (f *glFake) handle(route string, fn func(http.ResponseWriter, *http.Request, []byte)) {
	f.mu.Lock()
	f.routes[route] = fn
	f.mu.Unlock()
}

func (f *glFake) json(route string, v any) {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	etag := fmt.Sprintf(`W/"%x"`, sum[:8])
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

// reply serves v for a write, with the status.
func (f *glFake) reply(route string, status int, v any) {
	raw, _ := json.Marshal(v)
	f.handle(route, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(raw)
	})
}

func (f *glFake) fail(route string, status int, body string, headers map[string]string) {
	f.handle(route, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
}

func (f *glFake) calls(route string) []glRecorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []glRecorded
	for _, r := range f.log {
		if r.Method+" "+r.Path == route {
			out = append(out, r)
		}
	}
	return out
}

func (f *glFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.log)
}

var (
	glRepo = Repo{Host: "gitlab.com", Path: "acme/platform/billing-service"}
	glCred = Credential{Token: "glpat-super-secret-token"}
)

const (
	glProject = "/api/v4/projects/acme%2Fplatform%2Fbilling-service"
	glMRPath  = glProject + "/merge_requests/7"
)

func glSampleMR(extra map[string]any) map[string]any {
	mr := map[string]any{
		"iid": 7, "title": "Add retries", "description": "Retries the call.", "state": "opened",
		"draft": false, "web_url": "https://gitlab.com/acme/platform/billing-service/-/merge_requests/7",
		"source_branch": "feat/retries", "target_branch": "main", "source_project_id": 11, "target_project_id": 11,
		"sha": "abc123", "updated_at": "2026-10-17T10:00:00Z", "merge_status": "can_be_merged",
		"detailed_merge_status": "mergeable", "has_conflicts": false, "changes_count": "5",
		"author":        map[string]any{"username": "chiro"},
		"head_pipeline": map[string]any{"id": 99, "status": "success", "web_url": "https://gitlab.com/p/99", "sha": "abc123"},
	}
	for k, v := range extra {
		mr[k] = v
	}
	return mr
}

func serveJobs(f *glFake, jobs ...map[string]any) {
	f.json("GET "+glProject+"/pipelines/99/jobs", jobs)
}

func jobOf(name, status string) map[string]any {
	return map[string]any{"name": name, "stage": "test", "status": status, "web_url": "https://gitlab.com/j/" + name}
}

func glStats(f *glFake, add, del, files int) {
	f.json("POST /api/graphql", map[string]any{"data": map[string]any{"project": map[string]any{"mergeRequest": map[string]any{
		"diffStatsSummary": map[string]any{"additions": add, "deletions": del, "fileCount": files}}}}})
}

// serveFullMR serves everything Get reads for a passing request.
func serveFullMR(f *glFake) {
	f.json("GET "+glMRPath, glSampleMR(nil))
	serveJobs(f, jobOf("lint", "success"), jobOf("unit", "success"))
	f.json("GET "+glMRPath+"/approvals", map[string]any{"approved_by": []any{}})
	glStats(f, 120, 14, 5)
}

func TestGitLabServesAndParsesAddresses(t *testing.T) {
	g := NewGitLab()
	if !g.Serves("gitlab.com") || !g.Serves("GitLab.com.") {
		t.Error("gitlab.com must be served")
	}
	for _, host := range []string{"github.com", "git.example.com", "evil-gitlab.com", "gitlab.com.evil.net"} {
		if g.Serves(host) {
			t.Errorf("%s must not be served: a token is only sent to the host it was issued for", host)
		}
	}
	if !NewGitLab(WithGitLabHosts("git.example.com")).Serves("git.example.com") {
		t.Error("WithGitLabHosts adds a host")
	}
	cases := []struct {
		raw  string
		repo string
		n    int
		ok   bool
	}{
		{"https://gitlab.com/acme/platform/billing-service/-/merge_requests/12", "acme/platform/billing-service", 12, true},
		{"https://gitlab.com/acme/billing/-/merge_requests/3/diffs", "acme/billing", 3, true},
		{"https://gitlab.com/acme/billing/merge_requests/9", "acme/billing", 9, true},
		{"  https://gitlab.com/acme/billing/-/merge_requests/4/  ", "acme/billing", 4, true},
		{"https://gitlab.com/merge_requests/4", "", 0, false},
		{"https://gitlab.com/acme/billing/-/issues/4", "", 0, false},
		{"https://gitlab.com/acme/../billing/-/merge_requests/4", "", 0, false},
		{"https://github.com/acme/billing/pull/4", "", 0, false},
		{"https://git.example.com/acme/billing/-/merge_requests/4", "", 0, false},
		{"ssh://gitlab.com/acme/billing/-/merge_requests/4", "", 0, false},
	}
	for _, c := range cases {
		repo, n, ok := g.ParseURL(c.raw)
		if ok != c.ok || repo.Path != c.repo || n != c.n || (ok && repo.Host != "gitlab.com") {
			t.Errorf("ParseURL(%q) = %v %d %v", c.raw, repo, n, ok)
		}
	}
	if terms := g.Terms(); terms.Short != "MR" || terms.Checks != "pipelines" || terms.Long != "merge request" || terms.Platform != "GitLab" {
		t.Errorf("terms = %+v", terms)
	}
}

func TestRegistryPicksTheProviderByHost(t *testing.T) {
	r := NewRegistry(NewGitHub(), NewGitLab())
	if r.For("gitlab.com").Kind() != "gitlab" || r.For("github.com").Kind() != "github" || r.For("git.example.com") != nil {
		t.Error("registry mixed the hosts up")
	}
	p, repo, n, ok := r.ParseURL("https://gitlab.com/a/b/-/merge_requests/5")
	if !ok || p.Kind() != "gitlab" || repo.Path != "a/b" || n != 5 {
		t.Errorf("ParseURL = %v %v %d %v", p, repo, n, ok)
	}
	p, _, _, ok = r.ParseURL("https://github.com/a/b/pull/5")
	if !ok || p.Kind() != "github" {
		t.Error("a pull request address must still reach GitHub")
	}
}

func TestGitLabNestedGroupIsOneEncodedSegmentAndAuthIsPrivateToken(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glProject, map[string]any{"default_branch": "develop"})
	branch, err := f.provider().DefaultBranch(bg, glCred, glRepo)
	if err != nil || branch != "develop" {
		t.Fatalf("DefaultBranch = %q, %v", branch, err)
	}
	calls := f.calls("GET " + glProject)
	if len(calls) != 1 {
		t.Fatalf("the project was not asked for as one encoded segment: %+v", f.log)
	}
	if calls[0].PrivateToken != glCred.Token || calls[0].Authorization != "" {
		t.Errorf("auth must be the PRIVATE-TOKEN header only: %+v", calls[0])
	}
}

func TestGitLabRefusesAHostItDoesNotServeAndABadPath(t *testing.T) {
	f := newGLFake(t)
	g := f.provider()
	for _, repo := range []Repo{
		{Host: "git.example.com", Path: "a/b"},
		{Host: "gitlab.com", Path: "justone"},
		{Host: "gitlab.com", Path: "a//b"},
		{Host: "gitlab.com", Path: "a/../b"},
	} {
		if _, err := g.DefaultBranch(bg, glCred, repo); KindOf(err) != ErrInvalid {
			t.Errorf("%v: err = %v, want invalid", repo, err)
		}
	}
	if f.count() != 0 {
		t.Error("nothing may be sent for an invalid repository")
	}
	if _, err := g.DefaultBranch(bg, Credential{}, glRepo); KindOf(err) != ErrUnauthorized {
		t.Errorf("no token: %v", err)
	}
	if f.count() != 0 {
		t.Error("a call without a token must not reach the host")
	}
}

func TestGitLabFindBySourceBranchSeparatesForks(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glProject+"/merge_requests", []map[string]any{
		glSampleMR(map[string]any{"iid": 8, "state": "merged"}),
		glSampleMR(map[string]any{"iid": 9, "source_project_id": 55}),
	})
	g := f.provider()
	list, err := g.Find(bg, glCred, glRepo, Head{Owner: "acme", Branch: "feat/retries"})
	if err != nil || len(list) != 1 || list[0].Number != 8 || list[0].State != StateMerged || list[0].HeadRepo != glRepo.Path {
		t.Fatalf("same-project list = %+v, %v", list, err)
	}
	calls := f.calls("GET " + glProject + "/merge_requests")
	for _, want := range []string{"source_branch=feat%2Fretries", "state=all", "order_by=updated_at"} {
		if !strings.Contains(calls[0].Query, want) {
			t.Errorf("query %q lacks %s", calls[0].Query, want)
		}
	}
	if list[0].ChecksKnown || list[0].UnresolvedThreads != -1 || list[0].Terms.Short != "MR" {
		t.Errorf("a list result is light: %+v", list[0])
	}
	forks, err := g.Find(bg, glCred, glRepo, Head{Owner: "someone", Branch: "feat/retries"})
	if err != nil || len(forks) != 1 || forks[0].Number != 9 || forks[0].HeadRepo != "project:55" {
		t.Fatalf("fork list = %+v, %v", forks, err)
	}
	if _, err := g.Find(bg, glCred, glRepo, Head{}); KindOf(err) != ErrInvalid {
		t.Errorf("no branch: %v", err)
	}
}

func TestGitLabGetReadsTheMergeRequestPipelineApprovalsAndLineCounts(t *testing.T) {
	f := newGLFake(t)
	serveFullMR(f)
	cr, err := f.provider().Get(bg, glCred, glRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number != 7 || cr.Title != "Add retries" || cr.State != StateOpen || cr.Draft || cr.Author != "chiro" ||
		cr.Head != "feat/retries" || cr.Base != "main" || cr.HeadSHA != "abc123" || cr.HeadRepo != glRepo.Path ||
		cr.Mergeable != MergeClean || cr.Body != "Retries the call." || !cr.ChecksKnown ||
		cr.Additions != 120 || cr.Deletions != 14 || cr.ChangedFiles != 5 || cr.UnresolvedThreads != -1 {
		t.Errorf("cr = %+v", cr)
	}
	if cr.Checks.Total != 2 || cr.Checks.Passed != 2 || cr.Terms.Checks != "pipelines" || len(cr.Notes) != 0 {
		t.Errorf("checks = %+v notes=%v", cr.Checks, cr.Notes)
	}
	if cr.Decision != DecisionNone {
		t.Errorf("decision = %q", cr.Decision)
	}
}

func TestGitLabGetNamesWhatItCouldNotRead(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(nil))
	f.fail("GET "+glProject+"/pipelines/99/jobs", 403, `{"message":"403 Forbidden"}`, nil)
	f.fail("GET "+glMRPath+"/approvals", 404, `{"message":"404 Not Found"}`, nil)
	f.fail("POST /api/graphql", 500, `oops`, nil)
	cr, err := f.provider().Get(bg, glCred, glRepo, 7)
	if err != nil {
		t.Fatalf("a part the token cannot read must not fail the whole read: %v", err)
	}
	if cr.ChecksKnown || len(cr.Notes) != 2 {
		t.Errorf("checks known=%v notes=%v; want pipelines + line counts named (approvals 404 is a tier without them)", cr.ChecksKnown, cr.Notes)
	}
	if cr.Title != "Add retries" {
		t.Errorf("the rest of the card must survive: %+v", cr)
	}
}

func TestGitLabLineCountsAreReadOncePerHeadCommit(t *testing.T) {
	f := newGLFake(t)
	serveFullMR(f)
	g := f.provider()
	for i := 0; i < 3; i++ {
		if _, err := g.Get(bg, glCred, glRepo, 7); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.calls("POST /api/graphql")); n != 1 {
		t.Errorf("diff stats asked %d times for one head commit; want 1", n)
	}
	f.json("GET "+glMRPath, glSampleMR(map[string]any{"sha": "def456"}))
	glStats(f, 1, 2, 3)
	cr, _ := g.Get(bg, glCred, glRepo, 7)
	if cr.Additions != 1 || cr.Deletions != 2 || len(f.calls("POST /api/graphql")) != 2 {
		t.Errorf("a moved branch must be re-read: %+v", cr)
	}
}

func TestGitLabMergeableMapping(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  Mergeable
	}{
		{"mergeable", map[string]any{"detailed_merge_status": "mergeable"}, MergeClean},
		{"conflict", map[string]any{"detailed_merge_status": "conflict", "has_conflicts": true}, MergeConflicting},
		{"need rebase", map[string]any{"detailed_merge_status": "need_rebase"}, MergeBehind},
		{"checking", map[string]any{"detailed_merge_status": "checking"}, MergeUnknown},
		{"unchecked", map[string]any{"detailed_merge_status": "unchecked"}, MergeUnknown},
		{"ci must pass", map[string]any{"detailed_merge_status": "ci_must_pass"}, MergeBlocked},
		{"discussions", map[string]any{"detailed_merge_status": "discussions_not_resolved"}, MergeBlocked},
		{"draft", map[string]any{"detailed_merge_status": "draft_status", "draft": true}, MergeBlocked},
		{"not approved", map[string]any{"detailed_merge_status": "not_approved"}, MergeBlocked},
		{"blocked and conflicting", map[string]any{"detailed_merge_status": "ci_must_pass", "has_conflicts": true}, MergeConflicting},
		{"old host can", map[string]any{"detailed_merge_status": "", "merge_status": "can_be_merged"}, MergeClean},
		{"old host unresolved threads", map[string]any{"detailed_merge_status": "", "merge_status": "can_be_merged", "blocking_discussions_resolved": false}, MergeBlocked},
		{"old host cannot", map[string]any{"detailed_merge_status": "", "merge_status": "cannot_be_merged", "has_conflicts": true}, MergeConflicting},
		{"old host cannot, no conflict", map[string]any{"detailed_merge_status": "", "merge_status": "cannot_be_merged"}, MergeBlocked},
		{"old host checking", map[string]any{"detailed_merge_status": "", "merge_status": "checking"}, MergeUnknown},
	}
	for _, c := range cases {
		f := newGLFake(t)
		f.json("GET "+glMRPath, glSampleMR(c.extra))
		cr, err := f.provider().Get(bg, glCred, glRepo, 7)
		if err != nil || cr.Mergeable != c.want {
			t.Errorf("%s: mergeable = %q (%v), want %q", c.name, cr.Mergeable, err, c.want)
		}
	}
}

func TestGitLabDraftStatesAndRequestedChanges(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(map[string]any{"work_in_progress": true, "detailed_merge_status": "requested_changes"}))
	serveJobs(f)
	glStats(f, 1, 1, 1)
	cr, err := f.provider().Get(bg, glCred, glRepo, 7)
	if err != nil || !cr.Draft || cr.Decision != DecisionChangesRequested || cr.Mergeable != MergeBlocked {
		t.Fatalf("cr = %+v, %v", cr, err)
	}
	if len(f.calls("GET "+glMRPath+"/approvals")) != 0 {
		t.Error("requested_changes already says it; approvals need not be asked")
	}

	f2 := newGLFake(t)
	serveFullMR(f2)
	f2.json("GET "+glMRPath+"/approvals", map[string]any{"approved_by": []any{map[string]any{"user": map[string]any{"username": "lead"}}}})
	cr, _ = f2.provider().Get(bg, glCred, glRepo, 7)
	if cr.Decision != DecisionApproved {
		t.Errorf("decision = %q", cr.Decision)
	}
}

func TestGitLabMergedAndClosedAndLockedStates(t *testing.T) {
	for state, want := range map[string]State{"merged": StateMerged, "closed": StateClosed, "opened": StateOpen, "locked": StateOpen} {
		f := newGLFake(t)
		serveFullMR(f)
		f.json("GET "+glMRPath, glSampleMR(map[string]any{"state": state}))
		cr, err := f.provider().Get(bg, glCred, glRepo, 7)
		if err != nil || cr.State != want {
			t.Errorf("%s: state = %q, %v", state, cr.State, err)
		}
	}
}

func TestGitLabPipelineJobsBecomeChecks(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(nil))
	failed := jobOf("unit", "failed")
	failed["failure_reason"] = "script_failure"
	allowed := jobOf("flaky", "failed")
	allowed["allow_failure"] = true
	serveJobs(f, jobOf("lint", "success"), failed, jobOf("e2e", "running"), jobOf("deploy", "manual"), jobOf("scan", "skipped"),
		jobOf("queued", "pending"), jobOf("stopped", "canceled"), allowed)
	checks, err := f.provider().Checks(bg, glCred, glRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Check{}
	for _, ch := range checks {
		got[ch.Name] = ch
	}
	want := map[string]CheckStatus{"lint": CheckPassed, "unit": CheckFailed, "e2e": CheckRunning, "deploy": CheckSkipped,
		"scan": CheckSkipped, "queued": CheckRunning, "stopped": CheckFailed, "flaky": CheckSkipped}
	for name, status := range want {
		if got[name].Status != status {
			t.Errorf("%s = %q, want %q", name, got[name].Status, status)
		}
	}
	if got["unit"].Summary != "script failure" || got["flaky"].Conclusion != "failed (allowed)" || got["unit"].URL == "" {
		t.Errorf("details: %+v %+v", got["unit"], got["flaky"])
	}
	if checks[0].Status != CheckFailed || checks[len(checks)-1].Status != CheckSkipped {
		t.Errorf("failures first, skipped last: %+v", checks)
	}
}

func TestGitLabPipelineWithoutJobsIsStillReported(t *testing.T) {
	for status, want := range map[string]CheckStatus{"created": CheckRunning, "pending": CheckRunning, "failed": CheckFailed, "success": CheckPassed} {
		f := newGLFake(t)
		f.json("GET "+glMRPath, glSampleMR(map[string]any{"head_pipeline": map[string]any{"id": 99, "status": status}}))
		serveJobs(f)
		checks, err := f.provider().Checks(bg, glCred, glRepo, 7)
		if err != nil || len(checks) != 1 || checks[0].Name != "pipeline" || checks[0].Status != want {
			t.Errorf("%s: %+v, %v", status, checks, err)
		}
	}
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(map[string]any{"head_pipeline": nil}))
	checks, err := f.provider().Checks(bg, glCred, glRepo, 7)
	if err != nil || len(checks) != 0 {
		t.Errorf("no pipeline = no checks: %+v, %v", checks, err)
	}
}

func TestGitLabPaginatesByLinkHeaderAndByXNextPage(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(nil))
	jobsRoute := "GET " + glProject + "/pipelines/99/jobs"
	f.handle(jobsRoute, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", `<`+f.srv.URL+glProject+`/pipelines/99/jobs?per_page=100&page=2>; rel="next", <x>; rel="last"`)
			io.WriteString(w, `[{"name":"a","status":"success"}]`)
		case "2":
			// A proxy dropped the Link header; X-Next-Page is all that is left.
			w.Header().Set("X-Next-Page", "3")
			io.WriteString(w, `[{"name":"b","status":"failed"}]`)
		default:
			io.WriteString(w, `[{"name":"c","status":"running"}]`)
		}
	})
	checks, err := f.provider().Checks(bg, glCred, glRepo, 7)
	if err != nil || len(checks) != 3 {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	if n := len(f.calls(jobsRoute)); n != 3 {
		t.Errorf("pages fetched = %d", n)
	}
}

func TestGitLabNeverFollowsAPagingLinkToAnotherHost(t *testing.T) {
	other := newGLFake(t)
	other.json("GET /steal", []map[string]any{{"name": "x", "status": "success"}})
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(nil))
	f.handle("GET "+glProject+"/pipelines/99/jobs", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Link", `<`+other.srv.URL+`/steal>; rel="next"`)
		io.WriteString(w, `[{"name":"a","status":"success"}]`)
	})
	_, err := f.provider().Checks(bg, glCred, glRepo, 7)
	if KindOf(err) != ErrInvalid {
		t.Errorf("err = %v, want invalid", err)
	}
	if other.count() != 0 {
		t.Errorf("the token was sent to another host: %+v", other.log)
	}
}

func TestGitLabNeverFollowsARedirectWithTheToken(t *testing.T) {
	other := newGLFake(t)
	other.json("GET "+glProject, map[string]any{"default_branch": "stolen"})
	f := newGLFake(t)
	f.handle("GET "+glProject, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		http.Redirect(w, r, other.srv.URL+glProject, http.StatusFound)
	})
	_, err := f.provider().DefaultBranch(bg, glCred, glRepo)
	if KindOf(err) != ErrUnavailable {
		t.Errorf("err = %v", err)
	}
	if other.count() != 0 {
		t.Errorf("the redirect was followed with the token: %+v", other.log)
	}
	if err != nil && strings.Contains(err.Error(), glCred.Token) {
		t.Error("the token is in the error")
	}
}

func TestGitLabCommentsAndThreads(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath+"/discussions", []map[string]any{
		{"id": "n1", "individual_note": true, "notes": []map[string]any{
			{"id": 100, "body": "Looks good", "author": map[string]any{"username": "lead"}, "created_at": "2026-10-17T09:00:00Z", "system": false}}},
		{"id": strings.Repeat("a", 40), "individual_note": false, "notes": []map[string]any{
			{"id": 201, "body": "Why a retry here?", "author": map[string]any{"username": "lead"}, "resolvable": true, "resolved": false,
				"position": map[string]any{"new_path": "svc/call.go", "new_line": 42}, "created_at": "2026-10-17T09:05:00Z"},
			{"id": 202, "body": "Because of 503s", "author": map[string]any{"username": "chiro"}, "resolvable": true, "resolved": false,
				"position": map[string]any{"new_path": "svc/call.go", "new_line": 42}, "created_at": "2026-10-17T09:06:00Z"}}},
		{"id": strings.Repeat("b", 40), "individual_note": false, "notes": []map[string]any{
			{"id": 301, "body": "nit", "author": map[string]any{"username": "lead"}, "resolvable": true, "resolved": true,
				"position": map[string]any{"new_path": "a.go", "new_line": 1}, "created_at": "2026-10-17T09:07:00Z"}}},
		{"id": strings.Repeat("c", 40), "individual_note": false, "notes": []map[string]any{
			{"id": 401, "body": "merged by system", "system": true, "author": map[string]any{"username": "bot"}}}},
		{"id": strings.Repeat("d", 40), "individual_note": false, "notes": []map[string]any{
			{"id": 501, "body": "thread on the request", "author": map[string]any{"username": "lead"}, "resolvable": true, "resolved": false, "created_at": "2026-10-17T09:08:00Z"}}},
	})
	g := f.provider()
	comments, err := g.Comments(bg, glCred, glRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Comment{}
	for _, c := range comments {
		by[c.ID] = c
	}
	if len(comments) != 5 {
		t.Fatalf("system notes are not comments: %+v", comments)
	}
	if c := by["100"]; c.Kind != CommentConversation || c.Author != "lead" || c.ThreadID != "" ||
		c.URL != "https://gitlab.com/acme/platform/billing-service/-/merge_requests/7#note_100" {
		t.Errorf("conversation: %+v", c)
	}
	if c := by["201"]; c.Kind != CommentReview || c.Path != "svc/call.go" || c.Line != 42 || c.Resolved || c.ThreadID != strings.Repeat("a", 40) || c.ReplyTo != "" {
		t.Errorf("thread root: %+v", c)
	}
	if c := by["202"]; c.ReplyTo != "201" || c.Resolved {
		t.Errorf("thread reply: %+v", c)
	}
	if c := by["301"]; !c.Resolved || c.Kind != CommentReview {
		t.Errorf("resolved: %+v", c)
	}
	if c := by["501"]; c.Kind != CommentReview || c.Path != "" || c.Resolved {
		t.Errorf("a resolvable discussion on the request is a thread: %+v", c)
	}
	open, err := g.UnresolvedThreads(bg, glCred, glRepo, 7)
	if err != nil || open != 2 {
		t.Errorf("unresolved = %d, %v; want 2 (the diff thread and the request thread)", open, err)
	}
}

func TestGitLabReply(t *testing.T) {
	f := newGLFake(t)
	disc := strings.Repeat("a", 40)
	f.json("GET "+glMRPath+"/discussions", []map[string]any{
		{"id": "ind", "individual_note": true, "notes": []map[string]any{{"id": 100, "body": "hi"}}},
		{"id": disc, "individual_note": false, "notes": []map[string]any{
			{"id": 201, "body": "q", "resolvable": true, "position": map[string]any{"new_path": "a.go", "new_line": 3}}}},
	})
	f.reply("POST "+glMRPath+"/discussions/"+disc+"/notes", 201, map[string]any{
		"id": 202, "body": "answer", "author": map[string]any{"username": "chiro"}, "created_at": "2026-10-17T10:00:00Z",
		"position": map[string]any{"new_path": "a.go", "new_line": 3}})
	g := f.provider()

	cm, err := g.Reply(bg, glCred, glRepo, 7, "201", "answer")
	if err != nil || cm.ID != "202" || cm.Kind != CommentReview || cm.ThreadID != disc || cm.Path != "a.go" || cm.Line != 3 {
		t.Fatalf("reply = %+v, %v", cm, err)
	}
	posted := f.calls("POST " + glMRPath + "/discussions/" + disc + "/notes")
	if len(posted) != 1 || string(posted[0].Body) != `{"body":"answer"}` {
		t.Errorf("posted = %+v", posted)
	}
	if _, err := g.Reply(bg, glCred, glRepo, 7, disc, "by discussion id"); err != nil {
		t.Errorf("a discussion id is accepted: %v", err)
	}
	if _, err := g.Reply(bg, glCred, glRepo, 7, "100", "x"); KindOf(err) != ErrInvalid {
		t.Errorf("a conversation comment is not a thread: %v", err)
	}
	if _, err := g.Reply(bg, glCred, glRepo, 7, "999", "x"); KindOf(err) != ErrNotFound {
		t.Errorf("unknown note: %v", err)
	}
	for _, bad := range []string{"abc", "0", "-3", ""} {
		if _, err := g.Reply(bg, glCred, glRepo, 7, bad, "x"); KindOf(err) != ErrInvalid {
			t.Errorf("id %q: %v", bad, err)
		}
	}
	if _, err := g.Reply(bg, glCred, glRepo, 7, "201", "  "); KindOf(err) != ErrInvalid {
		t.Errorf("empty body: %v", err)
	}
}

func TestGitLabDiscussionsAreBoundedToThreePages(t *testing.T) {
	f := newGLFake(t)
	route := "GET " + glMRPath + "/discussions"
	f.handle(route, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Link", `<`+f.srv.URL+glMRPath+`/discussions?page=9>; rel="next"`)
		io.WriteString(w, `[]`)
	})
	if _, err := f.provider().UnresolvedThreads(bg, glCred, glRepo, 7); err != nil {
		t.Fatal(err)
	}
	if n := len(f.calls(route)); n != maxPages {
		t.Errorf("pages = %d, want %d", n, maxPages)
	}
}

func TestGitLabCreate(t *testing.T) {
	f := newGLFake(t)
	f.reply("POST "+glProject+"/merge_requests", 201, glSampleMR(map[string]any{"iid": 12, "title": "Draft: Add retries"}))
	g := f.provider()
	cr, err := g.Create(bg, glCred, glRepo, CreateInput{Title: "Add retries", Body: "why", Head: "feat/retries", Base: "main", Draft: true})
	if err != nil || cr.Number != 12 || cr.Terms.Short != "MR" {
		t.Fatalf("create = %+v, %v", cr, err)
	}
	var sent map[string]any
	json.Unmarshal(f.calls("POST " + glProject + "/merge_requests")[0].Body, &sent)
	if sent["source_branch"] != "feat/retries" || sent["target_branch"] != "main" || sent["title"] != "Draft: Add retries" || sent["description"] != "why" {
		t.Errorf("sent = %v", sent)
	}
	if _, ok := sent["remove_source_branch"]; ok {
		t.Error("remove_source_branch must not be sent")
	}
	if _, err := g.Create(bg, glCred, glRepo, CreateInput{Title: "Draft: already", Head: "x", Base: "main", Draft: true}); err != nil {
		t.Fatal(err)
	}
	last := f.calls("POST " + glProject + "/merge_requests")[1]
	if !strings.Contains(string(last.Body), `"title":"Draft: already"`) {
		t.Errorf("the draft prefix must not double: %s", last.Body)
	}
	if _, err := g.Create(bg, glCred, glRepo, CreateInput{Title: "t", Head: "someone:feat", Base: "main"}); KindOf(err) != ErrInvalid {
		t.Errorf("a fork head is not opened here: %v", err)
	}
	if _, err := g.Create(bg, glCred, glRepo, CreateInput{Title: " ", Head: "x", Base: "main"}); KindOf(err) != ErrInvalid {
		t.Errorf("empty title: %v", err)
	}
}

func TestGitLabCreateWhenOneAlreadyExists(t *testing.T) {
	f := newGLFake(t)
	f.fail("POST "+glProject+"/merge_requests", 409, `{"message":["Another open merge request already exists for this source branch: !7"]}`, nil)
	_, err := f.provider().Create(bg, glCred, glRepo, CreateInput{Title: "t", Head: "feat", Base: "main"})
	if KindOf(err) != ErrValidation || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
}

func TestGitLabUpdateKeepsTheDraftPrefix(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(map[string]any{"title": "Draft: Add retries", "draft": true}))
	f.reply("PUT "+glMRPath, 200, glSampleMR(map[string]any{"title": "Draft: Better title", "description": "new"}))
	g := f.provider()
	title, body := "Better title", "new"
	cr, err := g.Update(bg, glCred, glRepo, 7, UpdateInput{Title: &title, Body: &body})
	if err != nil || cr.Body != "new" {
		t.Fatalf("update = %+v, %v", cr, err)
	}
	var sent map[string]any
	json.Unmarshal(f.calls("PUT " + glMRPath)[0].Body, &sent)
	if sent["title"] != "Draft: Better title" || sent["description"] != "new" {
		t.Errorf("sent = %v: a draft must stay a draft", sent)
	}
	// A body-only edit needs no read of the request.
	before := len(f.calls("GET " + glMRPath))
	if _, err := g.Update(bg, glCred, glRepo, 7, UpdateInput{Body: &body}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls("GET "+glMRPath)) != before {
		t.Error("a description-only edit read the request first")
	}
	empty := ""
	if _, err := g.Update(bg, glCred, glRepo, 7, UpdateInput{Title: &empty}); KindOf(err) != ErrInvalid {
		t.Errorf("empty title: %v", err)
	}
	if _, err := g.Update(bg, glCred, glRepo, 7, UpdateInput{}); KindOf(err) != ErrInvalid {
		t.Errorf("nothing to change: %v", err)
	}
}

func TestGitLabCommentAndClose(t *testing.T) {
	f := newGLFake(t)
	f.reply("POST "+glMRPath+"/notes", 201, map[string]any{"id": 77, "body": "ping", "author": map[string]any{"username": "chiro"}, "created_at": "2026-10-17T10:00:00Z"})
	f.reply("PUT "+glMRPath, 200, glSampleMR(map[string]any{"state": "closed"}))
	g := f.provider()
	cm, err := g.Comment(bg, glCred, glRepo, 7, "ping")
	if err != nil || cm.ID != "77" || cm.Kind != CommentConversation || cm.Author != "chiro" {
		t.Fatalf("comment = %+v, %v", cm, err)
	}
	if _, err := g.Comment(bg, glCred, glRepo, 7, "  "); KindOf(err) != ErrInvalid {
		t.Errorf("empty comment: %v", err)
	}
	cr, err := g.Close(bg, glCred, glRepo, 7)
	if err != nil || cr.State != StateClosed {
		t.Fatalf("close = %+v, %v", cr, err)
	}
	if got := string(f.calls("PUT " + glMRPath)[0].Body); got != `{"state_event":"close"}` {
		t.Errorf("close sent %s", got)
	}
}

func TestGitLabMergeSendsTheShaAndRefusesAMovedBranch(t *testing.T) {
	f := newGLFake(t)
	mergePath := "PUT " + glMRPath + "/merge"
	f.handle(mergePath, func(w http.ResponseWriter, _ *http.Request, body []byte) {
		var in map[string]any
		json.Unmarshal(body, &in)
		w.Header().Set("Content-Type", "application/json")
		if in["sha"] != "abc123" {
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"message":"SHA does not match HEAD of source branch: abc123"}`)
			return
		}
		json.NewEncoder(w).Encode(glSampleMR(map[string]any{"state": "merged", "merge_commit_sha": "feed01"}))
	})
	g := f.provider()

	_, err := g.Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodMerge, SHA: "stale999"})
	if KindOf(err) != ErrValidation || !strings.Contains(err.Error(), "SHA does not match") {
		t.Fatalf("a moved branch must be refused by the host: %v", err)
	}
	res, err := g.Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodMerge, SHA: "abc123", Title: "T", Body: "B"})
	if err != nil || !res.Merged || res.SHA != "feed01" {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	var sent map[string]any
	json.Unmarshal(f.calls(mergePath)[1].Body, &sent)
	if sent["sha"] != "abc123" || sent["merge_commit_message"] != "T\n\nB" {
		t.Errorf("sent = %v", sent)
	}
	for _, key := range []string{"should_remove_source_branch", "squash", "merge_when_pipeline_succeeds"} {
		if _, ok := sent[key]; ok {
			t.Errorf("%s must not be sent for a plain merge", key)
		}
	}
}

func TestGitLabSquashAndTheMethodsItCannotDo(t *testing.T) {
	f := newGLFake(t)
	f.reply("PUT "+glMRPath+"/merge", 200, glSampleMR(map[string]any{"state": "merged", "squash_commit_sha": "5q0a5h"}))
	g := f.provider()
	res, err := g.Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodSquash, SHA: "abc123", Title: "T"})
	if err != nil || !res.Merged || res.SHA != "5q0a5h" {
		t.Fatalf("squash = %+v, %v", res, err)
	}
	var sent map[string]any
	json.Unmarshal(f.calls("PUT " + glMRPath + "/merge")[0].Body, &sent)
	if sent["squash"] != true || sent["squash_commit_message"] != "T" {
		t.Errorf("sent = %v", sent)
	}
	before := f.count()
	if _, err := g.Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodRebase}); KindOf(err) != ErrInvalid {
		t.Errorf("rebase: %v", err)
	}
	if _, err := g.Merge(bg, glCred, glRepo, 7, MergeInput{Method: "weird"}); KindOf(err) != ErrInvalid {
		t.Errorf("unknown method: %v", err)
	}
	if f.count() != before {
		t.Error("a refused method must send nothing")
	}
	var checker MergeMethodChecker = g
	if checker.CheckMergeMethod(MergeMethodRebase) == nil || checker.CheckMergeMethod(MergeMethodSquash) != nil || checker.CheckMergeMethod(MergeMethodMerge) != nil {
		t.Error("CheckMergeMethod must refuse rebase only")
	}
}

func TestGitLabMergeThatDoesNotLandIsNotReportedMerged(t *testing.T) {
	f := newGLFake(t)
	f.reply("PUT "+glMRPath+"/merge", 200, glSampleMR(map[string]any{"state": "opened"}))
	res, err := f.provider().Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodMerge, SHA: "abc123"})
	if err != nil || res.Merged || !strings.Contains(res.Message, "opened") {
		t.Errorf("res = %+v, %v", res, err)
	}
}

func TestGitLabMergeRefusals(t *testing.T) {
	for status, body := range map[int]string{
		405: `{"message":"405 Method Not Allowed"}`,
		406: `{"message":"Branch cannot be merged"}`,
		422: `{"message":"Branch cannot be merged"}`,
	} {
		f := newGLFake(t)
		f.fail("PUT "+glMRPath+"/merge", status, body, nil)
		_, err := f.provider().Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodMerge})
		if KindOf(err) != ErrValidation {
			t.Errorf("%d: %v", status, err)
		}
	}
}

func TestGitLabBranchAndDeleteBranch(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glProject, map[string]any{"default_branch": "main"})
	f.json("GET "+glProject+"/repository/branches/feat%2Fretries", map[string]any{"name": "feat/retries", "default": false, "protected": false})
	f.json("GET "+glProject+"/repository/branches/main", map[string]any{"name": "main", "default": true, "protected": true})
	f.json("GET "+glProject+"/repository/branches/release", map[string]any{"name": "release", "protected": true})
	f.json("GET "+glProject+"/repository/branches/trunk", map[string]any{"name": "trunk"}) // a host that omits "default"
	f.reply("DELETE "+glProject+"/repository/branches/feat%2Fretries", 204, nil)
	g := f.provider()
	for name, want := range map[string]BranchInfo{
		"feat/retries": {Name: "feat/retries"},
		"main":         {Name: "main", Default: true, Protected: true},
		"release":      {Name: "release", Protected: true},
	} {
		got, err := g.Branch(bg, glCred, glRepo, name)
		if err != nil || got != want {
			t.Errorf("%s = %+v, %v; want %+v", name, got, err, want)
		}
	}
	f.json("GET "+glProject, map[string]any{"default_branch": "trunk"})
	if got, _ := g.Branch(bg, glCred, glRepo, "trunk"); !got.Default {
		t.Errorf("the project's default_branch is the cross-check: %+v", got)
	}
	if err := g.DeleteBranch(bg, glCred, glRepo, "feat/retries"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls("DELETE "+glProject+"/repository/branches/feat%2Fretries")) != 1 {
		t.Errorf("a branch name with a slash must be one encoded segment: %+v", f.log)
	}
	if err := g.DeleteBranch(bg, glCred, glRepo, " "); KindOf(err) != ErrInvalid {
		t.Errorf("empty: %v", err)
	}
}

func TestGitLabErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		kind    ErrorKind
		message string
	}{
		{"401", 401, `{"message":"401 Unauthorized"}`, nil, ErrUnauthorized, "401 Unauthorized"},
		{"401 oauth shape", 401, `{"error":"invalid_token","error_description":"Token was revoked."}`, nil, ErrUnauthorized, "Token was revoked."},
		{"403", 403, `{"message":"403 Forbidden"}`, nil, ErrForbidden, "403 Forbidden"},
		{"404", 404, `{"message":"404 Project Not Found"}`, nil, ErrNotFound, "Project Not Found"},
		{"400", 400, `{"message":"400 Bad request - title is missing"}`, nil, ErrValidation, "title is missing"},
		{"409", 409, `{"message":"Branch already exists"}`, nil, ErrValidation, "already exists"},
		{"422", 422, `{"message":{"base":["Source branch is invalid"],"target_branch":["can't be blank"]}}`, nil, ErrValidation, "Source branch is invalid"},
		{"500", 500, `<html>boom</html>`, nil, ErrUnavailable, "Internal Server Error"},
		{"502 json string", 502, `"bad gateway"`, nil, ErrUnavailable, "bad gateway"},
		{"429 retry-after", 429, `{"message":"Retry later"}`, map[string]string{"Retry-After": "30"}, ErrRateLimited, "Retry later"},
		{"403 spent", 403, `{"message":"slow down"}`, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "1900000000"}, ErrRateLimited, "slow down"},
	}
	for _, c := range cases {
		f := newGLFake(t)
		f.fail("GET "+glProject, c.status, c.body, c.headers)
		_, err := f.provider().DefaultBranch(bg, glCred, glRepo)
		var fe *Error
		if !errors.As(err, &fe) || fe.Kind != c.kind || fe.Status != c.status || !strings.Contains(fe.Message, c.message) {
			t.Errorf("%s: %+v", c.name, err)
		}
	}
}

func TestGitLabRateLimitSaysWhenItLifts(t *testing.T) {
	f := newGLFake(t)
	f.fail("GET "+glProject, 429, `{"message":"Retry later"}`, map[string]string{"Retry-After": "120", "RateLimit-Reset": "1"})
	start := time.Now()
	_, err := f.provider().DefaultBranch(bg, glCred, glRepo)
	at := RetryAtOf(err)
	if KindOf(err) != ErrRateLimited || at.Before(start.Add(100*time.Second)) || at.After(start.Add(140*time.Second)) {
		t.Errorf("Retry-After wins: %v at %v", err, at)
	}
	f2 := newGLFake(t)
	reset := time.Now().Add(time.Hour).Unix()
	f2.fail("GET "+glProject, 429, `{"message":"x"}`, map[string]string{"RateLimit-Reset": fmt.Sprint(reset)})
	_, err = f2.provider().DefaultBranch(bg, glCred, glRepo)
	if !RetryAtOf(err).Equal(time.Unix(reset, 0)) {
		t.Errorf("RateLimit-Reset is a Unix time: %v", RetryAtOf(err))
	}
	f3 := newGLFake(t)
	f3.fail("GET "+glProject, 429, `{}`, nil)
	_, err = f3.provider().DefaultBranch(bg, glCred, glRepo)
	if KindOf(err) != ErrRateLimited || !RetryAtOf(err).IsZero() {
		t.Errorf("unknown reset: %v %v", err, RetryAtOf(err))
	}
}

func TestGitLabTokenNeverAppearsInAnErrorOrTheRequestURL(t *testing.T) {
	f := newGLFake(t)
	// A host that echoes the credential back in its explanation.
	f.fail("GET "+glProject, 401, fmt.Sprintf(`{"message":"bad token %s"}`, glCred.Token), nil)
	f.fail("PUT "+glMRPath+"/merge", 409, fmt.Sprintf(`{"message":{"x":["%s"]}}`, glCred.Token), nil)
	g := f.provider()
	_, err1 := g.DefaultBranch(bg, glCred, glRepo)
	_, err2 := g.Merge(bg, glCred, glRepo, 7, MergeInput{Method: MergeMethodMerge})
	for _, err := range []error{err1, err2} {
		if err == nil || strings.Contains(err.Error(), glCred.Token) || strings.Contains(fmt.Sprintf("%+v %v", err, err), glCred.Token) {
			t.Errorf("the token leaked into an error: %v", err)
		}
	}
	for _, r := range f.log {
		if strings.Contains(r.Path, glCred.Token) || strings.Contains(r.Query, glCred.Token) || strings.Contains(string(r.Body), glCred.Token) {
			t.Errorf("the token was put in the URL or body: %+v", r)
		}
	}

	// An unreachable host: net/http's own error must not carry it either.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	_, err := NewGitLab(WithGitLabRoot(url)).DefaultBranch(bg, glCred, glRepo)
	if KindOf(err) != ErrUnavailable || strings.Contains(err.Error(), glCred.Token) {
		t.Errorf("unreachable: %v", err)
	}
}

func TestGitLabConditionalRequestsAreUsedOnlyWhenTheHostSentAValidator(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glProject, map[string]any{"default_branch": "main"})
	g := f.provider()
	for i := 0; i < 3; i++ {
		if branch, err := g.DefaultBranch(bg, glCred, glRepo); err != nil || branch != "main" {
			t.Fatal(branch, err)
		}
	}
	calls := f.calls("GET " + glProject)
	if calls[0].IfNoneMatch != "" || calls[1].IfNoneMatch == "" {
		t.Errorf("the first call is plain, later ones conditional: %+v", calls)
	}
	if stats := g.Stats(); stats.Requests != 3 || stats.NotModified != 2 {
		t.Errorf("stats = %+v", stats)
	}
	// Another token must not be served the first one's cached answer.
	f.fail("GET "+glProject, 404, `{"message":"404 Project Not Found"}`, nil)
	if _, err := g.DefaultBranch(bg, Credential{Token: "someone-elses-token"}, glRepo); KindOf(err) != ErrNotFound {
		t.Errorf("a different token must not share a cache entry: %v", err)
	}

	// A host that never sends an ETag is simply asked every time.
	f2 := newGLFake(t)
	f2.reply("GET "+glProject, 200, map[string]any{"default_branch": "main"})
	g2 := f2.provider()
	g2.DefaultBranch(bg, glCred, glRepo)
	g2.DefaultBranch(bg, glCred, glRepo)
	for _, c := range f2.calls("GET " + glProject) {
		if c.IfNoneMatch != "" {
			t.Errorf("sent a validator the host never issued: %+v", c)
		}
	}
}

func TestGitLabGetOfAForkResolvesItsProjectPath(t *testing.T) {
	f := newGLFake(t)
	f.json("GET "+glMRPath, glSampleMR(map[string]any{"source_project_id": 55}))
	f.json("GET /api/v4/projects/55", map[string]any{"path_with_namespace": "someone/billing-service"})
	f.json("GET "+glProject+"/pipelines/99/jobs", []map[string]any{})
	f.json("GET "+glMRPath+"/approvals", map[string]any{})
	glStats(f, 0, 0, 0)
	cr, err := f.provider().Get(bg, glCred, glRepo, 7)
	if err != nil || cr.HeadRepo != "someone/billing-service" {
		t.Errorf("HeadRepo = %q, %v", cr.HeadRepo, err)
	}

	// When the fork cannot be looked up, the value still never equals the repository's.
	f2 := newGLFake(t)
	f2.json("GET "+glMRPath, glSampleMR(map[string]any{"source_project_id": 55}))
	cr, err = f2.provider().Get(bg, glCred, glRepo, 7)
	if err != nil || strings.EqualFold(cr.HeadRepo, glRepo.Path) || cr.HeadRepo == "" {
		t.Errorf("an unresolved fork must still read as a fork: %q, %v", cr.HeadRepo, err)
	}
}

func TestGitLabBadNumbersAreRefusedBeforeTheNetwork(t *testing.T) {
	f := newGLFake(t)
	g := f.provider()
	if _, err := g.Get(bg, glCred, glRepo, 0); KindOf(err) != ErrInvalid {
		t.Error(err)
	}
	if _, err := g.Comments(bg, glCred, glRepo, -1); KindOf(err) != ErrInvalid {
		t.Error(err)
	}
	if _, err := g.Close(bg, glCred, glRepo, 0); KindOf(err) != ErrInvalid {
		t.Error(err)
	}
	if f.count() != 0 {
		t.Error("nothing may be sent")
	}
}
