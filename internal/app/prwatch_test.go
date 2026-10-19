package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/prwatch"
)

func tokenFor(t *testing.T, c prCredentials, req prwatch.TokenRequest) (string, string, error) {
	t.Helper()
	req.Host, req.Owner, req.Repo = "gitlab.com", "acme", "acme/api"
	return c.Token(context.Background(), req)
}

// The person's case: the remote has a GitLab key and nothing else. That is not
// "no account" — the notice has to say the key cannot read merge requests, with
// the key's label and none of its secret.
func TestAKeyOnlyRemoteSaysItHasAKeyNotNoAccount(t *testing.T) {
	svc, _ := pickerFixture(t)
	key := addSSHKey(t, svc, "gitlab", "id_ed25519_gitlab")
	c := prCredentials{accounts: svc}

	for name, req := range map[string]prwatch.TokenRequest{
		"found by host":  {},
		"bound to it":    {BoundAccountID: key.ID},
		"bound, stale":   {BoundAccountID: "gone"},
		"another env":    {Env: "dev"},
		"a person asked": {Interactive: true},
	} {
		secret, _, err := tokenFor(t, c, req)
		var keyOnly *prwatch.KeyOnlyError
		if !errors.Is(err, prwatch.ErrNoAccount) || !errors.As(err, &keyOnly) || keyOnly.Account != "id_ed25519_gitlab" || secret != "" {
			t.Errorf("%s: secret %q, err %v", name, secret, err)
		}
	}
}

func TestSeveralKeysNameNoneOfThem(t *testing.T) {
	svc, _ := pickerFixture(t)
	addSSHKey(t, svc, "gitlab", "one")
	addSSHKey(t, svc, "gitlab", "two")
	_, _, err := tokenFor(t, prCredentials{accounts: svc}, prwatch.TokenRequest{})
	var keyOnly *prwatch.KeyOnlyError
	if !errors.As(err, &keyOnly) || keyOnly.Account != "" || !errors.Is(err, prwatch.ErrNoAccount) {
		t.Fatalf("%v", err)
	}
}

func TestATokenStillWinsOverAKey(t *testing.T) {
	svc, _ := pickerFixture(t)
	addSSHKey(t, svc, "gitlab", "id_ed25519_gitlab")
	addAccount(t, svc, "gitlab", "GitLab token", "glpat-secret")
	secret, label, err := tokenFor(t, prCredentials{accounts: svc}, prwatch.TokenRequest{})
	if err != nil || secret != "glpat-secret" || label != "GitLab token" {
		t.Fatalf("%q %q %v", secret, label, err)
	}
}

// The key a remote is bound to brings the token the person linked to it, even
// when the host has other tokens (which, by host alone, would be ambiguous).
func TestAKeyBoundToTheRemoteBringsItsLinkedToken(t *testing.T) {
	svc, _ := pickerFixture(t)
	key := addSSHKey(t, svc, "gitlab", "id_ed25519_gitlab")
	mine := addAccount(t, svc, "gitlab", "chiro", "glpat-mine")
	addAccount(t, svc, "gitlab", "a bot", "glpat-bot")
	c := prCredentials{accounts: svc}

	// Not linked yet: two tokens on the host, nobody to ask → ambiguous.
	if _, _, err := tokenFor(t, c, prwatch.TokenRequest{BoundAccountID: key.ID}); !errors.Is(err, prwatch.ErrAmbiguousAccount) {
		t.Fatalf("unlinked = %v, want ErrAmbiguousAccount", err)
	}
	if _, err := svc.LinkToken(key.ID, mine.ID); err != nil {
		t.Fatal(err)
	}
	secret, label, err := tokenFor(t, c, prwatch.TokenRequest{BoundAccountID: key.ID})
	if err != nil || secret != "glpat-mine" || label != "chiro" {
		t.Fatalf("linked = %q %q %v, want the linked token", secret, label, err)
	}
	// A remote not bound to the key still decides by host, as before.
	if _, _, err := tokenFor(t, c, prwatch.TokenRequest{}); !errors.Is(err, prwatch.ErrAmbiguousAccount) {
		t.Fatalf("not bound = %v, want ErrAmbiguousAccount", err)
	}
	// Unlinking goes back to the host rule.
	if _, err := svc.UnlinkToken(key.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tokenFor(t, c, prwatch.TokenRequest{BoundAccountID: key.ID}); !errors.Is(err, prwatch.ErrAmbiguousAccount) {
		t.Fatalf("unlinked again = %v", err)
	}
}

func TestNoAccountAtAllIsStillPlainNoAccount(t *testing.T) {
	svc, _ := pickerFixture(t)
	addSSHKey(t, svc, "github", "a github key") // another host's key is nothing to this remote
	_, _, err := tokenFor(t, prCredentials{accounts: svc}, prwatch.TokenRequest{})
	var keyOnly *prwatch.KeyOnlyError
	if !errors.Is(err, prwatch.ErrNoAccount) || errors.As(err, &keyOnly) {
		t.Fatalf("%v", err)
	}
}

func TestTheKeyErrorCarriesNoSecret(t *testing.T) {
	svc, _ := pickerFixture(t)
	addSSHKey(t, svc, "gitlab", "id_ed25519_gitlab")
	_, _, got := tokenFor(t, prCredentials{accounts: svc}, prwatch.TokenRequest{})
	if got == nil || strings.Contains(got.Error(), "PRIVATE KEY") || strings.Contains(got.Error(), "BEGIN") {
		t.Fatalf("%v", got)
	}
}
