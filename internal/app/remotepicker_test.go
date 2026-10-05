package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"devkit/internal/accounts"
	gitservice "devkit/internal/git"
	"devkit/internal/interaction"
	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type allEnvs struct{}

func (allEnvs) Exists(string) bool { return true }

func pickerFixture(t *testing.T) (*accounts.Service, *interaction.Hub) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	svc := accounts.NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), db,
		storage.TableAccountSecrets, v, accounts.NewRegistry(accounts.DefaultProviders()), allEnvs{})
	svc.SetRemoteChoices(db, storage.TableAccountRemoteChoices)
	return svc, interaction.NewHub(interaction.NoopPrompter{})
}

func addAccount(t *testing.T, svc *accounts.Service, provider, label, secret string) accounts.Account {
	t.Helper()
	account, err := svc.Create(accounts.CreateInput{Provider: provider, Kind: accounts.KindToken, Label: label, Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

// answerNextQuestion plays the person: it waits for the dialog and completes it.
func answerNextQuestion(t *testing.T, hub *interaction.Hub, answer func(interaction.Pending) string) {
	t.Helper()
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if pending := hub.Pending(); pending != nil {
				_ = hub.Complete(pending.ID, answer(*pending))
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
}

var pushToAcme = gitservice.PickAccountInput{
	Op: "push", Remote: "origin", Host: "github.com", Owner: "acme-corp", Repo: "api",
}

// The user's case end to end: two GitHub accounts, one GitLab. GitLab is never
// asked about; GitHub is asked once for the organisation, and then remembered.
func TestRemotePickerAsksOnceForAnOrganisationAndRemembersIt(t *testing.T) {
	svc, hub := pickerFixture(t)
	personal := addAccount(t, svc, "github", "Personal", "ghp_1")
	work := addAccount(t, svc, "github", "Work", "ghp_2")
	gitlab := addAccount(t, svc, "gitlab", "GitLab", "glpat-1")
	picker := remotePicker{accounts: svc, hub: hub}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := picker.PickAccount(ctx, gitservice.PickAccountInput{Op: "fetch", Remote: "origin", Host: "gitlab.com", Owner: "team", Repo: "svc"})
	if err != nil || id != gitlab.ID {
		t.Fatalf("gitlab.com = %q, %v; want the only account without asking", id, err)
	}

	var asked interaction.Pending
	answerNextQuestion(t, hub, func(p interaction.Pending) string {
		asked = p
		return interaction.AccountAnswer(work.ID, true)
	})
	id, err = picker.PickAccount(ctx, pushToAcme)
	if err != nil || id != work.ID {
		t.Fatalf("github.com/acme-corp = %q, %v; want the account the person chose", id, err)
	}
	if asked.Kind != interaction.KindGitAccount || len(asked.Options) != 2 ||
		asked.Summary != "Push acme-corp/api to github.com" || asked.Remember != "acme-corp on github.com" {
		t.Fatalf("the dialog was %+v", asked)
	}

	// Same organisation, another repository: answered from memory, no dialog.
	id, err = picker.PickAccount(ctx, gitservice.PickAccountInput{Op: "pull", Host: "github.com", Owner: "acme-corp", Repo: "web"})
	if err != nil || id != work.ID || hub.Pending() != nil {
		t.Fatalf("the organisation was asked about twice: id=%q err=%v pending=%v", id, err, hub.Pending())
	}

	// The person's own repositories on the same host are a different question.
	answerNextQuestion(t, hub, func(interaction.Pending) string { return interaction.AccountAnswer(personal.ID, false) })
	id, err = picker.PickAccount(ctx, gitservice.PickAccountInput{Op: "push", Host: "github.com", Owner: "syurodev", Repo: "dotfiles"})
	if err != nil || id != personal.ID {
		t.Fatalf("own repository = %q, %v", id, err)
	}
	// It was answered "just this once", so it is asked again.
	answerNextQuestion(t, hub, func(interaction.Pending) string { return interaction.AccountAnswer(personal.ID, false) })
	if _, err := picker.PickAccount(ctx, gitservice.PickAccountInput{Op: "push", Host: "github.com", Owner: "syurodev", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
}

func TestRemotePickerLeavesAHostWithNoAccountToGit(t *testing.T) {
	svc, hub := pickerFixture(t)
	addAccount(t, svc, "github", "Personal", "ghp_1")
	id, err := remotePicker{accounts: svc, hub: hub}.PickAccount(context.Background(),
		gitservice.PickAccountInput{Op: "push", Host: "git.example.test", Owner: "a", Repo: "b"})
	if err != nil || id != "" {
		t.Fatalf("unknown host = %q, %v; want git's own configuration", id, err)
	}
}

// Cancelling the dialog stops the command; it does not choose for the person.
func TestRemotePickerCancelledQuestionChoosesNothing(t *testing.T) {
	svc, hub := pickerFixture(t)
	addAccount(t, svc, "github", "Personal", "ghp_1")
	addAccount(t, svc, "github", "Work", "ghp_2")
	answerNextQuestion(t, hub, func(interaction.Pending) string { return "" })
	id, err := remotePicker{accounts: svc, hub: hub}.PickAccount(context.Background(), pushToAcme)
	if !errors.Is(err, interaction.ErrConfirmationDenied) || id != "" {
		t.Fatalf("cancel = %q, %v", id, err)
	}
}

// Server mode and the agent terminal have nobody to ask. Two accounts there is
// an error, not "the first one".
func TestRemotePickerWithNobodyToAskFailsFast(t *testing.T) {
	svc, hub := pickerFixture(t)
	addAccount(t, svc, "github", "Personal", "ghp_1")
	addAccount(t, svc, "github", "Work", "ghp_2")

	hub.Headless = true
	if _, err := (remotePicker{accounts: svc, hub: hub}).PickAccount(context.Background(), pushToAcme); !errors.Is(err, interaction.ErrSelectionRequired) {
		t.Fatalf("headless = %v", err)
	}
	if _, err := (remotePicker{accounts: svc}).PickAccount(context.Background(), pushToAcme); !errors.Is(err, interaction.ErrSelectionRequired) {
		t.Fatalf("no hub = %v", err)
	}
}

func addSSHKey(t *testing.T, svc *accounts.Service, provider, label string) accounts.Account {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.Create(accounts.CreateInput{
		Provider: provider, Kind: accounts.KindSSHKey, Label: label,
		Secret: string(pem.EncodeToMemory(block)), Local: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

// The user's real setup: SSH remotes, two GitHub keys and a GitLab key. GitLab
// is decided without a dialog; GitHub asks once per organisation, and the
// answer for SSH is its own — a token elsewhere is never offered.
func TestRemotePickerChoosesKeysForSSHRemotes(t *testing.T) {
	svc, hub := pickerFixture(t)
	work := addSSHKey(t, svc, "github", "Work key")
	addSSHKey(t, svc, "github", "Personal key")
	gitlab := addSSHKey(t, svc, "gitlab", "GitLab key")
	addAccount(t, svc, "github", "A token", "ghp_1") // must not be offered for SSH
	picker := remotePicker{accounts: svc, hub: hub}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := picker.PickAccount(ctx, gitservice.PickAccountInput{
		Op: "push", Transport: gitservice.TransportSSH, Host: "gitlab.com", Owner: "team", Repo: "svc",
	})
	if err != nil || id != gitlab.ID {
		t.Fatalf("gitlab = %q, %v", id, err)
	}

	var asked interaction.Pending
	answerNextQuestion(t, hub, func(p interaction.Pending) string {
		asked = p
		return interaction.AccountAnswer(work.ID, true)
	})
	ssh := gitservice.PickAccountInput{Op: "push", Transport: gitservice.TransportSSH, Host: "github.com", Owner: "acme-corp", Repo: "api"}
	id, err = picker.PickAccount(ctx, ssh)
	if err != nil || id != work.ID {
		t.Fatalf("github = %q, %v", id, err)
	}
	if len(asked.Options) != 2 {
		t.Fatalf("the dialog offered %d accounts, want only the two keys: %+v", len(asked.Options), asked.Options)
	}
	// Remembered for SSH: the next repository of the organisation is not asked.
	ssh.Repo = "web"
	if id, err = picker.PickAccount(ctx, ssh); err != nil || id != work.ID || hub.Pending() != nil {
		t.Fatalf("organisation asked twice over ssh: %q, %v", id, err)
	}
}

func TestResolverHandsGitAKeyAndSaysWhenAnAccountIsNotOne(t *testing.T) {
	svc, _ := pickerFixture(t)
	key := addSSHKey(t, svc, "github", "Key")
	token := addAccount(t, svc, "github", "Token", "ghp_1")
	resolver := accountResolver{accounts: svc}

	material, isKey, err := resolver.ResolveSSHKey(context.Background(), key.ID, "")
	if err != nil || !isKey || material.PrivateKey == "" {
		t.Fatalf("key = %v, %v", isKey, err)
	}
	if _, isKey, err = resolver.ResolveSSHKey(context.Background(), token.ID, ""); err != nil || isKey {
		t.Fatalf("a token is not a key: isKey=%v err=%v", isKey, err)
	}
	if _, _, err = resolver.ResolveSSHKey(context.Background(), "missing", ""); err == nil {
		t.Fatal("a missing account was not an error")
	}
}
