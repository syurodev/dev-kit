package git

import (
	"context"
	"net/url"
	"strings"
)

// RepoPath is the repository's path on its host, as the remote wrote it —
// "acme/api", or "group/sub/project" for a nested GitLab group — without a
// trailing .git and with the case kept. Address.Owner and Repo are the two
// ends of it, lower-cased for matching accounts; a host API wants the whole.
// Empty for a local path or a URL with no repository in it.
func RepoPath(raw string) string {
	raw = strings.TrimSpace(raw)
	address := ParseAddress(raw)
	if address.Transport != TransportHTTP && address.Transport != TransportSSH {
		return ""
	}
	var path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		path = u.Path
	} else {
		_, path, _ = strings.Cut(raw, ":")
	}
	var segments []string
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) < 2 {
		return ""
	}
	segments[len(segments)-1] = strings.TrimSuffix(segments[len(segments)-1], ".git")
	return strings.Join(segments, "/")
}

// Head is where the work tree stands and where its branch goes when pushed.
type Head struct {
	// Branch is the checked-out branch; empty when HEAD is detached.
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	// Remote is the remote the branch is pushed to: its pushRemote, else
	// remote.pushDefault, else the branch's own remote, else origin.
	Remote    string `json:"remote"`
	RemoteURL string `json:"remoteUrl"`
	// Origin is "origin"'s URL — where a pull request for a fork's branch is
	// opened. Equal to RemoteURL for a branch pushed to origin.
	OriginURL string `json:"originUrl"`
	// OriginEnv and OriginAccountID are the same for origin: the account a
	// pull request is looked up and opened with follows the repository, not the
	// fork the branch is pushed to.
	OriginEnv       string `json:"originEnv"`
	OriginAccountID string `json:"originAccountId"`
	// RemoteEnv is the environment tag of Remote, "" when untagged.
	RemoteEnv string `json:"remoteEnv"`
	// RemoteAccountID is the account bound to Remote, "" when none.
	RemoteAccountID string `json:"remoteAccountId"`
}

// Head reads the branch and its push remote without touching the work tree.
// It is cheap — a few config reads — which is what lets a card ask on every
// poll without costing a `git status`.
func (s *Service) Head(ctx context.Context) (Head, error) {
	r, err := s.active()
	if err != nil {
		return Head{}, err
	}
	var head Head
	if out, err := r.run(ctx, nil, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		head.Branch = strings.TrimSpace(string(out))
	}
	head.Detached = head.Branch == ""
	if !head.Detached {
		head.Remote = r.configValue(ctx, "branch."+head.Branch+".pushRemote")
		if head.Remote == "" {
			head.Remote = r.configValue(ctx, "remote.pushDefault")
		}
		if head.Remote == "" {
			head.Remote = r.configValue(ctx, "branch."+head.Branch+".remote")
		}
	}
	if head.Remote == "" || head.Remote == "." {
		head.Remote = "origin"
	}
	remotes, err := s.Remotes(ctx)
	if err != nil {
		return head, err
	}
	for _, remote := range remotes {
		switch remote.Name {
		case "origin":
			head.OriginURL, head.OriginEnv, head.OriginAccountID = remote.URL, remote.Env, remote.AccountID
		}
		if remote.Name == head.Remote {
			head.RemoteURL, head.RemoteEnv, head.RemoteAccountID = remote.URL, remote.Env, remote.AccountID
		}
	}
	return head, nil
}
