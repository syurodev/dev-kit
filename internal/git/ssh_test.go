package git

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"devkit/internal/gitssh"
)

func newSSHKey(t *testing.T) gitssh.Material {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	return gitssh.Material{PrivateKey: string(pem.EncodeToMemory(block))}
}

// keyCreds is a credential resolver that can also hand over SSH keys.
type keyCreds struct {
	labelledCreds
	keys map[string]gitssh.Material
	err  error
}

func (c *keyCreds) ResolveSSHKey(_ context.Context, id, _ string) (gitssh.Material, bool, error) {
	if c.err != nil {
		return gitssh.Material{}, false, c.err
	}
	key, ok := c.keys[id]
	return key, ok, nil
}

func sshRepo(t *testing.T, url string) (*Service, string) {
	t.Helper()
	svc, work, _ := newRepoWithRemote(t)
	runGit(t, svc, work, "remote", "set-url", "origin", url)
	return svc, work
}

func envValue(env []string, name string) string {
	for _, entry := range env {
		if key, value, _ := strings.Cut(entry, "="); key == name {
			return value
		}
	}
	return ""
}

// An SSH remote bound to a key account gets that key from a private agent, and
// no token: the key is the credential, and git would not read a token anyway.
func TestASSHRemoteWithAKeyAccountIsServedThatKeyAndNoToken(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh program")
	}
	svc, _ := sshRepo(t, "git@code.example.test:acme/api.git")
	creds := &keyCreds{keys: map[string]gitssh.Material{"acct-key": newSSHKey(t)}}
	svc.SetRemotes(stubRemotes{{Name: "origin", AccountID: "acct-key"}})
	svc.SetCredentials(creds)

	env, cleanup, err := svc.credentialEnv(context.Background(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	command := envValue(env, "GIT_SSH_COMMAND")
	if !strings.Contains(command, "IdentityAgent=") || !strings.Contains(command, "IdentitiesOnly=yes") {
		t.Fatalf("GIT_SSH_COMMAND = %q", command)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "GIT_ASKPASS=") || strings.HasPrefix(entry, "DEVKIT_GIT_SECRET=") {
			t.Fatalf("an SSH remote was given a password helper: %s", entry)
		}
	}
	if strings.Contains(command, "PRIVATE KEY") {
		t.Fatal("the private key is in the command line")
	}

	// The agent and its directory are gone when the command is.
	socket := strings.TrimPrefix(strings.Fields(strings.NewReplacer("'", "").Replace(command))[2], "IdentityAgent=")
	if conn, err := net.Dial("unix", socket); err != nil {
		t.Fatalf("the agent was not listening during the command: %v", err)
	} else {
		conn.Close()
	}
	cleanup()
	if _, err := net.Dial("unix", socket); err == nil {
		t.Fatal("the agent outlived the command")
	}
}

func TestATokenBoundToAnSSHRemoteIsStillIgnored(t *testing.T) {
	svc, _ := sshRepo(t, "git@code.example.test:acme/api.git")
	svc.SetRemotes(stubRemotes{{Name: "origin", AccountID: "acct-token"}})
	svc.SetCredentials(&keyCreds{}) // acct-token is not a key

	env, cleanup, err := svc.credentialEnv(context.Background(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(env) != 0 {
		t.Fatalf("a token produced an SSH environment: %v", env)
	}
}

// A key that is meant to be used and cannot be must stop the command. Falling
// back to the machine's own key would push as an identity nobody chose.
func TestAKeyThatCannotBeOpenedStopsTheCommand(t *testing.T) {
	svc, _ := sshRepo(t, "git@code.example.test:acme/api.git")
	broken := errors.New("passphrase no longer opens the key")
	svc.SetRemotes(stubRemotes{{Name: "origin", AccountID: "acct-key"}})
	svc.SetCredentials(&keyCreds{err: broken})

	if err := svc.Fetch(context.Background(), "origin"); !errors.Is(err, broken) {
		t.Fatalf("fetch = %v, want the key error", err)
	}
	svc.SetCredentials(&keyCreds{keys: map[string]gitssh.Material{"acct-key": {PrivateKey: "not a key"}}})
	if _, cleanup, err := svc.credentialEnv(context.Background(), "origin"); !errors.Is(err, gitssh.ErrNotAKey) {
		cleanup()
		t.Fatalf("an unreadable key = %v", err)
	}
}

// The user's setup: git@github-work:acme/api.git is a GitHub remote whose host
// is only known to ~/.ssh/config. The picker has to be asked about github.com.
func TestAnSSHAliasIsAskedAboutAsTheHostItReallyIs(t *testing.T) {
	svc, _ := sshRepo(t, "git@github-work:Acme-Corp/api.git")
	picker := &stubPicker{id: "acct-key"}
	svc.SetRemotes(stubRemotes{{Name: "origin"}})
	svc.SetCredentials(&keyCreds{keys: map[string]gitssh.Material{"acct-key": newSSHKey(t)}})
	svc.SetAccountPicker(picker)
	svc.SetHostResolver(func(_ context.Context, host string) string {
		if host == "github-work" {
			return "github.com"
		}
		return host
	})

	env, cleanup, err := svc.credentialEnv(context.Background(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if picker.called != 1 {
		t.Fatalf("picker asked %d times", picker.called)
	}
	got := picker.calls[0]
	if got.Transport != TransportSSH || got.Host != "github.com" || got.Alias != "github-work" || got.Owner != "acme-corp" || got.Repo != "api" {
		t.Fatalf("picker was asked %+v", got)
	}
	if envValue(env, "GIT_SSH_COMMAND") == "" {
		t.Fatalf("the picked key was not served: %v", env)
	}
}

// ---- git -> ssh -> agent -> server -------------------------------------------

type recordingServer struct {
	port    string
	mu      sync.Mutex
	offered []string
}

// startRepositoryServer accepts any key, remembers which it was offered, and
// answers a git command the way GitHub answers one for a repository the key
// cannot see.
func startRepositoryServer(t *testing.T) *recordingServer {
	t.Helper()
	_, hostKey, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	r := &recordingServer{}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			r.mu.Lock()
			r.offered = append(r.offered, ssh.FingerprintSHA256(key))
			r.mu.Unlock()
			return &ssh.Permissions{}, nil
		},
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	_, r.port, _ = net.SplitHostPort(listener.Addr().String())
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for newChannel := range channels {
					channel, reqs, err := newChannel.Accept()
					if err != nil {
						return
					}
					go func() {
						for req := range reqs {
							_ = req.Reply(true, nil)
							if req.Type == "exec" {
								_, _ = channel.Stderr().Write([]byte("ERROR: Repository not found.\n"))
								_, _ = channel.SendRequest("exit-status", false, []byte{0, 0, 0, 128})
								channel.Close()
								return
							}
						}
					}()
				}
			}()
		}
	}()
	return r
}

// The whole chain, with a real git and a real ssh: the key of the account the
// remote is bound to is the key the server is offered — and the refusal that
// comes back names that account instead of git's bare "Repository not found".
func TestPushOverSSHOffersTheBoundAccountsKeyAndNamesItWhenRefused(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh program")
	}
	server := startRepositoryServer(t)
	svc, _ := sshRepo(t, "ssh://git@127.0.0.1:"+server.port+"/acme/api.git")
	account := newSSHKey(t)
	info, err := gitssh.Inspect(account)
	if err != nil {
		t.Fatal(err)
	}
	svc.sshOptions = []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null", "-F", "/dev/null"}
	svc.SetRemotes(stubRemotes{{Name: "origin", AccountID: "acct-key"}})
	svc.SetCredentials(&keyCreds{
		labelledCreds: labelledCreds{labels: map[string]string{"acct-key": "github · acme-dev"}},
		keys:          map[string]gitssh.Material{"acct-key": account},
	})

	err = svc.Push(context.Background(), PushInput{Remote: "origin", Branch: "main"})

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("push = %v, want an AuthError", err)
	}
	if authErr.Transport != TransportSSH || authErr.Account != "github · acme-dev" {
		t.Fatalf("AuthError = %+v", authErr)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.offered) == 0 || server.offered[0] != info.Fingerprint {
		t.Fatalf("the server was offered %v, want the account key %s first", server.offered, info.Fingerprint)
	}
}

// A real host is not an alias: nothing was written that the person chose.
func TestARealHostIsNotAnAlias(t *testing.T) {
	svc, _ := sshRepo(t, "git@code.example.test:acme/api.git")
	picker := &stubPicker{}
	svc.SetRemotes(stubRemotes{{Name: "origin"}})
	svc.SetCredentials(&keyCreds{})
	svc.SetAccountPicker(picker)
	svc.SetHostResolver(func(_ context.Context, host string) string { return host })

	if _, cleanup, err := svc.credentialEnv(context.Background(), "origin"); err != nil {
		t.Fatal(err)
	} else {
		cleanup()
	}
	if picker.called != 1 || picker.calls[0].Alias != "" {
		t.Fatalf("picker calls = %+v", picker.calls)
	}
}
