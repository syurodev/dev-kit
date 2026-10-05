package accounts

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestRotateTokenKeepsTheIDTheUsernameAndWhatIsBoundToIt(t *testing.T) {
	svc := identityService(t)
	for _, local := range []bool{true, false} {
		account, err := svc.CreateGitToken(GitTokenInput{Provider: "bitbucket", Label: "BB", User: "acme-bb", Token: "old-pass", Local: local})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetIdentity(account.ID, Identity{Name: "Acme Dev", Email: "dev@acme.example"}); err != nil {
			t.Fatal(err)
		}

		rotated, err := svc.RotateToken(account.ID, "", "  new-pass\n")
		if err != nil {
			t.Fatalf("local=%v: %v", local, err)
		}
		if rotated.ID != account.ID || rotated.Kind != KindBasic || rotated.State != StateUnverified {
			t.Fatalf("local=%v: rotated = %+v", local, rotated)
		}
		credential, err := svc.Resolve(ResolveInput{AccountID: account.ID})
		if err != nil || credential.Secret != "new-pass" || credential.User != "acme-bb" {
			t.Fatalf("local=%v: credential = %+v, %v", local, credential, err)
		}
		if rotated.AuthorEmail != "dev@acme.example" {
			t.Fatalf("local=%v: the identity did not survive: %+v", local, rotated)
		}

		// A new username replaces the old one.
		if _, err := svc.RotateToken(account.ID, "other-bb", "newer-pass"); err != nil {
			t.Fatal(err)
		}
		if credential, _ := svc.Resolve(ResolveInput{AccountID: account.ID}); credential.User != "other-bb" || credential.Secret != "newer-pass" {
			t.Fatalf("local=%v: credential = %+v", local, credential)
		}
	}
}

// An account limited to one environment refuses a Resolve made with none; the
// owner replacing their own value must not be locked out of keeping the username.
func TestRotateTokenWorksOnAnAccountRestrictedToAnEnvironment(t *testing.T) {
	svc := keyService(t)
	account, err := svc.Create(CreateInput{
		Provider: "bitbucket", Kind: KindBasic, Label: "BB", User: "acme-bb", Secret: "old", Env: []string{"prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RotateToken(account.ID, "", "new"); err != nil {
		t.Fatal(err)
	}
	credential, err := svc.Resolve(ResolveInput{AccountID: account.ID, Env: "prod"})
	if err != nil || credential.User != "acme-bb" || credential.Secret != "new" {
		t.Fatalf("credential = %+v, %v", credential, err)
	}
}

func TestRotateTokenChecksTheNewValueAndTheAccount(t *testing.T) {
	svc := keyService(t)
	token, _ := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "T", Token: "ghp_old", Local: true})
	key := addKey(t, svc, "github", "Key", true)
	bitbucket, _ := svc.CreateGitToken(GitTokenInput{Provider: "bitbucket", Label: "BB", User: "u", Token: "p"})
	external, _ := svc.Create(CreateInput{Provider: "openai", Kind: KindExternal, Label: "Codex"})
	connection, _ := svc.Create(CreateInput{Provider: ProviderDatabase, Kind: KindSecret, Label: "db", Secret: "pw", ScopeOwner: "profile-1"})

	for name, in := range map[string]struct {
		id, user, token string
		want            error
	}{
		"empty":        {token.ID, "", "", ErrTokenInvalid},
		"space inside": {token.ID, "", "a b", ErrTokenInvalid},
		"line break":   {token.ID, "", "a\nb", ErrTokenInvalid},
		"bad username": {bitbucket.ID, "a b", "p2", ErrTokenInvalid},
		"ssh key":      {key.ID, "", "x", ErrInvalid},
		"external":     {external.ID, "", "x", ErrInvalid},
		"connection":   {connection.ID, "", "x", ErrInvalid},
		"missing":      {"missing", "", "x", ErrNotFound},
	} {
		if _, err := svc.RotateToken(in.id, in.user, in.token); !errors.Is(err, in.want) {
			t.Errorf("%s: err = %v, want %v", name, err, in.want)
		}
	}
	// A refused rotation changed nothing.
	if credential, _ := svc.Resolve(ResolveInput{AccountID: token.ID}); credential.Secret != "ghp_old" {
		t.Fatalf("a refused rotation changed the token to %q", credential.Secret)
	}
}

// What the provider said about the old token is a fact about the old token: a new
// one may belong to another login, and an expiry that outlived its token reads as
// a fresh token that has already run out.
func TestRotatingForgetsWhatTheProviderSaidAboutTheOldValue(t *testing.T) {
	var login = "acme-dev"
	svc, _ := verifyingService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo")
		w.Header().Set("GitHub-Authentication-Token-Expiration", "2020-01-01")
		_, _ = w.Write([]byte(`{"login":"` + login + `"}`))
	})

	shared, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Shared", Secret: "ghp_old"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Local", Secret: "ghp_old2", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{shared.ID, local.ID} {
		verified, err := svc.Verify(context.Background(), id)
		if err != nil || verified.Subject != "acme-dev" || verified.State == StateUnverified {
			t.Fatalf("verified = %+v, %v", verified, err)
		}
	}

	login = "someone-else"
	for _, id := range []string{shared.ID, local.ID} {
		rotated, err := svc.RotateToken(id, "", "ghp_new")
		if err != nil {
			t.Fatal(err)
		}
		if rotated.Subject != "" || rotated.State != StateUnverified || len(rotated.Scopes) != 0 || !rotated.ExpiresAt.IsZero() {
			t.Fatalf("rotating kept the old verdict: %+v", rotated)
		}
		again, err := svc.Verify(context.Background(), id)
		if err != nil || again.Subject != "someone-else" {
			t.Fatalf("verifying the new token = %+v, %v", again, err)
		}
	}
}
