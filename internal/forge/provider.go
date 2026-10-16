package forge

import "context"

// Provider is one code-hosting platform. Everything a caller needs from a host
// is here, in the platform-neutral vocabulary of types.go; GitHub is the first
// implementation and GitLab is the second. Adding one is a new file that
// implements this and a line where providers are listed — the UI, the stored
// shape and the MCP tools do not change.
//
// Every method takes the Credential for the one call. A provider never keeps
// it: the conditional-request cache is keyed by a digest of the token, not the
// token, so a different account cannot be served another's cached answer.
type Provider interface {
	// Kind is the stable id of the platform: "github", "gitlab".
	Kind() string
	// Terms are the words people use for this platform's change requests.
	Terms() Terms
	// Serves reports whether host is one this provider talks to.
	Serves(host string) bool
	// MergeMethods are the ways this platform can merge in one call. A caller
	// refuses an unsupported method before asking anyone.
	MergeMethods() []MergeMethod
	// ParseURL reads the address of a change request (what a person pastes to
	// attach one by hand): the repository and its number.
	ParseURL(raw string) (Repo, int, bool)

	// DefaultBranch is the repository's default branch.
	DefaultBranch(ctx context.Context, c Credential, repo Repo) (string, error)
	// Find lists change requests whose source branch is head, newest first.
	// Results are light: ChecksKnown is false and the line counts may be 0.
	Find(ctx context.Context, c Credential, repo Repo, head Head) ([]ChangeRequest, error)
	// Get reads one change request in full: size, mergeability, review
	// decision and the head commit's checks. A part the token cannot read is
	// left out and named in Notes rather than failing the whole call.
	Get(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error)
	// Checks lists every check of the head commit.
	Checks(ctx context.Context, c Credential, repo Repo, number int) ([]Check, error)
	// Comments lists conversation comments and review threads' comments.
	Comments(ctx context.Context, c Credential, repo Repo, number int) ([]Comment, error)
	// UnresolvedThreads counts review threads still open.
	UnresolvedThreads(ctx context.Context, c Credential, repo Repo, number int) (int, error)
	// Branch says whether a branch is the default or protected.
	Branch(ctx context.Context, c Credential, repo Repo, name string) (BranchInfo, error)

	// Runs lists recent workflow runs / pipelines, newest first.
	Runs(ctx context.Context, c Credential, repo Repo, q RunQuery) ([]Run, error)
	// Run reads one run with its jobs (and steps) and the annotations of the
	// jobs that failed.
	Run(ctx context.Context, c Credential, repo Repo, runID string) (RunDetail, error)
	// JobLog reads the end of one job's log. The credential is sent to the
	// host's API only: a redirect to a storage address is followed without it.
	JobLog(ctx context.Context, c Credential, repo Repo, jobID string) (LogTail, error)

	Create(ctx context.Context, c Credential, repo Repo, in CreateInput) (ChangeRequest, error)
	Update(ctx context.Context, c Credential, repo Repo, number int, in UpdateInput) (ChangeRequest, error)
	Comment(ctx context.Context, c Credential, repo Repo, number int, body string) (Comment, error)
	Reply(ctx context.Context, c Credential, repo Repo, number int, commentID, body string) (Comment, error)
	Merge(ctx context.Context, c Credential, repo Repo, number int, in MergeInput) (MergeResult, error)
	Close(ctx context.Context, c Credential, repo Repo, number int) (ChangeRequest, error)
	DeleteBranch(ctx context.Context, c Credential, repo Repo, name string) error
}

// Registry finds the provider for a host.
type Registry struct{ providers []Provider }

// NewRegistry lists providers in the order they are tried.
func NewRegistry(providers ...Provider) *Registry { return &Registry{providers: providers} }

// For is the provider that serves host, or nil.
func (r *Registry) For(host string) Provider {
	if r == nil {
		return nil
	}
	for _, p := range r.providers {
		if p.Serves(host) {
			return p
		}
	}
	return nil
}

// ParseURL tries each provider on a pasted address.
func (r *Registry) ParseURL(raw string) (Provider, Repo, int, bool) {
	if r == nil {
		return nil, Repo{}, 0, false
	}
	for _, p := range r.providers {
		if repo, n, ok := p.ParseURL(raw); ok {
			return p, repo, n, true
		}
	}
	return nil, Repo{}, 0, false
}
