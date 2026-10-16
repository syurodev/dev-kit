package prwatch

import (
	"strings"
	"testing"
	"time"

	"devkit/internal/forge"
)

func glRemote() RemoteRef {
	return RemoteRef{Name: "origin", URL: "git@gitlab.com:group/sub/widgets.git", Host: "gitlab.com", Path: "group/sub/widgets", Owner: "group"}
}

func newGitLabRig(t *testing.T) *rig {
	r := &rig{t: t, host: newHost(t), src: &source{co: map[string]Checkout{}}, creds: &creds{token: "glpat_secret"}, now: time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)}
	r.svc = New(forge.NewRegistry(forge.NewGitHub(), forge.NewGitLab(forge.WithGitLabAPIBase(r.host.srv.URL))), r.src, r.creds, nil)
	r.svc.now = func() time.Time { return r.now }
	r.src.co["/w/widgets"] = Checkout{Branch: "feat/retries", Origin: glRemote(), Push: glRemote(), DefaultBranch: "main"}
	return r
}

func (r *rig) serveMR(extra map[string]any, jobs ...map[string]any) {
	mr := map[string]any{
		"iid": 7, "title": "Add retries", "state": "opened", "source_branch": "feat/retries", "target_branch": "main", "sha": "abc",
		"web_url": "https://gitlab.com/group/sub/widgets/-/merge_requests/7", "updated_at": "2026-10-16T09:00:00Z",
		"detailed_merge_status": "mergeable", "changes_count": "2", "source_project_id": 1, "target_project_id": 1,
		"author": map[string]any{"username": "syuro"}, "head_pipeline": map[string]any{"id": 5, "status": "running"},
	}
	for k, v := range extra {
		mr[k] = v
	}
	if jobs == nil {
		jobs = []map[string]any{}
	}
	h := r.host
	h.set("GET /projects/group/sub/widgets/merge_requests", []any{mr})
	h.set("GET /projects/group/sub/widgets/merge_requests/7", mr)
	h.set("GET /projects/group/sub/widgets/merge_requests/7/diffs", []any{map[string]any{"diff": "@@ -1 +1,2 @@\n-a\n+b\n+c\n"}})
	h.set("GET /projects/group/sub/widgets/pipelines/5/jobs", jobs)
	h.set("GET /projects/group/sub/widgets/merge_requests/7/approvals", map[string]any{"approved": false, "approved_by": []any{}})
	h.set("GET /projects/group/sub/widgets/merge_requests/7/discussions", []any{
		map[string]any{"id": "aa11bb22", "notes": []any{map[string]any{"id": 1, "body": "x", "resolvable": true, "resolved": false, "type": "DiffNote", "author": map[string]any{"username": "c"}}}},
		map[string]any{"id": "cc33dd44", "notes": []any{map[string]any{"id": 2, "body": "y", "resolvable": true, "resolved": true, "type": "DiffNote", "author": map[string]any{"username": "c"}}}},
	})
}

func TestGitLabStateIsAMergeRequestCardWithPipelineProgress(t *testing.T) {
	r := newGitLabRig(t)
	r.serveMR(nil,
		map[string]any{"name": "build", "status": "success"},
		map[string]any{"name": "unit", "status": "failed", "failure_reason": "script_failure"},
		map[string]any{"name": "e2e", "status": "running"})
	st := r.state()
	it := st.Items[0]
	cr := it.ChangeRequest
	if it.Status != StatusFound || cr == nil || cr.Number != 7 || it.Platform != "GitLab" {
		t.Fatalf("%+v", it)
	}
	if cr.Terms.Short != "MR" || cr.Terms.Checks != "pipelines" {
		t.Errorf("terms %+v", cr.Terms)
	}
	if cr.Additions != 2 || cr.Deletions != 1 || cr.Checks.Passed != 1 || cr.Checks.Failed != 1 || cr.Checks.Running != 1 || cr.UnresolvedThreads != 1 {
		t.Errorf("%+v", cr)
	}
	if st.NextPollMs != int(pollRunning/time.Millisecond) {
		t.Errorf("poll %d", st.NextPollMs)
	}
	if got := r.creds.asked[0]; got.Host != "gitlab.com" || got.Owner != "group" || got.Interactive {
		t.Errorf("token request %+v", got)
	}
	// An MR found by branch is looked up by source_branch in the project's own path.
	found := false
	for _, l := range r.host.log {
		if strings.HasPrefix(l, "GET /projects/group/sub/widgets/merge_requests?") && strings.Contains(l, "source_branch=feat%2Fretries") {
			found = true
		}
	}
	if !found {
		t.Errorf("log %v", r.host.log)
	}
}

func TestGitLabMergedStopsPolling(t *testing.T) {
	r := newGitLabRig(t)
	r.serveMR(map[string]any{"state": "merged"})
	if st := r.state(); st.Items[0].ChangeRequest.State != forge.StateMerged || st.NextPollMs != -1 {
		t.Fatalf("%+v poll %d", st.Items[0].ChangeRequest, st.NextPollMs)
	}
}

func TestGitLabMissingAccountNamesGitLab(t *testing.T) {
	r := newGitLabRig(t)
	r.creds.err = ErrNoAccount
	it := r.state().Items[0]
	if it.Status != StatusNoAccount || !strings.Contains(it.Message, "No GitLab account for this remote") || !strings.Contains(it.Message, "group/sub/widgets on gitlab.com") {
		t.Fatalf("%+v", it)
	}
}

func TestGitLabAttachByAddress(t *testing.T) {
	r := newGitLabRig(t)
	r.serveMR(nil)
	r.src.co["/w/widgets"] = Checkout{Branch: "main", Origin: glRemote(), Push: glRemote(), DefaultBranch: "main"}
	it, err := r.svc.Attach(bg, "s1", "/w/widgets", "https://gitlab.com/group/sub/widgets/-/merge_requests/7")
	if err != nil || it.ChangeRequest == nil || it.ChangeRequest.Terms.Short != "MR" {
		t.Fatalf("%+v %v", it, err)
	}
	st := r.state()
	if len(st.Items) != 2 || st.Items[1].Source != "attached" {
		t.Fatalf("%+v", st.Items)
	}
	if got := r.creds.asked[0]; got.Host != "gitlab.com" || got.Owner != "group" || got.Repo != "group/sub/widgets" {
		t.Errorf("%+v", got)
	}
}

func TestGitLabForkBranchHeadIsOwnerColonBranchAndCreateRefusesIt(t *testing.T) {
	r := newGitLabRig(t)
	co := r.src.co["/w/widgets"]
	co.Push = RemoteRef{Name: "fork", Host: "gitlab.com", Path: "me/widgets", Owner: "me"}
	r.src.co["/w/widgets"] = co
	h, err := r.svc.Open(bg, OpenInput{Folder: "/w/widgets"})
	if err != nil {
		t.Fatal(err)
	}
	if h.HeadParam() != "me:feat/retries" || h.SameRepoBranch() {
		t.Errorf("head %q", h.HeadParam())
	}
	if _, err := h.Create(bg, forge.CreateInput{Title: "t", Base: "main"}); forge.KindOf(err) != forge.ErrInvalid {
		t.Errorf("a fork's merge request is not created yet: %v", err)
	}
	if got := h.MergeMethods(); len(got) != 2 {
		t.Errorf("methods %v", got)
	}
}
