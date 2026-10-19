package accounts

import (
	"errors"
	"strings"
	"testing"

	"devkit/internal/storage"
)

func linkService(t *testing.T) *Service {
	t.Helper()
	svc := keyService(t)
	svc.SetLinks(svc.remoteChoices.db, storage.TableAccountLinks)
	svc.SetIdentities(svc.remoteChoices.db, storage.TableAccountIdentities)
	return svc
}

func addToken(t *testing.T, svc *Service, provider, label string, local bool) Account {
	t.Helper()
	account, err := svc.Create(CreateInput{Provider: provider, Kind: KindToken, Label: label, Secret: "glpat-" + label, Local: local})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func TestAKeyNamesTheTokenThatGoesWithItAndTheTokenNamesItsKeys(t *testing.T) {
	svc := linkService(t)
	key := addKey(t, svc, "gitlab", "GitLab key", true)
	first := addToken(t, svc, "gitlab", "first", true)
	second := addToken(t, svc, "gitlab", "second", false)

	linked, err := svc.LinkToken(key.ID, first.ID)
	if err != nil || linked.LinkedTokenID != first.ID {
		t.Fatalf("link = %+v, %v", linked, err)
	}
	if token, ok := svc.LinkedTokenOf(key.ID); !ok || token.ID != first.ID {
		t.Fatalf("LinkedTokenOf = %+v, %v", token, ok)
	}
	byID := map[string]Account{}
	all, _ := svc.List()
	for _, account := range all {
		byID[account.ID] = account
	}
	if byID[key.ID].LinkedTokenID != first.ID || len(byID[first.ID].LinkedKeyIDs) != 1 || byID[first.ID].LinkedKeyIDs[0] != key.ID {
		t.Fatalf("List: key %+v, token %+v", byID[key.ID], byID[first.ID])
	}
	if got, _ := svc.Get(first.ID); len(got.LinkedKeyIDs) != 1 {
		t.Fatalf("Get does not show the key a token serves: %+v", got)
	}

	// One token per key: choosing another replaces the first.
	if _, err := svc.LinkToken(key.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if token, _ := svc.LinkedTokenOf(key.ID); token.ID != second.ID {
		t.Fatalf("the replaced link still points at %s", token.ID)
	}
	if got, _ := svc.Get(first.ID); len(got.LinkedKeyIDs) != 0 {
		t.Fatalf("the first token still claims the key: %+v", got)
	}

	// Unlinking keeps both accounts.
	cleared, err := svc.UnlinkToken(key.ID)
	if err != nil || cleared.LinkedTokenID != "" {
		t.Fatalf("unlink = %+v, %v", cleared, err)
	}
	if _, ok := svc.LinkedTokenOf(key.ID); ok {
		t.Fatal("an unlinked key still finds a token")
	}
	for _, id := range []string{key.ID, first.ID, second.ID} {
		if _, err := svc.Get(id); err != nil {
			t.Fatalf("unlinking removed an account: %v", err)
		}
	}
}

// One token may serve two keys of the same login.
func TestOneTokenMayServeTwoKeys(t *testing.T) {
	svc := linkService(t)
	a := addKey(t, svc, "gitlab", "key a", true)
	b := addKey(t, svc, "gitlab", "key b", true)
	token := addToken(t, svc, "gitlab", "shared", true)
	for _, key := range []Account{a, b} {
		if _, err := svc.LinkToken(key.ID, token.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := svc.Get(token.ID)
	if len(got.LinkedKeyIDs) != 2 {
		t.Fatalf("token serves %v, want both keys", got.LinkedKeyIDs)
	}
}

func TestALinkNeedsAKeyAndATokenOfOneProvider(t *testing.T) {
	svc := linkService(t)
	key := addKey(t, svc, "gitlab", "GitLab key", true)
	otherKey := addKey(t, svc, "github", "GitHub key", true)
	gitlabToken := addToken(t, svc, "gitlab", "gitlab token", true)
	githubToken := addToken(t, svc, "github", "github token", true)
	basic, err := svc.Create(CreateInput{Provider: "gitlab", Kind: KindBasic, Label: "basic", User: "u", Secret: "p"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := svc.Create(CreateInput{Provider: "database", Kind: KindBasic, Label: "db", Secret: "p", ScopeOwner: "profile-1"})
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]string{
		"another provider":     {key.ID, githubToken.ID},
		"a token as the key":   {gitlabToken.ID, gitlabToken.ID},
		"a key as the token":   {key.ID, otherKey.ID},
		"a basic credential":   {key.ID, basic.ID},
		"a connection's login": {key.ID, connection.ID},
		"no such token":        {key.ID, "nope"},
	} {
		if _, err := svc.LinkToken(pair[0], pair[1]); err == nil {
			t.Errorf("%s was linked", name)
		}
	}
	if _, err := svc.LinkToken(key.ID, githubToken.ID); !errors.Is(err, ErrLinkMismatch) {
		t.Errorf("another provider = %v, want ErrLinkMismatch", err)
	}
	if got, _ := svc.Get(key.ID); got.LinkedTokenID != "" {
		t.Errorf("a refused link was written: %+v", got)
	}
}

func TestDeletingEitherEndForgetsTheLink(t *testing.T) {
	svc := linkService(t)
	key := addKey(t, svc, "gitlab", "GitLab key", true)
	token := addToken(t, svc, "gitlab", "token", true)
	if _, err := svc.LinkToken(key.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(token.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(key.ID); got.LinkedTokenID != "" {
		t.Fatalf("the key still points at a deleted token: %+v", got)
	}

	token = addToken(t, svc, "gitlab", "token two", true)
	if _, err := svc.LinkToken(key.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(key.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(token.ID); len(got.LinkedKeyIDs) != 0 {
		t.Fatalf("the token still serves a deleted key: %+v", got)
	}
	if svc.allLinks()[key.ID] != "" {
		t.Fatal("a row of the deleted key is left in the table")
	}
}

// A link to something this machine does not have is not drawn: another device's
// account, deleted behind our back.
func TestALinkToAMissingAccountIsNotShown(t *testing.T) {
	svc := linkService(t)
	key := addKey(t, svc, "gitlab", "GitLab key", true)
	token := addToken(t, svc, "gitlab", "token", false)
	if _, err := svc.LinkToken(key.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	// The token vanishes without Delete (a sync removed it).
	if err := svc.items.Delete(token.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(key.ID)
	if got.LinkedTokenID != "" {
		t.Fatalf("a link to a missing token is shown: %+v", got)
	}
	if _, ok := svc.LinkedTokenOf(key.ID); ok {
		t.Fatal("LinkedTokenOf found a missing token")
	}
}

func TestSyncingAKeyCarriesItsLinkToTheNewID(t *testing.T) {
	svc := linkService(t)
	key := addKey(t, svc, "gitlab", "GitLab key", true)
	token := addToken(t, svc, "gitlab", "token", true)
	if _, err := svc.LinkToken(key.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	shared, err := svc.Promote(key.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if shared.ID == key.ID {
		t.Fatal("promotion kept the id; the test proves nothing")
	}
	if shared.LinkedTokenID != token.ID {
		t.Fatalf("the promoted key lost its token: %+v", shared)
	}
	if got, _ := svc.Get(token.ID); len(got.LinkedKeyIDs) != 1 || got.LinkedKeyIDs[0] != shared.ID {
		t.Fatalf("the token still names the old key id: %+v", got)
	}
}

func TestRenameChangesTheLabelAndNothingElse(t *testing.T) {
	svc := linkService(t)
	shared := addToken(t, svc, "gitlab", "old name", false)
	local := addKey(t, svc, "gitlab", "old key", true)
	if _, err := svc.SetIdentity(shared.ID, Identity{Name: "A", Email: "a@b.example"}); err != nil {
		t.Fatal(err)
	}

	renamed, err := svc.Rename(shared.ID, "  GitLab · chiro  ")
	if err != nil || renamed.Label != "GitLab · chiro" || renamed.ID != shared.ID || renamed.AuthorEmail != "a@b.example" {
		t.Fatalf("shared rename = %+v, %v", renamed, err)
	}
	// The value still opens: a rename never touches the secret.
	if credential, err := svc.Resolve(ResolveInput{AccountID: shared.ID, By: "test"}); err != nil || credential.Secret != "glpat-old name" {
		t.Fatalf("after rename the secret is %+v, %v", credential, err)
	}
	keyRenamed, err := svc.Rename(local.ID, "Work laptop key")
	if err != nil || keyRenamed.Label != "Work laptop key" || !keyRenamed.Local() {
		t.Fatalf("local rename = %+v, %v", keyRenamed, err)
	}
	if _, err := svc.ResolveSSHKey(local.ID, "", "test"); err != nil {
		t.Fatalf("after rename the key no longer opens: %v", err)
	}
}

func TestRenameRefusesWhatItShould(t *testing.T) {
	svc := linkService(t)
	token := addToken(t, svc, "gitlab", "token", false)
	external, err := svc.RecordExternal("openai", "Codex (ChatGPT)", "sign in again")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := svc.Create(CreateInput{Provider: "database", Kind: KindBasic, Label: "db", Secret: "p", ScopeOwner: "profile-1"})
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"empty":      func() error { _, err := svc.Rename(token.ID, "   "); return err },
		"too long":   func() error { _, err := svc.Rename(token.ID, strings.Repeat("x", maxLabelRunes+1)); return err },
		"line break": func() error { _, err := svc.Rename(token.ID, "a\nb"); return err },
		"external":   func() error { _, err := svc.Rename(external.ID, "other"); return err },
		"connection": func() error { _, err := svc.Rename(connection.ID, "other"); return err },
	} {
		if err := call(); !errors.Is(err, ErrLabelInvalid) {
			t.Errorf("%s = %v, want ErrLabelInvalid", name, err)
		}
	}
	if _, err := svc.Rename("no-such", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing account = %v, want ErrNotFound", err)
	}
	if got, _ := svc.Get(token.ID); got.Label != "token" {
		t.Errorf("a refused rename changed the label to %q", got.Label)
	}
}

// The Pull requests card asks again when accounts change, so a link, an unlink
// and a rename must tell it — once each, and not for a refusal.
func TestLinkUnlinkAndRenameTellTheWindowOncePerWrite(t *testing.T) {
	svc := linkService(t)
	key := addKey(t, svc, "gitlab", "GitLab key", true)
	token := addToken(t, svc, "gitlab", "token", true)
	n := 0
	svc.OnChanged = func() { n++ }
	step := func(what string, want int, run func()) {
		t.Helper()
		before := n
		run()
		if n-before != want {
			t.Errorf("%s: OnChanged fired %d times, want %d", what, n-before, want)
		}
	}
	step("link", 1, func() { _, _ = svc.LinkToken(key.ID, token.ID) })
	step("unlink", 1, func() { _, _ = svc.UnlinkToken(key.ID) })
	step("rename", 1, func() { _, _ = svc.Rename(token.ID, "renamed") })
	step("a refused link", 0, func() { _, _ = svc.LinkToken(token.ID, key.ID) })
	step("a refused rename", 0, func() { _, _ = svc.Rename(token.ID, "  ") })
}
