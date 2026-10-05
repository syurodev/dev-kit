package accounts

import (
	"errors"
	"testing"

	"devkit/internal/storage"
)

func identityService(t *testing.T) *Service {
	t.Helper()
	svc := keyService(t)
	svc.SetIdentities(svc.remoteChoices.db, storage.TableAccountIdentities)
	return svc
}

func TestAnAccountRemembersWhoItCommitsAs(t *testing.T) {
	svc := identityService(t)
	work, _ := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_1"})
	key := addKey(t, svc, "github", "Key", true)

	for _, id := range []string{work.ID, key.ID} {
		saved, err := svc.SetIdentity(id, Identity{Name: "  Acme Dev ", Email: " dev@acme.example "})
		if err != nil {
			t.Fatal(err)
		}
		if saved.AuthorName != "Acme Dev" || saved.AuthorEmail != "dev@acme.example" {
			t.Fatalf("saved = %+v", saved)
		}
		got, ok := svc.IdentityOf(id)
		if !ok || got.Email != "dev@acme.example" {
			t.Fatalf("IdentityOf = %+v, %v", got, ok)
		}
	}

	all, _ := svc.List()
	for _, account := range all {
		if account.AuthorEmail != "dev@acme.example" {
			t.Fatalf("List dropped the identity of %s: %+v", account.Label, account)
		}
	}

	cleared, err := svc.SetIdentity(work.ID, Identity{})
	if err != nil || cleared.AuthorName != "" || cleared.AuthorEmail != "" {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
	if _, ok := svc.IdentityOf(work.ID); ok {
		t.Fatal("a cleared identity is still there")
	}
}

// Half an identity would be a name from one place and an email from another.
func TestAnIdentityIsBothHalvesOrNeither(t *testing.T) {
	svc := identityService(t)
	account, _ := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_1"})
	for name, identity := range map[string]Identity{
		"name only":      {Name: "Acme Dev"},
		"email only":     {Email: "dev@acme.example"},
		"no at sign":     {Name: "A", Email: "dev.acme.example"},
		"angle brackets": {Name: "A <b>", Email: "a@b.example"},
		"email in angle": {Name: "A", Email: "<a@b.example>"},
		"newline name":   {Name: "A\nB", Email: "a@b.example"},
		"newline email":  {Name: "A", Email: "a@b.example\nCc: x"},
		"space in email": {Name: "A", Email: "a b@c.example"},
	} {
		if _, err := svc.SetIdentity(account.ID, identity); !errors.Is(err, ErrIdentityInvalid) {
			t.Errorf("%s: err = %v, want ErrIdentityInvalid", name, err)
		}
	}
	if _, err := svc.SetIdentity("missing", Identity{Name: "A", Email: "a@b.example"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing account: %v", err)
	}
}

func TestIdentityFollowsAnAccountThroughPromoteAndDiesWithDelete(t *testing.T) {
	svc := identityService(t)
	local := addKey(t, svc, "github", "Key", true)
	if _, err := svc.SetIdentity(local.ID, Identity{Name: "Acme Dev", Email: "dev@acme.example"}); err != nil {
		t.Fatal(err)
	}

	shared, err := svc.Promote(local.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if shared.AuthorEmail != "dev@acme.example" {
		t.Fatalf("promote lost the identity: %+v", shared)
	}
	if _, ok := svc.IdentityOf(local.ID); ok {
		t.Fatal("the old id still has an identity")
	}

	if err := svc.Delete(shared.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.IdentityOf(shared.ID); ok {
		t.Fatal("a deleted account's identity is still stored")
	}
}

func TestWithoutAStoreAccountsHaveNoIdentity(t *testing.T) {
	svc, _ := newService(t)
	account, _ := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_1"})
	if _, err := svc.SetIdentity(account.ID, Identity{Name: "A", Email: "a@b.example"}); err == nil {
		t.Fatal("an identity was accepted with nowhere to keep it")
	}
	if _, ok := svc.IdentityOf(account.ID); ok {
		t.Fatal("IdentityOf invented one")
	}
}
