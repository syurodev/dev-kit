package accounts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"devkit/internal/gitssh"
	"devkit/internal/storage"
)

func newPrivateKey(t *testing.T, passphrase string) string {
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
	return string(pem.EncodeToMemory(block))
}

func keyService(t *testing.T) *Service {
	t.Helper()
	svc := remoteService(t)
	svc.SetHostChoices(svc.remoteChoices.db, storage.TableAccountHostChoices)
	return svc
}

func addKey(t *testing.T, svc *Service, provider, label string, local bool) Account {
	t.Helper()
	account, err := svc.Create(CreateInput{Provider: provider, Kind: KindSSHKey, Label: label, Secret: newPrivateKey(t, ""), Local: local})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func TestASSHKeyAccountIsLocalWhenAskedAndResolvesByID(t *testing.T) {
	svc := keyService(t)
	passphrase := "correct horse"
	key := newPrivateKey(t, passphrase)
	account, err := svc.Create(CreateInput{
		Provider: "github", Kind: KindSSHKey, Label: "Work key", Secret: key, Passphrase: passphrase, Local: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !account.Local() || account.Kind != KindSSHKey {
		t.Fatalf("account = %+v", account)
	}
	material, err := svc.ResolveSSHKey(account.ID, "", "git")
	if err != nil {
		t.Fatal(err)
	}
	if material.PrivateKey != key || material.Passphrase != passphrase {
		t.Fatal("the key or its passphrase did not survive the store")
	}
	info, err := svc.SSHKeyInfo(account.ID)
	if err != nil || !strings.HasPrefix(info.Fingerprint, "SHA256:") || !info.Encrypted {
		t.Fatalf("info = %+v, %v", info, err)
	}
}

func TestASSHKeyAccountSyncsOnlyWhenItIsNotLocal(t *testing.T) {
	svc := keyService(t)
	local := addKey(t, svc, "github", "Local", true)
	shared := addKey(t, svc, "github", "Shared", false)
	if !local.Local() || shared.Scoped() {
		t.Fatalf("local=%+v shared=%+v", local, shared)
	}
	// The shared one is a vault item (it syncs); the local one is not.
	if _, err := svc.items.Get(shared.ID); err != nil {
		t.Fatalf("a shared key is not a vault item: %v", err)
	}
	if _, err := svc.items.Get(local.ID); err == nil {
		t.Fatal("a local key was written to vault_items, which replicates")
	}
	if _, err := svc.ResolveSSHKey(shared.ID, "", "git"); err != nil {
		t.Fatalf("shared key does not resolve: %v", err)
	}
}

func TestASSHKeyIsCheckedWhenItIsSaved(t *testing.T) {
	svc := keyService(t)
	encrypted := newPrivateKey(t, "pw")
	cases := map[string]struct {
		in   CreateInput
		want error
	}{
		"not a key":        {CreateInput{Provider: "github", Kind: KindSSHKey, Label: "x", Secret: "hello"}, ErrKeyInvalid},
		"no passphrase":    {CreateInput{Provider: "github", Kind: KindSSHKey, Label: "x", Secret: encrypted}, ErrKeyPassphrase},
		"wrong passphrase": {CreateInput{Provider: "github", Kind: KindSSHKey, Label: "x", Secret: encrypted, Passphrase: "no"}, ErrKeyPassphrase},
		"provider no keys": {CreateInput{Provider: "anthropic", Kind: KindSSHKey, Label: "x", Secret: newPrivateKey(t, "")}, ErrInvalid},
		"env restriction":  {CreateInput{Provider: "github", Kind: KindSSHKey, Label: "x", Secret: newPrivateKey(t, ""), Env: []string{"dev"}}, ErrInvalid},
	}
	for name, c := range cases {
		if _, err := svc.Create(c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	// And none of them left a half-saved account behind.
	all, _ := svc.List()
	if len(all) != 0 {
		t.Fatalf("a refused key left %d accounts", len(all))
	}
}

// Over SSH only keys are candidates; over HTTPS only tokens are. This is what
// keeps the popup for a push from offering an account that cannot be used.
func TestForRemoteOffersKeysOverSSHAndTokensOverHTTPS(t *testing.T) {
	svc := keyService(t)
	token, _ := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Token", Secret: "ghp_1"})
	key := addKey(t, svc, "github", "Key", true)

	https, err := svc.ForRemote("github.com", "acme-corp", "", ViaHTTPS)
	if err != nil || !https.Decided || https.Account.ID != token.ID {
		t.Fatalf("https = %+v, %v", https, err)
	}
	ssh, err := svc.ForRemote("github.com", "acme-corp", "", ViaSSH)
	if err != nil || !ssh.Decided || ssh.Account.ID != key.ID {
		t.Fatalf("ssh = %+v, %v", ssh, err)
	}
	// The API client's lookup never offers a key as a bearer credential.
	api, err := svc.ForHost("api.github.com", "")
	if err != nil || !api.Decided || api.Account.ID != token.ID {
		t.Fatalf("ForHost = %+v, %v", api, err)
	}
}

// The user's setup over SSH: two GitHub keys and a GitLab key.
func TestForRemoteAsksBetweenTwoGitHubKeysAndRemembersPerTransport(t *testing.T) {
	svc := keyService(t)
	work := addKey(t, svc, "github", "Work key", true)
	personal := addKey(t, svc, "github", "Personal key", true)
	gitlab := addKey(t, svc, "gitlab", "GitLab key", true)
	token, _ := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Token", Secret: "ghp_1"})

	if m, _ := svc.ForRemote("gitlab.com", "team", "", ViaSSH); !m.Decided || m.Account.ID != gitlab.ID {
		t.Fatalf("gitlab = %+v", m)
	}
	if m, _ := svc.ForRemote("github.com", "acme-corp", "", ViaSSH); m.Decided || len(m.Options) != 2 {
		t.Fatalf("two GitHub keys must be a question: %+v", m)
	}
	if err := svc.ChooseForRemote("github.com", "acme-corp", "", ViaSSH, work.ID); err != nil {
		t.Fatal(err)
	}
	if m, _ := svc.ForRemote("github.com", "acme-corp", "", ViaSSH); !m.Decided || m.Account.ID != work.ID || !m.Remembered {
		t.Fatalf("ssh answer not remembered: %+v", m)
	}
	// The answer for SSH does not touch HTTPS, and a token cannot be pinned to SSH.
	if m, _ := svc.ForRemote("github.com", "acme-corp", "", ViaHTTPS); !m.Decided || m.Account.ID != token.ID || m.Remembered {
		t.Fatalf("https was affected by the ssh answer: %+v", m)
	}
	if err := svc.ChooseForRemote("github.com", "acme-corp", "", ViaSSH, token.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a token was accepted for SSH: %v", err)
	}
	_ = personal
}

func TestASSHKeyIsNeverRevealedOrRotated(t *testing.T) {
	svc := keyService(t)
	for _, local := range []bool{true, false} {
		account := addKey(t, svc, "github", "Key", local)
		if _, err := svc.Reveal(account.ID); !errors.Is(err, ErrNothingToReveal) {
			t.Errorf("local=%v: reveal = %v", local, err)
		}
		if err := svc.Rotate(account.ID, "", "x"); !errors.Is(err, ErrInvalid) {
			t.Errorf("local=%v: rotate = %v", local, err)
		}
	}
}

// A local account is its own owner. The sweep that removes credentials of
// deleted profiles must not take it for one.
func TestSweepOrphansKeepsLocalAccounts(t *testing.T) {
	svc := keyService(t)
	account := addKey(t, svc, "github", "Key", true)
	removed, err := svc.SweepOrphans(nil)
	if err != nil || removed != 0 {
		t.Fatalf("sweep removed %d, %v", removed, err)
	}
	if _, err := svc.ResolveSSHKey(account.ID, "", "git"); err != nil {
		t.Fatalf("the sweep deleted a local key: %v", err)
	}
}

// A connection's scoped password is reachable only through its profile; a git
// binding naming its id must not get it.
func TestResolveByIDDoesNotReachAConnectionsPassword(t *testing.T) {
	svc := keyService(t)
	scoped, err := svc.Create(CreateInput{Provider: ProviderDatabase, Kind: KindSecret, Label: "db", Secret: "hunter2", ScopeOwner: "profile-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ResolveInput{AccountID: scoped.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a scoped password resolved by id: %v", err)
	}
}

func TestVerifyRecordsWhoTheProviderSaysTheKeyIs(t *testing.T) {
	svc := keyService(t)
	local := addKey(t, svc, "github", "Local", true)
	shared := addKey(t, svc, "gitlab", "Shared", false)
	var hosts []string
	svc.SetSSHIdentifier(func(_ context.Context, m gitssh.Material, host string) (string, error) {
		hosts = append(hosts, host)
		if m.PrivateKey == "" {
			t.Error("verify was given no key")
		}
		return "acme-dev", nil
	})

	for _, account := range []Account{local, shared} {
		verified, err := svc.Verify(context.Background(), account.ID)
		if err != nil {
			t.Fatal(err)
		}
		if verified.Subject != "acme-dev" || verified.State != StateValid || verified.VerifiedAt.IsZero() {
			t.Fatalf("verified = %+v", verified)
		}
		again, _ := svc.Get(account.ID)
		if again.Subject != "acme-dev" || again.State != StateValid {
			t.Fatalf("the verification was not kept: %+v", again)
		}
	}
	if len(hosts) != 2 || hosts[0] != "github.com" || hosts[1] != "gitlab.com" {
		t.Fatalf("keys were checked against %v", hosts)
	}
}

func TestVerifyMarksARefusedKeyRevokedButNotANetworkFailure(t *testing.T) {
	svc := keyService(t)
	account := addKey(t, svc, "github", "Key", true)

	svc.SetSSHIdentifier(func(context.Context, gitssh.Material, string) (string, error) {
		return "", gitssh.ErrNotAuthenticated
	})
	verified, err := svc.Verify(context.Background(), account.ID)
	if err != nil || verified.State != StateRevoked {
		t.Fatalf("a refused key = %+v, %v", verified, err)
	}

	svc.SetSSHIdentifier(func(context.Context, gitssh.Material, string) (string, error) {
		return "", errors.New("could not resolve host")
	})
	if _, err := svc.Verify(context.Background(), account.ID); err == nil {
		t.Fatal("a network failure was reported as a verdict")
	}
	after, _ := svc.Get(account.ID)
	if after.State != StateRevoked {
		t.Fatalf("a network failure overwrote the last verdict: %+v", after)
	}
}
