package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Remote is one configured remote and the account that authenticates to it.
type Remote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Transport is how git reaches the remote. It decides whether AccountID
	// can do anything: a bound account answers an HTTPS remote's password
	// prompt and has no way to answer an SSH remote, which authenticates by key.
	Transport Transport `json:"transport"`
	// AccountID is the credential binding. Empty means whatever git's own
	// configuration already does (ssh keys, a credential helper).
	AccountID string `json:"accountId"`
	// Env is the environment tag the remote counts as, for confirmation.
	Env string `json:"env"`
}

// RemoteStore holds the remote-to-account bindings. They are vault items: a
// binding names a credential and an environment, the same shape db-profile has.
type RemoteStore interface {
	ListRemotes(projectKey string) ([]Remote, error)
}

// SetRemotes wires the binding store.
func (s *Service) SetRemotes(r RemoteStore) { s.remotes = r }

// AccountPicker decides which account authenticates a remote that has no
// binding of its own. It is internal/accounts plus the interaction hub, declared
// here as an interface so git imports neither.
//
// An empty id with no error means no account applies, and git's own
// configuration answers. An error is final: a question nobody could answer
// must stop the command, not fall back to some account.
type AccountPicker interface {
	PickAccount(ctx context.Context, in PickAccountInput) (accountID string, err error)
}

// PickAccountInput is what the picker needs to find or ask for an account.
type PickAccountInput struct {
	// Op is "push", "fetch" or "pull", for the question the person is shown.
	Op     string
	Remote string
	Host   string
	// Owner and Repo are the first and last path segments of the URL.
	Owner string
	Repo  string
	// Env is the remote's environment tag; accounts limited to other
	// environments are not candidates.
	Env string
}

// SetAccountPicker wires the account choice for unbound remotes.
func (s *Service) SetAccountPicker(p AccountPicker) { s.picker = p }

// withAccount fills in the account for a remote that has no binding.
//
// An explicit binding always wins, and it is never second-guessed. Only an
// HTTPS remote is asked about: an SSH remote is authenticated by a key, which
// no account selected here could supply, so asking would present a choice that
// changes nothing.
func (s *Service) withAccount(ctx context.Context, op string, remote Remote) (Remote, error) {
	if s.picker == nil || remote.AccountID != "" || remote.Transport != TransportHTTP {
		return remote, nil
	}
	address := ParseAddress(remote.URL)
	if address.Host == "" {
		return remote, nil
	}
	id, err := s.picker.PickAccount(ctx, PickAccountInput{
		Op: op, Remote: remote.Name, Host: address.Host, Owner: address.Owner, Repo: address.Repo, Env: remote.Env,
	})
	if err != nil {
		return remote, err
	}
	remote.AccountID = id
	return remote, nil
}

// Remotes lists the repository's remotes, merged with any stored bindings.
func (s *Service) Remotes(ctx context.Context) ([]Remote, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	out, err := r.run(ctx, nil, "remote", "-v")
	if err != nil {
		return nil, err
	}
	bindings := map[string]Remote{}
	if s.remotes != nil {
		stored, err := s.remotes.ListRemotes(s.Project())
		if err == nil {
			for _, binding := range stored {
				bindings[binding.Name] = binding
			}
		}
	}
	seen := map[string]bool{}
	var remotes []Remote
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		name, rest, ok := strings.Cut(line, "\t")
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		url, _, _ := strings.Cut(rest, " ")
		remote := Remote{Name: name, URL: url, Transport: transportOf(url)}
		if binding, ok := bindings[name]; ok {
			remote.AccountID, remote.Env = binding.AccountID, binding.Env
		}
		remotes = append(remotes, remote)
	}
	return remotes, nil
}

// DefaultBranch is the remote's HEAD, which is the branch an agent may never
// push to.
func (s *Service) DefaultBranch(ctx context.Context, remote string) string {
	r, err := s.active()
	if err != nil {
		return ""
	}
	out, err := r.run(ctx, nil, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD")
	if err != nil {
		return ""
	}
	_, branch, _ := strings.Cut(strings.TrimSpace(string(out)), "/")
	return branch
}

// PushInput is one push.
type PushInput struct {
	Remote string
	Branch string
	// SetUpstream is -u, for a branch that has no tracking yet.
	SetUpstream bool
	// ByAgent applies the agent rules: never the default branch, and only a
	// branch recorded on a task the agent holds.
	ByAgent bool
	// TaskBranches are the branches recorded on tasks the caller holds.
	TaskBranches []string
}

// Push sends a branch to a remote.
//
// Push is the gated action, always: it goes outside and cannot be undone.
// Commit is not, unless a guard says so. Force is not here at all — there is
// no flag, no parameter and no code path, which is what "agents cannot
// force-push" has to mean to be true.
func (s *Service) Push(ctx context.Context, in PushInput) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if in.Remote == "" {
		in.Remote = "origin"
	}
	if in.Branch == "" {
		status, err := s.Status(ctx)
		if err != nil {
			return err
		}
		in.Branch = status.Branch
	}
	if in.ByAgent {
		if err := protectedForAgent(in.Branch, s.DefaultBranch(ctx, in.Remote), in.TaskBranches); err != nil {
			return err
		}
	}
	if err := s.gateRemote(ctx, in.Remote, in.Branch); err != nil {
		return err
	}
	remote, err := s.remoteNamed(ctx, in.Remote, true)
	if err != nil {
		return err
	}
	if remote, err = s.withAccount(ctx, "push", remote); err != nil {
		return err
	}
	env, cleanup, err := s.credentialEnvFor(ctx, remote)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"push"}
	if in.SetUpstream {
		args = append(args, "-u")
	}
	args = append(args, in.Remote, in.Branch)
	return s.explainRemoteError("push", remote, r.runWithEnv(ctx, env, args...))
}

// Fetch and Pull are the read half. Pull is --ff-only: a merge started behind
// the user's back is how a conflict appears with nobody expecting one.
func (s *Service) Fetch(ctx context.Context, remote string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if remote == "" {
		remote = "origin"
	}
	bound, err := s.remoteNamed(ctx, remote, false)
	if err != nil {
		return err
	}
	if bound, err = s.withAccount(ctx, "fetch", bound); err != nil {
		return err
	}
	env, cleanup, err := s.credentialEnvFor(ctx, bound)
	if err != nil {
		return err
	}
	defer cleanup()
	return s.explainRemoteError("fetch", bound, r.runWithEnv(ctx, env, "fetch", remote))
}

func (s *Service) Pull(ctx context.Context, remote string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if remote == "" {
		remote = "origin"
	}
	bound, err := s.remoteNamed(ctx, remote, false)
	if err != nil {
		return err
	}
	if bound, err = s.withAccount(ctx, "pull", bound); err != nil {
		return err
	}
	env, cleanup, err := s.credentialEnvFor(ctx, bound)
	if err != nil {
		return err
	}
	defer cleanup()
	return s.explainRemoteError("pull", bound, r.runWithEnv(ctx, env, "pull", "--ff-only", remote))
}

func (s *Service) gateRemote(ctx context.Context, remote, branch string) error {
	env := ""
	remotes, err := s.Remotes(ctx)
	if err == nil {
		for _, candidate := range remotes {
			if candidate.Name == remote {
				env = candidate.Env
			}
		}
	}
	if env != "" && s.envs != nil && s.envs.RequiresConfirmation(env) {
		if s.confirm == nil {
			return ErrNotAllowed
		}
		allowed, err := s.confirm.Confirm(ctx, "Push to "+remote+"/"+branch+" ("+env+")", "This leaves the machine and cannot be undone.")
		if err != nil {
			return err
		}
		if !allowed {
			return ErrNotAllowed
		}
		return nil
	}
	// A branch guard covers push too, so a protected branch asks even when the
	// remote carries no environment.
	return s.gate(ctx, branch, "Push", "to "+remote)
}

// remoteNamed is the remote called name with its binding merged in, or just
// the name when git has no such remote (git then reports that itself).
//
// push asks for the address a push would use. A remote may have a separate
// pushurl, or a pushInsteadOf rewrite, and the transport that matters for a
// push is that one rather than the fetch address.
func (s *Service) remoteNamed(ctx context.Context, name string, push bool) (Remote, error) {
	remotes, err := s.Remotes(ctx)
	if err != nil {
		return Remote{}, err
	}
	remote := Remote{Name: name}
	for _, candidate := range remotes {
		if candidate.Name == name {
			remote = candidate
		}
	}
	if push && remote.URL != "" {
		if r, err := s.active(); err == nil {
			if out, err := r.run(ctx, nil, "remote", "get-url", "--push", name); err == nil {
				if pushURL := strings.TrimSpace(string(out)); pushURL != "" {
					remote.URL, remote.Transport = pushURL, transportOf(pushURL)
				}
			}
		}
	}
	return remote, nil
}

// credentialEnv resolves the named remote's account for a fetch.
func (s *Service) credentialEnv(ctx context.Context, name string) ([]string, func(), error) {
	if s.creds == nil || s.remotes == nil {
		return nil, func() {}, nil
	}
	remote, err := s.remoteNamed(ctx, name, false)
	if err != nil {
		return nil, func() {}, err
	}
	if remote, err = s.withAccount(ctx, "fetch", remote); err != nil {
		return nil, func() {}, err
	}
	return s.credentialEnvFor(ctx, remote)
}

// credentialEnvFor resolves the remote's account and hands it to git through
// GIT_ASKPASS — never through the URL or argv, where it would land in the
// process list and in anyone's shell history.
//
// Only an HTTPS remote is answered this way. git does not run the askpass
// helper for an SSH remote — the key answers there — so resolving a token for
// one would do nothing except spend a credential, tell the audit trail an
// account was used, and put a secret into the environment of a process that
// will not read it. An SSH remote is left to git's own configuration.
func (s *Service) credentialEnvFor(ctx context.Context, binding Remote) ([]string, func(), error) {
	noop := func() {}
	if s.creds == nil || s.remotes == nil {
		return nil, noop, nil
	}
	if binding.AccountID == "" || binding.Transport == TransportSSH {
		return nil, noop, nil
	}
	user, secret, err := s.creds.ResolveForRemote(ctx, binding.AccountID, binding.Env)
	if err != nil {
		return nil, noop, err
	}
	script, cleanup, err := askpassScript()
	if err != nil {
		return nil, noop, err
	}
	// The values travel as environment, read by the helper on stdout. Not in
	// the URL and not in argv: both are visible in the process list, and the
	// URL would also end up in .git/config and in anyone's shell history.
	return []string{
		"GIT_ASKPASS=" + script,
		"GIT_TERMINAL_PROMPT=0",
		"DEVKIT_GIT_USER=" + user,
		"DEVKIT_GIT_SECRET=" + secret,
	}, cleanup, nil
}

// askpassScript writes the helper git calls when it wants a username or a
// password. The script carries no secret itself — it echoes two environment
// variables — so the file is a fixed three lines whatever the credential is.
//
// It lives in a 0700 directory and is removed when the command returns.
func askpassScript() (string, func(), error) {
	dir, err := os.MkdirTemp("", "devkit-askpass-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	path := filepath.Join(dir, "askpass.sh")
	// git passes the prompt as $1; it contains "Username" or "Password".
	body := "#!/bin/sh\ncase \"$1\" in\n*sername*) printf '%s' \"$DEVKIT_GIT_USER\" ;;\n*) printf '%s' \"$DEVKIT_GIT_SECRET\" ;;\nesac\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}
