package prwatch

import (
	"context"
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

	"devkit/internal/forge"
)

var bg = context.Background()

// host is a fake GitHub that records what it was asked.
type host struct {
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]func(http.ResponseWriter, *http.Request, []byte)
	log    []string
	bodies map[string][]byte
}

func newHost(t *testing.T) *host {
	h := &host{routes: map[string]func(http.ResponseWriter, *http.Request, []byte){}, bodies: map[string][]byte{}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		line := r.Method + " " + r.URL.Path
		h.mu.Lock()
		h.log = append(h.log, line+"?"+r.URL.RawQuery)
		h.bodies[line] = body
		fn := h.routes[line]
		h.mu.Unlock()
		if fn == nil {
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		fn(w, r, body)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *host) set(route string, v any) {
	raw, _ := json.Marshal(v)
	h.mu.Lock()
	h.routes[route] = func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.Write(raw) }
	h.mu.Unlock()
}

func (h *host) fail(route string, code int, message string, headers map[string]string) {
	h.mu.Lock()
	h.routes[route] = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"message":%q}`, message)
	}
	h.mu.Unlock()
}

func (h *host) count(prefix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (h *host) total() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.log) }

type source struct {
	mu sync.Mutex
	co map[string]Checkout
}

func (s *source) Checkout(_ context.Context, folder string) (Checkout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	co, ok := s.co[folder]
	if !ok {
		return Checkout{}, ErrNotRepository
	}
	return co, nil
}

type creds struct {
	mu    sync.Mutex
	token string
	err   error
	asked []TokenRequest
}

func (c *creds) Token(_ context.Context, req TokenRequest) (string, string, error) {
	c.mu.Lock()
	c.asked = append(c.asked, req)
	c.mu.Unlock()
	return c.token, "work", c.err
}

func origin() RemoteRef {
	return RemoteRef{Name: "origin", URL: "https://github.com/acme/widgets.git", Host: "github.com", Path: "acme/widgets", Owner: "acme"}
}

type rig struct {
	t     *testing.T
	host  *host
	src   *source
	creds *creds
	svc   *Service
	now   time.Time
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, host: newHost(t), src: &source{co: map[string]Checkout{}}, creds: &creds{token: "ghp_secret"}, now: time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)}
	r.svc = New(forge.NewRegistry(forge.NewGitHub(forge.WithAPIBase(r.host.srv.URL))), r.src, r.creds, nil)
	r.svc.now = func() time.Time { return r.now }
	r.src.co["/w/widgets"] = Checkout{Branch: "feat/retries", Origin: origin(), Push: origin(), DefaultBranch: "main"}
	return r
}

func (r *rig) advance(d time.Duration) { r.now = r.now.Add(d) }

func pull(extra map[string]any) map[string]any {
	p := map[string]any{
		"number": 7, "title": "Add retries", "html_url": "https://github.com/acme/widgets/pull/7", "state": "open",
		"additions": 10, "deletions": 2, "changed_files": 3, "mergeable": true, "mergeable_state": "clean",
		"updated_at": "2026-10-16T09:00:00Z", "user": map[string]any{"login": "syuro"},
		"head": map[string]any{"ref": "feat/retries", "sha": "abc"}, "base": map[string]any{"ref": "main"},
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// serve installs a pull request #7 with the given check runs.
func (r *rig) serve(p map[string]any, runs ...map[string]any) {
	h := r.host
	h.set("GET /repos/acme/widgets/pulls", []any{p})
	h.set("GET /repos/acme/widgets/pulls/7", p)
	if runs == nil {
		runs = []map[string]any{}
	}
	h.set("GET /repos/acme/widgets/commits/abc/check-runs", map[string]any{"check_runs": runs})
	h.set("GET /repos/acme/widgets/commits/abc/status", map[string]any{"statuses": []any{}})
	h.set("GET /repos/acme/widgets/pulls/7/reviews", []any{})
	h.set("POST /graphql", map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
		"reviewThreads": map[string]any{"nodes": []any{map[string]any{"isResolved": false}, map[string]any{"isResolved": true}}}}}}})
}

func run(name, status, conclusion string) map[string]any {
	return map[string]any{"name": name, "status": status, "conclusion": conclusion}
}

func (r *rig) state(folders ...string) State {
	if len(folders) == 0 {
		folders = []string{"/w/widgets"}
	}
	return r.svc.State(bg, "s1", folders)
}

func TestStateFindsTheBranchsPullRequestAndPollsQuicklyWhileChecksRun(t *testing.T) {
	r := newRig(t)
	r.serve(pull(nil), run("build", "completed", "success"), run("unit", "in_progress", ""), run("lint", "completed", "failure"))
	st := r.state()
	if len(st.Items) != 1 {
		t.Fatalf("%+v", st)
	}
	it := st.Items[0]
	cr := it.ChangeRequest
	if it.Status != StatusFound || cr == nil || cr.Number != 7 || it.Source != "branch" || it.Branch != "feat/retries" {
		t.Fatalf("%+v", it)
	}
	if cr.Checks.Passed != 1 || cr.Checks.Failed != 1 || cr.Checks.Running != 1 || cr.Checks.Failing[0].Name != "lint" {
		t.Errorf("checks %+v", cr.Checks)
	}
	if cr.UnresolvedThreads != 1 {
		t.Errorf("unresolved threads %d", cr.UnresolvedThreads)
	}
	if st.NextPollMs != int(pollRunning/time.Millisecond) {
		t.Errorf("next poll %d, want %d while a check runs", st.NextPollMs, pollRunning/time.Millisecond)
	}
	if got := r.creds.asked[0]; got.Interactive || got.Host != "github.com" || got.Owner != "acme" {
		t.Errorf("a background poll must not ask the person: %+v", got)
	}
}

func TestPollSlowsWhenSettledAndStopsWhenMerged(t *testing.T) {
	r := newRig(t)
	r.serve(pull(nil), run("build", "completed", "success"))
	if st := r.state(); st.NextPollMs != int(pollSettled/time.Millisecond) {
		t.Errorf("settled: %d", st.NextPollMs)
	}

	r.advance(time.Minute)
	r.serve(pull(map[string]any{"state": "closed", "merged": true}), run("build", "completed", "success"))
	st := r.state()
	if st.Items[0].ChangeRequest.State != forge.StateMerged || st.NextPollMs != -1 {
		t.Fatalf("merged: %+v poll %d", st.Items[0].ChangeRequest, st.NextPollMs)
	}
	// Merged is final: later calls are answered without the pull request
	// (only the branch lookup is repeated).
	pullsBefore := r.host.count("GET /repos/acme/widgets/pulls/7")
	r.advance(time.Hour)
	r.state()
	if r.host.count("GET /repos/acme/widgets/pulls/7") != pullsBefore {
		t.Error("a merged pull request was read again")
	}
}

func TestMergeableUnknownKeepsPollingQuickly(t *testing.T) {
	r := newRig(t)
	p := pull(nil)
	p["mergeable"] = nil
	r.serve(p)
	if st := r.state(); st.NextPollMs != int(pollRunning/time.Millisecond) {
		t.Errorf("GitHub computes mergeability lazily; poll %d", st.NextPollMs)
	}
}

func TestTwoCallsInAFewSecondsCostOneRoundOfRequests(t *testing.T) {
	r := newRig(t)
	r.serve(pull(nil), run("build", "completed", "success"))
	r.state()
	n := r.host.total()
	r.advance(time.Second)
	r.state()
	if r.host.total() != n {
		t.Errorf("a second window within %v asked the host again: %d → %d requests", minAge, n, r.host.total())
	}
	r.advance(5 * time.Second)
	r.state()
	if r.host.total() == n {
		t.Error("an old answer was never refreshed")
	}
}

func TestThreadCountIsRereadOnATimerAndWhenTheRequestMoves(t *testing.T) {
	r := newRig(t)
	r.serve(pull(nil))
	r.state()
	if r.host.count("POST /graphql") != 1 {
		t.Fatal("first read")
	}
	r.advance(10 * time.Second)
	r.state()
	if r.host.count("POST /graphql") != 1 {
		t.Error("the count was re-read though nothing moved")
	}
	r.advance(threadsEvery)
	r.state()
	if r.host.count("POST /graphql") != 2 {
		t.Error("the count must be re-read on a timer: resolving a thread does not bump updated_at")
	}
	r.advance(10 * time.Second)
	r.serve(pull(map[string]any{"updated_at": "2026-10-16T10:00:30Z"}))
	r.state()
	if r.host.count("POST /graphql") != 3 {
		t.Error("the count must be re-read when the request changed")
	}
}

func TestNoPullRequestYet(t *testing.T) {
	r := newRig(t)
	r.host.set("GET /repos/acme/widgets/pulls", []any{})
	st := r.state()
	if st.Items[0].Status != StatusNone || st.Items[0].ChangeRequest != nil || st.NextPollMs != int(pollNone/time.Millisecond) {
		t.Fatalf("%+v", st)
	}
}

func TestPrefersTheOpenRequestOverAMergedOne(t *testing.T) {
	r := newRig(t)
	old := pull(map[string]any{"number": 3, "state": "closed", "merged": true, "updated_at": "2026-10-20T00:00:00Z"})
	r.serve(pull(nil))
	r.host.set("GET /repos/acme/widgets/pulls", []any{old, pull(nil)})
	if st := r.state(); st.Items[0].ChangeRequest.Number != 7 {
		t.Fatalf("%+v", st.Items[0].ChangeRequest)
	}
}

func TestNothingIsAskedOnTheDefaultBranchOrDetached(t *testing.T) {
	r := newRig(t)
	co := r.src.co["/w/widgets"]
	co.Branch = "main"
	r.src.co["/w/widgets"] = co
	co.Branch, co.Detached = "", true
	r.src.co["/w/detached"] = co
	st := r.state("/w/widgets", "/w/detached", "/w/nope")
	want := []Status{StatusDefaultBranch, StatusDetached, StatusNotRepository}
	for i, w := range want {
		if st.Items[i].Status != w {
			t.Errorf("item %d: %q, want %q", i, st.Items[i].Status, w)
		}
	}
	if r.host.total() != 0 || st.NextPollMs != -1 {
		t.Errorf("requests %d, poll %d", r.host.total(), st.NextPollMs)
	}
}

func TestEveryFailureStatesItsReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*rig)
		status Status
		part   string
		poll   func(int) bool
	}{
		{"no account", func(r *rig) { r.creds.err = ErrNoAccount }, StatusNoAccount, "No GitHub account for this remote", func(p int) bool { return p == -1 }},
		{"two accounts", func(r *rig) { r.creds.err = ErrAmbiguousAccount }, StatusAmbiguousAccount, "More than one GitHub account", func(p int) bool { return p == -1 }},
		{"bad token", func(r *rig) { r.host.fail("GET /repos/acme/widgets/pulls", 401, "Bad credentials", nil) }, StatusUnauthorized, "rejected the account's token", func(p int) bool { return p == -1 }},
		{"forbidden", func(r *rig) { r.host.fail("GET /repos/acme/widgets/pulls", 403, "Resource not accessible", nil) }, StatusForbidden, "may not read", func(p int) bool { return p == -1 }},
		{"not found", func(r *rig) { r.host.fail("GET /repos/acme/widgets/pulls", 404, "Not Found", nil) }, StatusNotFound, "cannot find acme/widgets", func(p int) bool { return p == -1 }},
		{"rate limit", func(r *rig) {
			reset := r.now.Add(20 * time.Minute)
			r.host.fail("GET /repos/acme/widgets/pulls", 403, "API rate limit exceeded", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())})
		}, StatusRateLimited, "rate limit", func(p int) bool { return p >= int((19*time.Minute)/time.Millisecond) }},
		{"outage", func(r *rig) { r.host.fail("GET /repos/acme/widgets/pulls", 503, "Service Unavailable", nil) }, StatusUnavailable, "Service Unavailable", func(p int) bool { return p == int(pollRecover/time.Millisecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tc.setup(r)
			st := r.state()
			it := st.Items[0]
			if it.Status != tc.status || !strings.Contains(it.Message, tc.part) {
				t.Fatalf("%q %q", it.Status, it.Message)
			}
			if !tc.poll(st.NextPollMs) {
				t.Errorf("next poll %d", st.NextPollMs)
			}
			if strings.Contains(it.Message, "ghp_secret") {
				t.Error("the message names the token")
			}
		})
	}
}

func TestUnsupportedHostIsSaidNotSkipped(t *testing.T) {
	r := newRig(t)
	co := r.src.co["/w/widgets"]
	co.Origin.Host, co.Origin.Path = "git.corp.example", "team/app"
	r.src.co["/w/widgets"] = co
	it := r.state().Items[0]
	if it.Status != StatusUnsupportedHost || !strings.Contains(it.Message, "git.corp.example") {
		t.Fatalf("%+v", it)
	}
}

func TestABranchOnAForkIsLookedUpUnderTheForksOwner(t *testing.T) {
	r := newRig(t)
	co := r.src.co["/w/widgets"]
	co.Push = RemoteRef{Name: "fork", Host: "github.com", Path: "me/widgets", Owner: "me"}
	r.src.co["/w/widgets"] = co
	r.serve(pull(nil))
	r.state()
	var q string
	for _, l := range r.host.log {
		if strings.HasPrefix(l, "GET /repos/acme/widgets/pulls?") {
			q = l
		}
	}
	if !strings.Contains(q, "head=me%3Afeat%2Fretries") {
		t.Errorf("query %q: the PR is searched in origin's repo by the fork's owner", q)
	}
}

func TestAttachByURLPersistsAndShowsWithoutABranch(t *testing.T) {
	r := newRig(t)
	other := pull(map[string]any{"number": 12, "title": "Upstream fix", "html_url": "https://github.com/upstream/lib/pull/12"})
	r.host.set("GET /repos/upstream/lib/pulls/12", other)
	r.host.set("GET /repos/upstream/lib/commits/abc/check-runs", map[string]any{"check_runs": []any{}})
	r.host.set("GET /repos/upstream/lib/commits/abc/status", map[string]any{"statuses": []any{}})
	r.host.set("GET /repos/upstream/lib/pulls/12/reviews", []any{})
	r.host.set("POST /graphql", map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{"nodes": []any{}}}}}})
	r.host.set("GET /repos/acme/widgets/pulls", []any{})
	var notified []string
	r.svc.OnChanged = func(id string) { notified = append(notified, id) }

	it, err := r.svc.Attach(bg, "s1", "/w/widgets", "https://github.com/upstream/lib/pull/12")
	if err != nil || it.ChangeRequest.Number != 12 || it.Source != "attached" {
		t.Fatalf("%+v %v", it, err)
	}
	if len(notified) != 1 || notified[0] != "s1" {
		t.Errorf("notified %v", notified)
	}
	st := r.state()
	if len(st.Items) != 2 || st.Items[0].Status != StatusNone || st.Items[1].ChangeRequest.Number != 12 || st.Items[1].Source != "attached" {
		t.Fatalf("%+v", st.Items)
	}
	// Another session does not see it.
	if other := r.svc.State(bg, "s2", []string{"/w/widgets"}); len(other.Items) != 1 {
		t.Errorf("attachments leak across sessions: %+v", other.Items)
	}
	if err := r.svc.Detach(bg, "s1", "github.com", "upstream/lib", 12); err != nil {
		t.Fatal(err)
	}
	if st := r.state(); len(st.Items) != 1 {
		t.Errorf("detach: %+v", st.Items)
	}
}

func TestAttachRefusesWhatCannotBeReadAndWhatIsNotAnAddress(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Attach(bg, "s1", "/w/widgets", "https://example.com/not/a/pr"); err == nil {
		t.Error("a foreign address was accepted")
	}
	r.creds.err = ErrNoAccount
	_, err := r.svc.Attach(bg, "s1", "/w/widgets", "https://github.com/acme/widgets/pull/7")
	if err == nil || !strings.Contains(err.Error(), "No GitHub account") {
		t.Errorf("%v", err)
	}
	if list, _ := r.svc.store.List(bg, "s1"); len(list) != 0 {
		t.Errorf("an unreadable address was saved: %+v", list)
	}
	r.creds.err = nil
	r.host.fail("GET /repos/acme/widgets/pulls/7", 404, "Not Found", nil)
	if _, err := r.svc.Attach(bg, "s1", "/w/widgets", "https://github.com/acme/widgets/pull/7"); err == nil {
		t.Error("a 404 was attached")
	}
	if got := r.creds.asked[len(r.creds.asked)-1]; !got.Interactive {
		t.Errorf("attaching is the person's act and may ask which account: %+v", got)
	}
}

func TestAnAttachedRequestAlsoFoundByBranchIsOneCard(t *testing.T) {
	r := newRig(t)
	r.serve(pull(nil))
	if _, err := r.svc.Attach(bg, "s1", "/w/widgets", "https://github.com/acme/widgets/pull/7"); err != nil {
		t.Fatal(err)
	}
	if st := r.state(); len(st.Items) != 1 || st.Items[0].Source != "branch" {
		t.Fatalf("%+v", st.Items)
	}
}

func TestSeveralFoldersAreSeveralItemsAndOneBrokenDoesNotHideTheRest(t *testing.T) {
	r := newRig(t)
	r.serve(pull(nil))
	r.src.co["/w/api"] = Checkout{Branch: "feat/x", Origin: RemoteRef{Name: "origin", Host: "github.com", Path: "acme/api", Owner: "acme"}, Push: RemoteRef{Host: "github.com", Path: "acme/api", Owner: "acme"}}
	r.host.fail("GET /repos/acme/api/pulls", 404, "Not Found", nil)
	st := r.state("/w/widgets", "/w/api")
	if st.Items[0].Status != StatusFound || st.Items[1].Status != StatusNotFound {
		t.Fatalf("%q %q", st.Items[0].Status, st.Items[1].Status)
	}
}

func TestHandleCreateAttachesAtOnceAndSaysSo(t *testing.T) {
	r := newRig(t)
	r.host.mu.Lock()
	r.host.routes["POST /repos/acme/widgets/pulls"] = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(pull(nil))
	}
	r.host.mu.Unlock()
	var notified []string
	r.svc.OnChanged = func(id string) { notified = append(notified, id) }

	h, err := r.svc.Open(bg, OpenInput{Folder: "/w/widgets", SessionID: "s1", Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if h.HeadParam() != "feat/retries" || !h.SameRepoBranch() {
		t.Errorf("head %q", h.HeadParam())
	}
	cr, err := h.Create(bg, forge.CreateInput{Title: "Add retries", Base: "main"})
	if err != nil || cr.Number != 7 {
		t.Fatalf("%+v %v", cr, err)
	}
	var sent map[string]any
	json.Unmarshal(r.host.bodies["POST /repos/acme/widgets/pulls"], &sent)
	if sent["head"] != "feat/retries" {
		t.Errorf("sent %v", sent)
	}
	if len(notified) != 1 {
		t.Errorf("notified %v", notified)
	}
	list, _ := r.svc.store.List(bg, "s1")
	if len(list) != 1 || list[0].Number != 7 || list[0].Source != "created" || list[0].Folder != "/w/widgets" {
		t.Fatalf("%+v", list)
	}
	if !r.creds.asked[len(r.creds.asked)-1].Interactive {
		t.Error("Interactive was not passed through")
	}
}

func TestHandleOnAForkOpensTheRequestFromOwnerColonBranch(t *testing.T) {
	r := newRig(t)
	co := r.src.co["/w/widgets"]
	co.Push = RemoteRef{Name: "fork", Host: "github.com", Path: "me/widgets", Owner: "me"}
	r.src.co["/w/widgets"] = co
	h, err := r.svc.Open(bg, OpenInput{Folder: "/w/widgets"})
	if err != nil {
		t.Fatal(err)
	}
	if h.HeadParam() != "me:feat/retries" || h.SameRepoBranch() {
		t.Errorf("head %q same=%v", h.HeadParam(), h.SameRepoBranch())
	}
}

func TestHandleResolvesByBranchAndRefusesWhenThereIsNone(t *testing.T) {
	r := newRig(t)
	r.host.set("GET /repos/acme/widgets/pulls", []any{})
	h, _ := r.svc.Open(bg, OpenInput{Folder: "/w/widgets"})
	if _, err := h.ChangeRequest(bg); !errors.Is(err, ErrNoChangeRequest) {
		t.Fatalf("%v", err)
	}
	if _, err := h.Comment(bg, "hi"); !errors.Is(err, ErrNoChangeRequest) {
		t.Fatalf("a comment with nothing to comment on: %v", err)
	}
	r.serve(pull(nil))
	r.advance(time.Minute)
	h, _ = r.svc.Open(bg, OpenInput{Folder: "/w/widgets"})
	cr, err := h.ChangeRequest(bg)
	if err != nil || cr.Number != 7 || h.Number() != 7 {
		t.Fatalf("%+v %v", cr, err)
	}
}

func TestDeleteBranchRefusesTheDefaultAndProtectedBeforeDeleting(t *testing.T) {
	r := newRig(t)
	deleted := 0
	r.host.mu.Lock()
	r.host.routes["DELETE /repos/acme/widgets/git/refs/heads/feat/retries"] = func(w http.ResponseWriter, _ *http.Request, _ []byte) { deleted++; w.WriteHeader(204) }
	r.host.routes["DELETE /repos/acme/widgets/git/refs/heads/main"] = func(w http.ResponseWriter, _ *http.Request, _ []byte) { deleted++; w.WriteHeader(204) }
	r.host.routes["DELETE /repos/acme/widgets/git/refs/heads/release"] = func(w http.ResponseWriter, _ *http.Request, _ []byte) { deleted++; w.WriteHeader(204) }
	r.host.mu.Unlock()
	r.host.set("GET /repos/acme/widgets", map[string]any{"default_branch": "main"})
	r.host.set("GET /repos/acme/widgets/branches/feat/retries", map[string]any{"protected": false})
	r.host.set("GET /repos/acme/widgets/branches/main", map[string]any{"protected": true})
	r.host.set("GET /repos/acme/widgets/branches/release", map[string]any{"protected": true})
	h, _ := r.svc.Open(bg, OpenInput{Folder: "/w/widgets"})
	for _, name := range []string{"main", "release"} {
		if err := h.DeleteBranch(bg, name); !errors.Is(err, ErrProtectedBranch) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if deleted != 0 {
		t.Fatalf("%d refs were deleted before the check", deleted)
	}
	if err := h.DeleteBranch(bg, "feat/retries"); err != nil || deleted != 1 {
		t.Fatalf("%v, %d", err, deleted)
	}
}

func TestNextPollPureRules(t *testing.T) {
	now := time.Now()
	running := forge.ChangeRequest{State: forge.StateOpen, Mergeable: forge.MergeClean, Checks: forge.CheckSummary{Running: 1}}
	settled := forge.ChangeRequest{State: forge.StateOpen, Mergeable: forge.MergeClean}
	merged := forge.ChangeRequest{State: forge.StateMerged}
	ms := func(d time.Duration) int { return int(d / time.Millisecond) }
	for name, tc := range map[string]struct {
		items []Item
		want  int
	}{
		"empty":            {nil, -1},
		"merged only":      {[]Item{{Status: StatusFound, ChangeRequest: &merged}}, -1},
		"running wins":     {[]Item{{Status: StatusFound, ChangeRequest: &settled}, {Status: StatusFound, ChangeRequest: &running}}, ms(pollRunning)},
		"settled":          {[]Item{{Status: StatusFound, ChangeRequest: &settled}, {Status: StatusFound, ChangeRequest: &merged}}, ms(pollSettled)},
		"none":             {[]Item{{Status: StatusNone}}, ms(pollNone)},
		"needs a person":   {[]Item{{Status: StatusNoAccount}, {Status: StatusUnauthorized}}, -1},
		"rate limit floor": {[]Item{{Status: StatusRateLimited, RetryAt: ptrTime(now.Add(time.Second))}}, ms(pollRateFloor)},
		"rate limit far":   {[]Item{{Status: StatusRateLimited, RetryAt: ptrTime(now.Add(10 * time.Minute))}}, ms(10 * time.Minute)},
	} {
		if got := nextPoll(tc.items, now); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestSQLStoreRoundTrip(t *testing.T) {
	st := NewMemoryStore()
	a := Attachment{SessionID: "s", Host: "github.com", Path: "a/b", Number: 1, URL: "u", Source: "url", CreatedAt: time.Now()}
	_ = st.Put(bg, a)
	_ = st.Put(bg, a)
	if list, _ := st.List(bg, "s"); len(list) != 1 {
		t.Errorf("%+v", list)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
