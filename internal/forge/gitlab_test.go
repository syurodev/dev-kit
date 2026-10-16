package forge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

var glRepo = Repo{Host: "gitlab.com", Path: "group/sub/widgets"}

const mrBase = "/projects/group/sub/widgets/merge_requests/7"

func (f *fakeHost) gitlab() *GitLab { return NewGitLab(WithGitLabAPIBase(f.srv.URL)) }

func sampleMR(extra map[string]any) map[string]any {
	mr := map[string]any{
		"iid": 7, "title": "Add retries", "description": "Retries flaky calls", "state": "opened", "draft": false,
		"source_branch": "feat/retries", "target_branch": "main", "sha": "abc123",
		"web_url": "https://gitlab.com/group/sub/widgets/-/merge_requests/7", "updated_at": "2026-10-16T10:00:00Z",
		"has_conflicts": false, "detailed_merge_status": "mergeable", "changes_count": "3",
		"source_project_id": 11, "target_project_id": 11,
		"author":        map[string]any{"username": "syuro"},
		"head_pipeline": map[string]any{"id": 99, "status": "running", "web_url": "https://gitlab.com/group/sub/widgets/-/pipelines/99"},
	}
	for k, v := range extra {
		mr[k] = v
	}
	return mr
}

func job(name, status string, extra map[string]any) map[string]any {
	j := map[string]any{"name": name, "status": status, "web_url": "https://gitlab.com/j/" + name}
	for k, v := range extra {
		j[k] = v
	}
	return j
}

func serveGitLab(f *fakeHost, mr map[string]any, jobs []map[string]any) {
	f.json("GET "+mrBase, mr)
	f.json("GET /projects/group/sub/widgets/merge_requests", []any{mr})
	f.json("GET "+mrBase+"/diffs", []any{
		map[string]any{"diff": "@@ -1,2 +1,3 @@\n keep\n-old\n+new\n+more\n"},
		map[string]any{"diff": "@@ -5 +5 @@\n-gone\n"},
	})
	f.json("GET /projects/group/sub/widgets/pipelines/99/jobs", jobs)
	f.json("GET "+mrBase+"/approvals", map[string]any{"approved": false, "approved_by": []any{}})
}

func TestGitLabParseURL(t *testing.T) {
	g := NewGitLab()
	for _, tc := range []struct {
		raw  string
		ok   bool
		path string
		n    int
	}{
		{"https://gitlab.com/group/sub/widgets/-/merge_requests/7", true, "group/sub/widgets", 7},
		{"https://gitlab.com/group/widgets/-/merge_requests/12/diffs", true, "group/widgets", 12},
		{"https://gitlab.com/widgets/-/merge_requests/1", false, "", 0},
		{"https://gitlab.com/group/widgets/-/issues/7", false, "", 0},
		{"https://gitlab.com.evil.test/group/widgets/-/merge_requests/7", false, "", 0},
		{"https://github.com/group/widgets/pull/7", false, "", 0},
		{"https://gitlab.com/group/widgets/-/merge_requests/0", false, "", 0},
	} {
		r, n, ok := g.ParseURL(tc.raw)
		if ok != tc.ok || r.Path != tc.path || n != tc.n {
			t.Errorf("ParseURL(%q) = %v %d %v", tc.raw, r, n, ok)
		}
	}
	if !g.Serves("gitlab.com") || g.Serves("github.com") || g.Serves("gitlab.com.evil.test") {
		t.Error("Serves")
	}
	if g.Terms().Short != "MR" || g.Terms().Checks != "pipelines" || g.Kind() != "gitlab" {
		t.Errorf("%+v", g.Terms())
	}
}

func TestGitLabFindAsksTheProjectByEscapedPathAndSourceBranch(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /projects/group/sub/widgets/merge_requests", []any{
		sampleMR(map[string]any{"iid": 9, "state": "merged"}), sampleMR(nil),
	})
	got, err := f.gitlab().Find(bg, cred, glRepo, Head{Owner: "group", Branch: "feat/retries"})
	if err != nil || len(got) != 2 || got[0].State != StateMerged || got[1].Number != 7 || got[1].State != StateOpen {
		t.Fatalf("%+v %v", got, err)
	}
	req := f.requests("GET /projects/group/sub/widgets/merge_requests")[0]
	if req.Auth != "Bearer ghp_secret_token" || !strings.Contains(req.Query, "source_branch=feat%2Fretries") {
		t.Errorf("%+v", req)
	}
	if got[1].ChecksKnown || got[1].Terms.Short != "MR" {
		t.Errorf("a list result claims more than it read: %+v", got[1])
	}
}

func TestGitLabGetReadsSizePipelineJobsAndApproval(t *testing.T) {
	f := newFakeHost(t)
	serveGitLab(f, sampleMR(nil), []map[string]any{
		job("build", "success", nil),
		job("unit", "failed", map[string]any{"failure_reason": "script_failure"}),
		job("lint", "failed", map[string]any{"allow_failure": true}),
		job("e2e", "running", nil),
		job("deploy", "manual", nil),
		job("canary", "canceled", nil),
		job("queued", "created", nil),
	})
	f.json("GET "+mrBase+"/approvals", map[string]any{"approved": false, "approved_by": []any{map[string]any{"user": map[string]any{"username": "carol"}}}})
	cr, err := f.gitlab().Get(bg, cred, glRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Additions != 2 || cr.Deletions != 2 || cr.ChangedFiles != 3 || cr.Head != "feat/retries" || cr.Base != "main" || cr.Mergeable != MergeClean {
		t.Errorf("%+v", cr)
	}
	c := cr.Checks
	if c.Total != 7 || c.Failed != 2 || c.Running != 2 || c.Skipped != 2 || c.Passed != 3 {
		t.Errorf("checks %+v", c)
	}
	if len(c.Failing) != 2 || c.Failing[0].Name != "unit" || c.Failing[0].Summary != "script failure" {
		t.Errorf("failing %+v", c.Failing)
	}
	if !cr.ChecksKnown || cr.Decision != DecisionApproved || len(cr.Notes) != 0 {
		t.Errorf("known=%v decision=%q notes=%v", cr.ChecksKnown, cr.Decision, cr.Notes)
	}
	if c.Failing[0].URL != "https://gitlab.com/j/unit" {
		t.Errorf("url %q", c.Failing[0].URL)
	}
}

func TestGitLabMergeableStates(t *testing.T) {
	for status, want := range map[string]Mergeable{
		"mergeable": MergeClean, "need_rebase": MergeBehind, "conflict": MergeConflicting,
		"checking": MergeUnknown, "unchecked": MergeUnknown, "": MergeUnknown,
		"ci_must_pass": MergeBlocked, "not_approved": MergeBlocked, "discussions_not_resolved": MergeBlocked,
		"draft_status": MergeBlocked, "requested_changes": MergeBlocked,
	} {
		if got := gitlabMergeable(glMR{DetailedMergeStatus: status}); got != want {
			t.Errorf("%q → %q, want %q", status, got, want)
		}
	}
	if gitlabMergeable(glMR{HasConflicts: true, DetailedMergeStatus: "mergeable"}) != MergeConflicting {
		t.Error("has_conflicts wins")
	}
	g := NewGitLab()
	if g.changeRequest(glRepo, glMR{DetailedMergeStatus: "requested_changes"}).Decision != DecisionChangesRequested {
		t.Error("requested_changes is a review verdict")
	}
	if cr := g.changeRequest(glRepo, glMR{State: "locked"}); cr.State != StateOpen {
		t.Errorf("locked: %q", cr.State)
	}
	if cr := g.changeRequest(glRepo, glMR{SourceProjectID: 5, TargetProjectID: 6}); cr.HeadRepo == "" {
		t.Error("a fork's merge request must not look like this repository's")
	}
}

func TestGitLabPipelineWithoutJobsStillShowsSomethingIsComing(t *testing.T) {
	f := newFakeHost(t)
	serveGitLab(f, sampleMR(nil), []map[string]any{})
	cr, err := f.gitlab().Get(bg, cred, glRepo, 7)
	if err != nil || cr.Checks.Running != 1 || cr.Checks.Total != 1 {
		t.Fatalf("%+v %v", cr.Checks, err)
	}
	serveGitLab(f, sampleMR(map[string]any{"head_pipeline": nil}), nil)
	cr, _ = f.gitlab().Get(bg, cred, glRepo, 7)
	if cr.Checks.Total != 0 || !cr.ChecksKnown {
		t.Errorf("no pipeline is no checks: %+v", cr.Checks)
	}
}

func TestGitLabPollingAnUnchangedMergeRequestIsAnswered304(t *testing.T) {
	f := newFakeHost(t)
	serveGitLab(f, sampleMR(nil), []map[string]any{job("build", "success", nil)})
	g := f.gitlab()
	if _, err := g.Get(bg, cred, glRepo, 7); err != nil {
		t.Fatal(err)
	}
	before := g.Stats()
	second, err := g.Get(bg, cred, glRepo, 7)
	if err != nil {
		t.Fatal(err)
	}
	after := g.Stats()
	// mr, diffs, jobs, approvals
	if after.Requests-before.Requests != 4 || after.NotModified-before.NotModified != 4 {
		t.Fatalf("%+v → %+v", before, after)
	}
	if second.Title != "Add retries" || second.Additions != 2 || second.Checks.Passed != 1 {
		t.Errorf("a 304 must serve the cached body: %+v", second)
	}
}

func TestGitLabGetLosesPartsWithNotesNotTheCall(t *testing.T) {
	f := newFakeHost(t)
	serveGitLab(f, sampleMR(nil), nil)
	f.status("GET "+mrBase+"/diffs", 403, "403 Forbidden", nil)
	f.status("GET /projects/group/sub/widgets/pipelines/99/jobs", 403, "403 Forbidden", nil)
	f.status("GET "+mrBase+"/approvals", 404, "404 Not Found", nil)
	cr, err := f.gitlab().Get(bg, cred, glRepo, 7)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(cr.Notes) != 2 || cr.ChecksKnown || cr.Decision != DecisionNone {
		t.Errorf("known=%v decision=%q notes=%v", cr.ChecksKnown, cr.Decision, cr.Notes)
	}
}

func TestGitLabErrorsAreClassified(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		headers map[string]string
		kind    ErrorKind
	}{
		{"bad token", 401, nil, ErrUnauthorized},
		{"forbidden", 403, nil, ErrForbidden},
		{"missing", 404, nil, ErrNotFound},
		{"rate", 429, map[string]string{"Retry-After": "45"}, ErrRateLimited},
		{"unprocessable", 422, nil, ErrValidation},
		{"not mergeable", 406, nil, ErrValidation},
		{"down", 503, nil, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHost(t)
			f.status("GET "+mrBase, tc.code, "msg", tc.headers)
			_, err := f.gitlab().Get(bg, cred, glRepo, 7)
			if KindOf(err) != tc.kind || strings.Contains(err.Error(), cred.Token) {
				t.Fatalf("%q %v", KindOf(err), err)
			}
			if tc.kind == ErrRateLimited && RetryAtOf(err).IsZero() {
				t.Error("no retry time")
			}
		})
	}
}

func TestGitLabValidationMessagesAreFlattened(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /projects/group/sub/widgets/merge_requests", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(409)
		w.Write([]byte(`{"message":["Another open merge request already exists for this source branch: !3"]}`))
	})
	_, err := f.gitlab().Create(bg, cred, glRepo, CreateInput{Title: "t", Head: "feat/retries", Base: "main"})
	if KindOf(err) != ErrValidation || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("%v", err)
	}
	f.handle("POST /projects/group/sub/widgets/merge_requests", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(400)
		w.Write([]byte(`{"message":{"base":["title is too long"]},"error":"x"}`))
	})
	_, err = f.gitlab().Create(bg, cred, glRepo, CreateInput{Title: "t", Head: "feat/retries", Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "base title is too long") {
		t.Fatalf("%v", err)
	}
	f.handle("POST /projects/group/sub/widgets/merge_requests", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"invalid_token","error_description":"Token is expired"}`))
	})
	if _, err = f.gitlab().Create(bg, cred, glRepo, CreateInput{Title: "t", Head: "feat/retries", Base: "main"}); KindOf(err) != ErrUnauthorized || !strings.Contains(err.Error(), "invalid_token") {
		t.Fatalf("%v", err)
	}
}

func TestGitLabCommentsAndThreads(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET "+mrBase+"/discussions", []any{
		map[string]any{"id": "aaaa1111", "notes": []any{
			map[string]any{"id": 11, "body": "Looks good", "type": nil, "resolvable": false, "author": map[string]any{"username": "bob"}, "created_at": "2026-10-16T09:00:00Z"},
		}},
		map[string]any{"id": "bbbb2222", "notes": []any{
			map[string]any{"id": 21, "body": "nil check?", "type": "DiffNote", "resolvable": true, "resolved": false, "author": map[string]any{"username": "carol"},
				"position": map[string]any{"new_path": "a.go", "new_line": 42}},
			map[string]any{"id": 22, "body": "done", "type": "DiffNote", "resolvable": true, "resolved": false, "author": map[string]any{"username": "syuro"},
				"position": map[string]any{"new_path": "a.go", "new_line": 42}},
		}},
		map[string]any{"id": "cccc3333", "notes": []any{
			map[string]any{"id": 31, "body": "rename", "type": "DiffNote", "resolvable": true, "resolved": true, "author": map[string]any{"username": "dan"},
				"position": map[string]any{"old_path": "b.go", "old_line": 9}},
		}},
		map[string]any{"id": "dddd4444", "notes": []any{
			map[string]any{"id": 41, "body": "assigned to bob", "system": true, "author": map[string]any{"username": "gitlab"}},
		}},
	})
	g := f.gitlab()
	comments, err := g.Comments(bg, cred, glRepo, 7)
	if err != nil || len(comments) != 4 {
		t.Fatalf("%+v %v", comments, err)
	}
	if comments[0].Kind != CommentConversation || comments[0].ID != "aaaa1111:11" {
		t.Errorf("%+v", comments[0])
	}
	if c := comments[1]; c.Kind != CommentReview || c.Path != "a.go" || c.Line != 42 || c.Resolved || c.ThreadID != "bbbb2222" || c.ReplyTo != "" {
		t.Errorf("%+v", c)
	}
	if c := comments[2]; c.ReplyTo != "bbbb2222:21" || c.Author != "syuro" {
		t.Errorf("%+v", c)
	}
	if c := comments[3]; !c.Resolved || c.Path != "b.go" || c.Line != 9 {
		t.Errorf("%+v", c)
	}
	n, err := g.UnresolvedThreads(bg, cred, glRepo, 7)
	if err != nil || n != 1 {
		t.Fatalf("unresolved %d %v", n, err)
	}
}

func TestGitLabCreateMarksADraftAndRefusesAFork(t *testing.T) {
	f := newFakeHost(t)
	f.handle("POST /projects/group/sub/widgets/merge_requests", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(sampleMR(map[string]any{"iid": 12, "draft": true}))
	})
	g := f.gitlab()
	cr, err := g.Create(bg, cred, glRepo, CreateInput{Title: "Add retries", Body: "why", Head: "feat/retries", Base: "main", Draft: true})
	if err != nil || cr.Number != 12 {
		t.Fatalf("%+v %v", cr, err)
	}
	var sent map[string]any
	json.Unmarshal(f.exact("POST /projects/group/sub/widgets/merge_requests")[0].Body, &sent)
	if sent["title"] != "Draft: Add retries" || sent["source_branch"] != "feat/retries" || sent["target_branch"] != "main" || sent["description"] != "why" {
		t.Errorf("sent %v", sent)
	}
	if _, err := g.Create(bg, cred, glRepo, CreateInput{Title: "t", Head: "me:feat/retries", Base: "main"}); KindOf(err) != ErrInvalid {
		t.Errorf("fork: %v", err)
	}
	if _, err := g.Create(bg, cred, glRepo, CreateInput{Head: "x", Base: "y"}); KindOf(err) != ErrInvalid {
		t.Errorf("title: %v", err)
	}
}

func TestGitLabUpdateCommentReply(t *testing.T) {
	f := newFakeHost(t)
	f.handle("PUT "+mrBase, func(w http.ResponseWriter, _ *http.Request, _ []byte) { json.NewEncoder(w).Encode(sampleMR(nil)) })
	f.handle("POST "+mrBase+"/notes", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":501,"body":"thanks","author":{"username":"syuro"},"created_at":"2026-10-16T10:00:00Z"}`))
	})
	f.handle("POST "+mrBase+"/discussions/bbbb2222/notes", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":502,"body":"fixed","author":{"username":"syuro"}}`))
	})
	g := f.gitlab()
	title := "New title"
	if _, err := g.Update(bg, cred, glRepo, 7, UpdateInput{Title: &title}); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	json.Unmarshal(f.exact("PUT " + mrBase)[0].Body, &sent)
	if _, has := sent["description"]; has || sent["title"] != "New title" {
		t.Errorf("sent %v", sent)
	}
	if _, err := g.Update(bg, cred, glRepo, 7, UpdateInput{}); KindOf(err) != ErrInvalid {
		t.Errorf("empty update: %v", err)
	}
	c, err := g.Comment(bg, cred, glRepo, 7, "thanks")
	if err != nil || c.ID != "501" || c.Kind != CommentConversation {
		t.Fatalf("%+v %v", c, err)
	}
	r, err := g.Reply(bg, cred, glRepo, 7, "bbbb2222:21", "fixed")
	if err != nil || r.ID != "bbbb2222:502" || r.ReplyTo != "bbbb2222:21" || r.Kind != CommentReview {
		t.Fatalf("%+v %v", r, err)
	}
	for _, bad := range []string{"", "../x", "21", "ZZZZ"} {
		if _, err := g.Reply(bg, cred, glRepo, 7, bad, "x"); KindOf(err) != ErrInvalid {
			t.Errorf("reply to %q: %v", bad, err)
		}
	}
	if _, err := g.Comment(bg, cred, glRepo, 7, " "); KindOf(err) != ErrInvalid {
		t.Errorf("empty comment: %v", err)
	}
}

func TestGitLabMergeMethods(t *testing.T) {
	f := newFakeHost(t)
	f.handle("PUT "+mrBase+"/merge", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		json.NewEncoder(w).Encode(sampleMR(map[string]any{"state": "merged", "merge_commit_sha": "deadbeef"}))
	})
	g := f.gitlab()
	res, err := g.Merge(bg, cred, glRepo, 7, MergeInput{Method: MergeMethodSquash, Title: "Add retries", Body: "details", SHA: "abc123"})
	if err != nil || !res.Merged || res.SHA != "deadbeef" {
		t.Fatalf("%+v %v", res, err)
	}
	var sent map[string]any
	json.Unmarshal(f.exact("PUT " + mrBase + "/merge")[0].Body, &sent)
	if sent["squash"] != true || sent["sha"] != "abc123" || sent["squash_commit_message"] != "Add retries\n\ndetails" {
		t.Errorf("squash sent %v", sent)
	}
	if _, err := g.Merge(bg, cred, glRepo, 7, MergeInput{Method: MergeMethodMerge, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	sent = nil
	json.Unmarshal(f.exact("PUT " + mrBase + "/merge")[1].Body, &sent)
	if _, has := sent["squash"]; has || sent["merge_commit_message"] != "t" {
		t.Errorf("merge sent %v", sent)
	}
	before := len(f.exact("PUT " + mrBase + "/merge"))
	if _, err := g.Merge(bg, cred, glRepo, 7, MergeInput{Method: MergeMethodRebase}); KindOf(err) != ErrInvalid || len(f.exact("PUT "+mrBase+"/merge")) != before {
		t.Errorf("rebase must be refused locally: %v", err)
	}
	f.status("PUT "+mrBase+"/merge", 409, "SHA does not match HEAD of source branch", nil)
	if _, err := g.Merge(bg, cred, glRepo, 7, MergeInput{Method: MergeMethodMerge, SHA: "old"}); KindOf(err) != ErrValidation {
		t.Errorf("sha mismatch: %v", err)
	}
	f.status("PUT "+mrBase+"/merge", 405, "Method Not Allowed", nil)
	if _, err := g.Merge(bg, cred, glRepo, 7, MergeInput{Method: MergeMethodMerge}); KindOf(err) != ErrValidation {
		t.Errorf("not mergeable: %v", err)
	}
}

func TestGitLabCloseDeleteBranchAndBranchInfo(t *testing.T) {
	f := newFakeHost(t)
	f.handle("PUT "+mrBase, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		json.NewEncoder(w).Encode(sampleMR(map[string]any{"state": "closed"}))
	})
	f.handle("DELETE /projects/group/sub/widgets/repository/branches/feat/retries", func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.WriteHeader(204) })
	f.json("GET /projects/group/sub/widgets/repository/branches/feat/retries", map[string]any{"protected": false, "default": false})
	f.json("GET /projects/group/sub/widgets/repository/branches/main", map[string]any{"protected": true, "default": true})
	f.json("GET /projects/group/sub/widgets", map[string]any{"default_branch": "main"})
	g := f.gitlab()
	cr, err := g.Close(bg, cred, glRepo, 7)
	if err != nil || cr.State != StateClosed {
		t.Fatalf("%+v %v", cr, err)
	}
	if body := decodeBody(t, f.exact("PUT " + mrBase)[0]); len(body) != 1 || body["state_event"] != "close" {
		t.Errorf("close sent %v", body)
	}
	if err := g.DeleteBranch(bg, cred, glRepo, "feat/retries"); err != nil {
		t.Fatal(err)
	}
	// the slash in a branch name is escaped on the wire
	if del := f.requests("DELETE ")[0]; del.Path != "/projects/group/sub/widgets/repository/branches/feat/retries" {
		t.Errorf("%q", del.Path)
	}
	if b, err := g.Branch(bg, cred, glRepo, "main"); err != nil || !b.Default || !b.Protected {
		t.Fatalf("%+v %v", b, err)
	}
	if def, err := g.DefaultBranch(bg, cred, glRepo); err != nil || def != "main" {
		t.Fatalf("%q %v", def, err)
	}
	if err := g.DeleteBranch(bg, cred, glRepo, ""); KindOf(err) != ErrInvalid {
		t.Errorf("%v", err)
	}
}

func TestGitLabRefusesAMalformedProjectLocally(t *testing.T) {
	f := newFakeHost(t)
	g := f.gitlab()
	if _, err := g.Get(bg, cred, Repo{Host: "gitlab.com", Path: "solo"}, 7); KindOf(err) != ErrInvalid {
		t.Errorf("%v", err)
	}
	if _, err := g.Get(bg, cred, glRepo, 0); KindOf(err) != ErrInvalid {
		t.Errorf("%v", err)
	}
	if len(f.requests("")) != 0 {
		t.Errorf("something was sent: %v", f.requests(""))
	}
}

func TestRegistryRoutesBothPlatforms(t *testing.T) {
	r := NewRegistry(NewGitHub(), NewGitLab())
	if r.For("github.com").Kind() != "github" || r.For("gitlab.com").Kind() != "gitlab" || r.For("bitbucket.org") != nil {
		t.Error("routing")
	}
	if p, repo, n, ok := r.ParseURL("https://gitlab.com/g/s/p/-/merge_requests/3"); !ok || p.Kind() != "gitlab" || repo.Path != "g/s/p" || n != 3 {
		t.Error("gitlab url")
	}
	if p, _, _, ok := r.ParseURL("https://github.com/a/b/pull/3"); !ok || p.Kind() != "github" {
		t.Error("github url")
	}
	_ = fmt.Sprint
}
