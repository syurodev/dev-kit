package forge

import "time"

// Repo is a repository on a host. Path is "owner/name" on GitHub and
// "group/subgroup/project" on GitLab; it keeps the case the host reported.
type Repo struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

// String is "host/path", the form shown to a person.
func (r Repo) String() string { return r.Host + "/" + r.Path }

// Credential is what a provider authenticates with. It lives in memory for one
// call and is never serialised: it has no JSON tags on purpose, and nothing
// that crosses the bridge or MCP carries one.
type Credential struct {
	Token string
}

// Terms are the words a platform uses, so the UI never hard-codes "PR".
type Terms struct {
	// Platform is "GitHub" or "GitLab".
	Platform string `json:"platform"`
	// Short is "PR" or "MR".
	Short string `json:"short"`
	// Long is "pull request" or "merge request".
	Long string `json:"long"`
	// Checks is "checks" or "pipelines".
	Checks string `json:"checks"`
}

// State of a change request.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
	StateMerged State = "merged"
)

// Terminal reports whether nothing more will happen to the change request:
// polling stops there.
func (s State) Terminal() bool { return s == StateClosed || s == StateMerged }

// Mergeable is whether the host can merge it now, as far as it can say.
type Mergeable string

const (
	// MergeUnknown: the host has not worked it out yet (GitHub computes it
	// lazily) or does not say.
	MergeUnknown Mergeable = "unknown"
	// MergeClean: no conflict and nothing blocking.
	MergeClean Mergeable = "clean"
	// MergeConflicting: the branch conflicts with its base.
	MergeConflicting Mergeable = "conflicting"
	// MergeBlocked: no conflict, but a rule stops it (required checks or reviews, a draft).
	MergeBlocked Mergeable = "blocked"
	// MergeBehind: the branch is behind its base and the repository requires it to be current.
	MergeBehind Mergeable = "behind"
)

// Decision is the review verdict so far.
type Decision string

const (
	DecisionNone             Decision = ""
	DecisionApproved         Decision = "approved"
	DecisionChangesRequested Decision = "changes_requested"
)

// CheckStatus is one check's outcome, collapsed to what a person acts on.
type CheckStatus string

const (
	CheckPassed  CheckStatus = "passed"
	CheckFailed  CheckStatus = "failed"
	CheckRunning CheckStatus = "running"
	CheckSkipped CheckStatus = "skipped"
)

// Check is one CI job: a GitHub check run or commit status, a GitLab job.
type Check struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	// Conclusion is the host's own word ("timed_out", "action_required") when
	// it says more than Status does.
	Conclusion string `json:"conclusion,omitempty"`
	URL        string `json:"url,omitempty"`
	// Summary is a short reason the host reported, clipped. Third-party text.
	Summary string `json:"summary,omitempty"`
}

// CheckSummary counts the checks of a change request's head commit.
type CheckSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Running int `json:"running"`
	Skipped int `json:"skipped"`
	// Failing names the failed checks, bounded; the full list is Checks().
	Failing []Check `json:"failing,omitempty"`
}

// Settled reports that nothing is still running.
func (c CheckSummary) Settled() bool { return c.Running == 0 }

// ChangeRequest is a pull request or merge request, with the state a person
// wants at a glance. The same shape for every platform.
type ChangeRequest struct {
	Repo     Repo   `json:"repo"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	State    State  `json:"state"`
	Draft    bool   `json:"draft"`
	Author   string `json:"author,omitempty"`
	Head     string `json:"head"`
	HeadRepo string `json:"headRepo,omitempty"`
	HeadSHA  string `json:"headSha,omitempty"`
	Base     string `json:"base"`
	// Body is the description; third-party text. Empty in list results.
	Body         string       `json:"body,omitempty"`
	Additions    int          `json:"additions"`
	Deletions    int          `json:"deletions"`
	ChangedFiles int          `json:"changedFiles"`
	Mergeable    Mergeable    `json:"mergeable"`
	Decision     Decision     `json:"reviewDecision"`
	Checks       CheckSummary `json:"checks"`
	// ChecksKnown is false while the checks have not been read (a list result).
	ChecksKnown bool `json:"checksKnown"`
	// UnresolvedThreads is -1 when it has not been read.
	UnresolvedThreads int       `json:"unresolvedThreads"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Terms             Terms     `json:"terms"`
	// Notes say what could not be read (checks the token may not see), so a
	// missing number is never mistaken for a zero.
	Notes []string `json:"notes,omitempty"`
}

// CommentKind says where a comment lives.
type CommentKind string

const (
	// CommentConversation is on the change request as a whole.
	CommentConversation CommentKind = "conversation"
	// CommentReview is on a line of the diff, in a thread.
	CommentReview CommentKind = "review"
)

// Comment is one comment. Third-party text: a reader treats Body as data.
type Comment struct {
	ID       string      `json:"id"`
	Kind     CommentKind `json:"kind"`
	Author   string      `json:"author"`
	Body     string      `json:"body"`
	Path     string      `json:"path,omitempty"`
	Line     int         `json:"line,omitempty"`
	ThreadID string      `json:"threadId,omitempty"`
	// Resolved and Outdated describe the thread of a review comment.
	Resolved  bool      `json:"resolved,omitempty"`
	Outdated  bool      `json:"outdated,omitempty"`
	ReplyTo   string    `json:"replyTo,omitempty"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Head names the branch a change request comes from: Owner is the namespace
// that holds it (the same as the repository's for a branch pushed there, the
// fork's for a fork).
type Head struct {
	Owner  string
	Branch string
}

// CreateInput opens a change request.
type CreateInput struct {
	Title string
	Body  string
	// Head is the branch to merge; Base the branch it merges into.
	Head  string
	Base  string
	Draft bool
}

// UpdateInput edits a change request. A nil field is left as it is.
type UpdateInput struct {
	Title *string
	Body  *string
}

// MergeMethod is how a change request is merged.
type MergeMethod string

const (
	MergeMethodMerge  MergeMethod = "merge"
	MergeMethodSquash MergeMethod = "squash"
	MergeMethodRebase MergeMethod = "rebase"
)

// Valid reports whether m is one of the three.
func (m MergeMethod) Valid() bool {
	return m == MergeMethodMerge || m == MergeMethodSquash || m == MergeMethodRebase
}

// MergeInput merges a change request.
type MergeInput struct {
	Method MergeMethod
	// Title and Body are the merge commit's message; empty keeps the host's default.
	Title string
	Body  string
	// SHA is the head commit the caller looked at. The host refuses to merge
	// if the branch has moved since: what was confirmed is what lands.
	SHA string
}

// MergeResult is what a merge answers.
type MergeResult struct {
	Merged  bool   `json:"merged"`
	SHA     string `json:"sha,omitempty"`
	Message string `json:"message,omitempty"`
}

// BranchInfo says whether a branch may be deleted: never the default one, and
// never one the host protects.
type BranchInfo struct {
	Name      string `json:"name"`
	Default   bool   `json:"default"`
	Protected bool   `json:"protected"`
}
