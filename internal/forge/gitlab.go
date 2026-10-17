package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitLab is the Provider for GitLab's REST API v4: gitlab.com, and any other
// host handed to NewGitLab with WithGitLabHosts.
//
// Not yet: a self-hosted GitLab. Serves is exact, like GitHub's, because a
// token is only ever sent to the host it was issued for, and DevKit's account
// registry knows one GitLab host (gitlab.com): there is no way yet to say "the
// account bound to git.example.com is a GitLab token" to the registry that
// picks a provider from a bare host. WithGitLabHosts is where that mapping
// plugs in; nothing calls it with more than the default.
//
// A GitLab project is addressed by its whole path, URL-encoded as one segment
// (group%2Fsubgroup%2Frepo), so nested groups work. A merge request is its
// project-scoped iid — the number a person sees after the "!".
type GitLab struct {
	client *http.Client
	// root overrides "https://<repo.Host>" for every host: tests stand a fake
	// up with httptest. Empty in production.
	root  string
	hosts map[string]bool
	cache *etagCache
	slots chan struct{}

	mu    sync.Mutex
	stats Stats
}

// GitLabOption configures NewGitLab.
type GitLabOption func(*GitLab)

// WithGitLabHTTPClient replaces the HTTP client (timeouts, transport). Its
// redirect policy is overridden: a redirect is never followed.
func WithGitLabHTTPClient(client *http.Client) GitLabOption {
	return func(g *GitLab) { g.client = client }
}

// WithGitLabRoot points the provider at another server for every host (the
// address without /api/v4). It exists for tests; Serves still answers for the
// configured hosts only.
func WithGitLabRoot(root string) GitLabOption {
	return func(g *GitLab) { g.root = strings.TrimRight(root, "/") }
}

// WithGitLabHosts adds hosts this provider serves. Only for a host the caller
// knows is GitLab and holds a token for.
func WithGitLabHosts(hosts ...string) GitLabOption {
	return func(g *GitLab) {
		for _, h := range hosts {
			if h = normalizeHost(h); h != "" {
				g.hosts[h] = true
			}
		}
	}
}

// WithGitLabConcurrency bounds requests in flight.
func WithGitLabConcurrency(n int) GitLabOption {
	return func(g *GitLab) {
		if n > 0 {
			g.slots = make(chan struct{}, n)
		}
	}
}

func NewGitLab(opts ...GitLabOption) *GitLab {
	g := &GitLab{
		client: &http.Client{Timeout: 20 * time.Second},
		hosts:  map[string]bool{"gitlab.com": true},
		cache:  newETagCache(512),
		slots:  make(chan struct{}, defaultConcurrent),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Stats is a snapshot of the request counters.
func (g *GitLab) Stats() Stats {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stats
}

func (g *GitLab) Kind() string { return "gitlab" }
func (g *GitLab) Terms() Terms {
	return Terms{Platform: "GitLab", Short: "MR", Long: "merge request", Checks: "pipelines"}
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func (g *GitLab) Serves(host string) bool { return g.hosts[normalizeHost(host)] }

// api is the REST root for a repository's host.
func (g *GitLab) api(repo Repo) string { return g.origin(repo) + "/api/v4" }

func (g *GitLab) origin(repo Repo) string {
	if g.root != "" {
		return g.root
	}
	return "https://" + normalizeHost(repo.Host)
}

// sameHost refuses to send the token to an address on another host: a paging
// link comes from the server's answer, and an answer is not an instruction.
func (g *GitLab) sameHost(repo Repo, target string) error {
	want, err := url.Parse(g.origin(repo))
	if err != nil {
		return &Error{Kind: ErrInvalid, Message: "not a usable address"}
	}
	got, err := url.Parse(target)
	if err != nil || !strings.EqualFold(got.Host, want.Host) || got.Scheme != want.Scheme {
		return &Error{Kind: ErrInvalid, Message: "the host pointed to an address outside itself; not followed"}
	}
	return nil
}

var gitlabMergeURL = regexp.MustCompile(`^/(.+?)/(?:-/)?merge_requests/(\d+)(?:/.*)?$`)

// ParseURL reads https://host/group/sub/project/-/merge_requests/12 (and the
// older form without "/-/").
func (g *GitLab) ParseURL(raw string) (Repo, int, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !g.Serves(u.Hostname()) {
		return Repo{}, 0, false
	}
	m := gitlabMergeURL.FindStringSubmatch(strings.TrimSuffix(u.Path, "/"))
	if m == nil {
		return Repo{}, 0, false
	}
	n, _ := strconv.Atoi(m[2])
	path := strings.TrimSuffix(m[1], "/")
	if n <= 0 || !validProjectPath(path) {
		return Repo{}, 0, false
	}
	return Repo{Host: normalizeHost(u.Hostname()), Path: path}, n, true
}

// validProjectPath: at least group/project, no empty or dot segments.
func validProjectPath(path string) bool {
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return false
	}
	for _, s := range segments {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}

func (g *GitLab) validRepo(repo Repo) error {
	if !g.Serves(repo.Host) {
		return &Error{Kind: ErrInvalid, Message: "not a GitLab host: " + repo.Host}
	}
	if !validProjectPath(repo.Path) {
		return &Error{Kind: ErrInvalid, Message: "not a GitLab project path: " + repo.Path}
	}
	return nil
}

// projectPath is "/projects/group%2Fsub%2Fproject".
func projectPath(repo Repo) string { return "/projects/" + url.PathEscape(repo.Path) }

func glIID(number int) (string, error) {
	if number <= 0 {
		return "", &Error{Kind: ErrInvalid, Message: "not a merge request number: " + strconv.Itoa(number)}
	}
	return strconv.Itoa(number), nil
}

// mrPath is "/projects/<path>/merge_requests/<iid>".
func mrPath(repo Repo, iid string) string { return projectPath(repo) + "/merge_requests/" + iid }

// ---- wire shapes -----------------------------------------------------

type glMR struct {
	IID                         int       `json:"iid"`
	Title                       string    `json:"title"`
	Description                 string    `json:"description"`
	State                       string    `json:"state"`
	Draft                       bool      `json:"draft"`
	WorkInProgress              bool      `json:"work_in_progress"`
	WebURL                      string    `json:"web_url"`
	SourceBranch                string    `json:"source_branch"`
	TargetBranch                string    `json:"target_branch"`
	SourceProjectID             int       `json:"source_project_id"`
	TargetProjectID             int       `json:"target_project_id"`
	SHA                         string    `json:"sha"`
	UpdatedAt                   time.Time `json:"updated_at"`
	MergeStatus                 string    `json:"merge_status"`
	DetailedMergeStatus         string    `json:"detailed_merge_status"`
	HasConflicts                bool      `json:"has_conflicts"`
	BlockingDiscussionsResolved *bool     `json:"blocking_discussions_resolved"`
	ChangesCount                string    `json:"changes_count"`
	MergeCommitSHA              string    `json:"merge_commit_sha"`
	SquashCommitSHA             string    `json:"squash_commit_sha"`
	Author                      struct {
		Username string `json:"username"`
	} `json:"author"`
	HeadPipeline *glPipeline `json:"head_pipeline"`
}

type glPipeline struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	WebURL string `json:"web_url"`
	SHA    string `json:"sha"`
}

func (g *GitLab) changeRequest(repo Repo, m glMR) ChangeRequest {
	state := StateOpen
	switch m.State {
	case "merged":
		state = StateMerged
	case "closed":
		state = StateClosed
	} // "opened", and "locked" (being merged right now), are open
	cr := ChangeRequest{
		Repo: repo, Number: m.IID, Title: m.Title, URL: m.WebURL, State: state, Draft: m.Draft || m.WorkInProgress,
		Author: m.Author.Username, Head: m.SourceBranch, HeadSHA: m.SHA, Base: m.TargetBranch, Body: m.Description,
		ChangedFiles: changesCount(m.ChangesCount), Mergeable: glMergeable(m), Decision: DecisionNone,
		UnresolvedThreads: -1, UpdatedAt: m.UpdatedAt, Terms: g.Terms(),
	}
	if m.SourceProjectID == m.TargetProjectID {
		cr.HeadRepo = repo.Path
	} else if m.SourceProjectID != 0 {
		// A fork. The path is resolved by Get; until then a value that never
		// equals the repository's, so a caller comparing it treats it as a fork.
		cr.HeadRepo = "project:" + strconv.Itoa(m.SourceProjectID)
	}
	return cr
}

// changesCount reads "5" or "1000+" (GitLab stops counting).
func changesCount(s string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(s), "+"))
	return n
}

// glMergeable collapses detailed_merge_status (GitLab 15.6+) — or, on an older
// host, merge_status — into what a person acts on.
func glMergeable(m glMR) Mergeable {
	switch m.DetailedMergeStatus {
	case "mergeable":
		return MergeClean
	case "conflict":
		return MergeConflicting
	case "need_rebase":
		return MergeBehind
	case "checking", "unchecked", "preparing", "approvals_syncing":
		return MergeUnknown
	case "":
	default:
		// ci_must_pass, ci_still_running, discussions_not_resolved, draft_status,
		// not_approved, requested_changes, status_checks_must_pass, … : a rule
		// stops it, there is no conflict.
		if m.HasConflicts {
			return MergeConflicting
		}
		return MergeBlocked
	}
	switch {
	case m.HasConflicts:
		return MergeConflicting
	case m.MergeStatus == "can_be_merged":
		if m.BlockingDiscussionsResolved != nil && !*m.BlockingDiscussionsResolved {
			return MergeBlocked
		}
		return MergeClean
	case m.MergeStatus == "cannot_be_merged":
		return MergeBlocked
	}
	return MergeUnknown
}

// ---- reads -----------------------------------------------------------

func (g *GitLab) DefaultBranch(ctx context.Context, c Credential, repo Repo) (string, error) {
	if err := g.validRepo(repo); err != nil {
		return "", err
	}
	var out struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.get(ctx, c, repo, projectPath(repo), nil, &out); err != nil {
		return "", err
	}
	return out.DefaultBranch, nil
}

func (g *GitLab) Find(ctx context.Context, c Credential, repo Repo, head Head) ([]ChangeRequest, error) {
	if err := g.validRepo(repo); err != nil {
		return nil, err
	}
	if head.Branch == "" {
		return nil, &Error{Kind: ErrInvalid, Message: "no branch to look for"}
	}
	// The list is the target project's. A branch pushed to a fork is told
	// apart by its source project, which is not the target.
	owner, _, _ := strings.Cut(repo.Path, "/")
	wantFork := head.Owner != "" && !strings.EqualFold(head.Owner, owner)
	q := url.Values{}
	q.Set("source_branch", head.Branch)
	q.Set("state", "all")
	q.Set("order_by", "updated_at")
	q.Set("sort", "desc")
	q.Set("per_page", "20")
	var mrs []glMR
	if _, err := g.get(ctx, c, repo, projectPath(repo)+"/merge_requests", q, &mrs); err != nil {
		return nil, err
	}
	out := make([]ChangeRequest, 0, len(mrs))
	for _, m := range mrs {
		if (m.SourceProjectID != m.TargetProjectID) != wantFork {
			continue
		}
		out = append(out, g.changeRequest(repo, m))
		if len(out) == 10 {
			break
		}
	}
	return out, nil
}

func (g *GitLab) getMR(ctx context.Context, c Credential, repo Repo, iid string) (glMR, error) {
	var m glMR
	if _, err := g.get(ctx, c, repo, mrPath(repo, iid), nil, &m); err != nil {
		return glMR{}, err
	}
	return m, nil
}

func (g *GitLab) Get(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error) {
	if err := g.validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	iid, err := glIID(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	m, err := g.getMR(ctx, c, repo, iid)
	if err != nil {
		return ChangeRequest{}, err
	}
	cr := g.changeRequest(repo, m)

	// Everything below is a separate call that a token may be refused: losing
	// one is a note, not a failure — the card still says what it knows.
	if m.SourceProjectID != m.TargetProjectID && m.SourceProjectID != 0 {
		if path, err := g.projectPathByID(ctx, c, repo, m.SourceProjectID); err == nil {
			cr.HeadRepo = path
		}
	}
	checks, err := g.pipelineChecks(ctx, c, repo, m.HeadPipeline)
	if err != nil {
		cr.Notes = append(cr.Notes, "pipelines unavailable: "+err.Error())
	} else {
		cr.Checks, cr.ChecksKnown = summarize(checks), true
	}
	switch decision, err := g.decision(ctx, c, repo, iid, m); {
	case err != nil:
		cr.Notes = append(cr.Notes, "approvals unavailable: "+err.Error())
	default:
		cr.Decision = decision
	}
	if cr.State == StateOpen || cr.State == StateMerged {
		if st, err := g.diffStats(ctx, c, repo, number, m.SHA); err != nil {
			cr.Notes = append(cr.Notes, "line counts unavailable: "+err.Error())
		} else {
			cr.Additions, cr.Deletions = st.Additions, st.Deletions
			if st.Files > 0 {
				cr.ChangedFiles = st.Files
			}
		}
	}
	return cr, nil
}

func (g *GitLab) projectPathByID(ctx context.Context, c Credential, repo Repo, id int) (string, error) {
	var out struct {
		Path string `json:"path_with_namespace"`
	}
	if _, err := g.get(ctx, c, repo, "/projects/"+strconv.Itoa(id), url.Values{"simple": {"true"}}, &out); err != nil {
		return "", err
	}
	if out.Path == "" {
		return "", &Error{Kind: ErrNotFound, Message: "project " + strconv.Itoa(id) + " has no path"}
	}
	return out.Path, nil
}

type glDiffStats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Files     int `json:"files"`
}

// diffStats reads the merge request's line counts. REST has none (only a file
// count), so this asks GraphQL's diffStatsSummary — and remembers the answer by
// head commit, because it only changes when the branch does and Get runs on a
// 15–60 s poll.
func (g *GitLab) diffStats(ctx context.Context, c Credential, repo Repo, number int, sha string) (glDiffStats, error) {
	key := etagKey(c.Token, "stats "+repo.String()+"#"+strconv.Itoa(number))
	if hit, ok := g.cache.get(key); ok && sha != "" && hit.etag == sha {
		var st glDiffStats
		if json.Unmarshal(hit.body, &st) == nil {
			return st, nil
		}
	}
	var out struct {
		Data struct {
			Project *struct {
				MergeRequest *struct {
					Summary *struct {
						Additions int `json:"additions"`
						Deletions int `json:"deletions"`
						FileCount int `json:"fileCount"`
					} `json:"diffStatsSummary"`
				} `json:"mergeRequest"`
			} `json:"project"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	err := g.sendTo(ctx, c, http.MethodPost, g.origin(repo)+"/api/graphql", map[string]any{
		"query":     `query($path:ID!,$iid:String!){project(fullPath:$path){mergeRequest(iid:$iid){diffStatsSummary{additions deletions fileCount}}}}`,
		"variables": map[string]any{"path": repo.Path, "iid": strconv.Itoa(number)},
	}, &out)
	if err != nil {
		return glDiffStats{}, err
	}
	if len(out.Errors) > 0 {
		return glDiffStats{}, &Error{Kind: ErrUnavailable, Message: clip(out.Errors[0].Message, 200)}
	}
	if out.Data.Project == nil || out.Data.Project.MergeRequest == nil || out.Data.Project.MergeRequest.Summary == nil {
		return glDiffStats{}, &Error{Kind: ErrNotFound, Message: "the host reported no diff statistics"}
	}
	s := out.Data.Project.MergeRequest.Summary
	st := glDiffStats{Additions: s.Additions, Deletions: s.Deletions, Files: s.FileCount}
	if raw, err := json.Marshal(st); err == nil && sha != "" {
		g.cache.put(etagEntry{key: key, etag: sha, body: raw})
	}
	return st, nil
}

// decision reads the verdict: GitLab says "requested_changes" itself (15.x),
// and approvals are the approved_by list. A tier without approvals answers
// 403/404; that is "no decision", not a note on every card.
func (g *GitLab) decision(ctx context.Context, c Credential, repo Repo, iid string, m glMR) (Decision, error) {
	if m.DetailedMergeStatus == "requested_changes" {
		return DecisionChangesRequested, nil
	}
	var out struct {
		ApprovedBy []json.RawMessage `json:"approved_by"`
	}
	if _, err := g.get(ctx, c, repo, mrPath(repo, iid)+"/approvals", nil, &out); err != nil {
		if k := KindOf(err); k == ErrNotFound || k == ErrForbidden {
			return DecisionNone, nil
		}
		return DecisionNone, err
	}
	if len(out.ApprovedBy) > 0 {
		return DecisionApproved, nil
	}
	return DecisionNone, nil
}

func (g *GitLab) Checks(ctx context.Context, c Credential, repo Repo, number int) ([]Check, error) {
	if err := g.validRepo(repo); err != nil {
		return nil, err
	}
	iid, err := glIID(number)
	if err != nil {
		return nil, err
	}
	m, err := g.getMR(ctx, c, repo, iid)
	if err != nil {
		return nil, err
	}
	return g.pipelineChecks(ctx, c, repo, m.HeadPipeline)
}

type glJob struct {
	Name         string `json:"name"`
	Stage        string `json:"stage"`
	Status       string `json:"status"`
	AllowFailure bool   `json:"allow_failure"`
	WebURL       string `json:"web_url"`
	FailReason   string `json:"failure_reason"`
}

// pipelineChecks is the jobs of the merge request's head pipeline. A pipeline
// with no jobs yet (or none at all because its configuration is invalid) is one
// check of its own, so "still starting" and "failed to start" are not silence.
func (g *GitLab) pipelineChecks(ctx context.Context, c Credential, repo Repo, p *glPipeline) ([]Check, error) {
	if p == nil || p.ID == 0 {
		return nil, nil
	}
	var jobs []glJob
	q := url.Values{"per_page": {"100"}}
	next := ""
	for page := 0; page < maxPages; page++ {
		var batch []glJob
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, repo, projectPath(repo)+"/pipelines/"+strconv.Itoa(p.ID)+"/jobs", q, &batch)
		} else {
			next, err = g.getURL(ctx, c, repo, next, &batch)
		}
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, batch...)
		if next == "" {
			break
		}
	}
	checks := make([]Check, 0, len(jobs))
	for _, j := range jobs {
		checks = append(checks, checkOfJob(j))
	}
	if len(checks) == 0 {
		ch := Check{Name: "pipeline", URL: p.WebURL, Conclusion: p.Status}
		ch.Status = glStatus(p.Status, false)
		if ch.Status == CheckFailed {
			ch.Summary = "the pipeline failed before running any job"
		}
		checks = append(checks, ch)
	}
	sortChecks(checks)
	return checks, nil
}

func checkOfJob(j glJob) Check {
	ch := Check{Name: j.Name, URL: j.WebURL, Conclusion: j.Status, Status: glStatus(j.Status, j.AllowFailure)}
	if j.Status == "failed" && j.AllowFailure {
		ch.Conclusion = "failed (allowed)"
	}
	if ch.Status == CheckFailed && j.FailReason != "" {
		ch.Summary = clip(strings.ReplaceAll(j.FailReason, "_", " "), summaryClip)
	}
	return ch
}

// glStatus collapses a job or pipeline status. A failed job that is allowed to
// fail does not stop a merge, so it is not counted as a failure; a manual job
// nobody started is not running anything.
func glStatus(status string, allowFailure bool) CheckStatus {
	switch status {
	case "success":
		return CheckPassed
	case "skipped", "manual":
		return CheckSkipped
	case "failed":
		if allowFailure {
			return CheckSkipped
		}
		return CheckFailed
	case "canceled", "canceling":
		return CheckFailed
	default: // created, pending, running, preparing, waiting_for_resource, scheduled
		return CheckRunning
	}
}

func (g *GitLab) Branch(ctx context.Context, c Credential, repo Repo, name string) (BranchInfo, error) {
	if err := g.validRepo(repo); err != nil {
		return BranchInfo{}, err
	}
	if strings.TrimSpace(name) == "" {
		return BranchInfo{}, &Error{Kind: ErrInvalid, Message: "no branch named"}
	}
	var out struct {
		Default   bool `json:"default"`
		Protected bool `json:"protected"`
	}
	if _, err := g.get(ctx, c, repo, projectPath(repo)+"/repository/branches/"+url.PathEscape(name), nil, &out); err != nil {
		return BranchInfo{}, err
	}
	// "default" comes from the branch itself; the project's default_branch is
	// the cross-check, so a host that omits the field cannot make the default
	// branch look deletable.
	def, err := g.DefaultBranch(ctx, c, repo)
	if err != nil {
		return BranchInfo{}, err
	}
	return BranchInfo{Name: name, Default: out.Default || name == def, Protected: out.Protected}, nil
}
