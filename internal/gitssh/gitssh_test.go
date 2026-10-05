package gitssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// newKey returns a private key in OpenSSH format, optionally encrypted.
func newKey(t *testing.T, passphrase string) Material {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(private, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return Material{PrivateKey: string(pem.EncodeToMemory(block)), Passphrase: passphrase}
}

func TestPackRoundTripsAndRefusesAnythingElse(t *testing.T) {
	m := Material{PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n", Passphrase: "pw"}
	got, err := Unpack(Pack(m))
	if err != nil || got != m {
		t.Fatalf("Unpack(Pack(m)) = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "not json", `{"v":2,"key":"x"}`, `{"v":1}`} {
		if _, err := Unpack(bad); !errors.Is(err, ErrNotAKey) {
			t.Errorf("Unpack(%q) = %v, want ErrNotAKey", bad, err)
		}
	}
}

func TestInspectDescribesAKeyWithoutRevealingIt(t *testing.T) {
	m := newKey(t, "")
	info, err := Inspect(m)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != "ssh-ed25519" || !strings.HasPrefix(info.Fingerprint, "SHA256:") ||
		!strings.HasPrefix(info.PublicKey, "ssh-ed25519 ") || info.Encrypted {
		t.Fatalf("info = %+v", info)
	}
	if strings.Contains(info.PublicKey, "PRIVATE") {
		t.Fatal("the public key line carries private material")
	}
}

func TestInspectChecksThePassphraseWhenTheKeySavesIt(t *testing.T) {
	m := newKey(t, "correct horse")
	if _, err := Inspect(Material{PrivateKey: m.PrivateKey}); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("no passphrase = %v", err)
	}
	if _, err := Inspect(Material{PrivateKey: m.PrivateKey, Passphrase: "wrong"}); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("wrong passphrase = %v", err)
	}
	info, err := Inspect(m)
	if err != nil || !info.Encrypted {
		t.Fatalf("right passphrase = %+v, %v", info, err)
	}
	if _, err := Inspect(Material{PrivateKey: "hello"}); !errors.Is(err, ErrNotAKey) {
		t.Fatalf("garbage = %v", err)
	}
}

func skipWithoutUnixSockets(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no unix-socket agent on windows")
	}
}

// The session's agent holds exactly the one key, which is what makes "use this
// account" mean this account and not whichever key was loaded first.
func TestSessionServesExactlyOneKeyAndCleansUp(t *testing.T) {
	skipWithoutUnixSockets(t)
	m := newKey(t, "pw")
	info, _ := Inspect(m)
	session, err := Open(m)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("unix", session.socket)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := agent.NewClient(conn).List()
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || ssh.FingerprintSHA256(keys[0]) != info.Fingerprint {
		t.Fatalf("the agent holds %d keys, want only %s", len(keys), info.Fingerprint)
	}

	if stat, err := os.Stat(session.dir); err != nil || stat.Mode().Perm() != 0o700 {
		t.Fatalf("session dir = %v, %v; want 0700", stat, err)
	}
	entries, _ := os.ReadDir(session.dir)
	for _, entry := range entries {
		raw, _ := os.ReadFile(filepath.Join(session.dir, entry.Name()))
		if strings.Contains(string(raw), "PRIVATE KEY") {
			t.Fatalf("%s holds the private key on disk", entry.Name())
		}
	}

	dir := session.dir
	session.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the session outlived Close: %v", err)
	}
	session.Close() // closing twice must not panic on a nil listener elsewhere
}

func TestCommandQuotesPathsGitRunsThroughAShell(t *testing.T) {
	s := &Session{socket: "/tmp/it's here/a", identity: "/tmp/it's here/k.pub"}
	command := s.Command()
	if !strings.Contains(command, `'IdentityAgent=/tmp/it'\''s here/a'`) {
		t.Fatalf("command = %s", command)
	}
	if out, err := exec.Command("sh", "-c", "printf %s "+command).Output(); err != nil || !strings.Contains(string(out), "IdentityAgent=/tmp/it's here/a") {
		t.Fatalf("a shell did not read the command back: %q, %v", out, err)
	}
}

// ---- a real ssh against a local server -------------------------------------

// recorder is an SSH server that accepts one key and greets it the way GitHub
// does, remembering every key it was offered — including the ones it refused.
type recorder struct {
	addr     string
	mu       sync.Mutex
	offered  []string
	accepted string
}

func startRecorder(t *testing.T, accepted string) *recorder {
	t.Helper()
	_, hostKey, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{accepted: accepted}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := ssh.FingerprintSHA256(key)
			r.mu.Lock()
			r.offered = append(r.offered, fingerprint)
			r.mu.Unlock()
			if fingerprint == r.accepted {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	r.addr = listener.Addr().String()
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
							if req.Type == "shell" || req.Type == "exec" {
								_, _ = channel.Stderr().Write([]byte("Hi acme-dev! You've successfully authenticated, but GitHub does not provide shell access.\n"))
								_, _ = channel.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
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

func (r *recorder) offeredKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.offered...)
}

// testSSHOptions point ssh at the local server and at a config file the test
// controls (ssh finds ~/.ssh/config from the passwd entry, not $HOME, so the
// machine's own is not something a test can stand in for). They add to
// Session.Options; they do not replace the identity ones, which are under test.
func testSSHOptions(addr, config string) []string {
	_, port, _ := net.SplitHostPort(addr)
	return []string{"-p", port, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null", "-F", config}
}

// configNaming writes an ssh_config that names a key file for every host, as a
// ~/.ssh/config with an IdentityFile does.
func configNaming(t *testing.T, material Material) (config string, info KeyInfo) {
	t.Helper()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "id_other")
	if err := os.WriteFile(keyFile, []byte(strings.TrimSpace(material.PrivateKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config = filepath.Join(dir, "config")
	if err := os.WriteFile(config, []byte("Host *\n  IdentityFile "+keyFile+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ = Inspect(material)
	return config, info
}

func needSSH(t *testing.T) {
	t.Helper()
	skipWithoutUnixSockets(t)
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh program")
	}
}

// The key from the account is the one offered, and a key the person has in
// ~/.ssh/config is not tried first or at all when the account's key is good.
func TestSSHOffersOnlyTheAccountKeyWhenTheProviderKnowsIt(t *testing.T) {
	needSSH(t)
	other := newKey(t, "")
	config, otherInfo := configNaming(t, other)
	account := newKey(t, "pw")
	accountInfo, _ := Inspect(account)
	server := startRecorder(t, accountInfo.Fingerprint)

	session, err := Open(account)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	login, err := identify(context.Background(), session, "127.0.0.1", testSSHOptions(server.addr, config))
	if err != nil {
		t.Fatalf("identify: %v (offered %v)", err, server.offeredKeys())
	}
	if login != "acme-dev" {
		t.Fatalf("login = %q", login)
	}
	offered := server.offeredKeys()
	if len(offered) != 1 || offered[0] != accountInfo.Fingerprint {
		t.Fatalf("ssh offered %v, want only the account key %s (not %s)", offered, accountInfo.Fingerprint, otherInfo.Fingerprint)
	}
}

// What this does NOT promise, pinned so nobody reads more into it: when the
// provider refuses the account's key, ssh goes on to the IdentityFile that
// ~/.ssh/config names, because a command-line option cannot remove a config
// entry. The account's key is always first.
func TestSSHFallsBackToConfigIdentityOnlyAfterTheAccountKeyIsRefused(t *testing.T) {
	needSSH(t)
	other := newKey(t, "")
	config, otherInfo := configNaming(t, other)
	account := newKey(t, "")
	accountInfo, _ := Inspect(account)
	server := startRecorder(t, otherInfo.Fingerprint) // knows the config key, not the account's

	session, err := Open(account)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := identify(context.Background(), session, "127.0.0.1", testSSHOptions(server.addr, config)); err != nil {
		t.Fatalf("identify: %v", err)
	}
	offered := server.offeredKeys()
	if len(offered) != 2 || offered[0] != accountInfo.Fingerprint || offered[1] != otherInfo.Fingerprint {
		t.Fatalf("offered %v, want the account key first and then the config key", offered)
	}
}

func TestIdentifyReportsAKeyTheProviderDoesNotKnow(t *testing.T) {
	needSSH(t)
	config := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	server := startRecorder(t, "SHA256:somebody-else")
	session, err := Open(newKey(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := identify(ctx, session, "127.0.0.1", testSSHOptions(server.addr, config)); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("a refused key = %v", err)
	}
}

func TestResolveHostReadsTheAliasFromSSHConfig(t *testing.T) {
	needSSH(t)
	config := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(config, []byte("Host github-work\n  HostName GitHub.com\n  IdentityFile ~/.ssh/id_work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveHost(context.Background(), "github-work", []string{"-F", config}); got != "github.com" {
		t.Fatalf("alias resolved to %q", got)
	}
	if got := resolveHost(context.Background(), "gitlab.com", []string{"-F", config}); got != "gitlab.com" {
		t.Fatalf("a real host resolved to %q", got)
	}
	if got := ResolveHost(context.Background(), ""); got != "" {
		t.Fatalf("empty name resolved to %q", got)
	}
}
