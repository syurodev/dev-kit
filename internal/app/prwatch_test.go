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
