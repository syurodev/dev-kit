package git

import (
	"context"
	"strings"
)

// AccountIdentity is who an account commits as, with the account's label so
// the person can see where it came from.
type AccountIdentity struct {
	Name    string
	Email   string
	Account string
}

// IdentityResolver is the half of the account picker that can say who the
// account for a remote commits as — without ever asking. A commit is local and
// quiet, and a dialog in the middle of one is not something to add: when two
// accounts could fit and none has been chosen yet, the answer is "none yet",
// and the question waits for the first push.
//
// boundAccountID is the remote's explicit binding, which wins when set.
// Optional, like the label: a picker without it leaves commits to git's own
// configuration.
type IdentityResolver interface {
	IdentityFor(ctx context.Context, in PickAccountInput, boundAccountID string) (AccountIdentity, error)
}

// Where a commit's identity came from.
const (
	// IdentityFromAccount: the account in use for the repository's remote.
	IdentityFromAccount = "account"
	// IdentityFromRepository: user.email is set in this repository's own
	// config, which is a deliberate choice and wins over an account.
	IdentityFromRepository = "repository"
	// IdentityFromGit: git's own configuration, global or system.
	IdentityFromGit = "git"
)

// CommitIdentity is who the next commit will be written as, and why.
type CommitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	// Source is IdentityFromAccount, IdentityFromRepository or IdentityFromGit;
	// empty when nobody has configured an identity at all.
	Source string `json:"source"`
	// Account is the label of the account considered for the repository's
	// remote, and AccountName/AccountEmail what it would commit as. They are
	// filled even when the repository's config wins, so the person can see the
	// two disagree.
	Account      string `json:"account"`
	AccountName  string `json:"accountName"`
	AccountEmail string `json:"accountEmail"`
	// Remote is the remote the account was looked up for.
	Remote string `json:"remote"`
}

// Differs says an account was considered and would commit as someone other than
// the identity that wins.
func (c CommitIdentity) Differs() bool {
	return c.AccountEmail != "" && c.Source != IdentityFromAccount &&
		(c.AccountEmail != c.Email || c.AccountName != c.Name)
}

// configValue reads one git config value, "" when it is unset.
func (r runner) configValue(ctx context.Context, args ...string) string {
	out, err := r.run(ctx, nil, append([]string{"config", "--get"}, args...)...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CommitIdentity says who a commit made now would be written as.
//
// The account for the repository's remote supplies the identity, because a work
// account and a personal one differ in their email as much as in their token and
// the wrong email is not something a push can be taken back from. Two things
// stand in its way, deliberately:
//
//   - a user.email in the repository's OWN config. Someone typed that in this
//     repository, which is more specific than an account chosen for its host.
//   - an account with no identity, or half of one, which leaves git's
//     configuration alone rather than mixing a name from one place with an email
//     from another.
func (s *Service) CommitIdentity(ctx context.Context) (CommitIdentity, error) {
	r, err := s.active()
	if err != nil {
		return CommitIdentity{}, err
	}
	out := CommitIdentity{
		Name:  r.configValue(ctx, "user.name"),
		Email: r.configValue(ctx, "user.email"),
	}
	if out.Name != "" || out.Email != "" {
		out.Source = IdentityFromGit
	}
	repositoryOwn := r.configValue(ctx, "--local", "user.email") != ""

	account := s.accountIdentity(ctx, &out)
	if account.Name != "" && account.Email != "" {
		out.AccountName, out.AccountEmail = account.Name, account.Email
	}
	switch {
	case repositoryOwn:
		out.Source = IdentityFromRepository
	case account.Name != "" && account.Email != "":
		out.Name, out.Email, out.Source = account.Name, account.Email, IdentityFromAccount
	}
	return out, nil
}

// accountIdentity finds who the account for the repository's remote commits as,
// recording the remote and the account's label on out.
func (s *Service) accountIdentity(ctx context.Context, out *CommitIdentity) AccountIdentity {
	resolver, ok := s.picker.(IdentityResolver)
	if !ok {
		return AccountIdentity{}
	}
	remote, ok := s.commitRemote(ctx)
	if !ok {
		return AccountIdentity{}
	}
	out.Remote = remote.Name
	address := ParseAddress(remote.URL)
	written := address.Host
	if address.Transport == TransportSSH && address.Host != "" {
		resolve := s.hostOf
		if resolve == nil {
			resolve = defaultHostOf
		}
		address.Host = resolve(ctx, address.Host)
	}
	identity, err := resolver.IdentityFor(ctx, PickAccountInput{
		Op: "commit", Remote: remote.Name, Transport: address.Transport,
		Host: address.Host, Alias: aliasOf(written, address.Host), Owner: address.Owner, Repo: address.Repo, Env: remote.Env,
	}, remote.AccountID)
	if err != nil {
		return AccountIdentity{}
	}
	out.Account = identity.Account
	return identity
}

// commitRemote is the remote a commit here is headed for: the one the branch
// tracks, else origin, else the only remote there is. Several remotes and
// none of those is "unknown", not a guess.
func (s *Service) commitRemote(ctx context.Context) (Remote, bool) {
	remotes, err := s.Remotes(ctx)
	if err != nil || len(remotes) == 0 {
		return Remote{}, false
	}
	if status, err := s.Status(ctx); err == nil && status.Upstream != "" {
		var tracked Remote
		for _, candidate := range remotes {
			if strings.HasPrefix(status.Upstream, candidate.Name+"/") && len(candidate.Name) > len(tracked.Name) {
				tracked = candidate
			}
		}
		if tracked.Name != "" {
			return tracked, true
		}
	}
	for _, candidate := range remotes {
		if candidate.Name == "origin" {
			return candidate, true
		}
	}
	if len(remotes) == 1 {
		return remotes[0], true
	}
	return Remote{}, false
}

// identityEnv is the environment that writes a commit as the account. Author
// and committer both: a commit signed off as one person and committed by
// another is what the two-account problem looks like in git log.
func identityEnv(identity CommitIdentity, amend bool) []string {
	env := []string{
		"GIT_COMMITTER_NAME=" + identity.Name,
		"GIT_COMMITTER_EMAIL=" + identity.Email,
	}
	if !amend {
		// An amend keeps the author the commit already had; only the person
		// amending is new.
		env = append(env, "GIT_AUTHOR_NAME="+identity.Name, "GIT_AUTHOR_EMAIL="+identity.Email)
	}
	return env
}
