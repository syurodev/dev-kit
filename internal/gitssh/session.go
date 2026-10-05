package gitssh

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh/agent"
)

var (
	// ErrUnsupported is a platform this cannot serve an agent on. Windows'
	// OpenSSH reaches its agent through a named pipe, not a unix socket.
	ErrUnsupported = errors.New("gitssh.unsupported")
	// ErrNoSSH is a machine with no ssh program.
	ErrNoSSH = errors.New("gitssh.noSSH")
	// ErrNotAuthenticated is a provider refusing the key: it is not registered
	// with any account there. Anything else that goes wrong — no network, an
	// unknown host — is a different error, because it says nothing about the key.
	ErrNotAuthenticated = errors.New("gitssh.notAuthenticated")
)

// Session serves one key to ssh for as long as it is open.
type Session struct {
	dir      string
	socket   string
	identity string
	listener net.Listener
	done     chan struct{}
	extra    []string
}

// Open starts a private agent holding the key.
//
// The socket lives in a directory only this user can enter, holds exactly this
// key, and is gone when Close returns. The only file written is the PUBLIC
// key, which ssh needs in order to know which agent key to offer.
func Open(m Material) (*Session, error) { return OpenWith(m) }

// OpenWith is Open plus ssh options the caller must add after the identity
// ones. Production passes none; a test uses it to point ssh at a local server.
func OpenWith(m Material, extra ...string) (*Session, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrUnsupported
	}
	raw, _, err := parse(m)
	if err != nil {
		return nil, err
	}
	info, err := Inspect(m)
	if err != nil {
		return nil, err
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: raw}); err != nil {
		return nil, ErrNotAKey
	}

	dir, err := os.MkdirTemp("", "dk-ssh-")
	if err != nil {
		return nil, err
	}
	s := &Session{extra: extra, dir: dir, socket: filepath.Join(dir, "a"), identity: filepath.Join(dir, "k.pub"), done: make(chan struct{})}
	fail := func(err error) (*Session, error) {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.WriteFile(s.identity, []byte(info.PublicKey+"\n"), 0o600); err != nil {
		return fail(err)
	}
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		return fail(err)
	}
	if err := os.Chmod(s.socket, 0o600); err != nil {
		listener.Close()
		return fail(err)
	}
	s.listener = listener
	go func() {
		defer close(s.done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()
	return s, nil
}

// Close stops the agent and removes the socket.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.listener.Close()
	<-s.done
	os.RemoveAll(s.dir)
}

// Options are the ssh options that make it use this key and no other agent.
//
// IdentitiesOnly makes ssh offer only keys named by an IdentityFile; naming the
// public key file means "the agent key that matches this one". BatchMode keeps
// ssh from asking anything in a process nobody is watching.
//
// ssh_config can still name IdentityFile entries of its own for a host, and
// ssh will try those if this key is refused. An account is verified against
// its provider when it is saved, so a refusal means the key was removed from
// the account — and then the push fails, or authenticates as whoever ~/.ssh/config
// names for that host. That is the identity the person had before DevKit
// chose one, but it is not the one they picked, and it is why this is not
// described as exclusive.
func (s *Session) Options() []string {
	return append([]string{
		"-o", "IdentityAgent=" + s.socket,
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityFile=" + s.identity,
		"-o", "BatchMode=yes",
	}, s.extra...)
}

// Command is Options as the value of GIT_SSH_COMMAND, which git runs through a
// shell and so needs quoted.
func (s *Session) Command() string {
	parts := []string{"ssh"}
	for _, option := range s.Options() {
		parts = append(parts, shellQuote(option))
	}
	return strings.Join(parts, " ")
}

// Env is what to add to git's environment to authenticate with this key.
func (s *Session) Env() []string { return []string{"GIT_SSH_COMMAND=" + s.Command()} }

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// identities are what each provider says to a key that authenticates on its
// SSH port. The login is the only part worth reading.
var identities = []*regexp.Regexp{
	regexp.MustCompile(`Hi ([A-Za-z0-9][A-Za-z0-9-]*)!`),                   // GitHub
	regexp.MustCompile(`Welcome to GitLab, @([A-Za-z0-9._-]+)!`),           // GitLab
	regexp.MustCompile(`logged in as ([A-Za-z0-9._-]+)`),                   // Bitbucket
	regexp.MustCompile(`authenticated via ssh key.*?as ([A-Za-z0-9._-]+)`), // Bitbucket Server style
}

// Identify asks the provider at host who this key belongs to, by connecting to
// it the way a push would and reading the greeting. It is how an account learns
// its login, and the proof that the key is registered with the provider at all.
func Identify(ctx context.Context, m Material, host string) (string, error) {
	session, err := Open(m)
	if err != nil {
		return "", err
	}
	defer session.Close()
	return identify(ctx, session, host, nil)
}

// identify is Identify on a session that is already open. extra is for ssh
// options a caller has to add — a test pointing it at a local server.
func identify(ctx context.Context, s *Session, host string, extra []string) (string, error) {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return "", ErrNoSSH
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := append([]string{"-T"}, s.Options()...)
	args = append(args, extra...)
	args = append(args, "git@"+host)
	cmd := exec.CommandContext(ctx, path, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// The providers exit non-zero after a successful greeting (GitHub says it
	// does not provide shell access), so the exit status says nothing.
	_ = cmd.Run()
	for _, pattern := range identities {
		if match := pattern.FindStringSubmatch(out.String()); match != nil {
			return match[1], nil
		}
	}
	if strings.Contains(strings.ToLower(out.String()), "permission denied") {
		return "", ErrNotAuthenticated
	}
	return "", errors.New("gitssh.identify: " + firstLine(out.String()))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// ResolveHost returns the host ssh would really connect to for a name that may
// be an alias in ~/.ssh/config (git@github-work:acme/api.git). It asks ssh
// itself, so Include, Match and wildcard blocks all count, and it does not
// connect. Any failure returns the name unchanged.
func ResolveHost(ctx context.Context, name string) string { return resolveHost(ctx, name, nil) }

// resolveHost is ResolveHost with extra ssh options, for a test to point it at
// a config file: ssh finds ~/.ssh/config from the passwd entry, not $HOME.
func resolveHost(ctx context.Context, name string, extra []string) string {
	path, err := exec.LookPath("ssh")
	if err != nil || name == "" {
		return name
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, append(append([]string{"-G"}, extra...), name)...).Output()
	if err != nil {
		return name
	}
	for _, line := range strings.Split(string(out), "\n") {
		if key, value, ok := strings.Cut(line, " "); ok && key == "hostname" && strings.TrimSpace(value) != "" {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return name
}
