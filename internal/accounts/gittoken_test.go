package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAGitTokenStaysLocalUnlessSyncedAndResolvesByID(t *testing.T) {
	svc := keyService(t)

	local, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Work", Token: "ghp_work", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := svc.CreateGitToken(GitTokenInput{Provider: "gitlab", Label: "GitLab", User: "syuro", Token: "glpat-x"})
	if err != nil {
		t.Fatal(err)
	}
	if !local.Local() || local.Kind != KindToken || shared.Scoped() {
		t.Fatalf("local=%+v shared=%+v", local, shared)
	}
	// The shared one is a vault item (it syncs); the local one is not.
	if _, err := svc.items.Get(shared.ID); err != nil {
		t.Fatalf("a synced token is not a vault item: %v", err)
	}
	if _, err := svc.items.Get(local.ID); err == nil {
		t.Fatal("a local token was written to vault_items, which replicates")
	}

	credential, err := svc.Resolve(ResolveInput{AccountID: local.ID, By: "git"})
	if err != nil || credential.Secret != "ghp_work" || credential.Kind != KindToken {
		t.Fatalf("local credential = %+v, %v", credential, err)
	}
	credential, err = svc.Resolve(ResolveInput{AccountID: shared.ID, By: "git"})
	if err != nil || credential.Secret != "glpat-x" || credential.User != "syuro" {
		t.Fatalf("shared credential = %+v, %v", credential, err)
	}
}

// Pasting from a web page brings a trailing newline, and a wrapped token brings
// a space in the middle: either one makes a token git cannot use.
func TestAGitTokenIsCheckedBeforeItIsSaved(t *testing.T) {
	svc := keyService(t)
	for name, in := range map[string]GitTokenInput{
		"empty":           {Provider: "github", Label: "x"},
		"only spaces":     {Provider: "github", Label: "x", Token: "   "},
		"space inside":    {Provider: "github", Label: "x", Token: "ghp_a b"},
		"line break":      {Provider: "github", Label: "x", Token: "ghp_a\nb"},
		"control":         {Provider: "github", Label: "x", Token: "ghp_a\x00b"},
		"username spaces": {Provider: "github", Label: "x", Token: "ghp_a", User: "a b"},
	} {
		if _, err := svc.CreateGitToken(in); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("%s: err = %v, want ErrTokenInvalid", name, err)
		}
	}
	for name, in := range map[string]GitTokenInput{
		"not a git host": {Provider: "anthropic", Label: "x", Token: "sk-ant"},
		"no such":        {Provider: "nowhere", Label: "x", Token: "t"},
		"no label":       {Provider: "github", Token: "ghp_a"},
	} {
		if _, err := svc.CreateGitToken(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if all, _ := svc.List(); len(all) != 0 {
		t.Fatalf("a refused token left %d accounts", len(all))
	}

	// A paste with whitespace around it is just trimmed.
	account, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "x", Token: "  ghp_ok\n", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if credential, _ := svc.Resolve(ResolveInput{AccountID: account.ID}); credential.Secret != "ghp_ok" {
		t.Fatalf("stored %q", credential.Secret)
	}
}

func TestBitbucketNeedsAUsernameAndIsABasicCredential(t *testing.T) {
	svc := keyService(t)
	if _, err := svc.CreateGitToken(GitTokenInput{Provider: "bitbucket", Label: "BB", Token: "app-password"}); !errors.Is(err, ErrUsernameRequired) {
		t.Fatalf("no username: %v", err)
	}
	account, err := svc.CreateGitToken(GitTokenInput{Provider: "bitbucket", Label: "BB", User: "acme-bb", Token: "app-password", Local: true})
	if err != nil || account.Kind != KindBasic {
		t.Fatalf("account = %+v, %v", account, err)
	}
	if credential, _ := svc.Resolve(ResolveInput{AccountID: account.ID}); credential.User != "acme-bb" {
		t.Fatalf("credential = %+v", credential)
	}
}

// A local token is a candidate for a remote like any other: one on a host is
// chosen, two are a question.
func TestALocalTokenIsOfferedForHTTPSRemotesAndForTheAPIClient(t *testing.T) {
	svc := keyService(t)
	work, _ := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Work", Token: "ghp_work", Local: true})

	match, err := svc.ForRemote("github.com", "acme-corp", "", ViaHTTPS)
	if err != nil || !match.Decided || match.Account.ID != work.ID {
		t.Fatalf("https = %+v, %v", match, err)
	}
	api, err := svc.ForHost("api.github.com", "")
	if err != nil || !api.Decided || api.Account.ID != work.ID {
		t.Fatalf("api client = %+v, %v", api, err)
	}
	// Over SSH a token is not a candidate.
	if m, _ := svc.ForRemote("github.com", "acme-corp", "", ViaSSH); len(m.Options) != 0 {
		t.Fatalf("a token was offered for SSH: %+v", m)
	}

	if _, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Personal", Token: "ghp_me", Local: true}); err != nil {
		t.Fatal(err)
	}
	if m, _ := svc.ForRemote("github.com", "acme-corp", "", ViaHTTPS); m.Decided || len(m.Options) != 2 {
		t.Fatalf("two tokens on one host must be a question: %+v", m)
	}
}

func TestALocalTokenCanStartSyncing(t *testing.T) {
	svc := keyService(t)
	local, _ := svc.CreateGitToken(GitTokenInput{Provider: "gitlab", Label: "GL", User: "syuro", Token: "glpat-x", Local: true})
	shared, err := svc.Promote(local.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := svc.Resolve(ResolveInput{AccountID: shared.ID})
	if err != nil || credential.Secret != "glpat-x" || credential.User != "syuro" || shared.Scoped() {
		t.Fatalf("promoted = %+v, credential %+v, %v", shared, credential, err)
	}
}

// A local token is verified like a shared one; what the provider said about who
// it is stays in the local row.
func TestAnAccountKeptLocalIsVerifiedAndRemembersWho(t *testing.T) {
	var sawAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if sawAuth != "Bearer ghp_work" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-OAuth-Scopes", "repo")
		_, _ = w.Write([]byte(`{"login":"acme-dev"}`))
	}))
	t.Cleanup(server.Close)

	svc := keyService(t)
	svc.SetVerifyClient(server.Client())
	svc.providers = NewRegistry([]Provider{{
		ID: "github", Name: "GitHub", Kinds: []Kind{KindToken}, SSHHost: "github.com",
		Verify: VerifySpec{URL: server.URL + "/user", Header: "Authorization", HeaderFormat: "Bearer %s", SubjectField: "login", ScopesHeader: "X-OAuth-Scopes"},
		Hosts:  []string{"github.com"},
	}})

	good, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Work", Token: "ghp_work", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := svc.Verify(context.Background(), good.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Subject != "acme-dev" || verified.State != StateValid || len(verified.Scopes) != 1 || verified.Scopes[0] != "repo" {
		t.Fatalf("verified = %+v", verified)
	}
	again, _ := svc.Get(good.ID)
	if again.Subject != "acme-dev" || again.State != StateValid {
		t.Fatalf("the answer was not kept: %+v", again)
	}

	// A refused token is revoked, and the account keeps saying so.
	bad, _ := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Old", Token: "ghp_old", Local: true})
	revoked, err := svc.Verify(context.Background(), bad.ID)
	if err != nil || revoked.State != StateRevoked {
		t.Fatalf("revoked = %+v, %v", revoked, err)
	}
	if got, _ := svc.Get(bad.ID); got.State != StateRevoked {
		t.Fatalf("the verdict was not kept: %+v", got)
	}
}

// Local is for credentials that hold a value and are not restricted to an
// environment tag: the local row has nowhere to keep one.
func TestLocalIsRefusedWhereItCannotWork(t *testing.T) {
	svc := keyService(t)
	for name, in := range map[string]CreateInput{
		"external":   {Provider: "openai", Kind: KindExternal, Label: "x", Local: true},
		"with env":   {Provider: "github", Kind: KindToken, Label: "x", Secret: "t", Env: []string{"dev"}, Local: true},
		"with owner": {Provider: "github", Kind: KindToken, Label: "x", Secret: "t", ScopeOwner: "profile", Local: true},
	} {
		if _, err := svc.Create(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "x", Secret: "t", Passphrase: "p"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a passphrase on a token: %v", err)
	}
}
