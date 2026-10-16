package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const gitlabAPI = "https://gitlab.com/api/v4"

// GitLab is the Provider for gitlab.com: merge requests, their pipelines'
// jobs, discussions and approvals.
//
// Not yet: a self-managed GitLab. It answers on another host with another API
// root, and DevKit's account registry knows no such host, so there would be no
// token to send (the same reason GitHub Enterprise is not served). A merge
// request from a fork is found by the target project's list but not created.
type GitLab struct {
	*client
}

type GitLabOption func(*GitLab)

// WithGitLabAPIBase points the provider at another API root, for tests.
func WithGitLabAPIBase(base string) GitLabOption {
	return func(g *GitLab) { g.base = strings.TrimRight(base, "/") }
}

func NewGitLab(opts ...GitLabOption) *GitLab {
	g := &GitLab{client: newClient(gitlabAPI, func(req *http.Request) {
		req.Header.Set("Accept", "application/json")
	})}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *GitLab) Kind() string { return "gitlab" }
func (g *GitLab) Terms() Terms {
	return Terms{Platform: "GitLab", Short: "MR", Long: "merge request", Checks: "pipelines"}
}

func (g *GitLab) MergeMethods() []MergeMethod {
	return []MergeMethod{MergeMethodMerge, MergeMethodSquash}
}

func (g *GitLab) Serves(host string) bool {
	return strings.ToLower(strings.TrimSuffix(host, ".")) == "gitlab.com"
}

var gitlabMergeURL = regexp.MustCompile(`^/(.+?)/-/merge_requests/(\d+)(?:/.*)?$`)

func (g *GitLab) ParseURL(raw string) (Repo, int, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !g.Serves(u.Hostname()) {
		return Repo{}, 0, false
	}
	m := gitlabMergeURL.FindStringSubmatch(u.Path)
	if m == nil || !strings.Contains(m[1], "/") {
		return Repo{}, 0, false
	}
	n, _ := strconv.Atoi(m[2])
	if n <= 0 {
		return Repo{}, 0, false
	}
	return Repo{Host: "gitlab.com", Path: m[1]}, n, true
}

// projectPath is /projects/<url-encoded path>: GitLab names a project by its
// path with the slashes escaped.
func projectPath(repo Repo) string { return "/projects/" + url.PathEscape(repo.Path) }

func validProject(repo Repo) error {
	if parts := strings.Split(repo.Path, "/"); len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
		return &Error{Kind: ErrInvalid, Message: "not a GitLab project path: " + repo.Path}
	}
	return nil
}

// ---- wire shapes -----------------------------------------------------

type glMR struct {
	IID                 int       `json:"iid"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	State               string    `json:"state"`
	Draft               bool      `json:"draft"`
	SourceBranch        string    `json:"source_branch"`
	TargetBranch        string    `json:"target_branch"`
	SHA                 string    `json:"sha"`
	WebURL              string    `json:"web_url"`
	UpdatedAt           time.Time `json:"updated_at"`
	HasConflicts        bool      `json:"has_conflicts"`
	DetailedMergeStatus string    `json:"detailed_merge_status"`
	ChangesCount        string    `json:"changes_count"`
	SourceProjectID     int       `json:"source_project_id"`
	TargetProjectID     int       `json:"target_project_id"`
	MergeCommitSHA      string    `json:"merge_commit_sha"`
	SquashCommitSHA     string    `json:"squash_commit_sha"`
	Author              struct {
		Username string `json:"username"`
	} `json:"author"`
	HeadPipeline *struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"head_pipeline"`
}

func (g *GitLab) changeRequest(repo Repo, m glMR) ChangeRequest {
	state := StateOpen
	switch m.State {
	case "merged":
		state = StateMerged
	case "closed":
		state = StateClosed
	}
	cr := ChangeRequest{
		Repo: repo, Number: m.IID, Title: m.Title, URL: m.WebURL, State: state, Draft: m.Draft,
		Author: m.Author.Username, Head: m.SourceBranch, HeadSHA: m.SHA, Base: m.TargetBranch, Body: m.Description,
		Mergeable: gitlabMergeable(m), Decision: DecisionNone, UnresolvedThreads: -1,
		UpdatedAt: m.UpdatedAt, Terms: g.Terms(),
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(m.ChangesCount, "+")); err == nil {
		cr.ChangedFiles = n
	}
	if m.SourceProjectID != 0 && m.SourceProjectID != m.TargetProjectID {
		cr.HeadRepo = "project " + strconv.Itoa(m.SourceProjectID) // a fork: not this repository
	}
	if m.DetailedMergeStatus == "requested_changes" {
		cr.Decision = DecisionChangesRequested
	}
	return cr
}

func gitlabMergeable(m glMR) Mergeable {
	if m.HasConflicts || m.DetailedMergeStatus == "conflict" {
		return MergeConflicting
	}
	switch m.DetailedMergeStatus {
	case "mergeable":
		return MergeClean
	case "need_rebase":
		return MergeBehind
	case "checking", "unchecked", "preparing", "approvals_syncing", "":
		return MergeUnknown
	default: // ci_must_pass, not_approved, discussions_not_resolved, draft_status, requested_changes, blocked_status, ...
		return MergeBlocked
	}
}

// ---- reads -----------------------------------------------------------

func (g *GitLab) DefaultBranch(ctx context.Context, c Credential, repo Repo) (string, error) {
	if err := validProject(repo); err != nil {
		return "", err
	}
	var out struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.get(ctx, c, projectPath(repo), nil, &out); err != nil {
		return "", err
	}
	return out.DefaultBranch, nil
}

func (g *GitLab) Find(ctx context.Context, c Credential, repo Repo, head Head) ([]ChangeRequest, error) {
	if err := validProject(repo); err != nil {
		return nil, err
	}
	if head.Branch == "" {
		return nil, &Error{Kind: ErrInvalid, Message: "no branch to look for"}
	}
	q := url.Values{}
	q.Set("source_branch", head.Branch)
	q.Set("state", "all")
	q.Set("order_by", "updated_at")
	q.Set("sort", "desc")
	q.Set("per_page", "10")
	var mrs []glMR
	if _, err := g.get(ctx, c, projectPath(repo)+"/merge_requests", q, &mrs); err != nil {
		return nil, err
	}
	out := make([]ChangeRequest, 0, len(mrs))
	for _, m := range mrs {
		out = append(out, g.changeRequest(repo, m))
	}
	return out, nil
}

func (g *GitLab) mr(ctx context.Context, c Credential, repo Repo, number int) (glMR, string, error) {
	if err := validProject(repo); err != nil {
		return glMR{}, "", err
	}
	n, err := numberPath(number)
	if err != nil {
		return glMR{}, "", err
	}
	var m glMR
	if _, err := g.get(ctx, c, projectPath(repo)+"/merge_requests/"+n, nil, &m); err != nil {
		return glMR{}, "", err
	}
	return m, n, nil
}

func (g *GitLab) Get(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error) {
	m, n, err := g.mr(ctx, c, repo, number)
	if err != nil {
		return ChangeRequest{}, err
	}
	cr := g.changeRequest(repo, m)

	// Each part below is its own call with its own permissions; losing one is a
	// note on the card, not a failed read.
	if added, removed, partial, err := g.size(ctx, c, repo, n); err != nil {
		cr.Notes = append(cr.Notes, "size unavailable: "+err.Error())
	} else {
		cr.Additions, cr.Deletions = added, removed
		if partial {
			cr.Notes = append(cr.Notes, "size counts only the first files of a large merge request")
		}
	}
	checks, err := g.pipelineChecks(ctx, c, repo, m)
	if err != nil {
		cr.Notes = append(cr.Notes, "pipelines unavailable: "+err.Error())
	} else {
		cr.Checks, cr.ChecksKnown = summarize(checks), true
	}
	if cr.Decision == DecisionNone {
		// The approvals endpoint is free-tier; a project that hides it is not a failure.
		if approved, err := g.approved(ctx, c, repo, n); err == nil && approved {
			cr.Decision = DecisionApproved
		}
	}
	return cr, nil
}

// size sums the diff's added and removed lines. GitLab has no totals on the
// merge request, so the diffs are read (up to three pages of 100 files) and the
// lines counted; partial says there were more.
func (g *GitLab) size(ctx context.Context, c Credential, repo Repo, n string) (added, removed int, partial bool, err error) {
	next := ""
	for page := 0; page < maxPages; page++ {
		var files []struct {
			Diff string `json:"diff"`
		}
		var gerr error
		if page == 0 {
			next, gerr = g.get(ctx, c, projectPath(repo)+"/merge_requests/"+n+"/diffs", url.Values{"per_page": {"100"}}, &files)
		} else {
			next, gerr = g.getURL(ctx, c, next, &files)
		}
		if gerr != nil {
			return 0, 0, false, gerr
		}
		for _, f := range files {
			for _, line := range strings.Split(f.Diff, "\n") {
				switch {
				case strings.HasPrefix(line, "+"):
					added++
				case strings.HasPrefix(line, "-"):
					removed++
				}
			}
		}
		if next == "" {
			return added, removed, false, nil
		}
	}
	return added, removed, true, nil
}

func (g *GitLab) approved(ctx context.Context, c Credential, repo Repo, n string) (bool, error) {
	var out struct {
		Approved   bool              `json:"approved"`
		ApprovedBy []json.RawMessage `json:"approved_by"`
	}
	if _, err := g.get(ctx, c, projectPath(repo)+"/merge_requests/"+n+"/approvals", nil, &out); err != nil {
		return false, err
	}
	return out.Approved || len(out.ApprovedBy) > 0, nil
}

func (g *GitLab) Checks(ctx context.Context, c Credential, repo Repo, number int) ([]Check, error) {
	m, _, err := g.mr(ctx, c, repo, number)
	if err != nil {
		return nil, err
	}
	return g.pipelineChecks(ctx, c, repo, m)
}

type glJob struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Stage         string `json:"stage"`
	Status        string `json:"status"`
	AllowFailure  bool   `json:"allow_failure"`
	WebURL        string `json:"web_url"`
	FailureReason string `json:"failure_reason"`
}

// pipelineChecks reads the jobs of the merge request's head pipeline. A
// pipeline with no jobs yet is one running check named "pipeline", so a card
// shows that something is on its way; no pipeline is no checks.
func (g *GitLab) pipelineChecks(ctx context.Context, c Credential, repo Repo, m glMR) ([]Check, error) {
	if m.HeadPipeline == nil || m.HeadPipeline.ID == 0 {
		return nil, nil
	}
	var jobs []glJob
	next := ""
	for page := 0; page < maxPages; page++ {
		var batch []glJob
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, projectPath(repo)+"/pipelines/"+strconv.Itoa(m.HeadPipeline.ID)+"/jobs", url.Values{"per_page": {"100"}}, &batch)
		} else {
			next, err = g.getURL(ctx, c, next, &batch)
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
	for _, job := range jobs {
		ch := checkOfJob(job)
		ch.RunID = strconv.Itoa(m.HeadPipeline.ID)
		checks = append(checks, ch)
	}
	if len(checks) == 0 {
		switch m.HeadPipeline.Status {
		case "failed", "canceled":
			checks = append(checks, Check{Name: "pipeline", Status: CheckFailed, Conclusion: m.HeadPipeline.Status, URL: m.HeadPipeline.WebURL})
		case "success", "skipped":
			// nothing ran and nothing failed
		default:
			checks = append(checks, Check{Name: "pipeline", Status: CheckRunning, Conclusion: m.HeadPipeline.Status, URL: m.HeadPipeline.WebURL})
		}
	}
	rank := map[CheckStatus]int{CheckFailed: 0, CheckRunning: 1, CheckPassed: 2, CheckSkipped: 3}
	sort.SliceStable(checks, func(i, j int) bool { return rank[checks[i].Status] < rank[checks[j].Status] })
	return checks, nil
}

func checkOfJob(job glJob) Check {
	ch := Check{Name: job.Name, URL: job.WebURL, Conclusion: job.Status}
	if job.ID != 0 {
		ch.ID = strconv.Itoa(job.ID)
	}
	switch job.Status {
	case "success":
		ch.Status = CheckPassed
	case "failed":
		if job.AllowFailure {
			// A job the pipeline lets fail does not fail it.
			ch.Status = CheckSkipped
			ch.Conclusion = "failed (allowed to fail)"
		} else {
			ch.Status = CheckFailed
			ch.Summary = clip(strings.ReplaceAll(job.FailureReason, "_", " "), summaryClip)
		}
	case "canceled", "canceling":
		ch.Status = CheckFailed
	case "skipped", "manual":
		ch.Status = CheckSkipped
	default: // created, pending, running, preparing, waiting_for_resource, scheduled
		ch.Status = CheckRunning
	}
	return ch
}

func (g *GitLab) Branch(ctx context.Context, c Credential, repo Repo, name string) (BranchInfo, error) {
	if err := validProject(repo); err != nil {
		return BranchInfo{}, err
	}
	var out struct {
		Protected bool `json:"protected"`
		Default   bool `json:"default"`
	}
	if _, err := g.get(ctx, c, projectPath(repo)+"/repository/branches/"+url.PathEscape(name), nil, &out); err != nil {
		return BranchInfo{}, err
	}
	return BranchInfo{Name: name, Default: out.Default, Protected: out.Protected}, nil
}
