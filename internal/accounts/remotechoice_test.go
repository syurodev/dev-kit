package accounts

import (
	"errors"
	"testing"

	"devkit/internal/storage"
)

func remoteService(t *testing.T) *Service {
	t.Helper()
	svc, db := newService(t)
	svc.SetRemoteChoices(db, storage.TableAccountRemoteChoices)
	return svc
}

func twoGitHubAccounts(t *testing.T, svc *Service) (personal, work Account) {
	t.Helper()
	personal, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Personal", Secret: "ghp_1"})
	if err != nil {
		t.Fatal(err)
	}
	work, err = svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_2"})
	if err != nil {
		t.Fatal(err)
	}
	return personal, work
}

// The user's setup: a GitHub company account, a personal one, and a GitLab.
// GitLab is a different host, so it never needs asking about.
func TestForRemoteAsksOnlyWhereTheHostIsShared(t *testing.T) {
	svc := remoteService(t)
	twoGitHubAccounts(t, svc)
	gitlab, err := svc.Create(CreateInput{Provider: "gitlab", Kind: KindToken, Label: "GitLab", Secret: "glpat-1"})
	if err != nil {
		t.Fatal(err)
	}

	match, err := svc.ForRemote("gitlab.com", "acme", "")
	if err != nil {
		t.Fatal(err)
	}
	if !match.Decided || match.Account.ID != gitlab.ID {
		t.Fatalf("gitlab.com has one account and must be decided: %+v", match)
	}

	match, err = svc.ForRemote("github.com", "acme-corp", "")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided || len(match.Options) != 2 {
		t.Fatalf("two GitHub accounts must be a question, not a guess: %+v", match)
	}
}

// The reason the key is (host, owner) and not (host): company for acme-corp
// must not decide the person's own repositories on the same host.
func TestRemoteChoiceIsRememberedPerOwner(t *testing.T) {
	svc := remoteService(t)
	personal, work := twoGitHubAccounts(t, svc)

	if err := svc.ChooseForRemote("github.com", "Acme-Corp", "", work.ID); err != nil {
		t.Fatal(err)
	}
	match, err := svc.ForRemote("GITHUB.COM", "acme-corp", "")
	if err != nil {
		t.Fatal(err)
	}
	if !match.Decided || match.Account.ID != work.ID || !match.Remembered {
		t.Fatalf("the answer for the organisation was not remembered: %+v", match)
	}

	match, err = svc.ForRemote("github.com", "syurodev", "")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided {
		t.Fatalf("another owner on the same host was decided by acme-corp's answer: %+v", match)
	}

	if err := svc.ChooseForRemote("github.com", "syurodev", "", personal.ID); err != nil {
		t.Fatal(err)
	}
	match, _ = svc.ForRemote("github.com", "syurodev", "")
	if match.Account.ID != personal.ID {
		t.Fatalf("second owner = %+v", match)
	}
}

func TestChooseForRemoteRefusesAnAccountThatWasNeverOffered(t *testing.T) {
	svc := remoteService(t)
	twoGitHubAccounts(t, svc)
	gitlab, err := svc.Create(CreateInput{Provider: "gitlab", Kind: KindToken, Label: "GitLab", Secret: "glpat-1"})
	if err != nil {
		t.Fatal(err)
	}
	// A GitLab token pinned to github.com would be sent to the wrong service.
	if err := svc.ChooseForRemote("github.com", "acme-corp", "", gitlab.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an account of another provider was accepted: %v", err)
	}
	if err := svc.ChooseForRemote("github.com", "", "", gitlab.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty owner was accepted: %v", err)
	}
}

func TestStaleRemoteChoiceIsDroppedAndAskedAgain(t *testing.T) {
	svc := remoteService(t)
	personal, work := twoGitHubAccounts(t, svc)
	if err := svc.ChooseForRemote("github.com", "acme-corp", "", work.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(work.ID); err != nil {
		t.Fatal(err)
	}

	// One account left is decided by being the only one; add a second so the
	// stale row is what gets consulted.
	extra, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Extra", Secret: "ghp_3"})
	if err != nil {
		t.Fatal(err)
	}
	match, err := svc.ForRemote("github.com", "acme-corp", "")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided {
		t.Fatalf("a deleted account's answer decided: %+v", match)
	}
	if len(match.Options) != 2 || match.Options[0].ID == work.ID || match.Options[1].ID == work.ID {
		t.Fatalf("options include the deleted account: %+v (personal=%s extra=%s)", match.Options, personal.ID, extra.ID)
	}
	if id, _ := svc.remoteChoices.get("github.com", "acme-corp"); id != "" {
		t.Fatalf("the stale row was kept: %q", id)
	}
}

func TestForRemoteWithoutAStoreStillAsks(t *testing.T) {
	svc, _ := newService(t)
	twoGitHubAccounts(t, svc)
	match, err := svc.ForRemote("github.com", "acme-corp", "")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided || len(match.Options) != 2 {
		t.Fatalf("without a store the question must still be asked: %+v", match)
	}
}
