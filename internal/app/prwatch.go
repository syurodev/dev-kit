package app

import (
	"context"
	"errors"
	"strings"

	"devkit/internal/accounts"
	"devkit/internal/forge"
	gitservice "devkit/internal/git"
	"devkit/internal/prwatch"
)

// NewPullRequests builds the pull-request tracker over the Git module and
// Accounts. store may be nil (attachments then last until the process ends).
func NewPullRequests(git *gitservice.Service, accountSvc *accounts.Service, picker remotePicker, store prwatch.Store) *prwatch.Service {
	providers := forge.NewRegistry(forge.NewGitHub(), forge.NewGitLab())
	return prwatch.New(providers, gitCheckout{git: git}, prCredentials{accounts: accountSvc, picker: picker}, store)
}

// gitCheckout tells prwatch where a folder stands, from the Git module.
type gitCheckout struct{ git *gitservice.Service }

func (g gitCheckout) Checkout(ctx context.Context, folder string) (prwatch.Checkout, error) {
	view, err := g.git.At(ctx, folder)
	if errors.Is(err, gitservice.ErrNoRepository) {
		return prwatch.Checkout{}, prwatch.ErrNotRepository
	}
	if err != nil {
		return prwatch.Checkout{}, err
	}
	head, err := view.Head(ctx)
	if err != nil {
		return prwatch.Checkout{}, err
	}
	co := prwatch.Checkout{
		Branch: head.Branch, Detached: head.Detached,
		Origin:        remoteRef("origin", head.OriginURL, head.OriginEnv, head.OriginAccountID),
		Push:          remoteRef(head.Remote, head.RemoteURL, head.RemoteEnv, head.RemoteAccountID),
		DefaultBranch: view.DefaultBranch(ctx, "origin"),
	}
	return co, nil
}

// remoteRef reads a remote URL into the parts a host call needs. A local path
// or an exotic scheme has no host and so no provider.
func remoteRef(name, url, env, accountID string) prwatch.RemoteRef {
	ref := prwatch.RemoteRef{Name: name, URL: url, Env: env, AccountID: accountID}
	address := gitservice.ParseAddress(url)
	if address.Transport != gitservice.TransportHTTP && address.Transport != gitservice.TransportSSH {
		return ref
	}
	ref.Host = address.Host
	ref.Path = gitservice.RepoPath(url)
	ref.Owner, _, _ = strings.Cut(ref.Path, "/")
	return ref
}

// prCredentials answers "which token calls this repository's host".
//
// An account bound to the remote wins when it is a token (a key cannot call an
// API); a key bound to it brings the token linked to it, if any; otherwise Accounts decides by host and owner, the way a push does, and
// reuses the answer remembered for it. Two accounts that fit are asked about
// only when the call is the person's or an agent's turn — never from the
// background poll, which has no one to ask.
type prCredentials struct {
	accounts *accounts.Service
	picker   remotePicker
}

func (c prCredentials) Token(ctx context.Context, req prwatch.TokenRequest) (string, string, error) {
	id := ""
	if req.BoundAccountID != "" {
		if bound, err := c.accounts.Get(req.BoundAccountID); err == nil {
			switch bound.Kind {
			case accounts.KindSSHKey:
				// The token the person linked to this key is the one that goes
				// with it: asked before guessing a token by host.
				if linked, ok := c.accounts.LinkedTokenOf(bound.ID); ok {
					id = linked.ID
				}
			case accounts.KindExternal:
			default:
				id = bound.ID
			}
		}
	}
	if id == "" {
		match, err := c.accounts.ForRemote(req.Host, req.Owner, req.Env, accounts.ViaHTTPS)
		if err != nil {
			return "", "", err
		}
		switch {
		case match.Decided:
			id = match.Account.ID
		case len(match.Options) == 0:
			return "", "", c.noToken(req)
		case !req.Interactive || c.picker.hub == nil:
			return "", "", prwatch.ErrAmbiguousAccount
		default:
			chosen, err := c.picker.PickAccount(ctx, gitservice.PickAccountInput{
				Op: "pr", Transport: gitservice.TransportHTTP, Host: req.Host, Owner: req.Owner,
				Repo: req.Repo, Env: req.Env,
			})
			if err != nil {
				return "", "", err
			}
			if chosen == "" {
				return "", "", prwatch.ErrNoAccount
			}
			id = chosen
		}
	}
	credential, err := c.accounts.Resolve(accounts.ResolveInput{AccountID: id, Env: req.Env, By: "pullrequests"})
	if err != nil {
		return "", "", err
	}
	if credential.Secret == "" {
		return "", "", prwatch.ErrNoAccount
	}
	label := ""
	if account, err := c.accounts.Get(id); err == nil {
		label = account.Label
	}
	return credential.Secret, label, nil
}

// noToken says why no token fits: when the remote has a key account (bound to
// it, or the one an SSH push would use) the person is not "without an account",
// the key just cannot call an API, and the error carries its label so the
// notice can say so. Nothing is resolved or revealed: only labels are read.
func (c prCredentials) noToken(req prwatch.TokenRequest) error {
	if req.BoundAccountID != "" {
		if bound, err := c.accounts.Get(req.BoundAccountID); err == nil && bound.Kind == accounts.KindSSHKey {
			return prwatch.NewKeyOnlyErrorFor(bound.Label, bound.ID)
		}
	}
	match, err := c.accounts.ForRemote(req.Host, req.Owner, req.Env, accounts.ViaSSH)
	switch {
	case err != nil:
		return prwatch.ErrNoAccount
	case match.Decided:
		return prwatch.NewKeyOnlyErrorFor(match.Account.Label, match.Account.ID)
	case len(match.Options) > 0:
		return prwatch.NewKeyOnlyError("")
	}
	return prwatch.ErrNoAccount
}
