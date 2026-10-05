package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/gitssh"
)

// fakeHome is a home directory with a ~/.ssh in it. ~ in ssh config means the
// real home, which for these tests is this one.
func fakeHome(t *testing.T) (ssh string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh = filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	return ssh
}

func putKey(t *testing.T, dir, name, passphrase string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(newPrivateKey(t, passphrase)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverSSHKeysShowsWhatIsAlreadyImported(t *testing.T) {
	ssh := fakeHome(t)
	work := putKey(t, ssh, "id_work", "")
	putKey(t, ssh, "id_personal", "pw") // encrypted, no .pub
	if err := os.WriteFile(filepath.Join(ssh, "config"),
		[]byte("Host github-work\n  HostName github.com\n  IdentityFile ~/.ssh/id_work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := keyService(t)

	files, err := svc.DiscoverSSHKeys()
	if err != nil || len(files) != 2 {
		t.Fatalf("files = %+v, %v", files, err)
	}
	for _, f := range files {
		if len(f.Imported) != 0 || f.Fingerprint == "" {
			t.Fatalf("before any import: %+v", f)
		}
	}

	account, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: work, Provider: "github", Label: "Work", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	files, _ = svc.DiscoverSSHKeys()
	for _, f := range files {
		switch f.Name {
		case "id_work":
			if len(f.Imported) != 1 || f.Imported[0].AccountID != account.ID || f.Imported[0].Label != "Work" {
				t.Fatalf("id_work imported = %+v", f.Imported)
			}
			if len(f.Aliases) != 1 || f.Aliases[0].Name != "github-work" || f.Aliases[0].HostName != "github.com" {
				t.Fatalf("id_work aliases = %+v", f.Aliases)
			}
		default:
			if len(f.Imported) != 0 {
				t.Fatalf("%s reported as imported: %+v", f.Name, f.Imported)
			}
		}
	}
}

func TestImportReadsTheKeyAndLeavesTheFileAlone(t *testing.T) {
	ssh := fakeHome(t)
	path := putKey(t, ssh, "id_work", "")
	before, _ := os.ReadFile(path)
	stat, _ := os.Stat(path)
	svc := keyService(t)

	account, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: path, Provider: "github", Label: "Work", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if !account.Local() || account.Kind != KindSSHKey {
		t.Fatalf("account = %+v", account)
	}
	material, err := svc.ResolveSSHKey(account.ID, "", "git")
	if err != nil || material.PrivateKey != string(before) {
		t.Fatalf("the stored key is not the file's: %v", err)
	}
	after, _ := os.ReadFile(path)
	afterStat, _ := os.Stat(path)
	if string(after) != string(before) || !afterStat.ModTime().Equal(stat.ModTime()) {
		t.Fatal("import changed the original file")
	}
}

func TestImportNeedsThePassphraseOfAnEncryptedKeyAndChecksIt(t *testing.T) {
	ssh := fakeHome(t)
	path := putKey(t, ssh, "id_work", "pw")
	svc := keyService(t)
	for _, passphrase := range []string{"", "wrong"} {
		if _, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: path, Provider: "github", Label: "Work", Passphrase: passphrase}); !errors.Is(err, ErrKeyPassphrase) {
			t.Fatalf("passphrase %q: err = %v", passphrase, err)
		}
	}
	if _, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: path, Provider: "github", Label: "Work", Passphrase: "pw", Local: true}); err != nil {
		t.Fatal(err)
	}
}

func TestImportRefusesAFileThatIsNotAKeyWithoutQuotingIt(t *testing.T) {
	ssh := fakeHome(t)
	notKey := filepath.Join(ssh, "known_hosts")
	if err := os.WriteFile(notKey, []byte("github.com ssh-ed25519 AAAAsecretish\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := keyService(t)
	_, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: notKey, Provider: "github", Label: "x"})
	if !errors.Is(err, ErrKeyInvalid) || strings.Contains(err.Error(), "AAAA") {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: filepath.Join(ssh, "missing"), Provider: "github", Label: "x"}); err == nil {
		t.Fatal("a missing file was imported")
	}
}

// The same key may be on GitHub and GitLab both; never on one provider twice.
func TestTheSameKeyIsRefusedTwiceForOneProviderButNotForAnother(t *testing.T) {
	ssh := fakeHome(t)
	path := putKey(t, ssh, "id_work", "")
	svc := keyService(t)

	first, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: path, Provider: "github", Label: "Work", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ImportSSHKey(ImportSSHKeyInput{Path: path, Provider: "github", Label: "Again", Local: true})
	var exists *KeyExistsError
	if !errors.Is(err, ErrKeyExists) || !errors.As(err, &exists) || exists.AccountID != first.ID || exists.Label != "Work" {
		t.Fatalf("err = %v", err)
	}
	// Pasting the same key is the same refusal.
	key, _ := os.ReadFile(path)
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindSSHKey, Label: "Pasted", Secret: string(key)}); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("pasting a saved key = %v", err)
	}
	if _, err := svc.ImportSSHKey(ImportSSHKeyInput{Path: path, Provider: "gitlab", Label: "GitLab", Local: true}); err != nil {
		t.Fatalf("the same key for another provider: %v", err)
	}
}

func TestDetectSSHProviderReportsEveryProviderThatKnowsTheKey(t *testing.T) {
	ssh := fakeHome(t)
	path := putKey(t, ssh, "id_work", "pw")
	svc := keyService(t)
	svc.SetSSHIdentifier(func(_ context.Context, _ gitssh.Material, host string) (string, error) {
		switch host {
		case "github.com":
			return "acme-dev", nil
		case "bitbucket.org":
			return "acme-bb", nil
		}
		return "", gitssh.ErrNotAuthenticated
	})

	guesses, err := svc.DetectSSHProvider(context.Background(), path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(guesses) != 2 || guesses[0].Provider != "github" || guesses[0].Subject != "acme-dev" ||
		guesses[1].Provider != "bitbucket" || guesses[1].Subject != "acme-bb" {
		t.Fatalf("guesses = %+v", guesses)
	}
	if _, err := svc.DetectSSHProvider(context.Background(), path, "wrong"); !errors.Is(err, ErrKeyPassphrase) {
		t.Fatalf("a wrong passphrase reached the network: %v", err)
	}
}

func TestAnAliasIsBoundToAKeyAccountAndForgottenWhenItIsGone(t *testing.T) {
	svc := keyService(t)
	work := addKey(t, svc, "github", "Work key", true)
	token, _ := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Token", Secret: "ghp_1"})

	if err := svc.ChooseForAlias("GitHub-Work", work.ID); err != nil {
		t.Fatal(err)
	}
	got, ok := svc.ForAlias("github-work")
	if !ok || got.ID != work.ID {
		t.Fatalf("ForAlias = %+v, %v", got, ok)
	}
	// A token cannot be what an SSH alias names, and an empty alias names nothing.
	if err := svc.ChooseForAlias("github-work", token.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a token bound to an alias: %v", err)
	}
	if err := svc.ChooseForAlias("", work.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty alias: %v", err)
	}
	if err := svc.ChooseForAlias("github-work", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing account: %v", err)
	}

	if err := svc.Delete(work.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.ForAlias("github-work"); ok {
		t.Fatal("a deleted account still answers for the alias")
	}
	if id, _ := svc.remoteChoices.get("github-work", aliasOwner, ViaSSH); id != "" {
		t.Fatal("the stale alias row was kept")
	}
}

// The alias answer must not leak into the (host, owner) choices a real owner
// can have.
func TestAnAliasDoesNotDecideARealOwner(t *testing.T) {
	svc := keyService(t)
	work := addKey(t, svc, "github", "Work key", true)
	addKey(t, svc, "github", "Personal key", true)
	if err := svc.ChooseForAlias("github.com", work.ID); err != nil {
		t.Fatal(err)
	}
	match, err := svc.ForRemote("github.com", "acme-corp", "", ViaSSH)
	if err != nil || match.Decided {
		t.Fatalf("an alias row decided an owner: %+v, %v", match, err)
	}
}
