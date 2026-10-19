package prwatch

import (
	"strings"
	"testing"
	"time"

	"devkit/internal/forge"
)

const (
	glBase  = "/api/v4/projects/acme/platform/billing-service" // the host decodes %2F
	glMRURL = "https://gitlab.com/acme/platform/billing-service/-/merge_requests/7"
)

func glOrigin() RemoteRef {
	return RemoteRef{Name: "origin", URL: "git@gitlab.com:acme/platform/billing-service.git", Host: "gitlab.com", Path: "acme/platform/billing-service", Owner: "acme"}
}

func glMR(extra map[string]any) map[string]any {
	m := map[string]any{
		"iid": 7, "title": "Add retries", "state": "opened", "web_url": glMRURL, "source_branch": "feat/retries", "target_branch": "main",
		"source_project_id": 1, "target_project_id": 1, "sha": "abc", "updated_at": "2026-10-17T09:00:00Z",
		"detailed_merge_status": "mergeable", "changes_count": "3", "author": map[string]any{"username": "syuro"},
		"head_pipeline": map[string]any{"id": 99, "status": "success"},
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func newGitLabRig(t *testing.T) *rig {
	r := &rig{t: t, host: newHost(t), src: &source{co: map[string]Checkout{}}, creds: &creds{token: "glpat-secret"}, now: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)}
	r.svc = New(forge.NewRegistry(forge.NewGitHub(), forge.NewGitLab(forge.WithGitLabRoot(r.host.srv.URL))), r.src, r.creds, nil)
	r.svc.now = func() time.Time { return r.now }
	r.src.co["/w/billing"] = Checkout{Branch: "feat/retries", Origin: glOrigin(), Push: glOrigin(), DefaultBranch: "main"}
	return r
}

func (r *rig) serveGitLab(mr map[string]any, jobs ...map[string]any) {
	r.host.set("GET "+glBase+"/merge_requests", []any{mr})
	r.host.set("GET "+glBase+"/merge_requests/7", mr)
	r.host.set("GET "+glBase+"/pipelines/99/jobs", jobs)
	r.host.set("GET "+glBase+"/merge_requests/7/approvals", map[string]any{"approved_by": []any{}})
	r.host.set("GET "+glBase+"/merge_requests/7/discussions", []any{
		map[string]any{"id": strings.Repeat("a", 40), "notes": []any{map[string]any{"id": 1, "resolvable": true, "resolved": false}}},
		map[string]any{"id": strings.Repeat("b", 40), "notes": []any{map[string]any{"id": 2, "resolvable": true, "resolved": true}}},
	})
	r.host.set("POST /api/graphql", map[string]any{"data": map[string]any{"project": map[string]any{"mergeRequest": map[string]any{
		"diffStatsSummary": map[string]any{"additions": 10, "deletions": 2, "fileCount": 3}}}}})
}

func TestGitLabBranchFindsItsMergeRequestInANestedGroup(t *testing.T) {
	r := newGitLabRig(t)
	r.serveGitLab(glMR(nil), map[string]any{"name": "unit", "status": "success"})
	st := r.svc.State(bg, "s1", []string{"/w/billing"})
	if len(st.Items) != 1 || st.Items[0].Status != StatusFound {
		t.Fatalf("%+v", st.Items)
	}
	it := st.Items[0]
	cr := it.ChangeRequest
	if it.Platform != "GitLab" || cr.Number != 7 || cr.Terms.Short != "MR" || cr.Terms.Checks != "pipelines" ||
		cr.Additions != 10 || cr.UnresolvedThreads != 1 || cr.Checks.Passed != 1 || cr.Mergeable != forge.MergeClean {
		t.Errorf("%+v", cr)
	}
	if st.NextPollMs != int(pollSettled/time.Millisecond) {
		t.Errorf("settled poll = %d", st.NextPollMs)
	}
	if r.creds.asked[0].Host != "gitlab.com" || r.creds.asked[0].Repo != "acme/platform/billing-service" || r.creds.asked[0].Interactive {
		t.Errorf("token request = %+v: a background poll must never ask the person", r.creds.asked[0])
	}
}

func TestGitLabPollBudgetFollowsPipelinesAndMergeability(t *testing.T) {
	for name, tc := range map[string]struct {
		mr   map[string]any
		jobs []map[string]any
		want time.Duration
	}{
		"a job is running": {glMR(nil), []map[string]any{{"name": "unit", "status": "running"}}, pollRunning},
		"mergeability still being worked out": {glMR(map[string]any{"detailed_merge_status": "checking"}),
			[]map[string]any{{"name": "unit", "status": "success"}}, pollRunning},
		"settled":           {glMR(nil), []map[string]any{{"name": "unit", "status": "failed"}}, pollSettled},
		"merged is final":   {glMR(map[string]any{"state": "merged"}), nil, -1},
		"closed is final":   {glMR(map[string]any{"state": "closed"}), nil, -1},
		"pipeline starting": {glMR(map[string]any{"head_pipeline": map[string]any{"id": 99, "status": "created"}}), nil, pollRunning},
	} {
		t.Run(name, func(t *testing.T) {
			r := newGitLabRig(t)
			r.serveGitLab(tc.mr, tc.jobs...)
			st := r.svc.State(bg, "s1", []string{"/w/billing"})
			want := int(tc.want / time.Millisecond)
			if tc.want < 0 {
				want = -1
			}
			if st.NextPollMs != want {
				t.Errorf("nextPollMs = %d, want %d", st.NextPollMs, want)
			}
		})
	}
}

func TestGitLabUnchangedMergeRequestIsAnsweredFromCacheWithinTheMinimumAge(t *testing.T) {
	r := newGitLabRig(t)
	r.serveGitLab(glMR(nil), map[string]any{"name": "unit", "status": "success"})
	r.svc.State(bg, "s1", []string{"/w/billing"})
	before := r.host.total()
	r.advance(time.Second)
	r.svc.State(bg, "s1", []string{"/w/billing"})
	if r.host.total() != before {
		t.Errorf("two windows within %v cost %d extra requests", minAge, r.host.total()-before)
	}
}

func TestGitLabAttachByAddressStoresTheNestedPath(t *testing.T) {
	r := newGitLabRig(t)
	r.serveGitLab(glMR(nil), map[string]any{"name": "unit", "status": "success"})
	item, err := r.svc.Attach(bg, "s1", "/w/billing", glMRURL)
	if err != nil || item.Status != StatusFound || item.ChangeRequest.Repo.Path != "acme/platform/billing-service" {
		t.Fatalf("%+v %v", item, err)
	}
	if !r.creds.asked[0].Interactive || r.creds.asked[0].Owner != "acme" {
		t.Errorf("an attach is the person's call and may ask: %+v", r.creds.asked[0])
	}
	st := r.svc.State(bg, "s1", nil)
	if len(st.Items) != 1 || st.Items[0].Source != "attached" || st.Items[0].ChangeRequest.Number != 7 {
		t.Errorf("%+v", st.Items)
	}
	if _, err := r.svc.Attach(bg, "s1", "/w/billing", "https://github.com/acme/widgets/pull/3"); err != nil {
		// GitHub is still served; this must have been read (and failed on the fake's 404) rather than rejected as unsupported.
		if strings.Contains(err.Error(), "not the address") {
			t.Errorf("a pull request address must still be recognised: %v", err)
		}
	}
}

func TestGitLabFailuresAreStatedInGitLabsWords(t *testing.T) {
	r := newGitLabRig(t)
	r.host.fail("GET "+glBase+"/merge_requests", 401, "401 Unauthorized", nil)
	it := r.svc.State(bg, "s1", []string{"/w/billing"}).Items[0]
	if it.Status != StatusUnauthorized || !strings.Contains(it.Message, "GitLab rejected") {
		t.Errorf("%+v", it)
	}
	r.advance(10 * time.Second)
	r.host.fail("GET "+glBase+"/merge_requests", 429, "Retry later", map[string]string{"Retry-After": "120"})
	it = r.svc.State(bg, "s1", []string{"/w/billing"}).Items[0]
	if it.Status != StatusRateLimited || it.RetryAt == nil || !strings.Contains(it.Message, "GitLab rate limit") {
		t.Errorf("%+v", it)
	}
	r.creds.err = ErrNoAccount
	r.advance(10 * time.Second)
	it = r.svc.State(bg, "s1", []string{"/w/billing"}).Items[0]
	if it.Status != StatusNoAccount || !strings.Contains(it.Message, "No GitLab account") {
		t.Errorf("%+v", it)
	}
}

func TestSelfHostedGitLabIsStillUnsupportedAndSaysSo(t *testing.T) {
	r := newGitLabRig(t)
	self := RemoteRef{Name: "origin", Host: "git.example.com", Path: "acme/x", Owner: "acme"}
	r.src.co["/w/self"] = Checkout{Branch: "feat", Origin: self, Push: self, DefaultBranch: "main"}
	it := r.svc.State(bg, "s1", []string{"/w/self"}).Items[0]
	if it.Status != StatusUnsupportedHost || !strings.Contains(it.Message, "git.example.com") {
		t.Errorf("%+v", it)
	}
	if r.host.total() != 0 {
		t.Error("nothing may be sent to a host no provider serves")
	}
}

// A remote whose only account is an SSH key is not "without an account": the
// notice says the key pushes but cannot read merge requests. It names no
// repository, so the window folds folders on one key into one notice, and a
// label cannot smuggle parentheses in (they are what that folding strips).
func TestAKeyOnlyRemoteIsSaidAsSuch(t *testing.T) {
	r := newGitLabRig(t)
	r.src.co["/w/other"] = Checkout{Branch: "feat/x", Origin: glOrigin(), Push: glOrigin(), DefaultBranch: "main"}
	r.creds.err = NewKeyOnlyError("id_ed25519_gitlab")
	st := r.svc.State(bg, "s1", []string{"/w/billing", "/w/other"})
	want := "The GitLab key “id_ed25519_gitlab” pushes your code but can't read merge requests. Add a GitLab token with read_api access in Accounts."
	for _, it := range st.Items {
		if it.Status != StatusNoAccount || it.Message != want {
			t.Errorf("%+v", it)
		}
	}
	if r.host.total() != 0 || st.NextPollMs != -1 {
		t.Errorf("a notice that needs a person must not poll: %d requests, poll %d", r.host.total(), st.NextPollMs)
	}

	r.advance(10 * time.Second)
	r.creds.err = NewKeyOnlyError("")
	if got := r.svc.State(bg, "s1", []string{"/w/billing"}).Items[0].Message; !strings.HasPrefix(got, "The GitLab SSH key pushes") {
		t.Errorf("several keys: %q", got)
	}
	if got := NewKeyOnlyError("work (old)\n\t key").Account; got != "work [old] key" {
		t.Errorf("label %q", got)
	}
	if got := NewKeyOnlyError(strings.Repeat("é", 100)).Account; len([]rune(got)) != 61 {
		t.Errorf("label not clipped: %d", len([]rune(got)))
	}
}
