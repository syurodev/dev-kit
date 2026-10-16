package prwatch

import (
	"context"
	"sort"
	"strings"
	"sync"

	"devkit/internal/forge"
)

// OpenInput says which change request a call is about.
type OpenInput struct {
	// Folder is the work tree. Number 0 means "the one for this folder's branch".
	Folder string
	Number int
	// SessionID is the Agent Hub session on whose behalf this happens, "" for a
	// person's own terminal. It is what a created request is attached to.
	SessionID string
	// Interactive lets the account choice ask the person.
	Interactive bool
}

// Handle is one repository on one host with the credential to call it. The
// credential is a private field: tools receive a Handle, never the token.
type Handle struct {
	svc      *Service
	provider forge.Provider
	cred     forge.Credential
	repo     forge.Repo
	number   int
	session  string
	checkout Checkout
	account  string

	mu      sync.Mutex
	defBr   string
	defOnce bool
}

// Open resolves a folder to a repository, a provider and a token.
func (s *Service) Open(ctx context.Context, in OpenInput) (*Handle, error) {
	co, err := s.src.Checkout(ctx, in.Folder)
	if err != nil {
		return nil, err
	}
	co.Folder = in.Folder
	provider := s.providers.For(co.Origin.Host)
	if provider == nil || co.Origin.Path == "" {
		return nil, ErrUnsupportedHost
	}
	token, account, err := s.creds.Token(ctx, TokenRequest{
		Host: co.Origin.Host, Owner: co.Origin.Owner, Repo: co.Origin.Path,
		Env: co.Origin.Env, BoundAccountID: co.Origin.AccountID, Interactive: in.Interactive,
	})
	if err != nil {
		return nil, err
	}
	return &Handle{
		svc: s, provider: provider, cred: forge.Credential{Token: token},
		repo:   forge.Repo{Host: co.Origin.Host, Path: co.Origin.Path},
		number: in.Number, session: in.SessionID, checkout: co, account: account,
	}, nil
}

// Repo is the repository change requests are opened in.
func (h *Handle) Repo() forge.Repo { return h.repo }

// Branch is the folder's checked-out branch.
func (h *Handle) Branch() string { return h.checkout.Branch }

// Remote is the remote the branch is pushed to (its name and environment tag
// decide whether a write must ask).
func (h *Handle) Remote() RemoteRef { return h.checkout.Push }

// Account names the account the calls are made as.
func (h *Handle) Account() string { return h.account }

// Terms are the words of this handle's platform.
func (h *Handle) Terms() forge.Terms { return h.provider.Terms() }

// Number is the change request being acted on; 0 until resolved by branch.
func (h *Handle) Number() int { return h.number }

// headParam is the head a request for the folder's branch is opened from:
// the bare branch, or owner:branch when it lives on a fork.
func (h *Handle) head() forge.Head {
	owner := h.checkout.Push.Owner
	if h.checkout.Push.Path != "" {
		owner, _, _ = strings.Cut(h.checkout.Push.Path, "/")
	}
	if !strings.EqualFold(h.checkout.Push.Path, h.repo.Path) && h.checkout.Push.Path != "" {
		return forge.Head{Owner: owner, Branch: h.checkout.Branch}
	}
	repoOwner, _, _ := strings.Cut(h.repo.Path, "/")
	return forge.Head{Owner: repoOwner, Branch: h.checkout.Branch}
}

// HeadParam is the head to open a request with.
func (h *Handle) HeadParam() string {
	head := h.head()
	if strings.EqualFold(h.checkout.Push.Path, h.repo.Path) || h.checkout.Push.Path == "" {
		return head.Branch
	}
	return head.Owner + ":" + head.Branch
}

// SameRepoBranch reports whether the branch is pushed to the repository the
// request is opened in (not a fork). A write limited to "branches this
// session may push" is only meaningful for those.
func (h *Handle) SameRepoBranch() bool {
	return h.checkout.Push.Path == "" || strings.EqualFold(h.checkout.Push.Path, h.repo.Path)
}

// DefaultBranch is the repository's default branch: git's last word if it has
// one, else the host's.
func (h *Handle) DefaultBranch(ctx context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.checkout.DefaultBranch != "" {
		return h.checkout.DefaultBranch, nil
	}
	if h.defOnce {
		return h.defBr, nil
	}
	name, err := h.provider.DefaultBranch(ctx, h.cred, h.repo)
	if err != nil {
		return "", err
	}
	h.defBr, h.defOnce = name, true
	return name, nil
}

// List is every change request whose source is the folder's branch, newest
// first.
func (h *Handle) List(ctx context.Context) ([]forge.ChangeRequest, error) {
	if h.checkout.Detached || h.checkout.Branch == "" {
		return nil, ErrDetached
	}
	return h.svc.find(ctx, h.provider, h.cred, h.repo, h.head())
}

// resolve fixes the number: the one asked for, or the branch's open request
// (else its most recent).
func (h *Handle) resolve(ctx context.Context) (int, error) {
	if h.number > 0 {
		return h.number, nil
	}
	list, err := h.List(ctx)
	if err != nil {
		return 0, err
	}
	best := pick(list)
	if best == nil {
		return 0, ErrNoChangeRequest
	}
	h.number = best.Number
	return h.number, nil
}

// pick prefers an open request, then the most recently updated.
func pick(list []forge.ChangeRequest) *forge.ChangeRequest {
	if len(list) == 0 {
		return nil
	}
	sorted := append([]forge.ChangeRequest(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool {
		oi, oj := sorted[i].State == forge.StateOpen, sorted[j].State == forge.StateOpen
		if oi != oj {
			return oi
		}
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})
	return &sorted[0]
}

// ChangeRequest is the full state, with the unresolved-thread count.
func (h *Handle) ChangeRequest(ctx context.Context) (forge.ChangeRequest, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	return h.svc.fetch(ctx, h.provider, h.cred, h.repo, n, true)
}

func (h *Handle) Checks(ctx context.Context) ([]forge.Check, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return h.provider.Checks(ctx, h.cred, h.repo, n)
}

func (h *Handle) Comments(ctx context.Context) ([]forge.Comment, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return h.provider.Comments(ctx, h.cred, h.repo, n)
}

// ---- writes ------------------------------------------------------------

func (h *Handle) wrote(n int) {
	h.svc.forget(h.repo, n)
	h.svc.changed(h.session)
}

// Create opens a request for the folder's branch and attaches it to the
// session at once, so its card does not wait for the branch lookup.
func (h *Handle) Create(ctx context.Context, in forge.CreateInput) (forge.ChangeRequest, error) {
	if h.checkout.Detached || h.checkout.Branch == "" {
		return forge.ChangeRequest{}, ErrDetached
	}
	if in.Head == "" {
		in.Head = h.HeadParam()
	}
	cr, err := h.provider.Create(ctx, h.cred, h.repo, in)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	h.number = cr.Number
	if h.session != "" {
		_ = h.svc.store.Put(ctx, Attachment{
			SessionID: h.session, Folder: folderOf(h), Host: h.repo.Host, Path: h.repo.Path,
			Number: cr.Number, URL: cr.URL, Source: "created", CreatedAt: h.svc.now(),
		})
	}
	h.wrote(cr.Number)
	return cr, nil
}

func folderOf(h *Handle) string { return h.checkout.Folder }

func (h *Handle) Update(ctx context.Context, in forge.UpdateInput) (forge.ChangeRequest, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	cr, err := h.provider.Update(ctx, h.cred, h.repo, n, in)
	if err == nil {
		h.wrote(n)
	}
	return cr, err
}

func (h *Handle) Comment(ctx context.Context, body string) (forge.Comment, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return forge.Comment{}, err
	}
	c, err := h.provider.Comment(ctx, h.cred, h.repo, n, body)
	if err == nil {
		h.wrote(n)
	}
	return c, err
}

func (h *Handle) Reply(ctx context.Context, commentID, body string) (forge.Comment, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return forge.Comment{}, err
	}
	c, err := h.provider.Reply(ctx, h.cred, h.repo, n, commentID, body)
	if err == nil {
		h.wrote(n)
	}
	return c, err
}

func (h *Handle) Merge(ctx context.Context, in forge.MergeInput) (forge.MergeResult, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return forge.MergeResult{}, err
	}
	res, err := h.provider.Merge(ctx, h.cred, h.repo, n, in)
	if err == nil {
		h.wrote(n)
	}
	return res, err
}

func (h *Handle) Close(ctx context.Context) (forge.ChangeRequest, error) {
	n, err := h.resolve(ctx)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	cr, err := h.provider.Close(ctx, h.cred, h.repo, n)
	if err == nil {
		h.wrote(n)
	}
	return cr, err
}

// CheckDeletable refuses the default branch and a protected one. A caller
// that asks the person first calls it before asking: there is no point in
// putting a question to someone about something that will be refused.
func (h *Handle) CheckDeletable(ctx context.Context, name string) error {
	info, err := h.provider.Branch(ctx, h.cred, h.repo, name)
	if err != nil {
		return err
	}
	if info.Default || info.Protected {
		return ErrProtectedBranch
	}
	return nil
}

// DeleteBranch deletes a branch of the repository on the host. The default
// branch and a protected one are refused here, before the host is asked to
// delete anything: the host would refuse the protected one, but not the
// default on every plan, and "the host would have said no" is not the rule.
func (h *Handle) DeleteBranch(ctx context.Context, name string) error {
	if err := h.CheckDeletable(ctx, name); err != nil {
		return err
	}
	if err := h.provider.DeleteBranch(ctx, h.cred, h.repo, name); err != nil {
		return err
	}
	h.svc.forget(h.repo, h.number)
	h.svc.changed(h.session)
	return nil
}

// ---- shared reads ------------------------------------------------------

// find caches a branch lookup for a few seconds.
func (s *Service) find(ctx context.Context, p forge.Provider, c forge.Credential, repo forge.Repo, head forge.Head) ([]forge.ChangeRequest, error) {
	key := repo.String() + "|" + head.Owner + ":" + head.Branch
	s.mu.Lock()
	cached, ok := s.finds[key]
	s.mu.Unlock()
	if ok && s.now().Sub(cached.at) < minAge {
		return cached.list, nil
	}
	list, err := p.Find(ctx, c, repo, head)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.finds[key] = found{at: s.now(), list: list}
	s.mu.Unlock()
	return list, nil
}

// fetch reads a change request through the cache. A merged or closed one is
// served from cache for good: nothing more will happen to it. withThreads
// asks for the unresolved-thread count when it is due.
func (s *Service) fetch(ctx context.Context, p forge.Provider, c forge.Credential, repo forge.Repo, number int, withThreads bool) (forge.ChangeRequest, error) {
	e := s.entryFor(repo, number)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := s.now()
	if e.have && (e.cr.State.Terminal() || now.Sub(e.at) < minAge) {
		return e.cr, nil
	}
	cr, err := p.Get(ctx, c, repo, number)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	cr.UnresolvedThreads = e.cr.UnresolvedThreads
	if !e.have {
		cr.UnresolvedThreads = -1
	}
	// The count is re-read when the request changed, when it was never read,
	// or on a timer (a resolved thread does not move updated_at).
	if withThreads && cr.State == forge.StateOpen &&
		(!e.have || e.cr.UnresolvedThreads < 0 || !cr.UpdatedAt.Equal(e.threadsFor) || now.Sub(e.threadsAt) >= threadsEvery) {
		if n, err := p.UnresolvedThreads(ctx, c, repo, number); err == nil {
			cr.UnresolvedThreads, e.threadsAt, e.threadsFor = n, now, cr.UpdatedAt
		} else if kind := forge.KindOf(err); kind == forge.ErrRateLimited {
			return forge.ChangeRequest{}, err
		}
	}
	e.cr, e.have, e.at = cr, true, now
	return cr, nil
}
