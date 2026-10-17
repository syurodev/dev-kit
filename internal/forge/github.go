package forge

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub is the Provider for github.com.
//
// Not yet: GitHub Enterprise Server. It serves another host with another API
// root, and DevKit's account registry knows no such host, so there would be no
// token to send. Serves is exact for the same reason a token is never sent to
// a host it was not issued for.
type GitHub struct {
	client *http.Client
	base   string
	cache  *etagCache
	slots  chan struct{}

	mu    sync.Mutex
	stats Stats
}

// GitHubOption configures NewGitHub.
type GitHubOption func(*GitHub)

// WithHTTPClient replaces the HTTP client (timeouts, transport).
func WithHTTPClient(client *http.Client) GitHubOption { return func(g *GitHub) { g.client = client } }

// WithAPIBase points the provider at another API root. It exists for tests,
// which stand a fake host up with httptest; Serves still answers for github.com.
func WithAPIBase(base string) GitHubOption {
	return func(g *GitHub) { g.base = strings.TrimRight(base, "/") }
}

// WithConcurrency bounds requests in flight.
func WithConcurrency(n int) GitHubOption {
	return func(g *GitHub) {
		if n > 0 {
			g.slots = make(chan struct{}, n)
		}
	}
}

func NewGitHub(opts ...GitHubOption) *GitHub {
	g := &GitHub{
		client: &http.Client{Timeout: 20 * time.Second},
		base:   githubAPI,
		cache:  newETagCache(512),
		slots:  make(chan struct{}, defaultConcurrent),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Stats is a snapshot of the request counters.
func (g *GitHub) Stats() Stats {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stats
}

func (g *GitHub) Kind() string { return "github" }
func (g *GitHub) Terms() Terms {
	return Terms{Platform: "GitHub", Short: "PR", Long: "pull request", Checks: "checks"}
}

func (g *GitHub) Serves(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "github.com" || host == "www.github.com"
}

var githubPullURL = regexp.MustCompile(`^/([^/]+)/([^/]+)/pull/(\d+)(?:/.*)?$`)

func (g *GitHub) ParseURL(raw string) (Repo, int, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !g.Serves(u.Hostname()) {
		return Repo{}, 0, false
	}
	m := githubPullURL.FindStringSubmatch(u.Path)
	if m == nil {
		return Repo{}, 0, false
	}
	n, _ := strconv.Atoi(m[3])
	if n <= 0 {
		return Repo{}, 0, false
	}
	return Repo{Host: "github.com", Path: m[1] + "/" + m[2]}, n, true
}

// ---- wire shapes -----------------------------------------------------

type ghPull struct {
	Number       int                    `json:"number"`
	Title        string                 `json:"title"`
	Body         string                 `json:"body"`
	HTMLURL      string                 `json:"html_url"`
	State        string                 `json:"state"`
	Draft        bool                   `json:"draft"`
	Merged       bool                   `json:"merged"`
	MergedAt     *string                `json:"merged_at"`
	Additions    int                    `json:"additions"`
	Deletions    int                    `json:"deletions"`
	ChangedFiles int                    `json:"changed_files"`
	Mergeable    *bool                  `json:"mergeable"`
	MergeState   string                 `json:"mergeable_state"`
	UpdatedAt    time.Time              `json:"updated_at"`
	User         struct{ Login string } `json:"user"`
	Head         struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (g *GitHub) changeRequest(repo Repo, p ghPull) ChangeRequest {
	state := StateOpen
	switch {
	case p.Merged || p.MergedAt != nil:
		state = StateMerged
	case p.State == "closed":
		state = StateClosed
	}
	cr := ChangeRequest{
		Repo: repo, Number: p.Number, Title: p.Title, URL: p.HTMLURL, State: state, Draft: p.Draft,
		Author: p.User.Login, Head: p.Head.Ref, HeadSHA: p.Head.SHA, Base: p.Base.Ref, Body: p.Body,
		Additions: p.Additions, Deletions: p.Deletions, ChangedFiles: p.ChangedFiles,
		Mergeable: mergeableOf(p), Decision: DecisionNone, UnresolvedThreads: -1,
		UpdatedAt: p.UpdatedAt, Terms: g.Terms(),
	}
	if p.Head.Repo != nil {
		cr.HeadRepo = p.Head.Repo.FullName
	}
	return cr
}

func mergeableOf(p ghPull) Mergeable {
	if p.Mergeable == nil {
		return MergeUnknown
	}
	switch p.MergeState {
	case "dirty":
		return MergeConflicting
	case "blocked", "draft":
		return MergeBlocked
	case "behind":
		return MergeBehind
	case "clean", "has_hooks", "unstable":
		return MergeClean
	}
	if !*p.Mergeable {
		return MergeConflicting
	}
	return MergeUnknown
}

// ---- reads -----------------------------------------------------------

func (g *GitHub) DefaultBranch(ctx context.Context, c Credential, repo Repo) (string, error) {
	if err := validRepo(repo); err != nil {
		return "", err
	}
	var out struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.get(ctx, c, repoPath(repo), nil, &out); err != nil {
		return "", err
	}
	return out.DefaultBranch, nil
}

func (g *GitHub) Find(ctx context.Context, c Credential, repo Repo, head Head) ([]ChangeRequest, error) {
	if err := validRepo(repo); err != nil {
		return nil, err
	}
	if head.Branch == "" {
		return nil, &Error{Kind: ErrInvalid, Message: "no branch to look for"}
	}
	owner := head.Owner
	if owner == "" {
		owner, _, _ = strings.Cut(repo.Path, "/")
	}
	q := url.Values{}
	q.Set("head", owner+":"+head.Branch)
	q.Set("state", "all")
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	q.Set("per_page", "10")
	var pulls []ghPull
	if _, err := g.get(ctx, c, repoPath(repo)+"/pulls", q, &pulls); err != nil {
		return nil, err
	}
	out := make([]ChangeRequest, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, g.changeRequest(repo, p))
	}
	return out, nil
}

func (g *GitHub) Get(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error) {
	if err := validRepo(repo); err != nil {
		return ChangeRequest{}, err
	}
	n, err := numberPath(number)
	if err != nil {
		return ChangeRequest{}, err
	}
	var pull ghPull
	if _, err := g.get(ctx, c, repoPath(repo)+"/pulls/"+n, nil, &pull); err != nil {
		return ChangeRequest{}, err
	}
	cr := g.changeRequest(repo, pull)

	// The parts below are separate calls with separate permissions: a
	// fine-grained token may read the pull request and not its checks. Losing
	// one is a note, not a failure — the card still says what it knows.
	checks, err := g.headChecks(ctx, c, repo, pull.Head.SHA)
	if err != nil {
		cr.Notes = append(cr.Notes, "checks unavailable: "+err.Error())
	} else {
		cr.Checks, cr.ChecksKnown = summarize(checks), true
	}
	decision, err := g.decision(ctx, c, repo, n)
	if err != nil {
		cr.Notes = append(cr.Notes, "reviews unavailable: "+err.Error())
	} else {
		cr.Decision = decision
	}
	return cr, nil
}

func summarize(checks []Check) CheckSummary {
	var s CheckSummary
	s.Total = len(checks)
	for _, ch := range checks {
		switch ch.Status {
		case CheckPassed:
			s.Passed++
		case CheckSkipped:
			s.Skipped++
			s.Passed++
		case CheckRunning:
			s.Running++
		case CheckFailed:
			s.Failed++
			if len(s.Failing) < maxFailingChecks {
				s.Failing = append(s.Failing, ch)
			}
		}
	}
	return s
}

func (g *GitHub) Checks(ctx context.Context, c Credential, repo Repo, number int) ([]Check, error) {
	if err := validRepo(repo); err != nil {
		return nil, err
	}
	n, err := numberPath(number)
	if err != nil {
		return nil, err
	}
	var pull ghPull
	if _, err := g.get(ctx, c, repoPath(repo)+"/pulls/"+n, nil, &pull); err != nil {
		return nil, err
	}
	return g.headChecks(ctx, c, repo, pull.Head.SHA)
}

type ghCheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

// headChecks reads the check runs (GitHub Actions and Apps) and the commit
// statuses (older CI integrations) of a commit: a repository may use either,
// and a PR whose CI reports statuses would otherwise show no checks at all.
func (g *GitHub) headChecks(ctx context.Context, c Credential, repo Repo, sha string) ([]Check, error) {
	if sha == "" {
		return nil, &Error{Kind: ErrInvalid, Message: "no head commit"}
	}
	var checks []Check
	q := url.Values{"per_page": {"100"}}
	next := ""
	for page := 0; page < maxPages; page++ {
		var out struct {
			CheckRuns []ghCheckRun `json:"check_runs"`
		}
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, repoPath(repo)+"/commits/"+url.PathEscape(sha)+"/check-runs", q, &out)
		} else {
			next, err = g.getURL(ctx, c, next, &out)
		}
		if err != nil {
			return nil, err
		}
		for _, run := range out.CheckRuns {
			checks = append(checks, checkOfRun(run))
		}
		if next == "" {
			break
		}
	}
	var combined struct {
		Statuses []struct {
			State       string `json:"state"`
			Context     string `json:"context"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
		} `json:"statuses"`
	}
	if _, err := g.get(ctx, c, repoPath(repo)+"/commits/"+url.PathEscape(sha)+"/status", url.Values{"per_page": {"100"}}, &combined); err != nil {
		return nil, err
	}
	for _, st := range combined.Statuses {
		ch := Check{Name: st.Context, URL: st.TargetURL, Summary: clip(st.Description, summaryClip), Conclusion: st.State}
		switch st.State {
		case "success":
			ch.Status = CheckPassed
		case "pending":
			ch.Status = CheckRunning
		default: // failure, error
			ch.Status = CheckFailed
		}
		checks = append(checks, ch)
	}
	sortChecks(checks)
	return checks, nil
}

func checkOfRun(run ghCheckRun) Check {
	ch := Check{Name: run.Name, URL: run.HTMLURL, Conclusion: run.Conclusion}
	switch run.Status {
	case "completed":
		switch run.Conclusion {
		case "success":
			ch.Status = CheckPassed
		case "neutral":
			ch.Status = CheckPassed
		case "skipped":
			ch.Status = CheckSkipped
		default: // failure, timed_out, cancelled, action_required, stale, startup_failure
			ch.Status = CheckFailed
		}
	default: // queued, in_progress, waiting, requested, pending
		ch.Status = CheckRunning
	}
	if ch.Status == CheckFailed {
		text := run.Output.Title
		if text == "" {
			text = run.Output.Summary
		}
		ch.Summary = clip(firstLine(text), summaryClip)
	}
	return ch
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

// decision reads the verdict from the reviews: each reviewer's latest vote
// that is not a plain comment. It is not GitHub's own reviewDecision, which
// also knows the branch protection's required reviewers (GraphQL only).
func (g *GitHub) decision(ctx context.Context, c Credential, repo Repo, n string) (Decision, error) {
	type review struct {
		State string                 `json:"state"`
		User  struct{ Login string } `json:"user"`
	}
	latest := map[string]string{}
	next := ""
	for page := 0; page < maxPages; page++ {
		var reviews []review
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, repoPath(repo)+"/pulls/"+n+"/reviews", url.Values{"per_page": {"100"}}, &reviews)
		} else {
			next, err = g.getURL(ctx, c, next, &reviews)
		}
		if err != nil {
			return DecisionNone, err
		}
		for _, r := range reviews {
			switch r.State {
			case "APPROVED", "CHANGES_REQUESTED":
				latest[r.User.Login] = r.State
			case "DISMISSED":
				delete(latest, r.User.Login)
			}
		}
		if next == "" {
			break
		}
	}
	approved := false
	for _, state := range latest {
		if state == "CHANGES_REQUESTED" {
			return DecisionChangesRequested, nil
		}
		approved = true
	}
	if approved {
		return DecisionApproved, nil
	}
	return DecisionNone, nil
}

func (g *GitHub) Branch(ctx context.Context, c Credential, repo Repo, name string) (BranchInfo, error) {
	if err := validRepo(repo); err != nil {
		return BranchInfo{}, err
	}
	def, err := g.DefaultBranch(ctx, c, repo)
	if err != nil {
		return BranchInfo{}, err
	}
	var out struct {
		Protected bool `json:"protected"`
	}
	if _, err := g.get(ctx, c, repoPath(repo)+"/branches/"+escapeRef(name), nil, &out); err != nil {
		return BranchInfo{}, err
	}
	return BranchInfo{Name: name, Default: name == def, Protected: out.Protected}, nil
}

// escapeRef escapes a branch name for a path: each segment, keeping the slashes.
func escapeRef(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
