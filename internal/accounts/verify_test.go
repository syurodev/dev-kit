package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// verifyingService points one provider at a test server. The client comes from
// httptest so the package never needs a "skip TLS" flag that could be true in
// production.
func verifyingService(t *testing.T, handler http.HandlerFunc) (*Service, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	svc, _ := newService(t)
	svc.SetVerifyClient(server.Client())
	svc.providers = NewRegistry([]Provider{{
		ID: "github", Name: "GitHub", Kinds: []Kind{KindToken},
		Verify: VerifySpec{
			URL: server.URL + "/user", Header: "Authorization",
			HeaderFormat: "Bearer %s", SubjectField: "login",
			ScopesHeader: "X-OAuth-Scopes",
		},
		Hosts: []string{"api.github.com"},
	}})
	return svc, server.URL
}

// Verifying is what turns a stored string into an account: it learns who the
// credential belongs to and what it can do.
func TestVerifyRecordsSubjectAndScopes(t *testing.T) {
	var sawAuth string
	svc, _ := verifyingService(t, func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("X-OAuth-Scopes", "repo, read:org")
		w.Header().Set("GitHub-Authentication-Token-Expiration", "2026-12-01")
		_, _ = w.Write([]byte(`{"login":"syurodev"}`))
	})

	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "ghp_x"})
	if err != nil {
		t.Fatal(err)
	}
	if created.State != StateUnverified {
		t.Fatalf("a new account starts %q", created.State)
	}

	verified, err := svc.Verify(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer ghp_x" {
		t.Fatalf("provider saw %q", sawAuth)
	}
	if verified.Subject != "syurodev" {
		t.Fatalf("subject = %q", verified.Subject)
	}
	if len(verified.Scopes) != 2 || verified.Scopes[0] != "repo" {
		t.Fatalf("scopes = %v", verified.Scopes)
	}
	if verified.ExpiresAt.IsZero() {
		t.Fatal("the expiry the provider reported was not recorded")
	}
	if verified.State != StateExpired && verified.State != StateValid && verified.State != StateExpiring {
		t.Fatalf("state = %q", verified.State)
	}

	// It sticks: reading the account back shows what verification learned.
	again, err := svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Subject != "syurodev" || again.VerifiedAt.IsZero() {
		t.Fatalf("verification was not persisted: %+v", again)
	}
}

// A provider saying no is an answer worth recording — that is the whole point
// of a state the list can show.
func TestVerifyRecordsARejectionAsRevoked(t *testing.T) {
	svc, _ := verifyingService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "old"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := svc.Verify(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.State != StateRevoked {
		t.Fatalf("state = %q, want revoked", verified.State)
	}
}

// The network failing teaches nothing about the credential. Recording
// "revoked" would be a lie that outlives the outage.
func TestVerifyDoesNotRecordAnythingWhenItCannotReachTheProvider(t *testing.T) {
	svc, url := verifyingService(t, func(http.ResponseWriter, *http.Request) {})
	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Point at a port nothing is listening on.
	svc.providers = NewRegistry([]Provider{{
		ID: "github", Name: "GitHub", Kinds: []Kind{KindToken},
		Verify: VerifySpec{URL: url + "/user", Header: "Authorization", HeaderFormat: "Bearer %s"},
	}})
	svc.SetVerifyClient(&http.Client{Transport: failingTransport{}})

	if _, err := svc.Verify(context.Background(), created.ID); err == nil {
		t.Fatal("an unreachable provider reported success")
	}
	after, err := svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != StateUnverified {
		t.Fatalf("state = %q; an outage must not be recorded as a verdict", after.State)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial: no route to host")
}

// A connection provider has no identity endpoint; the owning module's Test
// Connection is its real check.
func TestVerifyRefusesAProviderThatCannotCheckItself(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.Create(CreateInput{
		Provider: ProviderDatabase, Kind: KindSecret, Label: "staging",
		Secret: "hunter2", ScopeOwner: "profile-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(context.Background(), created.ID); err == nil {
		t.Fatal("a scoped database credential was verified against a provider that cannot")
	}
}

// http is refused outright: a verify endpoint belongs to a real provider, and
// there is no loopback exception for sending a credential in the clear.
func TestVerifyRefusesPlainHTTP(t *testing.T) {
	svc, _ := newService(t)
	svc.providers = NewRegistry([]Provider{{
		ID: "github", Name: "GitHub", Kinds: []Kind{KindToken},
		Verify: VerifySpec{URL: "http://127.0.0.1:9/user", Header: "Authorization", HeaderFormat: "Bearer %s"},
	}})
	svc.SetVerifyClient(&http.Client{})
	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "x", Secret: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(context.Background(), created.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("verify over http = %v, want ErrInvalid", err)
	}
}
